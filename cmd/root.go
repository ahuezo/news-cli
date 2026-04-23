package cmd

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"telecom-news-cli/internal/config"
	"telecom-news-cli/internal/crawler"
	"telecom-news-cli/internal/db"
	"telecom-news-cli/internal/models"
	catalogschema "telecom-news-cli/schemas"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var (
	dbPath           string
	credsPath        string
	sourcesPath      string
	categoriesPath   string
	authOverridePath string
	verbose          bool
)

var rootCmd = &cobra.Command{
	Use:   "telecom-news",
	Short: "📡 Telecom News CLI — crawl, store, and query telecom industry news",
	Long: `
Telecom News CLI

	Crawl 40+ telecom news sources, auto-classify by category and region, store in SQLite.
Query by date, category, region, country, or keyword search.

Some sources require a login. Use 'credentials set <source>' to configure them.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var (
			sources []crawler.Source
			err     error
		)
		if sourcesPath != "" {
			sources, err = crawler.LoadSourcesFromFile(sourcesPath)
			if err != nil {
				return fmt.Errorf("load sources file: %w", err)
			}
		} else {
			sources, err = crawler.LoadEmbeddedSources()
			if err != nil {
				return fmt.Errorf("load embedded sources: %w", err)
			}
		}
		crawler.UseSources(sources)

		var categoryCatalog *models.CategoryCatalog
		if categoriesPath != "" {
			categoryCatalog, err = models.LoadCategoryCatalogFromFile(categoriesPath)
			if err != nil {
				return fmt.Errorf("load categories file: %w", err)
			}
		} else {
			categoryCatalog, err = models.LoadEmbeddedCategoryCatalog()
			if err != nil {
				return fmt.Errorf("load embedded categories: %w", err)
			}
		}
		models.UseCategoryCatalog(categoryCatalog)

		var authSources []config.Credential
		if authOverridePath != "" {
			authSources, err = config.LoadAuthSourcesFromFile(authOverridePath)
			if err != nil {
				return fmt.Errorf("load auth override file: %w", err)
			}
		} else {
			authSources = crawler.KnownAuthSourcesFromSources(sources)
		}
		config.UseKnownAuthSources(authSources)
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	home, _ := os.UserHomeDir()
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", filepath.Join(home, ".telecom-news.db"), "Path to SQLite database")
	rootCmd.PersistentFlags().StringVar(&credsPath, "creds", config.DefaultPath(), "Path to credentials file")
	rootCmd.PersistentFlags().StringVar(&sourcesPath, "sources", "", "Path to alternate sources JSON catalog")
	rootCmd.PersistentFlags().StringVar(&categoriesPath, "categories", "", "Path to alternate categories JSON catalog")
	rootCmd.PersistentFlags().StringVar(&authOverridePath, "auth-override", "", "Path to optional auth override JSON catalog")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	rootCmd.AddCommand(fetchCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(credentialsCmd)
	rootCmd.AddCommand(sourcesCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(listCategoriesCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(categoriesCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(validateSchemaCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(exportCmd)
}

func openDB() (*db.DB, error)                     { return db.Open(dbPath) }
func loadCreds() (*config.CredentialStore, error) { return config.LoadOrCreate(credsPath) }

// ─── FETCH ────────────────────────────────────────────────────────────────────

var fetchCmd = &cobra.Command{
	Use:   "fetch [source-name]",
	Short: "Crawl RSS feeds and store articles",
	Long: `Fetch and index telecom news from all configured sources.
Credentials are loaded automatically for sources that require login.

Examples:
  telecom-news fetch
  telecom-news fetch "BNamericas Telecom"
  telecom-news fetch --verbose`,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		creds, _ := loadCreds() // non-fatal if missing
		c := crawler.NewWithCreds(database, creds)
		before, _ := database.Count()

		if len(args) > 0 {
			name := strings.Join(args, " ")
			found := false
			for _, src := range crawler.TelecomSources {
				if strings.EqualFold(src.Name, name) {
					found = true
					fmt.Printf("🔍 Fetching: %s\n", src.Name)
					n, err := c.CrawlSource(src, verbose)
					if err != nil {
						fmt.Printf("  ⚠  Error: %v\n", err)
					} else {
						fmt.Printf("  ✓  Saved %d new articles\n", n)
					}
				}
			}
			if !found {
				return fmt.Errorf("source %q not found — run 'telecom-news sources' to list all", name)
			}
		} else {
			fmt.Printf("🔍 Fetching %d sources...\n\n", len(crawler.TelecomSources))
			c.CrawlAll(verbose)
			after, _ := database.Count()
			fmt.Printf("\n✅ Done. New articles saved: %d (total in DB: %d)\n", after-before, after)
		}
		return nil
	},
}

// ─── LIST ─────────────────────────────────────────────────────────────────────

var (
	listCategory  string
	listRegion    string
	listCountry   string
	listDateFrom  string
	listDateTo    string
	listSortBy    string
	listSortOrder string
	listLimit     int
	listOffset    int
	listShowURL   bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List articles with optional filters",
	Long: `List stored articles. Filter by category, region, country, or date range.

Examples:
  telecom-news list
  telecom-news list --category network-infrastructure
  telecom-news list --region latam --sort-by date
  telecom-news list --from 2025-01-01 --to 2025-06-01
  telecom-news list --url            # print URLs only (pipeable)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		opts, err := buildListOptions(
			listCategory, listRegion, listCountry,
			listDateFrom, listDateTo,
			listSortBy, listSortOrder,
			listLimit, listOffset,
		)
		if err != nil {
			return err
		}

		articles, err := database.List(opts)
		if err != nil {
			return err
		}
		if listShowURL {
			printURLs(articles)
		} else {
			printArticles(articles, verbose)
		}
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listCategory, "category", "", "Filter by category slug")
	listCmd.Flags().StringVar(&listRegion, "region", "", "Filter by region: global|north-america|europe|asia-pacific|latam|mea")
	listCmd.Flags().StringVar(&listCountry, "country", "", "Filter by country code (e.g. US, MX, BR)")
	listCmd.Flags().StringVar(&listDateFrom, "from", "", "From date YYYY-MM-DD")
	listCmd.Flags().StringVar(&listDateTo, "to", "", "To date YYYY-MM-DD")
	listCmd.Flags().StringVar(&listSortBy, "sort-by", "date", "Sort by: date|category|region|source")
	listCmd.Flags().StringVar(&listSortOrder, "order", "desc", "Order: asc|desc")
	listCmd.Flags().IntVar(&listLimit, "limit", 25, "Max results")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "Pagination offset")
	listCmd.Flags().BoolVar(&listShowURL, "url", false, "Print URLs only (pipe-friendly)")
}

// ─── SEARCH ───────────────────────────────────────────────────────────────────

var (
	searchLimit   int
	searchShowURL bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search articles by keyword (all words must match)",
	Long: `Case-insensitive keyword search across title, abstract, category, and source.
All words in the query must appear somewhere in the article.

Examples:
  telecom-news search "open RAN"
  telecom-news search "5G mexico"
  telecom-news search "spectrum auction" --limit 10
  telecom-news search "generative AI" --url`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		query := strings.Join(args, " ")
		articles, err := database.Search(query, searchLimit)
		if err != nil {
			return fmt.Errorf("search error: %w", err)
		}
		fmt.Printf("🔎 Results for %q: %d found\n\n", query, len(articles))
		if searchShowURL {
			printURLs(articles)
		} else {
			printArticles(articles, verbose)
		}
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchLimit, "limit", 25, "Max results")
	searchCmd.Flags().BoolVar(&searchShowURL, "url", false, "Print URLs only (pipe-friendly)")
}

// ─── EXPORT ───────────────────────────────────────────────────────────────────

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export stored articles to machine-readable formats",
	Long: `Export stored articles to a machine-readable format.

Subcommands:
  csv   Export articles as CSV to stdout or a file`,
}

var (
	exportCategory  string
	exportRegion    string
	exportCountry   string
	exportDateFrom  string
	exportDateTo    string
	exportSortBy    string
	exportSortOrder string
	exportOutput    string
)

var exportCSVCmd = &cobra.Command{
	Use:   "csv",
	Short: "Export articles to CSV",
	Long: `Export stored articles to CSV.

Examples:
  telecom-news export csv
  telecom-news export csv --output telecom-news.csv
  telecom-news export csv --from 2025-01-01 --to 2025-03-31 --output q1-news.csv
  telecom-news export csv --category network-infrastructure --region latam`,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		opts, err := buildListOptions(
			exportCategory, exportRegion, exportCountry,
			exportDateFrom, exportDateTo,
			exportSortBy, exportSortOrder,
			0, 0,
		)
		if err != nil {
			return err
		}

		articles, err := database.ListAll(opts)
		if err != nil {
			return err
		}

		out := io.Writer(os.Stdout)
		if exportOutput != "" {
			f, err := os.Create(exportOutput)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()
			out = f
		}

		if err := writeArticlesCSV(out, articles); err != nil {
			return fmt.Errorf("write csv: %w", err)
		}

		dest := "stdout"
		if exportOutput != "" {
			dest = exportOutput
		}
		fmt.Fprintf(os.Stderr, "Exported %d article(s) to %s\n", len(articles), dest)
		return nil
	},
}

func init() {
	exportCmd.AddCommand(exportCSVCmd)
	exportCSVCmd.Flags().StringVar(&exportCategory, "category", "", "Filter by category slug")
	exportCSVCmd.Flags().StringVar(&exportRegion, "region", "", "Filter by region: global|north-america|europe|asia-pacific|latam|mea")
	exportCSVCmd.Flags().StringVar(&exportCountry, "country", "", "Filter by country code (e.g. US, MX, BR)")
	exportCSVCmd.Flags().StringVar(&exportDateFrom, "from", "", "From date YYYY-MM-DD")
	exportCSVCmd.Flags().StringVar(&exportDateTo, "to", "", "To date YYYY-MM-DD")
	exportCSVCmd.Flags().StringVar(&exportSortBy, "sort-by", "date", "Sort by: date|category|region|source")
	exportCSVCmd.Flags().StringVar(&exportSortOrder, "order", "desc", "Order: asc|desc")
	exportCSVCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Write CSV to file instead of stdout")
}

// ─── OPEN ─────────────────────────────────────────────────────────────────────

var openCmd = &cobra.Command{
	Use:   "open <number|url>",
	Short: "Open an article in your default browser",
	Long: `Open an article in your default browser.

  telecom-news open 3                      # open row #3 from last list/search
  telecom-news open https://example.com    # open any URL directly`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		if n, err := strconv.Atoi(target); err == nil {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			articles, err := database.List(models.ListOptions{Limit: n, SortBy: "date", SortOrder: "desc"})
			if err != nil {
				return err
			}
			if n < 1 || n > len(articles) {
				return fmt.Errorf("article #%d not in range (1–%d)", n, len(articles))
			}
			target = articles[n-1].URL
			fmt.Printf("📰 [%d] %s\n", n, articles[n-1].Title)
		}
		fmt.Printf("🌐 Opening: %s\n", target)
		return openBrowser(target)
	},
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		for _, bin := range []string{"xdg-open", "sensible-browser", "x-www-browser"} {
			if path, err := exec.LookPath(bin); err == nil {
				cmd = exec.Command(path, url)
				break
			}
		}
		if cmd == nil {
			fmt.Printf("\n  No browser launcher found.\n  Copy and paste:\n\n  %s\n\n", url)
			return nil
		}
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// ─── CREDENTIALS ──────────────────────────────────────────────────────────────

var credentialsCmd = &cobra.Command{
	Use:   "credentials",
	Short: "Manage login credentials for sources that require authentication",
	Long: `Store and manage credentials for gated/subscription sources.
Credentials are saved in ` + config.DefaultPath() + ` with 0600 permissions.

Subcommands:
  list         Show all configured credentials
  set <name>   Add or update credentials for a source (interactive)
  remove <name> Delete credentials for a source
  info         Show which sources are known to require login`,
}

var credsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := loadCreds()
		if err != nil {
			return err
		}
		if len(store.Credentials) == 0 {
			fmt.Println("No credentials configured.")
			fmt.Printf("Run: telecom-news credentials info\n")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SOURCE\tAUTH TYPE\tUSERNAME / TOKEN")
		fmt.Fprintln(w, "──────\t─────────\t────────────────")
		for _, c := range store.Credentials {
			display := c.Username
			if display == "" && c.Token != "" {
				display = c.Token[:min(len(c.Token), 12)] + "..."
			}
			if display == "" && len(c.Cookies) > 0 {
				names := []string{}
				for k := range c.Cookies {
					names = append(names, k)
				}
				display = "cookies: " + strings.Join(names, ", ")
			} else if display == "" && c.RawCookie != "" {
				display = "(raw cookie set)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", c.SourceName, c.AuthType, display)
		}
		w.Flush()
		fmt.Printf("\nCredentials file: %s\n", store.Path())
		return nil
	},
}

var credsSetCmd = &cobra.Command{
	Use:   "set <source-name>",
	Short: "Add or update credentials for a source (interactive prompt)",
	Long: `Interactively set credentials for a named source.
Run 'telecom-news credentials info' to see which sources need auth and what type.

Examples:
  telecom-news credentials set "BNamericas Telecom"
  telecom-news credentials set "Telecompaper"
  telecom-news credentials set "El Economista – Telecomunicaciones"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceName := strings.Join(args, " ")
		store, err := loadCreds()
		if err != nil {
			return err
		}

		// Find known auth info for this source
		var known *config.Credential
		known = config.FindKnownAuthSource(sourceName)

		cred := config.Credential{SourceName: sourceName}
		scanner := bufio.NewScanner(os.Stdin)

		if known != nil {
			fmt.Printf("📋 Notes for %s:\n   %s\n\n", sourceName, known.Notes)
			cred.AuthType = known.AuthType
		} else {
			fmt.Printf("Auth type for %q\n", sourceName)
			fmt.Println("  1) basic   — username + password")
			fmt.Println("  2) cookie  — paste the full Cookie request header")
			fmt.Println("  3) bearer  — Bearer token / API key")
			fmt.Println("  4) header  — custom HTTP header + value")
			fmt.Print("Choice [1-4]: ")
			scanner.Scan()
			switch strings.TrimSpace(scanner.Text()) {
			case "2":
				cred.AuthType = config.AuthCookie
			case "3":
				cred.AuthType = config.AuthToken
			case "4":
				cred.AuthType = config.AuthHeader
			default:
				cred.AuthType = config.AuthBasic
			}
		}

		switch cred.AuthType {
		case config.AuthBasic:
			fmt.Print("Username / Email: ")
			scanner.Scan()
			cred.Username = strings.TrimSpace(scanner.Text())
			fmt.Print("Password: ")
			scanner.Scan()
			cred.Password = strings.TrimSpace(scanner.Text())

		case config.AuthCookie:
			fmt.Println("How to get the Cookie header from your browser:")
			fmt.Println("  1. Open the site and log in")
			fmt.Println("  2. Press F12 → Network tab → reload the page")
			fmt.Println("  3. Click any request → Request Headers")
			fmt.Println("  4. Copy the full Cookie header value")
			fmt.Println()
			fmt.Println("Paste only the header value, for example:")
			fmt.Println("  session_token=abc123; _gid=GA1.2.123456789")
			fmt.Print("Cookie: ")
			scanner.Scan()
			cred.RawCookie = strings.TrimSpace(scanner.Text())
			cred.Cookies = nil
			if cred.RawCookie == "" {
				return fmt.Errorf("cookie header cannot be empty")
			}

		case config.AuthToken:
			fmt.Print("Bearer token / API key: ")
			scanner.Scan()
			cred.Token = strings.TrimSpace(scanner.Text())

		case config.AuthHeader:
			fmt.Print("Header name (e.g. X-Api-Key): ")
			scanner.Scan()
			cred.HeaderName = strings.TrimSpace(scanner.Text())
			fmt.Print("Header value: ")
			scanner.Scan()
			cred.Token = strings.TrimSpace(scanner.Text())
		}

		store.Set(cred)
		if err := store.Save(); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
		fmt.Printf("\n✅ Credentials saved for %q (%s)\n", sourceName, cred.AuthType)
		fmt.Printf("   File: %s\n", store.Path())
		return nil
	},
}

var credsRemoveCmd = &cobra.Command{
	Use:   "remove <source-name>",
	Short: "Remove credentials for a source",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceName := strings.Join(args, " ")
		store, err := loadCreds()
		if err != nil {
			return err
		}
		if !store.Remove(sourceName) {
			return fmt.Errorf("no credentials found for %q", sourceName)
		}
		if err := store.Save(); err != nil {
			return err
		}
		fmt.Printf("✅ Credentials removed for %q\n", sourceName)
		return nil
	},
}

var credsInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show which sources require login and how to authenticate",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Sources known to require authentication:")
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SOURCE\tAUTH TYPE\tNOTES")
		fmt.Fprintln(w, "──────\t─────────\t─────")
		for _, k := range config.KnownAuthSources {
			notes := k.Notes
			if len(notes) > 80 {
				notes = notes[:77] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", k.SourceName, k.AuthType, notes)
		}
		w.Flush()
		fmt.Printf("\nTo configure: telecom-news credentials set \"<source name>\"\n")
	},
}

func init() {
	credentialsCmd.AddCommand(credsListCmd)
	credentialsCmd.AddCommand(credsSetCmd)
	credentialsCmd.AddCommand(credsRemoveCmd)
	credentialsCmd.AddCommand(credsInfoCmd)
}

// ─── SOURCES ──────────────────────────────────────────────────────────────────

var sourcesCmd = &cobra.Command{
	Use:   "sources",
	Short: "List all configured sources",
	Run: func(cmd *cobra.Command, args []string) {
		store, _ := loadCreds()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tREGION\tACCESS\tAUTH\tENDPOINT")
		fmt.Fprintln(w, "────\t──────\t──────\t────\t────────")
		for _, src := range crawler.TelecomSources {
			authStatus := "public"
			if src.RequiresAuth {
				authStatus = "⚠ login required"
				if store != nil && store.Get(src.Name) != nil {
					authStatus = "✓ configured"
				}
			} else if config.FindKnownAuthSource(src.Name) != nil {
				authStatus = "auth metadata only"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", src.Name, src.Region, src.AccessMethod(), authStatus, src.EndpointURL())
		}
		w.Flush()
	},
}

func authSourceNames(authSources []config.Credential) []string {
	names := make([]string, 0, len(authSources))
	for _, src := range authSources {
		names = append(names, src.SourceName)
	}
	return names
}

// ─── STATS ────────────────────────────────────────────────────────────────────

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show article counts by category and region",
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		total, _ := database.Count()
		byCat, byRegion, err := database.Stats()
		if err != nil {
			return err
		}

		fmt.Printf("📊 Telecom News — DB: %s\n   Total articles: %d\n\n", dbPath, total)
		fmt.Println("By Category:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  SLUG\tNAME\tCOUNT")
		fmt.Fprintln(w, "  ────\t────\t─────")
		for _, cat := range models.TelecomCategories {
			fmt.Fprintf(w, "  %s\t%s\t%d\n", cat.Slug, cat.Name, byCat[cat.Slug])
		}
		w.Flush()
		fmt.Println("\nBy Region:")
		w2 := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w2, "  REGION\tCOUNT")
		fmt.Fprintln(w2, "  ──────\t─────")
		for _, r := range []string{"global", "north-america", "europe", "asia-pacific", "latam", "mea"} {
			fmt.Fprintf(w2, "  %s\t%d\n", r, byRegion[r])
		}
		w2.Flush()
		return nil
	},
}

var listCategoriesCmd = &cobra.Command{
	Use:   "list-categories",
	Short: "List distinct categories found in stored articles",
	Long: `List the different category values currently present in the article database.

Examples:
  telecom-news list-categories
  telecom-news --db /path/to/news.db list-categories`,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		categories, err := database.DistinctCategories()
		if err != nil {
			return err
		}
		if len(categories) == 0 {
			fmt.Println("(no categories found)")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "CATEGORY\tNAME\tCOUNT")
		fmt.Fprintln(w, "────────\t────\t─────")
		for _, item := range categories {
			fmt.Fprintf(w, "%s\t%s\t%d\n", item.Category, categoryName(item.Category), item.Count)
		}
		w.Flush()
		fmt.Printf("\n%d distinct category value(s)\n", len(categories))
		return nil
	},
}

// ─── ADD ──────────────────────────────────────────────────────────────────────

var (
	addCategory string
	addRegion   string
	addCountry  string
)

var addCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Manually scrape and add a single article URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		creds, _ := loadCreds()
		c := crawler.NewWithCreds(database, creds)
		art, err := c.ScrapeURL(args[0], addCategory)
		if err != nil {
			return fmt.Errorf("scrape error: %w", err)
		}
		if addRegion != "" {
			art.Region = addRegion
		}
		if addCountry != "" {
			art.Country = addCountry
		}
		if err := database.UpsertArticle(art); err != nil {
			return fmt.Errorf("save error: %w", err)
		}
		fmt.Println("✅ Article saved:")
		printArticles([]models.Article{*art}, true)
		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&addCategory, "category", "", "Category slug override")
	addCmd.Flags().StringVar(&addRegion, "region", "", "Region override")
	addCmd.Flags().StringVar(&addCountry, "country", "", "Country code (e.g. MX, US, BR)")
}

// ─── CATEGORIES ───────────────────────────────────────────────────────────────

var categoriesCmd = &cobra.Command{
	Use:   "categories",
	Short: "List all telecom business categories and slugs",
	Run: func(cmd *cobra.Command, args []string) {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SLUG\tNAME\tDESCRIPTION")
		fmt.Fprintln(w, "────\t────\t───────────")
		for _, cat := range models.TelecomCategories {
			fmt.Fprintf(w, "%s\t%s\t%s\n", cat.Slug, cat.Name, cat.Description)
		}
		w.Flush()
	},
}

// ─── VALIDATE ─────────────────────────────────────────────────────────────────

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Check source, category, and auth catalogs for configuration issues",
	Long: `Scans TelecomSources for:
  - Duplicate source names
  - Two sources pointing to the same endpoint URL (causes double-ingestion)
  - Invalid access configuration

Scans categories for:
  - Duplicate category ids or slugs
  - Missing names
  - Invalid default category
  - Sources referencing unknown category slugs

Scans auth sources for:
  - Duplicate source names
  - Unsupported auth types
  - Missing setup notes

Examples:
  telecom-news validate`,
	Run: func(cmd *cobra.Command, args []string) {
		sourceWarnings := crawler.ValidateSources()
		categoryWarnings := models.ValidateActiveCategories()
		authWarnings := config.ValidateKnownAuthSources()
		sourceNames := make([]string, 0, len(crawler.TelecomSources))
		authRequiredSourceNames := make([]string, 0, len(crawler.TelecomSources))
		for _, src := range crawler.TelecomSources {
			sourceNames = append(sourceNames, src.Name)
			if src.RequiresAuth {
				authRequiredSourceNames = append(authRequiredSourceNames, src.Name)
			}
		}
		linkWarnings := config.ValidateAuthSourcesAgainstSources(config.KnownAuthSources, sourceNames)
		linkWarnings = append(linkWarnings, crawler.ValidateSourcesAgainstAuth(authRequiredSourceNames, authSourceNames(config.KnownAuthSources))...)

		if len(sourceWarnings) == 0 && len(categoryWarnings) == 0 && len(authWarnings) == 0 && len(linkWarnings) == 0 {
			fmt.Printf("✅ Source catalog clean: %d sources\n", len(crawler.TelecomSources))
			fmt.Printf("✅ Category catalog clean: %d categories\n", len(models.TelecomCategories))
			fmt.Printf("✅ Auth catalog clean: %d auth entries\n", len(config.KnownAuthSources))
			fmt.Println("✅ Cross-catalog references clean")
			return
		}

		total := len(sourceWarnings) + len(categoryWarnings) + len(authWarnings) + len(linkWarnings)
		fmt.Printf("⚠  Found %d issue(s):\n\n", total)

		if len(sourceWarnings) > 0 {
			fmt.Println("Source catalog:")
			for i, w := range sourceWarnings {
				fmt.Printf("  %d) %s\n", i+1, w)
			}
			fmt.Println()
		}

		if len(categoryWarnings) > 0 {
			fmt.Println("Category catalog:")
			for i, w := range categoryWarnings {
				fmt.Printf("  %d) %s\n", i+1, w)
			}
			fmt.Println()
		}

		if len(authWarnings) > 0 {
			fmt.Println("Auth catalog:")
			for i, w := range authWarnings {
				fmt.Printf("  %d) %s\n", i+1, w)
			}
			fmt.Println()
		}

		if len(linkWarnings) > 0 {
			fmt.Println("Cross-catalog references:")
			for i, w := range linkWarnings {
				fmt.Printf("  %d) %s\n", i+1, w)
			}
			fmt.Println()
		}

		fmt.Println("Fix these in internal/crawler/sources.json, internal/models/categories.json, or your override JSON files")
	},
}

var validateSchemaCmd = &cobra.Command{
	Use:          "validate-schema <sources|categories|auth> <file>",
	Short:        "Validate a JSON catalog file against the shipped JSON Schema",
	SilenceUsage: true,
	Args:         cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		issues, err := catalogschema.ValidateFile(args[0], args[1])
		if err != nil {
			return err
		}
		if len(issues) == 0 {
			fmt.Printf("✅ %s schema valid: %s\n", args[0], args[1])
			return nil
		}

		fmt.Printf("⚠  %s schema validation failed for %s:\n", args[0], args[1])
		for i, issue := range issues {
			fmt.Printf("  %d) %s\n", i+1, issue)
		}
		return fmt.Errorf("schema validation failed")
	},
}

var healthCmd = &cobra.Command{
	Use:   "health [source-name]",
	Short: "Probe source endpoints and parsing without saving articles",
	Long: `Runs a live health check against configured sources.
It verifies that the endpoint is reachable and that the configured crawl method
can parse at least one item, without writing anything to the database.

Examples:
  telecom-news health
  telecom-news health "BNamericas Telecom"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		creds, _ := loadCreds()
		c := crawler.NewWithCreds(nil, creds)

		sources := crawler.TelecomSources
		if len(args) > 0 {
			name := strings.Join(args, " ")
			filtered := make([]crawler.Source, 0, 1)
			for _, src := range crawler.TelecomSources {
				if strings.EqualFold(src.Name, name) {
					filtered = append(filtered, src)
				}
			}
			if len(filtered) == 0 {
				return fmt.Errorf("source %q not found — run 'telecom-news sources' to list all", name)
			}
			sources = filtered
		}

		fmt.Printf("🩺 Checking %d sources...\n\n", len(sources))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STATUS\tSOURCE\tMETHOD\tITEMS\tDETAIL")
		fmt.Fprintln(w, "──────\t──────\t──────\t─────\t──────")

		failures := 0
		warnings := 0
		for _, src := range sources {
			result, err := c.ProbeSource(src)
			if err != nil {
				failures++
				fmt.Fprintf(w, "FAIL\t%s\t%s\t-\t%s\n", src.Name, src.AccessMethod(), err)
				continue
			}
			detail := result.FinalURL
			if detail == "" {
				detail = result.Endpoint
			}
			if result.ItemCount == 0 {
				warnings++
				fmt.Fprintf(w, "WARN\t%s\t%s\t0\tparsed successfully but returned no items (%s)\n", src.Name, result.Method, detail)
				continue
			}
			fmt.Fprintf(w, "OK\t%s\t%s\t%d\t%s\n", src.Name, result.Method, result.ItemCount, detail)
		}
		w.Flush()

		if failures > 0 || warnings > 0 {
			return fmt.Errorf("health check found %d failing and %d empty source(s)", failures, warnings)
		}
		fmt.Printf("\n✅ All sources healthy: %d checked\n", len(sources))
		return nil
	},
}

// ─── COOKIE SUBCOMMAND ────────────────────────────────────────────────────────

var credsCookieCmd = &cobra.Command{
	Use:   "cookie",
	Short: "Manage individual cookies for a source",
	Long: `Add, update, remove, or list individual named cookies for a source.

Examples:
  telecom-news credentials cookie set "El Economista – Telecomunicaciones" session_token abc123
  telecom-news credentials cookie remove "El Economista – Telecomunicaciones" session_token
  telecom-news credentials cookie list "El Economista – Telecomunicaciones"`,
}

var credsCookieSetCmd = &cobra.Command{
	Use:   "set <source> <cookie-name> <cookie-value>",
	Short: "Add or update a single named cookie for a source",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceName, cookieName, cookieValue := args[0], args[1], args[2]
		store, err := loadCreds()
		if err != nil {
			return err
		}
		store.SetCookie(sourceName, cookieName, cookieValue)
		if err := store.Save(); err != nil {
			return err
		}
		fmt.Printf("✅ Cookie %q set for %q\n", cookieName, sourceName)
		return nil
	},
}

var credsCookieRemoveCmd = &cobra.Command{
	Use:   "remove <source> <cookie-name>",
	Short: "Remove a single named cookie from a source",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		sourceName, cookieName := args[0], args[1]
		store, err := loadCreds()
		if err != nil {
			return err
		}
		if !store.RemoveCookie(sourceName, cookieName) {
			return fmt.Errorf("cookie %q not found for %q", cookieName, sourceName)
		}
		if err := store.Save(); err != nil {
			return err
		}
		fmt.Printf("✅ Cookie %q removed from %q\n", cookieName, sourceName)
		return nil
	},
}

var credsCookieListCmd = &cobra.Command{
	Use:   "list <source>",
	Short: "List all named cookies stored for a source",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := loadCreds()
		if err != nil {
			return err
		}
		cred := store.Get(args[0])
		if cred == nil {
			return fmt.Errorf("no credentials found for %q", args[0])
		}
		if len(cred.Cookies) == 0 && cred.RawCookie == "" {
			fmt.Println("No cookies configured for this source.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tVALUE (truncated)")
		fmt.Fprintln(w, "────\t──────────────────")
		for name, value := range cred.Cookies {
			fmt.Fprintf(w, "%s\t%s\n", name, truncate(value, 40))
		}
		if cred.RawCookie != "" {
			fmt.Fprintf(w, "(raw)\t%s\n", truncate(cred.RawCookie, 40))
		}
		w.Flush()
		return nil
	},
}

func init() {
	credsCookieCmd.AddCommand(credsCookieSetCmd)
	credsCookieCmd.AddCommand(credsCookieRemoveCmd)
	credsCookieCmd.AddCommand(credsCookieListCmd)
	credentialsCmd.AddCommand(credsCookieCmd)
}

// ─── HELPERS ──────────────────────────────────────────────────────────────────

func printArticles(articles []models.Article, detail bool) {
	if len(articles) == 0 {
		fmt.Println("(no articles found)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tDATE\tCATEGORY\tREGION\tSOURCE\tTITLE")
	fmt.Fprintln(w, "─\t────\t────────\t──────\t──────\t─────")
	for i, a := range articles {
		title := a.Title
		if len(title) > 65 {
			title = title[:62] + "..."
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1, a.PublishedAt.Format("2006-01-02"), a.Category, a.Region, a.Source, title)
	}
	w.Flush()
	fmt.Printf("\n%d article(s)  —  run: telecom-news open <#> to open in browser\n", len(articles))

	if detail {
		fmt.Println()
		for i, a := range articles {
			fmt.Printf("[%d] %s\n", i+1, a.Title)
			fmt.Printf("    🔗 %s\n", a.URL)
			fmt.Printf("    📅 %s   🗂  %s   🌎 %s", a.PublishedAt.Format("2006-01-02 15:04"), a.Category, a.Region)
			if a.Country != "" {
				fmt.Printf("   🏳 %s", a.Country)
			}
			fmt.Println()
			if a.Abstract != "" {
				abs := a.Abstract
				if len(abs) > 220 {
					abs = abs[:217] + "..."
				}
				fmt.Printf("    %s\n", abs)
			}
			fmt.Println()
		}
	}
}

func printURLs(articles []models.Article) {
	for _, a := range articles {
		fmt.Println(a.URL)
	}
}

func writeArticlesCSV(w io.Writer, articles []models.Article) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"id", "url", "title", "abstract", "published_at",
		"category", "region", "country", "source", "created_at",
	}); err != nil {
		return err
	}

	for _, a := range articles {
		if err := cw.Write([]string{
			strconv.FormatInt(a.ID, 10),
			a.URL,
			a.Title,
			a.Abstract,
			formatCSVTime(a.PublishedAt),
			a.Category,
			a.Region,
			a.Country,
			a.Source,
			formatCSVTime(a.CreatedAt),
		}); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
}

func parseDate(s string) (time.Time, error) {
	for _, f := range []string{"2006-01-02", "2006/01/02", "01-02-2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format %q, use YYYY-MM-DD", s)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func buildListOptions(category, region, country, dateFrom, dateTo, sortBy, sortOrder string, limit, offset int) (models.ListOptions, error) {
	opts := models.ListOptions{
		Category:  category,
		Region:    region,
		Country:   country,
		SortBy:    sortBy,
		SortOrder: sortOrder,
		Limit:     limit,
		Offset:    offset,
	}

	if dateFrom != "" {
		t, err := parseDate(dateFrom)
		if err != nil {
			return models.ListOptions{}, err
		}
		opts.DateFrom = t
	}
	if dateTo != "" {
		t, err := parseDate(dateTo)
		if err != nil {
			return models.ListOptions{}, err
		}
		opts.DateTo = t.Add(24 * time.Hour)
	}

	return opts, nil
}

func formatCSVTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func categoryName(slug string) string {
	for _, cat := range models.TelecomCategories {
		if cat.Slug == slug {
			return cat.Name
		}
	}
	return "(not in catalog)"
}

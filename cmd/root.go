package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"telecom-news-cli/internal/crawler"
	"telecom-news-cli/internal/db"
	"telecom-news-cli/internal/models"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var (
	dbPath  string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "telecom-news",
	Short: "📡 Telecom News CLI — crawl, store, and query telecom industry news",
	Long: `
Telecom News CLI

Crawl RSS feeds from 18+ telecom publications, auto-classify articles
into 11 business categories and 6 world regions, store in SQLite, and
query by date, category, region, country, or full-text search.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".telecom-news.db")

	rootCmd.PersistentFlags().StringVar(&dbPath, "db", defaultDB, "Path to SQLite database file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	rootCmd.AddCommand(fetchCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(sourcesCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(categoriesCmd)
}

func openDB() (*db.DB, error) {
	return db.Open(dbPath)
}

// ─── FETCH ────────────────────────────────────────────────────────────────────

var fetchCmd = &cobra.Command{
	Use:   "fetch [source-name]",
	Short: "Crawl RSS feeds and store articles",
	Long: `Fetch and index telecom news from all configured RSS sources.
Optionally pass a source name to fetch only that one.

Examples:
  telecom-news fetch
  telecom-news fetch "Light Reading"
  telecom-news fetch --verbose`,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		c := crawler.New(database)
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
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List articles with optional filters",
	Long: `List stored articles filtered by category, region, country, or date range.

Examples:
  telecom-news list
  telecom-news list --category network-infrastructure
  telecom-news list --region europe --sort-by date --order asc
  telecom-news list --from 2025-01-01 --to 2025-06-01
  telecom-news list --category cybersecurity --region global --limit 20`,
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		opts := models.ListOptions{
			Category:  listCategory,
			Region:    listRegion,
			Country:   listCountry,
			SortBy:    listSortBy,
			SortOrder: listSortOrder,
			Limit:     listLimit,
			Offset:    listOffset,
		}

		if listDateFrom != "" {
			t, err := parseDate(listDateFrom)
			if err != nil {
				return fmt.Errorf("invalid --from: %w", err)
			}
			opts.DateFrom = t
		}
		if listDateTo != "" {
			t, err := parseDate(listDateTo)
			if err != nil {
				return fmt.Errorf("invalid --to: %w", err)
			}
			opts.DateTo = t.Add(24 * time.Hour)
		}

		articles, err := database.List(opts)
		if err != nil {
			return err
		}
		printArticles(articles, verbose)
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listCategory, "category", "", "Filter by category slug (run 'categories' to list)")
	listCmd.Flags().StringVar(&listRegion, "region", "", "Filter by region: global|north-america|europe|asia-pacific|latam|mea")
	listCmd.Flags().StringVar(&listCountry, "country", "", "Filter by country code (e.g. US, GB, DE)")
	listCmd.Flags().StringVar(&listDateFrom, "from", "", "From date YYYY-MM-DD")
	listCmd.Flags().StringVar(&listDateTo, "to", "", "To date YYYY-MM-DD")
	listCmd.Flags().StringVar(&listSortBy, "sort-by", "date", "Sort by: date|category|region|source")
	listCmd.Flags().StringVar(&listSortOrder, "order", "desc", "Order: asc|desc")
	listCmd.Flags().IntVar(&listLimit, "limit", 25, "Max results")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "Pagination offset")
}

// ─── SEARCH ───────────────────────────────────────────────────────────────────

var searchLimit int

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search across titles, abstracts, categories, and sources",
	Long: `Full-text search using SQLite FTS5. Supports boolean operators.

Examples:
  telecom-news search "open RAN"
  telecom-news search "5G AND security"
  telecom-news search "spectrum OR bandwidth" --limit 10
  telecom-news search "generative AI NOT chatbot"
  telecom-news search "veri*"`,
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
			return fmt.Errorf("search error (check FTS5 syntax): %w", err)
		}

		fmt.Printf("🔎 Results for %q: %d found\n\n", query, len(articles))
		printArticles(articles, verbose)
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchLimit, "limit", 25, "Max results")
}

// ─── SOURCES ──────────────────────────────────────────────────────────────────

var sourcesCmd = &cobra.Command{
	Use:   "sources",
	Short: "List all configured RSS sources",
	Run: func(cmd *cobra.Command, args []string) {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tREGION\tCATEGORY\tFEED URL")
		fmt.Fprintln(w, "────\t──────\t────────\t────────")
		for _, src := range crawler.TelecomSources {
			cat := src.Category
			if cat == "" {
				cat = "(auto-detect)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", src.Name, src.Region, cat, src.FeedURL)
		}
		w.Flush()
	},
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

// ─── ADD ──────────────────────────────────────────────────────────────────────

var (
	addCategory string
	addRegion   string
	addCountry  string
)

var addCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Manually scrape and add a single article URL",
	Long: `Scrape metadata from a URL and save it to the database.
Category and region are auto-detected if not specified.

Examples:
  telecom-news add https://example.com/article
  telecom-news add https://example.com/article --category cybersecurity --region europe`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDB()
		if err != nil {
			return err
		}
		defer database.Close()

		c := crawler.New(database)
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
	addCmd.Flags().StringVar(&addCountry, "country", "", "Country code override (e.g. US)")
}

// ─── CATEGORIES ───────────────────────────────────────────────────────────────

var categoriesCmd = &cobra.Command{
	Use:   "categories",
	Short: "List all telecom business categories and their slugs",
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
		if len(title) > 70 {
			title = title[:67] + "..."
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1, a.PublishedAt.Format("2006-01-02"), a.Category, a.Region, a.Source, title)
	}
	w.Flush()
	fmt.Printf("\n%d article(s)\n", len(articles))

	if detail {
		fmt.Println()
		for i, a := range articles {
			fmt.Printf("[%d] %s\n", i+1, a.Title)
			fmt.Printf("    URL:      %s\n", a.URL)
			fmt.Printf("    Date:     %s\n", a.PublishedAt.Format("2006-01-02 15:04"))
			fmt.Printf("    Category: %s  |  Region: %s", a.Category, a.Region)
			if a.Country != "" {
				fmt.Printf("  |  Country: %s", a.Country)
			}
			fmt.Println()
			if a.Abstract != "" {
				abs := a.Abstract
				if len(abs) > 200 {
					abs = abs[:197] + "..."
				}
				fmt.Printf("    Abstract: %s\n", abs)
			}
			fmt.Println()
		}
	}
}

func parseDate(s string) (time.Time, error) {
	for _, f := range []string{"2006-01-02", "2006/01/02", "01-02-2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported format %q, use YYYY-MM-DD", s)
}

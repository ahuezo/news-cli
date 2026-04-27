package crawler

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"telecom-news-cli/internal/config"
	"telecom-news-cli/internal/db"
	"telecom-news-cli/internal/models"
)

const (
	elEconomistaSourceName = "El Economista – Telecomunicaciones"
)

var elEconomistaURLDatePattern = regexp.MustCompile(`-(\d{8})-\d+\.html$`)
var inlinePublishedDatePattern = regexp.MustCompile(`([A-Z][a-z]{2},\s+\d{2}/\d{2}/\d{4}\s+-\s+\d{2}:\d{2}|\d{2}/\d{2}/\d{4}\s+-\s+\d{2}:\d{2})`)
var leadingLongDatePattern = regexp.MustCompile(`^\s*([0-9]{1,2}\s+[A-Z][a-z]+\s+[0-9]{4})\b`)

type AccessMethod string

const (
	AccessMethodRSS      AccessMethod = "rss"
	AccessMethodHTMLList AccessMethod = "html_list"
	AccessMethodAPI      AccessMethod = "api"
)

type HTMLListConfig struct {
	ArticleSelector string `json:"article_selector"`
	TitleLinkSel    string `json:"title_link_selector"`
	DescriptionSel  string `json:"description_selector"`
	TimeSelector    string `json:"time_selector"`
	TimeAttr        string `json:"time_attr"`
	URLContains     string `json:"url_contains"`
}

type APIConfig struct {
	ItemsPath        string `json:"items_path"`
	TitleField       string `json:"title_field"`
	URLField         string `json:"url_field"`
	DescriptionField string `json:"description_field,omitempty"`
	TimeField        string `json:"time_field,omitempty"`
	TimeFormat       string `json:"time_format,omitempty"`
}

type AccessConfig struct {
	Method   AccessMethod    `json:"method"`
	URL      string          `json:"url"`
	HTMLList *HTMLListConfig `json:"html_list,omitempty"`
	API      *APIConfig      `json:"api,omitempty"`
}

// Source defines a source target and its access configuration.
type Source struct {
	Name         string             `json:"name"`
	FeedURL      string             `json:"feed_url,omitempty"`
	Region       string             `json:"region"`
	Category     string             `json:"category,omitempty"` // override; empty = auto-detect
	RequiresAuth bool               `json:"requires_auth,omitempty"`
	Auth         *config.Credential `json:"auth,omitempty"`
	Access       AccessConfig       `json:"access,omitempty"`
}

func (s Source) AccessMethod() AccessMethod {
	if s.Access.Method != "" {
		return s.Access.Method
	}
	return AccessMethodRSS
}

func (s Source) EndpointURL() string {
	if s.Access.URL != "" {
		return s.Access.URL
	}
	return s.FeedURL
}

//go:embed sources.json
var defaultSourcesJSON []byte

// TelecomSources is the master list of telecom news sources loaded from config.
var TelecomSources = mustLoadSources(defaultSourcesJSON)

func mustLoadSources(data []byte) []Source {
	sources, err := parseSources(data)
	if err != nil {
		panic(fmt.Sprintf("load sources config: %v", err))
	}
	return sources
}

func parseSources(data []byte) ([]Source, error) {
	var sources []Source
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil, err
	}
	return sources, nil
}

func DefaultSources() []Source {
	return append([]Source(nil), TelecomSources...)
}

func LoadEmbeddedSources() ([]Source, error) {
	return parseSources(defaultSourcesJSON)
}

func LoadSourcesFromFile(path string) ([]Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseSources(data)
}

func SaveSourcesToFile(path string, sources []Source) error {
	data, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func UseSources(sources []Source) {
	TelecomSources = append([]Source(nil), sources...)
}

// Crawler handles fetching and storing articles
type Crawler struct {
	db     *db.DB
	creds  *config.CredentialStore
	client *http.Client
	parser *gofeed.Parser
}

type ProbeResult struct {
	Source    string
	Method    AccessMethod
	Endpoint  string
	FinalURL  string
	ItemCount int
}

// New creates a Crawler without credentials (public feeds only)
func New(database *db.DB) *Crawler {
	return NewWithCreds(database, nil)
}

// NewWithCreds creates a Crawler that injects auth for gated sources
func NewWithCreds(database *db.DB, creds *config.CredentialStore) *Crawler {
	return &Crawler{
		db:     database,
		creds:  creds,
		client: &http.Client{Timeout: 20 * time.Second},
		parser: gofeed.NewParser(),
	}
}

// ValidateSources checks TelecomSources for duplicate names, duplicate endpoint URLs,
// and incomplete access configuration.
func ValidateSources() []string {
	seenNames := map[string]string{}
	seenURLs := map[string]string{}
	var warnings []string

	for _, src := range TelecomSources {
		name := strings.ToLower(strings.TrimSpace(src.Name))
		rawURL := strings.TrimRight(strings.ToLower(strings.TrimSpace(src.EndpointURL())), "/")

		if prev, ok := seenNames[name]; ok {
			warnings = append(warnings, fmt.Sprintf("duplicate source name: %q (conflicts with %q)", src.Name, prev))
		} else {
			seenNames[name] = src.Name
		}

		if rawURL == "" {
			warnings = append(warnings, fmt.Sprintf("missing endpoint URL in %q", src.Name))
		} else if prev, ok := seenURLs[rawURL]; ok {
			warnings = append(warnings, fmt.Sprintf("duplicate endpoint URL in %q — same as %q: %s", src.Name, prev, src.EndpointURL()))
		} else {
			seenURLs[rawURL] = src.Name
		}

		if src.Category != "" && !models.HasCategorySlug(src.Category) {
			warnings = append(warnings, fmt.Sprintf("source %q uses unknown category slug %q", src.Name, src.Category))
		}

		switch src.AccessMethod() {
		case AccessMethodRSS:
			if src.EndpointURL() == "" {
				warnings = append(warnings, fmt.Sprintf("rss source %q is missing FeedURL or Access.URL", src.Name))
			}
		case AccessMethodHTMLList:
			cfg := src.Access.HTMLList
			if cfg == nil {
				warnings = append(warnings, fmt.Sprintf("html_list source %q is missing HTMLList config", src.Name))
				continue
			}
			if cfg.ArticleSelector == "" || cfg.TitleLinkSel == "" {
				warnings = append(warnings, fmt.Sprintf("html_list source %q is missing required selectors", src.Name))
			}
		case AccessMethodAPI:
			cfg := src.Access.API
			if cfg == nil {
				warnings = append(warnings, fmt.Sprintf("api source %q is missing API config", src.Name))
				continue
			}
			if cfg.TitleField == "" || cfg.URLField == "" {
				warnings = append(warnings, fmt.Sprintf("api source %q is missing required api fields", src.Name))
			}
		default:
			warnings = append(warnings, fmt.Sprintf("source %q uses unsupported access method %q", src.Name, src.AccessMethod()))
		}

		if src.Auth != nil && !src.RequiresAuth {
			warnings = append(warnings, fmt.Sprintf("source %q defines auth metadata but does not set requires_auth", src.Name))
		}
	}
	return warnings
}

func ValidateSourcesAgainstAuth(sourceNamesRequiringAuth []string, authNames []string) []string {
	authSet := make(map[string]struct{}, len(authNames))
	for _, name := range authNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		authSet[strings.ToLower(trimmed)] = struct{}{}
	}

	var warnings []string
	for _, name := range sourceNamesRequiringAuth {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := authSet[strings.ToLower(trimmed)]; !ok {
			warnings = append(warnings, fmt.Sprintf("source %q requires auth but has no matching auth metadata entry", trimmed))
		}
	}
	return warnings
}

func KnownAuthSourcesFromSources(sources []Source) []config.Credential {
	authSources := make([]config.Credential, 0)
	for _, src := range sources {
		if src.Auth == nil {
			continue
		}
		auth := *src.Auth
		auth.SourceName = src.Name
		authSources = append(authSources, auth)
	}
	return authSources
}

// CrawlAll fetches all configured sources
func (c *Crawler) CrawlAll(verbose bool) (int, error) {
	// Warn about any duplicate names or feed URLs at startup
	if warnings := ValidateSources(); len(warnings) > 0 {
		for _, w := range warnings {
			log.Printf("  ⚠  config warning: %s", w)
		}
	}

	total := 0
	for _, src := range TelecomSources {
		n, err := c.CrawlSource(src, verbose)
		if err != nil {
			log.Printf("  ⚠  %s: %v", src.Name, err)
			continue
		}
		total += n
		if verbose {
			fmt.Printf("  ✓  %-35s  +%d articles\n", src.Name, n)
		}
	}
	return total, nil
}

// CrawlSource fetches a single source and saves new articles
func (c *Crawler) CrawlSource(src Source, verbose bool) (int, error) {
	feed, err := c.fetchFeed(src)
	if err != nil {
		return 0, err
	}
	saved := 0
	for _, item := range feed.Items {
		art := c.itemToArticle(item, src)
		if err := c.db.UpsertArticle(art); err != nil {
			log.Printf("  db error for %s: %v", art.URL, err)
			continue
		}
		saved++
	}
	return saved, nil
}

// fetchFeed dispatches to the configured access method for the source.
func (c *Crawler) fetchFeed(src Source) (*gofeed.Feed, error) {
	switch src.AccessMethod() {
	case AccessMethodRSS:
		return c.fetchRSSFeed(src)
	case AccessMethodHTMLList:
		return c.fetchHTMLListFeed(src)
	case AccessMethodAPI:
		return c.fetchAPIFeed(src)
	default:
		return nil, fmt.Errorf("unsupported access method %q", src.AccessMethod())
	}
}

func (c *Crawler) ProbeSource(src Source) (*ProbeResult, error) {
	switch src.AccessMethod() {
	case AccessMethodRSS:
		return c.probeRSSFeed(src)
	case AccessMethodHTMLList:
		return c.probeHTMLListFeed(src)
	case AccessMethodAPI:
		return c.probeAPIFeed(src)
	default:
		return nil, fmt.Errorf("unsupported access method %q", src.AccessMethod())
	}
}

func (c *Crawler) fetchRSSFeed(src Source) (*gofeed.Feed, error) {
	req, err := http.NewRequest("GET", src.EndpointURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TelecomNewsCLI/1.0)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, text/xml, */*")

	// Inject credentials if we have them for this source
	c.applyCredentials(req, src.Name)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("HTTP %d — authentication required. Run: telecom-news credentials set %q", resp.StatusCode, src.Name)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	feed, err := c.parser.ParseString(string(body))
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	return feed, nil
}

func (c *Crawler) probeRSSFeed(src Source) (*ProbeResult, error) {
	req, err := http.NewRequest("GET", src.EndpointURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TelecomNewsCLI/1.0)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, text/xml, */*")
	c.applyCredentials(req, src.Name)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("HTTP %d — authentication required. Run: telecom-news credentials set %q", resp.StatusCode, src.Name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	feed, err := c.parser.ParseString(string(body))
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	return &ProbeResult{
		Source:    src.Name,
		Method:    src.AccessMethod(),
		Endpoint:  src.EndpointURL(),
		FinalURL:  resp.Request.URL.String(),
		ItemCount: len(feed.Items),
	}, nil
}

func (c *Crawler) fetchHTMLListFeed(src Source) (*gofeed.Feed, error) {
	req, err := http.NewRequest("GET", src.EndpointURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TelecomNewsCLI/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	c.applyCredentials(req, src.Name)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("HTTP %d — authentication required. Run: telecom-news credentials set %q", resp.StatusCode, src.Name)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return parseHTMLListFeed(resp.Body, src)
}

func (c *Crawler) probeHTMLListFeed(src Source) (*ProbeResult, error) {
	req, err := http.NewRequest("GET", src.EndpointURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TelecomNewsCLI/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	c.applyCredentials(req, src.Name)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("HTTP %d — authentication required. Run: telecom-news credentials set %q", resp.StatusCode, src.Name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	feed, err := parseHTMLListFeed(resp.Body, src)
	if err != nil {
		return nil, err
	}
	return &ProbeResult{
		Source:    src.Name,
		Method:    src.AccessMethod(),
		Endpoint:  src.EndpointURL(),
		FinalURL:  resp.Request.URL.String(),
		ItemCount: len(feed.Items),
	}, nil
}

func (c *Crawler) fetchAPIFeed(src Source) (*gofeed.Feed, error) {
	req, err := http.NewRequest("GET", src.EndpointURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TelecomNewsCLI/1.0)")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	c.applyCredentials(req, src.Name)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("HTTP %d — authentication required. Run: telecom-news credentials set %q", resp.StatusCode, src.Name)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return parseAPIFeed(resp.Body, src)
}

func (c *Crawler) probeAPIFeed(src Source) (*ProbeResult, error) {
	req, err := http.NewRequest("GET", src.EndpointURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TelecomNewsCLI/1.0)")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	c.applyCredentials(req, src.Name)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("HTTP %d — authentication required. Run: telecom-news credentials set %q", resp.StatusCode, src.Name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	feed, err := parseAPIFeed(resp.Body, src)
	if err != nil {
		return nil, err
	}
	return &ProbeResult{
		Source:    src.Name,
		Method:    src.AccessMethod(),
		Endpoint:  src.EndpointURL(),
		FinalURL:  resp.Request.URL.String(),
		ItemCount: len(feed.Items),
	}, nil
}

func parseHTMLListFeed(r io.Reader, src Source) (*gofeed.Feed, error) {
	cfg := src.Access.HTMLList
	if cfg == nil {
		return nil, fmt.Errorf("html_list config is missing")
	}
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}

	feed := &gofeed.Feed{
		Title:       src.Name,
		Link:        src.EndpointURL(),
		Description: "HTML listing feed",
	}

	seen := map[string]struct{}{}
	doc.Find(cfg.ArticleSelector).Each(func(_ int, sel *goquery.Selection) {
		titleSel := sel.Find(cfg.TitleLinkSel).First()
		title := strings.TrimSpace(titleSel.Text())
		if title == "" {
			return
		}

		linkSel := titleSel
		href, ok := linkSel.Attr("href")
		if !ok {
			if href, ok = sel.Attr("href"); ok {
				linkSel = sel
			} else {
				linkSel = sel.Find("a[href]").First()
				href, ok = linkSel.Attr("href")
			}
		}
		if !ok {
			return
		}
		href = strings.TrimSpace(href)
		if href == "" {
			return
		}
		if cfg.URLContains != "" && !strings.Contains(href, cfg.URLContains) {
			return
		}

		absURL := absoluteURL(src.EndpointURL(), href)
		if _, exists := seen[absURL]; exists {
			return
		}
		seen[absURL] = struct{}{}

		item := &gofeed.Item{
			Title: title,
			Link:  absURL,
		}
		if cfg.DescriptionSel != "" {
			if desc := strings.TrimSpace(sel.Find(cfg.DescriptionSel).First().Text()); desc != "" {
				item.Description = desc
			}
		}

		if cfg.TimeSelector != "" && cfg.TimeAttr != "" {
			if dateText, ok := sel.Find(cfg.TimeSelector).Attr(cfg.TimeAttr); ok {
				if parsed, err := parseSourceDate(strings.TrimSpace(dateText)); err == nil {
					item.PublishedParsed = &parsed
					item.Published = parsed.Format(time.RFC3339)
				}
			}
		}
		if item.PublishedParsed == nil {
			if parsed, err := parseSourceDate(strings.TrimSpace(sel.Find(cfg.TimeSelector).First().Text())); err == nil {
				item.PublishedParsed = &parsed
				item.Published = parsed.Format(time.RFC3339)
			}
		}
		if item.PublishedParsed == nil {
			if parsed, ok := publishedAtFromURL(absURL); ok {
				item.PublishedParsed = &parsed
				item.Published = parsed.Format(time.RFC3339)
			}
		}

		feed.Items = append(feed.Items, item)
	})

	if len(feed.Items) == 0 {
		return nil, fmt.Errorf("html_list parsing found no article links")
	}

	return feed, nil
}

func publishedAtFromURL(rawURL string) (time.Time, bool) {
	matches := elEconomistaURLDatePattern.FindStringSubmatch(rawURL)
	if len(matches) != 2 {
		return time.Time{}, false
	}
	parsed, err := time.Parse("20060102", matches[1])
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func parseSourceDate(value string) (time.Time, error) {
	if match := inlinePublishedDatePattern.FindString(value); match != "" && match != value {
		value = match
	}
	if match := leadingLongDatePattern.FindStringSubmatch(value); len(match) == 2 {
		value = match[1]
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006:01:02T15:04:05",
		"Monday, January 02, 2006",
		"Mon, 01/02/2006 - 15:04",
		"01/02/2006 - 15:04",
		"02/01/2006 - 15:04",
		"2 January 2006",
		"Jan 2, 2006",
		"Jan 02, 2006",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format: %q", value)
}

func parseAPIFeed(r io.Reader, src Source) (*gofeed.Feed, error) {
	cfg := src.Access.API
	if cfg == nil {
		return nil, fmt.Errorf("api config is missing")
	}

	var payload any
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, err
	}

	items, ok := arrayAtPath(payload, cfg.ItemsPath)
	if !ok {
		return nil, fmt.Errorf("api items path %q did not resolve to an array", cfg.ItemsPath)
	}

	feed := &gofeed.Feed{
		Title:       src.Name,
		Link:        src.EndpointURL(),
		Description: "API feed",
	}

	seen := map[string]struct{}{}
	for _, rawItem := range items {
		title, ok := stringAtPath(rawItem, cfg.TitleField)
		if !ok || strings.TrimSpace(title) == "" {
			continue
		}
		link, ok := stringAtPath(rawItem, cfg.URLField)
		if !ok || strings.TrimSpace(link) == "" {
			continue
		}

		link = strings.TrimSpace(link)
		link = absoluteURL(src.EndpointURL(), link)
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}

		item := &gofeed.Item{
			Title: strings.TrimSpace(title),
			Link:  link,
		}

		if cfg.DescriptionField != "" {
			if description, ok := stringAtPath(rawItem, cfg.DescriptionField); ok {
				item.Description = strings.TrimSpace(description)
			}
		}

		if cfg.TimeField != "" {
			if rawTime, ok := stringAtPath(rawItem, cfg.TimeField); ok {
				if parsed, err := parseAPITime(strings.TrimSpace(rawTime), cfg.TimeFormat); err == nil {
					item.PublishedParsed = &parsed
					item.Published = parsed.Format(time.RFC3339)
				}
			}
		}
		if item.PublishedParsed == nil {
			if parsed, ok := publishedAtFromURL(link); ok {
				item.PublishedParsed = &parsed
				item.Published = parsed.Format(time.RFC3339)
			}
		}

		feed.Items = append(feed.Items, item)
	}

	if len(feed.Items) == 0 {
		return nil, fmt.Errorf("api parsing found no items")
	}

	return feed, nil
}

func parseAPITime(value, format string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time value")
	}
	if format != "" {
		return time.Parse(format, value)
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		if len(value) > 10 {
			return time.UnixMilli(unix), nil
		}
		return time.Unix(unix, 0), nil
	}
	return parseSourceDate(value)
}

func absoluteURL(baseURL, href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	if ref.IsAbs() {
		return ref.String()
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

func valueAtPath(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := obj[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func arrayAtPath(value any, path string) ([]any, bool) {
	resolved, ok := valueAtPath(value, path)
	if !ok {
		return nil, false
	}
	arr, ok := resolved.([]any)
	return arr, ok
}

func stringAtPath(value any, path string) (string, bool) {
	resolved, ok := valueAtPath(value, path)
	if !ok {
		return "", false
	}
	switch v := resolved.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

// applyCredentials injects auth headers/cookies into a request based on stored creds
func (c *Crawler) applyCredentials(req *http.Request, sourceName string) {
	if c.creds == nil {
		return
	}
	if cred := c.creds.Get(sourceName); cred != nil {
		cred.ApplyToRequest(req)
	}
}

// ScrapeURL fetches a single article page and extracts metadata
func (c *Crawler) ScrapeURL(rawURL string, category string) (*models.Article, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TelecomNewsCLI/1.0)")

	// Try to match host to a source and inject credentials
	if c.creds != nil {
		u, _ := url.Parse(rawURL)
		if u != nil {
			for _, src := range TelecomSources {
				su, _ := url.Parse(src.EndpointURL())
				if su != nil && su.Hostname() == u.Hostname() {
					c.applyCredentials(req, src.Name)
					break
				}
			}
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(doc.Find("title").First().Text())
	if og := doc.Find(`meta[property="og:title"]`).AttrOr("content", ""); og != "" {
		title = og
	}

	abstract := doc.Find(`meta[name="description"]`).AttrOr("content", "")
	if og := doc.Find(`meta[property="og:description"]`).AttrOr("content", ""); og != "" {
		abstract = og
	}

	pubDate := time.Now()
	if dateStr := doc.Find(`meta[property="article:published_time"]`).AttrOr("content", ""); dateStr != "" {
		if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
			pubDate = t
		}
	}

	u, _ := url.Parse(rawURL)
	source := ""
	if u != nil {
		source = u.Hostname()
	}

	if category == "" {
		category = models.DetectCategory(title + " " + abstract)
	}

	return &models.Article{
		URL:         rawURL,
		Title:       title,
		Abstract:    abstract,
		PublishedAt: pubDate,
		Category:    category,
		Region:      detectRegion(title + " " + abstract),
		Source:      source,
	}, nil
}

// itemToArticle converts an RSS item to an Article
func (c *Crawler) itemToArticle(item *gofeed.Item, src Source) *models.Article {
	pubAt := time.Now()
	if item.PublishedParsed != nil {
		pubAt = *item.PublishedParsed
	} else if item.UpdatedParsed != nil {
		pubAt = *item.UpdatedParsed
	}

	abstract := stripHTML(item.Description)
	if len(abstract) > 500 {
		abstract = abstract[:500]
	}

	combined := strings.ToLower(item.Title + " " + abstract)

	category := src.Category
	if category == "" {
		category = models.DetectCategory(combined)
	}

	region := src.Region
	if region == "global" {
		if detected := detectRegion(combined); detected != "global" {
			region = detected
		}
	}

	u, _ := url.Parse(item.Link)
	source := src.Name
	if u != nil {
		source = u.Hostname()
	}

	return &models.Article{
		URL:         item.Link,
		Title:       strings.TrimSpace(item.Title),
		Abstract:    strings.TrimSpace(abstract),
		PublishedAt: pubAt,
		Category:    category,
		Region:      region,
		Source:      source,
	}
}

func detectRegion(text string) string {
	text = strings.ToLower(text)
	scores := map[string]int{}
	for region, keywords := range models.RegionKeywords {
		if region == "global" {
			continue
		}
		for _, kw := range keywords {
			if strings.Contains(text, strings.ToLower(kw)) {
				scores[region]++
			}
		}
	}
	best := "global"
	bestScore := 0
	for region, score := range scores {
		if score > bestScore {
			bestScore = score
			best = region
		}
	}
	return best
}

func stripHTML(s string) string {
	inTag := false
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

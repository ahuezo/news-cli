package crawler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"telecom-news-cli/internal/db"
	"telecom-news-cli/internal/models"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

// Source defines an RSS/Atom feed target
type Source struct {
	Name     string
	FeedURL  string
	Region   string
	Category string // override; empty = auto-detect
}

// TelecomSources is the master list of telecom news RSS sources
var TelecomSources = []Source{
	// ── Global trade publications ─────────────────────────────────────────────
	{Name: "Light Reading", FeedURL: "https://www.lightreading.com/rss.xml", Region: "global"},
	{Name: "TelecomTV", FeedURL: "https://www.telecomtv.com/content/rss/", Region: "global"},
	{Name: "RCR Wireless News", FeedURL: "https://www.rcrwireless.com/feed", Region: "global"},
	{Name: "Telecoms.com", FeedURL: "https://telecoms.com/feed/", Region: "global"},
	{Name: "Total Telecom", FeedURL: "https://www.totaltele.com/rss/news", Region: "global"},
	{Name: "Mobile World Live", FeedURL: "https://www.mobileworldlive.com/feed/", Region: "global"},
	{Name: "SDxCentral", FeedURL: "https://www.sdxcentral.com/feed/", Region: "global"},
	{Name: "Telegeography Blog", FeedURL: "https://blog.telegeography.com/rss.xml", Region: "global"},
	{Name: "The Register Networks", FeedURL: "https://www.theregister.com/networks/headlines.atom", Region: "global"},
	{Name: "Developing Telecoms", FeedURL: "https://www.developingtelecoms.com/feed.xml", Region: "global"},
	{Name: "Capacity Media", FeedURL: "https://www.capacitymedia.com/rss.xml", Region: "global"},

	// ── North America (US & Canada) ───────────────────────────────────────────
	{Name: "Fierce Telecom", FeedURL: "https://www.fiercetelecom.com/rss/xml", Region: "north-america"},
	{Name: "Fierce Wireless", FeedURL: "https://www.fiercewireless.com/rss/xml", Region: "north-america"},
	{Name: "CommLaw Monitor", FeedURL: "https://www.commlawmonitor.com/feed/", Region: "north-america", Category: "regulation"},
	{Name: "Telecompetitor", FeedURL: "https://www.telecompetitor.com/feed/", Region: "north-america"},
	{Name: "Multichannel News", FeedURL: "https://www.nexttv.com/rss", Region: "north-america"},
	{Name: "Wireless Week", FeedURL: "https://www.wirelessweek.com/rss.xml", Region: "north-america"},
	{Name: "Broadband Communities", FeedURL: "https://www.bbcmag.com/rss.xml", Region: "north-america", Category: "network-infrastructure"},
	{Name: "NTCA Rural Telecom", FeedURL: "https://www.ntca.org/rss/news", Region: "north-america", Category: "network-infrastructure"},

	// ── Mexico ────────────────────────────────────────────────────────────────
	{Name: "Mexico Business News – Telecom", FeedURL: "https://mexicobusiness.news/tag/telecommunications/rss", Region: "north-america"},
	{Name: "El Economista – Telecomunicaciones", FeedURL: "https://www.eleconomista.com.mx/rss/telecomunicaciones.xml", Region: "north-america"},
	{Name: "Expansion MX – Tecnologia", FeedURL: "https://expansion.mx/rss/tecnologia", Region: "north-america"},
	{Name: "Mediatelecom MX", FeedURL: "https://www.mediatelecom.com.mx/feed/", Region: "north-america"},
	{Name: "El Financiero – Tecnologia", FeedURL: "https://www.elfinanciero.com.mx/arc/outboundfeeds/rss/category/tech/", Region: "north-america"},

	// ── Latin America (regional) ──────────────────────────────────────────────
	{Name: "BNamericas Telecom", FeedURL: "https://www.bnamericas.com/en/rss/telecom", Region: "latam"},
	{Name: "TeleSemana", FeedURL: "https://www.telesemana.com/blog/feed/", Region: "latam"},
	{Name: "Convergencia Latina", FeedURL: "https://www.convergencialatina.com/rss", Region: "latam"},
	{Name: "IT Masters Mag LATAM", FeedURL: "https://www.itmastersmag.com/feed/", Region: "latam"},
	{Name: "Telam Tecnologia Argentina", FeedURL: "https://www.telam.com.ar/rss/tecnologia.xml", Region: "latam"},
	{Name: "Valor Economico Telecom Brazil", FeedURL: "https://valor.globo.com/rss/empresas/tecnologia.xml", Region: "latam"},
	{Name: "Developing Telecoms LATAM", FeedURL: "https://developingtelecoms.com/regions/latin-america-telecommunications?format=feed&type=rss", Region: "latam"},
	{Name: "Capacity Media LATAM", FeedURL: "https://www.capacitymedia.com/region/latin-america?format=feed&type=rss", Region: "latam"},

	// ── Cybersecurity ─────────────────────────────────────────────────────────
	{Name: "Dark Reading", FeedURL: "https://www.darkreading.com/rss.xml", Region: "global", Category: "cybersecurity"},
	{Name: "SecurityWeek", FeedURL: "https://feeds.feedburner.com/securityweek", Region: "global", Category: "cybersecurity"},

	// ── Sustainability ────────────────────────────────────────────────────────
	{Name: "GreenBiz Technology", FeedURL: "https://www.greenbiz.com/taxonomy/term/40/feed", Region: "global", Category: "sustainability"},

	// ── Asia Pacific ──────────────────────────────────────────────────────────
	{Name: "Telecom Asia", FeedURL: "https://www.telecomasia.net/rss.xml", Region: "asia-pacific"},

	// ── MEA ───────────────────────────────────────────────────────────────────
	{Name: "Connecting Africa", FeedURL: "https://www.connectingafrica.com/rss.xml", Region: "mea"},
}

// categoryKeywords maps slugs to detection keywords
var categoryKeywords = map[string][]string{
	"network-infrastructure": {"5g", "fibre", "fiber", "fwa", "ran", "spectrum", "antenna", "tower", "satellite", "leo", "bandwidth", "latency", "4g", "lte", "network build", "rollout", "coverage"},
	"ai-automation":          {"artificial intelligence", " ai ", "machine learning", "automation", "llm", "generative", "genai", "chatbot", "neural", "predictive maintenance", "agentic"},
	"cloud-it":               {"cloud", "bss", "oss", "devops", "microservices", "kubernetes", "saas", "paas", "it modernization", "digital transformation", "software-defined", "nfv", "vnf"},
	"cybersecurity":          {"security", "breach", "hack", "ransomware", "cyber", "threat", "vulnerab", "firewall", "zero trust", "gdpr", "compliance", "data protection", "fraud"},
	"b2b-enterprise":         {"enterprise", "b2b", "managed service", "sd-wan", "mpls", "techco", "wholesale", "corporate", "sme", "business customer"},
	"apis-monetization":      {"api", "open gateway", "monetiz", "camara", "network-as-a-service", "naas", "platform", "developer"},
	"iot":                    {"iot", "internet of things", "connected device", "smart city", "m2m", "industrial", "aep", "sensor", "telematics"},
	"business-models":        {"revenue", "arpu", "merger", "acquisition", "m&a", "partnership", "strategy", "profit", "investor", "earnings", "market share", "growth", "edge computing"},
	"sustainability":         {"sustainability", "esg", "carbon", "green", "renewable", "energy efficiency", "environment", "net zero", "emission"},
	"regulation":             {"regulation", "fcc", "ofcom", "spectrum auction", "policy", "legislation", "antitrust", "net neutrality", "compliance", "law", "eu act", "fine", "penalty"},
	"customer-experience":    {"customer experience", "cx ", "nps", "churn", "satisfaction", "loyalty", "digital channel", "self-service", "personali"},
}

// Crawler handles fetching and storing articles
type Crawler struct {
	db     *db.DB
	client *http.Client
	parser *gofeed.Parser
}

// New creates a Crawler
func New(database *db.DB) *Crawler {
	return &Crawler{
		db: database,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
		parser: gofeed.NewParser(),
	}
}

// CrawlAll fetches all configured sources
func (c *Crawler) CrawlAll(verbose bool) (int, error) {
	total := 0
	for _, src := range TelecomSources {
		n, err := c.CrawlSource(src, verbose)
		if err != nil {
			log.Printf("  ⚠  %s: %v", src.Name, err)
			continue
		}
		total += n
		if verbose {
			fmt.Printf("  ✓  %-30s  +%d articles\n", src.Name, n)
		}
	}
	return total, nil
}

// CrawlSource fetches a single source and saves new articles
func (c *Crawler) CrawlSource(src Source, verbose bool) (int, error) {
	feed, err := c.fetchFeed(src.FeedURL)
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

// fetchFeed fetches and parses an RSS/Atom feed
func (c *Crawler) fetchFeed(feedURL string) (*gofeed.Feed, error) {
	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TelecomNewsCLI/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

// ScrapeURL fetches a single article page and extracts metadata
func (c *Crawler) ScrapeURL(rawURL string, category string) (*models.Article, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TelecomNewsCLI/1.0")

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
		category = detectCategory(title + " " + abstract)
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
		category = detectCategory(combined)
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

// detectCategory scores each category by keyword hits
func detectCategory(text string) string {
	text = strings.ToLower(text)
	bestCat := "business-models"
	bestScore := 0
	for cat, keywords := range categoryKeywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestCat = cat
		}
	}
	return bestCat
}

// detectRegion infers a region from article text
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

// stripHTML removes basic HTML tags
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

# 📡 Telecom News CLI

A Go CLI tool to **crawl, index, and query** telecom industry news across
**11 business categories**, 6 world regions, and publication dates — stored locally in SQLite.

---

## Requirements

- Go 1.22+
- GCC (required for `go-sqlite3` CGo compilation)
  - macOS: `xcode-select --install`
  - Ubuntu/Debian: `sudo apt-get install build-essential`
  - Windows: use WSL2 or [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)

---

## Installation

```bash
# Clone or copy the project
cd telecom-news

# Download dependencies
go mod tidy

# Build the binary
go build -o telecom-news .

# Optional: install globally
go install .
```

---

## Quick Start

```bash
# 1. Crawl all configured RSS sources
./telecom-news fetch

# 2. List latest articles
./telecom-news list

# 3. Search for a topic
./telecom-news search "open RAN"
```

---

## Commands

### `fetch` — Crawl RSS sources

```bash
./telecom-news fetch                       # fetch all sources
./telecom-news fetch "Light Reading"       # fetch one source by name
./telecom-news fetch --verbose             # show per-source article counts
```

---

### `list` — Filter and sort articles

```bash
# By category
./telecom-news list --category network-infrastructure
./telecom-news list --category cybersecurity --limit 10

# By region
./telecom-news list --region europe
./telecom-news list --region asia-pacific --sort-by date --order asc

# By date range
./telecom-news list --from 2025-01-01 --to 2025-06-01

# By country
./telecom-news list --country US

# Combined filters
./telecom-news list --category ai-automation --region global --limit 20

# Sort options: date | category | region | source
./telecom-news list --sort-by source --order asc

# Pagination
./telecom-news list --limit 25 --offset 50
```

---

### `search` — Full-text search

Uses SQLite FTS5. Supports boolean operators and prefix matching.

```bash
./telecom-news search "open RAN"
./telecom-news search "5G AND security"
./telecom-news search "spectrum OR bandwidth"
./telecom-news search "generative AI NOT chatbot"
./telecom-news search "veri*"                    # prefix match
./telecom-news search "\"network slicing\""      # exact phrase
./telecom-news search "5G" --limit 50
```

---

### `add` — Manually add a URL

Scrapes title, abstract, and publish date from any article URL.
Category and region are auto-detected if not provided.

```bash
./telecom-news add https://example.com/some-article
./telecom-news add https://example.com/article --category cybersecurity
./telecom-news add https://example.com/article --region europe --country DE
```

---

### `stats` — Database summary

```bash
./telecom-news stats
```

Example output:
```
📊 Telecom News — DB: ~/.telecom-news.db
   Total articles: 1,423

By Category:
  SLUG                     NAME                                      COUNT
  network-infrastructure   Network Infrastructure & Connectivity     312
  ai-automation            AI & Automation                           287
  cybersecurity            Cybersecurity                             201
  ...

By Region:
  REGION          COUNT
  global          543
  north-america   312
  europe          267
  asia-pacific    198
  latam            63
  mea              40
```

---

### `sources` — List all RSS sources

```bash
./telecom-news sources
```

---

### `categories` — List all category slugs

```bash
./telecom-news categories
```

---

## Categories

| Slug | Name |
|---|---|
| `network-infrastructure` | Network Infrastructure & Connectivity |
| `ai-automation` | AI & Automation |
| `cloud-it` | Cloud & IT Modernization |
| `cybersecurity` | Cybersecurity |
| `b2b-enterprise` | B2B & Enterprise Services |
| `apis-monetization` | APIs & Network Monetization |
| `iot` | Internet of Things |
| `business-models` | Revenue & Business Model Transformation |
| `sustainability` | Sustainability & ESG |
| `regulation` | Regulation & Spectrum Policy |
| `customer-experience` | Customer Experience |

---

## Regions

| Value | Description |
|---|---|
| `global` | Multi-country or unspecified |
| `north-america` | US, Canada, Mexico |
| `europe` | EU + UK |
| `asia-pacific` | APAC |
| `latam` | Latin America |
| `mea` | Middle East & Africa |

---

## Database

Articles are stored in a local SQLite file at `~/.telecom-news.db` by default.

```bash
# Use a custom database path
./telecom-news --db /path/to/mydb.db fetch
./telecom-news --db /path/to/mydb.db list
```

The schema includes:
- A **FTS5 virtual table** for full-text search over title, abstract, category, and source
- Indexes on `category`, `region`, `published_at`, and `source` for fast filtered queries
- `INSERT OR IGNORE` on URL to prevent duplicates across fetches

---

## Adding More Sources

Edit `internal/crawler/crawler.go` and append to `TelecomSources`:

```go
{Name: "My Blog",      FeedURL: "https://example.com/rss",  Region: "europe"},
{Name: "Sec Blog",     FeedURL: "https://sec.example/feed",  Region: "global", Category: "cybersecurity"},
```

---

## Scheduled Crawling (cron)

```bash
# Crawl every 6 hours, log to file
0 */6 * * * /path/to/telecom-news fetch >> /var/log/telecom-news.log 2>&1
```

---

## Project Structure

```
telecom-news/
├── main.go                        # Entry point
├── go.mod                         # Module & dependencies
├── cmd/
│   └── root.go                    # All CLI commands (cobra)
└── internal/
    ├── models/models.go           # Article, Category, ListOptions, region keywords
    ├── db/db.go                   # SQLite layer — schema, upsert, list, search, stats
    └── crawler/crawler.go         # RSS fetcher, HTML scraper, auto-classifier
```

---

## License

MIT

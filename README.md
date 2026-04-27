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
# Copy the project folder, then:
cd telecom-news-cli

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
# 1. Crawl all configured sources
./telecom-news fetch

# 2. List latest articles
./telecom-news list

# 3. Search for a topic
./telecom-news search "open RAN"

# 4. Export articles to CSV
./telecom-news export csv --output telecom-news.csv

# 5. Open an article in your browser
./telecom-news open 3

# 6. Start the HTTP API
./telecom-news serve --addr :8080
```

---

## Commands

### `fetch` — Crawl sources

```bash
./telecom-news fetch                          # fetch all sources
./telecom-news fetch "Light Reading"          # fetch one source by name
./telecom-news fetch --verbose                # show per-source article counts
```

Credentials are loaded automatically for sources that require login.
See the [Credentials](#credentials) section below.

---

### `list` — Filter and sort articles

```bash
# By category
./telecom-news list --category network-infrastructure
./telecom-news list --category cybersecurity --limit 10

# By region
./telecom-news list --region latam
./telecom-news list --region north-america --sort-by date --order asc

# By date range
./telecom-news list --from 2025-01-01 --to 2025-06-01

# By country
./telecom-news list --country MX

# Combined filters
./telecom-news list --category ai-automation --region global --limit 20

# Sort options: date | category | region | source
./telecom-news list --sort-by source --order asc

# Print URLs only (pipe-friendly)
./telecom-news list --region latam --url

# Pagination
./telecom-news list --limit 25 --offset 50
```

---

### `search` — Keyword search

Searches across title, abstract, category, and source using case-insensitive LIKE matching.
Multiple words are treated as AND — all words must appear somewhere in the article.

```bash
./telecom-news search "open RAN"
./telecom-news search "5G security"          # both words must match
./telecom-news search "spectrum mexico"
./telecom-news search "generative AI"
./telecom-news search "5G" --limit 50
./telecom-news search "open RAN" --url       # print URLs only
```

---

### `export csv` — Export articles as CSV

Exports matching articles as CSV with columns:
`id,url,title,abstract,published_at,category,region,country,source,created_at`

```bash
# Export all stored articles to stdout
./telecom-news export csv

# Write all stored articles to a file
./telecom-news export csv --output telecom-news.csv

# Export a date range subset
./telecom-news export csv --from 2025-01-01 --to 2025-03-31 --output q1-news.csv

# Combine the same filters used by `list`
./telecom-news export csv --category network-infrastructure --region latam --output latam-network.csv
```

---

### `open` — Open an article in your browser

```bash
./telecom-news open 3                        # open row #3 from last list/search
./telecom-news open https://example.com/...  # open any URL directly
```

Works on macOS (`open`), Linux (`xdg-open`), and Windows (`rundll32`).
If no browser launcher is found, the URL is printed for copy-paste.

---

### `credentials` — Manage login credentials

Some sources require a subscription or free registration to access their content endpoints.
Credentials are stored in `~/.telecom-news-creds.json` with `0600` permissions (owner read/write only).

#### See which sources need login

```bash
./telecom-news credentials info
```

Output:

```
Sources known to require authentication:

SOURCE                              AUTH TYPE  NOTES
──────                              ─────────  ─────
BNamericas Telecom                  basic      Paid subscription. Register at https://www.bnamericas.com
Telecompaper                        basic      Paid subscription. Register at https://www.telecompaper.com
El Economista – Telecomunicaciones  cookie     Free registration at https://www.eleconomista.com.mx
Expansion MX – Tecnologia           cookie     Free registration at https://expansion.mx
Valor Economico Telecom Brazil      basic      Paid subscription (Globo account)
```

#### Set credentials interactively

```bash
./telecom-news credentials set "BNamericas Telecom"
./telecom-news credentials set "Telecompaper"
./telecom-news credentials set "El Economista – Telecomunicaciones"
```

The command walks you through the right inputs for each auth type:

**For `basic` sources** (BNamericas, Telecompaper, Valor Econômico):
```
Username / Email: you@example.com
Password: ••••••••
```

**For `cookie` sources** (El Economista, Expansión MX):

The prompt asks you to paste the full `Cookie` request header value:

```
How to get the Cookie header from your browser:
  1. Open the site and log in
  2. Press F12 → Network tab → reload the page
  3. Click any request → Request Headers
  4. Copy the full Cookie header value

Paste only the header value, for example:
  session_token=eyJhbGciOiJIUz...; _gid=GA1.2.123456789

Cookie: session_token=eyJhbGciOiJIUz...; _gid=GA1.2.123456789
```

You can also manage individual cookies directly without going through `set`:

```bash
# Add or update one cookie at a time
./telecom-news credentials cookie set "El Economista – Telecomunicaciones" session_token eyJhbGci...
./telecom-news credentials cookie set "El Economista – Telecomunicaciones" _gid GA1.2.12345

# See all stored cookies for a source
./telecom-news credentials cookie list "El Economista – Telecomunicaciones"

# Remove one cookie
./telecom-news credentials cookie remove "El Economista – Telecomunicaciones" _gid
```

To find the `Cookie` header in your browser:
1. Open the site and log in
2. Press `F12` → **Network** tab → reload the page
3. Click any request to the site → **Request Headers**
4. Copy the `Cookie:` header value — it looks like `name1=value1; name2=value2`
5. Paste that value directly into the prompt

#### List configured credentials

```bash
./telecom-news credentials list
```

#### Remove credentials

```bash
./telecom-news credentials remove "BNamericas Telecom"
```

#### Check auth status in sources list

```bash
./telecom-news sources
```

The `AUTH` column shows one of:
- `public` — no login needed
- `⚠ login required` — credentials not yet configured
- `✓ configured` — credentials are set and ready

---

### `sources` — List all configured sources

```bash
./telecom-news sources
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
  latam           198
  europe          167
  asia-pacific     98
  mea              40
```

---

### `list-categories` — Categories present in the database

Lists the distinct category values currently used by stored articles, plus article counts.

```bash
./telecom-news list-categories
```

---

### `add` — Manually add a URL

Scrapes title, abstract, and publish date from any article URL.
Category and region are auto-detected if not provided.

```bash
./telecom-news add https://example.com/some-article
./telecom-news add https://example.com/article --category cybersecurity
./telecom-news add https://example.com/article --region latam --country MX
```

---

### `categories` — List all category slugs

```bash
./telecom-news categories
```

---

## HTTP API

Start the server with:

```bash
./telecom-news serve --addr :8080
```

Recommended private mode for the web UI and API:

```bash
TELECOM_NEWS_WEB_USER=admin TELECOM_NEWS_WEB_PASSWORD='change-me' ./telecom-news serve --addr :8080
```

You can also use flags:

```bash
./telecom-news serve --addr :8080 --web-user admin --web-password 'change-me'
```

This uses HTTP Basic Auth for `/`, `/dashboard`, and `/api/v1/*`. Keep `/healthz` public for uptime checks. For internet-facing deployments, put the server behind HTTPS, a reverse proxy, or a VPN; Basic Auth should not be sent over plain HTTP outside localhost/trusted networks.

Default base URL: `http://localhost:8080/api/v1`

Open the HTML dashboard in a browser:

```text
http://localhost:8080/
http://localhost:8080/dashboard
```

### Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Basic liveness check |
| `POST` | `/fetch` | Crawl all sources or one source |
| `GET` | `/articles` | List stored articles with filters |
| `GET` | `/articles/search` | Search articles by `q` |
| `GET` | `/sources` | List configured sources |
| `POST` | `/sources` | Add or update a source |
| `DELETE` | `/sources/{source}` | Remove one source |
| `GET` | `/stats` | Database totals by category/region |
| `GET` | `/categories` | List category catalog |
| `POST` | `/categories` | Add or update a category |
| `DELETE` | `/categories/{slug}` | Remove one category |
| `GET` | `/validate` | Validate source/category/auth catalogs |
| `GET` | `/export/csv` | Export articles as CSV |
| `GET` | `/credentials` | List saved credentials |
| `POST` | `/credentials` | Save a credential |
| `GET` | `/credentials/{source}` | Read one credential |
| `DELETE` | `/credentials/{source}` | Remove one credential |

Dashboard source/category edits are persisted only when the server is launched with writable override files:

```bash
./telecom-news serve --sources /path/to/sources.json --categories /path/to/categories.json
```

### Examples

```bash
# List recent articles
curl 'http://localhost:8080/api/v1/articles?limit=5&sort-by=date&order=desc'

# Search articles
curl 'http://localhost:8080/api/v1/articles/search?q=open%20ran&limit=10'

# Crawl one source
curl -X POST 'http://localhost:8080/api/v1/fetch?source=Light%20Reading'

# Crawl all sources
curl -X POST 'http://localhost:8080/api/v1/fetch'

# Save credentials
curl -X POST 'http://localhost:8080/api/v1/credentials' \
  -H 'Content-Type: application/json' \
  -d '{"source_name":"BNamericas Telecom","auth_type":"basic","username":"you@example.com","password":"secret"}'
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

## Credentials

### Supported auth types

| Type | How it works | Typical sources |
|---|---|---|
| `basic` | HTTP Basic Auth — username + password sent with each request | BNamericas, Telecompaper, Valor Econômico |
| `cookie` | Session cookie injected into request headers | El Economista MX, Expansión MX |
| `bearer` | `Authorization: Bearer <token>` header | API-key gated feeds |
| `header` | Custom HTTP header + value | Proprietary feeds |

### Credentials file

Credentials are stored at `~/.telecom-news-creds.json` (customizable with `--creds`).
The file is created automatically on first use with `0600` permissions.

```bash
# Use a custom credentials file
./telecom-news --creds /path/to/creds.json credentials set "BNamericas Telecom"
./telecom-news --creds /path/to/creds.json fetch

# Use an alternate source catalog
./telecom-news --sources /path/to/sources.json sources
./telecom-news --sources /path/to/sources.json fetch

# Use an alternate category catalog
./telecom-news --categories /path/to/categories.json categories
./telecom-news --categories /path/to/categories.json validate

# Use an optional local auth override catalog
./telecom-news --auth-override /path/to/auth_sources.override.json credentials info

# Validate a file directly against the shipped JSON Schema
./telecom-news validate-schema sources /path/to/sources.json
./telecom-news validate-schema categories /path/to/categories.json
./telecom-news validate-schema auth /path/to/auth_sources.override.json
```

The file format is plain JSON — you can also edit it directly:

```json
{
  "credentials": [
    {
      "source_name": "BNamericas Telecom",
      "auth_type": "basic",
      "username": "you@example.com",
      "password": "yourpassword"
    },
    {
      "source_name": "El Economista – Telecomunicaciones",
      "auth_type": "cookie",
      "raw_cookie": "session_token=eyJhbGciOiJIUz...; _gid=GA1.2.123456789"
    }
  ]
}
```

> **Security note:** The file is stored in plain text. Do not commit it to version control.
> Add `~/.telecom-news-creds.json` to your `.gitignore` if you keep config in a repo.

---

## Database

Articles are stored in `~/.telecom-news.db` by default.

```bash
# Use a custom database path
./telecom-news --db /path/to/mydb.db fetch
./telecom-news --db /path/to/mydb.db list
```

The schema includes:
- Indexes on `category`, `region`, `published_at`, and `source` for fast filtered queries
- Case-insensitive LIKE-based search across title, abstract, category, and source
- `INSERT OR IGNORE` on URL to prevent duplicates across fetches

---

## Adding More Sources

Edit `internal/crawler/sources.json` and append a new source entry.
Source-level category overrides live there as `"category": "<slug>"`.
Category definitions and classifier keywords live in `internal/models/categories.json`.
You can also copy either file and pass them with `--sources /path/to/sources.json` or `--categories /path/to/categories.json` for local overrides without modifying the embedded defaults.

RSS example:

```json
{ "name": "My Blog", "feed_url": "https://example.com/rss", "region": "latam", "requires_auth": true }
```

HTML listing example:

```json
{
  "name": "Example Section",
  "region": "global",
  "access": {
    "method": "html_list",
    "url": "https://example.com/telecom",
    "html_list": {
      "article_selector": "article.card",
      "title_link_selector": "h2 a",
      "time_selector": "time",
      "time_attr": "datetime",
      "url_contains": "/telecom/"
    }
  }
}
```

If the source requires authentication, add inline auth metadata to the same source entry.
If you need local-only overrides, use `--auth-override` with a separate file such as
`examples/auth_sources.override.example.json`.

```json
{
  "name": "My Blog",
  "feed_url": "https://example.com/rss",
  "region": "latam",
  "requires_auth": true,
  "auth": {
    "auth_type": "basic",
    "notes": "Register at https://example.com. Use your account email and password."
  }
}
```

API example:

```json
{
  "name": "Example API",
  "region": "global",
  "access": {
    "method": "api",
    "url": "https://example.com/api/news",
    "api": {
      "items_path": "data.items",
      "title_field": "headline",
      "url_field": "url",
      "description_field": "summary",
      "time_field": "published_at"
    }
  }
}
```

Validate both catalogs before relying on them:

```bash
./telecom-news validate
./telecom-news --sources /path/to/sources.json --categories /path/to/categories.json --auth-override /path/to/auth_sources.override.json validate
./telecom-news validate-schema sources /path/to/sources.json
./telecom-news validate-schema categories /path/to/categories.json
./telecom-news validate-schema auth /path/to/auth_sources.override.json
```

This also checks that every auth entry matches a source name in the active source catalog.
If a source has `"requires_auth": true`, `validate` also checks that it has matching auth metadata.
JSON Schemas are available at `schemas/sources.schema.json` and `schemas/auth_sources.schema.json`.

---

## Scheduled Crawling (cron)

```bash
# Crawl every 6 hours, log to file
0 */6 * * * /path/to/telecom-news fetch >> /var/log/telecom-news.log 2>&1
```

---

## Project Structure

```
telecom-news-cli/
├── main.go                          # Entry point
├── go.mod                           # Module & dependencies
├── cmd/
│   └── root.go                      # All CLI commands (cobra)
└── internal/
    ├── config/
    │   └── credentials.go           # Credential store — auth types, load/save, auth metadata loader
    ├── models/
    │   └── models.go                # Article, Category, ListOptions, region keywords
    ├── db/
    │   └── db.go                    # SQLite layer — schema, upsert, list, search, stats
    └── crawler/
        ├── crawler.go               # Source loader, rss/html/api fetch dispatch, auth injection, classifier
        └── sources.json             # Source catalog with inline auth and per-source access config
    └── models/
        └── categories.json          # Category catalog and classifier keywords
├── examples/
│   └── auth_sources.override.example.json  # Optional local auth override example
├── schemas/
│   ├── auth_sources.schema.json    # JSON Schema for auth override catalogs
│   ├── sources.schema.json         # JSON Schema for source catalogs
│   └── validate.go                 # Schema validation command support
```

---

## License

MIT

package crawler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"telecom-news-cli/internal/config"
	"telecom-news-cli/internal/db"
)

func TestEmbeddedSourcesConfigValid(t *testing.T) {
	if len(TelecomSources) == 0 {
		t.Fatal("expected embedded sources config to load at least one source")
	}
	if warnings := ValidateSources(); len(warnings) != 0 {
		t.Fatalf("expected embedded sources config to validate cleanly, got: %v", warnings)
	}
}

func TestLoadSourcesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.json")
	data := `[{"name":"Test Source","feed_url":"https://example.com/rss.xml","region":"global","requires_auth":true}]`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sources, err := LoadSourcesFromFile(path)
	if err != nil {
		t.Fatalf("LoadSourcesFromFile() error = %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Name != "Test Source" {
		t.Fatalf("unexpected source name: %q", sources[0].Name)
	}
	if sources[0].EndpointURL() != "https://example.com/rss.xml" {
		t.Fatalf("unexpected endpoint URL: %q", sources[0].EndpointURL())
	}
	if !sources[0].RequiresAuth {
		t.Fatal("expected requires_auth to load from file")
	}
}

func TestValidateSourcesAgainstAuth(t *testing.T) {
	warnings := ValidateSourcesAgainstAuth(
		[]string{"Needs Auth", "Covered Source"},
		[]string{"Covered Source"},
	)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if warnings[0] != `source "Needs Auth" requires auth but has no matching auth metadata entry` {
		t.Fatalf("unexpected warning: %q", warnings[0])
	}
}

func TestKnownAuthSourcesFromSources(t *testing.T) {
	authSources := KnownAuthSourcesFromSources([]Source{
		{
			Name:         "Inline Auth",
			RequiresAuth: true,
			Auth: &config.Credential{
				AuthType: config.AuthBasic,
				Notes:    "inline",
			},
		},
		{Name: "Public Source"},
	})
	if len(authSources) != 1 {
		t.Fatalf("expected 1 auth source, got %d", len(authSources))
	}
	if authSources[0].SourceName != "Inline Auth" {
		t.Fatalf("unexpected source name: %q", authSources[0].SourceName)
	}
	if authSources[0].AuthType != config.AuthBasic {
		t.Fatalf("unexpected auth type: %q", authSources[0].AuthType)
	}
}

func TestParseAPIFeed(t *testing.T) {
	body := `{
	  "data": {
	    "items": [
	      {
	        "headline": "API article",
	        "link": "/story/api-article-20260303-123456.html",
	        "summary": "desc",
	        "published_at": "2026-03-03T13:05:31Z"
	      },
	      {
	        "headline": "API article",
	        "link": "/story/api-article-20260303-123456.html"
	      }
	    ]
	  }
	}`

	feed, err := parseAPIFeed(strings.NewReader(body), Source{
		Name: "API Source",
		Access: AccessConfig{
			Method: AccessMethodAPI,
			URL:    "https://example.com/api/news",
			API: &APIConfig{
				ItemsPath:        "data.items",
				TitleField:       "headline",
				URLField:         "link",
				DescriptionField: "summary",
				TimeField:        "published_at",
			},
		},
	})
	if err != nil {
		t.Fatalf("parseAPIFeed() error = %v", err)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(feed.Items))
	}
	if feed.Items[0].Link != "https://example.com/story/api-article-20260303-123456.html" {
		t.Fatalf("unexpected link: %q", feed.Items[0].Link)
	}
	if feed.Items[0].Description != "desc" {
		t.Fatalf("unexpected description: %q", feed.Items[0].Description)
	}
	if feed.Items[0].PublishedParsed == nil {
		t.Fatal("expected PublishedParsed to be set")
	}
}

func TestCrawlSourceAPIFetchWithCookieAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "session=abc123" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{
					"title": "Authenticated API article",
					"url": "/story/auth-api-20260303-123456.html",
					"description": "protected",
					"published_at": "2026-03-03T13:05:31Z"
				}
			]
		}`))
	}))
	defer server.Close()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	creds := &config.CredentialStore{
		Credentials: []config.Credential{
			{
				SourceName: "Authenticated API Source",
				AuthType:   config.AuthCookie,
				RawCookie:  "session=abc123",
			},
		},
	}

	c := NewWithCreds(database, creds)
	source := Source{
		Name:         "Authenticated API Source",
		Region:       "global",
		RequiresAuth: true,
		Access: AccessConfig{
			Method: AccessMethodAPI,
			URL:    server.URL,
			API: &APIConfig{
				ItemsPath:        "items",
				TitleField:       "title",
				URLField:         "url",
				DescriptionField: "description",
				TimeField:        "published_at",
			},
		},
	}

	saved, err := c.CrawlSource(source, false)
	if err != nil {
		t.Fatalf("CrawlSource() error = %v", err)
	}
	if saved != 1 {
		t.Fatalf("expected 1 saved article, got %d", saved)
	}

	count, err := database.Count()
	if err != nil {
		t.Fatalf("database.Count() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 stored article, got %d", count)
	}
}

func TestCrawlSourceAPIFetchWithBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user@example.com" || pass != "secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{
					"title": "Basic Auth API article",
					"url": "/story/basic-auth-api-20260303-654321.html",
					"description": "protected basic",
					"published_at": "2026-03-03T15:05:31Z"
				}
			]
		}`))
	}))
	defer server.Close()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	creds := &config.CredentialStore{
		Credentials: []config.Credential{
			{
				SourceName: "Basic Auth API Source",
				AuthType:   config.AuthBasic,
				Username:   "user@example.com",
				Password:   "secret",
			},
		},
	}

	c := NewWithCreds(database, creds)
	source := Source{
		Name:         "Basic Auth API Source",
		Region:       "global",
		RequiresAuth: true,
		Access: AccessConfig{
			Method: AccessMethodAPI,
			URL:    server.URL,
			API: &APIConfig{
				ItemsPath:        "items",
				TitleField:       "title",
				URLField:         "url",
				DescriptionField: "description",
				TimeField:        "published_at",
			},
		},
	}

	saved, err := c.CrawlSource(source, false)
	if err != nil {
		t.Fatalf("CrawlSource() error = %v", err)
	}
	if saved != 1 {
		t.Fatalf("expected 1 saved article, got %d", saved)
	}

	count, err := database.Count()
	if err != nil {
		t.Fatalf("database.Count() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 stored article, got %d", count)
	}
}

func TestCrawlSourceAPIFetchWithBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{
					"title": "Bearer Auth API article",
					"url": "/story/bearer-auth-api-20260303-777777.html",
					"description": "protected bearer",
					"published_at": "2026-03-03T16:05:31Z"
				}
			]
		}`))
	}))
	defer server.Close()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	creds := &config.CredentialStore{
		Credentials: []config.Credential{
			{
				SourceName: "Bearer Auth API Source",
				AuthType:   config.AuthToken,
				Token:      "token-123",
			},
		},
	}

	c := NewWithCreds(database, creds)
	source := Source{
		Name:         "Bearer Auth API Source",
		Region:       "global",
		RequiresAuth: true,
		Access: AccessConfig{
			Method: AccessMethodAPI,
			URL:    server.URL,
			API: &APIConfig{
				ItemsPath:        "items",
				TitleField:       "title",
				URLField:         "url",
				DescriptionField: "description",
				TimeField:        "published_at",
			},
		},
	}

	saved, err := c.CrawlSource(source, false)
	if err != nil {
		t.Fatalf("CrawlSource() error = %v", err)
	}
	if saved != 1 {
		t.Fatalf("expected 1 saved article, got %d", saved)
	}
}

func TestCrawlSourceAPIFetchWithHeaderAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "header-secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{
					"title": "Header Auth API article",
					"url": "/story/header-auth-api-20260303-888888.html",
					"description": "protected header",
					"published_at": "2026-03-03T17:05:31Z"
				}
			]
		}`))
	}))
	defer server.Close()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	creds := &config.CredentialStore{
		Credentials: []config.Credential{
			{
				SourceName: "Header Auth API Source",
				AuthType:   config.AuthHeader,
				HeaderName: "X-Api-Key",
				Token:      "header-secret",
			},
		},
	}

	c := NewWithCreds(database, creds)
	source := Source{
		Name:         "Header Auth API Source",
		Region:       "global",
		RequiresAuth: true,
		Access: AccessConfig{
			Method: AccessMethodAPI,
			URL:    server.URL,
			API: &APIConfig{
				ItemsPath:        "items",
				TitleField:       "title",
				URLField:         "url",
				DescriptionField: "description",
				TimeField:        "published_at",
			},
		},
	}

	saved, err := c.CrawlSource(source, false)
	if err != nil {
		t.Fatalf("CrawlSource() error = %v", err)
	}
	if saved != 1 {
		t.Fatalf("expected 1 saved article, got %d", saved)
	}
}

func TestParseHTMLListFeed(t *testing.T) {
	html := `
	<html><body>
	  <article class="c-article">
	    <h2 class="c-article__title">
	      <a href="/tecnologia/nota-prueba-20260303-123456.html">Nota de prueba</a>
	    </h2>
	    <p class="summary">Resumen de prueba</p>
	    <time class="c-article__date" datetime="2026:03:03T13:05:31">03/03/2026 - 13:05</time>
	  </article>
	  <article class="c-article">
	    <h2 class="c-article__title">
	      <a href="/politica/otra-nota-20260303-654321.html">No debe entrar</a>
	    </h2>
	  </article>
	  <article class="c-article">
	    <h2 class="c-article__title">
	      <a href="/tecnologia/nota-prueba-20260303-123456.html">Duplicada</a>
	    </h2>
	  </article>
	</body></html>`

	feed, err := parseHTMLListFeed(strings.NewReader(html), Source{
		Name:   elEconomistaSourceName,
		Region: "north-america",
		Access: AccessConfig{
			Method: AccessMethodHTMLList,
			URL:    "https://www.eleconomista.com.mx/tecnologia",
			HTMLList: &HTMLListConfig{
				ArticleSelector: "article.c-article",
				TitleLinkSel:    "h2.c-article__title a",
				DescriptionSel:  "p.summary",
				TimeSelector:    "time.c-article__date",
				TimeAttr:        "datetime",
				URLContains:     "/tecnologia/",
			},
		},
	})
	if err != nil {
		t.Fatalf("parseHTMLListFeed() error = %v", err)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(feed.Items))
	}
	if feed.Items[0].Title != "Nota de prueba" {
		t.Fatalf("unexpected title: %q", feed.Items[0].Title)
	}
	if feed.Items[0].Link != "https://www.eleconomista.com.mx/tecnologia/nota-prueba-20260303-123456.html" {
		t.Fatalf("unexpected link: %q", feed.Items[0].Link)
	}
	if feed.Items[0].Description != "Resumen de prueba" {
		t.Fatalf("unexpected description: %q", feed.Items[0].Description)
	}
	if feed.Items[0].PublishedParsed == nil {
		t.Fatal("expected PublishedParsed to be set")
	}
	want := time.Date(2026, time.March, 3, 13, 5, 31, 0, time.UTC)
	if !feed.Items[0].PublishedParsed.Equal(want) {
		t.Fatalf("unexpected published time: got %s want %s", feed.Items[0].PublishedParsed, want)
	}
}

func TestParseHTMLListFeedArticleNodeIsLink(t *testing.T) {
	html := `
	<html><body>
	  <a class="card article-preview-card" href="/news/test-story-1">
	    <div class="card-content">
	      <h2 class="is-title">Test story</h2>
	      <p class="is-abstract">Summary text</p>
	    </div>
	  </a>
	</body></html>`

	feed, err := parseHTMLListFeed(strings.NewReader(html), Source{
		Name:   "Telecompaper",
		Region: "global",
		Access: AccessConfig{
			Method: AccessMethodHTMLList,
			URL:    "https://www.telecompaper.com/news/",
			HTMLList: &HTMLListConfig{
				ArticleSelector: "a.card.article-preview-card",
				TitleLinkSel:    "h2.is-title",
				DescriptionSel:  "p.is-abstract",
				URLContains:     "/news/",
			},
		},
	})
	if err != nil {
		t.Fatalf("parseHTMLListFeed() error = %v", err)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(feed.Items))
	}
	if feed.Items[0].Title != "Test story" {
		t.Fatalf("unexpected title: %q", feed.Items[0].Title)
	}
	if feed.Items[0].Link != "https://www.telecompaper.com/news/test-story-1" {
		t.Fatalf("unexpected link: %q", feed.Items[0].Link)
	}
	if feed.Items[0].Description != "Summary text" {
		t.Fatalf("unexpected description: %q", feed.Items[0].Description)
	}
}

func TestPublishedAtFromURL(t *testing.T) {
	got, ok := publishedAtFromURL("https://www.eleconomista.com.mx/tecnologia/nota-prueba-20260303-123456.html")
	if !ok {
		t.Fatal("expected date extraction to succeed")
	}
	want := time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected extracted date: got %s want %s", got, want)
	}
}

func TestParseSourceDateEmbeddedInAuthorLine(t *testing.T) {
	got, err := parseSourceDate("By MBN Staff -  Wed, 10/29/2025 - 08:00")
	if err != nil {
		t.Fatalf("parseSourceDate() error = %v", err)
	}
	want := time.Date(2025, time.October, 29, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected parsed date: got %s want %s", got, want)
	}
}

func TestParseSourceDateMonthDayYear(t *testing.T) {
	got, err := parseSourceDate("Mar 3, 2026")
	if err != nil {
		t.Fatalf("parseSourceDate() error = %v", err)
	}
	want := time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected parsed date: got %s want %s", got, want)
	}
}

func TestParseSourceDateLeadingLongDate(t *testing.T) {
	got, err := parseSourceDate("23 February 2026 - Real ontology gives AI controls, not copies.")
	if err != nil {
		t.Fatalf("parseSourceDate() error = %v", err)
	}
	want := time.Date(2026, time.February, 23, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected parsed date: got %s want %s", got, want)
	}
}

func TestParseSourceDateWeekdayLongMonthDate(t *testing.T) {
	got, err := parseSourceDate("Tuesday, March 03, 2026")
	if err != nil {
		t.Fatalf("parseSourceDate() error = %v", err)
	}
	want := time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected parsed date: got %s want %s", got, want)
	}
}

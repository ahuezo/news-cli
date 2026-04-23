package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"telecom-news-cli/internal/config"
	"telecom-news-cli/internal/db"
	"telecom-news-cli/internal/models"
)

func TestListArticles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	if err := database.UpsertArticle(&models.Article{
		URL:         "https://example.com/a",
		Title:       "Example",
		PublishedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Category:    "network-infrastructure",
		Region:      "latam",
	}); err != nil {
		t.Fatalf("UpsertArticle() error = %v", err)
	}

	credsPath := filepath.Join(t.TempDir(), "creds.json")
	creds, err := config.LoadOrCreate(credsPath)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	app := New(database, creds)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/articles?limit=5", nil)
	rec := httptest.NewRecorder()

	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got == "" || got == "null\n" {
		t.Fatalf("expected response body, got %q", got)
	}
}

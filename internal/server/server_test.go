package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"telecom-news-cli/internal/config"
	"telecom-news-cli/internal/crawler"
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

func TestDashboardServed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	credsPath := filepath.Join(t.TempDir(), "creds.json")
	creds, err := config.LoadOrCreate(credsPath)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	app := New(database, creds)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()

	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, "Telecom News Dashboard") {
		t.Fatalf("expected dashboard HTML, got %q", got)
	}
}

func TestWebAuthProtectsDashboardAndAPI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	credsPath := filepath.Join(t.TempDir(), "creds.json")
	creds, err := config.LoadOrCreate(credsPath)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	app := New(database, creds)
	app.SetWebAuth("admin", "secret")
	router := app.Router()

	for _, path := range []string{"/dashboard", "/api/v1/health"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %s to require auth, got %d", path, rec.Code)
		}

		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.SetBasicAuth("admin", "secret")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %s with auth to succeed, got %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected healthz to remain public, got %d", rec.Code)
	}
}

func TestDeleteArticle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()

	if err := database.UpsertArticle(&models.Article{
		URL:         "https://example.com/delete",
		Title:       "Delete",
		PublishedAt: time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
		Category:    "network-infrastructure",
		Region:      "latam",
	}); err != nil {
		t.Fatalf("UpsertArticle() error = %v", err)
	}
	articles, err := database.List(models.ListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	credsPath := filepath.Join(t.TempDir(), "creds.json")
	creds, err := config.LoadOrCreate(credsPath)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	app := New(database, creds)
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/articles/%d", articles[0].ID), nil)
	rec := httptest.NewRecorder()

	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	count, err := database.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("expected article to be deleted, got count %d", count)
	}
}

func TestSourceMutationPersistsWhenPathConfigured(t *testing.T) {
	originalSources := append([]crawler.Source(nil), crawler.TelecomSources...)
	t.Cleanup(func() {
		crawler.UseSources(originalSources)
	})

	dir := t.TempDir()
	sourcesPath := filepath.Join(dir, "sources.json")
	crawler.UseSources([]crawler.Source{})

	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()
	creds, err := config.LoadOrCreate(filepath.Join(dir, "creds.json"))
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	app := NewWithCatalogPaths(database, creds, sourcesPath, "")
	body := bytes.NewBufferString(`{"name":"Test Feed","endpoint_url":"https://example.com/rss.xml","region":"global","access_method":"rss"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sources", body)
	rec := httptest.NewRecorder()

	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	sources, err := crawler.LoadSourcesFromFile(sourcesPath)
	if err != nil {
		t.Fatalf("LoadSourcesFromFile() error = %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "Test Feed" {
		t.Fatalf("unexpected sources: %+v", sources)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/sources/Test%20Feed", nil)
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected delete status: %d body=%s", rec.Code, rec.Body.String())
	}
	sources, err = crawler.LoadSourcesFromFile(sourcesPath)
	if err != nil {
		t.Fatalf("LoadSourcesFromFile() after delete error = %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected source deletion to persist, got %+v", sources)
	}
}

func TestCategoryMutationPersistsWhenPathConfigured(t *testing.T) {
	originalCatalog := &models.CategoryCatalog{
		DefaultCategory: models.DefaultCategorySlug(),
		Categories:      append([]models.Category(nil), models.TelecomCategories...),
	}
	t.Cleanup(func() {
		models.UseCategoryCatalog(originalCatalog)
	})

	dir := t.TempDir()
	categoriesPath := filepath.Join(dir, "categories.json")
	models.UseCategoryCatalog(&models.CategoryCatalog{
		DefaultCategory: "fallback",
		Categories: []models.Category{
			{ID: 1, Name: "Fallback", Slug: "fallback", Description: "Fallback category"},
		},
	})

	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer database.Close()
	creds, err := config.LoadOrCreate(filepath.Join(dir, "creds.json"))
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}

	app := NewWithCatalogPaths(database, creds, "", categoriesPath)
	body := bytes.NewBufferString(`{"name":"Satellite","slug":"satellite","description":"Satellite services","keywords":["leo"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/categories", body)
	rec := httptest.NewRecorder()

	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	catalog, err := models.LoadCategoryCatalogFromFile(categoriesPath)
	if err != nil {
		t.Fatalf("LoadCategoryCatalogFromFile() error = %v", err)
	}
	if len(catalog.Categories) != 2 || catalog.Categories[1].Slug != "satellite" {
		t.Fatalf("unexpected categories: %+v", catalog.Categories)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/categories/satellite", nil)
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected delete status: %d body=%s", rec.Code, rec.Body.String())
	}
	catalog, err = models.LoadCategoryCatalogFromFile(categoriesPath)
	if err != nil {
		t.Fatalf("LoadCategoryCatalogFromFile() after delete error = %v", err)
	}
	if len(catalog.Categories) != 1 || catalog.Categories[0].Slug != "fallback" {
		t.Fatalf("expected category deletion to persist, got %+v", catalog.Categories)
	}
}

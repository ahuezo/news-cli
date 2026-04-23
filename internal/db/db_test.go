package db

import (
	"path/filepath"
	"testing"
	"time"

	"telecom-news-cli/internal/models"
)

func TestListAllFiltersDateRange(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	articles := []models.Article{
		{
			URL:         "https://example.com/december",
			Title:       "December article",
			PublishedAt: time.Date(2024, 12, 31, 22, 0, 0, 0, time.UTC),
			Category:    "network-infrastructure",
			Region:      "global",
			Source:      "Example",
		},
		{
			URL:         "https://example.com/january",
			Title:       "January article",
			PublishedAt: time.Date(2025, 1, 10, 9, 30, 0, 0, time.UTC),
			Category:    "network-infrastructure",
			Region:      "latam",
			Source:      "Example",
		},
		{
			URL:         "https://example.com/february",
			Title:       "February article",
			PublishedAt: time.Date(2025, 2, 2, 18, 45, 0, 0, time.UTC),
			Category:    "cybersecurity",
			Region:      "north-america",
			Source:      "Example",
		},
	}

	for _, article := range articles {
		article := article
		if err := database.UpsertArticle(&article); err != nil {
			t.Fatalf("UpsertArticle() error = %v", err)
		}
	}

	got, err := database.ListAll(models.ListOptions{
		DateFrom:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		DateTo:    time.Date(2025, 1, 31, 23, 59, 59, 0, time.UTC),
		SortBy:    "date",
		SortOrder: "asc",
	})
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 article, got %d", len(got))
	}
	if got[0].Title != "January article" {
		t.Fatalf("unexpected article title: %q", got[0].Title)
	}
}

func TestDistinctCategories(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	articles := []models.Article{
		{
			URL:         "https://example.com/1",
			Title:       "Article 1",
			PublishedAt: time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC),
			Category:    "cybersecurity",
			Region:      "global",
			Source:      "Example",
		},
		{
			URL:         "https://example.com/2",
			Title:       "Article 2",
			PublishedAt: time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC),
			Category:    "network-infrastructure",
			Region:      "global",
			Source:      "Example",
		},
		{
			URL:         "https://example.com/3",
			Title:       "Article 3",
			PublishedAt: time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC),
			Category:    "cybersecurity",
			Region:      "global",
			Source:      "Example",
		},
	}

	for _, article := range articles {
		article := article
		if err := database.UpsertArticle(&article); err != nil {
			t.Fatalf("UpsertArticle() error = %v", err)
		}
	}

	got, err := database.DistinctCategories()
	if err != nil {
		t.Fatalf("DistinctCategories() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(got))
	}
	if got[0].Category != "cybersecurity" || got[0].Count != 2 {
		t.Fatalf("unexpected first category: %+v", got[0])
	}
	if got[1].Category != "network-infrastructure" || got[1].Count != 1 {
		t.Fatalf("unexpected second category: %+v", got[1])
	}
}

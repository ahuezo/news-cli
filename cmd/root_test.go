package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"telecom-news-cli/internal/models"
)

func TestWriteArticlesCSV(t *testing.T) {
	var buf bytes.Buffer
	articles := []models.Article{
		{
			ID:          7,
			URL:         "https://example.com/article",
			Title:       `Title with "quotes"`,
			Abstract:    "line one,\nline two",
			PublishedAt: time.Date(2025, 3, 18, 15, 4, 5, 0, time.UTC),
			Category:    "network-infrastructure",
			Region:      "latam",
			Country:     "MX",
			Source:      "Example Source",
			CreatedAt:   time.Date(2025, 3, 18, 16, 0, 0, 0, time.UTC),
		},
	}

	if err := writeArticlesCSV(&buf, articles); err != nil {
		t.Fatalf("writeArticlesCSV() error = %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "id,url,title,abstract,published_at,category,region,country,source,created_at\n") {
		t.Fatalf("missing csv header: %q", got)
	}
	if !strings.Contains(got, "\"Title with \"\"quotes\"\"\"") {
		t.Fatalf("expected escaped quoted title, got %q", got)
	}
	if !strings.Contains(got, "\"line one,\nline two\"") {
		t.Fatalf("expected escaped multiline abstract, got %q", got)
	}
	if !strings.Contains(got, "2025-03-18T15:04:05Z") {
		t.Fatalf("expected published timestamp in RFC3339, got %q", got)
	}
}

func TestBuildListOptionsDateRange(t *testing.T) {
	opts, err := buildListOptions("", "", "", "2025-01-01", "2025-01-31", "date", "desc", 0, 0)
	if err != nil {
		t.Fatalf("buildListOptions() error = %v", err)
	}

	wantFrom := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	if !opts.DateFrom.Equal(wantFrom) {
		t.Fatalf("unexpected DateFrom: got %v want %v", opts.DateFrom, wantFrom)
	}
	if !opts.DateTo.Equal(wantTo) {
		t.Fatalf("unexpected DateTo: got %v want %v", opts.DateTo, wantTo)
	}
}

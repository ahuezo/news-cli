package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedCategoryCatalogValid(t *testing.T) {
	catalog, err := LoadEmbeddedCategoryCatalog()
	if err != nil {
		t.Fatalf("LoadEmbeddedCategoryCatalog() error = %v", err)
	}
	if warnings := ValidateCategoryCatalog(catalog); len(warnings) != 0 {
		t.Fatalf("expected embedded category catalog to validate cleanly, got: %v", warnings)
	}
}

func TestLoadCategoryCatalogFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.json")
	data := `{
		"default_category":"custom",
		"categories":[
			{"id":1,"slug":"custom","name":"Custom","description":"Custom category","keywords":["alpha"]}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	catalog, err := LoadCategoryCatalogFromFile(path)
	if err != nil {
		t.Fatalf("LoadCategoryCatalogFromFile() error = %v", err)
	}
	if catalog.DefaultCategory != "custom" {
		t.Fatalf("unexpected default category: %q", catalog.DefaultCategory)
	}
	if len(catalog.Categories) != 1 || catalog.Categories[0].Slug != "custom" {
		t.Fatalf("unexpected categories: %+v", catalog.Categories)
	}
}

func TestDetectCategoryFromConfiguredKeywords(t *testing.T) {
	original := &CategoryCatalog{
		DefaultCategory: DefaultCategorySlug(),
		Categories:      append([]Category(nil), TelecomCategories...),
	}
	t.Cleanup(func() {
		UseCategoryCatalog(original)
	})

	UseCategoryCatalog(&CategoryCatalog{
		DefaultCategory: "fallback",
		Categories: []Category{
			{ID: 1, Slug: "fallback", Name: "Fallback", Description: "fallback"},
			{ID: 2, Slug: "automation", Name: "Automation", Description: "automation", Keywords: []string{"robotics", "orchestration"}},
		},
	})

	if got := DetectCategory("robotics and orchestration in telco"); got != "automation" {
		t.Fatalf("unexpected category: got %q want %q", got, "automation")
	}
	if got := DetectCategory("no keyword match"); got != "fallback" {
		t.Fatalf("unexpected fallback category: got %q want %q", got, "fallback")
	}
}

func TestValidateCategoryCatalogInvalid(t *testing.T) {
	warnings := ValidateCategoryCatalog(&CategoryCatalog{
		DefaultCategory: "missing",
		Categories: []Category{
			{ID: 1, Slug: "dup", Name: "First", Description: "a"},
			{ID: 1, Slug: "dup", Name: "", Description: "b"},
		},
	})
	if len(warnings) < 3 {
		t.Fatalf("expected multiple warnings, got: %v", warnings)
	}
}

package catalogschema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFileSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.json")
	data := `[{"name":"Test","feed_url":"https://example.com/rss.xml","region":"global"}]`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	issues, err := ValidateFile("sources", path)
	if err != nil {
		t.Fatalf("ValidateFile() error = %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no schema issues, got: %v", issues)
	}
}

func TestValidateFileAuthInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data := `[{"source_name":"","auth_type":"weird","notes":""}]`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	issues, err := ValidateFile("auth", path)
	if err != nil {
		t.Fatalf("ValidateFile() error = %v", err)
	}
	if len(issues) == 0 {
		t.Fatal("expected schema issues for invalid auth catalog")
	}
}

func TestValidateFileCategories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.json")
	data := `{
		"default_category":"network-infrastructure",
		"categories":[
			{"id":1,"slug":"network-infrastructure","name":"Network","description":"desc","keywords":["5g"]}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	issues, err := ValidateFile("categories", path)
	if err != nil {
		t.Fatalf("ValidateFile() error = %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no schema issues, got: %v", issues)
	}
}

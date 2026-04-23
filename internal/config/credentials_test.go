package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAuthSourcesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth_sources.json")
	data := `[{"source_name":"Test Auth","auth_type":"basic","notes":"test"}]`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	authSources, err := LoadAuthSourcesFromFile(path)
	if err != nil {
		t.Fatalf("LoadAuthSourcesFromFile() error = %v", err)
	}
	if len(authSources) != 1 {
		t.Fatalf("expected 1 auth source, got %d", len(authSources))
	}
	if authSources[0].SourceName != "Test Auth" {
		t.Fatalf("unexpected auth source name: %q", authSources[0].SourceName)
	}
}

func TestValidateAuthSources(t *testing.T) {
	warnings := ValidateAuthSources([]Credential{
		{SourceName: "Dup", AuthType: AuthBasic, Notes: "ok"},
		{SourceName: "dup", AuthType: AuthType("weird"), Notes: ""},
		{SourceName: "", AuthType: AuthCookie, Notes: "missing name"},
	})
	if len(warnings) != 4 {
		t.Fatalf("expected 4 warnings, got %d: %v", len(warnings), warnings)
	}
}

func TestValidateAuthSourcesAgainstSources(t *testing.T) {
	warnings := ValidateAuthSourcesAgainstSources(
		[]Credential{
			{SourceName: "Known Source", AuthType: AuthBasic, Notes: "ok"},
			{SourceName: "Missing Source", AuthType: AuthCookie, Notes: "ok"},
		},
		[]string{"Known Source", "Another Source"},
	)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if warnings[0] != `auth source "Missing Source" does not match any configured source` {
		t.Fatalf("unexpected warning: %q", warnings[0])
	}
}

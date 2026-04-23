package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// AuthType defines the authentication mechanism for a source
type AuthType string

const (
	AuthNone   AuthType = ""       // no auth needed
	AuthBasic  AuthType = "basic"  // HTTP Basic Auth (username:password)
	AuthCookie AuthType = "cookie" // full Cookie header or named session cookies
	AuthHeader AuthType = "header" // custom HTTP header (e.g. API key)
	AuthToken  AuthType = "bearer" // Authorization: Bearer <token>
)

// Credential holds authentication details for one source
type Credential struct {
	SourceName string            `json:"source_name"` // must match Source.Name exactly
	AuthType   AuthType          `json:"auth_type"`
	Username   string            `json:"username,omitempty"`
	Password   string            `json:"password,omitempty"`
	Token      string            `json:"token,omitempty"`       // bearer token or API key value
	Cookies    map[string]string `json:"cookies,omitempty"`     // legacy/manual: name → value
	RawCookie  string            `json:"raw_cookie,omitempty"`  // preferred: full Cookie header string
	HeaderName string            `json:"header_name,omitempty"` // for AuthHeader type
	Notes      string            `json:"notes,omitempty"`
}

// CookieHeader builds the full Cookie header string. RawCookie takes
// precedence because the interactive flow now stores the full header.
func (c *Credential) CookieHeader() string {
	if c.RawCookie != "" {
		return c.RawCookie
	}
	parts := []string{}
	for name, value := range c.Cookies {
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, "; ")
}

// SetCookie adds or updates a single named cookie
func (c *Credential) SetCookie(name, value string) {
	if c.Cookies == nil {
		c.Cookies = map[string]string{}
	}
	c.Cookies[name] = value
}

// RemoveCookie deletes a named cookie, returns true if it existed
func (c *Credential) RemoveCookie(name string) bool {
	if _, ok := c.Cookies[name]; ok {
		delete(c.Cookies, name)
		return true
	}
	return false
}

// ApplyToRequest injects the credential into an HTTP request
func (c *Credential) ApplyToRequest(req *http.Request) {
	switch c.AuthType {
	case AuthBasic:
		if c.Username != "" {
			req.SetBasicAuth(c.Username, c.Password)
		}
	case AuthToken:
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	case AuthHeader:
		if c.HeaderName != "" && c.Token != "" {
			req.Header.Set(c.HeaderName, c.Token)
		}
	case AuthCookie:
		if header := c.CookieHeader(); header != "" {
			req.Header.Set("Cookie", header)
		}
	}
}

// ─── Store ────────────────────────────────────────────────────────────────────

// CredentialStore holds all credentials and the path to the config file
type CredentialStore struct {
	Credentials []Credential `json:"credentials"`
	path        string
}

// DefaultPath returns ~/.telecom-news-creds.json
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".telecom-news-creds.json")
}

// LoadOrCreate loads the credential store from disk, creating it if missing
func LoadOrCreate(path string) (*CredentialStore, error) {
	store := &CredentialStore{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := store.Save(); err != nil {
			return nil, fmt.Errorf("create creds file: %w", err)
		}
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read creds file: %w", err)
	}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, fmt.Errorf("parse creds file: %w", err)
	}
	store.path = path
	return store, nil
}

// Save writes the store to disk with 0600 permissions
func (s *CredentialStore) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Get returns the credential for a source, or nil if not found
func (s *CredentialStore) Get(sourceName string) *Credential {
	for i := range s.Credentials {
		if strings.EqualFold(s.Credentials[i].SourceName, sourceName) {
			return &s.Credentials[i]
		}
	}
	return nil
}

// Set adds or updates a full credential entry
func (s *CredentialStore) Set(cred Credential) {
	for i := range s.Credentials {
		if strings.EqualFold(s.Credentials[i].SourceName, cred.SourceName) {
			s.Credentials[i] = cred
			return
		}
	}
	s.Credentials = append(s.Credentials, cred)
}

// SetCookie adds or updates a single named cookie for a source,
// creating the credential entry if it doesn't exist yet.
func (s *CredentialStore) SetCookie(sourceName, cookieName, cookieValue string) {
	for i := range s.Credentials {
		if strings.EqualFold(s.Credentials[i].SourceName, sourceName) {
			s.Credentials[i].SetCookie(cookieName, cookieValue)
			return
		}
	}
	cred := Credential{
		SourceName: sourceName,
		AuthType:   AuthCookie,
		Cookies:    map[string]string{cookieName: cookieValue},
	}
	s.Credentials = append(s.Credentials, cred)
}

// RemoveCookie removes a single named cookie for a source
func (s *CredentialStore) RemoveCookie(sourceName, cookieName string) bool {
	for i := range s.Credentials {
		if strings.EqualFold(s.Credentials[i].SourceName, sourceName) {
			return s.Credentials[i].RemoveCookie(cookieName)
		}
	}
	return false
}

// Remove deletes all credentials for a source
func (s *CredentialStore) Remove(sourceName string) bool {
	for i, c := range s.Credentials {
		if strings.EqualFold(c.SourceName, sourceName) {
			s.Credentials = append(s.Credentials[:i], s.Credentials[i+1:]...)
			return true
		}
	}
	return false
}

// Path returns the on-disk path of the store
func (s *CredentialStore) Path() string { return s.path }

// KnownAuthSources documents sources that require credentials, with guidance.
var KnownAuthSources []Credential

func parseAuthSources(data []byte) ([]Credential, error) {
	var authSources []Credential
	if err := json.Unmarshal(data, &authSources); err != nil {
		return nil, err
	}
	return authSources, nil
}

func LoadAuthSourcesFromFile(path string) ([]Credential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseAuthSources(data)
}

func UseKnownAuthSources(authSources []Credential) {
	KnownAuthSources = append([]Credential(nil), authSources...)
}

func FindKnownAuthSource(sourceName string) *Credential {
	for i := range KnownAuthSources {
		if strings.EqualFold(KnownAuthSources[i].SourceName, sourceName) {
			return &KnownAuthSources[i]
		}
	}
	return nil
}

func ValidateAuthSources(authSources []Credential) []string {
	seenNames := map[string]string{}
	var warnings []string

	for _, src := range authSources {
		name := strings.TrimSpace(src.SourceName)
		if name == "" {
			warnings = append(warnings, "auth source entry is missing source_name")
			continue
		}

		key := strings.ToLower(name)
		if prev, ok := seenNames[key]; ok {
			warnings = append(warnings, fmt.Sprintf("duplicate auth source name: %q (conflicts with %q)", name, prev))
		} else {
			seenNames[key] = name
		}

		switch src.AuthType {
		case AuthBasic, AuthCookie, AuthHeader, AuthToken:
		default:
			warnings = append(warnings, fmt.Sprintf("auth source %q uses unsupported auth_type %q", name, src.AuthType))
		}

		if strings.TrimSpace(src.Notes) == "" {
			warnings = append(warnings, fmt.Sprintf("auth source %q is missing notes", name))
		}
	}

	return warnings
}

func ValidateKnownAuthSources() []string {
	return ValidateAuthSources(KnownAuthSources)
}

func ValidateAuthSourcesAgainstSources(authSources []Credential, sourceNames []string) []string {
	knownSources := make(map[string]string, len(sourceNames))
	for _, name := range sourceNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		knownSources[strings.ToLower(trimmed)] = trimmed
	}

	var warnings []string
	for _, src := range authSources {
		name := strings.TrimSpace(src.SourceName)
		if name == "" {
			continue
		}
		if _, ok := knownSources[strings.ToLower(name)]; !ok {
			warnings = append(warnings, fmt.Sprintf("auth source %q does not match any configured source", name))
		}
	}

	return warnings
}

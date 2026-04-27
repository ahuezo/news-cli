package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"telecom-news-cli/internal/config"
	"telecom-news-cli/internal/crawler"
	"telecom-news-cli/internal/db"
	"telecom-news-cli/internal/models"
)

type App struct {
	DB             *db.DB
	Creds          *config.CredentialStore
	Crawler        *crawler.Crawler
	SourcesPath    string
	CategoriesPath string
	WebUser        string
	WebPassword    string
	mu             sync.Mutex
}

func New(database *db.DB, creds *config.CredentialStore) *App {
	return NewWithCatalogPaths(database, creds, "", "")
}

func NewWithCatalogPaths(database *db.DB, creds *config.CredentialStore, sourcesPath, categoriesPath string) *App {
	return &App{
		DB:             database,
		Creds:          creds,
		Crawler:        crawler.NewWithCreds(database, creds),
		SourcesPath:    sourcesPath,
		CategoriesPath: categoriesPath,
	}
}

func (a *App) SetWebAuth(username, password string) {
	a.WebUser = username
	a.WebPassword = password
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", a.health)
	r.Group(func(r chi.Router) {
		r.Use(a.basicAuth)
		r.Get("/", a.dashboard)
		r.Get("/dashboard", a.dashboard)
		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/health", a.health)
			r.Get("/sources", a.sources)
			r.Post("/sources", a.saveSource)
			r.Delete("/sources/{source}", a.deleteSource)
			r.Get("/stats", a.stats)
			r.Get("/categories", a.categories)
			r.Post("/categories", a.saveCategory)
			r.Delete("/categories/{slug}", a.deleteCategory)
			r.Get("/validate", a.validate)
			r.Get("/articles", a.listArticles)
			r.Delete("/articles/{id}", a.deleteArticle)
			r.Get("/articles/search", a.searchArticles)
			r.Get("/export/csv", a.exportCSV)
			r.Post("/fetch", a.fetch)
			r.Get("/credentials", a.listCredentials)
			r.Post("/credentials", a.saveCredential)
			r.Get("/credentials/info", a.credentialInfo)
			r.Get("/credentials/{source}", a.getCredential)
			r.Delete("/credentials/{source}", a.deleteCredential)
		})
	})

	return r
}

func (a *App) basicAuth(next http.Handler) http.Handler {
	if strings.TrimSpace(a.WebUser) == "" && a.WebPassword == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if ok && constantTimeEqual(user, a.WebUser) && constantTimeEqual(pass, a.WebPassword) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="Telecom News Dashboard"`)
		respondError(w, http.StatusUnauthorized, fmt.Errorf("authentication required"))
	})
}

func constantTimeEqual(a, b string) bool {
	aHash := sha256.Sum256([]byte(a))
	bHash := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1
}

func (a *App) health(w http.ResponseWriter, _ *http.Request) {
	total, _ := a.DB.Count()
	a.mu.Lock()
	sourcesCount := len(crawler.TelecomSources)
	categoriesCount := len(models.TelecomCategories)
	defaultCategory := models.DefaultCategorySlug()
	a.mu.Unlock()
	respondJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"articles":         total,
		"credentials":      len(a.Creds.Credentials),
		"sources":          sourcesCount,
		"categories":       categoriesCount,
		"default_category": defaultCategory,
	})
}

func (a *App) sources(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	sources := append([]crawler.Source(nil), crawler.TelecomSources...)
	a.mu.Unlock()

	store := a.Creds
	items := make([]map[string]any, 0, len(sources))
	for _, src := range sources {
		status := "public"
		if src.RequiresAuth {
			status = "login_required"
			if store != nil && store.Get(src.Name) != nil {
				status = "configured"
			}
		} else if config.FindKnownAuthSource(src.Name) != nil {
			status = "auth_metadata_only"
		}
		items = append(items, map[string]any{
			"name":          src.Name,
			"region":        src.Region,
			"category":      src.Category,
			"access_method": src.AccessMethod(),
			"endpoint_url":  src.EndpointURL(),
			"requires_auth": src.RequiresAuth,
			"auth_status":   status,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"sources": items, "count": len(items)})
}

func (a *App) saveSource(w http.ResponseWriter, r *http.Request) {
	var input sourceInput
	if err := decodeJSON(r.Body, &input); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	src, err := sourceFromInput(input)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	sources := append([]crawler.Source(nil), crawler.TelecomSources...)
	replaced := false
	for i := range sources {
		if strings.EqualFold(sources[i].Name, src.Name) {
			sources[i] = src
			replaced = true
			break
		}
	}
	if !replaced {
		sources = append(sources, src)
	}
	crawler.UseSources(sources)

	persisted, err := a.persistSources(sources)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"source":    sourceView(src),
		"created":   !replaced,
		"persisted": persisted,
	})
}

func (a *App) deleteSource(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "source"))
	if name == "" {
		respondError(w, http.StatusBadRequest, fmt.Errorf("source is required"))
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	sources := make([]crawler.Source, 0, len(crawler.TelecomSources))
	removed := false
	for _, src := range crawler.TelecomSources {
		if strings.EqualFold(src.Name, name) {
			removed = true
			continue
		}
		sources = append(sources, src)
	}
	if !removed {
		respondError(w, http.StatusNotFound, fmt.Errorf("source not found"))
		return
	}
	crawler.UseSources(sources)
	if _, err := a.persistSources(sources); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) stats(w http.ResponseWriter, _ *http.Request) {
	total, err := a.DB.Count()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	byCat, byRegion, err := a.DB.Stats()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"total":       total,
		"by_category": byCat,
		"by_region":   byRegion,
	})
}

func (a *App) categories(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	categories := append([]models.Category(nil), models.TelecomCategories...)
	defaultCategory := models.DefaultCategorySlug()
	a.mu.Unlock()

	items := make([]map[string]any, 0, len(categories))
	for _, cat := range categories {
		items = append(items, map[string]any{
			"id":          cat.ID,
			"name":        cat.Name,
			"slug":        cat.Slug,
			"description": cat.Description,
			"keywords":    cat.Keywords,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"default_category": defaultCategory,
		"categories":       items,
	})
}

func (a *App) saveCategory(w http.ResponseWriter, r *http.Request) {
	var input categoryInput
	if err := decodeJSON(r.Body, &input); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	cat, err := categoryFromInput(input)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	catalog := currentCategoryCatalog()
	if cat.ID == 0 {
		cat.ID = nextCategoryID(catalog.Categories)
	}
	replaced := false
	for i := range catalog.Categories {
		if strings.EqualFold(catalog.Categories[i].Slug, cat.Slug) {
			catalog.Categories[i] = cat
			replaced = true
			break
		}
	}
	if !replaced {
		catalog.Categories = append(catalog.Categories, cat)
	}
	models.UseCategoryCatalog(catalog)

	persisted, err := a.persistCategories(catalog)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"category":  cat,
		"created":   !replaced,
		"persisted": persisted,
	})
}

func (a *App) deleteCategory(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		respondError(w, http.StatusBadRequest, fmt.Errorf("category slug is required"))
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	catalog := currentCategoryCatalog()
	if strings.EqualFold(catalog.DefaultCategory, slug) {
		respondError(w, http.StatusBadRequest, fmt.Errorf("cannot delete default category %q", slug))
		return
	}
	categories := make([]models.Category, 0, len(catalog.Categories))
	removed := false
	for _, cat := range catalog.Categories {
		if strings.EqualFold(cat.Slug, slug) {
			removed = true
			continue
		}
		categories = append(categories, cat)
	}
	if !removed {
		respondError(w, http.StatusNotFound, fmt.Errorf("category not found"))
		return
	}
	catalog.Categories = categories
	models.UseCategoryCatalog(catalog)
	if _, err := a.persistCategories(catalog); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) validate(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	sources := append([]crawler.Source(nil), crawler.TelecomSources...)
	categories := append([]models.Category(nil), models.TelecomCategories...)
	defaultCategory := models.DefaultCategorySlug()
	sourceWarnings := crawler.ValidateSources()
	a.mu.Unlock()

	knownAuth := authSourceNames(config.KnownAuthSources)
	authRequired := sourceNamesRequiringAuth(sources)
	respondJSON(w, http.StatusOK, map[string]any{
		"sources":         sourceWarnings,
		"categories":      models.ValidateCategoryCatalog(&models.CategoryCatalog{DefaultCategory: defaultCategory, Categories: categories}),
		"auth_sources":    config.ValidateKnownAuthSources(),
		"auth_links":      crawler.ValidateSourcesAgainstAuth(authRequired, knownAuth),
		"auth_to_sources": config.ValidateAuthSourcesAgainstSources(config.KnownAuthSources, sourceNames(sources)),
	})
}

func (a *App) listArticles(w http.ResponseWriter, r *http.Request) {
	opts, err := listOptionsFromQuery(r.URL.Query())
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	articles, err := a.DB.List(opts)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"count":    len(articles),
		"limit":    opts.Limit,
		"offset":   opts.Offset,
		"articles": articles,
	})
}

func (a *App) searchArticles(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		respondError(w, http.StatusBadRequest, fmt.Errorf("missing q query parameter"))
		return
	}
	limit := 25
	if s := strings.TrimSpace(queryValue(r.URL.Query(), "limit")); s != "" {
		parsed, err := strconv.Atoi(s)
		if err != nil || parsed < 0 {
			respondError(w, http.StatusBadRequest, fmt.Errorf("invalid limit %q", s))
			return
		}
		limit = parsed
	}
	articles, err := a.DB.Search(query, limit)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"query":    query,
		"count":    len(articles),
		"articles": articles,
	})
}

func (a *App) deleteArticle(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimSpace(chi.URLParam(r, "id"))
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid article id %q", rawID))
		return
	}
	removed, err := a.DB.DeleteArticle(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if !removed {
		respondError(w, http.StatusNotFound, fmt.Errorf("article not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) exportCSV(w http.ResponseWriter, r *http.Request) {
	opts, err := listOptionsFromQuery(r.URL.Query())
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	articles, err := a.DB.ListAll(opts)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="articles.csv"`)
	if err := writeArticlesCSV(w, articles); err != nil {
		respondError(w, http.StatusInternalServerError, err)
	}
}

func (a *App) fetch(w http.ResponseWriter, r *http.Request) {
	var req fetchRequest
	if err := decodeJSON(r.Body, &req); err != nil && err != io.EOF {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.Source == "" {
		req.Source = strings.TrimSpace(queryValue(r.URL.Query(), "source", "source_name"))
	}

	var sources []crawler.Source
	a.mu.Lock()
	if req.Source != "" {
		src, ok := findSource(req.Source)
		if !ok {
			a.mu.Unlock()
			respondError(w, http.StatusNotFound, fmt.Errorf("source %q not found", req.Source))
			return
		}
		sources = []crawler.Source{src}
	} else {
		sources = append([]crawler.Source(nil), crawler.TelecomSources...)
	}
	a.mu.Unlock()

	results := make([]fetchResult, 0, len(sources))
	total := 0
	for _, src := range sources {
		saved, err := a.Crawler.CrawlSource(src, req.Verbose)
		item := fetchResult{Source: src.Name, Saved: saved}
		if err != nil {
			item.Error = err.Error()
		} else {
			total += saved
		}
		results = append(results, item)
	}
	respondJSON(w, http.StatusOK, fetchResponse{
		Source:  req.Source,
		Saved:   total,
		Results: results,
	})
}

func (a *App) listCredentials(w http.ResponseWriter, _ *http.Request) {
	items := make([]credentialView, 0, len(a.Creds.Credentials))
	for _, c := range a.Creds.Credentials {
		items = append(items, credentialViewFrom(c))
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"credentials": items,
		"count":       len(items),
		"path":        a.Creds.Path(),
	})
}

func (a *App) getCredential(w http.ResponseWriter, r *http.Request) {
	cred := a.Creds.Get(chi.URLParam(r, "source"))
	if cred == nil {
		respondError(w, http.StatusNotFound, fmt.Errorf("credential not found"))
		return
	}
	respondJSON(w, http.StatusOK, credentialViewFrom(*cred))
}

func (a *App) saveCredential(w http.ResponseWriter, r *http.Request) {
	var input credentialInput
	if err := decodeJSON(r.Body, &input); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if input.SourceName == "" {
		reqSource := strings.TrimSpace(queryValue(r.URL.Query(), "source", "source_name"))
		input.SourceName = reqSource
	}
	if strings.TrimSpace(input.SourceName) == "" {
		respondError(w, http.StatusBadRequest, fmt.Errorf("source_name is required"))
		return
	}
	cred := config.Credential{
		SourceName: strings.TrimSpace(input.SourceName),
		AuthType:   input.AuthType,
		Username:   input.Username,
		Password:   input.Password,
		Token:      input.Token,
		Cookies:    input.Cookies,
		RawCookie:  input.RawCookie,
		HeaderName: input.HeaderName,
		Notes:      input.Notes,
	}
	a.Creds.Set(cred)
	if err := a.Creds.Save(); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, credentialViewFrom(cred))
}

func (a *App) deleteCredential(w http.ResponseWriter, r *http.Request) {
	source := chi.URLParam(r, "source")
	if source == "" {
		respondError(w, http.StatusBadRequest, fmt.Errorf("source is required"))
		return
	}
	if !a.Creds.Remove(source) {
		respondError(w, http.StatusNotFound, fmt.Errorf("credential not found"))
		return
	}
	if err := a.Creds.Save(); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) credentialInfo(w http.ResponseWriter, _ *http.Request) {
	items := make([]credentialView, 0, len(config.KnownAuthSources))
	for _, c := range config.KnownAuthSources {
		items = append(items, credentialViewFrom(c))
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"credentials": items,
		"count":       len(items),
	})
}

type fetchRequest struct {
	Source  string `json:"source_name"`
	Verbose bool   `json:"verbose"`
}

type fetchResult struct {
	Source string `json:"source"`
	Saved  int    `json:"saved"`
	Error  string `json:"error,omitempty"`
}

type fetchResponse struct {
	Source  string        `json:"source,omitempty"`
	Saved   int           `json:"saved"`
	Results []fetchResult `json:"results"`
}

type credentialInput struct {
	SourceName string            `json:"source_name"`
	AuthType   config.AuthType   `json:"auth_type"`
	Username   string            `json:"username,omitempty"`
	Password   string            `json:"password,omitempty"`
	Token      string            `json:"token,omitempty"`
	Cookies    map[string]string `json:"cookies,omitempty"`
	RawCookie  string            `json:"raw_cookie,omitempty"`
	HeaderName string            `json:"header_name,omitempty"`
	Notes      string            `json:"notes,omitempty"`
}

type sourceInput struct {
	Name         string               `json:"name"`
	FeedURL      string               `json:"feed_url,omitempty"`
	EndpointURL  string               `json:"endpoint_url,omitempty"`
	Region       string               `json:"region"`
	Category     string               `json:"category,omitempty"`
	RequiresAuth bool                 `json:"requires_auth,omitempty"`
	AccessMethod crawler.AccessMethod `json:"access_method,omitempty"`
}

type categoryInput struct {
	ID          int      `json:"id,omitempty"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords,omitempty"`
}

type credentialView struct {
	SourceName string            `json:"source_name"`
	AuthType   config.AuthType   `json:"auth_type"`
	Username   string            `json:"username,omitempty"`
	Display    string            `json:"display,omitempty"`
	Cookies    map[string]string `json:"cookies,omitempty"`
	RawCookie  bool              `json:"raw_cookie,omitempty"`
	HeaderName string            `json:"header_name,omitempty"`
	Notes      string            `json:"notes,omitempty"`
}

func sourceFromInput(input sourceInput) (crawler.Source, error) {
	name := strings.TrimSpace(input.Name)
	region := strings.TrimSpace(input.Region)
	category := strings.TrimSpace(input.Category)
	endpoint := strings.TrimSpace(input.EndpointURL)
	feedURL := strings.TrimSpace(input.FeedURL)
	method := input.AccessMethod
	if method == "" {
		method = crawler.AccessMethodRSS
	}
	if endpoint == "" {
		endpoint = feedURL
	}
	if name == "" {
		return crawler.Source{}, fmt.Errorf("name is required")
	}
	if region == "" {
		return crawler.Source{}, fmt.Errorf("region is required")
	}
	if endpoint == "" {
		return crawler.Source{}, fmt.Errorf("feed_url or endpoint_url is required")
	}
	switch method {
	case crawler.AccessMethodRSS:
		return crawler.Source{
			Name:         name,
			FeedURL:      endpoint,
			Region:       region,
			Category:     category,
			RequiresAuth: input.RequiresAuth,
		}, nil
	case crawler.AccessMethodHTMLList, crawler.AccessMethodAPI:
		return crawler.Source{
			Name:         name,
			Region:       region,
			Category:     category,
			RequiresAuth: input.RequiresAuth,
			Access: crawler.AccessConfig{
				Method: method,
				URL:    endpoint,
			},
		}, nil
	default:
		return crawler.Source{}, fmt.Errorf("unsupported access_method %q", method)
	}
}

func categoryFromInput(input categoryInput) (models.Category, error) {
	name := strings.TrimSpace(input.Name)
	slug := strings.TrimSpace(input.Slug)
	description := strings.TrimSpace(input.Description)
	if name == "" {
		return models.Category{}, fmt.Errorf("name is required")
	}
	if slug == "" {
		return models.Category{}, fmt.Errorf("slug is required")
	}
	if description == "" {
		return models.Category{}, fmt.Errorf("description is required")
	}
	keywords := make([]string, 0, len(input.Keywords))
	for _, keyword := range input.Keywords {
		if trimmed := strings.TrimSpace(keyword); trimmed != "" {
			keywords = append(keywords, trimmed)
		}
	}
	return models.Category{
		ID:          input.ID,
		Name:        name,
		Slug:        slug,
		Description: description,
		Keywords:    keywords,
	}, nil
}

func sourceView(src crawler.Source) map[string]any {
	status := "public"
	if src.RequiresAuth {
		status = "login_required"
	}
	return map[string]any{
		"name":          src.Name,
		"region":        src.Region,
		"category":      src.Category,
		"access_method": src.AccessMethod(),
		"endpoint_url":  src.EndpointURL(),
		"requires_auth": src.RequiresAuth,
		"auth_status":   status,
	}
}

func currentCategoryCatalog() *models.CategoryCatalog {
	return &models.CategoryCatalog{
		DefaultCategory: models.DefaultCategorySlug(),
		Categories:      append([]models.Category(nil), models.TelecomCategories...),
	}
}

func nextCategoryID(categories []models.Category) int {
	maxID := 0
	for _, cat := range categories {
		if cat.ID > maxID {
			maxID = cat.ID
		}
	}
	return maxID + 1
}

func (a *App) persistSources(sources []crawler.Source) (bool, error) {
	if strings.TrimSpace(a.SourcesPath) == "" {
		return false, nil
	}
	return true, crawler.SaveSourcesToFile(a.SourcesPath, sources)
}

func (a *App) persistCategories(catalog *models.CategoryCatalog) (bool, error) {
	if strings.TrimSpace(a.CategoriesPath) == "" {
		return false, nil
	}
	return true, models.SaveCategoryCatalogToFile(a.CategoriesPath, catalog)
}

func credentialViewFrom(c config.Credential) credentialView {
	return credentialView{
		SourceName: c.SourceName,
		AuthType:   c.AuthType,
		Username:   c.Username,
		Display:    credentialDisplay(c),
		Cookies:    c.Cookies,
		RawCookie:  c.RawCookie != "",
		HeaderName: c.HeaderName,
		Notes:      c.Notes,
	}
}

func credentialDisplay(c config.Credential) string {
	display := c.Username
	if display == "" && c.Token != "" {
		if len(c.Token) > 12 {
			display = c.Token[:12] + "..."
		} else {
			display = c.Token
		}
	}
	if display == "" && len(c.Cookies) > 0 {
		names := make([]string, 0, len(c.Cookies))
		for k := range c.Cookies {
			names = append(names, k)
		}
		sort.Strings(names)
		display = "cookies: " + strings.Join(names, ", ")
	} else if display == "" && c.RawCookie != "" {
		display = "(raw cookie set)"
	}
	return display
}

func listOptionsFromQuery(values url.Values) (models.ListOptions, error) {
	limit := 25
	if s := strings.TrimSpace(values.Get("limit")); s != "" {
		parsed, err := strconv.Atoi(s)
		if err != nil || parsed < 0 {
			return models.ListOptions{}, fmt.Errorf("invalid limit %q", s)
		}
		limit = parsed
	}
	offset := 0
	if s := strings.TrimSpace(values.Get("offset")); s != "" {
		parsed, err := strconv.Atoi(s)
		if err != nil || parsed < 0 {
			return models.ListOptions{}, fmt.Errorf("invalid offset %q", s)
		}
		offset = parsed
	}
	opts := models.ListOptions{
		Category:  queryValue(values, "category"),
		Region:    queryValue(values, "region"),
		Country:   queryValue(values, "country"),
		SortBy:    defaultString(queryValue(values, "sort_by", "sort-by"), "date"),
		SortOrder: defaultString(queryValue(values, "order"), "desc"),
		Limit:     limit,
		Offset:    offset,
	}
	if dateFrom := strings.TrimSpace(queryValue(values, "from")); dateFrom != "" {
		t, err := parseDate(dateFrom)
		if err != nil {
			return models.ListOptions{}, err
		}
		opts.DateFrom = t
	}
	if dateTo := strings.TrimSpace(queryValue(values, "to")); dateTo != "" {
		t, err := parseDate(dateTo)
		if err != nil {
			return models.ListOptions{}, err
		}
		opts.DateTo = t.Add(24 * time.Hour)
	}
	return opts, nil
}

func parseDate(s string) (time.Time, error) {
	for _, f := range []string{"2006-01-02", "2006/01/02", "01-02-2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format %q, use YYYY-MM-DD", s)
}

func writeArticlesCSV(w io.Writer, articles []models.Article) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "url", "title", "abstract", "published_at", "category", "region", "country", "source", "created_at"}); err != nil {
		return err
	}
	for _, a := range articles {
		if err := cw.Write([]string{
			strconv.FormatInt(a.ID, 10),
			a.URL,
			a.Title,
			a.Abstract,
			formatCSVTime(a.PublishedAt),
			a.Category,
			a.Region,
			a.Country,
			a.Source,
			formatCSVTime(a.CreatedAt),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func formatCSVTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, err error) {
	respondJSON(w, status, map[string]any{"error": err.Error()})
}

func decodeJSON(r io.Reader, dst any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func findSource(name string) (crawler.Source, bool) {
	for _, src := range crawler.TelecomSources {
		if strings.EqualFold(src.Name, strings.TrimSpace(name)) {
			return src, true
		}
	}
	return crawler.Source{}, false
}

func sourceNames(sources []crawler.Source) []string {
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, src.Name)
	}
	return names
}

func sourceNamesRequiringAuth(sources []crawler.Source) []string {
	names := make([]string, 0)
	for _, src := range sources {
		if src.RequiresAuth {
			names = append(names, src.Name)
		}
	}
	return names
}

func authSourceNames(authSources []config.Credential) []string {
	names := make([]string, 0, len(authSources))
	for _, src := range authSources {
		names = append(names, src.SourceName)
	}
	return names
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func queryValue(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

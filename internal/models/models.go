package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Category represents a telecom business category
type Category struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords,omitempty"`
}

type CategoryCatalog struct {
	DefaultCategory string     `json:"default_category"`
	Categories      []Category `json:"categories"`
}

// Article represents a crawled news article
type Article struct {
	ID          int64
	URL         string
	Title       string
	Abstract    string
	PublishedAt time.Time
	Category    string
	Region      string
	Country     string
	Source      string
	CreatedAt   time.Time
}

type CategoryCount struct {
	Category string
	Count    int
}

// ListOptions defines filtering and sorting for article queries
type ListOptions struct {
	Category  string
	Region    string
	Country   string
	DateFrom  time.Time
	DateTo    time.Time
	Query     string
	SortBy    string
	SortOrder string
	Limit     int
	Offset    int
}

//go:embed categories.json
var defaultCategoriesJSON []byte

var TelecomCategories []Category
var defaultCategorySlug string

func init() {
	catalog, err := parseCategoryCatalog(defaultCategoriesJSON)
	if err != nil {
		panic(fmt.Sprintf("load categories config: %v", err))
	}
	UseCategoryCatalog(catalog)
}

func parseCategoryCatalog(data []byte) (*CategoryCatalog, error) {
	var catalog CategoryCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func LoadEmbeddedCategoryCatalog() (*CategoryCatalog, error) {
	return parseCategoryCatalog(defaultCategoriesJSON)
}

func LoadCategoryCatalogFromFile(path string) (*CategoryCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseCategoryCatalog(data)
}

func UseCategoryCatalog(catalog *CategoryCatalog) {
	if catalog == nil {
		TelecomCategories = nil
		defaultCategorySlug = ""
		return
	}
	TelecomCategories = append([]Category(nil), catalog.Categories...)
	defaultCategorySlug = catalog.DefaultCategory
}

func DefaultCategorySlug() string {
	return defaultCategorySlug
}

func ValidateCategoryCatalog(catalog *CategoryCatalog) []string {
	if catalog == nil {
		return []string{"category catalog is missing"}
	}
	var warnings []string
	seenIDs := map[int]string{}
	seenSlugs := map[string]string{}
	for _, cat := range catalog.Categories {
		if cat.ID == 0 {
			warnings = append(warnings, fmt.Sprintf("category %q is missing id", cat.Slug))
		} else if prev, ok := seenIDs[cat.ID]; ok {
			warnings = append(warnings, fmt.Sprintf("duplicate category id %d (%q conflicts with %q)", cat.ID, cat.Slug, prev))
		} else {
			seenIDs[cat.ID] = cat.Slug
		}

		slug := strings.TrimSpace(cat.Slug)
		if slug == "" {
			warnings = append(warnings, fmt.Sprintf("category id %d is missing slug", cat.ID))
		} else if prev, ok := seenSlugs[strings.ToLower(slug)]; ok {
			warnings = append(warnings, fmt.Sprintf("duplicate category slug %q (conflicts with %q)", cat.Slug, prev))
		} else {
			seenSlugs[strings.ToLower(slug)] = cat.Slug
		}

		if strings.TrimSpace(cat.Name) == "" {
			warnings = append(warnings, fmt.Sprintf("category %q is missing name", cat.Slug))
		}
	}

	if strings.TrimSpace(catalog.DefaultCategory) == "" {
		warnings = append(warnings, "category catalog is missing default_category")
	} else if _, ok := seenSlugs[strings.ToLower(strings.TrimSpace(catalog.DefaultCategory))]; !ok {
		warnings = append(warnings, fmt.Sprintf("default_category %q does not match any configured category slug", catalog.DefaultCategory))
	}

	return warnings
}

func ValidateActiveCategories() []string {
	return ValidateCategoryCatalog(&CategoryCatalog{
		DefaultCategory: defaultCategorySlug,
		Categories:      TelecomCategories,
	})
}

func HasCategorySlug(slug string) bool {
	needle := strings.ToLower(strings.TrimSpace(slug))
	for _, cat := range TelecomCategories {
		if strings.ToLower(cat.Slug) == needle {
			return true
		}
	}
	return false
}

func DetectCategory(text string) string {
	text = strings.ToLower(text)
	bestCat := defaultCategorySlug
	bestScore := 0
	for _, cat := range TelecomCategories {
		score := 0
		for _, kw := range cat.Keywords {
			if strings.Contains(text, strings.ToLower(kw)) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestCat = cat.Slug
		}
	}
	if bestCat == "" {
		bestCat = "business-models"
	}
	return bestCat
}

// RegionKeywords maps regions to geo keywords used in classification
var RegionKeywords = map[string][]string{
	"north-america": {
		"united states", "usa", "us telecom", "canada", "north america",
		"fcc", "at&t", "verizon", "t-mobile", "dish network", "comcast", "charter",
		"bell canada", "telus", "rogers",
		// Mexico
		"mexico", "mexican", "telmex", "telcel", "axtel", "megacable", "izzi",
		"ifetel", "ift ", "mediatelecom", "cfg telecom", "cfe telecom",
		"altan redes", "yucatan", "monterrey", "guadalajara", "cdmx",
	},
	"europe": {
		"europe", "eu ", "european", "uk ", "united kingdom", "germany", "france",
		"spain", "italy", "netherlands", "ofcom", "vodafone", "orange",
		"deutsche telekom", "telefonica", "swisscom", "proximus",
	},
	"asia-pacific": {
		"asia", "china", "japan", "south korea", "india", "australia", "singapore",
		"huawei", "samsung", "softbank", "reliance jio", "singtel",
	},
	"latam": {
		"latin america", "latam", "brazil", "brasil", "argentina", "colombia",
		"chile", "peru", "venezuela", "ecuador", "bolivia", "paraguay", "uruguay",
		"costa rica", "panama", "guatemala", "honduras", "el salvador", "nicaragua",
		"caribbean", "caribe",
		// Operators
		"claro", "movistar latam", "tigo", "entel", "oi ", "vivo ", "tim brasil",
		"liberty latin america", "digicel", "antel", "bnamericas",
		// Regulators
		"anatel", "osiptel", "subtel", "enacom", "crc colombia", "conatel",
	},
	"mea": {
		"africa", "middle east", "saudi", "uae", "nigeria", "kenya",
		"south africa", "mtn", "stc", "etisalat", "orange africa",
	},
	"global": {},
}

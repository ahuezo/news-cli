package models

import "time"

// Category represents a telecom business category
type Category struct {
	ID          int
	Name        string
	Slug        string
	Description string
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

// TelecomCategories defines the 11 main telecom business categories
var TelecomCategories = []Category{
	{
		ID: 1, Slug: "network-infrastructure", Name: "Network Infrastructure & Connectivity",
		Description: "5G, fibre, FWA, LEO satellites, RAN, spectrum",
	},
	{
		ID: 2, Slug: "ai-automation", Name: "AI & Automation",
		Description: "GenAI, network AI, automation, RAN intelligence",
	},
	{
		ID: 3, Slug: "cloud-it", Name: "Cloud & IT Modernization",
		Description: "BSS, OSS, cloud-native, network softwarization",
	},
	{
		ID: 4, Slug: "cybersecurity", Name: "Cybersecurity",
		Description: "Network security, managed security, compliance, breaches",
	},
	{
		ID: 5, Slug: "b2b-enterprise", Name: "B2B & Enterprise Services",
		Description: "Managed services, SD-WAN, TechCo, enterprise deals",
	},
	{
		ID: 6, Slug: "apis-monetization", Name: "APIs & Network Monetization",
		Description: "Open Gateway, 5G APIs, network-as-a-service",
	},
	{
		ID: 7, Slug: "iot", Name: "Internet of Things",
		Description: "IoT platforms, connected devices, AEP, industrial IoT",
	},
	{
		ID: 8, Slug: "business-models", Name: "Revenue & Business Model Transformation",
		Description: "ARPU, M&A, partnerships, vertical solutions, edge compute",
	},
	{
		ID: 9, Slug: "sustainability", Name: "Sustainability & ESG",
		Description: "Energy efficiency, carbon footprint, green networks",
	},
	{
		ID: 10, Slug: "regulation", Name: "Regulation & Spectrum Policy",
		Description: "FCC, OFCOM, GDPR, AI Act, spectrum auctions, net neutrality",
	},
	{
		ID: 11, Slug: "customer-experience", Name: "Customer Experience",
		Description: "CX, personalization, churn, NPS, digital channels",
	},
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

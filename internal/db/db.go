package db

import (
	"database/sql"
	"fmt"
	"strings"
	"telecom-news-cli/internal/models"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a SQLite connection
type DB struct {
	conn *sql.DB
	Path string
}

// Open opens (or creates) the SQLite database
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{conn: conn, Path: path}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// Close closes the database
func (d *DB) Close() error { return d.conn.Close() }

// migrate creates tables and indexes (no FTS5 dependency)
func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS articles (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		url          TEXT    NOT NULL UNIQUE,
		title        TEXT    NOT NULL,
		abstract     TEXT,
		published_at DATETIME,
		category     TEXT    NOT NULL,
		region       TEXT    NOT NULL DEFAULT 'global',
		country      TEXT,
		source       TEXT,
		created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_articles_category  ON articles(category);
	CREATE INDEX IF NOT EXISTS idx_articles_region    ON articles(region);
	CREATE INDEX IF NOT EXISTS idx_articles_published ON articles(published_at);
	CREATE INDEX IF NOT EXISTS idx_articles_source    ON articles(source);
	`
	_, err := d.conn.Exec(schema)
	return err
}

// UpsertArticle inserts or silently ignores duplicate URLs
func (d *DB) UpsertArticle(a *models.Article) error {
	_, err := d.conn.Exec(`
		INSERT OR IGNORE INTO articles
			(url, title, abstract, published_at, category, region, country, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.URL, a.Title, a.Abstract, a.PublishedAt, a.Category,
		a.Region, a.Country, a.Source, time.Now(),
	)
	return err
}

// List retrieves articles with flexible filtering and sorting
func (d *DB) List(opts models.ListOptions) ([]models.Article, error) {
	query, args := d.listQuery(opts)

	limit := 50
	if opts.Limit > 0 {
		limit = opts.Limit
	}

	query += "\n\t\tLIMIT ? OFFSET ?"
	args = append(args, limit, opts.Offset)

	return d.scan(query, args...)
}

// ListAll retrieves all matching articles without pagination.
func (d *DB) ListAll(opts models.ListOptions) ([]models.Article, error) {
	query, args := d.listQuery(opts)
	return d.scan(query, args...)
}

func (d *DB) listQuery(opts models.ListOptions) (string, []interface{}) {
	where := []string{"1=1"}
	args := []interface{}{}

	if opts.Category != "" {
		where = append(where, "category = ?")
		args = append(args, opts.Category)
	}
	if opts.Region != "" {
		where = append(where, "region = ?")
		args = append(args, opts.Region)
	}
	if opts.Country != "" {
		where = append(where, "country = ?")
		args = append(args, opts.Country)
	}
	if !opts.DateFrom.IsZero() {
		where = append(where, "published_at >= ?")
		args = append(args, opts.DateFrom)
	}
	if !opts.DateTo.IsZero() {
		where = append(where, "published_at <= ?")
		args = append(args, opts.DateTo)
	}

	sortCol := "published_at"
	allowed := map[string]string{
		"date": "published_at", "category": "category",
		"region": "region", "source": "source",
	}
	if col, ok := allowed[opts.SortBy]; ok {
		sortCol = col
	}

	sortOrder := "DESC"
	if strings.ToLower(opts.SortOrder) == "asc" {
		sortOrder = "ASC"
	}

	query := fmt.Sprintf(`
		SELECT id, url, title, abstract, published_at, category, region, country, source, created_at
		FROM articles
		WHERE %s
		ORDER BY %s %s`,
		strings.Join(where, " AND "), sortCol, sortOrder,
	)
	return query, args
}

// Search performs a multi-term LIKE search across title, abstract, category, and source.
// Multiple words are AND-ed: all terms must appear somewhere in the row.
func (d *DB) Search(query string, limit int) ([]models.Article, error) {
	if limit <= 0 {
		limit = 50
	}

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, fmt.Errorf("empty search query")
	}

	// Each term must match in at least one of the text columns
	where := []string{}
	args := []interface{}{}
	for _, term := range terms {
		like := "%" + term + "%"
		where = append(where,
			"(LOWER(title) LIKE ? OR LOWER(abstract) LIKE ? OR LOWER(category) LIKE ? OR LOWER(source) LIKE ?)",
		)
		args = append(args, like, like, like, like)
	}

	q := fmt.Sprintf(`
		SELECT id, url, title, abstract, published_at, category, region, country, source, created_at
		FROM articles
		WHERE %s
		ORDER BY published_at DESC
		LIMIT ?`,
		strings.Join(where, " AND "),
	)
	args = append(args, limit)

	return d.scan(q, args...)
}

// Stats returns article counts per category and per region
func (d *DB) Stats() (map[string]int, map[string]int, error) {
	byCat := map[string]int{}
	byRegion := map[string]int{}

	rows, err := d.conn.Query(`SELECT category, COUNT(*) FROM articles GROUP BY category`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var cnt int
		rows.Scan(&cat, &cnt)
		byCat[cat] = cnt
	}

	rows2, err := d.conn.Query(`SELECT region, COUNT(*) FROM articles GROUP BY region`)
	if err != nil {
		return nil, nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var reg string
		var cnt int
		rows2.Scan(&reg, &cnt)
		byRegion[reg] = cnt
	}

	return byCat, byRegion, nil
}

// DistinctCategories returns the categories present in stored articles with counts.
func (d *DB) DistinctCategories() ([]models.CategoryCount, error) {
	rows, err := d.conn.Query(`
		SELECT category, COUNT(*)
		FROM articles
		GROUP BY category
		ORDER BY category ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []models.CategoryCount
	for rows.Next() {
		var item models.CategoryCount
		if err := rows.Scan(&item.Category, &item.Count); err != nil {
			return nil, err
		}
		categories = append(categories, item)
	}

	return categories, rows.Err()
}

// Count returns total number of stored articles
func (d *DB) Count() (int, error) {
	var n int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&n)
	return n, err
}

// scan maps query rows to an Article slice
func (d *DB) scan(query string, args ...interface{}) ([]models.Article, error) {
	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		var a models.Article
		var pubAt sql.NullTime
		var abstract, country, source sql.NullString
		if err := rows.Scan(
			&a.ID, &a.URL, &a.Title, &abstract,
			&pubAt, &a.Category, &a.Region, &country, &source, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		a.Abstract = abstract.String
		a.Country = country.String
		a.Source = source.String
		if pubAt.Valid {
			a.PublishedAt = pubAt.Time
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}

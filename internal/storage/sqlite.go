package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/M3264/blog-api-go/internal/blog"
	_ "github.com/mattn/go-sqlite3"
)

var ErrNotFound = errors.New("post not found")

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct{ sql *sql.DB }

func Open(ctx context.Context, path string) (*DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: absolute}
	u.RawQuery = "_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(absolute, 0600); err != nil {
		db.Close()
		return nil, err
	}
	s := &DB{sql: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *DB) Close() error                   { return s.sql.Close() }
func (s *DB) Ping(ctx context.Context) error { return s.sql.PingContext(ctx) }

func (s *DB) migrate(ctx context.Context) error {
	if _, err := s.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		var applied int
		err := s.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, file.Name()).Scan(&applied)
		if err != nil {
			return err
		}
		if applied != 0 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		tx, err := s.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES (?, ?)`, file.Name(), time.Now().UTC().Format(time.RFC3339Nano))
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", file.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const postColumns = `slug,title,summary,body,category,author,cover_image,tags_json,featured,status,created_at,updated_at,published_at`

type scanner interface{ Scan(...any) error }

func scanPost(row scanner) (blog.Post, error) {
	var p blog.Post
	var tags, created, updated string
	var published sql.NullString
	var featured int
	err := row.Scan(&p.Slug, &p.Title, &p.Summary, &p.Body, &p.Category, &p.Author, &p.CoverImage, &tags, &featured, &p.Status, &created, &updated, &published)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(tags), &p.Tags); err != nil {
		return p, err
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	p.Featured = featured != 0
	if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return p, err
	}
	if p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return p, err
	}
	if published.Valid {
		t, err := time.Parse(time.RFC3339Nano, published.String)
		if err != nil {
			return p, err
		}
		p.PublishedAt = &t
	}
	return p, nil
}

func get(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, slug string, public bool) (blog.Post, error) {
	query := `SELECT ` + postColumns + ` FROM posts WHERE slug = ?`
	if public {
		query += ` AND status = 'published'`
	}
	p, err := scanPost(q.QueryRowContext(ctx, query, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *DB) Get(ctx context.Context, slug string, public bool) (blog.Post, error) {
	return get(ctx, s.sql, slug, public)
}

func insert(ctx context.Context, tx *sql.Tx, p blog.Post) (sql.Result, error) {
	tags, err := json.Marshal(p.Tags)
	if err != nil {
		return nil, err
	}
	var published any
	if p.PublishedAt != nil {
		published = p.PublishedAt.Format(time.RFC3339Nano)
	}
	return tx.ExecContext(ctx, `INSERT INTO posts (`+postColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.Slug, p.Title, p.Summary, p.Body, p.Category, p.Author, p.CoverImage, string(tags), p.Featured, p.Status,
		p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano), published)
}

func (s *DB) Create(ctx context.Context, p blog.Post) (blog.Post, error) {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	base := blog.Slugify(p.Title)
	for suffix := 1; ; suffix++ {
		p.Slug = base
		if suffix > 1 {
			p.Slug = fmt.Sprintf("%s-%d", base, suffix)
		}
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE slug = ?`, p.Slug).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return p, err
		}
	}
	if _, err := insert(ctx, tx, p); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func (s *DB) Update(ctx context.Context, slug string, input blog.Input, now time.Time) (blog.Post, error) {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return blog.Post{}, err
	}
	defer tx.Rollback()
	p, err := get(ctx, tx, slug, false)
	if err != nil {
		return p, err
	}
	if err := input.Apply(&p, now); err != nil {
		return p, err
	}
	tags, err := json.Marshal(p.Tags)
	if err != nil {
		return p, err
	}
	var published any
	if p.PublishedAt != nil {
		published = p.PublishedAt.Format(time.RFC3339Nano)
	}
	_, err = tx.ExecContext(ctx, `UPDATE posts SET title=?,summary=?,body=?,category=?,author=?,cover_image=?,tags_json=?,featured=?,status=?,updated_at=?,published_at=? WHERE slug=?`,
		p.Title, p.Summary, p.Body, p.Category, p.Author, p.CoverImage, string(tags), p.Featured, p.Status, p.UpdatedAt.Format(time.RFC3339Nano), published, p.Slug)
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func (s *DB) Delete(ctx context.Context, slug string) error {
	result, err := s.sql.ExecContext(ctx, `DELETE FROM posts WHERE slug = ?`, slug)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

type Filter struct {
	Public   bool
	Status   string
	Q        string
	Category string
	Tag      string
	Featured *bool
	Limit    int
	Offset   int
}

func (s *DB) List(ctx context.Context, f Filter) ([]blog.Post, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Public {
		where = append(where, `status='published'`)
	} else if f.Status != "" {
		where = append(where, `status=?`)
		args = append(args, f.Status)
	}
	if f.Category != "" {
		where = append(where, `lower(category)=lower(?)`)
		args = append(args, f.Category)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM json_each(posts.tags_json) WHERE lower(value)=lower(?))`)
		args = append(args, f.Tag)
	}
	if f.Featured != nil {
		where = append(where, `featured=?`)
		args = append(args, *f.Featured)
	}
	if f.Q != "" {
		where = append(where, `(lower(title) LIKE ? ESCAPE '\' OR lower(summary) LIKE ? ESCAPE '\' OR lower(body) LIKE ? ESCAPE '\')`)
		q := "%" + escapeLike(strings.ToLower(f.Q)) + "%"
		args = append(args, q, q, q)
	}
	condition := strings.Join(where, " AND ")
	var total int
	if err := s.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE `+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := `COALESCE(published_at, created_at) DESC, slug ASC`
	if !f.Public {
		order = `updated_at DESC, slug ASC`
	}
	pageArgs := append(append([]any{}, args...), f.Limit, f.Offset)
	rows, err := s.sql.QueryContext(ctx, `SELECT `+postColumns+` FROM posts WHERE `+condition+` ORDER BY `+order+` LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	posts := []blog.Post{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		posts = append(posts, p)
	}
	return posts, total, rows.Err()
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	return strings.ReplaceAll(s, `_`, `\_`)
}

type TermCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (s *DB) Terms(ctx context.Context, tags bool) ([]TermCount, error) {
	query := `SELECT MIN(category), COUNT(*) FROM posts WHERE status='published' GROUP BY lower(category) ORDER BY lower(category)`
	if tags {
		query = `SELECT MIN(j.value), COUNT(*) FROM posts p, json_each(p.tags_json) j WHERE p.status='published' GROUP BY lower(j.value) ORDER BY lower(j.value)`
	}
	rows, err := s.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TermCount{}
	for rows.Next() {
		var item TermCount
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DB) Related(ctx context.Context, slug string, limit, offset int) ([]blog.Post, int, error) {
	source, err := s.Get(ctx, slug, true)
	if err != nil {
		return nil, 0, err
	}
	tags, err := json.Marshal(source.Tags)
	if err != nil {
		return nil, 0, err
	}
	cte := `WITH ranked AS (
		SELECT ` + postColumns + `,
		(CASE WHEN lower(category)=lower(?) THEN 2 ELSE 0 END +
		 (SELECT COUNT(*) FROM json_each(posts.tags_json) t JOIN json_each(?) src ON lower(t.value)=lower(src.value))) AS score
		FROM posts WHERE status='published' AND slug<>?
	)`
	args := []any{source.Category, string(tags), slug}
	var total int
	if err := s.sql.QueryRowContext(ctx, cte+` SELECT COUNT(*) FROM ranked WHERE score>0`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.sql.QueryContext(ctx, cte+` SELECT `+postColumns+` FROM ranked WHERE score>0 ORDER BY score DESC, COALESCE(published_at, created_at) DESC, slug ASC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	posts := []blog.Post{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		posts = append(posts, p)
	}
	return posts, total, rows.Err()
}

func (s *DB) ImportJSON(ctx context.Context, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var posts []blog.Post
	if err := json.Unmarshal(data, &posts); err != nil {
		return 0, err
	}
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	inserted := 0
	for _, p := range posts {
		if p.Tags == nil {
			p.Tags = []string{}
		}
		if p.Slug == "" {
			return 0, errors.New("JSON post has empty slug")
		}
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE slug=?`, p.Slug).Scan(&exists)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if _, err := insert(ctx, tx, p); err != nil {
			return 0, err
		}
		inserted++
	}
	return inserted, tx.Commit()
}

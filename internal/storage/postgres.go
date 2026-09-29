package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/M3264/blog-api-go/internal/blog"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrNotFound = errors.New("post not found")

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct{ sql *sql.DB }

func Open(ctx context.Context, databaseURL string) (*DB, error) {
	if databaseURL == "" {
		return nil, errors.New("BLOG_DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
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
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(32640001)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
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
		var applied bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, file.Name()).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		for _, statement := range strings.Split(string(body), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migration %s: %w", file.Name(), err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name,applied_at) VALUES ($1,$2)`, file.Name(), time.Now().UTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const postColumns = `slug,title,summary,body,category,author,cover_image,tags_json::text,featured,status,created_at,updated_at,published_at`

type scanner interface{ Scan(...any) error }

func scanPost(row scanner) (blog.Post, error) {
	var p blog.Post
	var tags string
	var published sql.NullTime
	err := row.Scan(&p.Slug, &p.Title, &p.Summary, &p.Body, &p.Category, &p.Author, &p.CoverImage, &tags, &p.Featured, &p.Status, &p.CreatedAt, &p.UpdatedAt, &published)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(tags), &p.Tags); err != nil {
		return p, err
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if published.Valid {
		t := published.Time.UTC()
		p.PublishedAt = &t
	}
	return p, nil
}

func get(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, slug string, public, lock bool) (blog.Post, error) {
	query := `SELECT ` + postColumns + ` FROM posts WHERE slug=$1`
	if public {
		query += ` AND status='published'`
	}
	if lock {
		query += ` FOR UPDATE`
	}
	p, err := scanPost(q.QueryRowContext(ctx, query, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *DB) Get(ctx context.Context, slug string, public bool) (blog.Post, error) {
	return get(ctx, s.sql, slug, public, false)
}

func insert(ctx context.Context, tx *sql.Tx, p blog.Post) (sql.Result, error) {
	tags, err := json.Marshal(p.Tags)
	if err != nil {
		return nil, err
	}
	return tx.ExecContext(ctx, `INSERT INTO posts (slug,title,summary,body,category,author,cover_image,tags_json,featured,status,created_at,updated_at,published_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13) ON CONFLICT (slug) DO NOTHING`,
		p.Slug, p.Title, p.Summary, p.Body, p.Category, p.Author, p.CoverImage, string(tags), p.Featured, p.Status, p.CreatedAt, p.UpdatedAt, p.PublishedAt)
}

func (s *DB) Create(ctx context.Context, p blog.Post) (blog.Post, error) {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	base := blog.Slugify(p.Title)
	for suffix := 1; suffix <= 10000; suffix++ {
		p.Slug = base
		if suffix > 1 {
			p.Slug = fmt.Sprintf("%s-%d", base, suffix)
		}
		result, err := insert(ctx, tx, p)
		if err != nil {
			return p, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return p, err
		}
		if count == 1 {
			return p, tx.Commit()
		}
	}
	return p, errors.New("could not allocate unique slug")
}

func (s *DB) Update(ctx context.Context, slug string, input blog.Input, now time.Time) (blog.Post, error) {
	tx, err := s.sql.BeginTx(ctx, nil)
	if err != nil {
		return blog.Post{}, err
	}
	defer tx.Rollback()
	p, err := get(ctx, tx, slug, false, true)
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
	_, err = tx.ExecContext(ctx, `UPDATE posts SET title=$1,summary=$2,body=$3,category=$4,author=$5,cover_image=$6,tags_json=$7::jsonb,featured=$8,status=$9,updated_at=$10,published_at=$11 WHERE slug=$12`,
		p.Title, p.Summary, p.Body, p.Category, p.Author, p.CoverImage, string(tags), p.Featured, p.Status, p.UpdatedAt, p.PublishedAt, p.Slug)
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func (s *DB) Delete(ctx context.Context, slug string) error {
	result, err := s.sql.ExecContext(ctx, `DELETE FROM posts WHERE slug=$1`, slug)
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
	where := []string{"TRUE"}
	args := []any{}
	add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if f.Public {
		where = append(where, `status='published'`)
	} else if f.Status != "" {
		where = append(where, `status=`+add(f.Status))
	}
	if f.Category != "" {
		where = append(where, `lower(category)=lower(`+add(f.Category)+`)`)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM jsonb_array_elements_text(posts.tags_json) AS tag(value) WHERE lower(tag.value)=lower(`+add(f.Tag)+`))`)
	}
	if f.Featured != nil {
		where = append(where, `featured=`+add(*f.Featured))
	}
	if f.Q != "" {
		param := add("%" + escapeLike(strings.ToLower(f.Q)) + "%")
		where = append(where, `(lower(title) LIKE `+param+` ESCAPE '\' OR lower(summary) LIKE `+param+` ESCAPE '\' OR lower(body) LIKE `+param+` ESCAPE '\')`)
	}
	condition := strings.Join(where, " AND ")
	var total int
	if err := s.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE `+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := `COALESCE(published_at,created_at) DESC,slug ASC`
	if !f.Public {
		order = `updated_at DESC,slug ASC`
	}
	limit := add(f.Limit)
	offset := add(f.Offset)
	rows, err := s.sql.QueryContext(ctx, `SELECT `+postColumns+` FROM posts WHERE `+condition+` ORDER BY `+order+` LIMIT `+limit+` OFFSET `+offset, args...)
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
	query := `SELECT MIN(category),COUNT(*) FROM posts WHERE status='published' GROUP BY lower(category) ORDER BY lower(category)`
	if tags {
		query = `SELECT MIN(tag.value),COUNT(*) FROM posts p CROSS JOIN LATERAL jsonb_array_elements_text(p.tags_json) AS tag(value) WHERE p.status='published' GROUP BY lower(tag.value) ORDER BY lower(tag.value)`
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
	cte := `WITH ranked AS (SELECT posts.*,
		(CASE WHEN lower(category)=lower($1) THEN 2 ELSE 0 END +
		 (SELECT COUNT(*) FROM jsonb_array_elements_text(posts.tags_json) t(value)
		  JOIN jsonb_array_elements_text($2::jsonb) src(value) ON lower(t.value)=lower(src.value))) AS score
		 FROM posts WHERE status='published' AND slug<>$3)`
	args := []any{source.Category, string(tags), slug}
	var total int
	if err := s.sql.QueryRowContext(ctx, cte+` SELECT COUNT(*) FROM ranked WHERE score>0`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.sql.QueryContext(ctx, cte+` SELECT `+postColumns+` FROM ranked WHERE score>0 ORDER BY score DESC,COALESCE(published_at,created_at) DESC,slug ASC LIMIT $4 OFFSET $5`, append(args, limit, offset)...)
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

func (s *DB) ImportJSON(ctx context.Context, data []byte) (int, error) {
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
		result, err := insert(ctx, tx, p)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		inserted += int(count)
	}
	return inserted, tx.Commit()
}

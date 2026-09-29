//go:build ignore

// Run only against a disposable database restored from the prelaunch backup.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/M3264/blog-api-go/internal/storage"
	"os"
	"strings"
)

const fingerprint = `SELECT count(*),md5(COALESCE(string_agg(row(slug,title,summary,body,category,author,cover_image,tags_json,featured,status,created_at,updated_at,published_at)::text,'' ORDER BY slug),'')) FROM posts`

func main() {
	ctx := context.Background()
	url := os.Getenv("RESTORE_DATABASE_URL")
	db, e := sql.Open("pgx", url)
	if e != nil {
		panic(e)
	}
	defer db.Close()
	var name string
	if e = db.QueryRow(`SELECT current_database()`).Scan(&name); e != nil {
		panic(e)
	}
	if !strings.HasPrefix(name, "offscript_restore_") {
		panic("refusing to migrate a non-disposable restore database")
	}
	var before, after string
	var n, m int
	if e = db.QueryRow(fingerprint).Scan(&n, &before); e != nil {
		panic(e)
	}
	migrated, e := storage.Open(ctx, url)
	if e != nil {
		panic(e)
	}
	defer migrated.Close()
	if e = migrated.SQL().QueryRow(fingerprint).Scan(&m, &after); e != nil {
		panic(e)
	}
	if n != m || before != after {
		panic("migration changed original article data")
	}
	var authors int
	migrated.SQL().QueryRow(`SELECT count(*) FROM authors`).Scan(&authors)
	fmt.Printf("Restored backup and additive migration verified: %d original posts unchanged, %d author records.\n", m, authors)
}

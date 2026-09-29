package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// URL creates a separate PostgreSQL schema for one integration test.
func URL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "test_" + hex.EncodeToString(random)
	if _, err := admin.ExecContext(context.Background(), fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA "%s" CASCADE`, schema))
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	return u.String()
}

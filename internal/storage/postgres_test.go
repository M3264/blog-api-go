package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/M3264/blog-api-go/internal/storage"
	"github.com/M3264/blog-api-go/internal/testdb"
)

func TestImportLegacyJSONIsIdempotentAndPreservesSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "posts.json")
	legacy := `[{"slug":"old-guide","title":"Old guide","summary":"A previously saved how-to guide.","body":"This is a previously published article body.","category":"Guides","status":"published","created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-01T10:00:00Z"}]`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(context.Background(), testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for run, expected := range []int{1, 0} {
		count, err := db.ImportJSON(context.Background(), []byte(legacy))
		if err != nil || count != expected {
			t.Fatalf("import %d: count=%d error=%v", run, count, err)
		}
	}
	post, err := db.Get(context.Background(), "old-guide", true)
	if err != nil || post.Title != "Old guide" || post.Tags == nil {
		t.Fatalf("imported post: %+v %v", post, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != legacy {
		t.Fatalf("source was changed: %v", err)
	}
}

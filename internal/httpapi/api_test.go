package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/M3264/blog-api-go/internal/blog"
	"github.com/M3264/blog-api-go/internal/cache"
	"github.com/M3264/blog-api-go/internal/httpapi"
	"github.com/M3264/blog-api-go/internal/storage"
	"github.com/M3264/blog-api-go/internal/testdb"
)

func setup(t *testing.T) (http.Handler, *storage.DB) {
	t.Helper()
	db, err := storage.Open(context.Background(), testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return httpapi.New(db, nil, "test-secret-with-at-least-32-characters", "https://blog.example", slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

func request(t *testing.T, h http.Handler, method, path string, auth bool, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if auth {
		r.Header.Set("Authorization", "Bearer test-secret-with-at-least-32-characters")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func create(t *testing.T, h http.Handler, title, status, category string, tags ...string) blog.Post {
	t.Helper()
	input := map[string]any{
		"title": title, "summary": "A useful article for readers.",
		"body":     "This text is long enough to be a useful article body during tests.",
		"category": category, "tags": tags, "status": status, "author": "Editorial Team",
	}
	w := request(t, h, "POST", "/admin/posts", true, input)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var p blog.Post
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEditorialWorkflowAndPublicDiscovery(t *testing.T) {
	h, db := setup(t)
	if got := request(t, h, "GET", "/ready", false, nil).Code; got != 200 {
		t.Fatalf("ready: %d", got)
	}
	if got := request(t, h, "POST", "/admin/posts", false, map[string]string{}).Code; got != 401 {
		t.Fatalf("auth: %d", got)
	}
	draft := create(t, h, "Planning your week", "draft", "Guides", "Productivity")
	if got := request(t, h, "GET", "/posts/"+draft.Slug, false, nil).Code; got != 404 {
		t.Fatalf("draft public: %d", got)
	}
	if got := request(t, h, "GET", "/admin/posts/"+draft.Slug, true, nil).Code; got != 200 {
		t.Fatalf("draft admin: %d", got)
	}
	updated := request(t, h, "PATCH", "/admin/posts/"+draft.Slug, true, map[string]any{"status": "published", "featured": true})
	if updated.Code != 200 {
		t.Fatalf("publish: %d %s", updated.Code, updated.Body.String())
	}
	var published blog.Post
	if err := json.Unmarshal(updated.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published.PublishedAt == nil || !published.Featured {
		t.Fatalf("published metadata: %+v", published)
	}
	other := create(t, h, "Better study habits", "published", "Guides", "Study")
	create(t, h, "Private editorial update", "draft", "News", "Editorial")
	list := request(t, h, "GET", "/posts?category=guides&tag=productivity&featured=true", false, nil)
	if list.Code != 200 {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var page struct {
		Posts []blog.Post `json:"posts"`
		Total int         `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Posts) != 1 || page.Posts[0].Slug != draft.Slug {
		t.Fatalf("filtered list: %+v", page)
	}
	for _, endpoint := range []string{"/categories", "/tags", "/posts/" + draft.Slug + "/related"} {
		w := request(t, h, "GET", endpoint, false, nil)
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("Guides")) && endpoint == "/categories" {
			t.Fatalf("%s: %d %s", endpoint, w.Code, w.Body.String())
		}
	}
	related := request(t, h, "GET", "/posts/"+draft.Slug+"/related", false, nil)
	if !bytes.Contains(related.Body.Bytes(), []byte(other.Slug)) || bytes.Contains(related.Body.Bytes(), []byte("private-editorial-update")) {
		t.Fatalf("related: %s", related.Body.String())
	}
	admin := request(t, h, "GET", "/admin/posts?status=draft", true, nil)
	if !bytes.Contains(admin.Body.Bytes(), []byte("private-editorial-update")) || bytes.Contains(admin.Body.Bytes(), []byte(draft.Slug)) {
		t.Fatalf("admin filter: %s", admin.Body.String())
	}
	if got, err := db.Get(context.Background(), draft.Slug, true); err != nil || got.Slug != draft.Slug {
		t.Fatalf("database read: %+v %v", got, err)
	}
	if got := request(t, h, "DELETE", "/admin/posts/"+draft.Slug, true, nil).Code; got != 204 {
		t.Fatalf("delete: %d", got)
	}
	if got := request(t, h, "GET", "/posts/"+draft.Slug, false, nil).Code; got != 404 {
		t.Fatalf("deleted public: %d", got)
	}
}

func TestValidationSearchAndCORS(t *testing.T) {
	h, _ := setup(t)
	if got := request(t, h, "GET", "/posts?limit=51", false, nil).Code; got != 400 {
		t.Fatalf("limit: %d", got)
	}
	if got := request(t, h, "POST", "/admin/posts", true, map[string]any{"title": "x"}).Code; got != 400 {
		t.Fatalf("validation: %d", got)
	}
	if got := request(t, h, "PATCH", "/admin/posts/missing", true, map[string]any{}).Code; got != 400 {
		t.Fatalf("empty patch: %d", got)
	}
	create(t, h, "Percent proof guide", "published", "Guides", "Study")
	search := request(t, h, "GET", "/posts?q=%25", false, nil)
	if !bytes.Contains(search.Body.Bytes(), []byte(`"total":0`)) {
		t.Fatalf("LIKE wildcard was not escaped: %s", search.Body.String())
	}
	r := httptest.NewRequest("OPTIONS", "/admin/posts", nil)
	r.Header.Set("Origin", "https://blog.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://blog.example" {
		t.Fatalf("CORS: %d %v", w.Code, w.Header())
	}
}

func TestRedisCacheInvalidatesAfterPublish(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	db, err := storage.Open(context.Background(), testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client, err := cache.Open(redisURL, "test-blog-"+time.Now().Format("20060102150405.000000000"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	h := httpapi.New(db, client, "test-secret-with-at-least-32-characters", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	create(t, h, "First published article", "published", "Guides", "Writing")
	first := request(t, h, "GET", "/posts", false, nil)
	if first.Code != 200 {
		t.Fatalf("first read: %d %s", first.Code, first.Body.String())
	}
	second := request(t, h, "GET", "/posts", false, nil)
	if second.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected cache hit: %v", second.Header())
	}
	created := create(t, h, "Second draft article", "draft", "Guides", "Writing")
	if got := request(t, h, "PATCH", "/admin/posts/"+created.Slug, true, map[string]string{"status": "published"}).Code; got != 200 {
		t.Fatalf("publish: %d", got)
	}
	third := request(t, h, "GET", "/posts", false, nil)
	if third.Header().Get("X-Cache") == "HIT" || !bytes.Contains(third.Body.Bytes(), []byte(created.Slug)) {
		t.Fatalf("stale cache: %v %s", third.Header(), third.Body.String())
	}
}

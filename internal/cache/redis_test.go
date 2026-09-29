package cache

import (
	"context"
	"github.com/M3264/blog-api-go/internal/community"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPublicCacheHitsAndInvalidation(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not configured")
	}
	c, e := Open(url, "offscript-test-"+community.Random(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	defer func() {
		keys, _ := c.client.Keys(context.Background(), c.prefix+":*").Result()
		if len(keys) > 0 {
			c.client.Del(context.Background(), keys...)
		}
	}()
	calls := 0
	h := c.Public(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"posts":[]}`))
	})
	req := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("GET", "/posts", nil))
		return w
	}
	req()
	w := req()
	if calls != 1 || w.Header().Get("X-Cache") != "HIT" {
		t.Fatal("Redis public cache did not hit")
	}
	c.Invalidate()
	req()
	if calls != 2 {
		t.Fatal("mutation did not invalidate public cache")
	}
}

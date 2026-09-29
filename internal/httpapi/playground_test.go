package httpapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M3264/blog-api-go/internal/httpapi"
)

func TestPlaygroundIsOptInAndServesAssets(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	request := func(handler http.Handler, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if got := request(httpapi.New(nil, nil, "test-secret-with-at-least-32-characters", "", false, logger), "/").Code; got != http.StatusNotFound {
		t.Fatalf("disabled playground: %d", got)
	}
	handler := httpapi.New(nil, nil, "test-secret-with-at-least-32-characters", "", true, logger)
	page := request(handler, "/")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "The Journal") || !strings.Contains(page.Body.String(), "id=\"leadPost\"") || page.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("playground page: %d %s", page.Code, page.Body.String())
	}
	for _, asset := range []string{"/playground/app.js", "/playground/styles.css"} {
		if got := request(handler, asset).Code; got != http.StatusOK {
			t.Fatalf("asset %s: %d", asset, got)
		}
	}
	article := request(handler, "/stories/example-slug")
	if article.Code != http.StatusOK || !strings.Contains(article.Body.String(), "id=\"articlePanel\"") || article.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("article page: %d %s", article.Code, article.Body.String())
	}
	alias := request(handler, "/playground/posts/example-slug")
	if alias.Code != http.StatusTemporaryRedirect || alias.Header().Get("Location") != "/stories/example-slug" {
		t.Fatalf("legacy article path: %d %s", alias.Code, alias.Header().Get("Location"))
	}
}

func TestArticlePageRequiresPublishedPost(t *testing.T) {
	_, db := setup(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.New(db, nil, "test-secret-with-at-least-32-characters", "", true, logger)
	published := create(t, handler, "A published article", "published", "Guides")
	draft := create(t, handler, "A draft article", "draft", "Guides")
	page := request(t, handler, "GET", "/stories/"+published.Slug, false, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<title>A published article — The Journal</title>") {
		t.Fatalf("published article page: %d %s", page.Code, page.Body.String())
	}
	for _, slug := range []string{draft.Slug, "missing-article"} {
		if got := request(t, handler, "GET", "/stories/"+slug, false, nil).Code; got != http.StatusNotFound {
			t.Fatalf("article %s page: %d", slug, got)
		}
	}
	malicious := create(t, handler, "<script>alert(1)</script>", "published", "Guides")
	maliciousPage := request(t, handler, "GET", "/stories/"+malicious.Slug, false, nil).Body.String()
	if strings.Contains(maliciousPage, "<script>alert(1)</script>") || !strings.Contains(maliciousPage, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("article title was not escaped")
	}
}

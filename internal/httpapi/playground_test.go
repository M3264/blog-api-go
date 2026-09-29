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
	if got := request(httpapi.New(nil, nil, "test-secret-with-at-least-32-characters", "", false, logger), "/playground/").Code; got != http.StatusNotFound {
		t.Fatalf("disabled playground: %d", got)
	}
	handler := httpapi.New(nil, nil, "test-secret-with-at-least-32-characters", "", true, logger)
	page := request(handler, "/playground/")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Blog API") || page.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("playground page: %d %s", page.Code, page.Body.String())
	}
	for _, asset := range []string{"/playground/app.js", "/playground/styles.css"} {
		if got := request(handler, asset).Code; got != http.StatusOK {
			t.Fatalf("asset %s: %d", asset, got)
		}
	}
}

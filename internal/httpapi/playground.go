package httpapi

import (
	"bytes"
	"embed"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/M3264/blog-api-go/internal/storage"
)

//go:embed web
var playgroundFiles embed.FS

func mountPlayground(mux *http.ServeMux, db *storage.DB) {
	assets, err := fs.Sub(playgroundFiles, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/playground/", http.FileServer(http.FS(assets)))
	page, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		panic(err)
	}
	servePage := func(w http.ResponseWriter, r *http.Request) {
		playgroundHeaders(w)
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
	}
	mux.HandleFunc("GET /{$}", servePage)
	mux.HandleFunc("GET /stories/{slug}", func(w http.ResponseWriter, r *http.Request) {
		if db != nil {
			post, err := db.Get(r.Context(), r.PathValue("slug"), true)
			if errors.Is(err, storage.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			if err != nil {
				http.Error(w, "article unavailable", http.StatusInternalServerError)
				return
			}
			title := html.EscapeString(post.Title)
			description := html.EscapeString(post.Summary)
			meta := "<title>" + title + " — Offscript</title>\n  <meta name=\"description\" content=\"" + description + "\">\n  <meta property=\"og:title\" content=\"" + title + "\">\n  <meta property=\"og:description\" content=\"" + description + "\">"
			articlePage := strings.Replace(string(page), "<title>Offscript — Stories and updates</title>", meta, 1)
			playgroundHeaders(w)
			http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(articlePage))
			return
		}
		servePage(w, r)
	})
	mux.HandleFunc("GET /playground", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	})
	mux.Handle("GET /playground/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playground/" {
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}
		playgroundHeaders(w)
		fileServer.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /playground/posts/{slug}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/stories/"+r.PathValue("slug"), http.StatusTemporaryRedirect)
	})
}

func playgroundHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https: http: data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

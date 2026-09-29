package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/M3264/blog-api-go/internal/blog"
	"github.com/M3264/blog-api-go/internal/cache"
	"github.com/M3264/blog-api-go/internal/storage"
)

type API struct {
	db     *storage.DB
	cache  *cache.Cache
	token  string
	origin string
	log    *slog.Logger
}

func New(db *storage.DB, cacheClient *cache.Cache, token, origin string, enablePlayground bool, logger *slog.Logger) http.Handler {
	a := &API{db: db, cache: cacheClient, token: token, origin: origin, log: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /ready", a.ready)
	mux.HandleFunc("GET /posts", cacheClient.Public(a.listPublic))
	mux.HandleFunc("GET /posts/{slug}", cacheClient.Public(a.getPublic))
	mux.HandleFunc("GET /posts/{slug}/related", cacheClient.Public(a.related))
	mux.HandleFunc("GET /categories", cacheClient.Public(a.categories))
	mux.HandleFunc("GET /tags", cacheClient.Public(a.tags))
	mux.HandleFunc("GET /admin/posts", a.authorize(a.listAdmin))
	mux.HandleFunc("GET /admin/posts/{slug}", a.authorize(a.getAdmin))
	mux.HandleFunc("POST /admin/posts", a.authorize(a.create))
	mux.HandleFunc("PATCH /admin/posts/{slug}", a.authorize(a.update))
	mux.HandleFunc("DELETE /admin/posts/{slug}", a.authorize(a.delete))
	if enablePlayground {
		mountPlayground(mux, db)
	}
	return a.middleware(mux)
}

func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	if err := a.db.Ping(r.Context()); err != nil {
		a.internal(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

func (a *API) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		provided := strings.TrimPrefix(header, "Bearer ")
		if !strings.HasPrefix(header, "Bearer ") || len(provided) != len(a.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(a.token)) != 1 {
			writeError(w, 401, "valid bearer token required")
			return
		}
		next(w, r)
	}
}

func (a *API) listPublic(w http.ResponseWriter, r *http.Request) {
	f, ok := parseFilter(w, r)
	if !ok {
		return
	}
	f.Public = true
	posts, total, err := a.db.List(r.Context(), f)
	if err != nil {
		a.internal(w, err)
		return
	}
	writeJSON(w, 200, page(posts, total, f.Limit, f.Offset))
}

func (a *API) getPublic(w http.ResponseWriter, r *http.Request) {
	p, err := a.db.Get(r.Context(), r.PathValue("slug"), true)
	if err != nil {
		a.postError(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) related(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	posts, total, err := a.db.Related(r.Context(), r.PathValue("slug"), limit, offset)
	if err != nil {
		a.postError(w, err)
		return
	}
	writeJSON(w, 200, page(posts, total, limit, offset))
}

func (a *API) categories(w http.ResponseWriter, r *http.Request) {
	items, err := a.db.Terms(r.Context(), false)
	if err != nil {
		a.internal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"categories": items})
}

func (a *API) tags(w http.ResponseWriter, r *http.Request) {
	items, err := a.db.Terms(r.Context(), true)
	if err != nil {
		a.internal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"tags": items})
}

func (a *API) listAdmin(w http.ResponseWriter, r *http.Request) {
	f, ok := parseFilter(w, r)
	if !ok {
		return
	}
	f.Status = r.URL.Query().Get("status")
	if f.Status != "" && f.Status != "draft" && f.Status != "published" {
		writeError(w, 400, "status must be draft or published")
		return
	}
	posts, total, err := a.db.List(r.Context(), f)
	if err != nil {
		a.internal(w, err)
		return
	}
	writeJSON(w, 200, page(posts, total, f.Limit, f.Offset))
}

func (a *API) getAdmin(w http.ResponseWriter, r *http.Request) {
	p, err := a.db.Get(r.Context(), r.PathValue("slug"), false)
	if err != nil {
		a.postError(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeInput(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC()
	p := blog.Post{Status: "draft", Tags: []string{}, CreatedAt: now}
	if err := in.Apply(&p, now); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	p, err := a.db.Create(r.Context(), p)
	if err != nil {
		a.internal(w, err)
		return
	}
	a.cache.Invalidate()
	w.Header().Set("Location", "/admin/posts/"+p.Slug)
	writeJSON(w, 201, p)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeInput(w, r)
	if !ok {
		return
	}
	if emptyInput(in) {
		writeError(w, 400, "provide at least one field")
		return
	}
	p, err := a.db.Update(r.Context(), r.PathValue("slug"), in, time.Now().UTC())
	if err != nil {
		var validation blog.ValidationError
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, 404, "post not found")
		} else if errors.As(err, &validation) {
			writeError(w, 400, err.Error())
		} else {
			a.internal(w, err)
		}
		return
	}
	a.cache.Invalidate()
	writeJSON(w, 200, p)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	if err := a.db.Delete(r.Context(), r.PathValue("slug")); err != nil {
		a.postError(w, err)
		return
	}
	a.cache.Invalidate()
	w.WriteHeader(204)
}

func decodeInput(w http.ResponseWriter, r *http.Request) (blog.Input, bool) {
	var in blog.Input
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, 415, "Content-Type must be application/json")
		return in, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeError(w, 400, "invalid JSON body")
		return in, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, 400, "body must contain one JSON object")
		return in, false
	}
	return in, true
}

func emptyInput(in blog.Input) bool {
	return in.Title == nil && in.Summary == nil && in.Body == nil && in.Category == nil && in.Author == nil && in.CoverImage == nil && in.Tags == nil && in.Featured == nil && in.Status == nil
}

func parseFilter(w http.ResponseWriter, r *http.Request) (storage.Filter, bool) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return storage.Filter{}, false
	}
	f := storage.Filter{Limit: limit, Offset: offset, Q: strings.TrimSpace(r.URL.Query().Get("q")), Category: strings.TrimSpace(r.URL.Query().Get("category")), Tag: strings.TrimSpace(r.URL.Query().Get("tag"))}
	if len(f.Q) > 200 || len(f.Category) > 50 || len(f.Tag) > 30 {
		writeError(w, 400, "filter value too long")
		return f, false
	}
	if raw := r.URL.Query().Get("featured"); raw != "" {
		if raw != "true" && raw != "false" {
			writeError(w, 400, "featured must be true or false")
			return f, false
		}
		value := raw == "true"
		f.Featured = &value
	}
	return f, true
}

func pagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, offset := 10, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			writeError(w, 400, "limit must be between 1 and 50")
			return 0, 0, false
		}
		limit = n
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, 400, "offset must be zero or greater")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

func page(posts []blog.Post, total, limit, offset int) any {
	return map[string]any{"posts": posts, "total": total, "limit": limit, "offset": offset}
}

func (a *API) postError(w http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, 404, "post not found")
		return
	}
	a.internal(w, err)
}

func (a *API) internal(w http.ResponseWriter, err error) {
	a.log.Error("request failed", "error", err)
	writeError(w, 500, "internal server error")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		idBytes := make([]byte, 8)
		if _, err := rand.Read(idBytes); err == nil {
			w.Header().Set("X-Request-ID", hex.EncodeToString(idBytes))
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if a.origin != "" && r.Header.Get("Origin") == a.origin {
			w.Header().Set("Access-Control-Allow-Origin", a.origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
		}
		out := &responseWriter{ResponseWriter: w, status: 200}
		defer func() {
			if recovered := recover(); recovered != nil {
				a.log.Error("panic in request", "panic", recovered)
				if out.status == 200 {
					writeError(out, 500, "internal server error")
				}
			}
			a.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", out.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(out, r)
	})
}

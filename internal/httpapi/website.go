package httpapi

import (
	"bytes"
	"encoding/json"
	"github.com/M3264/blog-api-go/internal/blog"
	"github.com/M3264/blog-api-go/internal/community"
	"github.com/M3264/blog-api-go/internal/storage"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Page struct {
	Canonical, Title, Description, Path, Kind, Message, Query, Target, Frequency, Scope, Token string
	User                                                                                       *community.User
	Posts                                                                                      []blog.Post
	Post                                                                                       *blog.Post
	Terms                                                                                      []storage.TermCount
	Authors                                                                                    []Author
	Comments                                                                                   []Comment
	HTML                                                                                       template.HTML
	Likes                                                                                      int
	Liked, Saved, Following, Google                                                            bool
	Data                                                                                       string
	Total, Offset                                                                              int
	Profile                                                                                    *community.User
}
type Author struct{ Name, Bio, Avatar string }

var siteTemplate = template.Must(template.New("site.html").Funcs(template.FuncMap{
	"date": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format("Jan 2, 2006")
	},
	"readtime": func(body string) int { return max(1, (len(strings.Fields(body))+219)/220) },
	"path":     url.PathEscape, "query": url.QueryEscape,
	"owner": func(u *community.User, id int64) bool { return u != nil && u.ID == id },
	"inc":   func(i int) int { return i + 20 }, "dec": func(i int) int { return max(0, i-20) },
}).ParseFS(playgroundFiles, "web/site.html"))

func (a *API) mountWebsite(mux *http.ServeMux) {
	assets, _ := fs.Sub(playgroundFiles, "web")
	assetServer := http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("GET /assets/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/assets/")
		if name == "site.js" || name == "site.css" || strings.HasSuffix(name, ".ttf") || strings.HasSuffix(name, ".jpg") || strings.HasPrefix(name, "chunks/") && strings.HasSuffix(name, ".js") {
			assetServer.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /{$}", a.website)
	for _, route := range []string{"/search", "/topics", "/topics/{target}", "/authors", "/authors/{target}", "/stories/{slug}", "/register", "/login", "/forgot-password", "/account/{action}", "/profiles/{id}", "/settings", "/saved", "/feed", "/notifications", "/admin", "/admin/editor", "/admin/editor/{slug}", "/admin/members", "/admin/moderation", "/admin/authors", "/admin/topics", "/admin/newsletters"} {
		mux.HandleFunc("GET "+route, a.website)
	}
	mux.HandleFunc("POST /api/auth/{action}", a.auth)
	mux.HandleFunc("GET /api/me", a.private(func(w http.ResponseWriter, r *http.Request, u community.User) { writeJSON(w, 200, u) }, false))
	mux.HandleFunc("POST /api/account/{action}", a.private(a.account, false))
	mux.HandleFunc("POST /api/stories/{slug}/{action}", a.private(a.interaction, true))
	mux.HandleFunc("DELETE /api/stories/{slug}/{action}", a.private(a.interaction, true))
	mux.HandleFunc("PATCH /api/comments/{id}", a.private(a.commentAction, true))
	mux.HandleFunc("DELETE /api/comments/{id}", a.private(a.commentAction, true))
	mux.HandleFunc("POST /api/comments/{id}/{action}", a.private(a.commentAction, true))
	mux.HandleFunc("GET /api/stories/{slug}/comments", func(w http.ResponseWriter, r *http.Request) {
		if _, e := a.db.Get(r.Context(), r.PathValue("slug"), true); e != nil {
			a.postError(w, e)
			return
		}
		cs, e := a.comments(r, r.PathValue("slug"))
		if e != nil {
			a.internal(w, e)
			return
		}
		writeJSON(w, 200, cs)
	})
	for _, method := range []string{"POST", "DELETE"} {
		mux.HandleFunc(method+" /api/follows/{kind}/{target}", a.private(a.follow, true))
	}
	mux.HandleFunc("GET /api/notifications", a.private(func(w http.ResponseWriter, r *http.Request, u community.User) {
		a.queryJSON(w, r, `SELECT id,title,url,seen,created_at FROM notifications WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, u.ID)
	}, false))
	mux.HandleFunc("GET /api/subscription", a.private(func(w http.ResponseWriter, r *http.Request, u community.User) {
		a.queryJSON(w, r, `SELECT frequency,scope FROM subscriptions WHERE user_id=$1`, u.ID)
	}, false))
	mux.HandleFunc("GET /api/follows", a.private(func(w http.ResponseWriter, r *http.Request, u community.User) {
		a.queryJSON(w, r, `SELECT kind,target FROM follows WHERE user_id=$1 ORDER BY kind,target`, u.ID)
	}, false))
	mux.HandleFunc("GET /api/feed", a.private(func(w http.ResponseWriter, r *http.Request, u community.User) {
		ps, e := a.personalPosts(r, u.ID, false)
		if e != nil {
			a.internal(w, e)
			return
		}
		writeJSON(w, 200, ps)
	}, false))
	mux.HandleFunc("GET /api/bookmarks", a.private(func(w http.ResponseWriter, r *http.Request, u community.User) {
		ps, e := a.personalPosts(r, u.ID, true)
		if e != nil {
			a.internal(w, e)
			return
		}
		writeJSON(w, 200, ps)
	}, false))
	mux.HandleFunc("POST /api/admin/{action}", a.authorize(a.adminAction))
	mux.HandleFunc("POST /api/admin/uploads", a.authorize(a.upload))
	mux.HandleFunc("GET /api/admin/members", a.authorize(func(w http.ResponseWriter, r *http.Request) {
		a.queryJSON(w, r, `SELECT id,email,name,role,verified,suspended FROM users WHERE email ILIKE $1 OR name ILIKE $1 ORDER BY id DESC LIMIT 100`, "%"+r.URL.Query().Get("q")+"%")
	}))
	mux.HandleFunc("GET /api/admin/moderation", a.authorize(func(w http.ResponseWriter, r *http.Request) {
		a.queryJSON(w, r, `SELECT r.id,r.reason,r.resolved,c.id AS comment_id,c.body,c.hidden,u.name,p.slug FROM reports r JOIN comments c ON c.id=r.comment_id JOIN users u ON u.id=c.user_id JOIN posts p ON p.slug=c.slug ORDER BY r.resolved,r.id DESC LIMIT 100`)
	}))
	mux.HandleFunc("GET /api/admin/comments", a.authorize(func(w http.ResponseWriter, r *http.Request) {
		a.queryJSON(w, r, `SELECT c.id,c.body,c.hidden,c.deleted,u.name,c.slug FROM comments c JOIN users u ON u.id=c.user_id ORDER BY c.created_at DESC LIMIT 100`)
	}))
	mux.HandleFunc("GET /api/admin/newsletters", a.authorize(func(w http.ResponseWriter, r *http.Request) {
		a.queryJSON(w, r, `SELECT id,recipient,subject,state,attempts,last_error,created_at,sent_at FROM email_outbox ORDER BY id DESC LIMIT 100`)
	}))
	mux.HandleFunc("GET /api/admin/revisions/{slug}", a.authorize(func(w http.ResponseWriter, r *http.Request) {
		a.queryJSON(w, r, `SELECT id,snapshot,created_at FROM post_revisions WHERE slug=$1 ORDER BY id DESC LIMIT 30`, r.PathValue("slug"))
	}))
	mux.HandleFunc("POST /api/admin/revisions/{slug}/{id}", a.authorize(a.restoreRevision))
	mux.HandleFunc("GET /auth/google", a.googleStart)
	mux.HandleFunc("GET /auth/google/callback", a.googleCallback)
	mux.HandleFunc("GET /media/{name}", a.media)
	mux.HandleFunc("GET /rss.xml", a.syndication)
	mux.HandleFunc("GET /sitemap.xml", a.syndication)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("User-agent: *\nDisallow: /admin\nDisallow: /settings\nSitemap: " + a.community.Config.URL + "/sitemap.xml\n"))
	})
	mux.HandleFunc("GET /unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		a.renderMessage(w, r, 200, "Leave the newsletter?", "Confirm below to stop newsletter emails. You can subscribe again in account settings.")
	})
	mux.HandleFunc("POST /unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		_, e := a.community.DB.ExecContext(r.Context(), `UPDATE subscriptions SET frequency='off' WHERE unsubscribe_hash=$1`, r.FormValue("token"))
		if e != nil {
			a.internal(w, e)
			return
		}
		a.renderMessage(w, r, 200, "You’re unsubscribed", "Newsletter delivery is turned off.")
	})
	mux.HandleFunc("GET /playground", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", 301) })
	mux.HandleFunc("GET /playground/posts/{slug}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/stories/"+r.PathValue("slug"), 301)
	})
	mux.HandleFunc("GET /playground/", func(w http.ResponseWriter, r *http.Request) {
		target := "/"
		if strings.Contains(r.URL.Path, "editor") {
			target = "/admin"
		}
		if strings.Contains(r.URL.Path, "topics") {
			target = "/topics"
		}
		http.Redirect(w, r, target, 301)
	})
}
func (a *API) basePage(r *http.Request) Page {
	p := Page{Canonical: a.community.Config.URL + r.URL.Path, Path: r.URL.Path, Description: "Independent stories, useful ideas, and conversations worth having.", Google: a.community.Config.GoogleID != "", Frequency: "off", Scope: "all"}
	if u, e := a.current(r); e == nil {
		p.User = &u
	}
	return p
}
func (a *API) render(w http.ResponseWriter, r *http.Request, status int, p Page) {
	var b bytes.Buffer
	if e := siteTemplate.Execute(&b, p); e != nil {
		a.internal(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https: http:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.WriteHeader(status)
	w.Write(b.Bytes())
}
func (a *API) renderMessage(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	p := a.basePage(r)
	p.Kind = "message"
	p.Title = title
	p.Message = message
	p.Token = r.URL.Query().Get("token")
	a.render(w, r, status, p)
}
func (a *API) website(w http.ResponseWriter, r *http.Request) {
	p := a.basePage(r)
	path := r.URL.Path
	p.Kind = "listing"
	p.Title = "Stories and ideas, off the beaten track."
	p.Query = r.URL.Query().Get("q")
	p.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if p.Offset < 0 {
		p.Offset = 0
	}
	if len(p.Query) > 200 {
		a.renderMessage(w, r, 400, "Search too long", "Use at most 200 characters.")
		return
	}
	f := storage.Filter{Public: true, Limit: 20, Offset: p.Offset, Q: p.Query}
	var e error
	private := path == "/settings" || path == "/saved" || path == "/feed" || path == "/notifications" || strings.HasPrefix(path, "/admin")
	if private && p.User == nil {
		http.Redirect(w, r, "/login", 303)
		return
	}
	if strings.HasPrefix(path, "/admin") && (p.User.Role != "admin" || !p.User.Verified) {
		a.renderMessage(w, r, 403, "Admin access required", "This area is for verified administrators.")
		return
	}
	switch {
	case path == "/register" || path == "/login" || path == "/forgot-password" || strings.HasPrefix(path, "/account/"):
		p.Kind = "auth"
		p.Title = map[string]string{"/register": "Join the conversation.", "/login": "Welcome back.", "/forgot-password": "Recover your account.", "/account/reset": "Choose a new password.", "/account/setup": "Set up your admin account.", "/account/verify": "Verify your email."}[path]
		if p.Title == "" {
			http.NotFound(w, r)
			return
		}
		p.Token = r.URL.Query().Get("token")
	case strings.HasPrefix(path, "/stories/"):
		p.Kind = "article"
		post, err := a.db.Get(r.Context(), r.PathValue("slug"), true)
		if err != nil {
			a.postError(w, err)
			return
		}
		p.Post = &post
		p.Title = post.Title
		p.Description = post.Summary
		p.HTML, _, e = blog.RenderContent(post.Content, post.Body)
		if e != nil {
			a.internal(w, e)
			return
		}
		p.Comments, e = a.comments(r, post.Slug)
		if e != nil {
			a.internal(w, e)
			return
		}
		e = a.community.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM likes WHERE slug=$1`, post.Slug).Scan(&p.Likes)
		if p.User != nil {
			e = a.community.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM likes WHERE slug=$1 AND user_id=$2),EXISTS(SELECT 1 FROM bookmarks WHERE slug=$1 AND user_id=$2)`, post.Slug, p.User.ID).Scan(&p.Liked, &p.Saved)
		}
		p.Posts, _, e = a.db.Related(r.Context(), post.Slug, 3, 0)
	case path == "/topics":
		p.Kind = "topics"
		p.Title = "Find your next rabbit hole."
		p.Terms, e = a.db.Terms(r.Context(), false)
	case strings.HasPrefix(path, "/topics/"):
		p.Title = r.PathValue("target")
		p.Target = p.Title
		f.Category = p.Title
		p.Posts, p.Total, e = a.db.List(r.Context(), f)
		if p.User != nil {
			a.community.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM follows WHERE user_id=$1 AND kind='topic' AND target=$2)`, p.User.ID, p.Target).Scan(&p.Following)
		}
	case path == "/authors":
		p.Kind = "authors"
		p.Title = "Meet the voices."
		p.Authors, e = a.authorList(r)
	case strings.HasPrefix(path, "/authors/"):
		p.Target = r.PathValue("target")
		p.Title = p.Target
		var au Author
		e = a.community.DB.QueryRowContext(r.Context(), `SELECT name,bio,avatar FROM authors WHERE name=$1`, p.Target).Scan(&au.Name, &au.Bio, &au.Avatar)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		p.Description = au.Bio
		p.Posts, e = a.postsBySQL(r, `author=$1`, p.Target)
		if p.User != nil {
			a.community.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM follows WHERE user_id=$1 AND kind='author' AND target=$2)`, p.User.ID, p.Target).Scan(&p.Following)
		}
	case path == "/search":
		p.Title = "Search Offscript."
		p.Posts, p.Total, e = a.db.List(r.Context(), f)
	case path == "/feed" || path == "/saved":
		p.Title = "Your reading list."
		if path == "/feed" {
			p.Title = "A feed that follows your curiosity."
		}
		p.Posts, e = a.personalPosts(r, p.User.ID, path == "/saved")
		p.Total = len(p.Posts)
	case path == "/settings":
		p.Kind = "settings"
		p.Title = "Make yourself at home."
		a.community.DB.QueryRowContext(r.Context(), `SELECT frequency,scope FROM subscriptions WHERE user_id=$1`, p.User.ID).Scan(&p.Frequency, &p.Scope)
	case path == "/notifications":
		p.Kind = "notifications"
		p.Title = "Your notifications."
	case strings.HasPrefix(path, "/profiles/"):
		p.Kind = "profile"
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var u community.User
		e = a.community.DB.QueryRowContext(r.Context(), `SELECT id,name,bio FROM users WHERE id=$1 AND NOT suspended`, id).Scan(&u.ID, &u.Name, &u.Bio)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		p.Profile = &u
		p.Title = u.Name
	case strings.HasPrefix(path, "/admin/editor"):
		p.Kind = "editor"
		p.Title = "Give your next story a start."
		if slug := r.PathValue("slug"); slug != "" {
			post, err := a.db.Get(r.Context(), slug, false)
			if err != nil {
				a.postError(w, err)
				return
			}
			p.Post = &post
			p.Title = "Edit your story."
			raw, _ := json.Marshal(post)
			p.Data = string(raw)
		}
	case strings.HasPrefix(path, "/admin"):
		p.Kind = "admin"
		p.Title = map[string]string{"/admin": "The editorial desk.", "/admin/members": "Members.", "/admin/moderation": "Keep the conversation constructive.", "/admin/authors": "Author profiles.", "/admin/topics": "Topics.", "/admin/newsletters": "Email delivery."}[path]
		if path == "/admin" {
			f.Public = false
			f.Status = r.URL.Query().Get("status")
			p.Posts, p.Total, e = a.db.List(r.Context(), f)
		}
		if path == "/admin/authors" {
			p.Authors, e = a.authorList(r)
		}
		if path == "/admin/topics" {
			p.Terms, e = a.db.Terms(r.Context(), false)
		}
	default:
		p.Posts, p.Total, e = a.db.List(r.Context(), f)
	}
	if e != nil {
		a.internal(w, e)
		return
	}
	a.render(w, r, 200, p)
}
func (a *API) authorList(r *http.Request) ([]Author, error) {
	rows, e := a.community.DB.QueryContext(r.Context(), `SELECT name,bio,avatar FROM authors ORDER BY name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Author{}
	for rows.Next() {
		var u Author
		if e = rows.Scan(&u.Name, &u.Bio, &u.Avatar); e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (a *API) postsBySQL(r *http.Request, where string, args ...any) ([]blog.Post, error) {
	rows, e := a.community.DB.QueryContext(r.Context(), `SELECT slug FROM posts WHERE status='published' AND (`+where+`) ORDER BY published_at DESC LIMIT 100`, args...)
	if e != nil {
		return nil, e
	}
	var slugs []string
	for rows.Next() {
		var s string
		if e = rows.Scan(&s); e != nil {
			rows.Close()
			return nil, e
		}
		slugs = append(slugs, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []blog.Post{}
	for _, s := range slugs {
		p, e := a.db.Get(r.Context(), s, true)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (a *API) personalPosts(r *http.Request, id int64, saved bool) ([]blog.Post, error) {
	where := `EXISTS(SELECT 1 FROM follows f WHERE f.user_id=$1 AND ((f.kind='author' AND f.target=posts.author) OR (f.kind='topic' AND (f.target=posts.category OR posts.tags_json ? f.target))))`
	if saved {
		where = `EXISTS(SELECT 1 FROM bookmarks b WHERE b.user_id=$1 AND b.slug=posts.slug)`
	}
	return a.postsBySQL(r, where, id)
}
func (a *API) restoreRevision(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	e := a.community.DB.QueryRowContext(r.Context(), `SELECT snapshot FROM post_revisions WHERE slug=$1 AND id=$2`, r.PathValue("slug"), r.PathValue("id")).Scan(&raw)
	if e != nil {
		writeError(w, 404, "revision not found")
		return
	}
	var p blog.Post
	if e = json.Unmarshal(raw, &p); e != nil {
		a.internal(w, e)
		return
	}
	draft := "draft"
	clear := true
	in := blog.Input{ClearSchedule: &clear, Title: &p.Title, Summary: &p.Summary, Body: &p.Body, Category: &p.Category, Author: &p.Author, CoverImage: &p.CoverImage, Tags: &p.Tags, Status: &draft}
	if len(p.Content) > 0 {
		in.Content = &p.Content
	}
	post, e := a.db.Update(r.Context(), p.Slug, in, time.Now().UTC())
	if e != nil {
		a.internal(w, e)
		return
	}
	a.cache.Invalidate()
	writeJSON(w, 200, post)
}

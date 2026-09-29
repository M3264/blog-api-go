package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/M3264/blog-api-go/internal/blog"
	"github.com/M3264/blog-api-go/internal/community"
	"github.com/M3264/blog-api-go/internal/httpapi"
	"github.com/M3264/blog-api-go/internal/storage"
	"github.com/M3264/blog-api-go/internal/testdb"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type siteFixture struct {
	db *storage.DB
	s  *community.Service
	h  http.Handler
}

func newSite(t *testing.T) *siteFixture {
	t.Helper()
	db, e := storage.Open(context.Background(), testdb.URL(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s := &community.Service{DB: db.SQL(), Config: community.Config{URL: "http://example.test", MediaDir: t.TempDir()}}
	h := httpapi.NewWebsite(db, nil, s, "legacy-disabled", slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &siteFixture{db, s, h}
}
func (f *siteFixture) req(t *testing.T, method, path, token, csrf string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		b = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, "http://example.test"+path, b)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.test")
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "offscript_session", Value: token})
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func (f *siteFixture) user(t *testing.T, name, role string, verified bool) (int64, string, string) {
	t.Helper()
	var id int64
	hash, e := community.Password("a-long-test-password")
	if e != nil {
		t.Fatal(e)
	}
	e = f.s.DB.QueryRow(`INSERT INTO users(email,name,password_hash,role,verified) VALUES($1,$2,$3,$4,$5) RETURNING id`, strings.ToLower(name)+"@example.test", name, hash, role, verified).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	token, e := f.s.Session(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	u, e := f.s.User(context.Background(), token)
	if e != nil {
		t.Fatal(e)
	}
	return id, token, u.CSRF
}
func (f *siteFixture) post(t *testing.T, title, status string) blog.Post {
	t.Helper()
	now := time.Now().UTC()
	p := blog.Post{Title: title, Summary: "A sufficiently descriptive summary.", Body: "An original story with enough words for a useful test.", Category: "Ideas", Author: "Avery", Status: status, Tags: []string{}, CreatedAt: now, UpdatedAt: now}
	if status == "published" {
		p.PublishedAt = &now
	}
	p, e := f.db.Create(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func code(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("HTTP %d, want %d: %s", w.Code, want, w.Body.String())
	}
}
func TestWebsiteAccountsPermissionsAndIsolation(t *testing.T) {
	f := newSite(t)
	p := f.post(t, "Existing permanent story", "published")
	draft := f.post(t, "A private draft", "draft")
	_, reader, csrf := f.user(t, "Reader", "reader", true)
	_, unverified, uncsrf := f.user(t, "Unverified", "reader", false)
	adminID, admin, ac := f.user(t, "Admin", "admin", true)
	code(t, f.req(t, "GET", "/", "", "", nil), 200)
	for _, asset := range []string{"/assets/index.html", "/assets/app.js", "/assets/site.html"} {
		code(t, f.req(t, "GET", asset, "", "", nil), 404)
	}
	code(t, f.req(t, "GET", "/auth/google/callback?state=forged&code=forged", "", "", nil), 400)
	w := f.req(t, "GET", "/stories/"+p.Slug, "", "", nil)
	code(t, w, 200)
	if !strings.Contains(w.Body.String(), p.Body) {
		t.Fatal("article not server rendered")
	}
	code(t, f.req(t, "GET", "/stories/"+draft.Slug, "", "", nil), 404)
	for _, path := range []string{"/admin/posts", "/api/admin/members", "/api/admin/newsletters"} {
		code(t, f.req(t, "GET", path, reader, csrf, nil), 401)
		code(t, f.req(t, "GET", path, admin, ac, nil), 200)
	}
	body := map[string]any{"body": "This is a reader comment"}
	code(t, f.req(t, "POST", "/api/stories/"+p.Slug+"/comment", unverified, uncsrf, body), 403)
	code(t, f.req(t, "POST", "/api/stories/"+p.Slug+"/comment", reader, "", body), 403)
	code(t, f.req(t, "POST", "/api/stories/"+p.Slug+"/bookmark", reader, csrf, map[string]any{}), 200)
	code(t, f.req(t, "GET", "/api/bookmarks", "", "", nil), 401)
	w = f.req(t, "GET", "/api/bookmarks", reader, "", nil)
	code(t, w, 200)
	if !strings.Contains(w.Body.String(), p.Slug) {
		t.Fatal("missing private bookmark")
	}
	w = f.req(t, "GET", "/api/bookmarks", admin, "", nil)
	if strings.Contains(w.Body.String(), p.Slug) {
		t.Fatal("private bookmark leaked")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response cacheable")
	}
	code(t, f.req(t, "POST", "/api/admin/member", admin, ac, map[string]any{"id": adminID, "role": "reader", "suspended": false}), 400)
	code(t, f.req(t, "POST", "/api/account/revoke", reader, csrf, map[string]any{}), 200)
	code(t, f.req(t, "GET", "/api/me", reader, "", nil), 401)
	code(t, f.req(t, "POST", "/api/admin/member", admin, ac, map[string]any{"id": 2, "role": "reader", "suspended": true}), 200)
	code(t, f.req(t, "GET", "/api/me", reader, "", nil), 401)
	r := httptest.NewRequest("POST", "http://example.test/api/account/profile", strings.NewReader(`{"name":"Forged","bio":""}`))
	r.AddCookie(&http.Cookie{Name: "offscript_session", Value: admin})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", ac)
	r.Header.Set("Origin", "https://attacker.test")
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	code(t, w, 403)
}
func TestRegistrationRecoveryAndSingleUseTokens(t *testing.T) {
	f := newSite(t)
	ctx := context.Background()
	w := f.req(t, "POST", "/api/auth/register", "", "", map[string]string{"email": "new@example.test", "name": "New reader", "password": "this-is-a-good-password"})
	code(t, w, 202)
	var id int64
	var role string
	var verified bool
	f.s.DB.QueryRow(`SELECT id,role,verified FROM users WHERE email='new@example.test'`).Scan(&id, &role, &verified)
	if role != "reader" || verified {
		t.Fatal("public registration granted wrong role")
	}
	var html string
	f.s.DB.QueryRow(`SELECT html FROM email_outbox WHERE user_id=$1`, id).Scan(&html)
	token := strings.Split(strings.Split(html, "?token=")[1], `"`)[0]
	code(t, f.req(t, "POST", "/api/auth/verify", "", "", map[string]string{"token": token}), 200)
	code(t, f.req(t, "POST", "/api/auth/verify", "", "", map[string]string{"token": token}), 400)
	_, session, _ := f.user(t, "Reset", "reader", true)
	u, _ := f.s.User(ctx, session)
	reset, _ := f.s.Token(ctx, u.ID, "reset")
	code(t, f.req(t, "POST", "/api/auth/recover", "", "", map[string]string{"token": reset, "password": "updated-long-password"}), 200)
	if _, e := f.s.User(ctx, session); e == nil {
		t.Fatal("old session survived recovery")
	}
	code(t, f.req(t, "POST", "/api/auth/login", "", "", map[string]string{"email": "reset@example.test", "password": "updated-long-password"}), 200)
	code(t, f.req(t, "POST", "/api/auth/login", "", "", map[string]string{"email": "reset@example.test", "password": "wrong"}), 401)
	expired, _ := f.s.Token(ctx, u.ID, "reset")
	f.s.DB.Exec(`UPDATE account_tokens SET expires_at=now()-interval '1 second' WHERE hash=$1`, community.Hash(expired))
	code(t, f.req(t, "POST", "/api/auth/recover", "", "", map[string]string{"token": expired, "password": "updated-long-password"}), 400)
}
func TestCommentsRepliesModerationAndOwnership(t *testing.T) {
	f := newSite(t)
	p := f.post(t, "Conversation", "published")
	_, a, ac := f.user(t, "Alice", "reader", true)
	_, b, bc := f.user(t, "Bob", "reader", true)
	_, admin, dc := f.user(t, "Editor", "admin", true)
	w := f.req(t, "POST", "/api/stories/"+p.Slug+"/comment", a, ac, map[string]any{"body": "<script>alert('x')</script>"})
	code(t, w, 201)
	var result map[string]int64
	json.Unmarshal(w.Body.Bytes(), &result)
	id := result["id"]
	w = f.req(t, "GET", "/stories/"+p.Slug, "", "", nil)
	if strings.Contains(w.Body.String(), "<script>alert") {
		t.Fatal("comment executed")
	}
	code(t, f.req(t, "PATCH", fmt.Sprintf("/api/comments/%d", id), b, bc, map[string]string{"body": "hijacked"}), 404)
	w = f.req(t, "POST", "/api/stories/"+p.Slug+"/comment", b, bc, map[string]any{"body": "A reply", "parent_id": id})
	code(t, w, 201)
	json.Unmarshal(w.Body.Bytes(), &result)
	reply := result["id"]
	code(t, f.req(t, "POST", "/api/stories/"+p.Slug+"/comment", a, ac, map[string]any{"body": "Too deep", "parent_id": reply}), 400)
	var n int
	f.s.DB.QueryRow(`SELECT count(*) FROM notifications WHERE event_key=$1`, fmt.Sprintf("reply:%d", reply)).Scan(&n)
	if n != 1 {
		t.Fatal("reply notification missing")
	}
	code(t, f.req(t, "POST", fmt.Sprintf("/api/comments/%d/report", id), b, bc, map[string]string{"reason": "Abuse report"}), 200)
	code(t, f.req(t, "POST", "/api/admin/moderate", admin, dc, map[string]any{"id": id, "hidden": true}), 200)
	w = f.req(t, "GET", "/stories/"+p.Slug, "", "", nil)
	if strings.Contains(w.Body.String(), "alert('x')") {
		t.Fatal("hidden body leaked")
	}
	code(t, f.req(t, "DELETE", fmt.Sprintf("/api/comments/%d", reply), b, bc, nil), 200)
}
func TestSchedulingRevisionsAndNewsletterDeduplication(t *testing.T) {
	f := newSite(t)
	ctx := context.Background()
	p := f.post(t, "Scheduled story", "draft")
	id, reader, rc := f.user(t, "Subscriber", "reader", true)
	_, admin, ac := f.user(t, "Publisher", "admin", true)
	code(t, f.req(t, "POST", "/api/account/subscription", reader, rc, map[string]string{"frequency": "immediate", "scope": "following"}), 200)
	code(t, f.req(t, "POST", "/api/follows/author/Avery", reader, rc, map[string]any{}), 200)
	at := time.Now().Add(-time.Minute)
	_, e := f.db.Update(ctx, p.Slug, blog.Input{ScheduledAt: &at}, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	p, e = f.db.Get(ctx, p.Slug, true)
	if e != nil {
		t.Fatal(e)
	}
	if p.ScheduledAt != nil {
		t.Fatal("schedule not consumed")
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var n int
	f.s.DB.QueryRow(`SELECT count(*) FROM email_outbox WHERE user_id=$1 AND kind='newsletter'`, id).Scan(&n)
	if n != 1 {
		t.Fatalf("duplicate or missing publication email: %d", n)
	}
	title := "Edited published story"
	f.db.Update(ctx, p.Slug, blog.Input{Title: &title}, time.Now())
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	f.s.DB.QueryRow(`SELECT count(*) FROM email_outbox WHERE user_id=$1 AND kind='newsletter'`, id).Scan(&n)
	if n != 1 {
		t.Fatal("edit resent publication email")
	}
	w := f.req(t, "GET", "/api/feed", reader, "", nil)
	code(t, w, 200)
	if !strings.Contains(w.Body.String(), p.Slug) {
		t.Fatal("followed story missing from feed")
	}
	var rev int64
	f.s.DB.QueryRow(`SELECT id FROM post_revisions WHERE slug=$1 ORDER BY id LIMIT 1`, p.Slug).Scan(&rev)
	code(t, f.req(t, "POST", fmt.Sprintf("/api/admin/revisions/%s/%d", p.Slug, rev), admin, ac, map[string]any{}), 200)
	restored, e := f.db.Get(ctx, p.Slug, false)
	if e != nil || restored.Status != "draft" || restored.Title != p.Title {
		t.Fatal("revision recovery failed")
	}
	var token string
	f.s.DB.QueryRow(`SELECT unsubscribe_hash FROM subscriptions WHERE user_id=$1`, id).Scan(&token)
	r := httptest.NewRequest("POST", "http://example.test/unsubscribe", strings.NewReader("token="+token))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	code(t, w, 200)
	var freq string
	f.s.DB.QueryRow(`SELECT frequency FROM subscriptions WHERE user_id=$1`, id).Scan(&freq)
	if freq != "off" {
		t.Fatal("unsubscribe failed")
	}
}
func TestUploadsAndPageFlows(t *testing.T) {
	f := newSite(t)
	_, admin, csrf := f.user(t, "SiteAdmin", "admin", true)
	for _, path := range []string{"/", "/search", "/topics", "/authors", "/register", "/login", "/forgot-password", "/account/setup?token=test", "/settings", "/saved", "/feed", "/notifications", "/admin", "/admin/editor", "/admin/members", "/admin/moderation", "/admin/authors", "/admin/topics", "/admin/newsletters", "/rss.xml", "/sitemap.xml"} {
		w := f.req(t, "GET", path, admin, "", nil)
		code(t, w, 200)
	}
	upload := func(data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, _ := mw.CreateFormFile("image", "image.png")
		part.Write(data)
		mw.Close()
		r := httptest.NewRequest("POST", "http://example.test/api/admin/uploads", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "offscript_session", Value: admin})
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		return w
	}
	code(t, upload([]byte("<svg onload=alert(1)></svg>")), 400)
	var img bytes.Buffer
	png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 32, 32)))
	w := upload(img.Bytes())
	code(t, w, 201)
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)
	code(t, f.req(t, "GET", data["url"].(string), "", "", nil), 200)
}

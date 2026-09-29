package httpapi

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"github.com/M3264/blog-api-go/internal/cache"
	"github.com/M3264/blog-api-go/internal/community"
	"github.com/M3264/blog-api-go/internal/storage"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"strings"
)

func NewWebsite(db *storage.DB, c *cache.Cache, s *community.Service, token string, log *slog.Logger) http.Handler {
	return (&API{db: db, cache: c, community: s, token: token, log: log}).routes(false)
}
func (a *API) current(r *http.Request) (community.User, error) {
	c, e := r.Cookie("offscript_session")
	if e != nil {
		return community.User{}, e
	}
	return a.community.User(r.Context(), c.Value)
}
func (a *API) validCSRF(r *http.Request, u community.User) bool {
	t := r.Header.Get("X-CSRF-Token")
	if t == "" && r.Method == "GET" {
		t = r.URL.Query().Get("csrf")
	}
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(u.CSRF)) == 1 && a.sameOrigin(r)
}
func (a *API) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == a.community.Config.URL
}
func (a *API) cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: a.community.Config.Secure, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (a *API) private(next func(http.ResponseWriter, *http.Request, community.User), verified bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, e := a.current(r)
		if e != nil {
			writeError(w, 401, "sign in to continue")
			return
		}
		if verified && !u.Verified {
			writeError(w, 403, "verify your email to continue")
			return
		}
		if r.Method != "GET" && !a.validCSRF(r, u) {
			writeError(w, 403, "invalid CSRF token")
			return
		}
		next(w, r, u)
	}
}
func decodeCommunity(w http.ResponseWriter, r *http.Request, out any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, 415, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		writeError(w, 400, "invalid request body")
		return false
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		writeError(w, 400, "body must contain one JSON object")
		return false
	}
	return true
}
func (a *API) auth(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		writeError(w, 403, "request origin rejected")
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if a.community.Limited(r.Context(), "auth:"+host, 30) {
		writeError(w, 429, "too many attempts; try again in 15 minutes")
		return
	}
	var in struct{ Email, Password, Name, Token string }
	if !decodeCommunity(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	action := r.PathValue("action")
	s := a.community
	switch action {
	case "register":
		addr, e := mail.ParseAddress(in.Email)
		if e != nil || addr.Address != in.Email || len(in.Email) > 254 || len(in.Name) < 2 || len(in.Name) > 100 {
			writeError(w, 400, "enter a valid email and a name of 2 to 100 characters")
			return
		}
		hash, e := community.Password(in.Password)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
		var id int64
		e = s.DB.QueryRowContext(r.Context(), `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING RETURNING id`, in.Email, in.Name, hash).Scan(&id)
		if e == nil {
			e = s.QueueAccount(r.Context(), id, in.Email, "verify")
		}
		if e != nil && e != sql.ErrNoRows {
			a.internal(w, e)
			return
		}
		writeJSON(w, 202, map[string]string{"message": "Check your inbox for a verification link. If you already have an account, sign in."})
	case "login":
		if s.Limited(r.Context(), "login:"+in.Email, 10) {
			writeError(w, 429, "too many attempts; try again in 15 minutes")
			return
		}
		var id int64
		var hash string
		e := s.DB.QueryRowContext(r.Context(), `SELECT id,password_hash FROM users WHERE email=$1 AND NOT suspended`, in.Email).Scan(&id, &hash)
		if e != nil || !community.CheckPassword(hash, in.Password) {
			writeError(w, 401, "email or password is incorrect")
			return
		}
		a.startSession(w, r, id)
	case "reset":
		if s.Limited(r.Context(), "reset:"+in.Email, 5) {
			writeError(w, 429, "too many attempts; try again later")
			return
		}
		var id int64
		e := s.DB.QueryRowContext(r.Context(), `SELECT id FROM users WHERE email=$1 AND NOT suspended`, in.Email).Scan(&id)
		if e == nil {
			if e = s.QueueAccount(r.Context(), id, in.Email, "reset"); e != nil {
				a.internal(w, e)
				return
			}
		}
		writeJSON(w, 202, map[string]string{"message": "If that account exists, a recovery link is on its way."})
	case "verify", "setup", "recover":
		purpose := action
		if purpose == "recover" {
			purpose = "reset"
		}
		id, e := s.Consume(r.Context(), in.Token, purpose, in.Password)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
		a.startSession(w, r, id)
	default:
		writeError(w, 404, "unknown account action")
	}
}
func (a *API) startSession(w http.ResponseWriter, r *http.Request, id int64) {
	t, e := a.community.Session(r.Context(), id)
	if e != nil {
		a.internal(w, e)
		return
	}
	a.cookie(w, "offscript_session", t, 30*24*3600)
	writeJSON(w, 200, map[string]string{"message": "You’re signed in", "redirect": "/feed"})
}
func (a *API) account(w http.ResponseWriter, r *http.Request, u community.User) {
	s := a.community
	switch r.PathValue("action") {
	case "logout":
		c, _ := r.Cookie("offscript_session")
		_, e := s.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE hash=$1`, community.Hash(c.Value))
		if e != nil {
			a.internal(w, e)
			return
		}
		a.cookie(w, "offscript_session", "", -1)
	case "revoke":
		_, e := s.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, u.ID)
		if e != nil {
			a.internal(w, e)
			return
		}
		a.cookie(w, "offscript_session", "", -1)
	case "verify":
		if !u.Verified {
			if e := s.QueueAccount(r.Context(), u.ID, u.Email, "verify"); e != nil {
				a.internal(w, e)
				return
			}
		}
	case "profile":
		var in struct{ Name, Bio string }
		if !decodeCommunity(w, r, &in) {
			return
		}
		if len(in.Name) < 2 || len(in.Name) > 100 || len(in.Bio) > 1000 {
			writeError(w, 400, "name must be 2 to 100 characters; bio must be at most 1000")
			return
		}
		_, e := s.DB.ExecContext(r.Context(), `UPDATE users SET name=$2,bio=$3 WHERE id=$1`, u.ID, in.Name, in.Bio)
		if e != nil {
			a.internal(w, e)
			return
		}
	case "password":
		var in struct{ Current, Password string }
		if !decodeCommunity(w, r, &in) {
			return
		}
		var hash string
		s.DB.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=$1`, u.ID).Scan(&hash)
		if !community.CheckPassword(hash, in.Current) {
			writeError(w, 400, "current password is incorrect")
			return
		}
		hash, e := community.Password(in.Password)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			a.internal(w, e)
			return
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(r.Context(), `UPDATE users SET password_hash=$2 WHERE id=$1`, u.ID, hash); e == nil {
			_, e = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, u.ID)
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			a.internal(w, e)
			return
		}
		a.cookie(w, "offscript_session", "", -1)
	case "subscription":
		var in struct{ Frequency, Scope string }
		if !decodeCommunity(w, r, &in) {
			return
		}
		if in.Frequency != "off" && in.Frequency != "weekly" && in.Frequency != "immediate" || in.Scope != "all" && in.Scope != "following" {
			writeError(w, 400, "invalid newsletter preferences")
			return
		}
		_, e := s.DB.ExecContext(r.Context(), `INSERT INTO subscriptions(user_id,frequency,scope,unsubscribe_hash) VALUES($1,$2,$3,$4) ON CONFLICT(user_id) DO UPDATE SET frequency=$2,scope=$3`, u.ID, in.Frequency, in.Scope, community.Hash(community.Random()))
		if e != nil {
			a.internal(w, e)
			return
		}
	case "notifications":
		_, e := s.DB.ExecContext(r.Context(), `UPDATE notifications SET seen=true WHERE user_id=$1`, u.ID)
		if e != nil {
			a.internal(w, e)
			return
		}
	default:
		writeError(w, 404, "unknown account action")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) googleStart(w http.ResponseWriter, r *http.Request) {
	var link int64
	if r.URL.Query().Get("link") == "true" {
		u, e := a.current(r)
		if e != nil || !a.validCSRF(r, u) {
			writeError(w, 403, "sign in before linking Google")
			return
		}
		link = u.ID
	}
	url, state, e := a.community.GoogleStart(r.Context(), link)
	if e != nil {
		writeError(w, 503, e.Error())
		return
	}
	a.cookie(w, "offscript_oauth", state, 600)
	http.Redirect(w, r, url, 302)
}
func (a *API) googleCallback(w http.ResponseWriter, r *http.Request) {
	c, e := r.Cookie("offscript_oauth")
	state := r.URL.Query().Get("state")
	if e != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		writeError(w, 400, "invalid sign-in state")
		return
	}
	a.cookie(w, "offscript_oauth", "", -1)
	id, e := a.community.GoogleFinish(r.Context(), state, r.URL.Query().Get("code"))
	if e != nil {
		a.renderMessage(w, r, 400, "Google sign-in", e.Error())
		return
	}
	t, e := a.community.Session(r.Context(), id)
	if e != nil {
		a.internal(w, e)
		return
	}
	a.cookie(w, "offscript_session", t, 30*24*3600)
	http.Redirect(w, r, "/feed", 302)
}

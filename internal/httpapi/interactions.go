package httpapi

import (
	"database/sql"
	"fmt"
	"github.com/M3264/blog-api-go/internal/community"
	"net/http"
	"strconv"
	"strings"
)

type Comment struct {
	ID, UserID      int64
	ParentID        *int64
	Name, Body      string
	Hidden, Deleted bool
	Date            string
}

func (a *API) comments(r *http.Request, slug string) ([]Comment, error) {
	rows, e := a.community.DB.QueryContext(r.Context(), `SELECT c.id,c.user_id,c.parent_id,u.name,CASE WHEN c.deleted THEN '[Comment deleted]' WHEN c.hidden OR u.suspended THEN '[Comment hidden]' ELSE c.body END,c.hidden,c.deleted,to_char(c.created_at,'Mon DD, YYYY') FROM comments c JOIN users u ON u.id=c.user_id WHERE c.slug=$1 ORDER BY c.created_at,c.id LIMIT 500`, slug)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if e = rows.Scan(&c.ID, &c.UserID, &c.ParentID, &c.Name, &c.Body, &c.Hidden, &c.Deleted, &c.Date); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (a *API) interaction(w http.ResponseWriter, r *http.Request, u community.User) {
	slug, action := r.PathValue("slug"), r.PathValue("action")
	if _, e := a.db.Get(r.Context(), slug, true); e != nil {
		a.postError(w, e)
		return
	}
	s := a.community
	switch action {
	case "like", "bookmark":
		table := "likes"
		if action == "bookmark" {
			table = "bookmarks"
		}
		var e error
		if r.Method == "DELETE" {
			_, e = s.DB.ExecContext(r.Context(), `DELETE FROM `+table+` WHERE user_id=$1 AND slug=$2`, u.ID, slug)
		} else {
			_, e = s.DB.ExecContext(r.Context(), `INSERT INTO `+table+`(user_id,slug) VALUES($1,$2) ON CONFLICT DO NOTHING`, u.ID, slug)
		}
		if e != nil {
			a.internal(w, e)
			return
		}
	case "comment":
		if s.Limited(r.Context(), fmt.Sprintf("comments:%d", u.ID), 30) {
			writeError(w, 429, "too many comments; try again later")
			return
		}
		var in struct {
			Body     string
			ParentID *int64 `json:"parent_id"`
		}
		if !decodeCommunity(w, r, &in) {
			return
		}
		in.Body = strings.TrimSpace(in.Body)
		if len(in.Body) < 1 || len(in.Body) > 5000 {
			writeError(w, 400, "comment must be 1 to 5000 characters")
			return
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			a.internal(w, e)
			return
		}
		defer tx.Rollback()
		var parentUser int64
		if in.ParentID != nil {
			var parent sql.NullInt64
			e = tx.QueryRowContext(r.Context(), `SELECT parent_id,user_id FROM comments WHERE id=$1 AND slug=$2 AND NOT deleted AND NOT hidden FOR UPDATE`, *in.ParentID, slug).Scan(&parent, &parentUser)
			if e != nil || parent.Valid {
				writeError(w, 400, "reply to a visible top-level comment")
				return
			}
		}
		var id int64
		e = tx.QueryRowContext(r.Context(), `INSERT INTO comments(slug,user_id,parent_id,body) VALUES($1,$2,$3,$4) RETURNING id`, slug, u.ID, in.ParentID, in.Body).Scan(&id)
		if e != nil {
			a.internal(w, e)
			return
		}
		if parentUser != 0 && parentUser != u.ID {
			_, e = tx.ExecContext(r.Context(), `INSERT INTO notifications(user_id,title,url,event_key) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, parentUser, u.Name+" replied to your comment", "/stories/"+slug+"#comments", fmt.Sprintf("reply:%d", id))
			if e != nil {
				a.internal(w, e)
				return
			}
		}
		if e = tx.Commit(); e != nil {
			a.internal(w, e)
			return
		}
		writeJSON(w, 201, map[string]int64{"id": id})
		return
	default:
		writeError(w, 404, "unknown interaction")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) commentAction(w http.ResponseWriter, r *http.Request, u community.User) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		writeError(w, 400, "invalid comment")
		return
	}
	var in struct{ Body, Reason string }
	if r.Method != "DELETE" && !decodeCommunity(w, r, &in) {
		return
	}
	action := r.PathValue("action")
	var result sql.Result
	switch {
	case action == "report":
		if len(in.Reason) < 3 || len(in.Reason) > 1000 {
			writeError(w, 400, "report reason must be 3 to 1000 characters")
			return
		}
		result, e = a.community.DB.ExecContext(r.Context(), `INSERT INTO reports(comment_id,user_id,reason) SELECT c.id,$2,$3 FROM comments c JOIN posts p ON p.slug=c.slug WHERE c.id=$1 AND p.status='published' ON CONFLICT DO NOTHING`, id, u.ID, in.Reason)
	case r.Method == "DELETE":
		result, e = a.community.DB.ExecContext(r.Context(), `UPDATE comments SET deleted=true,body='',updated_at=now() WHERE id=$1 AND user_id=$2`, id, u.ID)
	default:
		in.Body = strings.TrimSpace(in.Body)
		if len(in.Body) < 1 || len(in.Body) > 5000 {
			writeError(w, 400, "comment must be 1 to 5000 characters")
			return
		}
		result, e = a.community.DB.ExecContext(r.Context(), `UPDATE comments SET body=$3,updated_at=now() WHERE id=$1 AND user_id=$2 AND NOT deleted AND NOT hidden`, id, u.ID, in.Body)
	}
	if e != nil {
		a.internal(w, e)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, 404, "comment unavailable or already reported")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) follow(w http.ResponseWriter, r *http.Request, u community.User) {
	kind, target := r.PathValue("kind"), r.PathValue("target")
	var exists bool
	var e error
	if kind == "author" {
		e = a.community.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM authors WHERE name=$1)`, target).Scan(&exists)
	} else if kind == "topic" {
		e = a.community.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM topics WHERE name=$1)`, target).Scan(&exists)
	}
	if e != nil {
		a.internal(w, e)
		return
	}
	if !exists {
		writeError(w, 404, "author or topic not found")
		return
	}
	if r.Method == "DELETE" {
		_, e = a.community.DB.ExecContext(r.Context(), `DELETE FROM follows WHERE user_id=$1 AND kind=$2 AND target=$3`, u.ID, kind, target)
	} else {
		_, e = a.community.DB.ExecContext(r.Context(), `INSERT INTO follows(user_id,kind,target) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, u.ID, kind, target)
	}
	if e != nil {
		a.internal(w, e)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *API) queryJSON(w http.ResponseWriter, r *http.Request, query string, args ...any) {
	rows, e := a.community.DB.QueryContext(r.Context(), query, args...)
	if e != nil {
		a.internal(w, e)
		return
	}
	defer rows.Close()
	cols, e := rows.Columns()
	if e != nil {
		a.internal(w, e)
		return
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if e = rows.Scan(dest...); e != nil {
			a.internal(w, e)
			return
		}
		row := map[string]any{}
		for i, c := range cols {
			if b, ok := values[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = values[i]
			}
		}
		out = append(out, row)
	}
	if e = rows.Err(); e != nil {
		a.internal(w, e)
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) adminAction(w http.ResponseWriter, r *http.Request) {
	s := a.community
	action := r.PathValue("action")
	var in struct {
		ID                                          int64
		Name, Bio, Avatar, Description, Role, Email string
		Suspended, Hidden                           bool
	}
	if !decodeCommunity(w, r, &in) {
		return
	}
	var e error
	switch action {
	case "member":
		e = s.Member(r.Context(), in.ID, in.Role, in.Suspended)
	case "invite":
		address, err := validEmail(in.Email)
		if err != nil {
			writeError(w, 400, "valid email required")
			return
		}
		var id int64
		e = s.DB.QueryRowContext(r.Context(), `INSERT INTO users(email,name,role) VALUES($1,'Editor','admin') ON CONFLICT DO NOTHING RETURNING id`, address).Scan(&id)
		if e == sql.ErrNoRows {
			writeError(w, 409, "account already exists; manage its role from Members")
			return
		}
		if e == nil {
			e = s.QueueAccount(r.Context(), id, address, "setup")
		}
	case "moderate":
		_, e = s.DB.ExecContext(r.Context(), `UPDATE comments SET hidden=$2 WHERE id=$1`, in.ID, in.Hidden)
	case "resolve":
		_, e = s.DB.ExecContext(r.Context(), `UPDATE reports SET resolved=true WHERE id=$1`, in.ID)
	case "author":
		if len(in.Name) < 1 || len(in.Name) > 100 || len(in.Bio) > 2000 || in.Avatar != "" && !safeImage(in.Avatar) {
			writeError(w, 400, "invalid author profile")
			return
		}
		_, e = s.DB.ExecContext(r.Context(), `INSERT INTO authors(name,bio,avatar) VALUES($1,$2,$3) ON CONFLICT(name) DO UPDATE SET bio=$2,avatar=$3`, in.Name, in.Bio, in.Avatar)
	case "topic":
		if len(in.Name) < 2 || len(in.Name) > 50 || len(in.Description) > 1000 {
			writeError(w, 400, "invalid topic")
			return
		}
		_, e = s.DB.ExecContext(r.Context(), `INSERT INTO topics(name,description) VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET description=$2`, in.Name, in.Description)
	case "retry-email":
		_, e = s.DB.ExecContext(r.Context(), `UPDATE email_outbox SET state='pending',attempts=0,available_at=now() WHERE id=$1 AND state='failed'`, in.ID)
	default:
		writeError(w, 404, "unknown management action")
		return
	}
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

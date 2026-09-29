package community

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const matches = `(s.scope='all' OR EXISTS(SELECT 1 FROM follows f WHERE f.user_id=u.id AND ((f.kind='author' AND f.target=p.author) OR (f.kind='topic' AND (f.target=p.category OR p.tags_json ? f.target)))))`

func (s *Service) Tick(ctx context.Context) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(32640003)`); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE posts SET status='published',published_at=now(),updated_at=now(),scheduled_at=NULL WHERE status='draft' AND scheduled_at<=now()`); e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, `SELECT slug,title,author,category FROM posts WHERE status='published' AND NOT EXISTS(SELECT 1 FROM publications WHERE slug=posts.slug)`)
	if e != nil {
		return e
	}
	type post struct{ slug, title, author, category string }
	var ps []post
	for rows.Next() {
		var p post
		if e = rows.Scan(&p.slug, &p.title, &p.author, &p.category); e != nil {
			rows.Close()
			return e
		}
		ps = append(ps, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, p := range ps {
		if _, e = tx.ExecContext(ctx, `INSERT INTO publications(slug) VALUES($1)`, p.slug); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO notifications(user_id,title,url,event_key) SELECT DISTINCT u.id,$2,$3,$4 FROM users u JOIN follows f ON f.user_id=u.id JOIN posts p ON p.slug=$1 WHERE u.verified AND NOT u.suspended AND ((f.kind='author' AND f.target=p.author) OR (f.kind='topic' AND (f.target=p.category OR p.tags_json ? f.target))) ON CONFLICT DO NOTHING`, p.slug, p.title, "/stories/"+p.slug, "publish:"+p.slug)
		if e != nil {
			return e
		}
		r, e := tx.QueryContext(ctx, `SELECT u.id,u.email,s.unsubscribe_hash FROM users u JOIN subscriptions s ON s.user_id=u.id JOIN posts p ON p.slug=$1 WHERE u.verified AND NOT u.suspended AND s.frequency='immediate' AND `+matches, p.slug)
		if e != nil {
			return e
		}
		type recipient struct {
			id           int64
			email, unsub string
		}
		var rs []recipient
		for r.Next() {
			var u recipient
			if e = r.Scan(&u.id, &u.email, &u.unsub); e != nil {
				r.Close()
				return e
			}
			rs = append(rs, u)
		}
		e = r.Err()
		r.Close()
		if e != nil {
			return e
		}
		for _, u := range rs {
			body := `<h1>` + html.EscapeString(p.title) + `</h1><p><a href="` + s.Config.URL + "/stories/" + p.slug + `">Read the story</a></p>` + s.emailFooter(u.unsub)
			_, e = tx.ExecContext(ctx, `INSERT INTO email_outbox(user_id,recipient,subject,html,event_key,kind) VALUES($1,$2,$3,$4,$5,'newsletter') ON CONFLICT DO NOTHING`, u.id, u.email, p.title, body, fmt.Sprintf("publication:%s:%d", p.slug, u.id))
			if e != nil {
				return e
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if e = s.digest(ctx, time.Now().UTC()); e != nil {
		return e
	}
	return s.deliver(ctx)
}
func (s *Service) emailFooter(hash string) string {
	return `<p><a href="` + s.Config.URL + `/settings">Manage preferences</a> · <a href="` + s.Config.URL + `/unsubscribe?token=` + hash + `">Unsubscribe</a></p>`
}
func (s *Service) digest(ctx context.Context, now time.Time) error {
	// Catch up a missed Friday run on restart; the week key makes this idempotent.
	friday := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
	days := (int(now.Weekday()) - int(time.Friday) + 7) % 7
	friday = friday.AddDate(0, 0, -days)
	if now.Before(friday) {
		friday = friday.AddDate(0, 0, -7)
	}
	key := friday.Format("2006-01-02")
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `INSERT INTO digest_runs(week) VALUES($1) ON CONFLICT DO NOTHING`, key)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil
	}
	rows, e := tx.QueryContext(ctx, `SELECT u.id,u.email,s.unsubscribe_hash,p.slug,p.title FROM users u JOIN subscriptions s ON s.user_id=u.id JOIN publications pub ON pub.first_at>$1 AND pub.first_at<=$2 JOIN posts p ON p.slug=pub.slug WHERE u.verified AND NOT u.suspended AND p.status='published' AND s.frequency='weekly' AND `+matches+` ORDER BY u.id,pub.first_at`, friday.AddDate(0, 0, -7), friday)
	if e != nil {
		return e
	}
	type digest struct {
		id                 int64
		email, unsub, body string
	}
	ds := map[int64]*digest{}
	for rows.Next() {
		var id int64
		var email, unsub, slug, title string
		if e = rows.Scan(&id, &email, &unsub, &slug, &title); e != nil {
			rows.Close()
			return e
		}
		d := ds[id]
		if d == nil {
			d = &digest{id: id, email: email, unsub: unsub, body: "<h1>Your week on Offscript</h1>"}
			ds[id] = d
		}
		d.body += `<p><a href="` + s.Config.URL + "/stories/" + slug + `">` + html.EscapeString(title) + `</a></p>`
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, d := range ds {
		_, e = tx.ExecContext(ctx, `INSERT INTO email_outbox(user_id,recipient,subject,html,event_key,kind,available_at) VALUES($1,$2,'Your week on Offscript',$3,$4,'newsletter',$5) ON CONFLICT DO NOTHING`, d.id, d.email, d.body+s.emailFooter(d.unsub), fmt.Sprintf("digest:%s:%d", key, d.id), friday)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Service) deliver(ctx context.Context) error {
	if !s.emailReady() {
		return nil
	}
	for i := 0; i < 20; i++ {
		var id, user int64
		var recipient, subject, body, key, kind string
		var attempts int
		e := s.DB.QueryRowContext(ctx, `UPDATE email_outbox SET state='sending',locked_at=now(),attempts=attempts+1 WHERE id=(SELECT id FROM email_outbox WHERE (state='pending' OR (state='sending' AND locked_at<now()-interval '5 minutes')) AND available_at<=now() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id,COALESCE(user_id,0),recipient,subject,html,event_key,kind,attempts`).Scan(&id, &user, &recipient, &subject, &body, &key, &kind, &attempts)
		if e == sql.ErrNoRows {
			return nil
		}
		if e != nil {
			return e
		}
		if kind == "newsletter" {
			var ok bool
			e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscriptions s JOIN users u ON u.id=s.user_id WHERE u.id=$1 AND u.verified AND NOT u.suspended AND s.frequency<>'off')`, user).Scan(&ok)
			if e != nil {
				return e
			}
			if !ok {
				_, e = s.DB.ExecContext(ctx, `UPDATE email_outbox SET state='cancelled' WHERE id=$1`, id)
				if e != nil {
					return e
				}
				continue
			}
		}
		msg := emailMessage{Recipient: recipient, Subject: subject, HTML: body, Key: key}
		if kind == "newsletter" {
			var unsub string
			if e = s.DB.QueryRowContext(ctx, `SELECT unsubscribe_hash FROM subscriptions WHERE user_id=$1`, user).Scan(&unsub); e != nil {
				return e
			}
			msg.Headers = map[string]string{"List-Unsubscribe": "<" + s.Config.URL + "/unsubscribe?token=" + unsub + ">", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}
		}
		deliveryErr := s.sendEmail(ctx, msg)
		success := deliveryErr == nil
		message := ""
		if deliveryErr != nil {
			message = deliveryErr.Error()
		}

		if success {
			_, e = s.DB.ExecContext(ctx, `UPDATE email_outbox SET state='sent',sent_at=now(),last_error='' WHERE id=$1`, id)
		} else {
			state := "pending"
			if attempts >= 8 {
				state = "failed"
			}
			delay := time.Minute * time.Duration(1<<min(attempts, 10))
			_, e = s.DB.ExecContext(ctx, `UPDATE email_outbox SET state=$2,last_error=$3,available_at=$4 WHERE id=$1`, id, state, message, time.Now().Add(delay))
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) Run(ctx context.Context, log *slog.Logger, invalidate func()) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if e := s.Tick(ctx); e != nil && !strings.Contains(e.Error(), "context canceled") {
			log.Error("worker failed", "error", e)
		}
		if invalidate != nil {
			invalidate()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) emailEndpoint() string {
	if s.EmailEndpoint != "" {
		return s.EmailEndpoint
	}
	return "https://api.resend.com/emails"
}
func (s *Service) emailClient() *http.Client {
	if s.EmailHTTP != nil {
		return s.EmailHTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

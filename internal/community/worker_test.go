package community

import (
	"context"
	"fmt"
	"github.com/M3264/blog-api-go/internal/storage"
	"github.com/M3264/blog-api-go/internal/testdb"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func service(t *testing.T) *Service {
	db, e := storage.Open(context.Background(), testdb.URL(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return &Service{DB: db.SQL(), Config: Config{URL: "http://example.test"}}
}
func TestDeliveryRetriesAndUnsubscribeCancellation(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	var id int64
	s.DB.QueryRow(`INSERT INTO users(email,name,verified) VALUES('recipient@example.test','Recipient',true) RETURNING id`).Scan(&id)
	s.DB.Exec(`INSERT INTO subscriptions(user_id,frequency,scope,unsubscribe_hash) VALUES($1,'immediate','all','unsub')`, id)
	s.DB.Exec(`INSERT INTO email_outbox(user_id,recipient,subject,html,event_key,kind) VALUES($1,'recipient@example.test','New story','<p>hello</p>','event-one','newsletter')`, id)
	calls := 0
	var firstKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		key := r.Header.Get("Idempotency-Key")
		if calls == 1 {
			firstKey = key
			w.WriteHeader(503)
		} else {
			if key != firstKey {
				t.Error("retry idempotency key changed")
			}
			w.Write([]byte(`{"id":"test"}`))
		}
	}))
	defer server.Close()
	s.Config.ResendKey = "test-provider-key"
	s.Config.EmailFrom = "Offscript <test@example.test>"
	s.EmailEndpoint = server.URL
	if e := s.deliver(ctx); e != nil {
		t.Fatal(e)
	}
	var state string
	var attempts int
	s.DB.QueryRow(`SELECT state,attempts FROM email_outbox WHERE event_key='event-one'`).Scan(&state, &attempts)
	if state != "pending" || attempts != 1 || calls != 1 {
		t.Fatalf("retry not deferred: %s %d %d", state, attempts, calls)
	}
	s.DB.Exec(`UPDATE email_outbox SET available_at=now()`)
	if e := s.deliver(ctx); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow(`SELECT state,attempts FROM email_outbox WHERE event_key='event-one'`).Scan(&state, &attempts)
	if state != "sent" || attempts != 2 || calls != 2 {
		t.Fatal("retry not delivered")
	}
	s.deliver(ctx)
	if calls != 2 {
		t.Fatal("sent email duplicated")
	}
	s.DB.Exec(`INSERT INTO email_outbox(user_id,recipient,subject,html,event_key,kind) VALUES($1,'recipient@example.test','New story','body','event-two','newsletter')`, id)
	s.DB.Exec(`UPDATE subscriptions SET frequency='off' WHERE user_id=$1`, id)
	s.deliver(ctx)
	s.DB.QueryRow(`SELECT state FROM email_outbox WHERE event_key='event-two'`).Scan(&state)
	if state != "cancelled" || calls != 2 {
		t.Fatal("unsubscribe did not cancel queued email")
	}
}
func TestWeeklyDigestMatchingTimingAndDedup(t *testing.T) {
	s := service(t)
	ctx := context.Background()
	friday := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	s.DB.Exec(`INSERT INTO users(email,name,verified) VALUES('weekly@example.test','Weekly',true),('empty@example.test','Empty',true)`)
	s.DB.Exec(`INSERT INTO subscriptions(user_id,frequency,scope,unsubscribe_hash) SELECT id,'weekly','following',email FROM users`)
	s.DB.Exec(`INSERT INTO follows(user_id,kind,target) SELECT id,'topic','Ideas' FROM users WHERE email='weekly@example.test'`)
	s.DB.Exec(`INSERT INTO posts(slug,title,summary,body,category,author,status,created_at,updated_at,published_at) VALUES('weekly-story','A weekly story','Useful summary','Long enough article body','Ideas','Avery','published',$1,$1,$1)`, friday.Add(-time.Hour))
	s.DB.Exec(`INSERT INTO publications(slug,first_at) VALUES('weekly-story',$1)`, friday.Add(-time.Hour))
	if e := s.digest(ctx, friday.Add(-time.Minute)); e != nil {
		t.Fatal(e)
	}
	var n int
	s.DB.QueryRow(`SELECT count(*) FROM email_outbox`).Scan(&n)
	if n != 0 {
		t.Fatal("digest sent before Friday 09 UTC")
	}
	if e := s.digest(ctx, friday); e != nil {
		t.Fatal(e)
	}
	if e := s.digest(ctx, friday.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow(`SELECT count(*) FROM email_outbox`).Scan(&n)
	if n != 1 {
		t.Fatalf("digest missing, duplicated, or sent without matching articles: %d", n)
	}
}
func TestBootstrapAndSuspension(t *testing.T) {
	s := service(t)
	link, e := s.Bootstrap(context.Background(), "initial@example.test")
	if e != nil || link == "" {
		t.Fatal(e)
	}
	var recipient, html string
	if err := s.DB.QueryRow(`SELECT recipient,html FROM email_outbox WHERE event_key LIKE 'setup:%'`).Scan(&recipient, &html); err != nil {
		t.Fatal(err)
	}
	if recipient != "initial@example.test" || !strings.Contains(html, link) {
		t.Fatal("bootstrap setup email does not match one-time link")
	}
	if _, e = s.Bootstrap(context.Background(), "another@example.test"); e == nil {
		t.Fatal("bootstrap permitted a second admin")
	}
	if e = s.Member(context.Background(), 1, "reader", false); e == nil {
		t.Fatal("last admin removed")
	}
	if e = s.Member(context.Background(), 1, "admin", true); e == nil {
		t.Fatal("last admin suspended")
	}
	_, e = s.DB.Exec(`INSERT INTO users(email,name,role,verified) VALUES('second@example.test','Second','admin',true)`)
	if e != nil {
		t.Fatal(e)
	}
	token, _ := s.Session(context.Background(), 1)
	if e = s.Member(context.Background(), 1, "admin", true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.User(context.Background(), token); e == nil {
		t.Fatal("suspended session retained access")
	}
	var n int
	s.DB.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=1`).Scan(&n)
	if n != 0 {
		t.Fatal(fmt.Sprintf("%d sessions survived suspension", n))
	}
}

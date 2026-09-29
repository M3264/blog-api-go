// Preview uses only the explicitly supplied isolated database, never BLOG_DATABASE_URL.
package main

import (
	"context"
	"fmt"
	"github.com/M3264/blog-api-go/internal/blog"
	"github.com/M3264/blog-api-go/internal/cache"
	"github.com/M3264/blog-api-go/internal/community"
	"github.com/M3264/blog-api-go/internal/httpapi"
	"github.com/M3264/blog-api-go/internal/storage"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	ctx := context.Background()
	db, e := storage.Open(ctx, os.Getenv("TEST_DATABASE_URL"))
	if e != nil {
		panic(e)
	}
	defer db.Close()
	svc := &community.Service{DB: db.SQL(), Config: community.Config{URL: "http://127.0.0.1:8098", MediaDir: "/tmp/offscript-preview-media"}}
	var n int
	db.SQL().QueryRow(`SELECT count(*) FROM posts`).Scan(&n)
	if n == 0 {
		for i, title := range []string{"A little room for a different kind of work", "The quiet joy of making something by hand", "What we find between the pages", "Ideas are better when we share them"} {
			now := time.Now().UTC()
			p := blog.Post{Title: title, Summary: []string{"Small experiments, slower mornings, and the space to ask a better question.", "Why the things we make help us see the world with fresh eyes.", "A reading habit is a conversation with a life you haven’t lived yet.", "Good conversations start with curiosity. Let’s make more space for them."}[i], Body: "Some days, the best thing we can do is leave a little room. Room for a question we haven’t answered yet. Room for a new approach, or a small experiment that might take us somewhere unexpected.\n\nWe spend a lot of time looking for the right answer. But there’s something to be said for paying attention to the things that make us curious. They’re often the beginning of a better story.\n\nThis is an invitation to notice those things. To share what you see, listen to someone else, and keep going with a little more curiosity than you had before.", Category: []string{"Work & life", "Making", "Reading", "Ideas"}[i], Author: "Avery Reed", CoverImage: fmt.Sprintf("http://127.0.0.1:8098/assets/%s.jpg", []string{"workspace", "writing", "library", "workspace"}[i]), Tags: []string{"Curiosity"}, Status: "published", CreatedAt: now, UpdatedAt: now, PublishedAt: &now}
			if _, e = db.Create(ctx, p); e != nil {
				panic(e)
			}
		}
		for _, u := range []struct{ email, name, role string }{{"reader@example.test", "Alex Reader", "reader"}, {"admin@example.test", "Avery Editor", "admin"}} {
			hash, _ := community.Password("offscript-preview-password")
			db.SQL().Exec(`INSERT INTO users(email,name,password_hash,role,verified) VALUES($1,$2,$3,$4,true)`, u.email, u.name, hash, u.role)
		}
	}
	c, e := cache.Open("redis://127.0.0.1:6379/0", "offscript-preview", slog.Default())
	if e != nil {
		panic(e)
	}
	defer c.Close()
	fmt.Println("Preview: http://127.0.0.1:8098 (fictional test accounts only)")
	http.ListenAndServe("127.0.0.1:8098", httpapi.NewWebsite(db, c, svc, "", slog.Default()))
}

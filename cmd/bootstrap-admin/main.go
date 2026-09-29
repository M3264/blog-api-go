package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/M3264/blog-api-go/internal/community"
	"github.com/M3264/blog-api-go/internal/config"
	"github.com/M3264/blog-api-go/internal/storage"
	"net/mail"
	"os"
	"strings"
)

func main() {
	email := flag.String("email", os.Getenv("BLOG_INITIAL_ADMIN_EMAIL"), "initial admin email (or BLOG_INITIAL_ADMIN_EMAIL)")
	flag.Parse()
	addr, e := mail.ParseAddress(*email)
	if e != nil || addr.Address != *email {
		fmt.Fprintln(os.Stderr, "provide a valid --email")
		os.Exit(1)
	}
	cfg, e := config.Load()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	db, e := storage.Open(context.Background(), cfg.DatabaseURL)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	defer db.Close()
	svc := community.Service{DB: db.SQL(), Config: cfg.Community}
	link, e := svc.Bootstrap(context.Background(), strings.ToLower(*email))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	fmt.Println(link)
}

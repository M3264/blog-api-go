package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/M3264/blog-api-go/internal/storage"
)

func main() {
	from := flag.String("from", "", "path to the old posts.json file")
	to := flag.String("database", os.Getenv("BLOG_DATABASE_URL"), "PostgreSQL connection URL")
	flag.Parse()
	if *from == "" {
		fmt.Fprintln(os.Stderr, "-from is required")
		os.Exit(2)
	}
	if *to == "" {
		fmt.Fprintln(os.Stderr, "-database or BLOG_DATABASE_URL is required")
		os.Exit(2)
	}
	db, err := storage.Open(context.Background(), *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	data, err := os.ReadFile(*from)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	n, err := db.ImportJSON(context.Background(), data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Imported %d posts; source JSON was left unchanged.\n", n)
}

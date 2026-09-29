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
	to := flag.String("to", "data/blog.db", "path to the SQLite database")
	flag.Parse()
	if *from == "" {
		fmt.Fprintln(os.Stderr, "-from is required")
		os.Exit(2)
	}
	db, err := storage.Open(context.Background(), *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	n, err := db.ImportJSON(context.Background(), *from)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Imported %d posts; source JSON was left unchanged.\n", n)
}

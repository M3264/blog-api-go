package main

import (
	"context"
	"github.com/M3264/blog-api-go/internal/community"
	"github.com/M3264/blog-api-go/internal/config"
	"github.com/M3264/blog-api-go/internal/storage"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg, e := config.Load()
	if e != nil {
		slog.Error("configuration", "error", e)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, e := storage.Open(ctx, cfg.DatabaseURL)
	if e != nil {
		slog.Error("database", "error", e)
		os.Exit(1)
	}
	defer db.Close()
	svc := community.Service{DB: db.SQL(), Config: cfg.Community}
	svc.Run(ctx, slog.Default(), nil)
}

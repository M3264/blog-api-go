package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/M3264/blog-api-go/internal/cache"
	"github.com/M3264/blog-api-go/internal/community"
	"github.com/M3264/blog-api-go/internal/config"
	"github.com/M3264/blog-api-go/internal/httpapi"
	"github.com/M3264/blog-api-go/internal/storage"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	cacheClient, err := cache.Open(cfg.RedisURL, cfg.CachePrefix, logger)
	if err != nil {
		return err
	}
	defer cacheClient.Close()
	svc := &community.Service{DB: db.SQL(), Config: cfg.Community}
	if cfg.RedisURL != "" {
		opts, e := redis.ParseURL(cfg.RedisURL)
		if e != nil {
			return e
		}
		svc.Redis = redis.NewClient(opts)
		defer svc.Redis.Close()
	}
	go svc.Run(ctx, logger, cacheClient.Invalidate)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewWebsite(db, cacheClient, svc, cfg.AdminToken, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	logger.Info("server started", "addr", cfg.Addr)
	select {
	case err := <-serverErrors:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("shutting down")
		return server.Shutdown(shutdownCtx)
	}
}

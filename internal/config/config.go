package config

import (
	"errors"
	"os"
)

type Config struct {
	Addr          string
	DBPath        string
	AdminToken    string
	AllowedOrigin string
}

func Load() (Config, error) {
	c := Config{
		Addr:          env("BLOG_ADDR", "127.0.0.1:8080"),
		DBPath:        env("BLOG_DB_PATH", "data/blog.db"),
		AdminToken:    os.Getenv("BLOG_ADMIN_TOKEN"),
		AllowedOrigin: os.Getenv("BLOG_ALLOWED_ORIGIN"),
	}
	if len(c.AdminToken) < 32 {
		return Config{}, errors.New("BLOG_ADMIN_TOKEN must be at least 32 characters")
	}
	return c, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

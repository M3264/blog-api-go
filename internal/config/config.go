package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Addr                 string
	DatabaseURL          string
	RedisURL             string
	CachePrefix          string
	AdminToken           string
	AllowedOrigin        string
	EnablePlayground     bool
	AllowShortAdminToken bool
}

func Load() (Config, error) {
	c := Config{
		Addr:          env("BLOG_ADDR", "127.0.0.1:8080"),
		DatabaseURL:   os.Getenv("BLOG_DATABASE_URL"),
		RedisURL:      os.Getenv("BLOG_REDIS_URL"),
		CachePrefix:   env("BLOG_CACHE_PREFIX", "blog-api"),
		AdminToken:    os.Getenv("BLOG_ADMIN_TOKEN"),
		AllowedOrigin: os.Getenv("BLOG_ALLOWED_ORIGIN"),
	}
	if raw := os.Getenv("BLOG_ENABLE_PLAYGROUND"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, errors.New("BLOG_ENABLE_PLAYGROUND must be true or false")
		}
		c.EnablePlayground = enabled
	}
	if raw := os.Getenv("BLOG_ALLOW_SHORT_ADMIN_TOKEN"); raw != "" {
		allowed, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, errors.New("BLOG_ALLOW_SHORT_ADMIN_TOKEN must be true or false")
		}
		c.AllowShortAdminToken = allowed
	}
	if len(c.AdminToken) < 32 && !c.AllowShortAdminToken {
		return Config{}, errors.New("BLOG_ADMIN_TOKEN must be at least 32 characters")
	}
	if c.AdminToken == "" {
		return Config{}, errors.New("BLOG_ADMIN_TOKEN is required")
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("BLOG_DATABASE_URL is required")
	}
	return c, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

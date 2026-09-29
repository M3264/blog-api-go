package config

import (
	"errors"
	"github.com/M3264/blog-api-go/internal/community"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Community            community.Config
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
	legacy := os.Getenv("BLOG_LEGACY_ADMIN") == "true"
	c.Community = community.Config{URL: strings.TrimRight(env("BLOG_SITE_URL", "http://localhost:8080"), "/"), ResendKey: os.Getenv("RESEND_API_KEY"), EmailFrom: os.Getenv("BLOG_EMAIL_FROM"), GoogleID: os.Getenv("GOOGLE_CLIENT_ID"), GoogleSecret: os.Getenv("GOOGLE_CLIENT_SECRET"), MediaDir: env("BLOG_MEDIA_DIR", "data/media"), Legacy: legacy}
	c.Community.SMTPAddr = os.Getenv("BLOG_SMTP_ADDR")
	c.Community.SMTPMode = env("BLOG_SMTP_MODE", "starttls")
	c.Community.SMTPUser = os.Getenv("BLOG_SMTP_USER")
	c.Community.SMTPPassword = os.Getenv("BLOG_SMTP_PASSWORD")
	if c.Community.SMTPAddr != "" {
		host, _, e := net.SplitHostPort(c.Community.SMTPAddr)
		if e != nil {
			return Config{}, errors.New("BLOG_SMTP_ADDR must be host:port")
		}
		mode := c.Community.SMTPMode
		if mode != "starttls" && mode != "tls" && mode != "local" {
			return Config{}, errors.New("BLOG_SMTP_MODE must be starttls, tls, or local")
		}
		if mode == "local" && host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return Config{}, errors.New("local SMTP mode requires a loopback address")
		}
	}
	c.Community.Secure = strings.HasPrefix(c.Community.URL, "https://")
	u, e := url.Parse(c.Community.URL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Config{}, errors.New("BLOG_SITE_URL must be an HTTP(S) origin")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback() {
		return Config{}, errors.New("public BLOG_SITE_URL must use HTTPS")
	}
	if legacy && len(c.AdminToken) < 32 && !c.AllowShortAdminToken {
		return Config{}, errors.New("BLOG_ADMIN_TOKEN must be at least 32 characters")
	}
	if legacy && c.AdminToken == "" {
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

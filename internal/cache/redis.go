package cache

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

const ttl = 30 * time.Second
const maxCachedBody = 1 << 20

type Cache struct {
	client *redis.Client
	prefix string
	log    *slog.Logger
}

func Open(redisURL, prefix string, logger *slog.Logger) (*Cache, error) {
	if redisURL == "" {
		return nil, nil
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	if prefix == "" {
		prefix = "blog-api"
	}
	return &Cache{client: redis.NewClient(options), prefix: prefix, log: logger}, nil
}

func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	return c.client.Close()
}

func (c *Cache) Invalidate() {
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := c.client.Incr(ctx, c.prefix+":version").Err(); err != nil {
		c.log.Warn("cache invalidation failed; entries expire within 30 seconds", "error", err)
	}
}

func (c *Cache) Public(next http.HandlerFunc) http.HandlerFunc {
	if c == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || len(r.URL.RequestURI()) > 2048 {
			next(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
		version, err := c.client.Get(ctx, c.prefix+":version").Result()
		cancel()
		if errors.Is(err, redis.Nil) {
			version = "0"
		} else if err != nil {
			next(w, r)
			return
		}
		key := c.prefix + ":public:" + version + ":" + r.URL.RequestURI()
		ctx, cancel = context.WithTimeout(r.Context(), 300*time.Millisecond)
		body, err := c.client.Get(ctx, key).Bytes()
		cancel()
		if err == nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
		out := &captureWriter{ResponseWriter: w, status: 200}
		next(out, r)
		if out.status != 200 || out.overflow {
			return
		}
		ctx, cancel = context.WithTimeout(r.Context(), 300*time.Millisecond)
		if err := c.client.Set(ctx, key, out.body.Bytes(), ttl).Err(); err != nil && !errors.Is(err, context.Canceled) {
			c.log.Debug("cache write failed", "error", err)
		}
		cancel()
	}
}

type captureWriter struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	overflow bool
}

func (w *captureWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(data []byte) (int, error) {
	if !w.overflow {
		if w.body.Len()+len(data) > maxCachedBody {
			w.overflow = true
			w.body.Reset()
		} else {
			_, _ = w.body.Write(data)
		}
	}
	return w.ResponseWriter.Write(data)
}

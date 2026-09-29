# Blog API

A Go backend for publishing articles. Editors create drafts, publish articles, and organize them with categories, tags, and featured flags. Readers can search, filter, and find related articles. The API starts with no content.

## Architecture

| Path | Responsibility |
| --- | --- |
| `cmd/api` | Server startup and graceful shutdown |
| `cmd/migrate-json` | One-time, repeatable import from the earlier JSON store |
| `internal/blog` | Post model and validation |
| `internal/httpapi` | HTTP routes, authentication, middleware |
| `internal/storage` | PostgreSQL repository and embedded schema migrations |
| `internal/cache` | Redis cache for published reads |
| `openapi.yaml` | Machine readable API contract |
| `Dockerfile`, `compose.yaml` | API, PostgreSQL, and Redis deployment |
| `.github/workflows/ci.yml` | Format, test, vet, and build gates |

PostgreSQL is the source of truth. Redis caches successful public reads for 30 seconds; writes increment a shared cache version so all API instances stop using old entries. If Redis is unavailable, reads fall through to PostgreSQL. Use a reverse proxy for HTTPS and back up the PostgreSQL volume.

## Run locally

Requires Go 1.25 or newer and a PostgreSQL database. Redis is optional for local development.

```bash
export BLOG_ADMIN_TOKEN="$(openssl rand -hex 32)"
export BLOG_DATABASE_URL='postgres://blog:password@127.0.0.1:5432/blog?sslmode=disable'
export BLOG_REDIS_URL='redis://127.0.0.1:6379/0'
go run ./cmd/api
```

The API listens at `http://127.0.0.1:8080`. To build a binary:

```bash
go build -o blog-api-go ./cmd/api
./blog-api-go
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `BLOG_ADMIN_TOKEN` | required | At least 32 characters; bearer token for editor routes |
| `BLOG_ADDR` | `127.0.0.1:8080` | Listening address |
| `BLOG_DATABASE_URL` | required | PostgreSQL connection URL |
| `BLOG_REDIS_URL` | empty | Redis connection URL; empty disables cache |
| `BLOG_CACHE_PREFIX` | `blog-api` | Redis key namespace |
| `BLOG_ALLOWED_ORIGIN` | empty | Exact browser origin allowed by CORS, such as `https://blog.example.com` |

The token is read at startup. Change it and restart the server to rotate it. Protect admin requests with HTTPS when the API is reachable outside localhost.

## Run with Docker Compose

Create `.env` from `.env.example`, set `BLOG_ADMIN_TOKEN` and `BLOG_DB_PASSWORD` to newly generated values, then run:

```bash
docker compose up --build -d
docker compose ps
```

Compose binds only the API to `127.0.0.1:8080` on the host and stores PostgreSQL data in the `postgres-data` volume. PostgreSQL and Redis are reachable only on the Compose network. Put your HTTPS reverse proxy in front of the API port. The API container runs as a nonroot user with a read-only root filesystem and exposes `/ready` to its health check.

## API

The full request and response contract is in [openapi.yaml](openapi.yaml). All responses are JSON except successful delete (204). Errors use `{"error":"message"}`. Public endpoints never return drafts.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Process health |
| `GET` | `/ready` | Database readiness |
| `GET` | `/posts` | Published posts; supports search and filters |
| `GET` | `/posts/{slug}` | Published article |
| `GET` | `/posts/{slug}/related` | Related published articles |
| `GET` | `/categories` | Published article counts by category |
| `GET` | `/tags` | Published article counts by tag |
| `GET` | `/admin/posts` | Editor list, including drafts |
| `GET` | `/admin/posts/{slug}` | Editor article detail |
| `POST` | `/admin/posts` | Create a draft or published article |
| `PATCH` | `/admin/posts/{slug}` | Edit, publish, or unpublish |
| `DELETE` | `/admin/posts/{slug}` | Permanently delete |

`GET /posts` accepts `q`, `category`, `tag`, `featured=true|false`, `limit` (1–50, default 10), and `offset` (default 0). Filters can be combined. `GET /admin/posts` accepts `status=draft|published`, plus the same search and pagination filters. `GET /posts/{slug}/related` accepts `limit` and `offset`. List responses include `posts`, `total`, `limit`, and `offset`.

Admin routes require `Authorization: Bearer <BLOG_ADMIN_TOKEN>`.

### Create and publish an article

```bash
curl -X POST http://127.0.0.1:8080/admin/posts \
  -H "Authorization: Bearer $BLOG_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Planning your week","summary":"Practical ways to organize your time.","body":"Start by writing down deadlines, then reserve time for focused work, breaks, and review.","category":"Guides","author":"Editorial Team","tags":["Planning","Productivity"],"featured":true}'

curl -X PATCH http://127.0.0.1:8080/admin/posts/planning-your-week \
  -H "Authorization: Bearer $BLOG_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"published"}'
```

Then query `GET /posts?category=Guides&featured=true`. The server generates a stable slug and publication timestamp. Article bodies are plain text; a web client should render them as text or sanitize any formatting it adds.

## Import existing JSON posts

If an earlier version saved `data/posts.json`, copy or back it up and import it before switching traffic:

```bash
BLOG_DATABASE_URL='postgres://blog:password@127.0.0.1:5432/blog?sslmode=disable' \
  go run ./cmd/migrate-json -from data/posts.json
```

For the Compose deployment, where PostgreSQL is not exposed to the host:

```bash
docker compose run --rm -v "$PWD/data/posts.json:/tmp/posts.json:ro" \
  api /usr/local/bin/migrate-json -from /tmp/posts.json
```

The import runs in a transaction, skips slugs already in PostgreSQL, and leaves the source JSON untouched. It is safe to rerun. Verify the imported count with `GET /admin/posts` before switching traffic. If you need to roll back, stop the new server and run the old version against the original JSON file; edits made only in PostgreSQL will need to be exported separately.

## Operations and verification

```bash
go test ./...
go vet ./...
go build -o blog-api-go ./cmd/api
```

Set `TEST_DATABASE_URL` and `TEST_REDIS_URL` to run integration tests against isolated PostgreSQL schemas and a namespaced Redis cache. CI starts both services and runs the full suite. Use `pg_dump` or a managed PostgreSQL backup for the database. Redis is disposable cache data. The API logs structured request records without logging the admin token.

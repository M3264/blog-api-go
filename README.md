# Blog API

A Go backend for publishing articles. Editors create drafts, publish articles, and organize them with categories, tags, and featured flags. Readers can search, filter, and find related articles. The API starts with no content.

## Architecture

| Path | Responsibility |
| --- | --- |
| `cmd/api` | Server startup and graceful shutdown |
| `cmd/migrate-json` | One-time, repeatable import from the earlier JSON store |
| `internal/blog` | Post model and validation |
| `internal/httpapi` | HTTP routes, authentication, middleware |
| `internal/storage` | SQLite repository and embedded schema migrations |
| `openapi.yaml` | Machine readable API contract |
| `Dockerfile`, `compose.yaml` | Single-server container deployment |
| `.github/workflows/ci.yml` | Format, test, vet, and build gates |

SQLite runs in WAL mode with a busy timeout. This deployment is for **one API instance** with a persistent local volume. Use a reverse proxy for HTTPS, keep the database volume backed up, and do not run multiple replicas against separate copies of the file.

## Run locally

Requires Go 1.22 or newer and a C compiler for the SQLite driver.

```bash
export BLOG_ADMIN_TOKEN="$(openssl rand -hex 32)"
go run ./cmd/api
```

The API listens at `http://127.0.0.1:8080` and stores data in `data/blog.db`. To build a binary:

```bash
go build -o blog-api-go ./cmd/api
BLOG_ADMIN_TOKEN='your-secret-of-at-least-32-characters' ./blog-api-go
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `BLOG_ADMIN_TOKEN` | required | At least 32 characters; bearer token for editor routes |
| `BLOG_ADDR` | `127.0.0.1:8080` | Listening address |
| `BLOG_DB_PATH` | `data/blog.db` | SQLite database path |
| `BLOG_ALLOWED_ORIGIN` | empty | Exact browser origin allowed by CORS, such as `https://blog.example.com` |

The token is read at startup. Change it and restart the server to rotate it. Protect admin requests with HTTPS when the API is reachable outside localhost.

## Run with Docker Compose

Create `.env` from `.env.example`, set `BLOG_ADMIN_TOKEN` to a newly generated value, then run:

```bash
docker compose up --build -d
docker compose ps
```

Compose binds the API to `127.0.0.1:8080` on the host and stores the database in the `blog-data` volume. Put your HTTPS reverse proxy in front of that port. The container runs as a nonroot user, has a read-only root filesystem, and exposes `/ready` to its health check.

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
go run ./cmd/migrate-json -from data/posts.json -to data/blog.db
```

The import runs in a transaction, skips slugs already in the database, and leaves the source JSON untouched. It is safe to rerun. Verify the imported count with `GET /admin/posts` before using the new server. If you need to roll back, stop the new server and run the old version against the original JSON file; edits made only in SQLite will need to be exported separately.

## Operations and verification

```bash
go test ./...
go vet ./...
go build -o blog-api-go ./cmd/api
```

Back up the SQLite database with SQLite's online backup API or `VACUUM INTO` while the server is live; copying only the `.db` file during WAL activity can miss recent changes. Keep the `-wal` and `-shm` files with the database if taking a filesystem snapshot. The API logs structured request records without logging the admin token.

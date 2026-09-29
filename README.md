# Offscript

A Go blog and reader community with PostgreSQL as the authoritative store and Redis for public article caching and rate limits. Go serves complete HTML pages and embedded Vite assets; production needs one application service, PostgreSQL, and Redis.

Public reading and the existing article API remain available without login. Verified readers can comment/reply, like, save stories privately, follow authors/topics, read their feed, manage email preferences, and receive notifications. Verified admins manage posts, author profiles, topics, members, moderation, and email delivery. Reader accounts cannot publish.

## Run

Copy `.env.example` to a private `.env` and set the database password and site origin. Configure SMTP (or Resend) and Google OAuth for real account email and Google sign-in. Start PostgreSQL/Redis and build the service:

```sh
docker compose up -d --build
```

Local development with Go/Node:

```sh
npm ci
npm run typecheck
npm run build
docker compose -f compose.yaml -f compose.local.yaml up -d postgres redis
# Supply BLOG_DATABASE_URL and BLOG_REDIS_URL through the environment.
go run ./cmd/api
```

`BLOG_SITE_URL` is the canonical origin; public origins must use HTTPS. Session cookies are Secure on HTTPS, HttpOnly, and SameSite=Lax. Passwords use Argon2id. Sessions and expiring one-time tokens are stored hashed. Mutating session requests require the CSRF token from `/api/me` (the website reads it from a meta tag). Redis rate limiting falls back to PostgreSQL during Redis outages. Private responses are never cached.

## Administration

```sh
docker compose run --rm api bootstrap-admin --email '<your initial admin email>'
```

The email flag defaults to private `BLOG_INITIAL_ADMIN_EMAIL` when set. The command also queues the setup link for email delivery. Use the printed or emailed private setup link within one hour. After password setup, sign in and use `/admin`. Additional admins are invited from Members. The last active admin is protected from removal/suspension. `BLOG_LEGACY_ADMIN=false` disables shared bearer administration, including the old browser token. The production website never serves the API console or playground navigation; legacy page URLs redirect to website pages.

The Tiptap editor supports headings, lists, quotes, safe links, images, preview, autosaved drafts, server-side revisions, restoration as a draft, scheduling in UTC, and publish/unpublish. Structured content is validated against an allowlist and its plain-text equivalent remains in `body`. Existing slugs are preserved on edits. Image uploads accept JPEG, PNG and WebP up to 10 MB/40 megapixels, resize to 2400 pixels per side, and reencode as JPEG in the persistent media directory.

## API

[OpenAPI](openapi.yaml) documents public, account, community, upload, and management endpoints.

| Routes | Access |
| --- | --- |
| `/posts`, `/posts/{slug}`, `/posts/{slug}/related`, `/categories`, `/tags` | Public, Redis cached |
| `/stories/{slug}`, `/`, `/search`, `/topics`, `/authors` | Public HTML |
| `/api/auth/{action}` | Registration, login, verification/setup, recovery |
| `/api/me`, `/api/account/{action}` | Signed-in account, private |
| `/api/stories/{slug}/{action}`, `/api/comments/{id}`, `/api/follows/{kind}/{target}` | Verified readers, CSRF on mutations |
| `/api/feed`, `/api/bookmarks`, `/api/follows`, `/api/notifications`, `/api/subscription` | Private, scoped to current user |
| `/admin/posts`, `/admin/posts/{slug}` | Verified admin article management API |
| `/api/admin/*` | Verified admin management, moderation, revisions, uploads |
| `/auth/google`, `/auth/google/callback` | Google OIDC with state, nonce, PKCE and signed ID-token validation |
| `/rss.xml`, `/sitemap.xml` | Public XML |

Public post list filters retain `q`, `category`, `tag`, `featured`, `limit`, and `offset`; only published posts are returned. The original JSON import command remains idempotent:

```sh
go run ./cmd/migrate-json --from data/posts.json
```

Author records represent bylines independently of reader login accounts. Google never links accounts solely by matching email; a signed-in linking flow is required. Suspension revokes sessions and prevents password/Google sign-in and community actions.

## Email and Google configuration

`BLOG_EMAIL_FROM` controls the sender independently of the admin email. For the host-local Postfix relay, use `BLOG_SMTP_ADDR=127.0.0.1:25` and `BLOG_SMTP_MODE=local`. That relay listens only on loopback and signs mail with OpenDKIM. Remote relays require `starttls` (default) or `tls` plus `BLOG_SMTP_USER`/`BLOG_SMTP_PASSWORD` if authentication is required. SMTP takes precedence over Resend; leaving its address blank uses `RESEND_API_KEY`. See [SMTP setup](docs/smtp.md) for deployment boundaries and DNS requirements.

Create a project in [Google Cloud](https://console.cloud.google.com/), configure Google Auth Platform Branding/Audience, then create a Web application client under Clients. Register `https://blog.kennyy.tech/auth/google/callback` exactly as an authorized redirect URI. Store its client ID and secret privately as `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`. Testing audiences must include the users who will sign in; configure a production audience before public launch. Offscript requests only `openid email profile`; Google account linking requires an existing signed-in account. [Google instructions](https://developers.google.com/identity/openid-connect/openid-connect).

## Worker, storage, rollout

The API runs scheduled publishing and a PostgreSQL-backed durable outbox worker every 30 seconds. `go run ./cmd/worker` supports a dedicated worker process. Immediate publication emails are deduplicated by article/recipient; edits do not resend them. Friday 09:00 UTC digests match all stories or followed authors/topics and skip empty results. Notification badges refresh once a minute while the tab is visible. Subscriptions and bookmarks are private.

Back up PostgreSQL and uploaded media together. Redis is disposable. [Rollout instructions](docs/rollout.md) cover configuration, bootstrap, backup/restore, live verification, disabling token access, and rollback. Live launch requires actual email delivery, Google console configuration and the user-supplied initial admin email.

## Verification

```sh
npm run typecheck
npm run build
TEST_DATABASE_URL='postgres://…' TEST_REDIS_URL='redis://127.0.0.1:6379/0' go test ./...
go vet ./...
go build ./cmd/api
```

Database integration tests create isolated schemas and clean up after themselves. They cover the retained API and legacy import, accounts, permissions, CSRF, recovery/revocation, OIDC linking/nonce/state, private-data isolation, moderation, replies, scheduling, revisions, hostile content, uploads, newsletter matching, retries and deduplication. Redis cache tests use a random namespace.

`tests/preview.go` is an explicitly isolated fictional-data preview helper. With that preview running, `npm run test:browser` exercises guest, reader and admin journeys at desktop/mobile sizes and captures screenshots. Never run the preview helper against the production schema. Browser tests cover UI behavior; mocked provider tests do not prove live Google/mail configuration.

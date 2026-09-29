# Offscript rollout

Offscript is live at `https://blog.kennyy.tech`. The administrator confirmed password and Google sign-in, and the database records a verified admin, linked Google identity and active session. The normal public upstream now uses the account-based site. PostgreSQL migrations, admin provisioning and real Resend delivery are complete. The rollout preview service is stopped and disabled; the original service/binary/config and private backups are retained for rollback.


## Required private configuration

- `BLOG_SITE_URL=https://blog.kennyy.tech`
- `BLOG_EMAIL_FROM` and either `BLOG_SMTP_ADDR`/`BLOG_SMTP_MODE` (plus credentials for an authenticated remote relay) or `RESEND_API_KEY`. See [SMTP setup](smtp.md).
- `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`; authorize the exact callback `https://blog.kennyy.tech/auth/google/callback` and configure the consent screen for public users.
- The user-supplied initial admin email is saved privately as `BLOG_INITIAL_ADMIN_EMAIL`. The bootstrap command defaults to it; do not publish this value.
- `BLOG_DATABASE_URL`, `BLOG_REDIS_URL`, `BLOG_MEDIA_DIR`. Keep secrets in a private environment file or deployment secret store, outside Git.

Keep `BLOG_LEGACY_ADMIN=false` after the account login is verified. The new binary disables bearer administration by default; only an explicitly enabled transition deployment supports it. Public registration always creates readers. The old Go `New` constructor remains for legacy API regression tests; production uses `NewWebsite` and never mounts the playground or console.

## Before switching traffic

1. Capture a database backup and existing media. `ops/backup.sh` covers the Compose PostgreSQL public schema and media volume. Inspect `pg_restore --list` and test restoration into an isolated database. Do not restore over the live database to test a backup.
2. Build the Vite bundle and Go binary, or build the multi-stage container. Additive migration `0002_community.sql` is applied transactionally on database open under an advisory lock. It preserves existing posts, dates, slugs, bylines and body; it backfills author/topic profiles and publication markers so launch does not email the archive.
3. Bootstrap privately: `docker compose run --rm api bootstrap-admin --email '<supplied email>'`. This prints a one-hour single-use setup URL and queues the same link for delivery through the durable account outbox. Open it privately, choose a password, and verify admin access. Bootstrap refuses to create another admin after the first; later invitations require a verified signed-in admin.
4. Complete password sign-in, a real verification/recovery email, and authenticated Google linking/sign-in on the staging origin. The local mocked-provider tests do not establish actual Google console configuration or mail deliverability.
5. Set `BLOG_LEGACY_ADMIN=false`, restart the account-based service, verify token admin access is denied and account admin access remains available, then switch the reverse proxy for `blog.kennyy.tech` to the tested service.
6. Verify a guest story, a reader interaction, an admin draft/upload, live email delivery, Google sign-in, `/ready`, RSS, sitemap, and Redis public cache hits. Retain backups and the previous binary/image until the launch is accepted.

The API runs the PostgreSQL worker every 30 seconds. `blog-worker` also supports a dedicated process; transaction locks and job claims allow concurrent instances. Redis data is disposable. Media is persistent and must be backed up with PostgreSQL. A host systemd deployment currently has `ProtectHome=read-only`; explicitly add a `ReadWritePaths=` entry for the configured media directory before enabling uploads. The Compose deployment provisions a writable media volume owned by UID 10001 while keeping its root filesystem read-only.

## Rollback

Stop the new worker/API, restore the previous binary/image and proxy target. The old article API can continue against the additive schema; leave the new tables in place to preserve accounts and interactions. Do not roll back by deleting tables or restoring the prelaunch dump after readers have started writing. A full database restore is disaster recovery and requires a deliberate plan for writes made since the backup.

## Email behavior

New publications are tracked once by slug. Edits and republishing do not resend the publication message. Each recipient/event has a unique durable outbox key; delivery uses the same Resend idempotency key or SMTP Message-ID on retry. SMTP acceptance means the relay accepted responsibility for delivery. A stable Message-ID does not guarantee recipient-side deduplication after an ambiguous DATA acknowledgement. Failed delivery retries with exponential delays and stops after eight attempts. Admins can retry failed jobs. Provider idempotency has a finite retention window; an ambiguous delivery retried after that window requires checking provider status before retrying. The dashboard reports provider/relay acceptance (`sent`), not inbox placement or bounce events.

Friday 09:00 UTC digests include only new matching publications in the preceding week and skip empty digests. A restart catches up the latest Friday run. Unsubscribe cancels queued newsletters at delivery time; transactional security emails continue. Email preference and unsubscribe links are included, along with one-click unsubscribe headers.

## Local rollout evidence (2026-09-29)

A private prelaunch dump of the live PostgreSQL public schema was created and restored into a disposable database. The additive migration ran against that restoration. All 12 original posts matched a fingerprint over the original article columns before and after migration; three author records were backfilled. The disposable database was removed after verification. The live schema was not migrated or switched. Backups are excluded from Git.

Final local checks passed: the complete PostgreSQL/Redis integration suite with Go's race detector (`go test -race -p 1 ./...`), Go vet, API/bootstrap/worker binary builds, TypeScript checking, Vite production bundling, Compose configuration validation, OpenAPI parsing, and desktop/mobile guest/reader/admin browser journeys. The independent UI reviewer scored all four requested corrections resolved. The production Docker image has not been built on this disk-constrained host; build and smoke-test it before launch. Resend email delivery has now been verified; real Google sign-in is now verified. Host-local Postfix/OpenDKIM are now installed, listening only on loopback. A reserved-domain test message was accepted and DKIM-signed, then its exact queue item was removed; no real recipient email was sent. App SMTP tests cover MIME, stable retry identifiers, failed acceptance retries, TLS downgrade rejection and header injection. Direct outbound SMTP port 25 timed out against two Gmail MX hosts; port 587 to Gmail was reachable. The mail A, MX, SPF and DMARC records have since been published and verified; DKIM matches the server key and `opendkim-testkey` passes. Reverse DNS/PTR remains absent and the repeated outbound port-25 tests still time out. The user subsequently selected Resend. Its domain is verified, the private SMTP override is cleared, and a setup test from the configured sender to the supplied admin recipient was reported `delivered` by Resend. Google credentials are saved privately, and the administrator has since verified real sign-in/linking; the account-based site is now the public default.


## Account rollout preview (2026-09-29)

`offscript-preview.service` runs the tested binary on loopback port 8099 using real PostgreSQL/Redis, Resend and Google configuration. Runtime overrides come from private `data/rollout.env`, loaded after `.env` because systemd environment files override `Environment=` entries. Uploaded media is writable through an explicit `ReadWritePaths` allowance. Public visitors continue on the original service at port 8080. The `/account/setup` route opts the browser into preview; all subsequent pages, assets and OAuth requests in that browser use 8099. The setup token is excluded from Nginx access logs. Reviewed configuration sources are in `ops/offscript-preview*`.

A fresh private backup includes the database, original Nginx configuration/service/binary, and a preservation fingerprint. Public HTTPS checks passed for setup/login pages, the secure preview cookie, no-store private responses, denial of guest account API access, Vite assets, and the live Google authorization redirect with the expected callback/scopes/PKCE. Actual password setup and Google linking/sign-in require the administrator's browser; they have not been automated. Verify those before changing the default upstream for everyone. The bootstrap command refuses a second initial admin; an expired setup link can be recovered using the password recovery flow in the same preview browser.


## Live launch verification (2026-09-29)

The production `blog-api-go.service` runs `bin/offscript-api` on loopback port 8080. Reviewed service and proxy sources are in `ops/offscript.service` and `ops/offscript-nginx.conf`. The private `data/production.env` overrides the port, media directory, public cache prefix and disabled legacy administration. Nginx routes all browsers directly to this service; the preview cookie no longer affects routing. The preview service is disabled.

Public HTTPS checks passed for home, registration/login, search, topics, authors, all twelve existing article URLs and their JSON API entries, Vite assets, RSS and sitemap XML, Redis cache hits, guest denial on private APIs, legacy bearer denial on admin APIs, unavailable API console assets and playground redirection. The original article text, bylines, slugs, creation/publication dates and metadata survived migration. A subsequent admin editor revision changed one cover and its update timestamp; that intentional edit is preserved. Post-verification database and media backups include the verified account. Previous race/browser checks and the administrator's real password/Google journeys establish the rollout evidence; no fake accounts were created in production.

Rebuildable Go, npm and apt caches were cleared at the user's request. Backups, PostgreSQL, media and the rollback binary were retained. The production Docker image is not required by this host systemd deployment and remains unbuilt; validate it separately before changing deployment mode.

#!/usr/bin/env bash
set -euo pipefail
umask 077
backup_root="${OFFSCRIPT_BACKUP_DIR:-backups/$(date -u +%Y%m%d-%H%M%S)}"
mkdir -p "$backup_root"
docker compose exec -T postgres pg_dump -U blog -d blog --schema=public --format=custom > "$backup_root/postgres.dump"
docker compose exec -T postgres pg_restore --list < "$backup_root/postgres.dump" > "$backup_root/manifest.txt"
# An existing deployment may not yet have the additive media volume.
if docker compose exec -T api test -d /app/media 2>/dev/null; then
  docker compose exec -T api tar -C /app/media -czf - . > "$backup_root/media.tar.gz"
elif [ -d "${BLOG_MEDIA_DIR:-data/media}" ]; then
  tar -C "${BLOG_MEDIA_DIR:-data/media}" -czf "$backup_root/media.tar.gz" .
else
  printf '%s\n' 'No pre-existing uploaded media directory.' > "$backup_root/media-status.txt"
fi
printf 'Backup written to %s\n' "$backup_root"

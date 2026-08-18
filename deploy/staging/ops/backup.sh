#!/bin/sh
set -eu

export PGPASSWORD="$(cat "$POSTGRES_PASSWORD_FILE")"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
temporary="/backups/forgeflow-${timestamp}.dump.tmp"
target="/backups/forgeflow-${timestamp}.dump"

trap 'rm -f "$temporary"' EXIT
pg_dump --format=custom --no-owner --no-privileges --file="$temporary"
pg_restore --list "$temporary" >/dev/null
mv "$temporary" "$target"
sha256sum "$target" >"${target}.sha256"
find /backups -type f \( -name 'forgeflow-*.dump' -o -name 'forgeflow-*.dump.sha256' \) -mtime "+${BACKUP_RETENTION_DAYS:-14}" -delete
printf '%s\n' "$target"

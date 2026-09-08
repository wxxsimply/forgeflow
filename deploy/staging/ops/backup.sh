#!/bin/sh
set -eu

export PGPASSWORD="$(cat "$POSTGRES_PASSWORD_FILE")"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
temporary="/backups/forgeflow-${timestamp}.dump.tmp"
target="/backups/forgeflow-${timestamp}.dump"
checksum_temporary="${target}.sha256.tmp"
checksum_target="${target}.sha256"
manifest_temporary="${target}.manifest.tmp"
manifest_target="${target}.manifest"
retention_days="${BACKUP_RETENTION_DAYS:-14}"

case "$retention_days" in
  ''|*[!0-9]*) echo "BACKUP_RETENTION_DAYS must be an integer" >&2; exit 2 ;;
esac
if [ "$retention_days" -lt 1 ] || [ "$retention_days" -gt 3650 ]; then
  echo "BACKUP_RETENTION_DAYS must be between 1 and 3650" >&2
  exit 2
fi
if [ -e "$target" ] || [ -e "$checksum_target" ] || [ -e "$manifest_target" ]; then
  echo "backup timestamp collision" >&2
  exit 2
fi

trap 'rm -f "$temporary" "$checksum_temporary" "$manifest_temporary"' EXIT
pg_dump --format=custom --no-owner --no-privileges --file="$temporary"
pg_restore --list "$temporary" >/dev/null
schema_version="$(psql --tuples-only --no-align --set=ON_ERROR_STOP=1 --command='SELECT COALESCE(MAX(version), 0) FROM schema_migrations')"
case "$schema_version" in
  ''|*[!0-9]*) echo "could not record schema migration version" >&2; exit 2 ;;
esac
checksum="$(sha256sum "$temporary" | awk '{print $1}')"
size_bytes="$(wc -c <"$temporary" | tr -d ' ')"
backup_name="$(basename "$target")"
printf '%s  %s\n' "$checksum" "$backup_name" >"$checksum_temporary"
{
  printf 'schemaVersion=forgeflow.backup/v1\n'
  printf 'backupFile=%s\n' "$backup_name"
  printf 'createdAt=%s\n' "$timestamp"
  printf 'migrationVersion=%s\n' "$schema_version"
  printf 'sha256=%s\n' "$checksum"
  printf 'sizeBytes=%s\n' "$size_bytes"
} >"$manifest_temporary"

mv "$temporary" "$target"
mv "$checksum_temporary" "$checksum_target"
mv "$manifest_temporary" "$manifest_target"
find /backups -type f \( -name 'forgeflow-*.dump' -o -name 'forgeflow-*.dump.sha256' -o -name 'forgeflow-*.dump.manifest' \) -mtime "+$retention_days" -delete
printf '%s\n' "$target" "$checksum_target" "$manifest_target"

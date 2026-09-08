#!/bin/sh
set -eu

printf '%s' "${RESTORE_DATABASE:-}" | grep -Eq '^forgeflow_restore_[A-Za-z0-9_]+$' ||
  { echo "RESTORE_DATABASE must match forgeflow_restore_[A-Za-z0-9_]+" >&2; exit 2; }
if [ "${CONFIRM_RESTORE:-}" != "restore-staging-drill" ]; then
  echo "CONFIRM_RESTORE must equal restore-staging-drill" >&2
  exit 2
fi

backup_name="$(basename "${BACKUP_FILE:-}")"
printf '%s' "$backup_name" | grep -Eq '^forgeflow-[0-9]{8}T[0-9]{6}Z\.dump$' ||
  { echo "BACKUP_FILE must use the canonical UTC backup name" >&2; exit 2; }
if [ "${BACKUP_FILE:-}" != "/backups/$backup_name" ]; then
  echo "BACKUP_FILE must be a direct child of /backups" >&2
  exit 2
fi
manifest_file="${BACKUP_FILE}.manifest"
if [ ! -f "$BACKUP_FILE" ] || [ ! -f "${BACKUP_FILE}.sha256" ] || [ ! -f "$manifest_file" ]; then
  echo "backup, checksum, or manifest file is missing" >&2
  exit 2
fi

export PGPASSWORD="$(cat "$POSTGRES_PASSWORD_FILE")"
cd /backups
sha256sum -c "$(basename "$BACKUP_FILE").sha256"
manifest_schema="$(sed -n 's/^schemaVersion=//p' "$manifest_file")"
manifest_backup="$(sed -n 's/^backupFile=//p' "$manifest_file")"
manifest_migration="$(sed -n 's/^migrationVersion=//p' "$manifest_file")"
manifest_checksum="$(sed -n 's/^sha256=//p' "$manifest_file")"
manifest_size="$(sed -n 's/^sizeBytes=//p' "$manifest_file")"
actual_checksum="$(sha256sum "$BACKUP_FILE" | awk '{print $1}')"
actual_size="$(wc -c <"$BACKUP_FILE" | tr -d ' ')"
if [ "$manifest_schema" != "forgeflow.backup/v1" ] || [ "$manifest_backup" != "$backup_name" ] || [ "$manifest_checksum" != "$actual_checksum" ] || [ "$manifest_size" != "$actual_size" ]; then
  echo "backup manifest does not match the archive" >&2
  exit 2
fi
case "$manifest_migration" in
  ''|*[!0-9]*) echo "backup manifest migration version is invalid" >&2; exit 2 ;;
esac

psql --dbname=postgres --set=ON_ERROR_STOP=1 --command="DROP DATABASE IF EXISTS \"$RESTORE_DATABASE\" WITH (FORCE)"
psql --dbname=postgres --set=ON_ERROR_STOP=1 --command="CREATE DATABASE \"$RESTORE_DATABASE\""
pg_restore --no-owner --no-privileges --exit-on-error --dbname="$RESTORE_DATABASE" "$BACKUP_FILE"
restored_migration="$(psql --dbname="$RESTORE_DATABASE" --set=ON_ERROR_STOP=1 --tuples-only --no-align --command='SELECT COALESCE(MAX(version), 0) FROM schema_migrations')"
if [ "$restored_migration" != "$manifest_migration" ]; then
  echo "restored migration version does not match the backup manifest" >&2
  exit 2
fi
printf 'Restore drill succeeded into %s at migration %s\n' "$RESTORE_DATABASE" "$restored_migration"

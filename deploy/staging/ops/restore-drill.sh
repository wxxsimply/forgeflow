#!/bin/sh
set -eu

case "${RESTORE_DATABASE:-}" in
  forgeflow_restore_*) ;;
  *) echo "RESTORE_DATABASE must start with forgeflow_restore_" >&2; exit 2 ;;
esac
if [ "${CONFIRM_RESTORE:-}" != "restore-staging-drill" ]; then
  echo "CONFIRM_RESTORE must equal restore-staging-drill" >&2
  exit 2
fi
case "${BACKUP_FILE:-}" in
  /backups/forgeflow-*.dump) ;;
  *) echo "BACKUP_FILE must be an absolute /backups/forgeflow-*.dump path" >&2; exit 2 ;;
esac
if [ ! -f "$BACKUP_FILE" ] || [ ! -f "${BACKUP_FILE}.sha256" ]; then
  echo "backup or checksum file is missing" >&2
  exit 2
fi

export PGPASSWORD="$(cat "$POSTGRES_PASSWORD_FILE")"
cd /backups
sha256sum -c "$(basename "$BACKUP_FILE").sha256"
psql --dbname=postgres --set=ON_ERROR_STOP=1 --command="DROP DATABASE IF EXISTS \"$RESTORE_DATABASE\" WITH (FORCE)"
psql --dbname=postgres --set=ON_ERROR_STOP=1 --command="CREATE DATABASE \"$RESTORE_DATABASE\""
pg_restore --no-owner --no-privileges --exit-on-error --dbname="$RESTORE_DATABASE" "$BACKUP_FILE"
psql --dbname="$RESTORE_DATABASE" --set=ON_ERROR_STOP=1 --tuples-only --command="SELECT count(*) FROM schema_migrations" >/dev/null
printf 'Restore drill succeeded into %s\n' "$RESTORE_DATABASE"

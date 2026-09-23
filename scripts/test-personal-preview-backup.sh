#!/usr/bin/env bash
set -euo pipefail

image="${POSTGRES_TEST_IMAGE:-postgres:17-alpine}"
test_id="$(date -u +%Y%m%d%H%M%S)$$"
container_name="forgeflow-preview-backup-test-$test_id"
temp_parent="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
private_dir="$(mktemp -d "$temp_parent/forgeflow-preview-backup.XXXXXX")"
backup_dir="$private_dir/backups"
password_file="$private_dir/postgres_password"
ops_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../deploy/personal-preview/ops" && pwd -P)"
backup_uid="$(id -u)"
backup_gid="$(id -g)"
host_platform="$(uname -s)"
docker_user_args=()
check_posix_permissions=true
ops_mount_source="$ops_dir"
password_mount_source="$password_file"
backup_mount_source="$backup_dir"
container_started=false

docker_cmd() {
  MSYS_NO_PATHCONV=1 docker "$@"
}

cleanup() {
  if $container_started; then
    docker rm --force "$container_name" >/dev/null 2>&1 || true
  fi
  case "$private_dir" in
    "$temp_parent"/forgeflow-preview-backup.*) rm -rf -- "$private_dir" ;;
    *) echo "refusing to remove unexpected temporary path: $private_dir" >&2; return 1 ;;
  esac
}
trap cleanup EXIT

mkdir "$backup_dir"
printf '%s' 'personal-preview-backup-test-only' >"$password_file"
if [[ "$host_platform" == MINGW* || "$host_platform" == MSYS* || "$host_platform" == CYGWIN* ]]; then
  check_posix_permissions=false
  ops_mount_source="$(cygpath -m "$ops_dir")"
  password_mount_source="$(cygpath -m "$password_file")"
  backup_mount_source="$(cygpath -m "$backup_dir")"
else
  chmod 700 "$private_dir" "$backup_dir"
  chmod 600 "$password_file"
  docker_user_args=(--user "$backup_uid:$backup_gid")
fi

if docker_cmd ps -a --format '{{.Names}}' | grep -Fxq "$container_name"; then
  echo "temporary PostgreSQL container name is already in use: $container_name" >&2
  exit 2
fi

docker_cmd run --detach --name "$container_name" \
  --env POSTGRES_DB=forgeflow \
  --env POSTGRES_USER=forgeflow \
  --env POSTGRES_PASSWORD=personal-preview-backup-test-only \
  "$image" >/dev/null
container_started=true

ready=false
for attempt in $(seq 1 30); do
  if docker_cmd exec "$container_name" pg_isready --username forgeflow --dbname forgeflow >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if ! $ready; then
  docker_cmd logs "$container_name" >&2
  echo "temporary PostgreSQL did not become ready" >&2
  exit 1
fi

docker_cmd exec "$container_name" psql --set=ON_ERROR_STOP=1 --username forgeflow --dbname forgeflow \
  --command="CREATE TABLE schema_migrations (version bigint PRIMARY KEY); INSERT INTO schema_migrations(version) VALUES (42); CREATE TABLE backup_fixture (id integer PRIMARY KEY, payload text NOT NULL); INSERT INTO backup_fixture(id, payload) VALUES (1, 'synthetic-backup-fixture');" >/dev/null

docker_args=(
  --rm
  --network "container:$container_name"
  "${docker_user_args[@]}"
  --read-only
  --security-opt no-new-privileges:true
  --cap-drop ALL
  --tmpfs /tmp:size=16m,noexec,nosuid,nodev
  --env PGHOST=127.0.0.1
  --env PGPORT=5432
  --env PGUSER=forgeflow
  --env POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password
  --mount "type=bind,source=$ops_mount_source,target=/ops,readonly"
  --mount "type=bind,source=$password_mount_source,target=/run/secrets/postgres_password,readonly"
)

backup_output="$(docker_cmd run "${docker_args[@]}" \
  --env PGDATABASE=forgeflow \
  --env BACKUP_RETENTION_DAYS=14 \
  --mount "type=bind,source=$backup_mount_source,target=/backups" \
  --entrypoint /bin/sh "$image" /ops/backup.sh)"
backup_path="$(printf '%s\n' "$backup_output" | sed -n '1p')"
backup_name="$(basename "$backup_path")"
if [[ ! "$backup_name" =~ ^forgeflow-preview-[0-9]{8}T[0-9]{6}Z\.dump$ ]]; then
  echo "backup script returned a non-canonical file path" >&2
  exit 1
fi
for suffix in '' '.sha256' '.manifest'; do
  if [[ ! -f "$backup_dir/$backup_name$suffix" ]]; then
    echo "backup script did not create $backup_name$suffix" >&2
    exit 1
  fi
done
if $check_posix_permissions && [[ "$(stat -c '%a' "$backup_dir/$backup_name")" != 600 ]]; then
  echo "backup archive permissions are not 0600" >&2
  exit 1
fi
grep -Fxq 'databaseName=forgeflow' "$backup_dir/$backup_name.manifest"
grep -Fxq 'migrationVersion=42' "$backup_dir/$backup_name.manifest"

if unsafe_restore_output="$(docker_cmd run "${docker_args[@]}" \
  --env BACKUP_FILE="$backup_path" \
  --env RESTORE_DATABASE=forgeflow \
  --env CONFIRM_RESTORE=restore-personal-preview-drill \
  --mount "type=bind,source=$backup_mount_source,target=/backups,readonly" \
  --entrypoint /bin/sh "$image" /ops/restore-drill.sh 2>&1)"; then
  echo "restore script accepted the online forgeflow database as a target" >&2
  exit 1
else
  restore_exit=$?
  if [[ "$restore_exit" -ne 2 ]]; then
    echo "unsafe restore target failed with unexpected exit code $restore_exit" >&2
    printf '%s\n' "$unsafe_restore_output" >&2
    exit "$restore_exit"
  fi
fi
if [[ "$unsafe_restore_output" != *'RESTORE_DATABASE must match forgeflow_preview_restore_'* ]]; then
  echo "unsafe restore target was rejected for an unexpected reason" >&2
  exit 1
fi

restore_database="forgeflow_preview_restore_$test_id"
docker_cmd run "${docker_args[@]}" \
  --env BACKUP_FILE="$backup_path" \
  --env RESTORE_DATABASE="$restore_database" \
  --env CONFIRM_RESTORE=restore-personal-preview-drill \
  --mount "type=bind,source=$backup_mount_source,target=/backups,readonly" \
  --entrypoint /bin/sh "$image" /ops/restore-drill.sh

restored_payload="$(docker_cmd exec "$container_name" psql --tuples-only --no-align --set=ON_ERROR_STOP=1 \
  --username forgeflow --dbname "$restore_database" \
  --command='SELECT payload FROM backup_fixture WHERE id = 1')"
source_payload="$(docker_cmd exec "$container_name" psql --tuples-only --no-align --set=ON_ERROR_STOP=1 \
  --username forgeflow --dbname forgeflow \
  --command='SELECT payload FROM backup_fixture WHERE id = 1')"
if [[ "$restored_payload" != 'synthetic-backup-fixture' || "$source_payload" != 'synthetic-backup-fixture' ]]; then
  echo "backup restore changed or lost the synthetic fixture" >&2
  exit 1
fi

echo "Personal preview backup and isolated restore integration test passed."

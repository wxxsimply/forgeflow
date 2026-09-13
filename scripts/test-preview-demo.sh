#!/bin/sh
# Local disposable-container test only. /srv and /tmp must be temporary filesystems.
set -eu
[ "${FORGEFLOW_DISPOSABLE_TEST:-}" = 1 ] || { echo 'Requires disposable test container' >&2; exit 1; }
[ ! -e /srv/forgeflow ] || { echo 'Refusing nonempty test target' >&2; exit 1; }
mkdir -p /srv/forgeflow
chmod 750 /srv/forgeflow
script=/fixture/scripts/prepare-preview-demo.sh
target=/srv/forgeflow/preview-repositories
demo=$target/demo

case "${1:-happy}" in
  symlink)
    mkdir /tmp/unrelated
    ln -s /tmp/unrelated "$target"
    if sh "$script" --apply; then echo 'Accepted a symlink' >&2; exit 1; fi
    [ -z "$(ls -A /tmp/unrelated)" ]
    echo 'PASS: symlink target refused without writes'
    exit 0
    ;;
  foreign)
    mkdir "$target"
    before=$(stat -c '%u:%g:%a' "$target")
    if sh "$script" --apply; then echo 'Changed a foreign directory' >&2; exit 1; fi
    [ "$(stat -c '%u:%g:%a' "$target")" = "$before" ]
    [ -z "$(ls -A "$target")" ]
    echo 'PASS: unexpected ownership refused without changes'
    exit 0
    ;;
  happy) ;;
  *) exit 2 ;;
esac

sh "$script" --check
[ ! -e "$target" ]
sh "$script" --apply
[ "$(stat -c '%u:%g:%a' "$target")" = 10001:10001:750 ]
[ "$(stat -c '%u:%g:%a' "$demo/.git")" = 10001:10001:750 ]
git_demo() { git -c safe.directory="$demo" -C "$demo" "$@"; }
before=$(git_demo rev-parse HEAD)
sh "$script" --apply
[ "$(git_demo rev-parse HEAD)" = "$before" ]
[ -z "$(git_demo status --porcelain)" ]
[ "$(git_demo ls-files | wc -l | tr -d ' ')" = 4 ]
[ ! -e /srv/forgeflow/repositories ]
# Host parent stays restricted. Change only this disposable parent for the UID
# test; in production Docker binds the dedicated root directly at /repositories.
chmod 755 /srv/forgeflow
su -s /bin/sh forgeflow -c "git -C $demo rev-parse HEAD"
su -s /bin/sh forgeflow -c "git -C $demo worktree add --detach /tmp/preview-worktree HEAD"
[ -f /tmp/preview-worktree/greeting.go ]
[ -z "$(git_demo status --porcelain)" ]
echo 'PASS: dry-run, initialize, repeat, app UID Git access and isolated worktree'

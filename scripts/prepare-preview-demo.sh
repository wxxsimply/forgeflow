#!/bin/sh
# Run only after owner approval. No existing repository or permissions are changed.
set -eu

mode=${1:---check}
case "$mode" in --check|--apply) ;; *) echo 'Usage: sh scripts/prepare-preview-demo.sh [--check|--apply]' >&2; exit 2 ;; esac
[ "$#" -le 1 ] || exit 2
target=/srv/forgeflow/preview-repositories
demo=$target/demo
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../examples/preview-demo" && pwd -P)

for command_name in git install stat readlink chown; do
    command -v "$command_name" >/dev/null || { echo "Missing command: $command_name" >&2; exit 1; }
done
for path in /srv /srv/forgeflow "$target" "$demo" "$demo/.git"; do
    [ ! -L "$path" ] || { echo "Refusing symlink: $path" >&2; exit 1; }
done
[ -d /srv/forgeflow ] || { echo '/srv/forgeflow must already exist' >&2; exit 1; }
[ "$(readlink -f /srv/forgeflow)" = /srv/forgeflow ] || exit 1
for name in go.mod greeting.go greeting_test.go README.md; do
    [ -f "$source_dir/$name" ] && [ ! -L "$source_dir/$name" ] || exit 1
done

# Initialization is an administrator operation on these four public files only.
# An exact safe.directory exception avoids trusting any unrelated repository.
demo_git() { git -c safe.directory="$demo" -c core.hooksPath=/dev/null "$@"; }

if [ -e "$target" ]; then
    [ -d "$target" ] && [ "$(stat -c '%u:%g:%a' "$target")" = 10001:10001:750 ] || {
        echo "Existing $target has unexpected ownership/permissions; refusing to change it" >&2; exit 1;
    }
fi
if [ -e "$demo" ]; then
    [ -d "$demo/.git" ] && [ "$(stat -c '%u:%g:%a' "$demo")" = 10001:10001:750 ] || {
        echo 'Existing demo is not a completed managed initialization; manual review required' >&2; exit 1;
    }
fi

printf 'Scope: %s only; container UID:GID 10001:10001\n' "$target"
printf 'Original /srv/forgeflow/repositories and its permissions will remain unchanged.\n'
if [ "$mode" = --check ]; then
    printf 'Check only: no changes made. After approval, run with sudo and --apply.\n'
    exit 0
fi
[ "$(id -u)" -eq 0 ] || { echo '--apply requires sudo/root for numeric ownership, not root app execution' >&2; exit 1; }

if [ -d "$demo" ]; then
    cd "$demo"
    [ "$(demo_git config --local --get forgeflow.previewDemo)" = true ] || {
        echo 'Unrecognized existing demo; refusing to overwrite it' >&2; exit 1;
    }
    demo_git rev-parse --verify HEAD
    printf 'Already initialized; existing files were not modified.\n'
    exit 0
fi
umask 027
if [ ! -d "$target" ]; then install -d -m 750 -o 10001 -g 10001 "$target"; fi
install -d -m 750 -o 10001 -g 10001 "$demo"
for name in go.mod greeting.go greeting_test.go README.md; do
    install -m 640 -o 10001 -g 10001 "$source_dir/$name" "$demo/$name"
done
cd "$demo"
demo_git init -b main
demo_git add -- go.mod greeting.go greeting_test.go README.md
demo_git -c commit.gpgsign=false -c user.name='ForgeFlow Demo' -c user.email='demo@example.invalid' commit -m 'Initialize isolated preview demo'
demo_git config --local forgeflow.previewDemo true
# Only the .git directory just created above is recursively assigned; never
# touch the original repository root or existing demo content on a rerun.
[ ! -L "$demo/.git" ] && [ "$(readlink -f "$demo/.git")" = "$demo/.git" ] || exit 1
chown -R 10001:10001 "$demo/.git"
printf 'Prepared %s; no service restarted or environment file changed.\n' "$demo"
printf 'After approval set FORGEFLOW_REPOSITORY_PATH=%s and register /repositories/demo, branch main.\n' "$target"

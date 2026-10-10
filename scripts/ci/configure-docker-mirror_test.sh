#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir "$work/bin"
export MIRROR_TEST_WORK="$work"
cat >"$work/bin/docker" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$MIRROR_TEST_WORK/docker.log"
case "$1" in
info)
  [ "${MIRROR_TEST_INFO_FAIL:-}" != true ] || exit 1
  if [ -f "$MIRROR_TEST_WORK/restarted" ]; then
    jq '."registry-mirrors"' "$MIRROR_TEST_WORK/installed.json"
  else
    printf '[]\n'
  fi ;;
ps) printf '%s' "${MIRROR_TEST_RUNNING:-}" ;;
*) exit 1 ;;
esac
EOF
cat >"$work/bin/sudo" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$MIRROR_TEST_WORK/sudo.log"
case "$1" in
test) test -f "$MIRROR_TEST_WORK/current.json" ;;
cat) cat "$MIRROR_TEST_WORK/current.json" ;;
dockerd) [ "${MIRROR_TEST_INVALID:-}" != 1 ] ;;
install) cp "$4" "$MIRROR_TEST_WORK/installed.json" ;;
systemctl) touch "$MIRROR_TEST_WORK/restarted" ;;
*) exit 1 ;;
esac
EOF
chmod +x "$work/bin/docker" "$work/bin/sudo"
export PATH="$work/bin:$PATH"
# Local invocations must not even query or mutate the user's Docker daemon.
GITHUB_ACTIONS=false "$script_dir/configure-docker-mirror.sh"
test ! -f "$work/docker.log"
test ! -f "$work/sudo.log"
export GITHUB_ACTIONS=true RUNNER_ENVIRONMENT=github-hosted RUNNER_OS=Linux
if RUNNER_ENVIRONMENT=self-hosted "$script_dir/configure-docker-mirror.sh" 2>/dev/null; then exit 1; fi
if MIRROR_TEST_INFO_FAIL=true "$script_dir/configure-docker-mirror.sh" 2>/dev/null; then exit 1; fi
if MIRROR_TEST_RUNNING=existing "$script_dir/configure-docker-mirror.sh" 2>/dev/null; then exit 1; fi
test ! -f "$work/sudo.log"
printf '{"data-root":"/existing","registry-mirrors":["https://existing.example"]}\n' >"$work/current.json"
if MIRROR_TEST_INVALID=1 "$script_dir/configure-docker-mirror.sh" 2>/dev/null; then exit 1; fi
test ! -f "$work/installed.json"
test ! -f "$work/restarted"
"$script_dir/configure-docker-mirror.sh"
jq -e '."data-root" == "/existing" and (."registry-mirrors" | sort) == ["https://existing.example","https://mirror.gcr.io"]' "$work/installed.json" >/dev/null
count=$(wc -l <"$work/sudo.log")
# Once configured, repeated calls must not restart fixtures already running.
MIRROR_TEST_RUNNING=existing "$script_dir/configure-docker-mirror.sh"
test "$(wc -l <"$work/sudo.log")" = "$count"
printf 'Docker mirror setup: local isolation, validation, config preservation and safe idempotence passed\n'

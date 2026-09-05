#!/usr/bin/env bash
# Port discovery must preserve exact container ownership and fail closed before
# exporting an unusable or non-loopback target to subsequent CI steps.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  create)
    [[ " $* " == *' -p 127.0.0.1::5432 '* ]] || exit 90
    printf 'owned-fixture-id\n'
    ;;
  cp) [[ "$3" == owned-fixture-id:/tmp/* ]] || exit 91 ;;
  start) [[ "$2" == owned-fixture-id ]] || exit 92 ;;
  port)
    [[ "$2" == owned-fixture-id && "$3" == 5432/tcp ]] || exit 93
    [[ "$DYNAMIC_PG_TEST_BINDING" != query-failed ]] || exit 94
    printf '%s\n' "$DYNAMIC_PG_TEST_BINDING"
    ;;
  exec)
    if [[ "$2" == -e ]]; then
      [[ "$4" == owned-fixture-id ]] || exit 95
      # The fixture's explicit plaintext refusal probe must fail.
      exit 1
    fi
    [[ "$2" == owned-fixture-id ]] || exit 96
    ;;
  rm)
    [[ "$2" == -f && "$3" == owned-fixture-id ]] || exit 97
    touch "$DYNAMIC_PG_TEST_REMOVED"
    ;;
  *) exit 98 ;;
esac
EOF
chmod +x "$work/bin/docker"
export PATH="$work/bin:$PATH"
export RUNNER_TEMP="$work"
export GITHUB_ENV="$work/github-env"
export DYNAMIC_PG_TEST_REMOVED="$work/removed"
export DYNAMIC_PG_TEST_BINDING=127.0.0.1:49157
"$root/scripts/ci/start-dynamic-pg.sh" >"$work/success.log" 2>&1
grep -Fx 'HIKYO_TEST_DYNAMIC_PG_DSN=postgres://leaseadmin@127.0.0.1:49157/app' "$GITHUB_ENV" >/dev/null
grep -Fx 'HIKYO_TEST_DYNAMIC_PG_CONTAINER=owned-fixture-id' "$GITHUB_ENV" >/dev/null
grep -Fx 'HIKYO_DYNAMIC_PG_REQUIRED=1' "$GITHUB_ENV" >/dev/null
[[ ! -e "$DYNAMIC_PG_TEST_REMOVED" ]]
for binding in '' '0.0.0.0:49157' '127.0.0.1:0' '127.0.0.1:65536' '127.0.0.1:abc' $'127.0.0.1:49157\n127.0.0.1:49158' query-failed; do
  rm -f "$GITHUB_ENV" "$DYNAMIC_PG_TEST_REMOVED"
  export DYNAMIC_PG_TEST_BINDING="$binding"
  if "$root/scripts/ci/start-dynamic-pg.sh" >"$work/refusal.log" 2>&1; then
    echo 'dynamic-pg port fixture: invalid discovery succeeded' >&2
    exit 1
  fi
  [[ ! -s "$GITHUB_ENV" && -e "$DYNAMIC_PG_TEST_REMOVED" ]]
done
printf 'dynamic-pg port discovery: owned binding exported; seven invalid/error bindings refused and cleaned\n'

#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir "$work/bin"
export PG_TEST_WORK="$work" GITHUB_ACTIONS=false
cat >"$work/bin/docker" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$PG_TEST_WORK/docker.log"
case "$1 $2" in
'container inspect') [ "${PG_TEST_EXISTING:-}" = true ] ;;
'container rm') touch "$PG_TEST_WORK/removed" ;;
'pull '*) [ "${PG_TEST_PULL_FAIL:-}" != true ] ;;
'run '*) [ "${PG_TEST_RUN_FAIL:-}" != true ] ;;
'inspect '*) printf '%s\n' "${PG_TEST_HEALTH:-healthy}" ;;
'logs '*) printf 'fixture logs\n' ;;
*) exit 1 ;;
esac
EOF
printf '#!/bin/sh\nexit 0\n' >"$work/bin/sleep"
chmod +x "$work/bin/docker" "$work/bin/sleep"
export PATH="$work/bin:$PATH"
"$script_dir/start-ci-postgres.sh"
test ! -f "$work/removed"
grep -Fx 'pull postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941' "$work/docker.log" >/dev/null
grep -F -- '--publish 127.0.0.1:5432:5432' "$work/docker.log" >/dev/null
if PG_TEST_EXISTING=true "$script_dir/start-ci-postgres.sh" 2>/dev/null; then exit 1; fi
test ! -f "$work/removed"
if PG_TEST_PULL_FAIL=true "$script_dir/start-ci-postgres.sh" 2>/dev/null; then exit 1; fi
test ! -f "$work/removed"
if PG_TEST_RUN_FAIL=true "$script_dir/start-ci-postgres.sh" 2>/dev/null; then exit 1; fi
test -f "$work/removed"
rm "$work/removed"
if PG_TEST_HEALTH=unhealthy "$script_dir/start-ci-postgres.sh" 2>/dev/null; then exit 1; fi
test -f "$work/removed"
rm "$work/removed"
if PG_TEST_HEALTH=starting "$script_dir/start-ci-postgres.sh" 2>/dev/null; then exit 1; fi
test -f "$work/removed"
POSTGRES_DB=hikyo_release_schema POSTGRES_PASSWORD=release-schema-fixture POSTGRES_HEALTH_RETRIES=12 "$script_dir/start-ci-postgres.sh"
grep -F -- '--health-cmd pg_isready -U hikyo -d hikyo_release_schema --health-interval 5s --health-timeout 5s --health-retries 12' "$work/docker.log" >/dev/null
printf 'CI PostgreSQL: canonical pin, isolation, refusal, health failure cleanup and release fixture passed\n'

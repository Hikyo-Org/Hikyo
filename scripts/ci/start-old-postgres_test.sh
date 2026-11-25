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
case "$1" in
run) printf 'fixture-id\n' ;;
exec) [ "${PG_TEST_NOT_READY:-}" != true ] ;;
port) printf '%s\n' "${PG_TEST_BINDING-127.0.0.1:49153}" ;;
rm) touch "$PG_TEST_WORK/removed" ;;
logs) printf 'fixture logs\n' ;;
*) exit 1 ;;
esac
EOF
printf '#!/bin/sh\nexit 0\n' >"$work/bin/sleep"
chmod +x "$work/bin/docker" "$work/bin/sleep"
export PATH="$work/bin:$PATH"
script="$script_dir/start-old-postgres.sh"

# Outside GitHub Actions the script refuses before it starts a container.
if (unset GITHUB_ENV; "$script" 2>/dev/null); then exit 1; fi
test ! -f "$work/docker.log"

# A healthy, uniquely bound server exports its DSN and is kept.
: >"$work/env"
GITHUB_ENV="$work/env" "$script"
grep -Fx 'HIKYO_TEST_POSTGRES_OLD_DSN=postgres://hikyo:hikyo@127.0.0.1:49153/hikyo_old?sslmode=disable' "$work/env" >/dev/null
grep -F 'postgres:17-alpine@sha256:' "$work/docker.log" >/dev/null
test ! -f "$work/removed"

# A server that never becomes ready is removed and exports nothing.
: >"$work/env"
if PG_TEST_NOT_READY=true GITHUB_ENV="$work/env" "$script" 2>/dev/null; then exit 1; fi
test -f "$work/removed"
test ! -s "$work/env"
rm "$work/removed"

# A refused binding is removed and exports nothing.
for binding in '0.0.0.0:49153' '127.0.0.1:0' '127.0.0.1:70000' '127.0.0.1:1
127.0.0.1:2' ''; do
	: >"$work/env"
	if PG_TEST_BINDING="$binding" GITHUB_ENV="$work/env" "$script" 2>/dev/null; then exit 1; fi
	test -f "$work/removed"
	test ! -s "$work/env"
	rm "$work/removed"
done
printf 'PostgreSQL 17 fixture: environment guard, export, readiness and binding refusal cleanup passed\n'

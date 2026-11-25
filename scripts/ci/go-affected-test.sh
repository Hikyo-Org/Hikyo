#!/bin/sh
# Run the Go tests the changes since BASE can affect (see go-affected-packages.sh)
# in the current tree, which the pre-push hook makes a detached worktree at the
# pushed commit:  scripts/ci/go-affected-test.sh BASE
# Exit 64 is this script's own setup failing; 130 an interrupt; 1 a failing test.
# CI splits the packages the way test-core-packages.sh and ci.yml do: the pool
# runs two package workers at a time, then internal/isolation (sharded in CI,
# serial here, with CI's 18m timeout), internal/app (two runs, packaged-binary
# tests apart, so none crosses Go's 10m alarm; a Go test keeps the pattern equal
# to test-core-packages.sh) and internal/lint (20m) run alone, since app drops
# PostgreSQL databases. PostgreSQL tests run when HIKYO_TEST_POSTGRES_DSN is set
# and skip otherwise; CI is unset, because with it set those tests fail instead
# of skipping. internal/webui/dist is gitignored and built by CI; without it
# the ui-tagged `go list` of internal/boundary cannot load, so a placeholder
# stands in (the tests that need the real SPA run in CI).
set -eu
[ "$#" -eq 1 ] || {
	printf 'usage: %s BASE\n' "$0" >&2
	exit 2
}
unset CI
here=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-go-affected.XXXXXX") || exit 64
trap 'rm -rf "$work"' EXIT
trap 'exit 130' HUP INT TERM

if [ -d internal/webui ] && [ ! -e internal/webui/dist ]; then
	mkdir internal/webui/dist && : >internal/webui/dist/.placeholder || exit 64
fi
"$here/go-affected-packages.sh" "$1" HEAD >"$work/all" || exit 64
app=$(go list ./internal/app) || exit 64
lint=$(go list ./internal/lint) || exit 64
isolation=$(go list ./internal/isolation) || exit 64
grep -Fvx -e "$app" -e "$lint" -e "$isolation" "$work/all" >"$work/pool" || :
count=$(grep -c . "$work/all" || :)
if [ "$count" -eq 0 ]; then
	printf 'go tests: no Go package is affected by the changes\n' >&2
	exit 0
fi
printf 'go tests: %s affected packages (minutes, longer with internal/app, internal/isolation or internal/lint among them)\n' "$count" >&2
[ -n "${HIKYO_TEST_POSTGRES_DSN:-}" ] ||
	printf 'go tests: HIKYO_TEST_POSTGRES_DSN is unset, so PostgreSQL tests are skipped; CI runs them\n' >&2

status=0
if [ -s "$work/pool" ]; then
	xargs go test -p 2 <"$work/pool" || status=1
fi
# The serial packages run even after a failure, for the full picture.
if grep -Fxq "$isolation" "$work/all"; then go test -p 2 -timeout=18m "$isolation" || status=1; fi
if grep -Fxq "$app" "$work/all"; then
	packaged='Packaged|^TestActualDefaultDevCLIStartsAndRestarts$'
	go test -p 2 -skip "$packaged" "$app" || status=1
	go test -p 2 -run "$packaged" "$app" || status=1
fi
if grep -Fxq "$lint" "$work/all"; then
	if go run ./scripts/ci/prepare-lint-cache; then
		go test -p 2 -timeout=20m "$lint" || status=1
	else
		status=64
	fi
fi
exit "$status"

#!/bin/sh
# Run the Go tests the changes since BASE can affect (see go-affected-packages.sh)
# in the current tree, which the pre-push hook makes a detached worktree at the
# pushed commit:  scripts/ci/go-affected-test.sh BASE
# Two package workers at a time, and internal/app (it drops PostgreSQL
# databases) and internal/lint alone afterwards, as test-core-packages.sh does.
# PostgreSQL tests run when HIKYO_TEST_POSTGRES_DSN is set and skip otherwise.
# The caller sets GOFLAGS=-trimpath so unchanged results come from the test cache.
set -eu
[ "$#" -eq 1 ] || {
	printf 'usage: %s BASE\n' "$0" >&2
	exit 2
}
here=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-go-affected.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

"$here/go-affected-packages.sh" "$1" HEAD >"$work/all" || exit 64
app=$(go list ./internal/app)
lint=$(go list ./internal/lint)
grep -Fvx -e "$app" -e "$lint" "$work/all" >"$work/pool" || :
count=$(grep -c . "$work/all" || :)
if [ "$count" -eq 0 ]; then
	printf 'go tests: no Go package is affected by the changes\n' >&2
	exit 0
fi
printf 'go tests: %s affected packages (a few minutes when the test cache is cold; unchanged results are cached)\n' "$count" >&2
[ -n "${HIKYO_TEST_POSTGRES_DSN:-}" ] ||
	printf 'go tests: HIKYO_TEST_POSTGRES_DSN is unset, so PostgreSQL tests are skipped; CI runs them\n' >&2

status=0
if [ -s "$work/pool" ]; then
	xargs go test -p 2 <"$work/pool" || status=1
fi
# Run the serial packages even after a failure, for the full picture.
if grep -Fxq "$app" "$work/all"; then go test -p 2 "$app" || status=1; fi
if grep -Fxq "$lint" "$work/all"; then
	go run ./scripts/ci/prepare-lint-cache && go test -p 2 -timeout=20m "$lint" || status=1
fi
exit "$status"

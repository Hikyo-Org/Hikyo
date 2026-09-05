#!/usr/bin/env bash
# Keep the app's database-drop checks out of concurrent package migrations:
# DROP DATABASE waits for the shared PostgreSQL checkpoint/fsync queue.
# Isolation has its existing dedicated shards; every other package runs once.
set -euo pipefail
work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-test-core.XXXXXX")
trap 'rm -rf "$work"' EXIT
isolation_package=$(go list ./internal/isolation)
app_package=$(go list ./internal/app)
go list ./... >"$work/all"
if [[ "$isolation_package" == "$app_package" ]] ||
  [[ $(grep -Fxc "$isolation_package" "$work/all" || true) != 1 ]] ||
  [[ $(grep -Fxc "$app_package" "$work/all" || true) != 1 ]] ||
  [[ -n $(sort "$work/all" | uniq -d) ]]; then
  echo 'test core: package inventory is missing or duplicates an expected package' >&2
  exit 1
fi
if ! grep -Fvx -e "$isolation_package" -e "$app_package" "$work/all" >"$work/concurrent" ||
  [[ ! -s "$work/concurrent" ]]; then
  echo 'test core: concurrent package inventory is empty' >&2
  exit 1
fi
status=0
xargs go test -count=1 <"$work/concurrent" || status=1
# Run even if another package failed, preserving useful coverage on red runs.
go test -count=1 "$app_package" || status=1
exit "$status"

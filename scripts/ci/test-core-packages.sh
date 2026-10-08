#!/usr/bin/env bash
# Keep the app's database-drop checks out of concurrent package migrations:
# DROP DATABASE waits for the shared PostgreSQL checkpoint/fsync queue.
# Run repository analysis separately: its cross-platform compiler loads must
# not compete with all runtime suites for CPU and memory. Every package runs once.
set -euo pipefail
# Compile the lint matrix before Go's package-test alarm starts. Do not let
# cold cross-platform export builds contend with the concurrent test pool.
go run ./scripts/ci/prepare-lint-cache
work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-test-core.XXXXXX")
trap 'rm -rf "$work"' EXIT
isolation_package=$(go list ./internal/isolation)
app_package=$(go list ./internal/app)
lint_package=$(go list ./internal/lint)
go list ./... >"$work/all"
if [[ "$isolation_package" == "$app_package" ]] ||
  [[ $(grep -Fxc "$isolation_package" "$work/all" || true) != 1 ]] ||
  [[ $(grep -Fxc "$app_package" "$work/all" || true) != 1 ]] ||
  [[ $(grep -Fxc "$lint_package" "$work/all" || true) != 1 ]] ||
  [[ -n $(sort "$work/all" | uniq -d) ]]; then
  echo 'test core: package inventory is missing or duplicates an expected package' >&2
  exit 1
fi
if ! grep -Fvx -e "$isolation_package" -e "$app_package" -e "$lint_package" "$work/all" >"$work/concurrent" ||
  [[ ! -s "$work/concurrent" ]]; then
  echo 'test core: concurrent package inventory is empty' >&2
  exit 1
fi
status=0
xargs go test -count=1 <"$work/concurrent" || status=1
# Run even if another package failed, preserving useful coverage on red runs.
go test -count=1 "$app_package" || status=1
go test -count=1 "$lint_package" || status=1
exit "$status"

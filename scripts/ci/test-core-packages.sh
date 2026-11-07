#!/usr/bin/env bash
# Keep the app's database-drop checks out of concurrent package migrations:
# DROP DATABASE waits for the shared PostgreSQL checkpoint/fsync queue.
# Run repository analysis separately: its cross-platform compiler loads must
# not compete with all runtime suites for CPU and memory. Every package runs once.
# Only internal/lint needs the compiled lint matrix; internal/boundary just runs
# `go list -json` (no export data) and internal/isolation is sharded elsewhere,
# so the pool skips the multi-minute prepare-lint-cache compile.
#
# Modes (default pool, which the frozen test_core step invokes without arguments):
#   pool    every package except isolation, app and lint, concurrently
#   serial  prepare the lint cache, then internal/app and internal/lint, alone,
#           in the test_core_serial job
# Both modes run the same inventory checks, so a package cannot drop out of both.
set -euo pipefail
mode=${1:-pool}
case "$mode" in
  pool | serial) ;;
  *)
    echo "test core: unknown mode '$mode' (want pool or serial)" >&2
    exit 2
    ;;
esac
# Compile the lint matrix before Go's package-test alarm starts, in serial mode
# only (the sole consumer is internal/lint, which runs alone on its own runner).
if [[ "$mode" == serial ]]; then
  go run ./scripts/ci/prepare-lint-cache
fi
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
if [[ "$mode" == pool ]]; then
  xargs go test -count=1 <"$work/concurrent" || status=1
else
  # Run lint even if app failed, preserving useful coverage on red runs.
  go test -count=1 "$app_package" || status=1
  go test -count=1 "$lint_package" || status=1
fi
exit "$status"

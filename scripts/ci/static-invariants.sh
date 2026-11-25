#!/usr/bin/env bash
# Fast-feedback form of the static repository invariants. Each test below reads
# Go sources or builds the lint driver: none opens a database, starts PostgreSQL
# or runs an isolation fixture. The complete suites stay in ci-invariants.yml
# and the isolation shards; this only reports their static failures on the PR
# head, before merge. Run from the checkout under test with Go on PATH.
#
#   internal/boundary   whole package: TestForbiddenEdges and the import/package fences
#   internal/isolation  TestInvariant* (route/CLI classification, registry, cache
#                       discipline = invariant 12, pinning) and their helpers
#   internal/lint       TestVetDriverRepo in the default build context: the analyzer
#                       suite (driver handles, fence, appendonly, redaction, ...) over
#                       the whole module. The darwin, windows and ui contexts stay
#                       in the full run.
set -euo pipefail

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-static-invariants.XXXXXX")
trap 'rm -rf "$work"' EXIT
root=$(pwd)

# One build graph for the three test binaries; they then run side by side.
go test -c -p 2 -o "$work/" ./internal/boundary ./internal/isolation ./internal/lint
printf 'static-invariants: test binaries built in %ss\n' "$SECONDS"

pids=()
run_in() {
	local dir=$1 binary=$2
	shift 2
	(cd "$root/$dir" && "$work/$binary" "$@" && printf 'static-invariants: %s finished at %ss\n' "$dir" "$SECONDS") &
	pids+=("$!")
}
run_in internal/boundary boundary.test
run_in internal/isolation isolation.test -test.run '^(TestInvariant[0-9]+.*|TestCacheDeclarationFindings.*|TestPackageScopeNames.*)$'
run_in internal/lint lint.test -test.run '^TestVetDriverRepo$/^default$'
status=0
for pid in "${pids[@]}"; do
	wait "$pid" || status=$?
done
exit "$status"

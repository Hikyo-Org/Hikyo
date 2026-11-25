#!/usr/bin/env bash
# Fast-feedback form of the static repository invariants. Each test below reads
# Go sources, the SQL query files or the authorization registry: none opens a
# database, starts PostgreSQL or runs an isolation fixture. The complete suites stay in ci-invariants.yml
# and the isolation shards; this only reports their static failures on the PR
# head, before merge. Run from the checkout under test with Go on PATH.
#
#   internal/boundary   whole package: TestForbiddenEdges and the import/package fences
#   internal/isolation  TestInvariant* (route/CLI classification, registry, cache
#                       discipline = invariant 12, pinning) and their helpers
#
# Not here: internal/lint TestVetDriverRepo. It is static too, but it vets the whole module and
# took 113 s on a warm 12-core laptop and did not finish inside the hosted deadline (about 50 s
# were left after the 105 s build). It stays in ci-invariants.yml and the full run.
set -euo pipefail

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-static-invariants.XXXXXX")
trap 'rm -rf "$work"' EXIT
root=$(pwd)

# One build graph for both test binaries; they then run side by side.
go test -c -p 2 -o "$work/" ./internal/boundary ./internal/isolation
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
status=0
for pid in "${pids[@]}"; do
	wait "$pid" || status=$?
done
exit "$status"

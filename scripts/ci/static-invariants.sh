#!/usr/bin/env bash
# Fast-feedback form of the static repository invariants. Each test below reads
# Go sources, the SQL query files or the authorization registry: none opens a
# database, starts PostgreSQL or runs an isolation fixture. The complete suites
# stay in ci-invariants.yml and the isolation shards; this only reports their
# static failures on the PR head, before merge. Run from the checkout under test with Go on PATH.
#
#   internal/boundary   whole package: TestForbiddenEdges and the import/package fences
#   internal/isolation  TestInvariant* (route/CLI classification, registry, cache
#                       discipline = invariant 12, pinning) and their helpers
#   internal/lint       TestVetDriverChangedPackages: the per-package analyzers (driver handles,
#                       fence, ...) in the default build context over the changed packages
#                       only, named in HIKYO_VET_PACKAGES. The whole-module parts and the other
#                       contexts (TestVetDriverRepo) stay in ci-invariants.yml and the full run.
set -euo pipefail

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-static-invariants.XXXXXX")
trap 'rm -rf "$work"' EXIT
root=$(pwd)

# One build graph for the three test binaries; they then run side by side.
go test -c -o "$work/" ./internal/boundary ./internal/isolation ./internal/lint
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
# A wide change would vet most of the module; the full run covers that.
max_vet_packages=25
read -r -a vet_packages <<<"${HIKYO_VET_PACKAGES:-}"
if [ "${#vet_packages[@]}" -gt "$max_vet_packages" ]; then
	printf 'static-invariants: more than %s changed packages; lint left to the full run\n' "$max_vet_packages"
	HIKYO_VET_PACKAGES=''
fi
export HIKYO_VET_PACKAGES
run_in internal/lint lint.test -test.run '^TestVetDriverChangedPackages$'
status=0
for pid in "${pids[@]}"; do
	wait "$pid" || status=$?
done
exit "$status"

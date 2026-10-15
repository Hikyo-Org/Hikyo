#!/usr/bin/env bash
# Advisory dependency feedback. Full candidate validation remains the merge gate.
# Compile every production and test consumer without running init or tests.
set -euo pipefail
[ "$#" -eq 0 ] || { printf 'usage: %s\n' "$0" >&2; exit 2; }
cd "$(git rev-parse --show-toplevel)"
export GOFLAGS=-mod=readonly
run() {
	local started=$SECONDS
	printf 'dependency-compatibility: running %s\n' "$*"
	"$@"
	printf 'dependency-compatibility: completed in %ss\n' "$((SECONDS - started))"
}
run go mod verify
# Export every package/test archive, sharing the build graph without linking
# all test binaries. Module integrity alone cannot catch upstream API changes.
run go list -deps -test -export -f '{{""}}' ./...
run go vet ./...
printf 'dependency-compatibility: all production and test consumers passed\n'

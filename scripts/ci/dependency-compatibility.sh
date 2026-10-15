#!/usr/bin/env bash
# Advisory dependency feedback. Full candidate validation remains the merge gate.
# Compile the selected target's default-tag consumers without running init or
# tests. Cross-platform and release-tag proof belongs to full candidate CI.
set -euo pipefail
[ "$#" -eq 0 ] || { printf 'usage: %s\n' "$0" >&2; exit 2; }
cd "$(git rev-parse --show-toplevel)"
export GOFLAGS=-mod=readonly
target_os=$(go env GOOS)
target_arch=$(go env GOARCH)
cgo_enabled=$(go env CGO_ENABLED)
printf 'dependency-compatibility: scope target=%s/%s CGO_ENABLED=%s build-tags=default\n' "$target_os" "$target_arch" "$cgo_enabled"
run() {
	local started=$SECONDS
	printf 'dependency-compatibility: running %s\n' "$*"
	"$@"
	printf 'dependency-compatibility: completed in %ss\n' "$((SECONDS - started))"
}
run go mod verify
# Export the target's default-tag package/test archives, sharing the build graph
# without linking test binaries. Integrity alone cannot catch upstream API changes.
run go list -deps -test -export -f '{{""}}' ./...
run go vet ./...
printf 'dependency-compatibility: default-tag production and test consumers passed for %s/%s (CGO_ENABLED=%s)\n' "$target_os" "$target_arch" "$cgo_enabled"

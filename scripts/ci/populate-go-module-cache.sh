#!/bin/sh
# Fills the Go module cache with the repository's modules and the pinned tool modules. The two
# trusted writers of go-mod-v2 (ci.yml and ci-race-cache.yml) both run this, so the entry they
# save is the same whichever of them saves first.
set -eu

: "${RUNNER_TEMP:?GitHub runner temp directory is required}"
repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)

(cd "$repo_root" && go mod download)
tool_module_dir="$RUNNER_TEMP/go-tool-modules"
mkdir -p "$tool_module_dir"
cd "$tool_module_dir"
go mod init ci-tools
while IFS= read -r module; do
	go mod edit -require="$module"
done <"$repo_root/scripts/ci/go-tool-modules.txt"
go mod download all

#!/bin/sh
set -eu

# Node.js stopped bundling Corepack after Node 24. Keep the bootstrap exact and
# script-free so every Node 26 CI path gets the same package-manager launcher.
corepack_version=0.35.0
npm install --global --ignore-scripts --no-audit --no-fund "corepack@$corepack_version"
corepack enable

actual=$(corepack --version)
if [ "$actual" != "$corepack_version" ]; then
	printf 'Corepack bootstrap: got %s, want %s\n' "$actual" "$corepack_version" >&2
	exit 1
fi

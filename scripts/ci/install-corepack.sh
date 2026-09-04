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

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
package_manager=
for manifest in \
	web/package.json \
	clients/ts/package.json \
	docs/site/package.json \
	scripts/mcp-conformance/package.json
do
	declared=$(node -p 'require(process.argv[1]).packageManager' \
		"$repo_root/$manifest")
	case "$declared" in
		pnpm@*) ;;
		*)
			printf 'Corepack bootstrap: %s must pin pnpm, got %s\n' \
				"$manifest" "$declared" >&2
			exit 1
			;;
	esac
	if [ -z "$package_manager" ]; then
		package_manager=$declared
	elif [ "$declared" != "$package_manager" ]; then
		printf 'Corepack bootstrap: package manager mismatch: %s has %s, want %s\n' \
			"$manifest" "$declared" "$package_manager" >&2
		exit 1
	fi
done

corepack install --global "$package_manager"
actual=$(pnpm --version)
expected=${package_manager#pnpm@}
if [ "$actual" != "$expected" ]; then
	printf 'pnpm bootstrap: got %s, want %s\n' "$actual" "$expected" >&2
	exit 1
fi

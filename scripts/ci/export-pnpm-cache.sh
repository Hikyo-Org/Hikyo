#!/bin/sh
set -eu

# pnpm chooses a store on the project's filesystem by default. In Actions
# containers that can be /__w/.pnpm-store rather than the restored HOME path.
# Keep the actual store explicit and the cache action's path relative so host
# and container readers use the same archive version.
workspace=${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required}
case "$workspace" in
	/*) ;;
	*) printf 'GITHUB_WORKSPACE must be an existing absolute directory\n' >&2; exit 1 ;;
esac
[ -d "$workspace" ] || {
	printf 'GITHUB_WORKSPACE must be an existing absolute directory\n' >&2
	exit 1
}
case "$workspace" in
	*'
'* | *"$(printf '\r')"*)
		printf 'GITHUB_WORKSPACE must fit one environment-file line\n' >&2
		exit 1
		;;
esac
export pnpm_config_store_dir="$workspace/.ci-cache/pnpm/store"
pnpm_version=$(pnpm --version)
case "$pnpm_version" in
	11.*) ;;
	*) printf 'CI pnpm cache requires pinned pnpm 11\n' >&2; exit 1 ;;
esac
case "$pnpm_version" in
	*[!0-9.]* | '') printf 'invalid pnpm version\n' >&2; exit 1 ;;
esac
actual_store=$(pnpm store path)
if [ "$actual_store" != "$pnpm_config_store_dir/v11" ]; then
	printf 'pnpm did not select the explicit CI store\n' >&2
	exit 1
fi
node_abi=$(node -p 'process.versions.modules')
case "$node_abi" in
	*[!0-9]* | '') printf 'invalid Node module ABI\n' >&2; exit 1 ;;
esac
printf 'pnpm_config_store_dir=%s\n' "$pnpm_config_store_dir" >>"${GITHUB_ENV:?GITHUB_ENV is required}"
{
	printf 'path=.ci-cache/pnpm/store\n'
	printf 'abi=node%s-pnpm%s\n' "$node_abi" "$pnpm_version"
} >>"${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
printf 'CI pnpm store: %s (Node ABI %s, pnpm %s)\n' "$actual_store" "$node_abi" "$pnpm_version"

#!/bin/sh
set -eu

CDPATH=
repo_root=$(cd -- "$(dirname "$0")/../.." && pwd)
# This shared bootstrap owns tools for both docs CI and Pages. The pinned
# Ubuntu container omits jq; policy validation itself must stay read-only.
if ! command -v jq >/dev/null 2>&1; then
	if [ "${GITHUB_ACTIONS:-}" = true ] && [ "${CI:-}" = true ] &&
		[ -f /.dockerenv ] && [ "$(id -u)" = 0 ] &&
		grep -qx 'ID=ubuntu' /etc/os-release &&
		grep -qx 'VERSION_ID="24.04"' /etc/os-release; then
		apt-get update
		apt-get install --yes --no-install-recommends jq
	else
		printf 'docs verification: jq is required; install jq locally before verify-docs.sh or pnpm docs verification\n' >&2
		exit 1
	fi
fi
command -v jq >/dev/null 2>&1 || {
	printf 'docs verification: jq provisioning did not provide jq\n' >&2
	exit 1
}

package_manager=$(node -p 'require(process.argv[1]).packageManager' \
	"$repo_root/docs/site/package.json")

case "$package_manager" in
	pnpm@*) ;;
	*)
		printf 'docs verification: expected a pinned pnpm packageManager, got %s\n' \
			"$package_manager" >&2
		exit 1
		;;
esac

"$repo_root/scripts/ci/install-corepack.sh"
corepack install --global "$package_manager"
pnpm --dir "$repo_root/docs/site" install --frozen-lockfile
node "$repo_root/scripts/ci/check-doc-status.mjs" --check --root "$repo_root"
"$repo_root/scripts/ci/check-doc-status_test.sh"
pnpm --dir "$repo_root/docs/site" peers check
pnpm --dir "$repo_root/docs/site" run verify
"$repo_root/scripts/ci/check-oss-policy_test.sh"
"$repo_root/scripts/ci/check-docs-live_test.sh"
"$repo_root/scripts/ci/check-fallback-channel-test_test.sh"

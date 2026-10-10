#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture_dir=$(mktemp -d)
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
mkdir -p "$fixture_dir/bin" "$fixture_dir/workspace with spaces"

cat >"$fixture_dir/bin/pnpm" <<'PNPM'
#!/bin/sh
set -eu
case "$*" in
	--version) printf '%s\n' 11.24.0 ;;
	'store path')
		if [ "${PNPM_FIXTURE_WRONG_PATH:-}" = true ]; then
			printf '%s\n' /unrelated/store/v11
		else
			printf '%s/v11\n' "$pnpm_config_store_dir"
		fi
		;;
	*) exit 2 ;;
esac
PNPM
cat >"$fixture_dir/bin/node" <<'NODE'
#!/bin/sh
set -eu
[ "$1" = -p ] && [ "$2" = process.versions.modules ]
printf '%s\n' 147
NODE
chmod +x "$fixture_dir/bin/pnpm" "$fixture_dir/bin/node"

export PATH="$fixture_dir/bin:$PATH"
export GITHUB_WORKSPACE="$fixture_dir/workspace with spaces"
export GITHUB_ENV="$fixture_dir/environment"
export GITHUB_OUTPUT="$fixture_dir/outputs"
# Inherited defaults cannot silently redirect the restored store.
export pnpm_config_store_dir=/unrelated/inherited/store
export npm_config_store_dir=/unrelated/legacy/store
"$script_dir/export-pnpm-cache.sh"
grep -Fx "pnpm_config_store_dir=$GITHUB_WORKSPACE/.ci-cache/pnpm/store" "$GITHUB_ENV"
grep -Fx 'path=.ci-cache/pnpm/store' "$GITHUB_OUTPUT"
grep -Fx 'abi=node147-pnpm11.24.0' "$GITHUB_OUTPUT"

export PNPM_FIXTURE_WRONG_PATH=true
if "$script_dir/export-pnpm-cache.sh" >"$fixture_dir/refusal.log" 2>&1; then
	printf 'pnpm cache fixture: accepted an unrelated actual store\n' >&2
	exit 1
fi
grep -F 'pnpm did not select the explicit CI store' "$fixture_dir/refusal.log"
unset PNPM_FIXTURE_WRONG_PATH
export GITHUB_WORKSPACE=relative/workspace
if "$script_dir/export-pnpm-cache.sh" >"$fixture_dir/refusal.log" 2>&1; then
	printf 'pnpm cache fixture: accepted a relative workspace\n' >&2
	exit 1
fi
grep -F 'GITHUB_WORKSPACE must be an existing absolute directory' "$fixture_dir/refusal.log"
printf 'pnpm cache fixture: explicit store, ABI and refusal boundaries verified\n'

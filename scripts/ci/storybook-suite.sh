#!/bin/sh
# The ci.yml `storybook` job's commands, in the same order, for the pre-push
# hook and the advisory workflow. storybook_suite_workflow_test.go fails when
# the two drift; edit both together. Run it from any directory.
set -eu

cd "$(dirname "$0")/../.."

fail() {
	printf 'storybook-suite: %s\n' "$*" >&2
	exit 1
}

command -v node >/dev/null 2>&1 || fail 'node is required'
command -v pnpm >/dev/null 2>&1 || fail 'pnpm is required'
node_version=$(node --version) || fail 'cannot determine Node version'
[ "$node_version" = "v$(tr -d '\r\n' <.nvmrc)" ] || fail "Node $node_version differs from .nvmrc; select the pinned contributor toolchain"
expected_pnpm=$(sed -n 's/^[[:space:]]*"packageManager":[[:space:]]*"pnpm@\([^"]*\)".*/\1/p' package.json)
[ -n "$expected_pnpm" ] || fail 'package.json has no pnpm packageManager'
pnpm_version=$(pnpm --version) || fail 'cannot determine pnpm version'
[ "$pnpm_version" = "$expected_pnpm" ] || fail "pnpm $pnpm_version differs from packageManager $expected_pnpm; select the pinned contributor toolchain"

# step DIR COMMAND...: one ci.yml step, run from DIR under the repository root.
step() {
	dir=$1
	shift
	printf '==> %s: %s\n' "$dir" "$*" >&2
	(cd "$dir" && "$@")
}

step clients/ts pnpm install --frozen-lockfile
step web pnpm install --frozen-lockfile
step web pnpm run design:export
step web pnpm run test-storybook
step web pnpm run test-storybook:light
step web pnpm exec storybook build
step web pnpm run check:storybook-docs
step web pnpm run test-storybook:theme
step web pnpm run build:demo
step web pnpm run test:demo

printf '==> web: every linked design image ships in the static build\n' >&2
cd web
for png in design/exports/*.png; do
	[ -e "$png" ] || continue
	[ -f "storybook-static/design/$(basename "$png")" ] || { echo "missing storybook-static/design/$(basename "$png")"; exit 1; }
done

#!/usr/bin/env bash
# The freshness check must pass on a current tree and fail on a stale, extra, or missing generated file.
# pnpm is a stub that "generates" the contract's output from $fresh; the real generator runs in CI.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-client-generated-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/fresh/client" "$work/repo/clients/ts/src"
printf 'types v2\n' >"$work/fresh/types.gen.ts"
printf 'client\n' >"$work/fresh/client/index.ts"
cat >"$work/bin/pnpm" <<STUB
#!/bin/sh
[ "\$*" = '--dir clients/ts run generate' ] || exit 64
rm -rf clients/ts/src/generated
cp -R "$work/fresh" clients/ts/src/generated
STUB
chmod +x "$work/bin/pnpm"
cd "$work/repo"
reset_tree() {
	rm -rf clients/ts/src/generated
	cp -R "$work/fresh" clients/ts/src/generated
}
check() { PATH="$work/bin:$PATH" bash "$script_dir/check-client-generated.sh" >"$work/output" 2>&1; }
expect_fail() {
	if check; then cat "$work/output" >&2; printf 'client generated fixture: %s passed, want failure\n' "$1" >&2; exit 1; fi
	grep -F 'is stale against api/openapi.yaml' "$work/output" >/dev/null
}
# The check judges what the generator emits against the tree as it stands, with no
# repository state involved, so uncommitted regenerated work passes.
reset_tree
check
printf 'types v1\n' >clients/ts/src/generated/types.gen.ts
expect_fail 'a stale file'
reset_tree
printf 'leftover\n' >clients/ts/src/generated/removed-from-contract.gen.ts
expect_fail 'a file the generator no longer emits'
reset_tree
rm clients/ts/src/generated/client/index.ts
expect_fail 'a deleted generated file'
reset_tree
check
printf 'client generated fixture: fresh, stale, leftover and missing generated trees judged correctly\n'

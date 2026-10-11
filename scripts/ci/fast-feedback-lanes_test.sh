#!/usr/bin/env bash
# Execute the actual helper and prove split-lane commands conserve default all.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
owner=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-feedback-lanes.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo" "$work/bin" "$work/tools/scripts/ci" "$work/tools/docs/site/scripts"
cp "$owner/docs/site/scripts/prepare-content.mjs" "$work/tools/docs/site/scripts/"
cd "$work/repo"
git init -q
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.invalid
export GIT_COMMITTER_NAME=$GIT_AUTHOR_NAME GIT_COMMITTER_EMAIL=$GIT_AUTHOR_EMAIL
commit_fixture() {
	git add .
	local tree revision
	tree=$(git write-tree)
	if git rev-parse --verify HEAD >/dev/null 2>&1; then
		revision=$(printf 'fixture\n' | git commit-tree "$tree" -p HEAD)
	else
		revision=$(printf 'fixture\n' | git commit-tree "$tree")
	fi
	git update-ref HEAD "$revision"
}
for path in SECURITY.md SUPPORT.md GOVERNANCE.md TRADEMARK.md CONTRIBUTING.md LICENSE docs/status/README.md docs/release/signing.md docs/release/online-signing.md docs/release/legacy-offline-signing.md install/upgrade-nightly.sh; do
	mkdir -p "$(dirname "$path")"
	cp "$owner/$path" "$path"
done
printf '%s\n' "$(node --version | cut -c2-)" > .nvmrc
printf '{"packageManager":"pnpm@11.24.0"}\n' >package.json
printf 'module fixture.invalid/lanes\n\ngo 1.27.2\n' >go.mod
commit_fixture
FAST_FEEDBACK_BASE=$(git rev-parse HEAD)
export FAST_FEEDBACK_BASE
unset FAST_FEEDBACK_HEAD FAST_FEEDBACK_DEADLINE FAST_FEEDBACK_TOOLS_DIR
mkdir -p web/node_modules clients/ts/node_modules .github/workflows docs/site/scripts
printf 'package lanes\n' >fixture.go
printf '#!/bin/sh\nset -eu\n' >fixture.sh
printf 'export const fixture = true;\n' >web/fixture.ts
printf 'export const fixture = true;\n' >clients/ts/fixture.ts
printf '# fixture\n' >.github/workflows/fixture.yml
printf '// docs selection\n' >docs/site/scripts/fixture.mjs
commit_fixture
FIXTURE_REAL_GO=$(command -v go)
# Resolve the real executable before prepending stubs: local Node wrappers
# may search PATH again and would otherwise recurse through this fixture.
FIXTURE_REAL_NODE=$(node -p 'process.execPath')
FIXTURE_REAL_SHELLCHECK=$(command -v shellcheck)
export FIXTURE_REAL_GO FIXTURE_REAL_NODE FIXTURE_REAL_SHELLCHECK FIXTURE_WORK=$work
cat >"$work/bin/go" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" = env ]; then exec "$FIXTURE_REAL_GO" "$@"; fi
printf 'go %s\n' "$*" >>"$FIXTURE_WORK/commands"
[ "${FIXTURE_FAILURE:-}" != compile ] || exit 31
STUB
cat >"$work/bin/pnpm" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" = --version ]; then printf '11.24.0\n'; exit; fi
printf 'pnpm %s\n' "$*" >>"$FIXTURE_WORK/commands"
[ "${FIXTURE_FAILURE:-}" != frontend ] || exit 32
STUB
cat >"$work/bin/node" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" != --version ]; then printf 'docs\n' >>"$FIXTURE_WORK/commands"; fi
exec "$FIXTURE_REAL_NODE" "$@"
STUB
cat >"$work/bin/shellcheck" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" != --version ]; then printf 'shellcheck\n' >>"$FIXTURE_WORK/commands"; fi
exec "$FIXTURE_REAL_SHELLCHECK" "$@"
STUB
cat >"$work/tools/scripts/ci/run-go-tool.sh" <<'STUB'
#!/usr/bin/env bash
set -eu
[ "$1" = actionlint ]
printf 'actionlint\n' >>"$FIXTURE_WORK/commands"
STUB
chmod +x "$work/bin/"* "$work/tools/scripts/ci/run-go-tool.sh"
export PATH="$work/bin:$PATH" FAST_FEEDBACK_TOOLS_DIR="$work/tools/scripts/ci"
run_lane() {
	: >"$work/commands"
	bash "$script_dir/fast-feedback.sh" "$@" >"$work/output" 2>&1
	sort "$work/commands"
}
run_lane >"$work/all"
FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) run_lane --lane=go >"$work/go"
FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) run_lane --lane=frontend >"$work/frontend"
cat "$work/go" "$work/frontend" | sort >"$work/union"
cmp "$work/all" "$work/union"
diff -u <(printf '%s\n' 'actionlint' 'docs' 'go test -c -o /dev/null ./.' 'shellcheck' | sort) "$work/go"
diff -u <(printf '%s\n' 'pnpm --dir clients/ts run typecheck' 'pnpm --dir clients/ts run test' 'pnpm --dir web run typecheck' 'pnpm --dir web run lint' 'pnpm --dir web run test' | sort) "$work/frontend"
refuse() {
	if bash "$script_dir/fast-feedback.sh" "$@" >"$work/output" 2>&1; then
		printf 'feedback lane fixture unexpectedly passed: %s\n' "$*" >&2; exit 1
	fi
	if grep -F 'selected checks passed' "$work/output"; then exit 1; fi
}
for lane in '' yes GO frontend,go; do
	refuse "--lane=$lane"
	grep -F 'invalid lane' "$work/output" >/dev/null
done
FIXTURE_FAILURE=compile FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) refuse --lane=go
FIXTURE_FAILURE=frontend FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) refuse --lane=frontend
# A frontend-only success cannot conceal invalid Go formatting in the other lane.
printf 'package lanes\nvar bad=1\n' >fixture.go
commit_fixture
run_lane --lane=frontend >/dev/null
refuse --lane=go
refuse
printf 'fast-feedback lane fixtures: complete union, ownership, invalid lanes and independent failure refusal passed\n'

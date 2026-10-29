#!/usr/bin/env bash
# Execute the actual helper and prove split-lane commands conserve default all.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
owner=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-feedback-lanes.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo" "$work/bin" "$work/tools/scripts/ci" "$work/tools/docs/site/scripts"
cp "$script_dir/feedback-plan.sh" "$work/tools/scripts/ci/"
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
for path in SECURITY.md SUPPORT.md GOVERNANCE.md TRADEMARK.md CONTRIBUTING.md LICENSE docs/status/README.md docs/release/signing.md docs/release/online-signing.md docs/release/apple-signing.md docs/release/legacy-offline-signing.md install/upgrade-nightly.sh; do
	mkdir -p "$(dirname "$path")"
	cp "$owner/$path" "$path"
done
cp "$owner/.nvmrc" .nvmrc
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
FIXTURE_REAL_GOFMT="$("$FIXTURE_REAL_GO" env GOROOT)/bin/gofmt"
# Resolve the real executable before prepending stubs: local Node wrappers
# may search PATH again and would otherwise recurse through this fixture.
FIXTURE_REAL_NODE=$(node -p 'process.execPath')
FIXTURE_REAL_SHELLCHECK=$(command -v shellcheck)
export FIXTURE_REAL_GO FIXTURE_REAL_GOFMT FIXTURE_REAL_NODE FIXTURE_REAL_SHELLCHECK FIXTURE_WORK=$work
mkdir -p "$work/goroot/bin"
cat >"$work/goroot/bin/gofmt" <<'STUB'
#!/usr/bin/env bash
set -eu
printf 'go-format\n' >>"$FIXTURE_WORK/checks"
[ "${FIXTURE_FAILURE:-}" != go-format ] || exit 40
exec "$FIXTURE_REAL_GOFMT" "$@"
STUB
chmod +x "$work/goroot/bin/gofmt"
cat >"$work/bin/go" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" = env ]; then
	[ "${FIXTURE_FAILURE:-}" != go-env ] || exit 33
	[ "$#" -eq 2 ] && [ "$2" = GOROOT ] || exit 34
	printf '%s\n' "$FIXTURE_WORK/goroot"
	exit
fi
case "$1" in
	test) printf 'go-compile\n' >>"$FIXTURE_WORK/checks" ;;
	mod) printf 'go-mod-verify\n' >>"$FIXTURE_WORK/checks" ;;
	*) exit 34 ;;
esac
printf 'go %s\n' "$*" >>"$FIXTURE_WORK/commands"
[ "${FIXTURE_FAILURE:-}" != compile ] || exit 31
STUB
cat >"$work/bin/pnpm" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" = --version ]; then
	[ "${FIXTURE_FAILURE:-}" != pnpm-version ] || { printf '0.0.0\n'; exit; }
	printf '11.24.0\n'
	[ "${FIXTURE_FAILURE:-}" != pnpm-status ] || exit 38
	exit
fi
case "$2" in clients/ts) owner=client ;; web) owner=web ;; *) exit 35 ;; esac
printf '%s-%s\n' "$owner" "$4" >>"$FIXTURE_WORK/checks"
printf 'pnpm %s\n' "$*" >>"$FIXTURE_WORK/commands"
[ "${FIXTURE_FAILURE:-}" != frontend ] || exit 32
STUB
cat >"$work/bin/node" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" != --version ]; then
	printf 'docs\n' >>"$FIXTURE_WORK/commands"
	printf 'docs\n' >>"$FIXTURE_WORK/checks"
	[ "${FIXTURE_FAILURE:-}" != docs ] || exit 36
elif [ "${FIXTURE_FAILURE:-}" = node-version ]; then printf 'v0.0.0\n'; exit
elif [ "${FIXTURE_FAILURE:-}" = node-status ]; then "$FIXTURE_REAL_NODE" "$@"; exit 39
fi
exec "$FIXTURE_REAL_NODE" "$@"
STUB
cat >"$work/bin/shellcheck" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" != --version ]; then
	printf 'shellcheck\n' >>"$FIXTURE_WORK/commands"
	printf 'shellcheck\n' >>"$FIXTURE_WORK/checks"
elif [ "${FIXTURE_FAILURE:-}" = shellcheck-version ]; then printf 'version: 0.0.0\n'; exit
fi
exec "$FIXTURE_REAL_SHELLCHECK" "$@"
STUB
cat >"$work/tools/scripts/ci/run-go-tool.sh" <<'STUB'
#!/usr/bin/env bash
set -eu
[ "$1" = actionlint ]
printf 'actionlint\n' >>"$FIXTURE_WORK/commands"
printf 'actionlint\n' >>"$FIXTURE_WORK/checks"
[ "${FIXTURE_FAILURE:-}" != actionlint ] || exit 37
STUB
chmod +x "$work/bin/"* "$work/tools/scripts/ci/run-go-tool.sh"
export PATH="$work/bin:$PATH" FAST_FEEDBACK_TOOLS_DIR="$work/tools/scripts/ci"
run_lane() {
	: >"$work/commands"
	: >"$work/checks"
	bash "$script_dir/fast-feedback.sh" "$@" >"$work/output" 2>&1
	sort "$work/commands"
}
assert_planned_checks() {
	local selected=$1
	bash "$script_dir/fast-feedback.sh" --plan >"$work/plan"
	jq -r --arg lane "$selected" '.lanes[$lane].checks[].id' "$work/plan" >"$work/planned-checks"
	cmp "$work/planned-checks" "$work/checks"
}
run_lane >"$work/all"
FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) run_lane --lane=go >"$work/go"
FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) run_lane --lane=frontend >"$work/frontend"
cat "$work/go" "$work/frontend" | sort >"$work/union"
cmp "$work/all" "$work/union"
diff -u <(printf '%s\n' 'actionlint' 'docs' 'go test -c -vet=off -gcflags=all=-dwarf=false -ldflags=-w -o /dev/null ./.' 'shellcheck' | sort) "$work/go"
diff -u <(printf '%s\n' 'pnpm --dir clients/ts run typecheck' 'pnpm --dir clients/ts run test' 'pnpm --dir web run typecheck' 'pnpm --dir web run lint' 'pnpm --dir web run test' | sort) "$work/frontend"
# The hosted Go scopes partition package owners and read-only tooling. Local
# unscoped Go feedback retains its complete single build graph.
mkdir -p scripts/tooling
printf 'package tooling\n' >scripts/tooling/fixture.go
printf '\n// Select dependency integrity.\n' >>go.mod
commit_fixture
run_lane --lane=go >"$work/scope-all"
run_lane --lane=go --go-scope=application >"$work/application"
assert_planned_checks go
run_lane --lane=go --go-scope=tooling >"$work/tooling"
assert_planned_checks tooling
run_lane --lane=frontend >"$work/frontend"
assert_planned_checks frontend
diff -u <(printf '%s\n' 'go mod verify' 'go test -c -vet=off -gcflags=all=-dwarf=false -ldflags=-w -o /dev/null ./.' | sort) "$work/application"
diff -u <(printf '%s\n' 'actionlint' 'docs' 'go test -c -vet=off -gcflags=all=-dwarf=false -ldflags=-w -o /dev/null ./scripts/tooling' 'shellcheck' | sort) "$work/tooling"
compile_prefix='go test -c -vet=off -gcflags=all=-dwarf=false -ldflags=-w -o /dev/null '
sed -n "s|^$compile_prefix||p" "$work/scope-all" | tr ' ' '\n' | sort >"$work/all-owners"
cat "$work/application" "$work/tooling" | sed -n "s|^$compile_prefix||p" | tr ' ' '\n' | sort >"$work/split-owners"
cmp "$work/all-owners" "$work/split-owners"
grep -v '^go test ' "$work/scope-all" >"$work/all-tooling"
cat "$work/application" "$work/tooling" | grep -v '^go test ' | sort >"$work/split-tooling"
cmp "$work/all-tooling" "$work/split-tooling"
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
for scope in '' unknown APPLICATION application,tooling; do
	refuse --lane=go "--go-scope=$scope"
	grep -F 'invalid Go scope' "$work/output" >/dev/null
done
refuse --lane=frontend --go-scope=application
grep -F 'frontend lane cannot select a Go scope' "$work/output" >/dev/null
FIXTURE_FAILURE=compile FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) refuse --lane=go
FIXTURE_FAILURE=compile FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) refuse --lane=go --go-scope=application
FIXTURE_FAILURE=compile FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) refuse --lane=go --go-scope=tooling
FIXTURE_FAILURE=frontend FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 20)) refuse --lane=frontend
for failure in go-env go-format shellcheck-version actionlint docs node-version node-status; do
	FIXTURE_FAILURE=$failure refuse --lane=go --go-scope=tooling
	case "$failure" in
		shellcheck-version) grep -F 'ShellCheck 0.11.0 is required' "$work/output" >/dev/null ;;
		node-version) grep -F 'Node differs from .nvmrc' "$work/output" >/dev/null ;;
		node-status) grep -F 'cannot determine Node version' "$work/output" >/dev/null ;;
	esac
done
FIXTURE_FAILURE=pnpm-version refuse --lane=frontend
grep -F 'pnpm differs from packageManager' "$work/output" >/dev/null
FIXTURE_FAILURE=pnpm-status refuse --lane=frontend
grep -F 'cannot determine pnpm version' "$work/output" >/dev/null
# A frontend-only success cannot conceal invalid Go formatting in the other lane.
printf 'package lanes\nvar bad=1\n' >fixture.go
commit_fixture
run_lane --lane=frontend >/dev/null
refuse --lane=go
refuse
printf 'fast-feedback lane fixtures: complete lane/scope unions, ownership, invalid selections and independent failure refusal passed\n'

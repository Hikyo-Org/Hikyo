#!/usr/bin/env bash
# Developer feedback only. Full current-main candidate CI remains the merge gate.
# Node dependencies must already be installed; no source fixes or generation.
# Go checks may download modules/toolchains and populate normal Go caches.
set -euo pipefail

working_tree=false
plan_only=false
for argument in "$@"; do
	case "$argument" in
		--working-tree) working_tree=true ;;
		--plan) plan_only=true ;;
		*) printf 'usage: %s [--working-tree] [--plan]\n' "$0" >&2; exit 2 ;;
	esac
done

fail() { printf 'fast-feedback: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1; prepare the pinned contributor toolchain first"; }
repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"
base=$(git rev-parse --verify "${FAST_FEEDBACK_BASE:-hikyo-upstream/main}^{commit}") || fail 'missing comparison base; fetch canonical main using CONTRIBUTING.md'
head=$(git rev-parse --verify "${FAST_FEEDBACK_HEAD:-HEAD}^{commit}") || fail 'invalid feedback revision'
comparison=$(git merge-base "$base" "$head") || fail 'base and revision have no merge base'
[ "$(git rev-parse HEAD)" = "$head" ] || fail 'checkout differs from requested revision; use a detached checkout of that revision'
if [ "$working_tree" = false ]; then
	[ -z "$(git status --porcelain --untracked-files=normal)" ] || fail 'checkout is dirty; use --working-tree for explicitly local feedback'
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-fast-feedback.XXXXXX")
trap 'rm -rf "$work"' EXIT
if [ "$working_tree" = true ]; then
	# Preserve both owners of a cross-package rename as deletion and addition.
	git diff --no-renames --name-only -z "$comparison" >"$work/changed"
	git ls-files --others --exclude-standard -z >"$work/untracked"
	cat "$work/untracked" >>"$work/changed"
else
	git diff --no-renames --name-only -z "$comparison" "$head" >"$work/changed"
fi

web=false
client=false
workflows=false
docs=false
all_go=false
go_files=()
go_packages=()
shell_files=()
while IFS= read -r -d '' path; do
	case "$path" in
		web/*) web=true ;;
		clients/ts/* | api/openapi.yaml) client=true; web=true ;;
		.nvmrc | package.json | pnpm-lock.yaml | pnpm-workspace.yaml) client=true; web=true ;;
	esac
	case "$path" in
		SECURITY.md | SUPPORT.md | GOVERNANCE.md | TRADEMARK.md | CONTRIBUTING.md | LICENSE | docs/status/* | docs/release/* | docs/site/* | install/upgrade-nightly.sh | .nvmrc | package.json | pnpm-lock.yaml | pnpm-workspace.yaml) docs=true ;;
	esac
	case "$path" in
		.github/workflows/*.yml | .github/workflows/*.yaml) workflows=true ;;
		go.mod | go.sum) all_go=true ;;
	esac
	case "$path" in
		*.go)
			package="./$(dirname "$path")"
			remaining_go=("$package"/*.go)
			if [ -f "${remaining_go[0]}" ] && ! printf '%s\n' "${go_packages[@]:-}" | grep -Fx -- "$package" >/dev/null; then go_packages+=("$package"); fi
			;;
	esac
	[ -f "$path" ] || continue
	case "$path" in
		*.go)
			go_files+=("./$path")
			;;
		*.sh | .githooks/* | install/install.sh.in) shell_files+=("./$path") ;;
	esac
done <"$work/changed"

if [ "$plan_only" = true ]; then
	printf '{"web":%s,"client":%s,"workflows":%s,"go":%s,"shell":%s,"docs":%s}\n' \
		"$web" "$client" "$workflows" "$([ "${#go_packages[@]}" -gt 0 ] || [ "$all_go" = true ] && printf true || printf false)" \
		"$([ "${#shell_files[@]}" -gt 0 ] && printf true || printf false)" "$docs"
	exit 0
fi

started=$SECONDS
run() {
	local check_started=$SECONDS
	printf 'fast-feedback: running %s\n' "$*"
	"$@"
	printf 'fast-feedback: completed in %ss\n' "$((SECONDS - check_started))"
}
printf 'fast-feedback: base=%s revision=%s working-tree=%s\n' "$base" "$head" "$working_tree"
if [ "$working_tree" = true ]; then
	run git diff --check "$comparison"
	while IFS= read -r -d '' path; do
		[ -f "$path" ] || continue
		status=0
		git diff --no-index --check -- /dev/null "$path" >"$work/whitespace" || status=$?
		[ ! -s "$work/whitespace" ] || { cat "$work/whitespace" >&2; fail 'untracked file whitespace differs'; }
		[ "$status" -le 1 ] || fail "cannot check untracked file $path"
	done <"$work/untracked"
else
	run git diff --check "$comparison" "$head"
fi

if [ "${#go_files[@]}" -gt 0 ]; then
	need go
	gofmt="$(go env GOROOT)/bin/gofmt"
	unformatted=$("$gofmt" -l "${go_files[@]}")
	[ -z "$unformatted" ] || { printf '%s\n' "$unformatted" >&2; fail 'changed Go files need gofmt'; }
	printf 'fast-feedback: Go formatting passed (%s files)\n' "${#go_files[@]}"
fi
if [ "${#go_packages[@]}" -gt 0 ] || [ "$all_go" = true ]; then
	need go
	if [ "$all_go" = true ]; then
		# Dependency integrity is quick feedback, not compatibility evidence.
		# The full candidate still compiles and tests every package.
		run go mod verify
	fi
	index=0
	for package in "${go_packages[@]:-}"; do
		[ -n "$package" ] || continue
		run go test -c -o "$work/compile-$index.test" "$package"
		index=$((index + 1))
	done
fi
if [ "${#shell_files[@]}" -gt 0 ]; then
	need shellcheck
	shellcheck --version | grep -Fx 'version: 0.11.0' >/dev/null || fail 'ShellCheck 0.11.0 is required, matching CI'
	run shellcheck -- "${shell_files[@]}"
fi
if [ "$workflows" = true ]; then
	need go
	# CI may select immutable merge tooling while checking PR source. Run its Go
	# module from that checkout, passing only the source workflow paths as data.
	tools_dir=$(CDPATH='' cd -- "${FAST_FEEDBACK_TOOLS_DIR:-$repo_root/scripts/ci}" && pwd)
	workflow_files=()
	for path in "$repo_root"/.github/workflows/*.yml "$repo_root"/.github/workflows/*.yaml; do
		[ -f "$path" ] && workflow_files+=("$path")
	done
	[ "${#workflow_files[@]}" -gt 0 ] || fail 'no workflows remain to validate'
	(cd "$tools_dir/../.."; run "$tools_dir/run-go-tool.sh" actionlint "${workflow_files[@]}")
fi
if [ "$docs" = true ]; then
	need node
	[ "$(node --version)" = "v$(tr -d '\r\n' <.nvmrc)" ] || fail 'Node differs from .nvmrc; select the pinned contributor toolchain'
	# Use the checker with the supported read-only interface from the tooling
	# checkout; an older source checkout may have a generation-only script.
	docs_checker="${FAST_FEEDBACK_TOOLS_DIR:-$repo_root/scripts/ci}/../../docs/site/scripts/prepare-content.mjs"
	run node "$docs_checker" --check --source-root "$repo_root"
fi
if [ "$web" = true ] || [ "$client" = true ]; then
	[ -d clients/ts/node_modules ] || fail 'client dependencies missing; run pnpm --dir clients/ts install --frozen-lockfile before feedback'
	if [ "$web" = true ]; then
		[ -d web/node_modules ] || fail 'web dependencies missing; run pnpm --dir web install --frozen-lockfile before feedback'
	fi
	need node
	need pnpm
	[ "$(node --version)" = "v$(tr -d '\r\n' <.nvmrc)" ] || fail 'Node differs from .nvmrc; select the pinned contributor toolchain'
	need jq
	expected_pnpm=$(jq -er '.packageManager | select(startswith("pnpm@")) | ltrimstr("pnpm@")' package.json)
	[ "$(pnpm --version)" = "$expected_pnpm" ] || fail 'pnpm differs from packageManager; select the pinned contributor toolchain'
fi
if [ "$client" = true ]; then
	run pnpm --dir clients/ts run typecheck
	run pnpm --dir clients/ts run test
fi
if [ "$web" = true ]; then
	run pnpm --dir web run typecheck
	run pnpm --dir web run lint
	run pnpm --dir web run test
fi
printf 'fast-feedback: selected checks passed in %ss; full candidate CI remains required\n' "$((SECONDS - started))"

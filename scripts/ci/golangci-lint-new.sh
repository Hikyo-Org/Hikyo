#!/bin/sh
# New-code-only golangci-lint ratchet: lint the packages that changed since the
# merge base of BASE and HEAD and report only issues on changed lines. The
# existing baseline is not enforced. Used by the pre-push hook and by
# ci-golangci.yml (advisory); never part of a required job.
# Usage: scripts/ci/golangci-lint-new.sh BASE
# It lints the work tree it runs in (the hook runs it in a detached worktree at
# the pushed commit); the tool pin and .golangci.yml come from this script's checkout.
set -eu

# Exit codes: 1 findings (golangci-lint's own), 64 this script could not set up
# (the hook tells the two apart); golangci-lint's other codes pass through.
fail() {
	printf 'golangci-lint-new: %s\n' "$*" >&2
	exit 64
}

[ "$#" -eq 1 ] || { printf 'usage: %s BASE\n' "$0" >&2; exit 2; }
base=$1
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
config=$script_dir/../../.golangci.yml
cd "$(git rev-parse --show-toplevel)"

merge_base=$(git merge-base "$base" HEAD) || fail "no merge base between $base and HEAD; fetch $base first"

# Inputs that change what every package is checked against: lint all of them.
# ci-golangci.yml triggers on exactly these (plus **/*.go); a Go test keeps them equal.
everything='go.mod go.sum .golangci.yml scripts/ci/golangci-lint-version scripts/ci/golangci-lint-new.sh scripts/ci/run-go-tool.sh'

# shellcheck disable=SC2086 # the paths contain no whitespace or glob characters
if [ -n "$(git diff --name-only "$merge_base" HEAD -- $everything)" ]; then
	set -- ./...
else
	dirs=$(mktemp "${TMPDIR:-/tmp}/golangci-dirs.XXXXXX") || fail 'cannot create a temporary file'
	trap 'rm -f "$dirs"' EXIT HUP INT TERM
	# Directories of every changed Go file (names with newlines are unsupported).
	git -c core.quotePath=false diff --name-only "$merge_base" HEAD -- '*.go' |
		while IFS= read -r file; do dirname "$file"; done | sort -u >"$dirs"
	set --
	while IFS= read -r dir; do
		# Gone with its last file, or only testdata: nothing to load.
		[ -d "$dir" ] || continue
		case "/$dir/" in (*/testdata/*) continue ;; esac
		# A directory with no Go file left, or none that builds on this platform
		# (build constraints), makes golangci-lint fail with "no go files to
		# analyze". -e keeps go list quiet about that; any other go failure aborts.
		buildable=$(go list -e -f '{{if or .GoFiles .CgoFiles .TestGoFiles .XTestGoFiles}}y{{end}}' "./$dir") || fail "go list failed for ./$dir"
		[ -n "$buildable" ] || continue
		if [ "$dir" = . ]; then set -- "$@" .; else set -- "$@" "./$dir"; fi
	done <"$dirs"
	rm -f "$dirs"
	if [ "$#" -eq 0 ]; then
		printf 'golangci-lint-new: no lintable Go inputs changed since %s; nothing to lint\n' "$base" >&2
		exit 0
	fi
fi

printf 'golangci-lint-new: linting new code since %s (%s)\n' "$base" "$*" >&2
# The config next to the work tree's root is found on its own (and findings get
# paths relative to it); this script's checkout supplies one only when the tree has none.
if [ -f .golangci.yml ]; then
	exec "$script_dir/run-go-tool.sh" golangci-lint run -j 2 --new-from-merge-base="$base" "$@"
fi
exec "$script_dir/run-go-tool.sh" golangci-lint run -j 2 --config "$config" --new-from-merge-base="$base" "$@"

#!/bin/sh
# golangci-lint ratchet over the packages that changed since the merge base of
# BASE and HEAD, in two passes: .golangci.yml reports only issues on changed
# lines (its existing baseline is not enforced), and .golangci-strict.yml holds
# the zero-baseline rules (wastedassign, unused, SA4006, SA4009) for every whole
# changed package. Used by the pre-push hook and by ci-golangci.yml (advisory);
# never part of a required job.
# Usage: scripts/ci/golangci-lint-new.sh BASE
# It lints the work tree it runs in (the hook runs this script from a detached
# worktree at the pushed commit, so the tool pin, both configs and this script
# all come from that commit).
set -eu

# Exit 64: this script could not set up. Any other non-zero code comes from
# golangci-lint run through `go run`, which maps every tool exit to 1, so
# findings and tool failures look alike.
fail() {
	printf 'golangci-lint-new: %s\n' "$*" >&2
	exit 64
}

[ "$#" -eq 1 ] || { printf 'usage: %s BASE\n' "$0" >&2; exit 2; }
base=$1
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd) || fail 'cannot locate this script'
config=$script_dir/../../.golangci.yml
# Every command whose status matters is run on its own, never inside a test or
# a pipeline, where its failure would read as "no changes".
top=$(git rev-parse --show-toplevel) || fail 'not inside a git work tree'
cd "$top" || fail "cannot enter $top"

merge_base=$(git merge-base "$base" HEAD) || fail "no merge base between $base and HEAD; fetch $base first"

# Inputs that change what every package is checked against: lint all of them.
# ci-golangci.yml triggers on exactly these (plus **/*.go); a Go test keeps them equal.
everything='go.mod go.sum .golangci.yml .golangci-strict.yml scripts/ci/golangci-lint-version scripts/ci/golangci-lint-new.sh scripts/ci/run-go-tool.sh'

# shellcheck disable=SC2086 # the paths contain no whitespace or glob characters
changed_inputs=$(git diff --name-only "$merge_base" HEAD -- $everything) || fail "git diff failed against $merge_base"
if [ -n "$changed_inputs" ]; then
	set -- ./...
else
	files=$(mktemp "${TMPDIR:-/tmp}/golangci-files.XXXXXX") || fail 'cannot create a temporary file'
	dirs=$(mktemp "${TMPDIR:-/tmp}/golangci-dirs.XXXXXX") || { rm -f "$files"; fail 'cannot create a temporary file'; }
	trap 'rm -f "$files" "$dirs" "$dirs.raw"' EXIT
	trap 'exit 130' HUP INT TERM
	# Directories of every changed Go file (names with newlines are unsupported).
	# --no-renames lists a moved file under its old directory too, which must be
	# linted for what the move left behind.
	git -c core.quotePath=false diff --name-only --no-renames "$merge_base" HEAD -- '*.go' >"$files" || fail "git diff failed against $merge_base"
	while IFS= read -r file; do
		dir=$(dirname "$file") || fail "dirname failed for $file"
		printf '%s\n' "$dir"
	done <"$files" >"$dirs.raw" || fail 'cannot list changed directories'
	sort -u "$dirs.raw" >"$dirs" || fail 'cannot sort changed directories'
	rm -f "$dirs.raw"
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
	rm -f "$files" "$dirs"
	if [ "$#" -eq 0 ]; then
		printf 'golangci-lint-new: no lintable Go inputs changed since %s; nothing to lint\n' "$base" >&2
		exit 0
	fi
fi

# Two passes over the same packages; the second runs even when the first fails.
# Either status, other than this script's own 64, is the tool's and is returned.
printf 'golangci-lint-new: new code since %s (%s)\n' "$base" "$*" >&2
# golangci-lint finds .golangci.yml at the work tree's root by itself, and then
# reports paths relative to it. --config is only the fallback for a tree without
# one: the config of the checkout this script lives in. A tree without
# .golangci-strict.yml predates the zero-baseline fixes, so the strict pass is
# skipped there instead of blocking on findings the push did not add.
new_status=0
if [ -f .golangci.yml ]; then
	"$script_dir/run-go-tool.sh" golangci-lint run -j 2 --new-from-merge-base="$base" "$@" || new_status=$?
else
	"$script_dir/run-go-tool.sh" golangci-lint run -j 2 --config "$config" --new-from-merge-base="$base" "$@" || new_status=$?
fi
strict_status=0
if [ -f .golangci-strict.yml ]; then
	printf 'golangci-lint-new: strict rules on whole packages (%s)\n' "$*" >&2
	"$script_dir/run-go-tool.sh" golangci-lint run -j 2 --config .golangci-strict.yml "$@" || strict_status=$?
else
	printf 'golangci-lint-new: no .golangci-strict.yml in this tree; skipping the strict pass\n' >&2
fi
[ "$new_status" -eq 0 ] || exit "$new_status"
exit "$strict_status"

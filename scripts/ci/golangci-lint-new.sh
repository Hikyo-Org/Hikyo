#!/bin/sh
# New-code-only golangci-lint ratchet: lint the packages that changed since the
# merge base of BASE and HEAD and report only issues on changed lines. The
# existing baseline is not enforced. Used by the pre-push hook and by
# ci-golangci.yml (advisory); never part of a required job.
# Usage: scripts/ci/golangci-lint-new.sh BASE
set -eu

[ "$#" -eq 1 ] || { printf 'usage: %s BASE\n' "$0" >&2; exit 2; }
cd "$(dirname "$0")/../.."

merge_base=$(git merge-base "$1" HEAD) || {
	printf 'golangci-lint-new: no merge base between %s and HEAD; fetch %s first\n' "$1" "$1" >&2
	exit 1
}

# Inputs that change what every package is checked against: lint all of them.
# ci-golangci.yml triggers on exactly these (plus **/*.go); a Go test keeps them equal.
everything='go.mod go.sum .golangci.yml scripts/ci/golangci-lint-version scripts/ci/golangci-lint-new.sh scripts/ci/run-go-tool.sh'

# shellcheck disable=SC2086 # the paths contain no whitespace or glob characters
if [ -n "$(git diff --name-only "$merge_base" HEAD -- $everything)" ]; then
	packages=./...
else
	# Directories of every changed Go file; one that no longer exists (all its
	# files deleted) has nothing to load and is skipped.
	packages=$(git diff --name-only "$merge_base" HEAD -- '*.go' |
		while IFS= read -r file; do
			dir=$(dirname "$file")
			[ -d "$dir" ] || continue
			# The go tool ignores testdata trees; they are not packages.
			case "/$dir/" in (*/testdata/*) continue ;; esac
			printf './%s\n' "$dir"
		done | sort -u | sed 's|^\./\.$|.|')
	if [ -z "$packages" ]; then
		printf 'golangci-lint-new: no Go inputs changed since %s; nothing to lint\n' "$1" >&2
		exit 0
	fi
fi

printf 'golangci-lint-new: linting new code since %s (%s)\n' "$1" "$(printf '%s' "$packages" | tr '\n' ' ')" >&2
# shellcheck disable=SC2086 # one pattern per line; no pattern contains whitespace
exec scripts/ci/run-go-tool.sh golangci-lint run -j 2 --new-from-merge-base="$1" $packages

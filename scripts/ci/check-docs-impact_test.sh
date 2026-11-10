#!/bin/sh
set -eu

check="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/check-docs-impact.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-docs-impact-fixture.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
repo=$tmp/repo
mkdir "$repo"

git -C "$repo" init -q
git -C "$repo" config user.name 'Fixture Author'
git -C "$repo" config user.email 'fixture@example.com'
git -C "$repo" config commit.gpgsign false
git -C "$repo" commit -q --allow-empty -m base
base=$(git -C "$repo" rev-parse HEAD)

help=internal/cli/testdata/help.txt
api=api/openapi.yaml
vars=internal/config/variables.go
page=docs/site/src/content/docs/docs/cli-reference.mdx

# Run the check on base..HEAD; sets $status and $out.
run_check() {
	head=$(git -C "$repo" rev-parse HEAD)
	git -C "$repo" diff --no-renames --name-only -z "$base" "$head" >"$tmp/changed"
	status=0
	out=$(cd "$repo" && sh "$check" "$base" "$head" "$tmp/changed" 2>&1) || status=$?
}

# commit_on_base MESSAGE PATH... : one commit on top of base touching the paths.
commit_on_base() {
	message=$1
	shift
	git -C "$repo" switch -q --detach "$base"
	git -C "$repo" clean -fdq
	for path in "$@"; do
		mkdir -p "$repo/$(dirname "$path")"
		printf 'change\n' >>"$repo/$path"
	done
	git -C "$repo" add -A
	git -C "$repo" commit -q --allow-empty -m "$message"
}

expect_pass() {
	name=$1
	shift
	commit_on_base "$@"
	run_check
	[ "$status" -eq 0 ] || { printf 'docs-impact fixture %s: expected pass, got %s\n%s\n' "$name" "$status" "$out" >&2; exit 1; }
}

expect_fail() {
	name=$1
	shift
	commit_on_base "$@"
	run_check
	[ "$status" -eq 1 ] || { printf 'docs-impact fixture %s: expected exit 1, got %s\n%s\n' "$name" "$status" "$out" >&2; exit 1; }
	case "$out" in
		*'Docs-Impact: none - <reason>'*) ;;
		*) printf 'docs-impact fixture %s: failure omits the opt-out\n%s\n' "$name" "$out" >&2; exit 1 ;;
	esac
}

expect_page() {
	case "$out" in
		*"$1"*) ;;
		*) printf 'docs-impact fixture: output lacks %s\n%s\n' "$1" "$out" >&2; exit 1 ;;
	esac
}

expect_pass no-trigger 'chore: unrelated' README.md
expect_pass docs-only 'docs: tweak' "$page"
expect_pass trigger-with-docs 'feat: flag' "$help" "$page"
expect_pass trigger-with-operations-docs 'feat: setting' "$vars" docs/operations/runbook.md

expect_fail help-alone 'feat: flag' "$help"
expect_page cli-reference.mdx
expect_fail openapi-alone 'feat: operation' "$api"
expect_page http-api.mdx
expect_fail variables-alone 'feat: setting' "$vars"
expect_page configuration.mdx

expect_pass trailer 'feat: flag

Docs-Impact: none - internal rename only' "$help"
expect_fail empty-reason 'feat: flag

Docs-Impact: none - ' "$help"
expect_fail no-reason 'feat: flag

Docs-Impact: none' "$help"

# A trailer on a merge commit alone does not count.
commit_on_base 'feat: flag' "$help"
topic=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$base"
printf 'side\n' >"$repo/side"
git -C "$repo" add -A
git -C "$repo" commit -q -m side
git -C "$repo" merge -q --no-ff "$topic" -m 'Merge topic

Docs-Impact: none - merge only'
run_check
[ "$status" -eq 1 ] || { printf 'docs-impact fixture merge-trailer: expected exit 1, got %s\n%s\n' "$status" "$out" >&2; exit 1; }

printf 'docs-impact fixture: all cases passed\n'

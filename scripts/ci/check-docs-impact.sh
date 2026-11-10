#!/bin/sh
# Usage: check-docs-impact.sh COMPARISON HEAD CHANGED_LIST_FILE
# Fails when a user-visible surface changed without a user docs change or an
# explicit "Docs-Impact: none - <reason>" trailer on a non-merge commit in
# COMPARISON..HEAD. CHANGED_LIST_FILE is the NUL-separated list that
# fast-feedback.sh produces. Run from inside the repository being checked.
set -eu

[ "$#" -eq 3 ] || { printf 'usage: %s COMPARISON HEAD CHANGED_LIST_FILE\n' "$0" >&2; exit 2; }
comparison=$1 head=$2 list=$3

# shortcut: a path containing a newline splits into two entries; none exist in these trees.
changed=$(tr '\0' '\n' <"$list")

if printf '%s\n' "$changed" | grep -Eq '^(docs/site/src/content/docs/|docs/operations/)'; then
	printf 'docs-impact: user docs changed\n'
	exit 0
fi

hints=''
add_hint() {
	if printf '%s\n' "$changed" | grep -Fxq "$1"; then
		hints="$hints  $1 changed: update $2
"
	fi
}
add_hint internal/cli/testdata/help.txt 'docs/site/src/content/docs/docs/cli-reference.mdx and the page for the feature'
add_hint api/openapi.yaml 'the page for the feature (docs/site/src/content/docs/docs/http-api.mdx only for contract-wide rules)'
add_hint internal/config/variables.go 'docs/site/src/content/docs/docs/configuration.mdx'

if [ -z "$hints" ]; then
	printf 'docs-impact: no user-visible surface changed\n'
	exit 0
fi

if git log --no-merges --format='%(trailers:key=Docs-Impact,valueonly,unfold)' "$comparison..$head" |
	grep -Eiq '^none - [^[:space:]]'; then
	printf 'docs-impact: opted out with a Docs-Impact trailer\n'
	exit 0
fi

{
	printf 'docs-impact: a user-visible surface changed without a user docs update.\n'
	printf '%s' "$hints"
	printf 'Update the page in this PR (any change under docs/site/src/content/docs/ or docs/operations/ satisfies the check).\n'
	printf 'If no docs change is needed, add this trailer to a commit in the PR, in the\n'
	printf 'final trailer block next to Signed-off-by (git ignores trailers elsewhere):\n'
	printf '  Docs-Impact: none - <reason>\n'
	printf 'Trailers exist only on commits. With fast-feedback.sh --working-tree, uncommitted\n'
	printf 'changes are checked, so change the docs page or commit with the trailer.\n'
} >&2
exit 1

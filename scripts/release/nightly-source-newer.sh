#!/usr/bin/env bash
# Decides whether the selected dev commit is newer than the previous nightly, so
# an unchanged day dispatches no build and the run stays green. Writes
# newer=true|false to $GITHUB_OUTPUT only once every check has succeeded.
set -euo pipefail
fail() { printf 'nightly source: %s\n' "$*" >&2; exit 1; }
: "${GH_TOKEN:?}" "${GITHUB_REPOSITORY:?}" "${GITHUB_OUTPUT:?}" "${GITHUB_RUN_NUMBER:?}" "${SOURCE:?}"
GH_BIN=${GH_BIN:-gh}
repo=$GITHUB_REPOSITORY
here=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)

[[ "$SOURCE" =~ ^[0-9a-f]{40}$ ]] || fail 'invalid source commit'

newer=
reason=
run_tag=$($GH_BIN api --paginate --slurp "repos/$repo/git/matching-refs/tags/v" | "$here/nightly-run-tag.sh" "$GITHUB_RUN_NUMBER")
if [ -n "$run_tag" ]; then
	# A full rerun must still be able to resume or retry publication and the container step.
	newer=true reason="this run already created $run_tag"
else
	previous_tag=$($GH_BIN api --paginate --slurp "repos/$repo/releases?per_page=100" | "$here/latest-nightly-tag.sh")
	if [ -z "$previous_tag" ]; then
		newer=true reason='there is no previous nightly'
	else
		previous=$($GH_BIN api "repos/$repo/git/ref/tags/$previous_tag" --jq '.object.sha')
		[[ "$previous" =~ ^[0-9a-f]{40}$ ]] || fail "previous nightly $previous_tag resolves to no commit"
		status=$($GH_BIN api "repos/$repo/compare/$previous...$SOURCE" --jq .status)
		case "$status" in
		ahead) newer=true reason="it is ahead of $previous_tag ($previous)" ;;
		identical) newer=false reason="$previous_tag already publishes it" ;;
		behind) newer=false reason="it is behind $previous_tag ($previous)" ;;
		# Ancestry cannot order them, as after the one-time main bridge whose merge commit is not on dev.
		# The previous nightly is the last one published, so the next night compares identical and stops.
		diverged) newer=true reason="it diverged from $previous_tag ($previous) and cannot be ordered by ancestry" ;;
		*) fail "unexpected comparison status '$status' against $previous_tag" ;;
		esac
	fi
fi

if [ "$newer" = true ]; then
	printf 'nightly source: dev commit %s proceeds: %s\n' "$SOURCE" "$reason"
else
	printf 'nightly source: dev commit %s does not proceed: %s; nothing is built or published\n' "$SOURCE" "$reason"
fi
printf 'newer=%s\n' "$newer" >>"$GITHUB_OUTPUT"

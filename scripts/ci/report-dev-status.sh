#!/bin/sh
# Keeps one issue open while dev integration is red. Coalesced dev runs can cover
# several PRs, and a red run otherwise only appears in Actions. fuzz-report.yml
# runs this from the default branch; nothing from dev is executed.
#
# The issue names jobs and PR numbers only. Commit text is attacker-controlled
# and never reaches the body.
set -eu

: "${GH_REPO:?}" "${RUN_ID:?}" "${RUN_ATTEMPT:?}" "${RUN_URL:?}" "${HEAD_SHA:?}" "${CONCLUSION:?}"
GH_BIN=${GH_BIN:-gh}
marker='<!-- hikyo-dev-red'

printf '%s' "$HEAD_SHA" | grep -Eq '^[0-9a-f]{40}$' || { printf 'dev status: invalid head\n' >&2; exit 2; }
case "$RUN_ID$RUN_ATTEMPT" in *[!0-9]* | '') printf 'dev status: invalid run\n' >&2; exit 2 ;; esac

issues=$($GH_BIN api --paginate "repos/$GH_REPO/issues?state=open&per_page=100" \
	--jq ".[] | select(.user.login == \"github-actions[bot]\" and (.pull_request == null) and ((.body // \"\") | startswith(\"$marker\"))) | [.number, (.body | split(\"\n\")[0])] | @tsv")
number=$(printf '%s\n' "$issues" | head -n 1 | cut -f1)

case "$CONCLUSION" in
success)
	[ -n "$number" ] || exit 0
	red=$(printf '%s\n' "$issues" | head -n 1 | sed -n 's/.*red_sha=\([0-9a-f]\{40\}\).*/\1/p')
	printf '%s' "$red" | grep -Eq '^[0-9a-f]{40}$' || { printf 'dev status: issue has no red commit\n' >&2; exit 1; }
	# A rerun of an older green commit says nothing about the newer red one.
	status=$($GH_BIN api "repos/$GH_REPO/compare/$red...$HEAD_SHA" --jq '.status')
	case "$status" in ahead | identical) ;; *) exit 0 ;; esac
	$GH_BIN api -X POST "repos/$GH_REPO/issues/$number/comments" -f "body=Dev is green again at $HEAD_SHA: $RUN_URL" >/dev/null
	$GH_BIN api -X PATCH "repos/$GH_REPO/issues/$number" -f state=closed >/dev/null
	exit 0
	;;
failure | timed_out | startup_failure) ;;
*) exit 0 ;; # A cancelled or skipped run reached no verdict.
esac

# A rerun of an older commit says nothing about current dev: never report it once
# a newer commit went green, and never move the issue back to it.
red=$(printf '%s\n' "$issues" | head -n 1 | sed -n 's/.*red_sha=\([0-9a-f]\{40\}\).*/\1/p')
if [ -n "$red" ]; then
	status=$($GH_BIN api "repos/$GH_REPO/compare/$red...$HEAD_SHA" --jq '.status')
	[ "$status" != behind ] || exit 0
fi

# Job names come from the workflow definition, so keep only plain characters.
jobs=$($GH_BIN api --paginate "repos/$GH_REPO/actions/runs/$RUN_ID/attempts/$RUN_ATTEMPT/jobs?per_page=100" \
	--jq '.jobs[] | select(.conclusion == "failure" or .conclusion == "timed_out") | .name' |
	LC_ALL=C sed 's/[^A-Za-z0-9 ._()\/-]//g; s/^/- /')
green=$($GH_BIN api "repos/$GH_REPO/actions/workflows/ci-dev.yml/runs?branch=dev&event=push&status=success&per_page=1" \
	--jq '.workflow_runs[0].head_sha // ""')
if printf '%s' "$green" | grep -Eq '^[0-9a-f]{40}$'; then
	compare=$($GH_BIN api "repos/$GH_REPO/compare/$green...$HEAD_SHA")
	case "$(printf '%s' "$compare" | jq -r '.status')" in behind | identical) exit 0 ;; esac
	pulls=$(printf '%s' "$compare" |
		jq -r '.commits[] | select(.commit.committer.name == "GitHub" and (.parents | length) == 2) | .commit.message | split("\n")[0]' |
		sed -n 's/^Merge pull request #\([1-9][0-9]*\) .*/- #\1/p')
	scope="PRs merged since the [last green commit](https://github.com/$GH_REPO/commit/$green):"
	[ -n "$pulls" ] || pulls='- None listed. See the commit range.'
	range="Compare: https://github.com/$GH_REPO/compare/$green...$HEAD_SHA"
	# The compare API lists a limited number of commits.
	if printf '%s' "$compare" | jq -e '.total_commits > (.commits | length)' >/dev/null; then
		range="This list may be partial. $range"
	fi
else
	scope='No green dev commit exists yet.'
	pulls=
	range=
fi
body="$marker red_sha=$HEAD_SHA -->
Dev integration is red at $HEAD_SHA. Fix or revert it before the next promotion.

Run: $RUN_URL

Failed jobs:
${jobs:-- None listed. See the run.}

$scope
$pulls

$range

This issue updates on each red run and closes when dev is green again."

if [ -n "$number" ]; then
	$GH_BIN api -X PATCH "repos/$GH_REPO/issues/$number" -f "body=$body" >/dev/null
else
	$GH_BIN api -X POST "repos/$GH_REPO/issues" -f 'title=Dev integration is red' -f "body=$body" >/dev/null
fi

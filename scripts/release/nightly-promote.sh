#!/bin/sh
# Lands the last green dev commit on main through the merge queue, so the
# nightly can build it from main with main's signing identity. Runs from main's
# own checkout: nothing from dev is executed, only its commit id is read.
#
# Reads use GH_TOKEN (the workflow token). Every write uses APP_TOKEN, because
# a pull request opened with the workflow token starts no CI. A failure here
# leaves main untouched.
set -eu

: "${GH_REPO:?}" "${GITHUB_SHA:?}" "${GH_TOKEN:?}" "${APP_TOKEN:?}"
GH_BIN=${GH_BIN:-gh}
GO_BIN=${GO_BIN:-go}
server=${GITHUB_SERVER_URL:-https://github.com}
max_age=10800
repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-promote.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

note() {
	printf '%s\n' "$*"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi
}
die() {
	printf 'nightly-promote: %s\n' "$*" >&2
	exit 1
}
write() { GH_TOKEN=$APP_TOKEN "$GH_BIN" "$@"; }
# Prints nothing for a missing branch. Any other failure stops the run.
branch_sha() {
	if sha=$("$GH_BIN" api "repos/$GH_REPO/git/ref/heads/$1" --jq '.object.sha' 2>"$work/err"); then
		printf '%s\n' "$sha"
	elif grep -F '(HTTP 404)' "$work/err" >/dev/null; then
		return 0
	else
		cat "$work/err" >&2
		return 1
	fi
}
arm() { write pr merge "$1" --repo "$GH_REPO" --auto --merge --match-head-commit "$commit"; }

printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || die 'invalid repository'
printf '%s\n' "$GITHUB_SHA" | grep -Eq '^[0-9a-f]{40}$' || die 'invalid main commit'
date_utc=${PROMOTE_DATE:-$(date -u +%Y%m%d)}
printf '%s\n' "$date_utc" | grep -Eq '^[0-9]{8}$' || die 'invalid date'

dev_sha=$(branch_sha dev)
if [ -z "$dev_sha" ]; then
	note 'nightly-promote: dev does not exist yet, so there is nothing to promote.'
	exit 0
fi

# A failure here is the signal: no green dev commit, or a CI definition on main
# that dev has not taken back yet.
proof=$("$GO_BIN" -C "$repo_root" run -p 2 ./scripts/ci/source-proof \
	--repository "$GH_REPO" --mode dev --latest --authority-commit "$GITHUB_SHA") ||
	die 'no verified green dev commit to promote'
commit=$(printf '%s' "$proof" | jq -er '.commit')
run_id=$(printf '%s' "$proof" | jq -er '.run_id')
printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$' || die 'invalid dev commit from source proof'
printf '%s\n' "$run_id" | grep -Eq '^[1-9][0-9]*$' || die 'invalid source proof run'

# Commits the dev commit has that main lacks. Main carries merge commits dev
# lacks, so diverged is the ordinary case.
compare=$("$GH_BIN" api "repos/$GH_REPO/compare/main...$commit")
status=$(printf '%s' "$compare" | jq -er '.status')
case "$status" in
behind | identical)
	note "nightly-promote: main already contains $commit."
	exit 0
	;;
ahead | diverged) ;;
*) die "unexpected compare status '$status' for $commit" ;;
esac
# The queue's DCO check skips merge commits and refuses an empty range, so a dev
# tip with only merges (a backmerge) can never be admitted. A truncated list may
# hide a real commit, so it counts as content.
if printf '%s' "$compare" | jq -e '(.commits | length) as $listed | .total_commits == $listed and all(.commits[]; (.parents | length) > 1)' >/dev/null; then
	note "nightly-promote: $commit adds only merge commits to main, so there is nothing to promote."
	exit 0
fi

repo_lower=$(printf '%s' "$GH_REPO" | tr '[:upper:]' '[:lower:]')
open=$("$GH_BIN" api --paginate "repos/$GH_REPO/pulls?state=open&base=main&per_page=100" --jq \
	".[] | select(((.head.repo.full_name // \"\") | ascii_downcase) == \"$repo_lower\" and (.head.ref | startswith(\"release/nightly-\"))) | [.number, .head.sha, .head.ref, (.auto_merge != null), (.created_at | fromdateiso8601)] | @tsv")
# A refused or failed admission, or a PR that has sat for hours (for example one
# waiting for a maintainer's approval), means this promotion cannot land.
stuck_reason() {
	printf '%s\n' "$2" | grep -Eq '^[0-9]+$' || die 'invalid pull request age'
	check=$("$GH_BIN" api "repos/$GH_REPO/commits/$1/check-runs?check_name=ci-queue-required&per_page=100" \
		--jq '.check_runs | max_by(.id) // {}')
	state=$(printf '%s' "$check" | jq -r '"\(.status // "none")/\(.conclusion // "none")"')
	reason=
	case "$state" in
	none/* | queued/* | in_progress/* | completed/success) ;;
	*) reason="the ci-queue-required check is $state" ;;
	esac
	if [ -z "$reason" ] && [ "$3" = true ] && [ $((now - $2)) -gt "$max_age" ]; then
		reason="it has not landed after more than $((max_age / 3600)) hours"
	fi
}
# The check text is untrusted. Keep it in the log only, as plain printable lines that
# cannot begin or embed a workflow command.
quote_check() { printf '%s' "$check" | jq -r "$1 // \"\"" | tr -cd '[:print:]\n' | head -n 20 | cut -c1-300 | sed 's/##\[/## [/g; s/^/    | /'; }
report_stuck() {
	{
		printf 'nightly-promote: promotion #%s (%s/%s/pull/%s) is stuck: %s.\n' "$1" "$server" "$GH_REPO" "$1" "$reason"
		printf '  Check title:\n%s\n  Check summary:\n%s\n' "$(quote_check .output.title)" "$(quote_check .output.summary)"
		printf '  If the PR changes .github/, an independent maintainer must approve its exact head. Otherwise read the check and the merge queue run, fix the cause, and run nightly-promote again.\n'
	} >&2
}

now=${PROMOTE_NOW:-$(date +%s)}
printf '%s\n' "$now" | grep -Eq '^[0-9]+$' || die 'invalid clock'
stuck=false
same=
same_armed=false
same_created=0
tab=$(printf '\t')
while IFS=$tab read -r number head ref armed created; do
	[ -n "$number" ] || continue
	printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number'
	printf '%s\n' "$ref" | grep -Eq '^release/nightly-[0-9]{8}-[0-9a-f]{7}$' ||
		die "pull request #$number is a release/nightly- branch this workflow did not create; close it by hand"
	if [ "$head" = "$commit" ]; then
		same=$number
		same_armed=$armed
		same_created=$created
		continue
	fi
	# Replacing a promotion that could not land must not hide why main stopped moving.
	stuck_reason "$head" "$created" true
	[ -z "$reason" ] || { report_stuck "$number"; stuck=true; }
	write api -X POST "repos/$GH_REPO/issues/$number/comments" \
		-f "body=A newer green dev commit exists, so nightly-promote.yml replaces this promotion." >/dev/null
	write api -X PATCH "repos/$GH_REPO/pulls/$number" -f state=closed >/dev/null
	if ! write api -X DELETE "repos/$GH_REPO/git/refs/heads/$ref" >/dev/null 2>"$work/err"; then
		grep -E 'Reference does not exist|\(HTTP 404\)' "$work/err" >/dev/null || { cat "$work/err" >&2; exit 1; }
	fi
	note "nightly-promote: closed superseded promotion #$number."
done <<EOF
$open
EOF

# A transient queue failure disarms the PR, so arm it again, and give that retry
# its chance before judging the PR's age.
if [ -n "$same" ]; then
	check_age=true
	if [ "$same_armed" != true ]; then
		arm "$same"
		note "nightly-promote: queued promotion #$same again for $commit."
		check_age=false
	fi
	stuck_reason "$commit" "$same_created" "$check_age"
	[ -z "$reason" ] || { report_stuck "$same"; stuck=true; }
	[ "$stuck" = false ] || exit 1
	note "nightly-promote: promotion #$same already carries $commit and is queued."
	exit 0
fi

short=$(printf '%.7s' "$commit")
branch="release/nightly-$date_utc-$short"
existing=$(branch_sha "$branch")
if [ -z "$existing" ]; then
	write api -X POST "repos/$GH_REPO/git/refs" -f "ref=refs/heads/$branch" -f "sha=$commit" >/dev/null
elif [ "$existing" != "$commit" ]; then
	die "$branch already exists at $existing, not $commit"
fi

body=$(printf 'Promotes the last green dev commit %s to main, so the next nightly is built and signed from main.\n\nSource proof: %s/%s/actions/runs/%s\n\nOpened and queued by nightly-promote.yml. Do not push to this branch.\n' \
	"$commit" "$server" "$GH_REPO" "$run_id")
number=$(write api -X POST "repos/$GH_REPO/pulls" -f "title=chore(release): promote dev $short to main for the nightly" \
	-f "head=$branch" -f base=main -f "body=$body" --jq '.number')
printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number from GitHub'
arm "$number"
note "nightly-promote: opened and queued promotion #$number for $commit."
[ "$stuck" = false ] || exit 1

#!/bin/sh
# Keeps dev containing main. Hotfixes land on main directly and change CI files;
# promotion's source proof refuses a dev whose CI definitions differ from main's,
# so a dev that never takes them back blocks the nightly. Runs from main's own
# checkout and executes nothing from dev.
#
# The pull request head is a dedicated backmerge/auto-<date>-<sha7> branch, never
# main and never a branch at main's SHA (#1373): the dev gate posts
# ci-dev-required on the head SHA, and a check run attaches to the SHA, so the
# head must be a distinct merge commit, as in the manual #1550. The merge commit
# is created by GitHub's merges API, not signed by this script. Whether dev's
# required-signatures rule accepts it is verified at runtime: if it does not, the
# pull request cannot merge and the age rule reports it stuck.
#
# Known limit: the dev gate's devPublisherContract lets dev's ci-dev-gate.yml
# differ from main's only when the pull request head is main's exact SHA. A
# hotfix that changes ci-dev-gate.yml therefore cannot flow through this branch:
# the gate refuses it and, after six hours, it is reported stuck. That case needs
# a manual back-merge, which the gate admits only from main's exact head.
#
# Reads use GH_TOKEN (the workflow token). Every write uses APP_TOKEN, because a
# pull request opened with the workflow token starts no CI.
set -eu

: "${GH_REPO:?}" "${GITHUB_SHA:?}" "${GH_TOKEN:?}" "${APP_TOKEN:?}"
GH_BIN=${GH_BIN:-gh}
server=${GITHUB_SERVER_URL:-https://github.com}
max_age=21600
branch_re='^backmerge/auto-[0-9]{8}-[0-9a-f]{7}$'
docs="$server/$GH_REPO/blob/main/docs/development/branching.md#manual-back-merge"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-backmerge.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

note() {
	printf '%s\n' "$*"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi
}
die() {
	printf 'nightly-backmerge: %s\n' "$*" >&2
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
# $1 is what is stuck (a URL), $2 why. Only validated values reach the log.
stuck() {
	printf 'nightly-backmerge: back-merge %s is stuck: %s.\n  Resolve it by hand: %s\n' "$1" "$2" "$docs" >&2
	exit 1
}
# Merges main into branch $1. Returns 1 on a conflict (HTTP 409); 201 is a new
# merge commit and 204 (no output) means the branch already contains main.
merge_main() {
	# A ref created a moment ago can still read as missing to the merges API, so
	# that one answer is retried briefly; any other failure stops at once.
	attempt=1
	while ! write api -X POST "repos/$GH_REPO/merges" -f "base=$1" -f "head=$main_sha" \
		-f "commit_message=Merge main $short into dev" >/dev/null 2>"$work/err"; do
		if grep -F '(HTTP 409)' "$work/err" >/dev/null; then
			return 1
		elif [ "$attempt" -lt 5 ] && grep -F 'Base does not exist (HTTP 404)' "$work/err" >/dev/null; then
			attempt=$((attempt + 1))
			sleep "$retry_delay"
			continue
		fi
		cat "$work/err" >&2
		exit 1
	done
}
is_sha() { printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{40}$'; }

printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || die 'invalid repository'
is_sha "$GITHUB_SHA" || die 'invalid workflow commit'
retry_delay=${BACKMERGE_RETRY_DELAY:-3}
printf '%s\n' "$retry_delay" | grep -Eq '^[0-9]{1,2}$' || die 'invalid retry delay'
now=${BACKMERGE_NOW:-$(date +%s)}
printf '%s\n' "$now" | grep -Eq '^[0-9]+$' || die 'invalid clock'
date_utc=${BACKMERGE_DATE:-$(date -u +%Y%m%d)}
printf '%s\n' "$date_utc" | grep -Eq '^[0-9]{8}$' || die 'invalid date'
tab=$(printf '\t')

dev_sha=$(branch_sha dev)
if [ -z "$dev_sha" ]; then
	note 'nightly-backmerge: dev does not exist yet, so there is nothing to back-merge.'
	exit 0
fi
is_sha "$dev_sha" || die 'invalid dev commit'
# Main's live tip, not this run's commit: a re-run keeps an old GITHUB_SHA and must
# not judge, merge or close against it. Every main commit passed the merge queue.
main_sha=$(branch_sha main)
is_sha "$main_sha" || die 'invalid main commit'
short=$(printf '%.7s' "$main_sha")

# Only this repository's pull requests count. A back-merge is automatic only when
# the release App opened it (the id the landed job pins); the same branch name
# from anyone else is neither automatic nor manual, and is left alone. Refs are
# classified in jq, so a branch name reaches the shell only after matching the
# exact pattern.
repo_lower=$(printf '%s' "$GH_REPO" | tr '[:upper:]' '[:lower:]')
"$GH_BIN" api --paginate "repos/$GH_REPO/pulls?state=open&per_page=100" --jq \
	".[] | select(((.head.repo.full_name // \"\") | ascii_downcase) == \"$repo_lower\") | (.head.ref | test(\"$branch_re\")) as \$named | [.number, (if .base.ref != \"dev\" then \"other\" elif \$named and .user.id == 320493937 then \"auto\" elif \$named then \"other\" elif (.head.ref | startswith(\"backmerge/\")) then \"human\" else \"other\" end), .head.ref] | @tsv" >"$work/open"
auto='' auto_ref='' human=''
: >"$work/heads"
while IFS=$tab read -r number kind ref; do
	[ -n "$number" ] || continue
	printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number'
	# Every open head is in use, whatever its kind or base.
	printf '%s\n' "$ref" >>"$work/heads"
	case "$kind" in
	human)
		human=$number
		continue
		;;
	auto) ;;
	*) continue ;;
	esac
	[ -z "$auto" ] || die "pull requests #$auto and #$number are both automatic back-merges; close one by hand"
	printf '%s\n' "$ref" | grep -Eq "$branch_re" || die "invalid head branch on #$number"
	auto=$number auto_ref=$ref
done <"$work/open"

# Orphaned back-merge branches (closed or conflicted pull requests, finished
# merges). Runs share one concurrency group, so none can sit between creating a
# branch and opening its pull request. Only exact-pattern refs no open pull
# request uses are deleted.
"$GH_BIN" api --paginate "repos/$GH_REPO/git/matching-refs/heads/backmerge/auto-" --jq \
	'.[].ref' >"$work/refs"
while IFS= read -r ref; do
	[ -n "$ref" ] || continue
	branch=${ref#refs/heads/}
	# Not one of ours (a human branch sharing the prefix): leave it alone.
	printf '%s\n' "$branch" | grep -Eq "$branch_re" || continue
	grep -Fxq -- "$branch" "$work/heads" && continue
	if ! write api -X DELETE "repos/$GH_REPO/git/refs/heads/$branch" >/dev/null 2>"$work/err"; then
		# A ruleset refusal is also a 422, so only a missing ref counts as gone.
		grep -E 'Reference does not exist|\(HTTP 404\)' "$work/err" >/dev/null || { cat "$work/err" >&2; exit 1; }
		continue
	fi
	note "nightly-backmerge: deleted orphaned back-merge branch $branch."
done <"$work/refs"

# An automatic back-merge left open after dev took main another way (by hand, say)
# has nothing left to do. The next run deletes its branch.
retire_auto() {
	[ -n "$auto" ] || return 0
	write api -X POST "repos/$GH_REPO/issues/$auto/comments" \
		-f "body=Dev already contains main, so nightly-backmerge closes this back-merge." >/dev/null
	write api -X PATCH "repos/$GH_REPO/pulls/$auto" -f state=closed >/dev/null
	note "nightly-backmerge: closed back-merge #$auto, because dev already contains main."
}

# Commits main has that dev lacks.
compare=$("$GH_BIN" api "repos/$GH_REPO/compare/dev...$main_sha")
status=$(printf '%s' "$compare" | jq -er '.status')
case "$status" in
behind | identical)
	note 'nightly-backmerge: dev already contains main.'
	retire_auto
	exit 0
	;;
ahead | diverged) ;;
*) die "unexpected compare status '$status'" ;;
esac
# Promotion merges are all main has over dev in the ordinary case, and carry no
# content to back-merge. A truncated list may hide a real commit, so it counts as
# content.
if printf '%s' "$compare" | jq -e '(.commits | length) as $listed | .total_commits == $listed and all(.commits[]; (.parents | length) > 1)' >/dev/null; then
	note 'nightly-backmerge: main leads dev by merge commits only, so there is nothing to back-merge.'
	retire_auto
	exit 0
fi
if [ -n "$human" ]; then
	note "nightly-backmerge: pull request #$human back-merges by hand, so no new one is opened."
	exit 0
fi

if [ -n "$auto" ]; then
	pr_url="$server/$GH_REPO/pull/$auto"
	merge_main "$auto_ref" || stuck "$pr_url" 'main conflicts with its branch'
	# mergeable is null while GitHub computes it: unknown, not stuck.
	pr=$("$GH_BIN" api "repos/$GH_REPO/pulls/$auto" --jq \
		'[(.mergeable | tostring), (.mergeable_state // "-"), (.auto_merge != null), ((.created_at | fromdateiso8601?) // "-")] | @tsv')
	IFS=$tab read -r mergeable state armed created <<PR
$pr
PR
	printf '%s\n' "$mergeable" | grep -Eq '^(true|false|null)$' || die "invalid mergeable value on #$auto"
	printf '%s\n' "$state" | grep -Eq '^[a-z_-]{1,20}$' || die "invalid merge state on #$auto"
	printf '%s\n' "$armed" | grep -Eq '^(true|false)$' || die "invalid auto-merge value on #$auto"
	printf '%s\n' "$created" | grep -Eq '^[0-9]{1,12}$' || die "invalid creation time on #$auto"
	# The refs API is current right after the merge; the pull request's head can lag.
	head=$(branch_sha "$auto_ref")
	is_sha "$head" || die "invalid head commit on $auto_ref"
	if [ "$mergeable" = false ] || [ "$state" = dirty ]; then
		stuck "$pr_url" 'it conflicts with dev'
	fi
	# Re-arm before the age check, so an old pull request still gets its chance. A
	# failed re-arm still lets an overdue one report its recovery route.
	rearm=ok
	if [ "$armed" != true ]; then
		if write pr merge "$auto" --repo "$GH_REPO" --auto --merge --match-head-commit "$head"; then
			note "nightly-backmerge: queued back-merge #$auto again."
		else
			rearm=failed
		fi
	fi
	if [ $((now - created)) -gt "$max_age" ]; then
		stuck "$pr_url" "it has not landed after more than $((max_age / 3600)) hours"
	fi
	[ "$rearm" = ok ] || die "back-merge #$auto could not be queued again"
	note "nightly-backmerge: back-merge #$auto now carries main $short and is queued."
	exit 0
fi

branch="backmerge/auto-$date_utc-$short"
# Cleanup removed any orphan, so an existing branch is unexpected.
existing=$(branch_sha "$branch")
[ -z "$existing" ] || die "$branch already exists at $existing, not $dev_sha"
write api -X POST "repos/$GH_REPO/git/refs" -f "ref=refs/heads/$branch" -f "sha=$dev_sha" >/dev/null
merge_main "$branch" || stuck "$server/$GH_REPO/tree/$branch" 'main conflicts with dev'
head=$(branch_sha "$branch")
is_sha "$head" || die "invalid commit on $branch"
# Dev lacks main, so the merge must have moved the branch; a head equal to dev has
# nothing to propose.
[ "$head" != "$dev_sha" ] || die "merging main into $branch left it at dev"
body=$(printf 'Merges main %s into dev so the next promotion'\''s CI-definition check passes.\n\nOpened and queued by nightly-promote.yml (nightly-backmerge). The head is its own merge commit, never main. Do not push to this branch.\n\nIf it conflicts, resolve it by hand: %s\n' "$short" "$docs")
number=$(write api -X POST "repos/$GH_REPO/pulls" -f 'title=chore(release): back-merge main into dev' \
	-f "head=$branch" -f base=dev -f "body=$body" --jq '.number')
printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number from GitHub'
write pr merge "$number" --repo "$GH_REPO" --auto --merge --match-head-commit "$head"
note "nightly-backmerge: opened and queued back-merge #$number from $branch."

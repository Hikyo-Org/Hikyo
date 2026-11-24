#!/bin/sh
# Keeps dev containing main. Hotfixes land on main directly and change CI files;
# promotion's source proof refuses a dev whose CI definitions differ from main's,
# so a dev that never takes them back blocks the nightly. Runs from main's own
# checkout and executes nothing from dev.
#
# The pull request has head `main` and base `dev`, with no snapshot branch:
# ci-feedback.yml treats exactly that head as a back-merge, and the dev gate
# admits policy paths that match main. Dev's ruleset needs ci-dev-required and
# signed commits; GitHub signs the merge commit.
#
# Reads use GH_TOKEN (the workflow token). Every write uses APP_TOKEN, because a
# pull request opened with the workflow token starts no CI.
set -eu

: "${GH_REPO:?}" "${GITHUB_SHA:?}" "${GH_TOKEN:?}" "${APP_TOKEN:?}"
GH_BIN=${GH_BIN:-gh}
server=${GITHUB_SERVER_URL:-https://github.com}
max_age=21600
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
# The head is main and moves, so there is no head commit to match.
arm() { write pr merge "$1" --repo "$GH_REPO" --auto --merge; }

printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || die 'invalid repository'
printf '%s\n' "$GITHUB_SHA" | grep -Eq '^[0-9a-f]{40}$' || die 'invalid main commit'
now=${BACKMERGE_NOW:-$(date +%s)}
printf '%s\n' "$now" | grep -Eq '^[0-9]+$' || die 'invalid clock'

dev_sha=$(branch_sha dev)
if [ -z "$dev_sha" ]; then
	note 'nightly-backmerge: dev does not exist yet, so there is nothing to back-merge.'
	exit 0
fi
printf '%s\n' "$dev_sha" | grep -Eq '^[0-9a-f]{40}$' || die 'invalid dev commit'

# Commits main has that dev lacks.
compare=$("$GH_BIN" api "repos/$GH_REPO/compare/dev...main")
status=$(printf '%s' "$compare" | jq -er '.status')
case "$status" in
behind | identical)
	note 'nightly-backmerge: dev already contains main.'
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
	exit 0
fi

# Only this repository's pull requests count. The ref is classified in jq, so no
# branch name reaches the shell.
repo_lower=$(printf '%s' "$GH_REPO" | tr '[:upper:]' '[:lower:]')
"$GH_BIN" api --paginate "repos/$GH_REPO/pulls?state=open&base=dev&per_page=100" --jq \
	".[] | select(((.head.repo.full_name // \"\") | ascii_downcase) == \"$repo_lower\") | [.number, (.head.ref | if startswith(\"backmerge/\") then \"human\" elif . == \"main\" then \"auto\" else \"other\" end)] | @tsv" >"$work/open"
tab=$(printf '\t')
auto=
while IFS=$tab read -r number kind; do
	[ -n "$number" ] || continue
	printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number'
	case "$kind" in
	human)
		note "nightly-backmerge: pull request #$number back-merges by hand, so no new one is opened."
		exit 0
		;;
	auto) auto=$number ;;
	esac
done <"$work/open"

if [ -n "$auto" ]; then
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
	reason=
	if [ "$mergeable" = false ] || [ "$state" = dirty ]; then
		reason='it conflicts with dev; resolve it on a backmerge/main-into-dev-<date> branch by hand, as in #1550'
	elif [ $((now - created)) -gt "$max_age" ]; then
		reason="it has not landed after more than $((max_age / 3600)) hours"
	fi
	if [ -n "$reason" ]; then
		printf 'nightly-backmerge: back-merge %s/%s/pull/%s is stuck: %s.\n' "$server" "$GH_REPO" "$auto" "$reason" >&2
		exit 1
	fi
	# Arming a conflicting pull request can fail, so a stuck one reports first.
	if [ "$armed" != true ]; then
		arm "$auto"
		note "nightly-backmerge: queued back-merge #$auto again."
	fi
	note "nightly-backmerge: back-merge #$auto is open and queued."
	exit 0
fi

body="Opened by nightly-backmerge so dev contains main and promotion's CI-definition check passes. It merges on its own once ci-dev-required is green."
number=$(write api -X POST "repos/$GH_REPO/pulls" -f 'title=chore(release): back-merge main into dev' \
	-f head=main -f base=dev -f "body=$body" --jq '.number')
printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number from GitHub'
arm "$number"
note "nightly-backmerge: opened and queued back-merge #$number."

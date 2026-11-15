#!/bin/sh
# Queues Dependabot security-update pull requests into main, and only those.
#
# A pull request is a security update when an OPEN Dependabot alert links to it
# (GraphQL `dependabotUpdate.pullRequest`). No name, label or branch pattern
# decides that: a version update has no such link. The alert read needs the
# workflow token's `vulnerability-alerts: read` permission.
#
# Everything else is checked as well, because auto-merge removes the person who
# would have looked: the author is Dependabot by numeric id, every commit is
# authored by Dependabot, committed and verified by GitHub, and the pull request
# changes only dependency manifests and lock files. The merge itself is the
# normal queue: `ci-queue-required` and the full merge-group run still decide.
#
# Any read that fails stops the run without queueing anything. Environment:
# GH_REPO (owner/name), GH_TOKEN, DRY_RUN=true to only report, GH_BIN for tests.
set -eu

GH_BIN=${GH_BIN:-gh}
dependabot_id=49699333
web_flow_id=19864447
# Only these file names, in any directory, may change in an unattended merge.
manifests='["go.mod","go.sum","package.json","package-lock.json","pnpm-lock.yaml","yarn.lock"]'
max_files=300
max_commits=100

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dependabot-automerge.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

note() {
	printf '%s\n' "$*"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi
}
die() {
	printf 'dependabot-security-automerge: %s\n' "$*" >&2
	exit 1
}
[ -n "${GH_REPO:-}" ] || die 'GH_REPO is required'
[ -n "${GH_TOKEN:-}" ] || die 'GH_TOKEN is required'
printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || die 'invalid repository'
owner=${GH_REPO%%/*}
name=${GH_REPO#*/}

# Paginated REST list into one JSON array. A failed page fails the run.
list() {
	"$GH_BIN" api --paginate "$1" >"$work/pages" || die "cannot read $1"
	jq -s 'add // []' "$work/pages" >"$work/list" || die "unreadable answer for $1"
	cat "$work/list"
}

# gh --paginate follows pageInfo, so the query must select it. GraphQL variables, not shell ones.
# shellcheck disable=SC2016
alert_query='query($owner:String!,$name:String!,$endCursor:String){repository(owner:$owner,name:$name){vulnerabilityAlerts(first:100,after:$endCursor,states:[OPEN]){pageInfo{hasNextPage endCursor} nodes{number dependabotUpdate{pullRequest{number}}}}}}'
"$GH_BIN" api graphql --paginate -f owner="$owner" -f name="$name" -f query="$alert_query" >"$work/alerts.pages" ||
	die 'cannot read the Dependabot alerts (the workflow needs vulnerability-alerts: read)'
# An absent alert connection is a refused read, not an empty repository.
jq -es 'all(.[]; .data.repository.vulnerabilityAlerts.nodes | type == "array")' "$work/alerts.pages" >/dev/null ||
	die 'the Dependabot alert answer is incomplete'
linked=$(jq -s '[.[].data.repository.vulnerabilityAlerts.nodes[] | .dependabotUpdate.pullRequest.number // empty] | unique' "$work/alerts.pages")

pulls=$(list "repos/$GH_REPO/pulls?state=open&base=main&per_page=100")
failed=false
tab=$(printf '\t')
rows=$(printf '%s' "$pulls" | jq -r --argjson id "$dependabot_id" --arg repo "$GH_REPO" '
	.[] | select(.user.id == $id and .user.login == "dependabot[bot]") |
	[.number, .head.sha, (.head.repo.full_name // ""), .head.ref, (.draft | tostring), (.auto_merge != null | tostring)] | @tsv')

while IFS=$tab read -r number head head_repo ref draft armed; do
	[ -n "$number" ] || continue
	printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number'
	printf '%s\n' "$head" | grep -Eq '^[0-9a-f]{40}$' || die "pull request #$number has an invalid head"
	if ! printf '%s' "$linked" | jq -e --argjson n "$number" 'index($n) != null' >/dev/null; then
		note "Left for a person: #$number is a Dependabot update that no open alert links to."
		continue
	fi
	reason=
	if [ "$head_repo" != "$GH_REPO" ]; then reason='its head is not in this repository'
	elif [ "${ref#dependabot/}" = "$ref" ]; then reason='its branch is not a Dependabot branch'
	elif [ "$draft" = true ]; then reason='it is a draft'
	fi
	if [ -z "$reason" ]; then
		commits=$(list "repos/$GH_REPO/pulls/$number/commits?per_page=100")
		reason=$(printf '%s' "$commits" | jq -r --arg head "$head" --argjson dep "$dependabot_id" --argjson web "$web_flow_id" --argjson max "$max_commits" '
			if length >= $max then "it has too many commits to check"
			elif (any(.[]; .sha == $head) | not) then "its head is not among its commits"
			elif (all(.[]; .author.id == $dep and .committer.id == $web and .commit.verification.verified == true) | not)
			then "a commit is not authored by Dependabot, committed by GitHub and verified"
			else "" end')
	fi
	if [ -z "$reason" ]; then
		files=$(list "repos/$GH_REPO/pulls/$number/files?per_page=100")
		reason=$(printf '%s' "$files" | jq -r --argjson ok "$manifests" --argjson max "$max_files" '
			def base: split("/") | last;
			if length == 0 or length >= $max then "it changes no files or too many to check"
			elif (all(.[]; [.filename, .previous_filename] | map(select(. != null) | base) | all(. as $b | $ok | index($b) != null)) | not)
			then "it changes a file that is not a dependency manifest or lock file"
			else "" end')
	fi
	if [ -z "$reason" ]; then
		"$GH_BIN" api "repos/$GH_REPO/commits/$head/check-runs?check_name=ci-queue-required&per_page=100" \
			--jq '[.check_runs[] | select(.app.id == 15368 and .app.slug == "github-actions")] | max_by(.id) // {}' >"$work/check" ||
			die "cannot read the checks of #$number"
		state=$(jq -r '"\(.status // "none")/\(.conclusion // "none")"' "$work/check")
		case "$state" in
		none/* | queued/* | in_progress/* | completed/success) ;;
		*) reason="its ci-queue-required check is $state, so a person must look at it" ;;
		esac
	fi
	if [ -n "$reason" ]; then
		note "Not queued: security update #$number: $reason."
		failed=true
		continue
	fi
	if [ "$armed" = true ]; then
		note "Already queued: security update #$number."
		continue
	fi
	if [ "${DRY_RUN:-}" = true ]; then
		note "Would queue security update #$number at $head."
		continue
	fi
	if "$GH_BIN" pr merge "$number" --repo "$GH_REPO" --auto --merge --match-head-commit "$head"; then
		note "Queued security update #$number at $head."
	else
		note "Not queued: security update #$number: GitHub refused to arm auto-merge."
		failed=true
	fi
done <<EOF
$rows
EOF

[ "$failed" = false ] || exit 1

#!/bin/sh
# One level-triggered step of the unattended stable patch release. Every
# subcommand re-derives the state it needs, so a lost trigger, a retry or an
# overlapping run only repeats work that is already idempotent.
#
#   plan        read-only: pick the single next step and the side lanes
#   tag         create the app's annotated tag for the planned commit
#   publish     publish the staged draft of an app-built run after the digest check
#   sync-trust  open the app-signed trust PR for a published release
#   homebrew    update the stable Homebrew cask and merge its PR
#   automerge   arm auto-merge on proven Dependabot security-update PRs
#   cleanup     delete the merged trust-sync branch of the current release
#   issue       open, update or close the single stall issue
#
# Environment: GH_REPO, GH_TOKEN (the job's own scoped token) and, where noted,
# STABLE_RELEASE_APP_BOT_ID (plan, publish, sync-trust), TAP_TOKEN and
# ISSUES_TOKEN (plan only: read-only tokens for the tap and the issue list).
# Release tools run from this checkout, which is main's tip. The scripts never
# read a private key.
#
# Every read that decides something goes through api(), which stops the run when
# GitHub cannot answer. A failed lookup must never look like an empty answer.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=${HIKYO_REPO_ROOT:-$(CDPATH='' cd -- "$script_dir/../.." && pwd)}
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
: "${GH_BIN:=gh}" "${GO_BIN:=go}" "${GIT_BIN:=git}"
: "${GREEN_BIN:=$script_dir/require-green-main.sh}"
: "${SYNCED_BIN:=$script_dir/require-synced-trust.sh}"
: "${DOCS_LIVE_BIN:=$script_dir/../ci/check-docs-live.sh}"
: "${FALLBACK_BIN:=$script_dir/../ci/check-fallback-channel-test.sh}"
: "${HOMEBREW_BIN:=$script_dir/publish-homebrew-cask.sh}"
: "${PUBLISH_BIN:=$script_dir/publish-stable-draft.sh}"
: "${GH_REPO:?}"
dependabot_user_id=49699333
issue_title='Unattended stable patch release is stalled'
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stable-reconcile.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
trust="$repo_root/release/trust"
# A security merge that is still unreleased after this long raises the stall issue.
escalate_after=86400
# A trust PR that the queue has not merged after this long is a failure, not a retry.
trust_pr_patience=10800

die() {
	printf 'stable-reconcile: %s\n' "$*" >&2
	exit 1
}
note() {
	printf '%s\n' "$*"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi
}
emit() { printf '%s=%s\n' "$1" "$2" >>"${GITHUB_OUTPUT:-/dev/null}"; }
gh() { "$GH_BIN" "$@"; }
go_stable() { "$GO_BIN" -C "$repo_root" run ./scripts/release/stable "$@"; }
tap_gh() { if [ -n "${TAP_TOKEN:-}" ]; then GH_TOKEN=$TAP_TOKEN gh "$@"; else gh "$@"; fi; }
issue_gh() { if [ -n "${ISSUES_TOKEN:-}" ]; then GH_TOKEN=$ISSUES_TOKEN gh "$@"; else gh "$@"; fi; }
printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || die 'invalid repository'
# api FILE ARGS...: read an endpoint into FILE. A failed read stops the run.
api() {
	api_file=$1
	shift
	gh api "$@" >"$api_file" 2>"$work/err" || { cat "$work/err" >&2; die "cannot read: gh api $*"; }
}
# 404 is an answer, anything else is a failure to look.
ref_exists() {
	if gh api "repos/$GH_REPO/git/ref/$1" >/dev/null 2>"$work/err"; then return 0; fi
	grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die "cannot inspect $1"; }
	return 1
}
need_version() {
	printf '%s\n' "${1:-}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || die 'a plain stable version X.Y.Z is required'
}
need_sha() { printf '%s\n' "${1:-}" | grep -Eq '^[0-9a-f]{40}$' || die "a full commit SHA is required: ${1:-empty}"; }
need_bot() { printf '%s\n' "${STABLE_RELEASE_APP_BOT_ID:-}" | grep -Eq '^[1-9][0-9]*$' || die 'STABLE_RELEASE_APP_BOT_ID must be the numeric bot user id'; }
patch_successor() { printf '%s\n' "$1" | awk -F. '{printf "%d.%d.%d\n", $1, $2, $3 + 1}'; }
# semver_gt A B: A has higher SemVer precedence than B (a release outranks its own prerelease).
semver_gt() {
	jq -n -e --arg a "$1" --arg b "$2" '
		def key: capture("^(?<core>[0-9]+\\.[0-9]+\\.[0-9]+)(?:-(?<pre>[0-9A-Za-z.-]+))?(?:\\+.*)?$") |
			[(.core | split(".") | map(tonumber)),
			 (if .pre == null then [1] else [0, (.pre | split(".") | map(if test("^[0-9]+$") then tonumber else . end))] end)];
		($a | key) > ($b | key)' >/dev/null
}
epoch() { date -u -d "$1" +%s 2>/dev/null || date -u -j -f %Y-%m-%dT%H:%M:%SZ "$1" +%s; }
# Seeds the rollback floor from main's authenticated trust, never from a local file.
seed_state() {
	state="$work/release-trust-state.json"
	go_stable verify --trust "$trust" --trust-only --state "$state" >/dev/null
}
# Sets tagged_commit to the commit the tag names, through an annotated tag object if any.
tag_commit() {
	api "$work/tag-ref.json" "repos/$GH_REPO/git/ref/tags/$1"
	tag_type=$(jq -er '.object.type' "$work/tag-ref.json") || die "cannot read the $1 ref"
	tag_sha=$(jq -er '.object.sha' "$work/tag-ref.json") || die "cannot read the $1 ref"
	if [ "$tag_type" = tag ]; then
		api "$work/tag-object.json" "repos/$GH_REPO/git/tags/$tag_sha"
		tagged_commit=$(jq -er '.object | select(.type == "commit") | .sha' "$work/tag-object.json") || die "$1 does not name a commit"
	else
		tagged_commit=$tag_sha
	fi
	need_sha "$tagged_commit"
}
# Prints the number of the open stall issue, or nothing. Never guesses on a failed listing.
find_issue() {
	issue_gh issue list --repo "$GH_REPO" --state open --limit 1000 --json number,title >"$work/issues.json" 2>"$work/err" ||
		{ cat "$work/err" >&2; die 'cannot list open issues'; }
	jq -r --arg title "$issue_title" '[.[] | select(.title == $title)][0].number // empty' "$work/issues.json"
}
security_prs() {
	# shellcheck disable=SC2016
	query='query($owner:String!,$name:String!,$endCursor:String){repository(owner:$owner,name:$name){vulnerabilityAlerts(first:100,after:$endCursor){pageInfo{hasNextPage endCursor}nodes{dependabotUpdate{pullRequest{number merged mergedAt baseRefName headRefName author{login} mergeCommit{oid}}}}}}}'
	gh api graphql --paginate -f query="$query" -f owner="${GH_REPO%/*}" -f name="${GH_REPO#*/}" >"$work/alerts" ||
		die 'cannot read Dependabot alerts'
	# An alert that Dependabot itself linked to a pull request proves a security update.
	jq -s -c '[.[].data.repository.vulnerabilityAlerts.nodes[].dependabotUpdate.pullRequest | select(. != null) |
		select(.author.login == "dependabot" and .baseRefName == "main" and (.headRefName | startswith("dependabot/")))] | unique_by(.number)' "$work/alerts"
}

# Everything tag() refuses, so the plan never decides a tag that would die and
# the job never burns an immutable tag on a state the build would refuse.
tag_checks() {
	checks_version=$1
	checks_commit=$2
	"$SYNCED_BIN" "$GH_REPO" "$trust/metadata.json" ||
		die 'trust is not synchronized with the public releases (an outstanding stable draft also stops this)'
	go_stable preflight --trust "$trust" >/dev/null || die 'stable trust does not authenticate'
	go_stable candidate --patch-only --trust "$trust" --version "$checks_version" --commit "$checks_commit" \
		--out "$work/candidate.json" || die "v$checks_version is not the patch successor of the signed latest stable release"
	"$GIT_BIN" -C "$repo_root" merge-base --is-ancestor "$checks_commit" "$GITHUB_SHA" || die 'the commit is not on main history'
	"$GREEN_BIN" "$GH_REPO" "$checks_commit" </dev/null || die "$checks_commit has no complete green source proof"
	api "$work/matching-tags.json" --paginate "repos/$GH_REPO/git/matching-refs/tags/v"
	jq -s -e --arg tag "v$checks_version" '
		def triple: ltrimstr("v") | capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)") | [.a, .b, .c | tonumber];
		([.[][] | .ref | ltrimstr("refs/tags/") | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+") and (contains("-nightly.") | not)) | triple | select(. >= ($tag | triple))] | length) == 0
	' "$work/matching-tags.json" >/dev/null || die "a stable tag at or above v$checks_version already exists"
	if gh api "repos/$GH_REPO/releases/tags/v$checks_version" >/dev/null 2>"$work/err"; then die "v$checks_version already has a release"; fi
	grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die 'cannot prove the version is unused'; }
}

plan_inner() {
	latest_tag_file="$work/latest.json"
	api "$latest_tag_file" "repos/$GH_REPO/releases/latest"
	latest_tag=$(jq -er '.tag_name' "$latest_tag_file") || die 'cannot read the latest release'
	printf '%s\n' "$latest_tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || die "latest release $latest_tag is not a plain stable tag"
	latest=${latest_tag#v}
	highest=$(jq -r '.highest_release // empty' "$trust/metadata.json")
	api "$work/latest-commit.json" "repos/$GH_REPO/commits/$latest_tag"
	latest_commit=$(jq -er '.sha' "$work/latest-commit.json") || die "cannot resolve $latest_tag"
	need_sha "$latest_commit"
	next=$(patch_successor "$latest")
	next_tag="v$next"

	# Side lane: Dependabot security-update PRs into main. The proof is an alert
	# that Dependabot itself linked to the pull request, not a label or a title,
	# and every commit of the PR must be authored by Dependabot.
	security_prs >"$work/security.json"
	api "$work/pulls.json" --paginate "repos/$GH_REPO/pulls?state=open&base=main&per_page=100"
	jq -s -r --slurpfile security "$work/security.json" --argjson bot "$dependabot_user_id" --arg repo "$GH_REPO" '
		.[][] | select(.user.id == $bot and .head.repo.full_name == $repo and (.head.ref | startswith("dependabot/")) and .auto_merge == null and
			(.number as $n | $security[0] | any(.number == $n))) | "\(.number) \(.head.sha)"' "$work/pulls.json" >"$work/candidates"
	while read -r number sha; do
		[ -n "$number" ] || continue
		api "$work/pr-commits.json" --paginate "repos/$GH_REPO/pulls/$number/commits?per_page=100"
		if jq -s -e --arg sha "$sha" '[.[][]] | length > 0 and (.[-1].sha == $sha) and all(.[]; .author.login == "dependabot[bot]")' "$work/pr-commits.json" >/dev/null; then
			l_automerge="${l_automerge:+$l_automerge }$number:$sha"
		else
			note "stable-reconcile: security PR #$number has a commit not authored by dependabot[bot]; it is not queued"
		fi
	done <"$work/candidates"

	# Merged security updates whose merge commit the latest stable tag lacks.
	jq -r '.[] | select(.merged == true and .mergeCommit != null) | "\(.mergeCommit.oid) \(.mergedAt)"' "$work/security.json" >"$work/merged"
	: >"$work/pending"
	while read -r oid merged_at; do
		[ -n "$oid" ] || continue
		"$GIT_BIN" -C "$repo_root" cat-file -e "$oid^{commit}" 2>/dev/null || continue
		"$GIT_BIN" -C "$repo_root" merge-base --is-ancestor "$oid" "$GITHUB_SHA" 2>/dev/null || continue
		if ! "$GIT_BIN" -C "$repo_root" merge-base --is-ancestor "$oid" "$latest_commit" 2>/dev/null; then
			printf '%s %s\n' "$oid" "$merged_at" >>"$work/pending"
		fi
	done <"$work/merged"

	if [ -z "$highest" ]; then
		decision none 'trust names no signed latest stable release, so no patch successor exists'
		return 0
	fi
	if [ "$latest" != "$highest" ]; then
		if semver_gt "$latest" "$highest"; then
			decision sync-trust "v$latest is published but main's trust still ends at v$highest" "$latest"
			escalate "main's trust still ends at v$highest although v$latest is published"
		else
			decision none "the latest release v$latest is older than trusted v$highest"
			want_issue open "The latest GitHub release v$latest is older than the signed latest v$highest. A maintainer must reconcile them."
		fi
		return 0
	fi

	# Side lanes once trust is synced: the Homebrew cask follows the latest
	# stable release, and the merged trust-sync branch is deleted.
	tap=${HIKYO_HOMEBREW_TAP_REPOSITORY:-${GH_REPO%/*}/homebrew-tap}
	cask=
	if tap_gh api "repos/$tap/contents/Casks/hikyo.rb?ref=main" >"$work/cask.json" 2>"$work/err"; then
		jq -er '.content' "$work/cask.json" | base64 -d >"$work/cask.rb" || die 'cannot decode the Homebrew cask'
		cask=$(sed -n 's/^[[:space:]]*version[[:space:]]*"\([^"]*\)".*/\1/p' "$work/cask.rb" | head -n 1)
	else
		grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die "cannot read the cask of $tap"; }
	fi
	[ "$cask" = "$latest" ] || l_homebrew=$latest
	if ref_exists "heads/release/sync-trust-$latest"; then l_cleanup="release/sync-trust-$latest"; fi

	if ref_exists "tags/$next_tag"; then
		tag_commit "$next_tag"
		api "$work/runs.json" --paginate "repos/$GH_REPO/actions/workflows/release.yml/runs?event=push&head_sha=$tagged_commit&per_page=100"
		# Only the exact push of this tag at this commit counts, whoever started it.
		jq -s -c --arg tag "$next_tag" --arg commit "$tagged_commit" '[.[].workflow_runs[] |
			select(.head_sha == $commit and .head_branch == $tag and .event == "push" and (.path | startswith(".github/workflows/release.yml")))]' \
			"$work/runs.json" >"$work/tag-runs.json"
		jq -c --argjson bot "$STABLE_RELEASE_APP_BOT_ID" '[.[] | select(.actor.id == $bot and .triggering_actor.id == $bot)] | sort_by(.run_number, .run_attempt) | last // empty' \
			"$work/tag-runs.json" >"$work/bot-run.json"
		if [ "$(jq 'length' "$work/tag-runs.json")" = 0 ]; then
			decision wait "$next_tag exists and its release-build run has not started" "$next"
			escalate "$next_tag exists but no release-build run has started"
		elif [ ! -s "$work/bot-run.json" ]; then
			# A person tagged this release: the ceremony reviews and publishes it.
			decision none "$next_tag was tagged by a person; its draft awaits the human ceremony, not this reconciler"
			escalate "$next_tag was tagged by a person and its draft still awaits the human ceremony"
		else
			run_status=$(jq -r '.status' "$work/bot-run.json")
			run_conclusion=$(jq -r '.conclusion // ""' "$work/bot-run.json")
			run_id=$(jq -er '.id' "$work/bot-run.json")
			if [ "$run_status" != completed ]; then
				decision wait "the release-build run $run_id of $next_tag is $run_status" "$next"
				escalate "the release-build run $run_id of $next_tag is still $run_status"
			elif [ "$run_conclusion" = success ]; then
				api "$work/artifacts.json" "repos/$GH_REPO/actions/runs/$run_id/artifacts?per_page=100"
				artifact=$(jq -r --arg id "$run_id" \
					'[.artifacts[] | select((.name | startswith("release-manifest-sha256-" + $id + "-")) and .expired == false)] | sort_by(.id) | last | .name // empty' "$work/artifacts.json")
				if [ -z "$artifact" ]; then
					decision wait "the manifest digest artifact of run $run_id is missing or expired" "$next" "" "$run_id"
					escalate "the manifest digest artifact of run $run_id is missing or expired; the draft cannot be published unattended"
				else
					decision publish "the release-build run $run_id of $next_tag succeeded" "$next" "" "$run_id" "$artifact"
				fi
			else
				decision none "the release-build run of $next_tag ended $run_conclusion; its tag is immutable"
				want_issue open "The release-build run of $next_tag ended $run_conclusion. Tags are immutable, so the unattended release cannot retry. If it staged a draft, delete it first (the tag stays burned): gh release delete $next_tag --repo $GH_REPO --yes. Then release the next patch by hand with scripts/release/ceremony.sh, or grant the app actions: write."
			fi
		fi
		return 0
	fi

	if [ ! -s "$work/pending" ]; then
		decision none "no merged Dependabot security update is missing from $latest_tag"
		want_issue close
		return 0
	fi
	# Newest first-parent commit since the latest tag that holds a pending merge.
	"$GIT_BIN" -C "$repo_root" rev-list --first-parent "$latest_commit..$GITHUB_SHA" >"$work/chain"
	floor=
	while read -r commit; do
		if grep -q "^$commit " "$work/pending"; then floor=$commit; break; fi
	done <"$work/chain"
	if [ -z "$floor" ]; then
		decision none 'a pending security merge is not on main first-parent history'
		want_issue open 'A merged Dependabot security update is not on main first-parent history, so no release can contain it.'
		return 0
	fi
	# The build reads trust at the tag commit, so a commit whose trust predates the
	# latest release would burn the tag. Take only commits that already record it.
	target=
	checked=0
	while read -r commit; do
		checked=$((checked + 1))
		[ "$checked" -le 60 ] || break
		at=$("$GIT_BIN" -C "$repo_root" show "$commit:release/trust/metadata.json" 2>/dev/null | jq -r '.highest_release // empty' || true)
		if [ "$at" = "$latest" ] && "$GREEN_BIN" "$GH_REPO" "$commit" </dev/null >/dev/null 2>&1; then target=$commit; break; fi
		[ "$commit" != "$floor" ] || break
	done <"$work/chain"
	if [ -z "$target" ]; then
		decision none 'no main commit at or after the newest security merge both records the latest release in trust and has complete green source proof'
		escalate 'no main commit at or after the security merge has synchronized trust and complete green source proof; run the full ci-main workflow on the main tip'
		return 0
	fi
	api "$work/docs.json" "repos/$GH_REPO/actions/workflows/docs.yml/runs?branch=main&per_page=10"
	if jq -e '[.workflow_runs[] | select(.status != "completed")] | length > 0' "$work/docs.json" >/dev/null; then
		decision wait 'a docs deployment is still running, and a release build refuses until the live docs are current' "$next"
		escalate 'a docs deployment has not finished'
		return 0
	fi
	if ! "$DOCS_LIVE_BIN" https://hikyo.app security@hikyo.app >/dev/null 2>&1; then
		decision wait 'hikyo.app does not pass the live docs gate yet, which would fail the release build' "$next"
		escalate 'hikyo.app does not pass the live docs gate'
		return 0
	fi
	if ! { "$GIT_BIN" -C "$repo_root" show "$target:release/repository/fallback-channel-test.json" >"$work/fallback.json" 2>/dev/null &&
		"$FALLBACK_BIN" "$work/fallback.json" security@hikyo.app >/dev/null 2>&1; }; then
		decision wait 'the fallback channel test record is stale at the target commit, which would fail the release build' "$next"
		escalate 'the fallback channel test record is stale (it expires after 93 days); a maintainer must run and record a new test'
		return 0
	fi
	if ! (tag_checks "$next" "$target") >"$work/refusal" 2>&1; then
		refusal=$(tail -n 1 "$work/refusal" | tr '\n' ' ')
		decision none "the tag would be refused: $refusal"
		escalate "the next tag would be refused: $refusal"
		return 0
	fi
	decision tag "merged security updates are missing from $latest_tag; $target is the newest green commit at or after them" "$next" "$target"
	want_issue close
}

decision() { # action reason [version commit run artifact]
	d_action=$1 d_reason=$2 d_version=${3:-} d_commit=${4:-} d_run=${5:-} d_artifact=${6:-}
}
want_issue() { i_action=$1 i_text=${2:-}; }
# While a security merge waits for its release, every stuck state alerts after a day.
escalate() {
	[ -s "$work/pending" ] || return 0
	oldest=$(sort -k2 "$work/pending" | head -n 1 | cut -d' ' -f2)
	age=$(($(date +%s) - $(epoch "$oldest")))
	if [ "$age" -gt "$escalate_after" ]; then
		want_issue open "A merged Dependabot security update has waited more than 24 hours for its release: $1."
	fi
}

plan() {
	: "${GITHUB_SHA:?}"
	need_sha "$GITHUB_SHA"
	need_bot
	d_action=none d_reason='' d_version='' d_commit='' d_run='' d_artifact='' i_action=none i_text='' l_automerge='' l_homebrew='' l_cleanup=''
	plan_inner
	# Run the issue job only when it has something to do.
	if [ "$i_action" = close ]; then
		existing=$(find_issue) || die 'cannot look up the stall issue'
		[ -n "$existing" ] || i_action=none
	fi
	emit action "$d_action"
	emit reason "$d_reason"
	emit version "$d_version"
	emit commit "$d_commit"
	emit run_id "$d_run"
	emit artifact "$d_artifact"
	emit issue "$i_action"
	emit issue_text "$i_text"
	emit automerge "$l_automerge"
	emit homebrew "$l_homebrew"
	emit cleanup "$l_cleanup"
	note "stable-reconcile: next step is $d_action: $d_reason"
}

tag() {
	: "${VERSION:?}" "${COMMIT:?}" "${GITHUB_SHA:?}"
	need_version "$VERSION"
	need_sha "$COMMIT"
	# A lagging read can plan the same tag twice; the tag already being there is the goal.
	if ref_exists "tags/v$VERSION"; then
		note "stable-reconcile: v$VERSION already exists; nothing more to do"
		return 0
	fi
	tag_checks "$VERSION" "$COMMIT"
	# An annotated tag object, so the build can read its tagger and the run is the app's.
	api "$work/tag-object.json" --method POST "repos/$GH_REPO/git/tags" -f tag="v$VERSION" -f message="Hikyo $VERSION" \
		-f object="$COMMIT" -f type=commit
	object=$(jq -er '.sha' "$work/tag-object.json") || die 'no tag object was created'
	need_sha "$object"
	if ! gh api --method POST "repos/$GH_REPO/git/refs" -f ref="refs/tags/v$VERSION" -f sha="$object" >/dev/null 2>"$work/err"; then
		# A lagging read can plan the same tag twice; the second create then fails.
		cat "$work/err" >&2
		ref_exists "tags/v$VERSION" || die "cannot create tags/v$VERSION"
		note "stable-reconcile: v$VERSION already exists; nothing more to do"
		return 0
	fi
	note "stable-reconcile: tagged v$VERSION at $COMMIT; release-build starts as the app"
}

publish() {
	: "${VERSION:?}" "${RUN_ID:?}" "${DIGEST_FILE:?}"
	need_version "$VERSION"
	need_bot
	printf '%s\n' "$RUN_ID" | grep -Eq '^[1-9][0-9]*$' || die 'invalid release-build run id'
	# Only a draft that the app itself built and tagged is published unattended. The
	# publish helper checks the same authority again before it touches the release.
	tag_commit "v$VERSION"
	api "$work/run.json" "repos/$GH_REPO/actions/runs/$RUN_ID"
	jq -e --arg tag "v$VERSION" --arg commit "$tagged_commit" --arg repo "$GH_REPO" --argjson bot "$STABLE_RELEASE_APP_BOT_ID" '
		(.path | startswith(".github/workflows/release.yml")) and .event == "push" and .head_branch == $tag and .head_sha == $commit and
		.repository.full_name == $repo and .status == "completed" and .conclusion == "success" and
		.actor.id == $bot and .triggering_actor.id == $bot and .actor.type == "Bot" and .triggering_actor.type == "Bot"
	' "$work/run.json" >/dev/null || die "run $RUN_ID is not a successful release-build of v$VERSION started and triggered by the stable-release app; a person's release is published by the ceremony"
	digest=$(cat "$DIGEST_FILE")
	printf '%s\n' "$digest" | grep -Eq '^[0-9a-f]{64}$' || die 'the retained manifest digest is not a SHA-256'
	seed_state
	# The helper pins the digest to the downloaded draft, rechecks the tag authority
	# against this run, the build, immutable releases, every signature and OCI subject.
	HIKYO_RELEASE_RUN_ID=$RUN_ID HIKYO_RELEASE_TRUST_STATE=$state "$PUBLISH_BIN" "$GH_REPO" "v$VERSION" "$digest" "$trust"
}

# Downloads and verifies the public release the way the ceremony does.
verify_public() {
	version=$1
	api "$work/release-view.json" "repos/$GH_REPO/releases/tags/v$version"
	[ "$(jq -r '.draft' "$work/release-view.json")" = false ] || die "v$version is not public"
	seed_state
	download="$work/public"
	mkdir "$download"
	gh release download "v$version" --repo "$GH_REPO" --dir "$download" || die "cannot download v$version"
	commit=$(jq -er '.source_commit' "$download/release-manifest.json") || die 'the release manifest names no source commit'
	go_stable verify --trust "$trust" --directory "$download" --version "$version" --commit "$commit" \
		--state "$state" --latest --published
}

sync_trust() {
	: "${VERSION:?}" "${APP_SLUG:?}"
	need_version "$VERSION"
	need_bot
	printf '%s\n' "$APP_SLUG" | grep -Eq '^[A-Za-z0-9-]+$' || die 'invalid app slug'
	verify_public "$VERSION"
	files='metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json'
	same=true
	for name in $files; do cmp -s "$download/$name" "$trust/$name" || same=false; done
	if [ "$same" = true ]; then note "trust already records v$VERSION"; return 0; fi
	branch="release/sync-trust-$VERSION"
	api "$work/main-ref.json" "repos/$GH_REPO/git/ref/heads/main"
	main_sha=$(jq -er '.object.sha' "$work/main-ref.json") || die 'cannot read main'
	need_sha "$main_sha"
	head=
	if ref_exists "heads/$branch"; then
		api "$work/branch-ref.json" "repos/$GH_REPO/git/ref/heads/$branch"
		head=$(jq -er '.object.sha' "$work/branch-ref.json") || die "cannot read $branch"
		# Only the four trust files may differ from main on an existing branch.
		api "$work/compare.json" "repos/$GH_REPO/compare/$main_sha...$head"
		jq -e --arg names "$files" '
			($names | split(" ") | map("release/trust/" + .)) as $allowed |
			(.files | type == "array") and all(.files[]; .filename as $f | $allowed | index($f) != null)
		' "$work/compare.json" >/dev/null || die "$branch contains unrelated changes; nothing was overwritten"
	else
		gh api --method POST "repos/$GH_REPO/git/refs" -f ref="refs/heads/$branch" -f sha="$main_sha" >/dev/null || die "cannot create $branch"
		head=$main_sha
	fi
	bot_login="${APP_SLUG}[bot]"
	bot_email="$STABLE_RELEASE_APP_BOT_ID+$bot_login@users.noreply.github.com"
	# check-dco.sh needs this trailer to equal the commit author exactly.
	body="Public release trust evidence for v$VERSION, verified against the published release.

Signed-off-by: $bot_login <$bot_email>"
	additions=$(for name in $files; do
		jq -n --arg path "release/trust/$name" --arg contents "$(base64 <"$download/$name" | tr -d '\n')" '{path: $path, contents: $contents}'
	done | jq -s '.')
	jq -n --arg repo "$GH_REPO" --arg branch "$branch" --arg oid "$head" --arg headline "chore(release): record published $VERSION trust" \
		--arg body "$body" --argjson additions "$additions" '{
		query: "mutation($input: CreateCommitOnBranchInput!){createCommitOnBranch(input:$input){commit{oid}}}",
		variables: {input: {branch: {repositoryNameWithOwner: $repo, branchName: $branch}, expectedHeadOid: $oid,
			message: {headline: $headline, body: $body}, fileChanges: {additions: $additions}}}}' >"$work/commit.json"
	committed=false
	if [ "$head" != "$main_sha" ]; then
		# A retry finds the commit already on the branch: reuse it, never add an empty one.
		matches=true
		for name in $files; do
			api "$work/branch-file.json" "repos/$GH_REPO/contents/release/trust/$name?ref=$branch"
			have=$(jq -r '.content // empty' "$work/branch-file.json" | tr -d '\n')
			[ "$have" = "$(base64 <"$download/$name" | tr -d '\n')" ] || matches=false
		done
		[ "$matches" = false ] || committed=true
	fi
	if [ "$committed" = true ]; then
		commit=$head
	else
		api "$work/commit-result.json" graphql --input "$work/commit.json"
		commit=$(jq -er '.data.createCommitOnBranch.commit.oid' "$work/commit-result.json") || die 'the trust commit was not created'
	fi
	need_sha "$commit"
	# GitHub's own verdict decides, not a local signature check.
	api "$work/commit-view.json" "repos/$GH_REPO/commits/$commit"
	jq -e --arg name "$bot_login" --arg email "$bot_email" '
		.commit.verification.verified == true and .commit.author.name == $name and .commit.author.email == $email and
		(.commit.message | endswith("Signed-off-by: " + $name + " <" + $email + ">"))
	' "$work/commit-view.json" >/dev/null || die "commit $commit is not GitHub-verified with a matching DCO sign-off"
	api "$work/prs.json" "repos/$GH_REPO/pulls?state=open&base=main&head=${GH_REPO%/*}:$branch"
	number=$(jq -r '.[0].number // empty' "$work/prs.json")
	if [ -z "$number" ]; then
		api "$work/pr-created.json" --method POST "repos/$GH_REPO/pulls" -f title="chore(release): record published $VERSION trust" -f head="$branch" -f base=main \
			-f body='Public release trust evidence, verified against the published release by stable-release-reconciler.yml. The merge queue runs the full gate.'
		number=$(jq -er '.number' "$work/pr-created.json") || die 'the trust pull request was not created'
	elif [ "$(jq -r '.[0].auto_merge != null' "$work/prs.json")" != true ]; then
		# The queue removes auto-merge from a rejected PR. Re-arm for a while, then stop and alert.
		opened=$(jq -r '.[0].created_at' "$work/prs.json")
		[ $(($(date +%s) - $(epoch "$opened"))) -le "$trust_pr_patience" ] ||
			die "trust PR #$number was not merged within $((trust_pr_patience / 3600)) hours and is no longer queued; a maintainer must resolve it"
	fi
	printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number'
	if [ "$(jq -r '.[0].auto_merge != null' "$work/prs.json")" != true ]; then
		gh pr merge "$number" --repo "$GH_REPO" --auto --merge --match-head-commit "$commit"
	fi
	note "stable-reconcile: trust PR #$number carries $commit and is queued"
}

homebrew() {
	: "${VERSION:?}"
	need_version "$VERSION"
	tap=${HIKYO_HOMEBREW_TAP_REPOSITORY:-${GH_REPO%/*}/homebrew-tap}
	verify_public "$VERSION"
	"$HOMEBREW_BIN" "$GH_REPO" "v$VERSION" "$download" "$tap" >"$work/homebrew.out" || { cat "$work/homebrew.out"; die 'cannot update the tap'; }
	cat "$work/homebrew.out"
	url=$(sed -n 's/^homebrew publish: review and merge \(https:\/\/github.com\/.*\/pull\/[0-9][0-9]*\)$/\1/p' "$work/homebrew.out")
	[ -n "$url" ] || return 0
	number=${url##*/}
	api "$work/tap-pr.json" "repos/$tap/pulls/$number"
	sha=$(jq -er '.head.sha' "$work/tap-pr.json") || die "cannot read tap PR #$number"
	need_sha "$sha"
	# The tap disables auto-merge, so merge once its required check is green; a
	# later run retries while the check is still pending.
	api "$work/tap-checks.json" "repos/$tap/commits/$sha/check-runs?check_name=ci-required&per_page=100"
	if jq -e '[.check_runs[] | select(.conclusion == "success")] | length > 0' "$work/tap-checks.json" >/dev/null; then
		gh api --method PUT "repos/$tap/pulls/$number/merge" -f merge_method=squash -f sha="$sha" >/dev/null || die "cannot merge tap PR #$number"
		note "stable-reconcile: merged tap PR #$number"
	else
		note "stable-reconcile: tap PR #$number waits for ci-required"
	fi
}

automerge() {
	: "${PRS:=}"
	failed=false
	for entry in $PRS; do
		printf '%s\n' "$entry" | grep -Eq '^[1-9][0-9]*:[0-9a-f]{40}$' || die "invalid pull request entry $entry"
		# One refused pull request must not hide the others.
		if gh pr merge "${entry%%:*}" --repo "$GH_REPO" --auto --merge --match-head-commit "${entry#*:}"; then
			note "stable-reconcile: queued Dependabot security update #${entry%%:*}"
		else
			failed=true
		fi
	done
	[ "$failed" = false ] || die 'could not queue every security update'
}

# Deletes the trust-sync branch only after its pull request merged exactly that head.
cleanup() {
	: "${BRANCH:?}"
	printf '%s\n' "$BRANCH" | grep -Eq '^release/sync-trust-[0-9]+\.[0-9]+\.[0-9]+$' || die 'invalid trust-sync branch'
	ref_exists "heads/$BRANCH" || { note "stable-reconcile: $BRANCH is already gone"; return 0; }
	api "$work/branch-ref.json" "repos/$GH_REPO/git/ref/heads/$BRANCH"
	head=$(jq -er '.object.sha' "$work/branch-ref.json") || die "cannot read $BRANCH"
	api "$work/closed.json" "repos/$GH_REPO/pulls?state=closed&base=main&head=${GH_REPO%/*}:$BRANCH&per_page=100"
	if jq -e --arg sha "$head" 'any(.[]; .merged_at != null and .head.sha == $sha)' "$work/closed.json" >/dev/null; then
		gh api --method DELETE "repos/$GH_REPO/git/refs/heads/$BRANCH" >/dev/null || die "cannot delete $BRANCH"
		note "stable-reconcile: deleted merged branch $BRANCH"
	else
		note "stable-reconcile: kept $BRANCH; no merged pull request has exactly its head"
	fi
}

issue() {
	number=$(find_issue) || die 'cannot look up the stall issue'
	case "$1" in
	open)
		text=${ISSUE_TEXT:-stalled}
		if [ -n "$number" ]; then
			gh issue edit "$number" --repo "$GH_REPO" --body "$text"
		else
			gh issue create --repo "$GH_REPO" --title "$issue_title" --body "$text"
		fi
		;;
	close) [ -z "$number" ] || gh issue close "$number" --repo "$GH_REPO" --comment 'The unattended release is no longer stalled.' ;;
	*) die 'usage: issue open|close' ;;
	esac
}

[ "$#" -ge 1 ] || die 'usage: stable-reconcile.sh plan|tag|publish|sync-trust|homebrew|automerge|cleanup|issue'
command=$1
shift
case "$command" in
plan | tag | publish | homebrew | automerge | cleanup | issue) "$command" "$@" ;;
sync-trust) sync_trust "$@" ;;
*) die "unknown command $command" ;;
esac

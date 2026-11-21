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
: "${COMPAT_BIN:=$script_dir/check-unattended-compatibility.sh}"
: "${POSTGRES_BIN:=$script_dir/../ci/start-ci-postgres.sh}"
: "${GH_REPO:?}"
# Numeric ids, never logins: Dependabot's bot user and GitHub's web-flow committer.
dependabot_user_id=49699333
web_flow_user_id=19864447
issue_title='Unattended stable patch release is stalled'
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stable-reconcile.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
trust="$repo_root/release/trust"
# A security merge that is still unreleased after this long raises the stall issue.
escalate_after=86400
# A draft must contain the security merges that were known this long before it was tagged.
floor_grace=300
# A trust PR that the queue has not merged after this long is a failure, not a retry.
trust_pr_patience=10800
# The scratch database the compatibility generator needs; release.yml uses the same one.
scratch_postgres_dsn='postgres://hikyo:release-schema-fixture@127.0.0.1:5432/hikyo_release_schema?sslmode=disable'

die() {
	printf 'stable-reconcile: %s\n' "$*" >&2
	exit 1
}
# A rule says no: the plan turns exit 3 into "do not tag" and a stall alert.
# Everything else that stops a run (die) is a failed read or a broken tool, and
# must fail the run instead of passing for "no".
refuse() {
	printf 'stable-reconcile: %s\n' "$*" >&2
	exit 3
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
need_release() { is_semver "${1:-}" || die "a release version X.Y.Z or X.Y.Z-pre is required: ${1:-empty}"; }
need_sha() { printf '%s\n' "${1:-}" | grep -Eq '^[0-9a-f]{40}$' || die "a full commit SHA is required: ${1:-empty}"; }
need_bot() { printf '%s\n' "${STABLE_RELEASE_APP_BOT_ID:-}" | grep -Eq '^[1-9][0-9]*$' || die 'STABLE_RELEASE_APP_BOT_ID must be the numeric bot user id'; }
patch_successor() { printf '%s\n' "$1" | awk -F. '{printf "%d.%d.%d\n", $1, $2, $3 + 1}'; }
# SemVer precedence key: a release outranks its own prerelease; numeric
# prerelease identifiers sort below alphanumeric ones.
semver_key_jq='def key: capture("^(?<core>[0-9]+\\.[0-9]+\\.[0-9]+)(?:-(?<pre>[0-9A-Za-z.-]+))?(?:\\+.*)?$") |
	[(.core | split(".") | map(tonumber)),
	 (if .pre == null then [1] else [0, (.pre | split(".") | map(if test("^[0-9]+$") then tonumber else . end))] end)];'
# semver_gt A B: A has higher SemVer precedence than B.
semver_gt() {
	jq -n -e --arg a "$1" --arg b "$2" "$semver_key_jq"'($a | key) > ($b | key)' >/dev/null
}
epoch() { date -u -d "$1" +%s 2>/dev/null || date -u -j -f %Y-%m-%dT%H:%M:%SZ "$1" +%s; }
# Seeds the rollback floor from main's authenticated trust, never from a local file.
seed_state() {
	state="$work/release-trust-state.json"
	go_stable verify --trust "$trust" --trust-only --state "$state" >/dev/null
}
# is_green COMMIT: 0 = complete green source proof, 1 = not proven green. A failed
# GitHub read stops the run: "could not look" is never the same as "not green".
is_green() {
	if "$GREEN_BIN" "$GH_REPO" "$1" </dev/null >"$work/green.out" 2>&1; then return 0; fi
	if grep -F 'GitHub read failed' "$work/green.out" >/dev/null; then cat "$work/green.out" >&2; die "cannot read the source proof of $1"; fi
	return 1
}
# b64_oneline FILE: the file as one base64 line; an empty or failed encoding stops the run.
b64_oneline() {
	[ -s "$1" ] || die "$1 is empty"
	base64 <"$1" >"$work/b64.tmp" || die "cannot encode $1"
	tr -d '\n' <"$work/b64.tmp" >"$work/b64.line" || die "cannot encode $1"
	[ -s "$work/b64.line" ] || die "$1 encoded to nothing"
	cat "$work/b64.line"
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
# Reads every Dependabot alert (all states listed explicitly, so a change of the
# default cannot hide the fixed ones) and prints the pull requests Dependabot
# itself linked to an alert. OPEN alerts name the open update PRs, FIXED ones
# the merged ones; the author is bound by its numeric id.
security_prs() {
	# shellcheck disable=SC2016
	query='query($owner:String!,$name:String!,$endCursor:String){repository(owner:$owner,name:$name){vulnerabilityAlerts(first:100,after:$endCursor,states:[OPEN,FIXED]){pageInfo{hasNextPage endCursor}nodes{state fixedAt dependabotUpdate{pullRequest{number merged mergedAt baseRefName headRefName author{login ... on Bot{databaseId}} mergeCommit{oid}}}}}}}'
	gh api graphql --paginate -f query="$query" -f owner="${GH_REPO%/*}" -f name="${GH_REPO#*/}" >"$work/alerts" ||
		die 'cannot read Dependabot alerts'
	# An alert that Dependabot itself linked to a pull request proves a security update.
	jq -s -c --argjson bot "$dependabot_user_id" '[.[].data.repository.vulnerabilityAlerts.nodes[].dependabotUpdate.pullRequest | select(. != null) |
		select(.author.databaseId == $bot and .baseRefName == "main" and (.headRefName | startswith("dependabot/")))] | unique_by(.number)' "$work/alerts"
}

# authority_at COMMIT: 0 when COMMIT carries the actor-based tag authority, 1 when it
# does not; an unreadable file stops the run.
authority_at() {
	rc=0
	commit_has_actor_tag_authority "$GH_REPO" "$1" || rc=$?
	case $rc in
	0) return 0 ;;
	1) return 1 ;;
	*) die "cannot read the tag authority of $1" ;;
	esac
}

# Everything tag() refuses, so the plan never decides a tag that would die and
# the job never burns an immutable tag on a state the build would refuse.
# Exit 3 (refuse) = a rule says no. Any other failure = a read failed; the run
# fails instead of pretending the answer was no.
tag_checks() {
	checks_version=$1
	checks_commit=$2
	checks_compat=${3:-}
	rc=0
	"$SYNCED_BIN" "$GH_REPO" "$trust/metadata.json" || rc=$?
	case $rc in
	0) ;;
	1) refuse 'trust is not synchronized with the public releases (an outstanding stable draft also stops this)' ;;
	*) die 'cannot list the public releases to check that trust is synchronized' ;;
	esac
	go_stable preflight --trust "$trust" >/dev/null || refuse 'stable trust does not authenticate'
	go_stable candidate --patch-only --trust "$trust" --version "$checks_version" --commit "$checks_commit" \
		--out "$work/candidate.json" || refuse "v$checks_version is not the patch successor of the signed latest stable release"
	"$GIT_BIN" -C "$repo_root" cat-file -e "$checks_commit^{commit}" 2>/dev/null || refuse 'the commit is not on main history'
	rc=0
	"$GIT_BIN" -C "$repo_root" merge-base --is-ancestor "$checks_commit" "$GITHUB_SHA" || rc=$?
	case $rc in
	0) ;;
	1) refuse 'the commit is not on main history' ;;
	*) die 'cannot compare the commit with main history' ;;
	esac
	is_green "$checks_commit" || refuse "$checks_commit has no complete green source proof"
	authority_at "$checks_commit" || refuse "$checks_commit predates the actor-based tag authority, so its release build could not tell who pushed the tag"
	api "$work/matching-tags.json" --paginate "repos/$GH_REPO/git/matching-refs/tags/v"
	jq -s -e --arg tag "v$checks_version" '
		def triple: ltrimstr("v") | capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)") | [.a, .b, .c | tonumber];
		([.[][] | .ref | ltrimstr("refs/tags/") | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+") and (contains("-nightly.") | not)) | triple | select(. >= ($tag | triple))] | length) == 0
	' "$work/matching-tags.json" >/dev/null || refuse "a stable tag at or above v$checks_version already exists; a stray tag (for example one made with a stolen app key) blocks the unattended release until a repository admin removes it, see docs/release/online-signing.md"
	if gh api "repos/$GH_REPO/releases/tags/v$checks_version" >/dev/null 2>"$work/err"; then refuse "v$checks_version already has a release"; fi
	grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die 'cannot prove the version is unused'; }
	[ -z "$checks_compat" ] || compat_checks "$checks_version" "$checks_commit"
}

# The candidate's compatibility declaration is generated at the commit that would
# be tagged, with the real generator on scratch PostgreSQL, and compared with the
# previous stable release's published one by the very script release-build runs
# again. PostgreSQL starts only here, when a tag is otherwise ready.
compat_checks() {
	cc_version=$1
	cc_commit=$2
	cc_tree="$work/target-tree"
	mkdir "$cc_tree"
	"$GIT_BIN" -C "$repo_root" archive "$cc_commit" | tar -x -C "$cc_tree" || die "cannot export $cc_commit"
	[ -f "$cc_tree/release/compatibility/sources.json" ] || die "$cc_commit carries no release/compatibility/sources.json"
	POSTGRES_PASSWORD=release-schema-fixture POSTGRES_DB=hikyo_release_schema POSTGRES_HEALTH_RETRIES=12 \
		"$POSTGRES_BIN" >/dev/null || die 'cannot start scratch PostgreSQL for the compatibility declaration'
	HIKYO_RELEASE_SCHEMA_POSTGRES_DSN=$scratch_postgres_dsn "$GO_BIN" -C "$cc_tree" run ./scripts/release/compatibility \
		--candidate "$work/candidate.json" --out "$work/declaration.json" || die "cannot generate the compatibility declaration of $cc_commit"
	rc=0
	"$COMPAT_BIN" "$GH_REPO" "$cc_tree/release/trust" "$cc_version" "$work/declaration.json" >"$work/compat.out" 2>&1 || rc=$?
	case $rc in
	0) ;;
	1) refuse "$(tail -n 1 "$work/compat.out")" ;;
	*) cat "$work/compat.out" >&2; die 'cannot compare the compatibility declaration with the previous release' ;;
	esac
}

# What is published decides, never GitHub's "latest" flag: the highest published
# release of any channel (what trust must record) and the highest stable one
# (what a patch builds on). Drafts and nightlies never count. Sets published_highest,
# latest, latest_tag, latest_commit, latest_published_at, highest, next and next_tag.
read_published() {
	api "$work/releases.json" --paginate --slurp "repos/$GH_REPO/releases?per_page=100"
	jq -c "$semver_key_jq"'
		[.[][] | select(.draft == false and (.tag_name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+(-[A-Za-z][0-9A-Za-z.-]*)?$") and (contains("-nightly.") | not)))] as $published |
		($published | map(.tag_name | ltrimstr("v"))) as $all |
		{highest: ($all | max_by(key)), stable: ($all | map(select(contains("-") | not)) | max_by(key)),
		 published_at: ($published | max_by(.tag_name | ltrimstr("v") | key) | .published_at)}
	' "$work/releases.json" >"$work/published.json" || die 'cannot read the published releases'
	published_highest=$(jq -r '.highest // empty' "$work/published.json")
	latest=$(jq -r '.stable // empty' "$work/published.json")
	[ -n "$latest" ] || die 'there is no published stable release'
	latest_tag="v$latest"
	latest_published_at=$(jq -r --arg tag "$latest_tag" '[.[][] | select(.tag_name == $tag)][0].published_at // empty' "$work/releases.json")
	[ -n "$latest_published_at" ] || die "$latest_tag has no publication time"
	highest=$(jq -r '.highest_release // empty' "$trust/metadata.json")
	api "$work/latest-commit.json" "repos/$GH_REPO/commits/$latest_tag"
	latest_commit=$(jq -er '.sha' "$work/latest-commit.json") || die "cannot resolve $latest_tag"
	need_sha "$latest_commit"
	next=$(patch_successor "$latest")
	next_tag="v$next"
}

# Merged security updates whose merge commit the latest stable tag lacks, as
# "OID MERGED_AT" lines in $work/pending.
read_pending() {
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
}

# floor_check COMMIT BUILT_AT: 0 when COMMIT holds the security merges that were
# already known when the draft was built, else 1 with floor_reason. A merge that
# landed within floor_grace seconds of the build start is not required: the tag
# is immutable, so insisting on a merge that raced the tag would strand the draft
# for good. At least one pending merge must be in COMMIT.
floor_check() {
	fc_commit=$1
	fc_at=$(epoch "$2")
	if [ ! -s "$work/pending" ]; then
		floor_reason="no merged Dependabot security update is missing from $latest_tag"
		return 1
	fi
	fc_included=false
	while read -r oid merged_at; do
		[ -n "$oid" ] || continue
		rc=0
		"$GIT_BIN" -C "$repo_root" merge-base --is-ancestor "$oid" "$fc_commit" || rc=$?
		case $rc in
		0) fc_included=true ;;
		1)
			if [ "$(epoch "$merged_at")" -le $((fc_at - floor_grace)) ]; then
				floor_reason="$fc_commit lacks the security merge $oid, which was merged before the draft was built"
				return 1
			fi
			;;
		*) die "cannot compare $oid with $fc_commit" ;;
		esac
	done <"$work/pending"
	if [ "$fc_included" != true ]; then
		floor_reason="$fc_commit contains none of the security merges missing from $latest_tag"
		return 1
	fi
	return 0
}

plan_inner() {
	read_published

	# Side lane: Dependabot security-update PRs into main. The proof is an alert
	# that Dependabot itself linked to the pull request, not a label or a title,
	# and every commit of the PR must be Dependabot's own: authored by
	# dependabot[bot], committed by GitHub's web-flow and verified by GitHub
	# (all bound by numeric user id).
	security_prs >"$work/security.json"
	# A Dependabot alert that was fixed after the latest release but that no
	# Dependabot pull request is linked to would leave "no pending merge" looking
	# like "nothing to release". Raise that, never close the issue over it.
	unlinked=$(jq -s --arg since "$latest_published_at" '[.[].data.repository.vulnerabilityAlerts.nodes[] |
		select(.state == "FIXED" and (.fixedAt // "") > $since and .dependabotUpdate.pullRequest == null)] | length' "$work/alerts") ||
		die 'cannot read the fixed Dependabot alerts'
	if [ "$unlinked" -gt 0 ]; then
		raise "$unlinked Dependabot alert(s) were fixed on main after $latest_tag but none is linked to a merged Dependabot pull request, so no unattended patch release is planned; check the alerts or release by hand with scripts/release/ceremony.sh"
	fi
	api "$work/pulls.json" --paginate "repos/$GH_REPO/pulls?state=open&base=main&per_page=100"
	jq -s -r --slurpfile security "$work/security.json" --argjson bot "$dependabot_user_id" --arg repo "$GH_REPO" '
		.[][] | select(.user.id == $bot and .head.repo.full_name == $repo and (.head.ref | startswith("dependabot/")) and .auto_merge == null and
			(.number as $n | $security[0] | any(.number == $n))) | "\(.number) \(.head.sha)"' "$work/pulls.json" >"$work/candidates"
	while read -r number sha; do
		[ -n "$number" ] || continue
		api "$work/pr-commits.json" --paginate "repos/$GH_REPO/pulls/$number/commits?per_page=100"
		if jq -s -e --arg sha "$sha" --argjson dep "$dependabot_user_id" --argjson web "$web_flow_user_id" '[.[][]] | length > 0 and (.[-1].sha == $sha) and
			all(.[]; .author.id == $dep and .committer.id == $web and .commit.verification.verified == true)' "$work/pr-commits.json" >/dev/null; then
			l_automerge="${l_automerge:+$l_automerge }$number:$sha"
		else
			raise "security PR #$number has a commit that is not Dependabot's own GitHub-verified commit, so it is not queued"
		fi
	done <"$work/candidates"

	read_pending

	if [ -z "$highest" ]; then
		decision none 'trust names no signed latest stable release, so no patch successor exists'
		return 0
	fi
	if semver_gt "$published_highest" "$highest"; then
		# Includes a person's prerelease that was never synced.
		decision sync-trust "v$published_highest is published but main's trust still ends at v$highest" "$published_highest"
		escalate "main's trust still ends at v$highest although v$published_highest is published"
		escalate_trust_lag "$published_highest"
		return 0
	fi
	if semver_gt "$highest" "$published_highest"; then
		decision none "trust names v$highest, which is not published; the highest published release is v$published_highest"
		want_issue open "Main's signed trust names v$highest, but the highest published release is v$published_highest. A maintainer must reconcile them."
		return 0
	fi

	# Side lanes once trust is synced: the Homebrew cask follows the latest
	# stable release, and the merged trust-sync branch is deleted.
	tap=${HIKYO_HOMEBREW_TAP_REPOSITORY:-${GH_REPO%/*}/homebrew-tap}
	cask=
	if tap_gh api "repos/$tap/contents/Casks/hikyo.rb?ref=main" >"$work/cask.json" 2>"$work/err"; then
		jq -er '.content' "$work/cask.json" >"$work/cask.b64" || die 'the tap cask answer has no content'
		base64 -d <"$work/cask.b64" >"$work/cask.rb" || die 'cannot decode the Homebrew cask'
		[ -s "$work/cask.rb" ] || die 'the Homebrew cask is empty'
		cask=$(sed -n 's/^[[:space:]]*version[[:space:]]*"\([^"]*\)".*/\1/p' "$work/cask.rb" | head -n 1)
	else
		grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die "cannot read the cask of $tap"; }
	fi
	[ "$cask" = "$latest" ] || l_homebrew=$latest
	if ref_exists "heads/release/sync-trust-$highest"; then l_cleanup="release/sync-trust-$highest"; fi

	if [ "$highest" != "$latest" ]; then
		# trust and the published releases agree on a prerelease above the stable line:
		# there is no stable signed latest to build a patch on.
		decision none "the signed latest v$highest is a prerelease; an unattended patch needs a stable signed latest"
		escalate "the signed latest v$highest is a prerelease, so the unattended patch release of v$latest cannot run"
		return 0
	fi

	if ref_exists "tags/$next_tag"; then
		tag_commit "$next_tag"
		api "$work/runs.json" --paginate "repos/$GH_REPO/actions/workflows/release.yml/runs?event=push&head_sha=$tagged_commit&per_page=100"
		# Only the exact push of this tag at this commit counts, whoever started it.
		# shellcheck disable=SC2154
		jq -s -c --arg tag "$next_tag" --arg commit "$tagged_commit" "$release_run_jq"'[.[].workflow_runs[] |
			select(.head_sha == $commit and .head_branch == $tag and .event == "push" and release_run)]' \
			"$work/runs.json" >"$work/tag-runs.json"
		jq -c --argjson bot "$STABLE_RELEASE_APP_BOT_ID" '[.[] | select(.actor.id == $bot and .triggering_actor.id == $bot)] | sort_by(.run_number, .run_attempt) | last // empty' \
			"$work/tag-runs.json" >"$work/bot-run.json"
		jq -c --argjson bot "$STABLE_RELEASE_APP_BOT_ID" '[.[] | select(.actor.id == $bot and .triggering_actor.id != $bot)] | sort_by(.run_number, .run_attempt) | last // empty' \
			"$work/tag-runs.json" >"$work/rerun.json"
		if [ "$(jq 'length' "$work/tag-runs.json")" = 0 ]; then
			decision wait "$next_tag exists and its release-build run has not started" "$next"
			escalate "$next_tag exists but no release-build run has started"
		elif [ -s "$work/rerun.json" ] && [ ! -s "$work/bot-run.json" ]; then
			# GitHub keeps only the latest attempt of a run, with the re-runner as triggering
			# actor. The tag authority refuses such a run for the reconciler and for the ceremony.
			rerun_id=$(jq -er '.id' "$work/rerun.json")
			decision none "the release-build run $rerun_id of $next_tag was re-run by a person, so no one can publish its draft"
			want_issue open "The app pushed $next_tag, but a person re-ran its release-build run $rerun_id. GitHub then reports the person as the triggering actor and the tag authority refuses the draft for the reconciler and for the ceremony alike. The tag is immutable. Recovery: delete the draft (the tag stays burned): gh release delete $next_tag --repo $GH_REPO --yes. Then release the next patch with scripts/release/ceremony.sh; do not re-run an app build."
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
				elif ! authority_at "$tagged_commit"; then
					decision none "$next_tag was tagged at $tagged_commit, which predates the actor-based tag authority; its draft is not published unattended"
					want_issue open "The app pushed $next_tag at $tagged_commit, whose release workflow cannot tell who pushed a tag, so the draft is not published unattended. Delete the draft (the tag stays burned): gh release delete $next_tag --repo $GH_REPO --yes. Then release the next patch with scripts/release/ceremony.sh."
				elif ! floor_check "$tagged_commit" "$(jq -er '.created_at' "$work/bot-run.json")"; then
					decision none "the draft of $next_tag is not published: $floor_reason"
					want_issue open "The draft of $next_tag is not published unattended: $floor_reason. The tag is immutable. Delete the draft (the tag stays burned): gh release delete $next_tag --repo $GH_REPO --yes. Then release the next patch with scripts/release/ceremony.sh."
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
	# The walk stops at the commit that holds the security merge (nothing older
	# contains it) and after 60 commits.
	target=
	checked=0
	while read -r commit; do
		checked=$((checked + 1))
		[ "$checked" -le 60 ] || break
		at=$("$GIT_BIN" -C "$repo_root" show "$commit:release/trust/metadata.json" 2>/dev/null | jq -r '.highest_release // empty' || true)
		if [ "$at" = "$latest" ] && is_green "$commit"; then target=$commit; break; fi
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
	rc=0
	(tag_checks "$next" "$target" compat) >"$work/refusal" 2>&1 || rc=$?
	case $rc in
	0) ;;
	3)
		refusal=$(tail -n 1 "$work/refusal" | sed 's/^stable-reconcile: //' | tr '\n' ' ')
		decision none "the tag would be refused: $refusal"
		escalate "the next tag would be refused: $refusal"
		return 0
		;;
	*)
		cat "$work/refusal" >&2
		die 'a lookup failed while checking the next tag; nothing was decided'
		;;
	esac
	decision tag "merged security updates are missing from $latest_tag; $target is the newest green commit at or after them" "$next" "$target"
	want_issue close
}

decision() { # action reason [version commit run artifact]
	d_action=$1 d_reason=$2 d_version=${3:-} d_commit=${4:-} d_run=${5:-} d_artifact=${6:-}
}
want_issue() { i_action=$1 i_text=${2:-}; }
# raise TEXT: a condition that must open the stall issue whatever else the plan decides.
raise() { alarms="${alarms:+$alarms }$1."; }
# While a security merge waits for its release, every stuck state alerts after a day.
escalate() {
	[ -s "$work/pending" ] || return 0
	oldest=$(sort -k2 "$work/pending" | head -n 1 | cut -d' ' -f2)
	age=$(($(date +%s) - $(epoch "$oldest")))
	if [ "$age" -gt "$escalate_after" ]; then
		want_issue open "A merged Dependabot security update has waited more than 24 hours for its release: $1."
	fi
}
# A trust sync that has been outstanding for a day alerts with or without a pending
# security merge: until it lands, nothing else can be released from main.
escalate_trust_lag() {
	published_at=$(jq -er '.published_at' "$work/published.json") || die 'the published release has no publication time'
	age=$(($(date +%s) - $(epoch "$published_at")))
	if [ "$age" -gt "$escalate_after" ]; then
		want_issue open "The trust synchronization of v$1 has been outstanding for more than 24 hours. Check the trust-sync pull request and the stable-release-reconciler runs, or run scripts/release/ceremony.sh sync-trust."
	fi
}

plan() {
	: "${GITHUB_SHA:?}"
	need_sha "$GITHUB_SHA"
	need_bot
	d_action=none d_reason='' d_version='' d_commit='' d_run='' d_artifact='' i_action=none i_text='' l_automerge='' l_homebrew='' l_cleanup='' alarms=''
	plan_inner
	# An alarm is never erased by a later "close": it is what a silent plan would hide.
	if [ -n "$alarms" ]; then
		note "stable-reconcile: ALARM: $alarms"
		if [ "$i_action" = open ]; then i_text="$i_text $alarms"; else i_action=open i_text=$alarms; fi
	fi
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
	jq -e --arg tag "v$VERSION" --arg commit "$tagged_commit" --arg repo "$GH_REPO" --argjson bot "$STABLE_RELEASE_APP_BOT_ID" "$release_run_jq"'
		release_run and .event == "push" and .head_branch == $tag and .head_sha == $commit and
		.repository.full_name == $repo and .status == "completed" and .conclusion == "success" and
		.actor.id == $bot and .triggering_actor.id == $bot and .actor.type == "Bot" and .triggering_actor.type == "Bot"
	' "$work/run.json" >/dev/null || die "run $RUN_ID is not a successful release-build of v$VERSION started and triggered by the stable-release app; a person's release is published by the ceremony"
	digest=$(cat "$DIGEST_FILE")
	printf '%s\n' "$digest" | grep -Eq '^[0-9a-f]{64}$' || die 'the retained manifest digest is not a SHA-256'
	# The plan decided this from the same reads; look again, because the draft is about to become public.
	authority_at "$tagged_commit" || die "v$VERSION was tagged at $tagged_commit, which predates the actor-based tag authority"
	: "${GITHUB_SHA:?}"
	need_sha "$GITHUB_SHA"
	read_published
	security_prs >"$work/security.json"
	read_pending
	floor_check "$tagged_commit" "$(jq -er '.created_at' "$work/run.json")" || die "v$VERSION is not published: $floor_reason"
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

# Sets bot_login and bot_email: the app's commit identity, which every commit it
# writes signs off with (the DCO check needs the trailer to equal the author).
bot_identity() {
	[ -n "${APP_SLUG:-}" ] || die 'APP_SLUG is required'
	need_bot
	printf '%s\n' "$APP_SLUG" | grep -Eq '^[A-Za-z0-9-]+$' || die 'invalid app slug'
	bot_login="${APP_SLUG}[bot]"
	bot_email="$STABLE_RELEASE_APP_BOT_ID+$bot_login@users.noreply.github.com"
}

sync_trust() {
	: "${VERSION:?}"
	need_release "$VERSION"
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
	bot_identity
	# check-dco.sh needs this trailer to equal the commit author exactly.
	body="Public release trust evidence for v$VERSION, verified against the published release.

Signed-off-by: $bot_login <$bot_email>"
	: >"$work/additions.jsonl"
	for name in $files; do
		encoded=$(b64_oneline "$download/$name")
		jq -n --arg path "release/trust/$name" --arg contents "$encoded" '{path: $path, contents: $contents}' >>"$work/additions.jsonl" || die 'cannot build the trust additions'
	done
	additions=$(jq -s '.' "$work/additions.jsonl") || die 'cannot build the trust additions'
	[ "$(printf '%s' "$additions" | jq 'length')" = 4 ] || die 'the trust commit must carry exactly four files'
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
			jq -r '.content // empty' "$work/branch-file.json" >"$work/branch-file.b64" || die "cannot read $name on $branch"
			have=$(tr -d '\n' <"$work/branch-file.b64")
			want=$(b64_oneline "$download/$name")
			[ -n "$have" ] && [ "$have" = "$want" ] || matches=false
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
		# The clock starts at the first pull request ever opened from this branch, so closing
		# and reopening or replacing the pull request never restarts the patience.
		api "$work/pr-history.json" --paginate "repos/$GH_REPO/pulls?state=all&base=main&head=${GH_REPO%/*}:$branch&per_page=100"
		opened=$(jq -s -er '[.[][] | .created_at] | sort | first' "$work/pr-history.json") || die "cannot tell when the first trust PR of $branch was opened"
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
	bot_identity
	tap=${HIKYO_HOMEBREW_TAP_REPOSITORY:-${GH_REPO%/*}/homebrew-tap}
	verify_public "$VERSION"
	HIKYO_HOMEBREW_SIGNOFF="$bot_login <$bot_email>" "$HOMEBREW_BIN" "$GH_REPO" "v$VERSION" "$download" "$tap" >"$work/homebrew.out" || { cat "$work/homebrew.out"; die 'cannot update the tap'; }
	cat "$work/homebrew.out"
	url=$(sed -n 's/^homebrew publish: review and merge \(https:\/\/github.com\/.*\/pull\/[0-9][0-9]*\)$/\1/p' "$work/homebrew.out")
	[ -n "$url" ] || return 0
	number=${url##*/}
	api "$work/tap-pr.json" "repos/$tap/pulls/$number"
	sha=$(jq -er '.head.sha' "$work/tap-pr.json") || die "cannot read tap PR #$number"
	need_sha "$sha"
	# The tap disables auto-merge, so merge once its required check is green; a
	# later run retries while the check is still pending.
	# Only the newest run of the check counts: a later failure or a re-run in
	# progress outranks an older success.
	api "$work/tap-checks.json" "repos/$tap/commits/$sha/check-runs?check_name=ci-required&filter=latest&per_page=100"
	if jq -e '[.check_runs[]] | sort_by(.started_at // "", .id) | last | .conclusion == "success"' "$work/tap-checks.json" >/dev/null; then
		title=$(jq -er '.title' "$work/tap-pr.json") || die "cannot read the title of tap PR #$number"
		gh api --method PUT "repos/$tap/pulls/$number/merge" -f merge_method=squash -f sha="$sha" \
			-f commit_title="$title (#$number)" -f commit_message="Signed-off-by: $bot_login <$bot_email>" >/dev/null || die "cannot merge tap PR #$number"
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
	case "$BRANCH" in release/sync-trust-*) is_semver "${BRANCH#release/sync-trust-}" ;; *) false ;; esac || die 'invalid trust-sync branch'
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

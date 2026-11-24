#!/bin/sh
# Prepares the maintainer's tag for a merged Dependabot security update. It
# tags nothing and publishes nothing: it decides what the release would be,
# checks everything a person would otherwise check by hand, and writes ONE issue
# with exact commands for the signed tag and the existing release ceremony.
#
#   plan            read-only: decide the issue (open, close or leave) into $PLAN_OUT
#   issue FILE      apply a plan file to the single issue (the only write)
#   issue --failed RUN_URL
#                   the same single issue, when the planning run failed or timed out
#   recheck patch|minor VERSION COMMIT [--tip]
#                   the first link of the issue's command, run by the maintainer
#                   with their own gh login: refuse when anything changed since
#                   the plan, above all when a security merge is not in COMMIT
#
# A patch is planned first. When the compatibility declaration at the commit that
# would be tagged differs from the last stable release's, a patch cannot carry the
# change (compatibility must stay unchanged for a patch): the issue then proposes the
# next minor, with the prerequisites a person must confirm before tagging.
#
# Environment: GH_REPO, GH_TOKEN; plan also GITHUB_SHA (main's tip, from a
# checkout with full history). The scripts never read a private key.
#
# Every read that decides something goes through api(), which stops the run when
# GitHub cannot answer. A failed lookup must never look like an empty answer.
# A rule that says no (refuse, exit 3) is reported in the issue; a failed read
# or an unreadable value (die) fails the run instead. Compatibility that differs
# is its own answer (differs, exit 5): it turns a patch into a minor proposal.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=${HIKYO_REPO_ROOT:-$(CDPATH='' cd -- "$script_dir/../.." && pwd)}
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
: "${GH_BIN:=gh}" "${GO_BIN:=go}" "${GIT_BIN:=git}"
: "${GREEN_BIN:=$script_dir/require-green-main.sh}"
: "${SYNCED_BIN:=$script_dir/require-synced-trust.sh}"
: "${COMPAT_BIN:=$script_dir/check-compatibility-unchanged.sh}"
: "${FALLBACK_BIN:=}"
: "${POSTGRES_BIN:=$script_dir/../ci/start-ci-postgres.sh}"
# Numeric ids, never logins: Dependabot's bot user.
dependabot_user_id=49699333
web_flow_user_id=19864447
github_actions_user_id=41898282
# GitHub Actions' app id: the only author of the admission check that counts.
actions_app_id=15368
issue_title='Security patch release: maintainer action needed'
issue_marker='<!-- security-patch-plan -->'
# At most this many first-parent commits are examined for a target.
max_walk=60
# A merge that waited longer than this is called out in the issue.
overdue_after=86400
# The scratch database the compatibility generator needs; release.yml uses the same one.
scratch_postgres_dsn='postgres://hikyo:release-schema-fixture@127.0.0.1:5432/hikyo_release_schema?sslmode=disable'
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-security-patch.XXXXXX")
# The scratch PostgreSQL container this run started, if any (start-ci-postgres.sh refuses to
# replace an existing one, so a container that this run's start succeeded for is its own).
# POSTGRES_STOP_BIN overrides the removal in the fixture; override it together with
# POSTGRES_BIN, or the removal targets hikyo-ci-postgres by name.
: "${POSTGRES_STOP_BIN:=}"
stop_postgres() {
	[ -e "$work/postgres-owned" ] || return 0
	rm -f "$work/postgres-owned"
	if [ -n "$POSTGRES_STOP_BIN" ]; then "$POSTGRES_STOP_BIN" >/dev/null 2>&1 || true; else docker container rm --force hikyo-ci-postgres >/dev/null 2>&1 || true; fi
}
trap 'stop_postgres; rm -rf "$work"' EXIT
# A signal ends the run (through the EXIT cleanup) instead of continuing without $work.
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
trust="$repo_root/release/trust"
# What the issue's commands run from the target commit's own tree: the recheck and
# its helpers, the hooks, the signature check, the verifiers, and the Go packages
# the verifiers build from (scripts/ci checks this list against `go list -deps`, so
# a new dependency cannot slip out of it).
tool_paths='scripts/release/security-patch-plan.sh scripts/release/require-green-main.sh scripts/release/require-synced-trust.sh scripts/release/check-compatibility-unchanged.sh scripts/lib/release.sh scripts/git .githooks scripts/ci/check-commit-signatures.sh scripts/ci/check-fallback-channel-test.sh scripts/ci/source-proof scripts/release/stable go.mod go.sum internal/definitions internal/domain internal/jsonwire internal/releaseidentity internal/releasetrust internal/schema'
# The fallback-channel gate release.yml asserts before it builds (a tag whose
# build dies on it is burned). scripts/ci checks these against release.yml.
fallback_record=release/repository/fallback-channel-test.json
fallback_address=security@hikyo.app

die() {
	printf 'security-patch-plan: %s\n' "$*" >&2
	exit 1
}
refuse() {
	printf 'security-patch-plan: %s\n' "$*" >&2
	exit 3
}
differs() {
	printf 'security-patch-plan: %s\n' "$*" >&2
	exit 5
}
note() {
	printf '%s\n' "$*"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi
}
# command: GH_BIN defaults to gh, which would otherwise call this function again.
gh() { command "$GH_BIN" "$@"; }
go_stable() { "$GO_BIN" -C "$repo_root" run ./scripts/release/stable "$@"; }
git_() { "$GIT_BIN" -C "$repo_root" "$@"; }
# The maintainer's recheck reads the repository the ceremony uses; CI always passes GH_REPO.
# shortcut: run addresses are compared case-sensitively, so GH_REPO must use GitHub's own spelling of owner/name.
case "${1:-}" in recheck) : "${GH_REPO:=${HIKYO_REPOSITORY:-Hikyo-Org/Hikyo}}" ;; esac
[ -n "${GH_REPO:-}" ] || die 'GH_REPO is required'
# matches REGEX VALUE: the whole value matches, anchored at both ends. grep reads line by
# line, so a value with a newline in it would pass a grep ^...$ guard on its first line.
matches() { jq -n -e --arg pattern "$1" --arg value "$2" '$value | test("\\A(?:" + $pattern + ")\\z")' >/dev/null 2>&1; }
matches '[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+' "$GH_REPO" || die 'invalid repository'
# The address of one of this repository's own workflow runs (the dots of the repository are escaped).
run_url_pattern="https://github\\.com/$(printf '%s' "$GH_REPO" | sed 's/\./\\./g')/actions/runs/[0-9]+"
# api FILE ARGS...: read an endpoint into FILE. A failed read stops the run.
api() {
	api_file=$1
	shift
	gh api "$@" >"$api_file" 2>"$work/err" </dev/null || { cat "$work/err" >&2; die "cannot read: gh api $*"; }
}
need_sha() { matches '[0-9a-f]{40}' "${1:-}" || die "a full commit SHA is required: ${1:-empty}"; }
patch_successor() { printf '%s\n' "$1" | awk -F. '{printf "%d.%d.%d\n", $1, $2, $3 + 1}'; }
minor_successor() { printf '%s\n' "$1" | awk -F. '{printf "%d.%d.0\n", $1, $2 + 1}'; }
# SemVer precedence key: a release outranks its own prerelease; numeric
# prerelease identifiers sort below alphanumeric ones.
semver_key_jq='def key: capture("^(?<core>[0-9]+\\.[0-9]+\\.[0-9]+)(?:-(?<pre>[0-9A-Za-z.-]+))?(?:\\+.*)?$") |
	[(.core | split(".") | map(tonumber)),
	 (if .pre == null then [1] else [0, (.pre | split(".") | map(if test("^[0-9]+$") then tonumber else . end))] end)];'
semver_gt() {
	jq -n -e --arg a "$1" --arg b "$2" "$semver_key_jq"'($a | key) > ($b | key)' >/dev/null
}
# epoch TIMESTAMP: seconds since 1970 for a strict UTC timestamp. Anything else,
# including a value that GNU date would read as "tomorrow", stops the run: an
# unreadable time must never turn into "not required" or "not overdue".
epoch() {
	matches '[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z' "$1" || die "unparseable timestamp: $1"
	epoch_value=$(date -u -d "$1" +%s 2>/dev/null || date -u -j -f %Y-%m-%dT%H:%M:%SZ "$1" +%s 2>/dev/null) || die "unparseable timestamp: $1"
	printf '%s\n' "$epoch_value" | grep -Eq '^[0-9]+$' || die "unparseable timestamp: $1"
	# BSD date normalizes an impossible date (February 30) instead of refusing it; the value
	# must format back to exactly what was read.
	epoch_back=$(date -u -d "@$epoch_value" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -r "$epoch_value" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || die "unparseable timestamp: $1"
	[ "$epoch_back" = "$1" ] || die "unparseable timestamp: $1"
	printf '%s\n' "$epoch_value"
}
# is_green COMMIT: 0 = complete green source proof, 1 = not proven green. The
# verifier exits 1 for "no such proof" and 4 for a failed GitHub read (any other
# status is a failure too): "could not look" is never the same as "not green".
is_green() {
	green_rc=0
	"$GREEN_BIN" "$GH_REPO" "$1" </dev/null >"$work/green.out" 2>&1 || green_rc=$?
	case $green_rc in
	0) return 0 ;;
	1) return 1 ;;
	*) cat "$work/green.out" >&2; die "cannot read the source proof of $1" ;;
	esac
}
# is_ancestor A B: 0 yes, 1 no; any other result stops the run.
is_ancestor() {
	rc=0
	git_ merge-base --is-ancestor "$1" "$2" 2>/dev/null || rc=$?
	case $rc in
	0) return 0 ;;
	1) return 1 ;;
	*) die "cannot compare $1 with $2" ;;
	esac
}

# The ONE definition of a Dependabot security pull request, used by both the listing
# and the unlinked-alert count: Dependabot's numeric author id, base main, a
# dependabot/ branch. A pull request that fails it explains nothing.
# shellcheck disable=SC2016
security_pr_def='def is_security_pr($bot): . != null and .author.databaseId == $bot and .baseRefName == "main" and ((.headRefName // "") | startswith("dependabot/"));'
# A title is untrusted text that lands in an issue: keep a small alphabet and never a URL scheme.
clean_title_def='def clean_title: gsub("[^A-Za-z0-9 ._:/+(),-]"; "") | until((contains("://") or test("www\\."; "i")) | not; gsub("://"; "") | gsub("www\\."; ""; "i")) | .[0:100];'

# Reads every Dependabot alert (all states listed explicitly, so a change of the
# default cannot hide the fixed ones) and prints the pull requests Dependabot
# itself linked to an alert. The author is bound by its numeric id. An answer with
# no alert at all is cross-checked over REST: GraphQL can hand back an empty list
# where REST would say "forbidden".
security_prs() {
	# shellcheck disable=SC2016
	query='query($owner:String!,$name:String!,$endCursor:String){repository(owner:$owner,name:$name){vulnerabilityAlerts(first:100,after:$endCursor,states:[OPEN,FIXED]){pageInfo{hasNextPage endCursor}nodes{state fixedAt dependabotUpdate{pullRequest{number title state merged mergedAt baseRefName headRefName author{login ... on Bot{databaseId}} mergeCommit{oid}}}}}}}'
	gh api graphql --paginate -f query="$query" -f owner="${GH_REPO%/*}" -f name="${GH_REPO#*/}" >"$work/alerts" ||
		die 'cannot read Dependabot alerts'
	jq -es 'all(.[]; .data.repository.vulnerabilityAlerts.nodes | type == "array")' "$work/alerts" >/dev/null ||
		die 'the Dependabot alert answer is incomplete'
	if jq -s -e '[.[].data.repository.vulnerabilityAlerts.nodes[]] | length == 0' "$work/alerts" >/dev/null; then
		api "$work/alerts-rest.json" "repos/$GH_REPO/dependabot/alerts?state=open,fixed&per_page=1"
		jq -e 'type == "array" and length == 0' "$work/alerts-rest.json" >/dev/null ||
			die 'GraphQL listed no Dependabot alerts but REST lists some (or answered unexpectedly); nothing is decided'
	fi
	jq -s -c --argjson bot "$dependabot_user_id" "$security_pr_def"'[.[].data.repository.vulnerabilityAlerts.nodes[].dependabotUpdate.pullRequest | select(is_security_pr($bot))] | unique_by(.number)' "$work/alerts"
}

# Step 0, read only: the open Dependabot security pull requests into main. GitHub's
# built-in token cannot add a pull request to the merge queue, so nothing here
# queues anything: a person clicks "Merge when ready" on each one that qualifies.
# A pull request qualifies when an OPEN Dependabot alert links it, Dependabot (by
# numeric id) opened it from a branch of this repository, it is not a draft, every
# commit is authored by Dependabot, committed by GitHub's web-flow and verified,
# and the admission check (ci-queue-required, GitHub Actions' app, newest run only)
# has not failed. One that the merge queue removed after its newest commit needs a
# person to look at why before it is queued again. Sets open_block (markdown, empty
# when there is nothing to say) and leaves one JSON line per pull request in
# $work/open.json.
read_open_security() {
	open_block=
	: >"$work/open.json"
	# shellcheck disable=SC2016
	open_query='query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){number title state isDraft isInMergeQueue baseRefName headRefOid headRepository{nameWithOwner} author{login ... on Bot{databaseId}} timelineItems(itemTypes:[REMOVED_FROM_MERGE_QUEUE_EVENT],last:20){nodes{... on RemovedFromMergeQueueEvent{createdAt}}}}}}'
	jq -s -r '[.[].data.repository.vulnerabilityAlerts.nodes[] | select(.state == "OPEN") | .dependabotUpdate.pullRequest |
		select(. != null and .state == "OPEN" and .baseRefName == "main")] | unique_by(.number) | .[].number' "$work/alerts" >"$work/open-numbers" ||
		die 'cannot read the open Dependabot alerts'
	while read -r number; do
		[ -n "$number" ] || continue
		printf '%s\n' "$number" | grep -Eq '^[0-9]+$' || die "unreadable pull request number: $number"
		api "$work/open-$number.graphql" graphql -f query="$open_query" -f owner="${GH_REPO%/*}" -f name="${GH_REPO#*/}" -F number="$number"
		jq -e '.data.repository.pullRequest.number != null' "$work/open-$number.graphql" >/dev/null || die "cannot read pull request #$number"
		head=$(jq -er '.data.repository.pullRequest.headRefOid' "$work/open-$number.graphql") || die "pull request #$number has no head commit"
		need_sha "$head"
		api "$work/open-$number.commits" --paginate --slurp "repos/$GH_REPO/pulls/$number/commits?per_page=100"
		api "$work/open-$number.checks" --paginate --slurp "repos/$GH_REPO/commits/$head/check-runs?check_name=ci-queue-required&app_id=$actions_app_id&per_page=100"
		jq -nc --slurpfile pr "$work/open-$number.graphql" --slurpfile commits "$work/open-$number.commits" --slurpfile checks "$work/open-$number.checks" \
			--arg repo "$GH_REPO" --argjson bot "$dependabot_user_id" --argjson flow "$web_flow_user_id" "$clean_title_def"'
			($pr[0].data.repository.pullRequest) as $p | ([$commits[0][][]]) as $c | ([$checks[0][].check_runs[]?] | max_by(.id)) as $run |
			($c | length > 0 and all(.[]; .author.id == $bot and .committer.id == $flow and .commit.verification.verified == true) and (.[-1].sha == $p.headRefOid)) as $signed |
			(($run == null) or ($run.status != "completed") or ($run.conclusion == "success")) as $admitted |
			($c | if length > 0 then .[-1].commit.committer.date else "" end) as $pushed |
			[$p.timelineItems.nodes[]?.createdAt | select(. > $pushed)] as $removed |
			{number: $p.number, title: ($p.title | clean_title), head: $p.headRefOid,
			 queued: ($p.isInMergeQueue == true), removed: ($removed | length > 0),
			 reason: (if $p.author.databaseId != $bot then "not opened by Dependabot"
				elif ($p.headRepository.nameWithOwner // "") != $repo then "its branch is not in this repository"
				elif $p.isDraft then "it is a draft"
				elif $signed | not then "its commits are not all Dependabot-authored, GitHub-committed and verified"
				elif $admitted | not then "the admission check failed"
				else null end)}' >>"$work/open.json" || die "cannot classify pull request #$number"
	done <"$work/open-numbers"
	jq -r 'select(.queued | not) | select(.reason == null and (.removed | not)) | "- #\(.number) \(.title), head `\(.head)`"' "$work/open.json" >"$work/open-ready"
	jq -r 'select(.queued | not) | select(.reason == null and .removed) | "- #\(.number) \(.title), head `\(.head)`"' "$work/open.json" >"$work/open-removed"
	jq -r 'select(.queued | not) | select(.reason != null) | "- #\(.number) \(.title): \(.reason)"' "$work/open.json" >"$work/open-other"
	if [ -s "$work/open-ready" ] || [ -s "$work/open-removed" ] || [ -s "$work/open-other" ]; then
		open_block="### 0. Open security updates: a person queues each one

GitHub's built-in token cannot add a pull request to the merge queue, so the automation does not. Open each pull request below and click **Merge when ready**."
		if [ -s "$work/open-ready" ]; then open_block="$open_block

Qualifying (alert-linked, Dependabot-authored, verified, not draft, admission check not failed):

$(cat "$work/open-ready")"; fi
		if [ -s "$work/open-removed" ]; then open_block="$open_block

Removed from the merge queue after their newest commit. Find out why before queueing them again; they need a person:

$(cat "$work/open-removed")"; fi
		if [ -s "$work/open-other" ]; then open_block="$open_block

Open and alert-linked, but not qualifying (review by hand):

$(cat "$work/open-other")"; fi
	fi
}

# What is published decides, never GitHub's "latest" flag: the highest published
# release of any channel (what trust must record) and the highest stable one
# (what a patch builds on). Drafts and nightlies never count as published. Sets
# published_highest, latest, latest_tag, latest_commit, latest_published_at, next
# and minor (the successors of the latest stable), and leaves every release including drafts in $work/releases.json.
read_published() {
	api "$work/releases.json" --paginate --slurp "repos/$GH_REPO/releases?per_page=100"
	jq -c "$semver_key_jq"'
		[.[][] | select(.draft == false and (.tag_name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+(-[A-Za-z][0-9A-Za-z.-]*)?$") and (contains("-nightly.") | not)))] as $published |
		($published | map(.tag_name | ltrimstr("v"))) as $all |
		{highest: ($all | max_by(key)), stable: ($all | map(select(contains("-") | not)) | max_by(key))}
	' "$work/releases.json" >"$work/published.json" || die 'cannot read the published releases'
	published_highest=$(jq -r '.highest // empty' "$work/published.json")
	latest=$(jq -r '.stable // empty' "$work/published.json")
	[ -n "$latest" ] || die 'there is no published stable release'
	latest_tag="v$latest"
	latest_published_at=$(jq -r --arg tag "$latest_tag" '[.[][] | select(.tag_name == $tag)][0].published_at // empty' "$work/releases.json")
	[ -n "$latest_published_at" ] || die "$latest_tag has no publication time"
	api "$work/latest-commit.json" "repos/$GH_REPO/commits/$latest_tag"
	latest_commit=$(jq -er '.sha' "$work/latest-commit.json") || die "cannot resolve $latest_tag"
	need_sha "$latest_commit"
	next=$(patch_successor "$latest")
	minor=$(minor_successor "$latest")
	trust_highest=$(jq -r '.highest_release // empty' "$trust/metadata.json")
}

# tag_run VERSION: the release-build runs of the tag push. Sets run_state (none: no run
# found; going: a run has not finished or ended without a verdict; success: a run
# succeeded, a re-run of failed jobs counts; failed: every run ended in failure) and
# run_url (the newest run's page). The planner's contents:read token cannot be relied on
# to list draft releases, so "staged" means a succeeded run, never the draft listing.
tag_run() {
	api "$work/runs-$1.json" "repos/$GH_REPO/actions/workflows/release.yml/runs?event=push&branch=v$1&per_page=20"
	# A listing without a runs array is a broken answer, never "no run".
	jq -e '.workflow_runs | type == "array"' "$work/runs-$1.json" >/dev/null || die "the release-build runs of v$1 are not a list"
	run_state=$(jq -er --arg tag "v$1" '[.workflow_runs[] | select(.head_branch == $tag and .event == "push")] |
		if length == 0 then "none"
		elif any(.[]; .status == "completed" and .conclusion == "success") then "success"
		elif all(.[]; .status == "completed" and (.conclusion | IN("failure", "cancelled", "timed_out", "startup_failure"))) then "failed"
		else "going" end' "$work/runs-$1.json") || die "cannot read the release-build runs of v$1"
	# The address reaches the issue: only this repository's own run page, whole value, is kept.
	run_url=$(jq -r --arg tag "v$1" --arg pattern "\\A(?:$run_url_pattern)\\z" '[.workflow_runs[] | select(.head_branch == $tag and .event == "push")][0].html_url // "" |
		if . == "" or test($pattern) then . else error("unreadable run address") end' "$work/runs-$1.json") ||
		die "unreadable release-build run address of v$1"
}

# is_burned VERSION: 0 when the tag exists, no PUBLISHED release is attached to it, and
# every release-build run of the tag push has ended without success. A run still going,
# a run that succeeded, a published release or no run at all is not burned: the tag may
# yet complete. Drafts never count: the plan's token cannot be relied on to list them
# and the maintainer's recheck can, so a rule that looked at drafts would let the two
# disagree about which version comes next. A run that failed after it created the draft
# is therefore burned, and the issue tells the maintainer to delete that draft. A pushed
# tag is never moved (the ceremony's rule), so a failed build is what makes its version
# unusable, once the failed jobs cannot be re-run to completion (the alarm says to try
# that first).
is_burned() {
	jq -s -e --arg ref "refs/tags/v$1" 'any(.[][]; .ref == $ref)' "$work/matching-tags.json" >/dev/null || return 1
	if jq -e --arg tag "v$1" 'any(.[][]; .tag_name == $tag and .draft == false)' "$work/releases.json" >/dev/null; then return 1; fi
	tag_run "$1"
	[ "$run_state" = failed ]
}

# The exact, checked deletion of the draft a failed run may have left, per burned tag. The
# tags passed the stable tag pattern, so they are safe in markdown and in a command line.
# The backticks are markdown.
# shellcheck disable=SC2016
draft_delete_commands() {
	for tag in $burned_tags; do
		printf 'For %s: check that `gh release view %s --repo %s --json isDraft -q .isDraft` prints `true`, then run `gh release delete %s --repo %s --yes`. ' "$tag" "$tag" "$GH_REPO" "$tag" "$GH_REPO"
	done
	printf 'Never pass `--cleanup-tag`, which would delete the tag.'
}

# burn KIND VERSION: record a burned version (KIND is patch or minor) and the failed
# run's page for the alarm. burned_tags lists every burned tag for the tag-order rule.
burn() {
	burned_note="${burned_note:+$burned_note, }v$2"
	burned_runs="${burned_runs:+$burned_runs; }v$2 (${run_url:-no run address})"
	burned_tags="${burned_tags:+$burned_tags }v$2"
	case $1 in
	patch) burned_patch_note="${burned_patch_note:+$burned_patch_note, }v$2" ;;
	*) burned_minor_note="${burned_minor_note:+$burned_minor_note, }v$2" ;;
	esac
	skips=$((skips + 1))
	[ "$skips" -le 5 ] || die 'more than five burned versions; a person must look'
}

# skip_burned: move the patch version and the minor version past versions a failed
# release-build burned, so the issue never offers a tag that cannot be used. Sets
# burned_note and burned_runs (empty when none), burned_patch_note and burned_minor_note
# (which of them were skipped as patches and as minors), and burned_tags (read by the tag
# order rule: a burned tag above the next patch does not forbid it). Leaves the tag list
# in $work/matching-tags.json. Needs read_published.
skip_burned() {
	burned_note='' burned_runs='' burned_patch_note='' burned_minor_note='' burned_tags=''
	api "$work/matching-tags.json" --paginate "repos/$GH_REPO/git/matching-refs/tags/v"
	skips=0
	while is_burned "$next"; do
		burn patch "$next"
		next=$(patch_successor "$next")
	done
	while is_burned "$minor"; do
		burn minor "$minor"
		minor=$(minor_successor "$minor")
	done
}

# Merged security updates whose merge commit the latest stable tag lacks, as
# "OID PR MERGED_AT" lines in $work/pending. A merge that cannot be placed in
# history is raised, never skipped: skipping it would look like "nothing to release".
read_pending() {
	: >"$work/pending"
	placement_blocked=
	jq -r '.[] | select(.merged == true) | "\(.number) \(.mergeCommit.oid // "none") \(.mergedAt // "none")"' "$work/security.json" >"$work/merged"
	while read -r number oid merged_at; do
		[ -n "$number" ] || continue
		if [ "$oid" = none ] || [ "$merged_at" = none ]; then
			raise "security pull request #$number is merged but GitHub reports no merge commit or merge time for it"
			placement_blocked=1
			continue
		fi
		need_sha "$oid"
		if ! git_ cat-file -e "$oid^{commit}" 2>/dev/null || ! is_ancestor "$oid" "$GITHUB_SHA"; then
			raise "the merge commit $oid of security pull request #$number is not in main's history"
			placement_blocked=1
			continue
		fi
		if ! is_ancestor "$oid" "$latest_commit"; then
			# Every state may show this time, so an unreadable one stops the run here.
			epoch "$merged_at" >/dev/null
			printf '%s %s %s\n' "$oid" "$number" "$merged_at" >>"$work/pending"
		fi
	done <"$work/merged"
}

# contains_all COMMIT: 0 when COMMIT holds every pending security merge.
contains_all() {
	while read -r oid _number _at; do
		[ -n "$oid" ] || continue
		is_ancestor "$oid" "$1" || return 1
	done <"$work/pending"
	return 0
}

# Everything the build and the ceremony would refuse, so the issue never offers a
# tag that would die. Exit 3 (refuse) = a rule says no; exit 5 = the compatibility
# declaration differs from the last stable release's. Any other failure = a read
# failed; the run fails instead of pretending the answer was no. The third
# argument runs the compatibility step: patch (it must be identical) or minor (the
# declaration must generate; a difference is expected and left to a person).
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
	# Every attempt writes into its own directory: the candidate and the declaration are
	# created with O_EXCL, and the patch attempt and the minor attempt run in one workspace.
	attempt_dir="$work/attempt-$checks_version"
	mkdir -p "$attempt_dir" || die "cannot create $attempt_dir"
	go_stable candidate --trust "$trust" --version "$checks_version" --commit "$checks_commit" \
		--out "$attempt_dir/candidate.json" || refuse "v$checks_version is not a valid successor of the signed latest stable release"
	git_ cat-file -e "$checks_commit^{commit}" 2>/dev/null || refuse 'the commit is not on main history'
	is_ancestor "$checks_commit" "$main_tip" || refuse 'the commit is not on main history'
	is_green "$checks_commit" || refuse "$checks_commit has no complete green source proof"
	fallback_check "$checks_commit"
	edge_check "$checks_commit" "$checks_version"
	api "$work/matching-tags.json" --paginate "repos/$GH_REPO/git/matching-refs/tags/v"
	# A tag whose build failed and left no release is burned and orders nothing: only the
	# planner enforces this rule (release.yml and the ceremony do not order tags), and a burned
	# minor must not forbid a patch that is otherwise valid.
	jq -s -e --arg tag "v$checks_version" --arg burned "$burned_tags" '
		def triple: ltrimstr("v") | capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)") | [.a, .b, .c | tonumber];
		($burned | split(" ")) as $dead |
		([.[][] | .ref | ltrimstr("refs/tags/") | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+") and (contains("-nightly.") | not) and (. as $t | $dead | index($t) | not)) | triple | select(. >= ($tag | triple))] | length) == 0
	' "$work/matching-tags.json" >/dev/null || refuse "a stable tag at or above v$checks_version already exists"
	if gh api "repos/$GH_REPO/releases/tags/v$checks_version" >/dev/null 2>"$work/err"; then refuse "v$checks_version already has a release"; fi
	grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die 'cannot prove the version is unused'; }
	[ -z "$checks_compat" ] || compat_checks "$checks_version" "$checks_commit" "$checks_compat"
}

# fallback_check COMMIT: release.yml asserts the fallback-channel record before it
# builds, and a tag whose build dies on it is burned. Run the target tree's own script
# on the target tree's own record, so a stale record stops the plan, not the build.
# Exit 1 of the gate is a refusal; any other failure (a missing node, say) is not an answer.
fallback_check() {
	fb_dir="$work/fallback-$1"
	mkdir -p "$fb_dir"
	git_ archive -o "$work/fallback.tar" "$1" scripts/ci/check-fallback-channel-test.sh "$fallback_record" 2>"$work/err" ||
		refuse "$1 carries no fallback channel gate or record ($(tr '\n' ' ' <"$work/err"))"
	tar -x -f "$work/fallback.tar" -C "$fb_dir" || die "cannot unpack the fallback channel record of $1"
	fb_bin=${FALLBACK_BIN:-$fb_dir/scripts/ci/check-fallback-channel-test.sh}
	fb_rc=0
	(cd "$fb_dir" && "$fb_bin" "$fallback_record" "$fallback_address") >"$work/fallback.out" 2>&1 </dev/null || fb_rc=$?
	case $fb_rc in
	0) ;;
	1) refuse "the fallback channel gate would fail the build of $1: $(tail -n 1 "$work/fallback.out")" ;;
	*) cat "$work/fallback.out" >&2; die 'cannot run the fallback channel gate' ;;
	esac
}

# edge_check COMMIT VERSION: the target's release/compatibility/sources.json must carry
# the reviewed release-source edge from the latest stable release for each engine,
# checked against that release's own verified declaration (check-compatibility-unchanged.sh
# --edges-only, the same rule the pre-tag compatibility check applies; release.yml runs
# no edge check at all, so this and that check are the only mechanical gates). A missing or wrong edge
# is a blocking prerequisite that names what to add; an unverifiable baseline is not an
# answer. Writes the problems to $work/edge-blocked when it refuses.
edge_check() {
	ec_dir="$work/edges-$1"
	mkdir -p "$ec_dir"
	rm -f "$work/edge-blocked"
	git_ archive -o "$work/edges.tar" "$1" release/trust release/compatibility/sources.json 2>"$work/err" ||
		die "cannot read release/compatibility/sources.json and release/trust at $1"
	tar -x -f "$work/edges.tar" -C "$ec_dir" || die "cannot unpack the compatibility sources of $1"
	ec_rc=0
	"$COMPAT_BIN" --edges-only "$GH_REPO" "$ec_dir/release/trust" "$2" "$ec_dir/release/compatibility/sources.json" >"$work/edges.out" 2>&1 </dev/null || ec_rc=$?
	case $ec_rc in
	0) ;;
	1)
		cp "$work/edges.out" "$work/edge-blocked"
		refuse "release/compatibility/sources.json at $1 lacks the reviewed release-source edge from $latest_tag: $(sed -e 's/^compatibility check: //' "$work/edges.out" | tr '\n' ' ' | sed 's/ *$//')"
		;;
	5) refuse "$(tail -n 1 "$work/edges.out")" ;;
	*) cat "$work/edges.out" >&2; die "cannot check the release-source edge of $1" ;;
	esac
}

# The candidate's compatibility declaration is generated at the commit that would
# be tagged, with the real generator on scratch PostgreSQL, and compared with the
# previous stable release's published one. PostgreSQL starts only here, when a
# tag is otherwise ready.
compat_checks() {
	cc_version=$1
	cc_commit=$2
	cc_kind=$3
	cc_tree="$attempt_dir/target-tree"
	mkdir "$cc_tree"
	git_ archive "$cc_commit" | tar -x -C "$cc_tree" || die "cannot export $cc_commit"
	[ -f "$cc_tree/release/compatibility/sources.json" ] || die "$cc_commit carries no release/compatibility/sources.json"
	POSTGRES_PASSWORD=release-schema-fixture POSTGRES_DB=hikyo_release_schema POSTGRES_HEALTH_RETRIES=12 \
		"$POSTGRES_BIN" >/dev/null || die 'cannot start scratch PostgreSQL for the compatibility declaration'
	: >"$work/postgres-owned" || die 'cannot record the scratch PostgreSQL this run started'
	# The generator needs an empty scratch database, and the starter refuses to replace its
	# container: each attempt starts one and removes it again before the next can start.
	gen_rc=0
	HIKYO_RELEASE_SCHEMA_POSTGRES_DSN=$scratch_postgres_dsn "$GO_BIN" -C "$cc_tree" run ./scripts/release/compatibility \
		--candidate "$attempt_dir/candidate.json" --out "$attempt_dir/declaration.json" || gen_rc=$?
	stop_postgres
	[ "$gen_rc" = 0 ] || die "cannot generate the compatibility declaration of $cc_commit"
	rc=0
	"$COMPAT_BIN" "$GH_REPO" "$cc_tree/release/trust" "$cc_version" "$attempt_dir/declaration.json" >"$work/compat.out" 2>&1 || rc=$?
	case $rc in
	0) [ "$cc_kind" = patch ] || refuse "the compatibility declaration is identical to $latest_tag's, so a patch release is enough" ;;
	1)
		# Only the check's own "differs" verdict, which it prints last, is a difference. Any
		# other refusal (an incomplete declaration, a baseline that needs a recovery bridge)
		# is not one: it neither turns a patch into a minor nor lets a minor through. The
		# diff before the verdict is untrusted text and decides nothing.
		cc_last=$(tail -n 1 "$work/compat.out")
		case $cc_last in
		'compatibility check: the compatibility declaration differs from the one v'*)
			if [ "$cc_kind" = patch ]; then differs "$cc_last"; fi
			;;
		*) refuse "the compatibility check refused the declaration of $cc_commit: $cc_last" ;;
		esac
		;;
	5) refuse "$(tail -n 1 "$work/compat.out")" ;;
	*) cat "$work/compat.out" >&2; die 'cannot compare the compatibility declaration with the previous release' ;;
	esac
}

# unlinked_count: Dependabot alerts fixed after the latest stable release that no
# merged Dependabot security pull request on main (the same filter security_prs
# applies) explains (a promotion that bumped the dependency, or a fix by hand,
# leaves one until the next stable release is published). A fixed alert without a
# time counts too. An unreadable time fails the run. Needs read_published and
# security_prs to have run.
unlinked_count() {
	# A fixedAt that jq would read leniently must round-trip to the same text, or the run stops.
	jq -s --argjson since "$(epoch "$latest_published_at")" --argjson bot "$dependabot_user_id" "$security_pr_def"'
		def fixed_epoch: if (.fixedAt | fromdateiso8601 | todateiso8601) == .fixedAt then (.fixedAt | fromdateiso8601) else error("unparseable fixedAt") end;
		[.[].data.repository.vulnerabilityAlerts.nodes[] |
		select(.state == "FIXED" and (((.fixedAt // "") == "") or (fixed_epoch > $since)) and
			((.dependabotUpdate.pullRequest | is_security_pr($bot) and .merged == true) | not))] | length' "$work/alerts" ||
		die 'cannot read the fixed Dependabot alerts'
}

# raise TEXT: a condition that must reach the issue whatever else the plan decides.
raise() { alarms="${alarms:+$alarms
}- $1."; }

# --- plan -------------------------------------------------------------------

# Sets state (idle, blocked, ready, tagged, drafted, trust) and the fields the
# issue renders: why, version, kind (patch or minor), target.
plan_inner() {
	read_published
	security_prs >"$work/security.json"
	# A Dependabot alert that was fixed after the latest release but whose pull
	# request is missing or not merged would leave "no pending merge" looking like
	# "nothing to release". An unreadable time fails the run.
	unlinked=$(unlinked_count)
	if [ "$unlinked" -gt 0 ]; then
		raise "$unlinked Dependabot alert(s) were fixed after $latest_tag but none is linked to a merged Dependabot pull request on main, so their fix cannot be traced to a commit; check the alerts and decide by hand. No tag command is offered while this holds. The alarm clears once a stable release is published after the fix; publish the fix through the ordinary release ceremony, by hand"
	fi
	read_pending
	read_open_security
	skip_burned
	if [ -n "$burned_note" ] && [ -s "$work/pending" ]; then
		raise "the release-build run of $burned_runs failed and no published release exists for it, so this plan skips that version. If the failed run already created a draft release for the tag, delete that draft first (it stops the trust synchronization check, and re-running the failed jobs cannot create the release again while it exists; this plan cannot see drafts). Then re-run the failed jobs of the run: a failure before the build (validation, docs, floor-bench, everything build-artifacts needs) is almost always rerunnable, and the skip clears once a re-run succeeds. Only if the byte set cannot be completed safely, use the next version this issue plans (a pushed tag is never moved or recreated). $(draft_delete_commands)"
	fi
	version=$next kind=patch
	if [ -n "$placement_blocked" ]; then
		state=blocked
		why="a merged security update cannot be placed in main's history (see below), so no release commit can be chosen"
		return 0
	fi
	# A published release that trust does not record yet keeps the issue open, whether
	# or not a security merge is still pending: the next release refuses to start
	# until trust is synchronized, and the issue must not close before that.
	if [ -z "$trust_highest" ] || semver_gt "$published_highest" "$trust_highest"; then
		state=trust
		why="v$published_highest is published but main's trust ends at ${trust_highest:+v}${trust_highest:-nothing}; trust must be synchronized before another release"
		return 0
	fi
	if [ ! -s "$work/pending" ]; then
		state=idle
		why="no merged Dependabot security update is missing from $latest_tag"
		return 0
	fi
	if semver_gt "$trust_highest" "$published_highest"; then
		state=blocked
		why="main's signed trust names v$trust_highest, but the highest published release is v$published_highest; a maintainer must reconcile them"
		return 0
	fi
	if [ "$trust_highest" != "$latest" ]; then
		state=blocked
		why="the signed latest v$trust_highest is a prerelease; a patch needs a stable signed latest"
		return 0
	fi
	# The version may already be tagged or staged (as a patch or as the minor): say so,
	# never offer a second tag. Staged means the tag's release-build run succeeded: the
	# draft listing is not trusted (the token may not see drafts), and a failed run that
	# already created a draft is not staged.
	for candidate_version in "$next" "$minor"; do
		if jq -s -e --arg ref "refs/tags/v$candidate_version" 'any(.[][]; .ref == $ref)' "$work/matching-tags.json" >/dev/null; then
			version=$candidate_version
			[ "$version" != "$minor" ] || kind=minor
			tag_run "$version"
			run_at=${run_url:+ ($run_url)}
			if [ "$run_state" = success ]; then
				state=drafted
				why="v$version is tagged and its release-build run succeeded$run_at, so its draft release is staged (this run cannot list drafts; \`review\` shows the real one)"
				# The tag was pushed at some commit: a security merge that landed after it is not in
				# this release and stays pending once it is published.
				api "$work/tag-commit.json" "repos/$GH_REPO/commits/v$version"
				tag_commit=$(jq -er '.sha' "$work/tag-commit.json") || die "cannot resolve v$version"
				need_sha "$tag_commit"
				if ! git_ cat-file -e "$tag_commit^{commit}" 2>/dev/null || ! contains_all "$tag_commit"; then
					why="$why. Its commit $tag_commit does not hold every security merge listed below, so those stay pending after this release is published and need a further release (this issue stays open until then)"
				fi
			else
				state=tagged
				case $run_state in
				going) why="v$version is tagged, and its release-build run has not finished or ended without a verdict$run_at" ;;
				failed) why="v$version is tagged and its release-build run failed$run_at, yet a release is already attached to the tag; do not trust it until you know why the run failed" ;;
				*) why="v$version is tagged, but no release-build run was found for that tag push, so nothing says the build ever started" ;;
				esac
				state_note=$(tagged_note)
			fi
			return 0
		fi
	done
	if [ "$unlinked" -gt 0 ]; then
		state=blocked
		why="$unlinked Dependabot alert(s) fixed after $latest_tag are not traced to a merged security pull request on main (see below), so what the release would hold is unknown"
		return 0
	fi
	# Candidates are the first-parent commits since the latest tag, newest first. A pending merge
	# that main reaches only through a side parent is held by the merge commit that brought it, so
	# contains_all alone decides which candidates qualify.
	git_ rev-list --first-parent "$latest_commit..$GITHUB_SHA" >"$work/chain"
	# The build reads trust at the tag commit, so a commit whose trust predates the
	# latest release would burn the tag. Take only commits that already record it
	# and hold every pending security merge. The walk stops at the first commit
	# that lacks one (nothing older holds it) and after max_walk commits.
	target=
	checked=0
	while read -r commit; do
		checked=$((checked + 1))
		[ "$checked" -le "$max_walk" ] || break
		contains_all "$commit" || break
		recorded=
		if git_ show "$commit:release/trust/metadata.json" >"$work/commit-trust.json" 2>/dev/null; then
			recorded=$(jq -r '.highest_release // empty' "$work/commit-trust.json") || die "cannot read the trust metadata at $commit"
		fi
		if [ "$recorded" = "$latest" ]; then
			if is_green "$commit"; then target=$commit; break; fi
		fi
	done <"$work/chain"
	if [ -z "$target" ]; then
		state=blocked
		why="no main commit among the newest $max_walk at or after the newest security merge both records $latest_tag in trust and has complete green source proof; run the full ci-main workflow on the main tip"
		return 0
	fi
	# The issue's commands run the recheck, its hooks and helpers from the target's own tree.
	# Below the tip, that tree is only as trustworthy as the tip's: accept it when none of
	# those tools differs.
	if [ "$target" != "$GITHUB_SHA" ]; then
		tools_rc=0
		# shellcheck disable=SC2086
		git_ diff --quiet "$target" "$GITHUB_SHA" -- $tool_paths || tools_rc=$?
		case $tools_rc in
		0) ;;
		1)
			state=blocked
			why="$target is the newest green commit that holds every pending security merge, but the release tools differ between it and the main tip $GITHUB_SHA, and the issue's commands would run them from $target; wait for the tip to become green"
			return 0
			;;
		*) die "cannot compare the release tools of $target with the main tip" ;;
		esac
	fi
	rc=0
	main_tip=$GITHUB_SHA
	edge_block=
	(tag_checks "$next" "$target" patch) >"$work/refusal" 2>&1 || rc=$?
	if [ "$rc" = 5 ]; then
		# A patch cannot carry a compatibility change. Plan the next minor instead, if it
		# passes every other check. A burned minor tag does not make this a minor: only a
		# declaration that differs does.
		compat_note=$(tail -n 1 "$work/refusal" | sed -e 's/^security-patch-plan: //' -e 's/^compatibility check: //' -e 's/ *$//' | tr '\n' ' ' | sed 's/ *$//')
		version=$minor kind=minor
		rc=0
		(tag_checks "$minor" "$target" minor) >"$work/refusal" 2>&1 || rc=$?
	fi
	case $rc in
	0) ;;
	3)
		state=blocked
		if [ -s "$work/edge-blocked" ]; then
			why="the release-source edge from $latest_tag is missing or wrong in release/compatibility/sources.json at $target, so an installed $latest_tag would have no upgrade route to v$version; a person must add it (below)"
			edge_block=$(edge_prerequisite)
		else
			why="the tag would be refused: $(tail -n 1 "$work/refusal" | sed 's/^security-patch-plan: //' | tr '\n' ' ' | sed 's/ *$//')"
		fi
		return 0
		;;
	*)
		cat "$work/refusal" >&2
		die 'a lookup failed while checking the next tag; nothing was decided'
		;;
	esac
	state=ready
	if [ "$kind" = minor ]; then
		why="$target is the newest green commit that holds every pending security merge. A patch cannot ship it: $compat_note"
	else
		why="$target is the newest green commit that holds every pending security merge"
	fi
	[ "$target" = "$GITHUB_SHA" ] || why="$why (the main tip $GITHUB_SHA is not green)"
}

# Pending merges as markdown bullets.
pending_list() {
	while read -r oid number merged_at; do
		[ -n "$oid" ] || continue
		# The backticks are markdown.
		# shellcheck disable=SC2016
		printf -- '- #%s, merge commit `%s`, merged %s\n' "$number" "$oid" "$merged_at"
	done <"$work/pending"
}

overdue_note() {
	[ -s "$work/pending" ] || return 0
	oldest=$(sort -k3 "$work/pending" | head -n 1 | cut -d' ' -f3)
	oldest_epoch=$(epoch "$oldest")
	if [ $(($(date +%s) - oldest_epoch)) -gt "$overdue_after" ]; then
		printf '\nThe oldest of these has waited more than 24 hours for its release.\n'
	fi
}

# The four ceremony commands after the tag, each from a fresh checkout of main: a
# command that ran before it (sync-trust opens a branch) must not leave the next one elsewhere.
ceremony_block() {
	printf '%s\n' \
		"git fetch origin main && git switch --detach origin/main && scripts/release/ceremony.sh review $1" \
		"git fetch origin main && git switch --detach origin/main && scripts/release/ceremony.sh publish $1 REVIEWED_MANIFEST_SHA256" \
		"git fetch origin main && git switch --detach origin/main && scripts/release/ceremony.sh sync-trust $1" \
		"git fetch origin main && git switch --detach origin/main && scripts/release/ceremony.sh homebrew $1"
}

burned_tag_hint="A pushed tag is never moved or recreated. If \`release-build\` fails, inspect the run first. If it already created the draft release, delete that draft before anything else: re-running the failed jobs cannot create the release while it exists, and an outstanding stable draft stops the trust synchronization check. Then re-run its failed jobs: a failure before the build (validation, docs, floor-bench, everything \`build-artifacts\` needs) is almost always rerunnable. Only if the byte set cannot be completed safely is the version burned, and you then use a new version (the ceremony's own rule). To delete it, check that \`gh release view vVERSION --repo $GH_REPO --json isDraft -q .isDraft\` prints \`true\`, then run \`gh release delete vVERSION --repo $GH_REPO --yes\`; never pass \`--cleanup-tag\`, which would delete the tag."

# What a person needs when a tag exists without a staged release: the run, what to try and
# the way out the planner cannot take for them.
tagged_note() {
	cat <<EOF
$burned_tag_hint

${run_url:+Release-build run: $run_url

}If the run cannot be completed, treat the version as burned. The plan then skips it once its run has failed and no published release is attached (a draft the failed run left does not count; delete it); a tag with no run at all is never skipped by this plan, so the next version is tagged by hand through the ceremony after your own checks of the commit.
EOF
}

# What a person must do about a missing or wrong release-source edge. The tool gap is
# stated, not papered over: nothing authors a stable edge today.
edge_prerequisite() {
	# Only a patch is turned into a minor by a promotion; a plan that is already a minor is not.
	promotion_note=
	[ "$kind" != patch ] || promotion_note=", which turns this patch into a minor release"
	cat <<EOF
### Prerequisite that blocks tagging: the release-source edge from $latest_tag

An upgrade records the installed release by its identity, and the planner only routes it to the next release through an edge whose source is that exact release (the \`fresh/v1\` and \`legacy/v1\` genesis edges never match). \`release/compatibility/sources.json\` at \`$target\` must therefore carry, for **each engine** (\`sqlite\` and \`postgres\`), one edge from $latest_tag, whose \`migrations\` and \`schema_sha256\` are the ones $latest_tag's own verified \`upgrade-compatibility.json\` carries. This applies to a patch as well as a minor release. The check reported:

\`\`\`text
$(tr -cd '[:print:]\n' <"$work/edge-blocked" | head -n 40)
\`\`\`

How to author it: **no supported command writes a stable release edge** (\`scripts/release/nightly sources\` reads verified nightly directories only). This is a tooling gap that the release owner has been told about. Until a tool exists, write the edge by hand, in a pull request that is reviewed and merged to \`main\`. A pull request to \`main\` must come from a same-repository \`hotfix/\` branch ([branching guide](https://github.com/$GH_REPO/blob/main/docs/development/branching.md)); going through \`dev\` instead means a promotion that brings everything on \`dev\`, its migrations included, to \`main\`${promotion_note}.

Fetch and verify $latest_tag's files exactly as the check does, and read the identity the verifier prints:

\`\`\`sh
dir=\$(mktemp -d) && gh release download $latest_tag --repo $GH_REPO --dir "\$dir/files" &&
  go run ./scripts/release/stable verify --trust release/trust --trust-only --state "\$dir/state.json" &&
  go run ./scripts/release/stable verify --trust release/trust --directory "\$dir/files" --version ${latest_tag#v} \\
    --commit "\$(jq -r .source_commit "\$dir/files/release-manifest.json")" --state "\$dir/state.json" --historical --published
\`\`\`

It prints \`{"identity": {...}, "published_oci_verified": true}\`. Each engine's edge is: \`source.release\` is that \`identity\` object verbatim (\`profile\`, \`version\`, \`sequence\`, \`commit\`, \`compatibility_sha256\`, \`manifest_sha256\`), \`migrations\` and \`schema_sha256\` are copied from that engine's entry in $latest_tag's \`upgrade-compatibility.json\`, and \`mode\` is the reviewer's decision, see [signed upgrade compatibility](https://github.com/$GH_REPO/blob/main/docs/adr/signed-upgrade-compatibility.md). \`restart\` is valid only when the release's migrations and schema digest are unchanged from $latest_tag's (\`internal/upgradecompat/declaration.go\` refuses it otherwise); \`maintenance\` is the choice when they changed. This issue is rewritten once the edge is on main; \`recheck\` enforces the same check against the commit you tag.

EOF
}

# What a person must confirm before a minor release: not mechanically satisfiable, so
# these block tagging.
minor_prerequisites() {
	cat <<EOF
### Prerequisites that block tagging (a minor release)

The planning run checked, for \`$target\`: trust is synchronized and authenticated, \`v$version\` is a valid successor of $latest_tag, the commit has complete green source proof, the fallback channel gate passes, no tag or release at or above \`v$version\` exists, \`release/compatibility/sources.json\` carries a sound release-source edge from $latest_tag for each engine, and the real compatibility generator produced a declaration. The declaration **differs** from $latest_tag's, which is why a patch cannot ship this commit. The run cannot tell whether a difference is intended: that is yours to confirm from the diff below. A minor release ships everything on that commit, not only the security fix. Confirm each of these yourself before the commands below:

- [ ] Read the difference below, which the planning run generated for \`$target\` itself, and confirm that it is the unreleased migrations and nothing else (no changed earlier migration, no unexpected source edge or schema change). The \`release-compatibility-check\` workflow compares main's tip, which is not this commit unless the tip is the target, so it is no substitute.
- [ ] Review the edge modes (\`restart\` or \`maintenance\`) of the release-source edges and of the new release's declaration against [signed upgrade compatibility](https://github.com/$GH_REPO/blob/main/docs/adr/signed-upgrade-compatibility.md).
- [ ] Decide that everything on \`$target\` since $latest_tag, not only the security fix, may ship: \`git log $latest_tag..$target\`.

<details>
<summary>The compatibility difference the planning run saw (at most 200 lines)</summary>

\`\`\`diff
$(tr -cd '[:print:]\n' <"$work/compat.out" | head -n 200)
\`\`\`

</details>

EOF
}

# The backticks are markdown.
# shellcheck disable=SC2016
minor_notes_note() {
	printf 'A minor release needs real release notes: `publish` refuses the placeholder, so write them before it.'
}

render_ready() {
	if [ "$kind" = minor ]; then
		heading="Security release v$version (minor) is ready to tag once you have confirmed its prerequisites"
		if [ -n "$burned_minor_note" ]; then successor="the next unused minor of $latest_tag after the burned $burned_minor_note"; else successor="the next minor of $latest_tag"; fi
		checked="trust synchronized and authenticated; \`v$version\` is $successor; green source proof; the fallback channel gate passes; the commit holds every merge above; no tag or release for \`v$version\` exists; the release-source edge from $latest_tag is sound; the compatibility generator produced a declaration, which differs from $latest_tag's."
	else
		heading="Security patch v$version is ready to tag"
		if [ -n "$burned_patch_note" ]; then successor="the next unused patch version of $latest_tag after the burned $burned_patch_note"; else successor="the patch successor of $latest_tag"; fi
		checked="trust synchronized and authenticated; \`v$version\` is $successor; green source proof; the fallback channel gate passes; the commit holds every merge above; no tag or release for \`v$version\` exists; the release-source edge from $latest_tag is sound; the compatibility declaration is identical to $latest_tag's apart from that edge."
	fi
	if [ "$target" = "$GITHUB_SHA" ]; then
		tag_block=$(printf '%s\n' \
			'git fetch origin main &&' \
			'  git switch --detach origin/main &&' \
			"  scripts/release/security-patch-plan.sh recheck $kind $version $target --tip &&" \
			"  scripts/release/ceremony.sh build $version")
		tag_expect="\`recheck\` prints \`ok to tag v$version\`, then the ceremony asks you to confirm \`build v$version at $target\`. If \`recheck\` refuses, nothing was tagged: read its message and wait for this issue to refresh."
	else
		tag_block=$(printf '%s\n' \
			'git fetch origin main &&' \
			"  git switch --detach $target &&" \
			"  scripts/release/security-patch-plan.sh recheck $kind $version $target &&" \
			'  scripts/git/install-hooks.sh &&' \
			"  git tag -s v$version -m \"Hikyo $version\" $target &&" \
			'  git fetch hikyo-upstream main &&' \
			'  scripts/ci/check-commit-signatures.sh origin/main HEAD &&' \
			"  git push origin refs/tags/v$version")
		tag_expect="\`recheck\` prints \`ok to tag v$version\`, then your signed tag \`v$version\` is pushed at $target after the signature check passes. The main tip is not green, so the ceremony's \`build\` (which tags the tip) cannot be used; this tags the same commit with the same signature. If \`recheck\` refuses, nothing was tagged."
	fi
	prerequisites='' notes_note='' burned_block=''
	# A skipped version changes what step 1 tags, so it is said where the commands are, not only
	# under "Also needs a person".
	if [ -n "$burned_note" ]; then
		burned_block="**Skipped versions:** the release-build run of $burned_runs failed and no published release exists for it, so this plan does not offer that version. If the failed run left a draft release, delete it before step 1 (the plan cannot see drafts, and an outstanding stable draft stops the trust synchronization check). $(draft_delete_commands)"
	fi
	if [ "$kind" = minor ]; then
		prerequisites=$(minor_prerequisites)
		notes_note=$(minor_notes_note)
	fi
	cat <<EOF
$issue_marker
## $heading

Merged Dependabot security updates that $latest_tag does not contain:

$(pending_list)
$(overdue_note)
**Planned release:** \`v$version\` at \`$target\`. $why.

Checked by the planning run for exactly this commit: $checked Nothing has been tagged or published.

$step0_block
$prerequisites
${burned_block:+
$burned_block
}
### 1. Tag (a maintainer with the admin role and a signing key, in a clean checkout of this repository)

\`\`\`sh
$tag_block
\`\`\`

Expected: $tag_expect

$burned_tag_hint

### 2. Review, publish and finish (once \`release-build\` has staged the draft)

\`\`\`sh
$(ceremony_block "$version")
\`\`\`

${notes_note:+$notes_note

}Expected: \`review\` prints a manifest SHA-256; paste it in place of \`REVIEWED_MANIFEST_SHA256\`. Each later command asks for its own confirmation. Merge the trust PR and the Homebrew tap PR after review. Homebrew stays a manual step (owner decision, 2026-11-16); this issue closes once trust records the release, so run \`homebrew\` right after the trust PR has merged.

This issue is rewritten when the state changes. Use the commands in its latest version, and close nothing by hand: it closes itself once the security merges are released and trust records the release.
EOF
	printf '%s\n' "$alarms_block"
}

render() {
	alarms_block=
	[ -z "$alarms" ] || alarms_block=$(printf '\n### Also needs a person\n\n%s\n' "$alarms")
	trust_commands="git fetch origin main && git switch --detach origin/main && scripts/release/ceremony.sh sync-trust $published_highest"
	case $published_highest in
	*-*) ;;
	*) trust_commands="$trust_commands
git fetch origin main && git switch --detach origin/main && scripts/release/ceremony.sh homebrew $published_highest" ;;
	esac
	step0_block=
	[ -z "$open_block" ] || step0_block=$(printf '%s\n' "$open_block")
	case $state in
	idle)
		if [ -z "$alarms" ] && [ -z "$open_block" ]; then action=close; printf '%s\nnothing is waiting\n' "$issue_marker"; return 0; fi
		action=open
		printf '%s\n## Dependabot security updates need a look\n\n%s\n\n%s\n%s\n' "$issue_marker" "$why." "$step0_block" "$alarms_block"
		;;
	ready)
		action=open
		render_ready
		;;
	drafted)
		action=open
		drafted_notes=
		[ "$kind" != minor ] || drafted_notes=$(minor_notes_note)
		cat <<EOF
$issue_marker
## Security release v$version is staged: finish the ceremony

$why. Merged Dependabot security updates that $latest_tag does not contain:

$(pending_list)

$step0_block

\`\`\`sh
$(ceremony_block "$version")
\`\`\`

${drafted_notes:+$drafted_notes

}Expected: \`review\` prints a manifest SHA-256; paste it in place of \`REVIEWED_MANIFEST_SHA256\`. Homebrew stays manual; this issue closes once trust records the release.

$burned_tag_hint
$alarms_block
EOF
		;;
	trust)
		action=open
		cat <<EOF
$issue_marker
## Security patch blocked: synchronize trust first

$why.
$(if [ -s "$work/pending" ]; then printf '\nMerged Dependabot security updates waiting for a release:\n\n%s\n' "$(pending_list)"; fi)

$step0_block

If the public release is verified, record it, then update the Homebrew tap once the trust PR has merged:

\`\`\`sh
$trust_commands
\`\`\`

Expected: a trust PR opens. Merge it after review and green CI; this issue then offers the tag or closes. Homebrew stays manual (owner decision, 2026-11-16): run \`homebrew\` after the trust PR has merged.
$alarms_block
EOF
		;;
	*)
		action=open
		cat <<EOF
$issue_marker
## Security patch release cannot be prepared

$why.
$(if [ -s "$work/pending" ]; then printf '\nMerged Dependabot security updates waiting for a release:\n\n%s\n' "$(pending_list)"; fi)
$(overdue_note)
$step0_block
${edge_block:+
$edge_block
}${state_note:+
$state_note
}
No tag command is offered while this holds. This issue is rewritten when the state changes.
$alarms_block
EOF
		;;
	esac
}

plan() {
	: "${GITHUB_SHA:?}"
	need_sha "$GITHUB_SHA"
	main_tip=$GITHUB_SHA
	: "${PLAN_OUT:=$work/plan.json}"
	state=idle why='' target='' alarms='' alarms_block='' edge_block='' state_note='' burned_runs='' burned_note='' burned_patch_note='' burned_minor_note='' burned_tags='' action=none version='' kind=patch open_block='' step0_block='' placement_blocked=''
	plan_inner
	render >"$work/body.md" || die 'cannot render the issue'
	jq -n --arg action "$action" --rawfile body "$work/body.md" '{action:$action, body:$body}' >"$PLAN_OUT" || die 'cannot write the plan'
	note "security-patch-plan: $state: $why"
	[ "$action" = open ] || [ "$action" = close ] || note 'security-patch-plan: nothing to report'
}

# --- issue ------------------------------------------------------------------

# find_issue: sets number to our single open issue, or empty. The workflow's own bot
# is bound by its numeric user id, never by a login spelling (gh and REST disagree on
# it), and a pull request is never an issue. A listing that fails is never "no issue".
find_issue() {
	api "$work/issues.json" --paginate --slurp "repos/$GH_REPO/issues?state=open&creator=github-actions%5Bbot%5D&per_page=100"
	number=$(jq -r --arg title "$issue_title" --arg marker "$issue_marker" --argjson bot "$github_actions_user_id" '
		[.[][] | select(.pull_request == null and .user.type == "Bot" and .user.id == $bot and .title == $title and ((.body // "") | startswith($marker)))][0].number // empty' "$work/issues.json") ||
		die 'cannot read the open issues'
}

issue() {
	if [ "${1:-}" = --failed ]; then
		if [ "$#" -ne 2 ] || ! matches "$run_url_pattern" "$2"; then die 'usage: security-patch-plan.sh issue --failed RUN_URL'; fi
		# The planning run failed or timed out, so nothing was decided: say so in the same
		# issue instead of leaving a stale one (or none) looking healthy.
		# The backticks are markdown.
		# shellcheck disable=SC2016
		printf '%s\n## Security patch planning failed\n\nThe planning run did not finish: %s\n\nNothing was decided, so the commands in any earlier version of this issue are stale. They stay guarded by `recheck`, which refuses when anything changed. This issue is rewritten by the next run that succeeds.\n' "$issue_marker" "$2" >"$work/new-body"
		action=open
		new=$(cat "$work/new-body")
	else
		[ "$#" -eq 1 ] && [ -f "$1" ] || die 'usage: security-patch-plan.sh issue PLAN_FILE | --failed RUN_URL'
		action=$(jq -er '.action | select(. == "open" or . == "close" or . == "none")' "$1") || die 'the plan has no valid action'
		jq -er '.body | select(type == "string" and length > 0 and length < 60000)' "$1" >"$work/new-body" || die 'the plan has no usable body'
		new=$(jq -r '.body | gsub("\r"; "")' "$1")
	fi
	find_issue
	case "$action" in
	open)
		if [ -n "$number" ]; then
			# Rewrite only on a change, so the issue is not edited every hour.
			if [ "$(jq -r --argjson n "$number" '[.[][] | select(.number == $n)][0].body // "" | gsub("\r"; "")' "$work/issues.json")" = "$new" ]; then
				note "security-patch-plan: issue #$number is current"
			else
				gh issue edit "$number" --repo "$GH_REPO" --body-file "$work/new-body"
			fi
		else
			gh issue create --repo "$GH_REPO" --title "$issue_title" --body-file "$work/new-body"
		fi
		;;
	close)
		[ -z "$number" ] || gh issue close "$number" --repo "$GH_REPO" --comment 'The merged security updates are released and trust records the release; nothing is waiting.'
		;;
	none) ;;
	esac
}

# --- recheck ----------------------------------------------------------------

# The first link of the maintainer's command. It repeats, at tag time and with
# the maintainer's own login, what the issue claimed when it was written. A
# security merge that landed after the issue was written must stop the tag:
# a person tags, so there is no grace period. KIND is the kind of release the
# issue planned: the version must be that kind's successor of the latest stable
# release. A patch also must not carry a change to the migrations or the
# compatibility sources since that release (the planning run proved the exact
# declaration; this is the cheap guard at tag time). A minor is a person's
# decision, confirmed against the prerequisites the issue lists.
recheck() {
	[ "$#" -ge 3 ] && [ "$#" -le 4 ] || die 'usage: security-patch-plan.sh recheck patch|minor VERSION COMMIT [--tip]'
	kind=$1 version=$2 commit=$3 tip_only=${4:-}
	case $kind in patch | minor) ;; *) die 'the release kind must be patch or minor' ;; esac
	matches '[0-9]+\.[0-9]+\.[0-9]+' "$version" || die 'a plain stable version X.Y.Z is required'
	need_sha "$commit"
	[ -z "$tip_only" ] || [ "$tip_only" = --tip ] || die 'the only option is --tip'
	[ "$(git_ rev-parse HEAD)" = "$commit" ] || refuse "this checkout is not at $commit; run the issue's commands from their first line"
	# The ceremony's build and a signed tag both start from a clean checkout.
	[ -z "$(git_ status --porcelain)" ] || refuse 'this checkout has uncommitted or untracked changes; use a clean checkout'
	main_tip=$(git_ rev-parse origin/main) || die 'cannot resolve origin/main; run git fetch origin main'
	GITHUB_SHA=$main_tip
	[ "$tip_only" != --tip ] || [ "$commit" = "$main_tip" ] || refuse "main moved to $main_tip since the issue was written; wait for it to refresh"
	read_published
	skip_burned
	if [ "$kind" = minor ]; then expected=$minor; else expected=$next; fi
	# The issue's commands run these tools from $commit's own tree. Below the tip, accept
	# that only while none of them differs from the tip's (the plan checked the same).
	if [ "$commit" != "$main_tip" ]; then
		tools_rc=0
		# shellcheck disable=SC2086
		git_ diff --quiet "$commit" "$main_tip" -- $tool_paths || tools_rc=$?
		case $tools_rc in
		0) ;;
		1) refuse "the release tools differ between $commit and the main tip $main_tip, and these commands run them from $commit; wait for the issue to refresh" ;;
		*) die "cannot compare the release tools of $commit with the main tip" ;;
		esac
	fi
	[ "$version" = "$expected" ] || refuse "v$version is not the $kind successor of $latest_tag (v$expected); the issue is out of date"
	security_prs >"$work/security.json"
	# An alert fixed without a traceable merge is decided by hand, not tagged over.
	[ "$(unlinked_count)" -eq 0 ] || refuse "Dependabot alerts were fixed after $latest_tag without a linked merged pull request on main; decide by hand (see the issue's note on clearing this)"
	alarms=
	read_pending
	[ -z "$alarms" ] || refuse "a security merge cannot be placed in main's history: $alarms"
	[ -s "$work/pending" ] || refuse "no merged Dependabot security update is missing from $latest_tag any more; nothing to release"
	contains_all "$commit" || refuse "$commit does not contain every merged security update; a newer one landed since the issue was written, so wait for it to refresh"
	[ "$trust_highest" = "$latest" ] || refuse "main's trust ends at v$trust_highest, not $latest_tag; synchronize trust first"
	if [ "$kind" = patch ]; then
		# sources.json may only gain the one edge per engine whose source is $latest_tag (the required
		# release-source edge; an exact-identity check follows in tag_checks); every other edge,
		# the migrations and everything else under release/compatibility must be unchanged.
		changed=$(git_ diff --name-only "$latest_commit" "$commit" -- internal/store/migrations release/compatibility ':(exclude)release/compatibility/sources.json') || die "cannot compare $commit with $latest_tag"
		[ -z "$changed" ] || refuse "$commit changes migrations or compatibility files since $latest_tag, which a patch must not carry; the issue will propose a minor release"
		git_ show "$latest_commit:release/compatibility/sources.json" >"$work/sources-old.json" || die "cannot read $latest_tag's release/compatibility/sources.json"
		git_ show "$commit:release/compatibility/sources.json" >"$work/sources-new.json" || die "cannot read release/compatibility/sources.json at $commit"
		# An edge is the latest release's only when its source.release is an object naming it;
		# anything else (a string, a list, a missing source, a source that also names a genesis) is not one and stays in the comparison.
		jq -n -e --arg latest "$latest" --slurpfile old "$work/sources-old.json" --slurpfile new "$work/sources-new.json" '
			def is_latest_edge: type == "object" and (.source | type) == "object" and (.source | keys) == ["release"] and (.source.release | type) == "object" and .source.release.version == $latest;
			def shaped: (.engines | type) == "object" and ([.engines[] | type == "array"] | all);
			def plain: .engines |= with_entries(.value |= map(select(is_latest_edge | not)));
			def lookalikes: [.engines[] | map(select(is_latest_edge)) | length] | max // 0;
			($old[0] | shaped) and ($new[0] | shaped) and ($new[0] | lookalikes) <= 1 and ($old[0] | plain) == ($new[0] | plain)' >/dev/null ||
			refuse "$commit changes release/compatibility/sources.json beyond adding the release-source edge from $latest_tag, which a patch must not carry; the issue will propose a minor release"
	fi
	tag_checks "$version" "$commit"
	printf 'security-patch-plan: ok to tag v%s at %s (%s release)\n' "$version" "$commit" "$kind"
}

[ "$#" -ge 1 ] || die 'usage: security-patch-plan.sh plan|issue|recheck'
command=$1
shift
case "$command" in
plan | issue | recheck) "$command" "$@" ;;
*) die "unknown command $command" ;;
esac

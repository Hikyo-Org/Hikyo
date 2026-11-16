#!/bin/sh
# Prepares the maintainer's tag for a merged Dependabot security update. It
# tags nothing and publishes nothing: it decides what the release would be,
# checks everything a person would otherwise check by hand, and writes ONE issue
# with exact commands for the signed tag and the existing release ceremony.
#
#   plan            read-only: decide the issue (open, close or leave) into $PLAN_OUT
#   issue FILE      apply a plan file to the single issue (the only write)
#   recheck VERSION COMMIT [--tip]
#                   the first link of the issue's command, run by the maintainer
#                   with their own gh login: refuse when anything changed since
#                   the plan, above all when a security merge is not in COMMIT
#
# Environment: GH_REPO, GH_TOKEN; plan also GITHUB_SHA (main's tip, from a
# checkout with full history). The scripts never read a private key.
#
# Every read that decides something goes through api(), which stops the run when
# GitHub cannot answer. A failed lookup must never look like an empty answer.
# A rule that says no (refuse, exit 3) is reported in the issue; a failed read
# or an unreadable value (die) fails the run instead.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=${HIKYO_REPO_ROOT:-$(CDPATH='' cd -- "$script_dir/../.." && pwd)}
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
: "${GH_BIN:=gh}" "${GO_BIN:=go}" "${GIT_BIN:=git}"
: "${GREEN_BIN:=$script_dir/require-green-main.sh}"
: "${SYNCED_BIN:=$script_dir/require-synced-trust.sh}"
: "${COMPAT_BIN:=$script_dir/check-compatibility-unchanged.sh}"
: "${POSTGRES_BIN:=$script_dir/../ci/start-ci-postgres.sh}"
# Numeric ids, never logins: Dependabot's bot user.
dependabot_user_id=49699333
issue_title='Security patch release: maintainer action needed'
issue_marker='<!-- security-patch-plan -->'
# At most this many first-parent commits are examined for a target.
max_walk=60
# A merge that waited longer than this is called out in the issue.
overdue_after=86400
# The scratch database the compatibility generator needs; release.yml uses the same one.
scratch_postgres_dsn='postgres://hikyo:release-schema-fixture@127.0.0.1:5432/hikyo_release_schema?sslmode=disable'
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-security-patch.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
trust="$repo_root/release/trust"

die() {
	printf 'security-patch-plan: %s\n' "$*" >&2
	exit 1
}
refuse() {
	printf 'security-patch-plan: %s\n' "$*" >&2
	exit 3
}
note() {
	printf '%s\n' "$*"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >>"$GITHUB_STEP_SUMMARY"; fi
}
gh() { "$GH_BIN" "$@"; }
go_stable() { "$GO_BIN" -C "$repo_root" run ./scripts/release/stable "$@"; }
git_() { "$GIT_BIN" -C "$repo_root" "$@"; }
# The maintainer's recheck reads the repository the ceremony uses; CI always passes GH_REPO.
case "${1:-}" in recheck) : "${GH_REPO:=${HIKYO_REPOSITORY:-Hikyo-Org/Hikyo}}" ;; esac
[ -n "${GH_REPO:-}" ] || die 'GH_REPO is required'
printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || die 'invalid repository'
# api FILE ARGS...: read an endpoint into FILE. A failed read stops the run.
api() {
	api_file=$1
	shift
	gh api "$@" >"$api_file" 2>"$work/err" || { cat "$work/err" >&2; die "cannot read: gh api $*"; }
}
need_sha() { printf '%s\n' "${1:-}" | grep -Eq '^[0-9a-f]{40}$' || die "a full commit SHA is required: ${1:-empty}"; }
patch_successor() { printf '%s\n' "$1" | awk -F. '{printf "%d.%d.%d\n", $1, $2, $3 + 1}'; }
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
	printf '%s\n' "$1" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' || die "unparseable timestamp: $1"
	epoch_value=$(date -u -d "$1" +%s 2>/dev/null || date -u -j -f %Y-%m-%dT%H:%M:%SZ "$1" +%s 2>/dev/null) || die "unparseable timestamp: $1"
	printf '%s\n' "$epoch_value" | grep -Eq '^[0-9]+$' || die "unparseable timestamp: $1"
	printf '%s\n' "$epoch_value"
}
# is_green COMMIT: 0 = complete green source proof, 1 = not proven green. A failed
# GitHub read stops the run: "could not look" is never the same as "not green".
is_green() {
	if "$GREEN_BIN" "$GH_REPO" "$1" </dev/null >"$work/green.out" 2>&1; then return 0; fi
	if grep -F 'GitHub read failed' "$work/green.out" >/dev/null; then cat "$work/green.out" >&2; die "cannot read the source proof of $1"; fi
	return 1
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

# Reads every Dependabot alert (all states listed explicitly, so a change of the
# default cannot hide the fixed ones) and prints the pull requests Dependabot
# itself linked to an alert. The author is bound by its numeric id.
security_prs() {
	# shellcheck disable=SC2016
	query='query($owner:String!,$name:String!,$endCursor:String){repository(owner:$owner,name:$name){vulnerabilityAlerts(first:100,after:$endCursor,states:[OPEN,FIXED]){pageInfo{hasNextPage endCursor}nodes{state fixedAt dependabotUpdate{pullRequest{number merged mergedAt baseRefName headRefName author{login ... on Bot{databaseId}} mergeCommit{oid}}}}}}}'
	gh api graphql --paginate -f query="$query" -f owner="${GH_REPO%/*}" -f name="${GH_REPO#*/}" >"$work/alerts" ||
		die 'cannot read Dependabot alerts'
	jq -es 'all(.[]; .data.repository.vulnerabilityAlerts.nodes | type == "array")' "$work/alerts" >/dev/null ||
		die 'the Dependabot alert answer is incomplete'
	jq -s -c --argjson bot "$dependabot_user_id" '[.[].data.repository.vulnerabilityAlerts.nodes[].dependabotUpdate.pullRequest | select(. != null) |
		select(.author.databaseId == $bot and .baseRefName == "main" and (.headRefName | startswith("dependabot/")))] | unique_by(.number)' "$work/alerts"
}

# What is published decides, never GitHub's "latest" flag: the highest published
# release of any channel (what trust must record) and the highest stable one
# (what a patch builds on). Drafts and nightlies never count as published. Sets
# published_highest, latest, latest_tag, latest_commit, latest_published_at, next
# and next_tag, and leaves every release including drafts in $work/releases.json.
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
	next_tag="v$next"
	trust_highest=$(jq -r '.highest_release // empty' "$trust/metadata.json")
}

# Merged security updates whose merge commit the latest stable tag lacks, as
# "OID PR MERGED_AT" lines in $work/pending. A merge that cannot be placed in
# history is raised, never skipped: skipping it would look like "nothing to release".
read_pending() {
	: >"$work/pending"
	jq -r '.[] | select(.merged == true) | "\(.number) \(.mergeCommit.oid // "none") \(.mergedAt // "none")"' "$work/security.json" >"$work/merged"
	while read -r number oid merged_at; do
		[ -n "$number" ] || continue
		if [ "$oid" = none ] || [ "$merged_at" = none ]; then
			raise "security pull request #$number is merged but GitHub reports no merge commit or merge time for it"
			continue
		fi
		need_sha "$oid"
		if ! git_ cat-file -e "$oid^{commit}" 2>/dev/null || ! is_ancestor "$oid" "$GITHUB_SHA"; then
			raise "the merge commit $oid of security pull request #$number is not in main's history"
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
# tag that would die. Exit 3 (refuse) = a rule says no. Any other failure = a
# read failed; the run fails instead of pretending the answer was no.
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
	go_stable candidate --trust "$trust" --version "$checks_version" --commit "$checks_commit" \
		--out "$work/candidate.json" || refuse "v$checks_version is not a valid successor of the signed latest stable release"
	git_ cat-file -e "$checks_commit^{commit}" 2>/dev/null || refuse 'the commit is not on main history'
	is_ancestor "$checks_commit" "$main_tip" || refuse 'the commit is not on main history'
	is_green "$checks_commit" || refuse "$checks_commit has no complete green source proof"
	api "$work/matching-tags.json" --paginate "repos/$GH_REPO/git/matching-refs/tags/v"
	jq -s -e --arg tag "v$checks_version" '
		def triple: ltrimstr("v") | capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)") | [.a, .b, .c | tonumber];
		([.[][] | .ref | ltrimstr("refs/tags/") | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+") and (contains("-nightly.") | not)) | triple | select(. >= ($tag | triple))] | length) == 0
	' "$work/matching-tags.json" >/dev/null || refuse "a stable tag at or above v$checks_version already exists"
	if gh api "repos/$GH_REPO/releases/tags/v$checks_version" >/dev/null 2>"$work/err"; then refuse "v$checks_version already has a release"; fi
	grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die 'cannot prove the version is unused'; }
	[ -z "$checks_compat" ] || compat_checks "$checks_version" "$checks_commit"
}

# The candidate's compatibility declaration is generated at the commit that would
# be tagged, with the real generator on scratch PostgreSQL, and compared with the
# previous stable release's published one. PostgreSQL starts only here, when a
# tag is otherwise ready.
compat_checks() {
	cc_version=$1
	cc_commit=$2
	cc_tree="$work/target-tree"
	mkdir "$cc_tree"
	git_ archive "$cc_commit" | tar -x -C "$cc_tree" || die "cannot export $cc_commit"
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

# raise TEXT: a condition that must reach the issue whatever else the plan decides.
raise() { alarms="${alarms:+$alarms
}- $1."; }

# --- plan -------------------------------------------------------------------

# Sets state (idle, blocked, ready, tagged, drafted, trust) and the fields the
# issue renders: why, next, target.
plan_inner() {
	read_published
	security_prs >"$work/security.json"
	# A Dependabot alert that was fixed after the latest release but whose pull
	# request is missing or not merged would leave "no pending merge" looking like
	# "nothing to release". An unreadable time fails the run.
	unlinked=$(jq -s --argjson since "$(epoch "$latest_published_at")" '[.[].data.repository.vulnerabilityAlerts.nodes[] |
		select(.state == "FIXED" and (((.fixedAt // "") == "") or ((.fixedAt | fromdateiso8601) > $since)) and
			(.dependabotUpdate.pullRequest == null or .dependabotUpdate.pullRequest.merged != true))] | length' "$work/alerts") ||
		die 'cannot read the fixed Dependabot alerts'
	if [ "$unlinked" -gt 0 ]; then
		raise "$unlinked Dependabot alert(s) were fixed after $latest_tag but none is linked to a merged Dependabot pull request on main, so their fix cannot be traced to a commit; check the alerts and decide by hand"
	fi
	read_pending
	if [ ! -s "$work/pending" ]; then
		state=idle
		why="no merged Dependabot security update is missing from $latest_tag"
		return 0
	fi
	if [ -z "$trust_highest" ] || semver_gt "$published_highest" "$trust_highest"; then
		state=trust
		why="v$published_highest is published but main's trust ends at ${trust_highest:+v}${trust_highest:-nothing}; trust must be synchronized before another release"
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
	# The version may already be tagged or staged: say so, never offer a second tag.
	api "$work/matching-tags.json" --paginate "repos/$GH_REPO/git/matching-refs/tags/v"
	if jq -s -e --arg ref "refs/tags/$next_tag" 'any(.[][]; .ref == $ref)' "$work/matching-tags.json" >/dev/null; then
		if jq -e --arg tag "$next_tag" 'any(.[][]; .tag_name == $tag and .draft == true)' "$work/releases.json" >/dev/null; then
			state=drafted
			why="$next_tag is tagged and its draft release is staged"
		else
			state=tagged
			why="$next_tag is tagged but has no release; its release-build run is still going or failed"
		fi
		return 0
	fi
	# Newest first-parent commit since the latest tag that holds a pending merge.
	git_ rev-list --first-parent "$latest_commit..$GITHUB_SHA" >"$work/chain"
	floor=
	while read -r commit; do
		if grep -q "^$commit " "$work/pending"; then floor=$commit; break; fi
	done <"$work/chain"
	if [ -z "$floor" ]; then
		state=blocked
		why='a pending security merge is not on main first-parent history, so no commit that follows it can be chosen'
		return 0
	fi
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
	rc=0
	main_tip=$GITHUB_SHA
	(tag_checks "$next" "$target" compat) >"$work/refusal" 2>&1 || rc=$?
	case $rc in
	0) ;;
	3)
		state=blocked
		why="the tag would be refused: $(tail -n 1 "$work/refusal" | sed 's/^security-patch-plan: //' | tr '\n' ' ')"
		return 0
		;;
	*)
		cat "$work/refusal" >&2
		die 'a lookup failed while checking the next tag; nothing was decided'
		;;
	esac
	state=ready
	why="$target is the newest green commit that holds every pending security merge"
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

render_ready() {
	if [ "$target" = "$GITHUB_SHA" ]; then
		tag_block=$(printf '%s\n' \
			'git fetch origin main &&' \
			'  git switch --detach origin/main &&' \
			"  scripts/release/security-patch-plan.sh recheck $next $target --tip &&" \
			"  scripts/release/ceremony.sh build $next")
		tag_expect="\`recheck\` prints \`ok to tag v$next\`, then the ceremony asks you to confirm \`build v$next at $target\`. If \`recheck\` refuses, nothing was tagged: read its message and wait for this issue to refresh."
	else
		tag_block=$(printf '%s\n' \
			'git fetch origin main &&' \
			"  git switch --detach $target &&" \
			"  scripts/release/security-patch-plan.sh recheck $next $target &&" \
			'  scripts/git/install-hooks.sh &&' \
			"  git tag -s v$next -m \"Hikyo $next\" $target &&" \
			"  git push origin refs/tags/v$next")
		tag_expect="\`recheck\` prints \`ok to tag v$next\`, then your signed tag \`v$next\` is pushed at $target. The main tip is not green, so the ceremony's \`build\` (which tags the tip) cannot be used; this tags the same commit with the same signature. If \`recheck\` refuses, nothing was tagged."
	fi
	cat <<EOF
$issue_marker
## Security patch v$next is ready to tag

Merged Dependabot security updates that $latest_tag does not contain:

$(pending_list)
$(overdue_note)
**Planned release:** \`v$next\` at \`$target\`. $why.

Checked by the planning run for exactly this commit: trust synchronized and authenticated; \`v$next\` is the patch successor of $latest_tag; green source proof; the commit holds every merge above; no tag or release for \`v$next\` exists; the compatibility declaration is identical to $latest_tag's. Nothing has been tagged or published.

### 1. Tag (a maintainer with the admin role and a signing key, in a clean checkout of this repository)

\`\`\`sh
$tag_block
\`\`\`

Expected: $tag_expect

### 2. Review, publish and finish (once \`release-build\` has staged the draft)

\`\`\`sh
git fetch origin main &&
  git switch --detach origin/main &&
  scripts/release/ceremony.sh review $next
scripts/release/ceremony.sh publish $next REVIEWED_MANIFEST_SHA256
scripts/release/ceremony.sh sync-trust $next
scripts/release/ceremony.sh homebrew $next
\`\`\`

Expected: \`review\` prints a manifest SHA-256; paste it in place of \`REVIEWED_MANIFEST_SHA256\`. Each later command asks for its own confirmation. Merge the trust PR and the Homebrew tap PR after review.

This issue is rewritten when the state changes. Use the commands in its latest version, and close nothing by hand: it closes itself once the security merges are released.
EOF
	printf '%s\n' "$alarms_block"
}

render() {
	alarms_block=
	[ -z "$alarms" ] || alarms_block=$(printf '\n### Also needs a person\n\n%s\n' "$alarms")
	case $state in
	idle)
		if [ -z "$alarms" ]; then action=close; printf '%s\nnothing is waiting\n' "$issue_marker"; return 0; fi
		action=open
		printf '%s\n## Dependabot security updates need a look\n\n%s\n%s\n' "$issue_marker" "$why." "$alarms_block"
		;;
	ready)
		action=open
		render_ready
		;;
	drafted)
		action=open
		cat <<EOF
$issue_marker
## Security patch $next_tag is staged: finish the ceremony

$why. Merged Dependabot security updates that $latest_tag does not contain:

$(pending_list)

\`\`\`sh
git fetch origin main &&
  git switch --detach origin/main &&
  scripts/release/ceremony.sh review $next
scripts/release/ceremony.sh publish $next REVIEWED_MANIFEST_SHA256
scripts/release/ceremony.sh sync-trust $next
scripts/release/ceremony.sh homebrew $next
\`\`\`

Expected: \`review\` prints a manifest SHA-256; paste it in place of \`REVIEWED_MANIFEST_SHA256\`.
$alarms_block
EOF
		;;
	trust)
		action=open
		cat <<EOF
$issue_marker
## Security patch blocked: synchronize trust first

$why. Merged Dependabot security updates waiting for a release:

$(pending_list)

If the public release is verified, record it:

\`\`\`sh
git fetch origin main &&
  git switch --detach origin/main &&
  scripts/release/ceremony.sh sync-trust $published_highest
\`\`\`

Expected: a trust PR opens. Merge it after review and green CI; this issue then offers the tag.
$alarms_block
EOF
		;;
	*)
		action=open
		cat <<EOF
$issue_marker
## Security patch release cannot be prepared

$why.

Merged Dependabot security updates waiting for a release:

$(pending_list)
$(overdue_note)
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
	state=idle why='' target='' alarms='' alarms_block='' action=none
	plan_inner
	render >"$work/body.md" || die 'cannot render the issue'
	jq -n --arg action "$action" --rawfile body "$work/body.md" '{action:$action, body:$body}' >"$PLAN_OUT" || die 'cannot write the plan'
	note "security-patch-plan: $state: $why"
	[ "$action" = open ] || [ "$action" = close ] || note 'security-patch-plan: nothing to report'
}

# --- issue ------------------------------------------------------------------

issue() {
	[ "$#" -eq 1 ] && [ -f "$1" ] || die 'usage: security-patch-plan.sh issue PLAN_FILE'
	action=$(jq -er '.action | select(. == "open" or . == "close" or . == "none")' "$1") || die 'the plan has no valid action'
	jq -er '.body | select(type == "string" and length > 0 and length < 60000)' "$1" >"$work/new-body" || die 'the plan has no usable body'
	gh issue list --repo "$GH_REPO" --state open --limit 1000 --json number,title,body,author >"$work/issues.json" 2>"$work/err" ||
		{ cat "$work/err" >&2; die 'cannot list open issues'; }
	# Title, our marker and the workflow's own bot as author identify the issue: anyone can
	# open an issue with the marker, and its author could later edit commands people copy.
	# A listing that fails is never "no issue".
	number=$(jq -r --arg title "$issue_title" --arg marker "$issue_marker" '[.[] | select(.title == $title and (.body | startswith($marker)) and ((.author.login // "") | test("^(app/)?github-actions(\\[bot\\])?$")))][0].number // empty' "$work/issues.json")
	case "$action" in
	open)
		new=$(jq -r '.body | gsub("\r"; "")' "$1")
		if [ -n "$number" ]; then
			# Rewrite only on a change, so the issue is not edited every hour.
			if [ "$(jq -r --argjson n "$number" '.[] | select(.number == $n) | .body | gsub("\r"; "")' "$work/issues.json")" = "$new" ]; then
				note "security-patch-plan: issue #$number is current"
			else
				gh issue edit "$number" --repo "$GH_REPO" --body-file "$work/new-body"
			fi
		else
			gh issue create --repo "$GH_REPO" --title "$issue_title" --body-file "$work/new-body"
		fi
		;;
	close)
		[ -z "$number" ] || gh issue close "$number" --repo "$GH_REPO" --comment 'The merged security updates are released; nothing is waiting.'
		;;
	none) ;;
	esac
}

# --- recheck ----------------------------------------------------------------

# The first link of the maintainer's command. It repeats, at tag time and with
# the maintainer's own login, what the issue claimed when it was written. A
# security merge that landed after the issue was written must stop the tag:
# a person tags, so there is no grace period.
recheck() {
	[ "$#" -ge 2 ] && [ "$#" -le 3 ] || die 'usage: security-patch-plan.sh recheck VERSION COMMIT [--tip]'
	version=$1 commit=$2 tip_only=${3:-}
	printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || die 'a plain stable version X.Y.Z is required'
	need_sha "$commit"
	[ -z "$tip_only" ] || [ "$tip_only" = --tip ] || die 'the only option is --tip'
	[ "$(git_ rev-parse HEAD)" = "$commit" ] || refuse "this checkout is not at $commit; run the issue's commands from their first line"
	main_tip=$(git_ rev-parse origin/main) || die 'cannot resolve origin/main; run git fetch origin main'
	GITHUB_SHA=$main_tip
	[ "$tip_only" != --tip ] || [ "$commit" = "$main_tip" ] || refuse "main moved to $main_tip since the issue was written; wait for it to refresh"
	read_published
	[ "$version" = "$next" ] || refuse "v$version is not the patch successor of $latest_tag (v$next); the issue is out of date"
	security_prs >"$work/security.json"
	alarms=
	read_pending
	[ -z "$alarms" ] || refuse "a security merge cannot be placed in main's history: $alarms"
	[ -s "$work/pending" ] || refuse "no merged Dependabot security update is missing from $latest_tag any more; nothing to release"
	contains_all "$commit" || refuse "$commit does not contain every merged security update; a newer one landed since the issue was written, so wait for it to refresh"
	[ "$trust_highest" = "$latest" ] || refuse "main's trust ends at v$trust_highest, not $latest_tag; synchronize trust first"
	tag_checks "$version" "$commit"
	printf 'security-patch-plan: ok to tag v%s at %s\n' "$version" "$commit"
}

[ "$#" -ge 1 ] || die 'usage: security-patch-plan.sh plan|issue|recheck'
command=$1
shift
case "$command" in
plan | issue | recheck) "$command" "$@" ;;
*) die "unknown command $command" ;;
esac

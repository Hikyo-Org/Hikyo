#!/bin/sh
# One level-triggered step of the unattended stable patch release. Every
# subcommand re-derives the state it needs, so a lost trigger, a retry or an
# overlapping run only repeats work that is already idempotent.
#
#   plan        read-only: pick the single next step and the side lanes
#   tag         create the app's annotated tag for the planned commit
#   publish     publish the staged draft after the exact-run digest check
#   sync-trust  open the app-signed trust PR for a published release
#   homebrew    update the stable Homebrew cask and merge its PR
#   automerge   arm auto-merge on proven Dependabot security-update PRs
#   issue       open, update or close the single stall issue
#
# Environment: GH_REPO, GH_TOKEN (the job's own scoped token). Release tools run
# from this checkout, which is main's tip. The scripts never read a private key.
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
printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || die 'invalid repository'
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
patch_successor() { printf '%s\n' "$1" | awk -F. '{printf "%d.%d.%d\n", $1, $2, $3 + 1}'; }
# Seeds the rollback floor from main's authenticated trust, never from a local file.
seed_state() {
	state="$work/release-trust-state.json"
	go_stable verify --trust "$trust" --trust-only --state "$state" >/dev/null
}
security_prs() {
	# shellcheck disable=SC2016
	query='query($owner:String!,$name:String!,$endCursor:String){repository(owner:$owner,name:$name){vulnerabilityAlerts(first:100,after:$endCursor){pageInfo{hasNextPage endCursor}nodes{dependabotUpdate{pullRequest{number merged mergedAt baseRefName headRefName author{login} mergeCommit{oid}}}}}}}'
	gh api graphql --paginate -f query="$query" -f owner="${GH_REPO%/*}" -f name="${GH_REPO#*/}" >"$work/alerts" ||
		die 'cannot read Dependabot alerts'
	# An alert that Dependabot linked to a pull request proves a security update.
	jq -s -c '[.[].data.repository.vulnerabilityAlerts.nodes[].dependabotUpdate.pullRequest | select(. != null) |
		select(.author.login == "dependabot" and .baseRefName == "main" and (.headRefName | startswith("dependabot/")))] | unique_by(.number)' "$work/alerts"
}

plan() {
	: "${GITHUB_SHA:?}"
	need_sha "$GITHUB_SHA"
	decision() { # action reason [version commit run artifact]
		emit action "$1"
		emit reason "$2"
		emit version "${3:-}"
		emit commit "${4:-}"
		emit run_id "${5:-}"
		emit artifact "${6:-}"
		note "stable-reconcile: next step is $1: $2"
	}
	want_issue() { emit issue "$1"; emit issue_text "${2:-}"; }
	emit issue none
	emit automerge ''
	emit homebrew ''
	latest_tag=$(gh api "repos/$GH_REPO/releases/latest" | jq -er '.tag_name') || die 'cannot read the latest release'
	printf '%s\n' "$latest_tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || die "latest release $latest_tag is not a plain stable tag"
	latest=${latest_tag#v}
	highest=$(jq -r '.highest_release // empty' "$trust/metadata.json")

	# Side lane: Dependabot security-update PRs into main. The proof is an alert
	# that Dependabot itself linked to the pull request, not a label or a title.
	security_prs >"$work/security.json"
	open=$(gh api --paginate "repos/$GH_REPO/pulls?state=open&base=main&per_page=100" |
		jq -s -r --slurpfile security "$work/security.json" --argjson bot "$dependabot_user_id" --arg repo "$GH_REPO" '
		[.[][] | select(.user.id == $bot and .head.repo.full_name == $repo and (.head.ref | startswith("dependabot/")) and .auto_merge == null and
			(.number as $n | $security[0] | any(.number == $n)))] | map("\(.number):\(.head.sha)") | join(" ")')
	emit automerge "$open"

	if [ -z "$highest" ]; then
		decision none 'trust names no signed latest stable release, so no patch successor exists'
		return 0
	fi
	if [ "$latest" != "$highest" ]; then
		if [ "$(printf '%s\n%s\n' "$highest" "$latest" | sort -V | tail -n 1)" = "$latest" ]; then
			decision sync-trust "v$latest is published but main's trust still ends at v$highest" "$latest"
		else
			decision none "the latest release v$latest is older than trusted v$highest"
			want_issue open "The latest GitHub release v$latest is older than the signed latest v$highest. A maintainer must reconcile them."
		fi
		return 0
	fi

	# Side lane: Homebrew follows every latest stable release once its trust is synced.
	tap=${HIKYO_HOMEBREW_TAP_REPOSITORY:-${GH_REPO%/*}/homebrew-tap}
	cask=$(gh api "repos/$tap/contents/Casks/hikyo.rb?ref=main" 2>/dev/null | jq -r '.content // empty' | base64 -d 2>/dev/null |
		sed -n 's/^[[:space:]]*version[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1 || true)
	[ "$cask" = "$latest" ] || emit homebrew "$latest"

	latest_commit=$(gh api "repos/$GH_REPO/commits/$latest_tag" | jq -er '.sha') || die "cannot resolve $latest_tag"
	need_sha "$latest_commit"
	next=$(patch_successor "$latest")
	next_tag="v$next"
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

	if ref_exists "tags/$next_tag"; then
		runs=$(gh api "repos/$GH_REPO/actions/workflows/release.yml/runs?event=push&per_page=100")
		run=$(printf '%s\n' "$runs" | jq -c --arg tag "$next_tag" '[.workflow_runs[] | select(.head_branch == $tag)] | sort_by(.run_number, .run_attempt) | last // empty')
		if [ -z "$run" ]; then
			decision wait "$next_tag exists and its release-build run has not started" "$next"
		elif [ "$(printf '%s\n' "$run" | jq -r '.status')" != completed ]; then
			decision wait "the release-build run of $next_tag is $(printf '%s\n' "$run" | jq -r '.status')" "$next"
		elif [ "$(printf '%s\n' "$run" | jq -r '.conclusion')" = success ]; then
			run_id=$(printf '%s\n' "$run" | jq -er '.id')
			artifact=$(gh api "repos/$GH_REPO/actions/runs/$run_id/artifacts?per_page=100" | jq -r --arg id "$run_id" \
				'[.artifacts[] | select((.name | startswith("release-manifest-sha256-" + $id + "-")) and .expired == false)] | sort_by(.id) | last | .name // empty')
			[ -n "$artifact" ] || { decision wait "the manifest digest artifact of run $run_id is not available yet" "$next" "" "$run_id"; return 0; }
			decision publish "the release-build run $run_id of $next_tag succeeded" "$next" "" "$run_id" "$artifact"
		else
			decision none "the release-build run of $next_tag ended $(printf '%s\n' "$run" | jq -r '.conclusion'); its tag is immutable"
			want_issue open "The release-build run of $next_tag did not succeed. Tags are immutable, so the unattended release cannot retry. A maintainer must release the next patch by hand or grant the app actions: write."
		fi
		return 0
	fi

	if [ ! -s "$work/pending" ]; then
		decision none "no merged Dependabot security update is missing from $latest_tag"
		want_issue close
		return 0
	fi
	# Newest first-parent commit since the latest tag that holds a pending merge.
	: >"$work/candidates"
	"$GIT_BIN" -C "$repo_root" rev-list --first-parent "$latest_commit..$GITHUB_SHA" >"$work/chain"
	floor=
	while read -r commit; do
		if grep -q "^$commit " "$work/pending"; then floor=$commit; break; fi
	done <"$work/chain"
	[ -n "$floor" ] || { decision none 'a pending security merge is not on main first-parent history'; want_issue open 'A merged Dependabot security update is not on main first-parent history, so no release can contain it.'; return 0; }
	target=
	checked=0
	while read -r commit; do
		checked=$((checked + 1))
		[ "$checked" -le 60 ] || break
		if "$GREEN_BIN" "$GH_REPO" "$commit" >/dev/null 2>&1; then target=$commit; break; fi
		[ "$commit" != "$floor" ] || break
	done <"$work/chain"
	if [ -z "$target" ]; then
		oldest=$(sort -k2 "$work/pending" | head -n 1 | cut -d' ' -f2)
		decision none 'no main commit at or after the newest security merge has complete green source proof'
		age=$(($(date +%s) - $(date -u -d "$oldest" +%s 2>/dev/null || date -u -j -f %Y-%m-%dT%H:%M:%SZ "$oldest" +%s)))
		if [ "$age" -gt 86400 ]; then want_issue open "A merged Dependabot security update has waited more than 24 hours, and no main commit at or after it has complete green source proof. Run the full ci-main workflow on the main tip."; fi
		return 0
	fi
	if "$GH_BIN" api "repos/$GH_REPO/actions/workflows/docs.yml/runs?branch=main&per_page=10" |
		jq -e '[.workflow_runs[] | select(.status != "completed")] | length > 0' >/dev/null; then
		decision wait 'a docs deployment is still running, and a release build refuses until the live docs are current' "$next"
		return 0
	fi
	if ! "$DOCS_LIVE_BIN" https://hikyo.app security@hikyo.app >/dev/null 2>&1; then
		decision wait 'hikyo.app does not pass the live docs gate yet, which would fail the release build' "$next"
		return 0
	fi
	if ! { "$GIT_BIN" -C "$repo_root" show "$target:release/repository/fallback-channel-test.json" >"$work/fallback.json" 2>/dev/null &&
		"$FALLBACK_BIN" "$work/fallback.json" security@hikyo.app >/dev/null 2>&1; }; then
		decision wait 'the fallback channel test record is stale at the target commit, which would fail the release build' "$next"
		return 0
	fi
	decision tag "merged security updates are missing from $latest_tag; $target is the newest green commit at or after them" "$next" "$target"
	want_issue close
}

tag() {
	: "${VERSION:?}" "${COMMIT:?}" "${GITHUB_SHA:?}"
	need_version "$VERSION"
	need_sha "$COMMIT"
	[ "${DRY_RUN:-false}" != true ] || { note "dry run: would tag v$VERSION at $COMMIT"; return 0; }
	"$SYNCED_BIN" "$GH_REPO" "$trust/metadata.json"
	go_stable preflight --trust "$trust" >/dev/null
	go_stable candidate --patch-only --trust "$trust" --version "$VERSION" --commit "$COMMIT" --out "$work/candidate.json"
	"$GIT_BIN" -C "$repo_root" merge-base --is-ancestor "$COMMIT" "$GITHUB_SHA" || die 'the commit is not on main history'
	"$GREEN_BIN" "$GH_REPO" "$COMMIT"
	gh api --paginate "repos/$GH_REPO/git/matching-refs/tags/v" | jq -s -e --arg tag "v$VERSION" '
		def triple: ltrimstr("v") | capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)") | [.a, .b, .c | tonumber];
		([.[][] | .ref | ltrimstr("refs/tags/") | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+") and (contains("-nightly.") | not)) | triple | select(. >= ($tag | triple))] | length) == 0
	' >/dev/null || die "a stable tag at or above v$VERSION already exists"
	if gh api "repos/$GH_REPO/releases/tags/v$VERSION" >/dev/null 2>"$work/err"; then die "v$VERSION already has a release"; fi
	grep -F '(HTTP 404)' "$work/err" >/dev/null || { cat "$work/err" >&2; die 'cannot prove the version is unused'; }
	# An annotated tag object, so the build can read its tagger and the run is the app's.
	object=$(gh api --method POST "repos/$GH_REPO/git/tags" -f tag="v$VERSION" -f message="Hikyo $VERSION" \
		-f object="$COMMIT" -f type=commit | jq -er '.sha')
	need_sha "$object"
	gh api --method POST "repos/$GH_REPO/git/refs" -f ref="refs/tags/v$VERSION" -f sha="$object" >/dev/null
	note "stable-reconcile: tagged v$VERSION at $COMMIT; release-build starts as the app"
}

publish() {
	: "${VERSION:?}" "${RUN_ID:?}" "${DIGEST_FILE:?}"
	need_version "$VERSION"
	printf '%s\n' "$RUN_ID" | grep -Eq '^[1-9][0-9]*$' || die 'invalid release-build run id'
	[ "${DRY_RUN:-false}" != true ] || { note "dry run: would publish v$VERSION from run $RUN_ID"; return 0; }
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
	is_draft=$(gh release view "v$version" --repo "$GH_REPO" --json isDraft | jq -r '.isDraft')
	[ "$is_draft" = false ] || die "v$version is not public"
	seed_state
	download="$work/public"
	mkdir "$download"
	gh release download "v$version" --repo "$GH_REPO" --dir "$download"
	commit=$(jq -er '.source_commit' "$download/release-manifest.json")
	go_stable verify --trust "$trust" --directory "$download" --version "$version" --commit "$commit" \
		--state "$state" --latest --published
}

sync_trust() {
	: "${VERSION:?}" "${APP_SLUG:?}" "${STABLE_RELEASE_APP_BOT_ID:?}"
	need_version "$VERSION"
	printf '%s\n' "$STABLE_RELEASE_APP_BOT_ID" | grep -Eq '^[1-9][0-9]*$' || die 'invalid bot user id'
	printf '%s\n' "$APP_SLUG" | grep -Eq '^[A-Za-z0-9-]+$' || die 'invalid app slug'
	[ "${DRY_RUN:-false}" != true ] || { note "dry run: would sync trust for v$VERSION"; return 0; }
	verify_public "$VERSION"
	files='metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json'
	same=true
	for name in $files; do cmp -s "$download/$name" "$trust/$name" || same=false; done
	if [ "$same" = true ]; then note "trust already records v$VERSION"; return 0; fi
	branch="release/sync-trust-$VERSION"
	main_sha=$(gh api "repos/$GH_REPO/git/ref/heads/main" | jq -er '.object.sha')
	need_sha "$main_sha"
	head=
	if ref_exists "heads/$branch"; then
		head=$(gh api "repos/$GH_REPO/git/ref/heads/$branch" | jq -er '.object.sha')
		# Only the four trust files may differ from main on an existing branch.
		gh api "repos/$GH_REPO/compare/$main_sha...$head" | jq -e --arg names "$files" '
			($names | split(" ") | map("release/trust/" + .)) as $allowed |
			(.files | type == "array") and all(.files[]; .filename as $f | $allowed | index($f) != null)
		' >/dev/null || die "$branch contains unrelated changes; nothing was overwritten"
	else
		gh api --method POST "repos/$GH_REPO/git/refs" -f ref="refs/heads/$branch" -f sha="$main_sha" >/dev/null
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
	committed=true
	if [ "$head" != "$main_sha" ]; then
		# A retry finds the commit already on the branch: reuse it, never add an empty one.
		committed=false
		matches=true
		for name in $files; do
			have=$(gh api "repos/$GH_REPO/contents/release/trust/$name?ref=$branch" | jq -r '.content // empty' | tr -d '\n')
			[ "$have" = "$(base64 <"$download/$name" | tr -d '\n')" ] || matches=false
		done
		[ "$matches" = false ] || committed=true
	fi
	if [ "$committed" = true ] && [ "$head" != "$main_sha" ]; then
		commit=$head
	else
		commit=$(gh api graphql --input "$work/commit.json" | jq -er '.data.createCommitOnBranch.commit.oid')
	fi
	need_sha "$commit"
	# GitHub's own verdict decides, not a local signature check.
	gh api "repos/$GH_REPO/commits/$commit" | jq -e --arg name "$bot_login" --arg email "$bot_email" '
		.commit.verification.verified == true and .commit.author.name == $name and .commit.author.email == $email and
		(.commit.message | endswith("Signed-off-by: " + $name + " <" + $email + ">"))
	' >/dev/null || die "commit $commit is not GitHub-verified with a matching DCO sign-off"
	prs=$(gh api "repos/$GH_REPO/pulls?state=open&base=main&head=${GH_REPO%/*}:$branch")
	number=$(printf '%s\n' "$prs" | jq -r '.[0].number // empty')
	if [ -z "$number" ]; then
		number=$(gh api --method POST "repos/$GH_REPO/pulls" -f title="chore(release): record published $VERSION trust" -f head="$branch" -f base=main \
			-f body='Public release trust evidence, verified against the published release by stable-release-reconciler.yml. The merge queue runs the full gate.' | jq -er '.number')
	fi
	printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]*$' || die 'invalid pull request number'
	if [ "$(printf '%s\n' "$prs" | jq -r '.[0].auto_merge != null')" != true ]; then
		gh pr merge "$number" --repo "$GH_REPO" --auto --merge --match-head-commit "$commit"
	fi
	note "stable-reconcile: trust PR #$number carries $commit and is queued"
}

homebrew() {
	: "${VERSION:?}"
	need_version "$VERSION"
	tap=${HIKYO_HOMEBREW_TAP_REPOSITORY:-${GH_REPO%/*}/homebrew-tap}
	[ "${DRY_RUN:-false}" != true ] || { note "dry run: would update the cask for v$VERSION in $tap"; return 0; }
	verify_public "$VERSION"
	"$HOMEBREW_BIN" "$GH_REPO" "v$VERSION" "$download" "$tap" >"$work/homebrew.out" || { cat "$work/homebrew.out"; die 'cannot update the tap'; }
	cat "$work/homebrew.out"
	url=$(sed -n 's/^homebrew publish: review and merge \(https:\/\/github.com\/.*\/pull\/[0-9][0-9]*\)$/\1/p' "$work/homebrew.out")
	[ -n "$url" ] || return 0
	number=${url##*/}
	sha=$(gh api "repos/$tap/pulls/$number" | jq -er '.head.sha')
	need_sha "$sha"
	# The tap disables auto-merge, so merge once its required check is green; a
	# later run retries while the check is still pending.
	if gh api "repos/$tap/commits/$sha/check-runs?check_name=ci-required&per_page=100" |
		jq -e '[.check_runs[] | select(.conclusion == "success")] | length > 0' >/dev/null; then
		gh api --method PUT "repos/$tap/pulls/$number/merge" -f merge_method=squash -f sha="$sha" >/dev/null
		note "stable-reconcile: merged tap PR #$number"
	else
		note "stable-reconcile: tap PR #$number waits for ci-required"
	fi
}

automerge() {
	: "${PRS:=}"
	[ "${DRY_RUN:-false}" != true ] || { note "dry run: would arm auto-merge for: $PRS"; return 0; }
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

issue() {
	[ "${DRY_RUN:-false}" != true ] || { note "dry run: issue $1"; return 0; }
	number=$(gh issue list --repo "$GH_REPO" --state open --search "\"$issue_title\" in:title" --json number,title |
		jq -r --arg title "$issue_title" '[.[] | select(.title == $title)][0].number // empty')
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

[ "$#" -ge 1 ] || die 'usage: stable-reconcile.sh plan|tag|publish|sync-trust|homebrew|automerge|issue'
command=$1
shift
case "$command" in
plan | tag | publish | homebrew | automerge | issue) "$command" "$@" ;;
sync-trust) sync_trust "$@" ;;
*) die "unknown command $command" ;;
esac

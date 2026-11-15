#!/bin/sh
# Fixtures for the unattended stable patch release: the planner's single next
# step, the tag, trust-PR, Homebrew and Dependabot lanes, and their refusals.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stable-reconcile.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
repo="$scratch/repo"
mkdir -p "$scratch/bin" "$repo/release/trust" "$repo/release/repository"
git -C "$repo" init -q -b main
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
git -C "$repo" config commit.gpgsign false
printf '{"highest_release":"1.1.0"}\n' >"$repo/release/trust/metadata.json"
printf '{}\n' >"$repo/release/repository/fallback-channel-test.json"
git -C "$repo" add -A
for name in c0 c1 c2 c3; do
	git -C "$repo" commit -q --allow-empty -m "$name" --no-gpg-sign
done
c0=$(git -C "$repo" rev-parse HEAD~3) c1=$(git -C "$repo" rev-parse HEAD~2) c2=$(git -C "$repo" rev-parse HEAD~1) c3=$(git -C "$repo" rev-parse HEAD)
tip=$c3
export FIXTURE="$scratch" C0="$c0" C1="$c1" C2="$c2" C3="$c3" TIP="$tip"

cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE/calls"
mode=${FIXTURE_MODE:-pending}
not_found() { printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; }
case "$*" in
	'api repos/o/r/releases/latest') printf '{"tag_name":"%s"}\n' "${FIXTURE_LATEST:-v1.1.0}" ;;
	'api graphql --paginate '*)
		merged=true
		[ "$mode" != unmerged ] || merged=false
		jq -nc --arg oid "$C1" --argjson merged "$merged" --arg at "${FIXTURE_MERGED_AT:-2026-11-14T00:00:00Z}" '
			{data:{repository:{vulnerabilityAlerts:{nodes:[
				{dependabotUpdate:{pullRequest:{number:7,merged:$merged,mergedAt:$at,baseRefName:"main",headRefName:"dependabot/npm_and_yarn/x",author:{login:"dependabot"},mergeCommit:{oid:$oid}}}},
				{dependabotUpdate:{pullRequest:{number:7,merged:$merged,mergedAt:$at,baseRefName:"main",headRefName:"dependabot/npm_and_yarn/x",author:{login:"dependabot"},mergeCommit:{oid:$oid}}}},
				{dependabotUpdate:null},
				{dependabotUpdate:{pullRequest:{number:8,merged:false,mergedAt:null,baseRefName:"main",headRefName:"dependabot/go/y",author:{login:"dependabot"},mergeCommit:null}}},
				{dependabotUpdate:{pullRequest:{number:9,merged:true,mergedAt:"2026-01-01T00:00:00Z",baseRefName:"main",headRefName:"feature/z",author:{login:"someone"},mergeCommit:{oid:"9999999999999999999999999999999999999999"}}}}
			]}}}}' ;;
	'api --paginate repos/o/r/pulls?state=open&base=main&per_page=100')
		jq -nc --arg head "$(printf 'b%.0s' $(seq 40))" '[
			{number:8,user:{id:49699333},head:{ref:"dependabot/go/y",sha:$head,repo:{full_name:"o/r"}},auto_merge:null},
			 {number:8,user:{id:49699333},head:{ref:"dependabot/go/y",sha:$head,repo:{full_name:"fork/r"}},auto_merge:null},
			 {number:10,user:{id:49699333},head:{ref:"dependabot/go/version",sha:$head,repo:{full_name:"o/r"}},auto_merge:null},
			 {number:11,user:{id:1},head:{ref:"dependabot/go/y",sha:$head,repo:{full_name:"o/r"}},auto_merge:null}]' ;;
	'api repos/o/homebrew-tap/contents/Casks/hikyo.rb?ref=main')
		jq -nc --arg c "$(printf 'cask "hikyo" do\n  version "%s"\nend\n' "${FIXTURE_CASK:-1.1.0}" | base64)" '{content:$c}' ;;
	'api repos/o/r/commits/v1.1.0') jq -nc --arg sha "$C0" '{sha:$sha}' ;;
	'api repos/o/r/git/ref/tags/v1.1.1') [ "$mode" = tag-exists ] || [ "$mode" = build-ok ] || [ "$mode" = build-failed ] || [ "$mode" = build-running ] || not_found; echo '{"object":{"sha":"x"}}' ;;
	'api repos/o/r/actions/workflows/release.yml/runs?event=push&per_page=100')
		status=completed conclusion=success
		[ "$mode" != build-running ] || { status=in_progress; conclusion=null; }
		[ "$mode" != build-failed ] || conclusion=failure
		[ "$mode" != tag-exists ] || { printf '{"workflow_runs":[]}\n'; exit 0; }
		jq -nc --arg s "$status" --arg c "$conclusion" '{workflow_runs:[{id:77,head_branch:"v1.1.1",run_number:5,run_attempt:1,status:$s,conclusion:(if $c == "null" then null else $c end)}]}' ;;
	'api repos/o/r/actions/runs/77/artifacts?per_page=100')
		echo '{"artifacts":[{"id":1,"name":"release-manifest-sha256-77-1","expired":false},{"id":2,"name":"release-manifest-sha256-77-2","expired":false},{"id":3,"name":"other","expired":false}]}' ;;
	'api repos/o/r/actions/workflows/docs.yml/runs?branch=main&per_page=10')
		if [ "$mode" = docs-running ]; then echo '{"workflow_runs":[{"status":"in_progress"}]}'; else echo '{"workflow_runs":[{"status":"completed"}]}'; fi ;;
	'api --paginate repos/o/r/git/matching-refs/tags/v')
		printf '[{"ref":"refs/tags/v1.1.0"},{"ref":"refs/tags/v-ruleset-probe"},{"ref":"refs/tags/v1.2.0-nightly.20261112.1.gabcdef12"}]\n'
		[ "$mode" != higher-tag ] || printf '[{"ref":"refs/tags/v1.1.1"}]\n' ;;
	'api repos/o/r/releases/tags/v1.1.1') not_found ;;
	'api --method POST repos/o/r/git/tags '*) echo '{"sha":"dddddddddddddddddddddddddddddddddddddddd"}' ;;
	'api --method POST repos/o/r/git/refs '*) echo '{}' ;;
	'release view v1.1.1 --repo o/r --json isDraft') echo '{"isDraft":false}' ;;
	'release download v1.1.1 --repo o/r --dir '*)
		for last; do :; done
		jq -nc --arg c "$C3" '{source_commit:$c}' >"$last/release-manifest.json"
		for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json; do printf 'new %s\n' "$name" >"$last/$name"; done ;;
	'api repos/o/r/git/ref/heads/main') jq -nc --arg sha "$TIP" '{object:{sha:$sha}}' ;;
	'api repos/o/r/git/ref/heads/release/sync-trust-1.1.1')
		[ "$mode" = branch-exists ] || not_found
		jq -nc --arg sha "$C3" '{object:{sha:$sha}}' ;;
	'api repos/o/r/compare/'*) echo '{"files":[{"filename":"release/trust/metadata.json"}]}' ;;
	'api repos/o/r/contents/release/trust/'*) printf '{"content":""}\n' ;;
	'api --method POST repos/o/r/git/refs -f ref=refs/heads/release/sync-trust-1.1.1 '*) echo '{}' ;;
	'api graphql --input '*) jq -e '.variables.input.fileChanges.additions | length == 4' "$4" >/dev/null; jq -nc --arg oid "$(printf 'e%.0s' $(seq 40))" '{data:{createCommitOnBranch:{commit:{oid:$oid}}}}' ;;
	'api repos/o/r/commits/'eeee*)
		verified=true
		[ "$mode" != unverified ] || verified=false
		jq -nc --argjson verified "$verified" '{commit:{verification:{verified:$verified},author:{name:"app[bot]",email:"900+app[bot]@users.noreply.github.com"},message:"chore\n\nSigned-off-by: app[bot] <900+app[bot]@users.noreply.github.com>"}}' ;;
	'api repos/o/r/pulls?state=open&base=main&head=o:release/sync-trust-1.1.1') echo '[]' ;;
	'api --method POST repos/o/r/pulls '*) echo '{"number":42}' ;;
	'pr merge '*) ;;
	'api repos/o/homebrew-tap/pulls/5') jq -nc --arg sha "$(printf 'f%.0s' $(seq 40))" '{head:{sha:$sha}}' ;;
	'api repos/o/homebrew-tap/commits/'ffff*)
		conclusion=success
		[ "$mode" != tap-pending ] || conclusion=null
		jq -nc --arg c "$conclusion" '{check_runs:[{conclusion:(if $c == "null" then null else $c end)}]}' ;;
	'api --method PUT repos/o/homebrew-tap/pulls/5/merge '*) echo '{}' ;;
	'issue list '*) if [ "$mode" = issue-open ]; then echo '[{"number":3,"title":"Unattended stable patch release is stalled"}]'; else echo '[]'; fi ;;
	'issue '*) ;;
	*) printf 'unexpected gh invocation: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$scratch/bin/go" <<'SH'
#!/bin/sh
set -eu
printf 'go %s\n' "$*" >>"$FIXTURE/calls"
case "$*" in
	*'stable candidate --patch-only '*) [ "${FIXTURE_MODE:-}" != not-patch ] || exit 1 ;;
esac
SH
cat >"$scratch/bin/green" <<'SH'
#!/bin/sh
case " $GREEN " in *" $2 "*) exit 0 ;; esac
exit 1
SH
cat >"$scratch/bin/ok" <<'SH'
#!/bin/sh
[ "${FIXTURE_MODE:-}" != "${OK_FAIL:-none}" ]
SH
cat >"$scratch/bin/synced" <<'SH'
#!/bin/sh
exit 0
SH
cat >"$scratch/bin/homebrew" <<'SH'
#!/bin/sh
echo 'homebrew publish: review and merge https://github.com/o/homebrew-tap/pull/5'
SH
cat >"$scratch/bin/publish" <<'SH'
#!/bin/sh
printf 'publish %s run=%s state=%s\n' "$*" "$HIKYO_RELEASE_RUN_ID" "$HIKYO_RELEASE_TRUST_STATE" >>"$FIXTURE/calls"
SH
chmod +x "$scratch"/bin/*
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go" GREEN_BIN="$scratch/bin/green" SYNCED_BIN="$scratch/bin/synced" \
	DOCS_LIVE_BIN="$scratch/bin/ok" FALLBACK_BIN="$scratch/bin/ok" HOMEBREW_BIN="$scratch/bin/homebrew" PUBLISH_BIN="$scratch/bin/publish" \
	GH_REPO=o/r HIKYO_REPO_ROOT="$repo" GITHUB_SHA="$tip"

fail() { printf 'stable-reconcile fixture: %s\n' "$*" >&2; exit 1; }
run() { # mode command...
	FIXTURE_MODE=$1
	shift
	export FIXTURE_MODE
	: >"$scratch/out"
	: >"$scratch/calls"
	GITHUB_OUTPUT="$scratch/out" sh ${TRACE:+-x} "$script_dir/stable-reconcile.sh" "$@" >"$scratch/log" 2>&1 || { cat "$scratch/log" >&2; return 1; }
}
out() { sed -n "s/^$1=//p" "$scratch/out" | tail -n 1; }
expect() { [ "$(out "$1")" = "$2" ] || fail "$3: $1 was '$(out "$1")', want '$2'"; }

# The newest green commit at or after the newest security merge is released.
export GREEN="$c3 $c2 $c1 $c0"
run pending plan
expect action tag "green tip"; expect commit "$tip" "green tip"; expect version 1.1.1 "green tip"
# Dependabot lane: only the proven, unarmed, same-repo security PR is queued.
expect automerge "8:$(printf 'b%.0s' $(seq 40))" "security lane"
# A red tip walks back to the newest green commit; the security merge is the floor.
(GREEN="$c2 $c1" run pending plan); expect action tag "red tip"; expect commit "$c2" "red tip"
(GREEN="$c1" run pending plan); expect action tag "walk back"; expect commit "$c1" "walk back"
# Nothing at or after the floor is green: release nothing, no error, no issue before 24h.
(GREEN="$c0" FIXTURE_MERGED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) run pending plan)
expect action none "no green commit"; expect issue none "recent stall"
(GREEN="$c0" run pending plan); expect action none "stale stall"; expect issue open "stale stall"
(GREEN="$c3" run unmerged plan); expect action none "unmerged"
# Docs or fallback gates would fail the build, and an immutable tag cannot be retried.
(GREEN="$c3" run docs-running plan); expect action wait "docs running"
(OK_FAIL=docs-running run docs-running plan); expect action wait "docs running"
(GREEN="$c3" DOCS_LIVE_BIN="$scratch/bin/false" run pending plan); expect action wait "docs not live"
# Trust behind the published release: sync it first, regardless of other state.
(FIXTURE_LATEST=v1.1.1 run pending plan); expect action sync-trust "unsynced"; expect version 1.1.1 "unsynced"
# Existing tag: wait for the build, publish after success, stop after failure.
(GREEN="$c3" run build-running plan); expect action wait "building"
(GREEN="$c3" run build-ok plan); expect action publish "built"; expect run_id 77 "built"; expect artifact release-manifest-sha256-77-2 "built"
(GREEN="$c3" run build-failed plan); expect action none "failed build"; expect issue open "failed build"
# Homebrew follows the latest release; Dependabot lane is independent.
(FIXTURE_CASK=1.0.0 run pending plan); expect homebrew 1.1.0 "stale cask"
run pending plan; expect homebrew '' "current cask"

# tag: creates the annotated tag object, then the ref, for exactly the planned commit.
(VERSION=1.1.1 COMMIT=$c3 run pending tag)
grep -q 'api --method POST repos/o/r/git/tags -f tag=v1.1.1 -f message=Hikyo 1.1.1 -f object='"$c3"' -f type=commit' "$scratch/calls" || fail 'tag object not created'
grep -q 'api --method POST repos/o/r/git/refs -f ref=refs/tags/v1.1.1 -f sha=dddd' "$scratch/calls" || fail 'tag ref not created'
grep -q 'stable candidate --patch-only' "$scratch/calls" || fail 'tag skipped the patch-only rule'
for mode in higher-tag not-patch; do
	if (VERSION=1.1.1 COMMIT=$c3 run $mode tag); then fail "tagged despite $mode"; fi
	grep -q 'git/refs -f ref=refs/tags' "$scratch/calls" && fail "created a ref despite $mode"
done
if (GREEN='' VERSION=1.1.1 COMMIT=$c3 GREEN_BIN="$scratch/bin/green" run pending tag); then fail 'tagged a commit without green proof'; fi
if (VERSION=1.1.1 COMMIT=$c3 DRY_RUN=true run pending tag) && grep -q 'POST' "$scratch/calls"; then fail 'dry run wrote'; fi
if (VERSION=1.2 COMMIT=$c3 run pending tag); then fail 'accepted a non-patch version string'; fi

# publish: the digest comes from the retained artifact and the run id reaches the tag authority.
printf '%064d\n' 0 >"$scratch/digest"
(VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" run pending publish)
grep -q "publish o/r v1.1.1 $(printf '%064d' 0) $repo/release/trust run=77 state=" "$scratch/calls" || fail 'publish helper not called with the artifact digest'
grep -q 'stable verify .*--trust-only --state' "$scratch/calls" || fail 'rollback state was not seeded from main trust'
printf 'not-a-digest\n' >"$scratch/digest"
if (VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" run pending publish); then fail 'published without a SHA-256 digest'; fi

# sync-trust: one app commit with the four files, verified by GitHub, auto-merge armed.
(VERSION=1.1.1 APP_SLUG=app STABLE_RELEASE_APP_BOT_ID=900 run pending sync-trust)
grep -q 'graphql --input' "$scratch/calls" || fail 'no createCommitOnBranch'
grep -q 'pr merge 42 --repo o/r --auto --merge --match-head-commit eeee' "$scratch/calls" || fail 'auto-merge not armed'
if (VERSION=1.1.1 APP_SLUG=app STABLE_RELEASE_APP_BOT_ID=900 run unverified sync-trust); then fail 'accepted an unverified commit'; fi
grep -q 'pr merge' "$scratch/calls" && fail 'armed an unverified commit'
if (VERSION=1.1.1 APP_SLUG=app STABLE_RELEASE_APP_BOT_ID=0 run pending sync-trust); then fail 'accepted a bad bot id'; fi

# homebrew: merge once the tap's required check is green, otherwise wait for the next run.
(VERSION=1.1.1 run pending homebrew)
grep -q 'merge_method=squash -f sha=ffff' "$scratch/calls" || fail 'tap PR not squash-merged'
(VERSION=1.1.1 run tap-pending homebrew)
grep -q 'merge_method=squash' "$scratch/calls" && fail 'merged a tap PR with a pending check'

# automerge: only well-formed entries, always pinned to the verified head.
(PRS="8:$(printf 'b%.0s' $(seq 40))" run pending automerge)
grep -q "pr merge 8 --repo o/r --auto --merge --match-head-commit $(printf 'b%.0s' $(seq 40))" "$scratch/calls" || fail 'auto-merge not armed'
if (PRS='8:short' run pending automerge); then fail 'accepted a malformed entry'; fi
# issue: one issue, edited in place, closed when the stall clears.
(ISSUE_TEXT=stalled run issue-open issue open); grep -q 'issue edit 3' "$scratch/calls" || fail 'issue not updated in place'
(ISSUE_TEXT=stalled run pending issue open); grep -q 'issue create' "$scratch/calls" || fail 'issue not created'
run issue-open issue close; grep -q 'issue close 3' "$scratch/calls" || fail 'issue not closed'
printf 'stable-reconcile fixture: planner states, tag, publish, trust PR, Homebrew, Dependabot and issue lanes passed\n'

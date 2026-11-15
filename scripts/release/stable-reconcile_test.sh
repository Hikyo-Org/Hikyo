#!/bin/sh
# Fixtures for the unattended stable patch release: the planner's single next
# step, the tag, publish, trust-PR, Homebrew, cleanup and Dependabot lanes, the
# stall alerts, and every refusal. Each lookup that decides something also has a
# failing-API case, because a failed read must never look like an empty answer.
# shellcheck disable=SC2030,SC2031,SC2086,SC2155
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
# c0 is the latest stable release; trust records it only from c2 on, so a build
# at c0 or c1 would read stale trust and burn the tag.
printf '{"highest_release":"1.0.0"}\n' >"$repo/release/trust/metadata.json"
for name in metadata.sigstore.json catalog.json catalog.sigstore.json; do printf 'old %s\n' "$name" >"$repo/release/trust/$name"; done
printf '{}\n' >"$repo/release/repository/fallback-channel-test.json"
git -C "$repo" add -A
git -C "$repo" commit -q -m c0 --no-gpg-sign
git -C "$repo" commit -q --allow-empty -m c1 --no-gpg-sign
printf '{"highest_release":"1.1.0"}\n' >"$repo/release/trust/metadata.json"
git -C "$repo" commit -q -a -m c2 --no-gpg-sign
git -C "$repo" commit -q --allow-empty -m c3 --no-gpg-sign
c0=$(git -C "$repo" rev-parse HEAD~3) c1=$(git -C "$repo" rev-parse HEAD~2) c2=$(git -C "$repo" rev-parse HEAD~1) c3=$(git -C "$repo" rev-parse HEAD)
tip=$c3
hex() { printf "$1%.0s" $(seq 40); }
export FIXTURE="$scratch" C0="$c0" C1="$c1" C2="$c2" C3="$c3" TIP="$tip" REPO="$repo"

cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE/calls"
mode=${FIXTURE_MODE:-pending}
hex() { printf "$1%.0s" $(seq 40); }
not_found() { printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; }
boom() { printf 'gh: server error (HTTP 500)\n' >&2; exit 1; }
# A mode whose name ends in the lookup's name makes exactly that lookup fail.
failing() { case "$mode" in *"$1") boom ;; esac; }
# id actor actor-type triggering triggering-type status conclusion run-number head_sha branch
run_json() {
	jq -nc --argjson id "$1" --argjson a "$2" --arg at "$3" --argjson t "$4" --arg tt "$5" --arg status "$6" --arg conclusion "$7" \
		--argjson n "$8" --arg sha "$9" --arg branch "${10}" '{id:$id,status:$status,conclusion:(if $conclusion == "null" then null else $conclusion end),
		run_number:$n,run_attempt:1,head_sha:$sha,head_branch:$branch,event:"push",path:".github/workflows/release.yml@refs/tags/v1.1.1",
		repository:{full_name:"o/r"},actor:{id:$a,type:$at},triggering_actor:{id:$t,type:$tt}}'
}
tag_run() { # the release-build run of v1.1.1 for this mode
	case "$mode" in
		build-running) run_json 77 900 Bot 900 Bot in_progress null 5 "$C3" v1.1.1 ;;
		build-failed) run_json 77 900 Bot 900 Bot completed failure 5 "$C3" v1.1.1 ;;
		human-run) run_json 77 1 User 1 User completed success 5 "$C3" v1.1.1 ;;
		human-rerun) run_json 77 900 Bot 1 User completed success 5 "$C3" v1.1.1 ;;
		run-other-commit) run_json 77 900 Bot 900 Bot completed success 5 "$(hex 9)" v1.1.1 ;;
		*) run_json 77 900 Bot 900 Bot completed success 5 "$C3" v1.1.1 ;;
	esac
}
tag_exists() {
	case "$mode" in
		tag-exists | build-running | build-ok | build-failed | human-run | human-rerun | run-not-started | run-other-commit | no-artifact | expired-artifact | artifact-fail | runs-fail) return 0 ;;
		tag-race) [ -e "$FIXTURE/created" ] ;;
		*) return 1 ;;
	esac
}
case "$*" in
	'api repos/o/r/releases/latest') failing latest-fail; printf '{"tag_name":"%s"}\n' "${FIXTURE_LATEST:-v1.1.0}" ;;
	'api repos/o/r/commits/v1.'*) jq -nc --arg sha "$C0" '{sha:$sha}' ;;
	'api graphql --paginate '*)
		failing alerts-fail
		merged=true
		case "$mode" in unmerged*) merged=false ;; esac
		jq -nc --arg oid "$C1" --argjson merged "$merged" --arg at "${FIXTURE_MERGED_AT:-2026-01-01T00:00:00Z}" '
			def pr($n; $m; $ref; $oid): {dependabotUpdate:{pullRequest:{number:$n,merged:$m,mergedAt:(if $m then $at else null end),baseRefName:"main",headRefName:$ref,author:{login:"dependabot"},mergeCommit:(if $m then {oid:$oid} else null end)}}};
			{data:{repository:{vulnerabilityAlerts:{nodes:[
				pr(7; $merged; "dependabot/npm_and_yarn/x"; $oid), pr(7; $merged; "dependabot/npm_and_yarn/x"; $oid),
				{dependabotUpdate:null},
				pr(8; false; "dependabot/go/y"; null), pr(12; false; "dependabot/go/armed"; null), pr(13; false; "dependabot/go/foreign"; null),
				{dependabotUpdate:{pullRequest:{number:9,merged:true,mergedAt:"2026-01-01T00:00:00Z",baseRefName:"main",headRefName:"feature/z",author:{login:"someone"},mergeCommit:{oid:"9999999999999999999999999999999999999999"}}}}
			]}}}}' ;;
	'api --paginate repos/o/r/pulls?state=open&base=main&per_page=100')
		failing pulls-fail
		jq -nc --arg b "$(hex b)" --arg a "$(hex a)" --arg c "$(hex c)" '[
			{number:8,user:{id:49699333},head:{ref:"dependabot/go/y",sha:$b,repo:{full_name:"o/r"}},auto_merge:null},
			{number:8,user:{id:49699333},head:{ref:"dependabot/go/y",sha:$b,repo:{full_name:"fork/r"}},auto_merge:null},
			{number:10,user:{id:49699333},head:{ref:"dependabot/go/version",sha:$b,repo:{full_name:"o/r"}},auto_merge:null},
			{number:11,user:{id:1},head:{ref:"dependabot/go/y",sha:$b,repo:{full_name:"o/r"}},auto_merge:null},
			{number:12,user:{id:49699333},head:{ref:"dependabot/go/armed",sha:$a,repo:{full_name:"o/r"}},auto_merge:{merge_method:"merge"}},
			{number:13,user:{id:49699333},head:{ref:"dependabot/go/foreign",sha:$c,repo:{full_name:"o/r"}},auto_merge:null}]' ;;
	'api --paginate repos/o/r/pulls/8/commits?per_page=100')
		failing commits-fail
		jq -nc --arg b "$(hex b)" '[{sha:$b,author:{login:"dependabot[bot]"}}]' ;;
	'api --paginate repos/o/r/pulls/13/commits?per_page=100')
		jq -nc --arg c "$(hex c)" '[{sha:"1111111111111111111111111111111111111111",author:{login:"dependabot[bot]"}},{sha:$c,author:{login:"someone"}}]' ;;
	'api repos/o/homebrew-tap/contents/Casks/hikyo.rb?ref=main')
		failing cask-fail
		[ "$mode" != cask-missing ] || not_found
		jq -nc --arg c "$(printf 'cask "hikyo" do\n  version "%s"\nend\n' "${FIXTURE_CASK:-1.1.0}" | base64)" '{content:$c}' ;;
	'api repos/o/r/git/ref/tags/v1.1.1')
		tag_exists || not_found
		echo '{"object":{"type":"tag","sha":"dddddddddddddddddddddddddddddddddddddddd"}}' ;;
	'api repos/o/r/git/tags/'*) jq -nc --arg c "$C3" '{object:{type:"commit",sha:$c}}' ;;
	'api --paginate repos/o/r/actions/workflows/release.yml/runs?event=push&head_sha='*'&per_page=100')
		failing runs-fail
		[ "$mode" != run-not-started ] || { echo '{"workflow_runs":[]}'; exit 0; }
		# A decoy run of another tag and an older failed attempt of this one never decide.
		older_sha=$C3
		[ "$mode" != run-other-commit ] || older_sha=$(hex 9)
		# A person's release has no bot attempt at all.
		older_actor=900 older_type=Bot
		case "$mode" in human-*) older_actor=1 older_type=User ;; esac
		jq -nc --argjson run "$(tag_run)" --argjson older "$(run_json 76 "$older_actor" "$older_type" "$older_actor" "$older_type" completed failure 4 "$older_sha" v1.1.1)" \
			--argjson decoy "$(run_json 70 900 Bot 900 Bot completed success 9 "$C3" v1.1.0)" '{workflow_runs:[$older,$run,$decoy]}' ;;
	'api repos/o/r/actions/runs/77') tag_run ;;
	'api repos/o/r/actions/runs/77/artifacts?per_page=100')
		failing artifact-fail
		case "$mode" in
			no-artifact) echo '{"artifacts":[{"id":3,"name":"other","expired":false},{"id":4,"name":"release-manifest-sha256-770-1","expired":false}]}' ;;
			expired-artifact) echo '{"artifacts":[{"id":1,"name":"release-manifest-sha256-77-1","expired":true}]}' ;;
			*) echo '{"artifacts":[{"id":1,"name":"release-manifest-sha256-77-1","expired":false},{"id":2,"name":"release-manifest-sha256-77-2","expired":false},{"id":3,"name":"other","expired":false},{"id":4,"name":"release-manifest-sha256-770-1","expired":false}]}' ;;
		esac ;;
	'api repos/o/r/actions/workflows/docs.yml/runs?branch=main&per_page=10')
		failing docs-fail
		if [ "$mode" = docs-running ]; then echo '{"workflow_runs":[{"status":"in_progress"}]}'; else echo '{"workflow_runs":[{"status":"completed"}]}'; fi ;;
	'api --paginate repos/o/r/git/matching-refs/tags/v')
		failing matching-refs-fail
		printf '[{"ref":"refs/tags/v1.1.0"},{"ref":"refs/tags/v-ruleset-probe"},{"ref":"refs/tags/v1.2.0-nightly.20261112.1.gabcdef12"}]\n'
		[ "$mode" != higher-tag ] || printf '[{"ref":"refs/tags/v1.1.2"}]\n' ;;
	'api repos/o/r/releases/tags/v1.1.1')
		failing release-lookup-fail
		if [ "${FIXTURE_PUBLISHED:-}" = 1 ]; then echo '{"draft":false}'; elif [ "$mode" = release-exists ]; then echo '{"draft":true}'; else not_found; fi ;;
	'api --method POST repos/o/r/git/tags '*) echo '{"sha":"dddddddddddddddddddddddddddddddddddddddd"}' ;;
	'api --method POST repos/o/r/git/refs -f ref=refs/tags/'*)
		if [ "$mode" = tag-race ]; then : >"$FIXTURE/created"; printf 'gh: Reference already exists (HTTP 422)\n' >&2; exit 1; fi
		echo '{}' ;;
	'api --method POST repos/o/r/git/refs '*) echo '{}' ;;
	'release download v1.1.1 --repo o/r --dir '*)
		for last; do :; done
		jq -nc --arg c "$C3" '{source_commit:$c}' >"$last/release-manifest.json"
		for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json; do
			if [ "$mode" = trust-recorded ]; then cp "$REPO/release/trust/$name" "$last/$name"; else printf 'new %s\n' "$name" >"$last/$name"; fi
		done ;;
	'api repos/o/r/git/ref/heads/main') jq -nc --arg sha "$TIP" '{object:{sha:$sha}}' ;;
	'api repos/o/r/git/ref/heads/release/sync-trust-1.1.1')
		case "$mode" in
			branch-unrelated | branch-related | contents-fail) jq -nc --arg sha "$C2" '{object:{sha:$sha}}' ;;
			branch-has-commit | pr-armed | pr-unarmed-recent | pr-unarmed-old) jq -nc --arg sha "$(hex e)" '{object:{sha:$sha}}' ;;
			*) not_found ;;
		esac ;;
	'api repos/o/r/git/ref/heads/release/sync-trust-1.1.0')
		case "$mode" in cleanup-merged | cleanup-unmerged | cleanup-other-head) jq -nc --arg sha "$(hex 5)" '{object:{sha:$sha}}' ;; *) not_found ;; esac ;;
	'api repos/o/r/compare/'*)
		if [ "$mode" = branch-unrelated ]; then echo '{"files":[{"filename":"release/trust/metadata.json"},{"filename":"scripts/evil.sh"}]}'; else echo '{"files":[{"filename":"release/trust/metadata.json"}]}'; fi ;;
	'api repos/o/r/contents/release/trust/'*)
		failing contents-fail
		name=${2#repos/o/r/contents/release/trust/}
		name=${name%%\?*}
		case "$mode" in
			branch-has-commit | pr-armed | pr-unarmed-recent | pr-unarmed-old) jq -nc --arg c "$(printf 'new %s\n' "$name" | base64)" '{content:$c}' ;;
			*) jq -nc --arg c "$(printf 'old %s\n' "$name" | base64)" '{content:$c}' ;;
		esac ;;
	'api graphql --input '*)
		jq -e '.variables.input.fileChanges.additions | length == 4' "$4" >/dev/null
		jq -nc --arg oid "$(hex e)" '{data:{createCommitOnBranch:{commit:{oid:$oid}}}}' ;;
	'api repos/o/r/commits/'eeee*)
		verified=true
		[ "$mode" != unverified ] || verified=false
		jq -nc --argjson verified "$verified" '{commit:{verification:{verified:$verified},author:{name:"app[bot]",email:"900+app[bot]@users.noreply.github.com"},message:"chore\n\nSigned-off-by: app[bot] <900+app[bot]@users.noreply.github.com>"}}' ;;
	'api repos/o/r/pulls?state=open&base=main&head=o:release/sync-trust-1.1.1')
		now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
		case "$mode" in
			pr-armed) jq -nc --arg now "$now" '[{number:42,created_at:$now,auto_merge:{merge_method:"merge"}}]' ;;
			pr-unarmed-recent) jq -nc --arg now "$now" '[{number:42,created_at:$now,auto_merge:null}]' ;;
			pr-unarmed-old) echo '[{"number":42,"created_at":"2026-01-01T00:00:00Z","auto_merge":null}]' ;;
			*) echo '[]' ;;
		esac ;;
	'api repos/o/r/pulls?state=closed&base=main&head=o:release/sync-trust-1.1.0&per_page=100')
		case "$mode" in
			cleanup-merged) jq -nc --arg sha "$(hex 5)" '[{merged_at:"2026-11-14T00:00:00Z",head:{sha:$sha}}]' ;;
			cleanup-other-head) jq -nc --arg sha "$(hex 6)" '[{merged_at:"2026-11-14T00:00:00Z",head:{sha:$sha}}]' ;;
			*) jq -nc --arg sha "$(hex 5)" '[{merged_at:null,head:{sha:$sha}}]' ;;
		esac ;;
	'api --method DELETE repos/o/r/git/refs/heads/release/sync-trust-1.1.0') echo '{}' ;;
	'api --method POST repos/o/r/pulls '*) echo '{"number":42}' ;;
	'pr merge 8 '*) [ "$mode" != merge-refused ] || { echo 'gh: auto-merge refused' >&2; exit 1; } ;;
	'pr merge '*) ;;
	'api repos/o/homebrew-tap/pulls/5') jq -nc --arg sha "$(hex f)" '{head:{sha:$sha}}' ;;
	'api repos/o/homebrew-tap/commits/'ffff*)
		failing checks-fail
		conclusion=success
		[ "$mode" != tap-pending ] || conclusion=null
		jq -nc --arg c "$conclusion" '{check_runs:[{conclusion:(if $c == "null" then null else $c end)}]}' ;;
	'api --method PUT repos/o/homebrew-tap/pulls/5/merge '*) echo '{}' ;;
	'issue list '*)
		failing issues-fail
		case "$mode" in
			issue-open | unmerged-issue-open) echo '[{"number":2,"title":"Re: Unattended stable patch release is stalled (copy)"},{"number":3,"title":"Unattended stable patch release is stalled"}]' ;;
			*) echo '[{"number":2,"title":"Unrelated"}]' ;;
		esac ;;
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
	*'stable preflight '*) [ "${FIXTURE_MODE:-}" != preflight-fail ] || exit 1 ;;
esac
SH
cat >"$scratch/bin/green" <<'SH'
#!/bin/sh
case " $GREEN " in *" $2 "*) exit 0 ;; esac
exit 1
SH
cat >"$scratch/bin/docs" <<'SH'
#!/bin/sh
[ -z "${DOCS_FAIL:-}" ]
SH
cat >"$scratch/bin/fallback" <<'SH'
#!/bin/sh
[ -z "${FALLBACK_FAIL:-}" ]
SH
cat >"$scratch/bin/synced" <<'SH'
#!/bin/sh
[ "${FIXTURE_MODE:-}" != unsynced ]
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
	DOCS_LIVE_BIN="$scratch/bin/docs" FALLBACK_BIN="$scratch/bin/fallback" HOMEBREW_BIN="$scratch/bin/homebrew" PUBLISH_BIN="$scratch/bin/publish" \
	GH_REPO=o/r HIKYO_REPO_ROOT="$repo" GITHUB_SHA="$tip" STABLE_RELEASE_APP_BOT_ID=900

fail() { printf 'stable-reconcile fixture: %s\n' "$*" >&2; exit 1; }
run() { # mode command...
	FIXTURE_MODE=$1
	shift
	export FIXTURE_MODE
	: >"$scratch/out"
	: >"$scratch/calls"
	rm -f "$scratch/created"
	GITHUB_OUTPUT="$scratch/out" sh "$script_dir/stable-reconcile.sh" "$@" >"$scratch/log" 2>&1 || { cat "$scratch/log" >&2; return 1; }
}
out() { sed -n "s/^$1=//p" "$scratch/out" | tail -n 1; }
expect() { [ "$(out "$1")" = "$2" ] || fail "$3: $1 was '$(out "$1")', want '$2'"; }
called() { grep -q -- "$1" "$scratch/calls"; }
# A refused or unreadable lookup stops the run with the reason, and never writes.
expect_dies() { # reason-fragment description command...
	want=$1 what=$2
	shift 2
	if "$@" >/dev/null 2>&1; then fail "$what: succeeded"; fi
	grep -qF -- "$want" "$scratch/log" || { cat "$scratch/log" >&2; fail "$what: died for the wrong reason, want '$want'"; }
}
must_fail() { # description command...
	what=$1
	shift
	if "$@" >/dev/null 2>&1; then fail "$what"; fi
}
no_writes() { if grep -Eq 'POST|PUT|DELETE|pr merge|issue (create|edit|close)|graphql --input|^publish' "$scratch/calls"; then cat "$scratch/calls" >&2; fail "$1: wrote"; fi; }
# With an old security merge waiting a stuck state raises the stall issue; with a recent one it does not.
expect_stall() { # mode action
	(run "$1" plan) || fail "$1: plan failed"
	expect action "$2" "$1 stall"; expect issue open "$1 stall (24h escalation)"
	(FIXTURE_MERGED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) run "$1" plan) || fail "$1: plan failed"
	expect action "$2" "$1 recent"; expect issue none "$1 recent (no alert inside 24h)"
}
export GREEN="$c3 $c2 $c1 $c0"
b=$(hex b)

# --- plan: the newest green commit that already records the latest release is tagged.
run pending plan
expect action tag "green tip"; expect commit "$tip" "green tip"; expect version 1.1.1 "green tip"
# Dependabot lane: only the proven, unarmed, same-repo PR whose commits are all Dependabot's. #12 is
# already armed, #13 has a commit by someone else, #10 has no alert, #11 is not Dependabot, the fork copy is foreign.
expect automerge "8:$b" "security lane"
(GREEN="$c2 $c1" run pending plan); expect action tag "red tip"; expect commit "$c2" "red tip"
# c1 holds the security merge but its trust predates the latest release: the build would burn the tag.
(GREEN="$c1" run pending plan); expect action none "stale trust at the only green commit"
(GREEN="$c0" run pending plan); expect action none "no green commit"; expect issue open "no green commit stalled"
(GREEN="$c0" FIXTURE_MERGED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) run pending plan); expect issue none "no green commit, recent"
# Nothing to release: the stall issue closes only when there is one to close.
run unmerged plan; expect action none "unmerged"; expect issue none "no stall issue to close"
run unmerged-issue-open plan; expect issue close "an open stall issue is closed"
# --- plan: every refusal tag() would hit keeps the plan from deciding tag, and alerts after a day.
for mode in higher-tag not-patch preflight-fail unsynced release-exists matching-refs-fail release-lookup-fail; do
	expect_stall "$mode" none
done
(DOCS_FAIL=1; export DOCS_FAIL; expect_stall pending wait)
(FALLBACK_FAIL=1; export FALLBACK_FAIL; expect_stall pending wait)
expect_stall docs-running wait
# --- plan: an existing tag decides by who pushed it and how its run ended.
expect_stall run-not-started wait
expect_stall run-other-commit wait
expect_stall build-running wait
expect_stall no-artifact wait
expect_stall expired-artifact wait
run build-ok plan
expect action publish "built"; expect run_id 77 "built"; expect artifact release-manifest-sha256-77-2 "built"
# A person's release is the ceremony's, even when the bot's run was rerun by a person.
for mode in human-run human-rerun; do
	expect_stall "$mode" none
	case $(out issue_text) in *'cannot retry'* | *immutable*) fail "$mode: a human build must not get the cannot-retry text" ;; esac
done
run build-failed plan; expect action none "failed build"; expect issue open "failed build"
case $(out issue_text) in *'gh release delete v1.1.1 --repo o/r --yes'*) ;; *) fail 'the failed build issue lacks the draft deletion command' ;; esac
# Trust behind the published release: sync it first. Semver precedence, not text order.
(FIXTURE_LATEST=v1.1.1 run pending plan); expect action sync-trust "unsynced"; expect version 1.1.1 "unsynced"
printf '{"highest_release":"1.1.1-rc.1"}\n' >"$repo/release/trust/metadata.json"
(FIXTURE_LATEST=v1.1.1 run pending plan); expect action sync-trust "a release outranks its own prerelease"
printf '{"highest_release":"1.9.0"}\n' >"$repo/release/trust/metadata.json"
(FIXTURE_LATEST=v1.10.0 run pending plan); expect action sync-trust "1.10.0 outranks 1.9.0"
printf '{"highest_release":"1.2.0"}\n' >"$repo/release/trust/metadata.json"
(FIXTURE_LATEST=v1.1.0 run pending plan); expect action none "latest older than trusted"; expect issue open "latest older than trusted"
git -C "$repo" checkout -q -- release/trust/metadata.json
# Homebrew follows the latest release; the merged trust branch is cleaned up.
(FIXTURE_CASK=1.0.0 run pending plan); expect homebrew 1.1.0 "stale cask"
run pending plan; expect homebrew '' "current cask"
run cask-missing plan; expect homebrew 1.1.0 "no cask yet"
run cleanup-merged plan; expect cleanup release/sync-trust-1.1.0 "merged branch"
# --- plan fails closed on every listing it decides from.
for mode in latest-fail alerts-fail pulls-fail commits-fail cask-fail docs-fail runs-fail artifact-fail; do
	expect_dies 'cannot' "plan with $mode" run "$mode" plan
	no_writes "plan with $mode"
done
expect_dies 'cannot list open issues' 'closing with a failed issue listing' run unmerged-issues-fail plan

# --- tag: creates the annotated tag object, then the ref, for exactly the planned commit.
(VERSION=1.1.1 COMMIT=$c3 run pending tag)
called 'api --method POST repos/o/r/git/tags -f tag=v1.1.1 -f message=Hikyo 1.1.1 -f object='"$c3"' -f type=commit' || fail 'tag object not created'
called 'api --method POST repos/o/r/git/refs -f ref=refs/tags/v1.1.1 -f sha=dddd' || fail 'tag ref not created'
called 'stable candidate --patch-only' || fail 'tag skipped the patch-only rule'
# Every refusal tag() has stops it before any write, and a failed listing is a refusal, never a pass.
tag_refused() { # mode reason
	(export VERSION=1.1.1 COMMIT=$c3; expect_dies "$2" "tagged despite $1" run "$1" tag)
	called 'git/refs -f ref=refs/tags' && fail "created a ref despite $1"
	no_writes "tag with $1"
}
tag_refused higher-tag 'stable tag at or above v1.1.1 already exists'
tag_refused not-patch 'patch successor'
tag_refused preflight-fail 'does not authenticate'
tag_refused unsynced 'not synchronized'
tag_refused release-exists 'already has a release'
tag_refused matching-refs-fail 'cannot read'
tag_refused release-lookup-fail 'cannot prove the version is unused'
(export VERSION=1.1.1 COMMIT=$c3 GREEN=''; expect_dies 'green source proof' 'tagged a commit without green proof' run pending tag)
(export VERSION=1.1.1 COMMIT="$(hex 7)"; expect_dies 'not on main history' 'tagged a commit off main' run pending tag)
(export VERSION=1.2 COMMIT=$c3; expect_dies 'plain stable version' 'accepted a non-patch version string' run pending tag)
# A lagging read plans the same tag twice: the second run finds it there and does nothing, or loses the create race and says so.
(VERSION=1.1.1 COMMIT=$c3 run tag-exists tag); called 'POST' && fail 'tagged a tag that already exists'
(VERSION=1.1.1 COMMIT=$c3 run tag-race tag); grep -q 'already exists' "$scratch/log" || fail 'a lost create race was not recognised'

# --- publish: digest from the retained artifact; only the app's own successful push of this tag commit.
printf '%064d\n' 0 >"$scratch/digest"
(VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" run build-ok publish)
called "publish o/r v1.1.1 $(printf '%064d' 0) $repo/release/trust run=77 state=" || fail 'publish helper not called with the artifact digest'
called 'stable verify .*--trust-only --state' || fail 'rollback state was not seeded from main trust'
for mode in human-run human-rerun run-other-commit build-failed build-running; do
	(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest"; expect_dies 'not a successful release-build' "published a draft for $mode" run "$mode" publish)
	called '^publish' && fail "the publish helper ran for $mode"
done
printf 'not-a-digest\n' >"$scratch/digest"
(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest"; expect_dies 'SHA-256' 'published without a SHA-256 digest' run build-ok publish)

# --- sync-trust: one app commit with the four files, verified by GitHub, auto-merge armed.
export FIXTURE_PUBLISHED=1
sync() { VERSION=1.1.1 APP_SLUG=app run "$@" sync-trust; }
sync pending
called 'graphql --input' || fail 'no createCommitOnBranch'
called 'pr merge 42 --repo o/r --auto --merge --match-head-commit eeee' || fail 'auto-merge not armed'
must_fail 'accepted an unverified commit' sync unverified
called 'pr merge' && fail 'armed an unverified commit'
(export VERSION=1.1.1 APP_SLUG=app STABLE_RELEASE_APP_BOT_ID=0; expect_dies 'numeric bot user id' 'accepted a bad bot id' run pending sync-trust)
# Trust already records the release: nothing to commit.
sync trust-recorded; grep -q 'already records' "$scratch/log" || fail 'trust-recorded was not recognised'
no_writes 'trust already recorded'
# An existing branch may differ from main in the four trust files only; anything else is refused untouched.
must_fail 'overwrote a branch with unrelated changes' sync branch-unrelated
grep -q 'unrelated changes' "$scratch/log" || fail 'wrong refusal for unrelated changes'
no_writes 'unrelated branch'
sync branch-related; called 'graphql --input' || fail 'an existing branch without the commit got none'
# A retry finds the commit already on the branch: reuse it and arm, never add an empty one.
sync branch-has-commit; called 'graphql --input' && fail 'a retry added a duplicate commit'
called 'pr merge 42 --repo o/r --auto --merge --match-head-commit eeee' || fail 'a retry did not arm the reused commit'
# The queue unarms a rejected PR: re-arm for a while, never when armed, and stop with an error after the patience.
sync pr-armed; called 'pr merge' && fail 'armed a PR that is already armed'
sync pr-unarmed-recent; called 'pr merge 42' || fail 'did not re-arm a recent unarmed PR'
must_fail 're-armed a PR the queue has refused for hours' sync pr-unarmed-old
grep -q 'no longer queued' "$scratch/log" || fail 'wrong error for an abandoned trust PR'
called 'pr merge' && fail 'armed a stale trust PR'
# A failed contents read must not become a redundant commit.
must_fail 'went on after a failed contents read' sync contents-fail
called 'graphql --input' && fail 'created a commit after a failed contents read'
# Merged trust branches are deleted by the cleanup lane, only for exactly the merged head.
(BRANCH=release/sync-trust-1.1.0 run cleanup-merged cleanup); called 'DELETE repos/o/r/git/refs/heads/release/sync-trust-1.1.0' || fail 'merged branch not deleted'
for mode in cleanup-unmerged cleanup-other-head; do
	(BRANCH=release/sync-trust-1.1.0 run $mode cleanup); called 'DELETE' && fail "deleted a branch for $mode"
done
(export BRANCH=release/other; expect_dies 'invalid trust-sync branch' 'cleaned an arbitrary branch' run pending cleanup)

# --- homebrew: merge once the tap's required check is green, otherwise wait for the next run.
(VERSION=1.1.1 run pending homebrew); called 'merge_method=squash -f sha=ffff' || fail 'tap PR not squash-merged'
(VERSION=1.1.1 run tap-pending homebrew); called 'merge_method=squash' && fail 'merged a tap PR with a pending check'
(export VERSION=1.1.1; expect_dies 'cannot read' 'homebrew went on after a failed check-run read' run checks-fail homebrew)
called 'merge_method=squash' && fail 'merged after a failed check-run read'
unset FIXTURE_PUBLISHED

# --- automerge: only well-formed entries, pinned to the verified head, and one refusal hides no other.
(PRS="8:$b" run pending automerge)
called "pr merge 8 --repo o/r --auto --merge --match-head-commit $b" || fail 'auto-merge not armed'
(export PRS='8:short'; expect_dies 'invalid pull request entry' 'accepted a malformed entry' run pending automerge)
(export PRS="8:$b 9:$(hex c)"; expect_dies 'could not queue every' 'a refused auto-merge was not an error' run merge-refused automerge)
called "pr merge 9 --repo o/r --auto --merge --match-head-commit $(hex c)" || fail 'the first refusal hid the second pull request'

# --- issue: one issue found by exact title, edited in place, closed when the stall clears.
(ISSUE_TEXT=stalled run issue-open issue open); called 'issue edit 3' || fail 'issue not updated in place'
(ISSUE_TEXT=stalled run pending issue open); called 'issue create' || fail 'issue not created'
run issue-open issue close; called 'issue close 3' || fail 'issue not closed'
(export ISSUE_TEXT=stalled; expect_dies 'cannot list open issues' 'opened an issue after a failed listing' run issues-fail issue open)
called 'issue create' && fail 'created a duplicate after a failed listing'
printf 'stable-reconcile fixture: planner states, stall alerts, tag, publish, trust PR, cleanup, Homebrew, Dependabot and issue lanes passed\n'

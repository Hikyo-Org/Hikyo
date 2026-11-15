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
trust_metadata() { jq -nc --arg highest "$1" --args '{highest_release:$highest, releases:($ARGS.positional | map({version:., sequence:1, manifest_sha256:("a"*64)}))}' "$@"; }
trust_metadata 1.0.0 1.0.0 >"$repo/release/trust/metadata.json"
for name in metadata.sigstore.json catalog.json catalog.sigstore.json; do printf 'old %s\n' "$name" >"$repo/release/trust/$name"; done
printf '{}\n' >"$repo/release/repository/fallback-channel-test.json"
mkdir -p "$repo/release/compatibility"
cp "$script_dir/../../release/compatibility/sources.json" "$repo/release/compatibility/sources.json"
git -C "$repo" add -A
git -C "$repo" commit -q -m c0 --no-gpg-sign
git -C "$repo" commit -q --allow-empty -m c1 --no-gpg-sign
trust_metadata 1.1.0 1.0.0 1.1.0 >"$repo/release/trust/metadata.json"
git -C "$repo" commit -q -a -m c2 --no-gpg-sign
git -C "$repo" commit -q --allow-empty -m c3 --no-gpg-sign
c0=$(git -C "$repo" rev-parse HEAD~3) c1=$(git -C "$repo" rev-parse HEAD~2) c2=$(git -C "$repo" rev-parse HEAD~1) c3=$(git -C "$repo" rev-parse HEAD)
tip=$c3
# c4 lies after the tip: a security merge only the tip's successor holds, for the floor cases.
git -C "$repo" commit -q --allow-empty -m c4 --no-gpg-sign
c4=$(git -C "$repo" rev-parse HEAD)
hex() { printf "$1%.0s" $(seq 40); }
# Real-shaped declarations: the migrations of the declaration the real generator
# makes for this tree, the source edges of the hand-reviewed sources.json.
real_declaration() { # version sequence commit
	jq -S --slurpfile sources "$script_dir/../../release/compatibility/sources.json" --arg v "$1" --argjson s "$2" --arg c "$3" '
		.version = $v | .sequence = $s | .commit = $c |
		.engines |= map(.sources = $sources[0].engines[.migrations.engine])' "$script_dir/../../internal/buildcompat/development.json"
}
mkdir "$scratch/baseline"
real_declaration 1.1.0 2 "$c0" >"$scratch/baseline/upgrade-compatibility.json"
real_declaration 1.1.1 3 "$c3" >"$scratch/candidate-decl.json"
jq -S '.engines[0].schema_sha256 = ("b" * 64)' "$scratch/candidate-decl.json" >"$scratch/changed-decl.json"
jq -nc --arg c "$c0" '{version:"1.1.0",sequence:2,commit:$c,key_id:"k",public_key:"k.pub"}' >"$scratch/baseline/release-candidate.json"
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
jq -nc --arg c "$c0" --arg sha "$(sha256_file "$scratch/baseline/upgrade-compatibility.json")" \
	'{source_commit:$c,artifacts:[{name:"upgrade-compatibility.json",kind:"upgrade-compatibility",sha256:$sha}]}' >"$scratch/baseline/release-manifest.json"
export FIXTURE="$scratch" C0="$c0" C1="$c1" C2="$c2" C3="$c3" C4="$c4" TIP="$tip" REPO="$repo"

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
# The run path is the bare workflow file or the file with an @ref suffix. A file
# that merely shares the prefix is a different workflow and never a release build.
run_path=.github/workflows/release.yml@refs/tags/v1.1.1
[ "$mode" != run-prefix-path ] || run_path=.github/workflows/release.yml.evil
# id actor actor-type triggering triggering-type status conclusion run-number head_sha branch
run_json() {
	jq -nc --argjson id "$1" --argjson a "$2" --arg at "$3" --argjson t "$4" --arg tt "$5" --arg status "$6" --arg conclusion "$7" \
		--argjson n "$8" --arg sha "$9" --arg branch "${10}" --arg path "$run_path" '{id:$id,status:$status,conclusion:(if $conclusion == "null" then null else $conclusion end),
		run_number:$n,run_attempt:1,head_sha:$sha,head_branch:$branch,event:"push",path:$path,created_at:"2026-02-01T00:00:00Z",
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
		tag-exists | build-running | build-ok | build-failed | human-run | human-rerun | run-not-started | run-prefix-path | run-other-commit | no-artifact | expired-artifact | artifact-fail | runs-fail) return 0 ;;
		tag-race) [ -e "$FIXTURE/created" ] ;;
		*) return 1 ;;
	esac
}
case "$*" in
	'api --paginate --slurp repos/o/r/releases?per_page=100')
		failing releases-fail
		# Pages of published releases (and a draft, which never counts). FIXTURE_LATEST names the
		# newest stable release; FIXTURE_EXTRA adds more tags, for example a prerelease.
		at=${FIXTURE_PUBLISHED_AT:-2026-01-01T00:00:00Z}
		jq -nc --arg latest "${FIXTURE_LATEST:-v1.1.0}" --arg extra "${FIXTURE_EXTRA:-}" --arg at "$at" '
			[($latest, "v1.0.0", ($extra | select(. != "") | split(" ")[]), "v9.9.9-nightly.20260101.1.gabcdef12")] |
			map({tag_name: ., draft: false, prerelease: contains("-"), published_at: $at}) + [{tag_name: "v8.0.0", draft: true, prerelease: false, published_at: null}] | [.]' ;;
	'api repos/o/r/commits/v1.'*) jq -nc --arg sha "${FIXTURE_LATEST_COMMIT:-$C0}" '{sha:$sha}' ;;
	'api graphql --paginate '*)
		failing alerts-fail
		# Every alert state is listed explicitly, so a changed API default cannot hide the fixed ones.
		case "$*" in *'states:[OPEN,FIXED]'*) ;; *) printf 'the alert query must list its states explicitly\n' >&2; exit 2 ;; esac
		merged=true
		case "$mode" in unmerged*) merged=false ;; esac
		# The author is bound by the numeric id; alert-impostor has the right login and the wrong id.
		bot_id=49699333
		[ "$mode" != alert-impostor ] || bot_id=1
		jq -nc --arg oid "${FIXTURE_SEC_OID:-$C1}" --argjson merged "$merged" --arg at "${FIXTURE_MERGED_AT:-2026-01-01T00:00:00Z}" --argjson bot "$bot_id" \
			--arg oid2 "${FIXTURE_SEC2_OID:-}" --arg at2 "${FIXTURE_SEC2_AT:-2026-01-01T00:00:00Z}" --arg mode "$mode" '
			def pr($n; $m; $ref; $oid; $at): {state:(if $m then "FIXED" else "OPEN" end),fixedAt:(if $m then $at else null end),dependabotUpdate:{pullRequest:{number:$n,merged:$m,mergedAt:(if $m then $at else null end),baseRefName:"main",headRefName:$ref,author:{login:"dependabot",databaseId:$bot},mergeCommit:(if $m then {oid:$oid} else null end)}}};
			{data:{repository:{vulnerabilityAlerts:{nodes:([
				pr(7; $merged; "dependabot/npm_and_yarn/x"; $oid; $at), pr(7; $merged; "dependabot/npm_and_yarn/x"; $oid; $at),
				{dependabotUpdate:null},
				pr(8; false; "dependabot/go/y"; null; $at), pr(12; false; "dependabot/go/armed"; null; $at), pr(13; false; "dependabot/go/foreign"; null; $at),
				{dependabotUpdate:{pullRequest:{number:9,merged:true,mergedAt:"2026-01-01T00:00:00Z",baseRefName:"main",headRefName:"feature/z",author:{login:"someone",databaseId:1},mergeCommit:{oid:"9999999999999999999999999999999999999999"}}}}
			] + (if $oid2 != "" then [pr(14; true; "dependabot/go/second"; $oid2; $at2)] else [] end)
			+ (if $mode == "unlinked-alert" then [{state:"FIXED",fixedAt:"2026-06-01T00:00:00Z",dependabotUpdate:null}] else [] end))}}}}' ;;
	'api --paginate repos/o/r/pulls?state=open&base=main&per_page=100')
		failing pulls-fail
		jq -nc --arg b "$(hex b)" --arg a "$(hex a)" --arg c "$(hex c)" --arg foreign "${FIXTURE_FOREIGN:-0}" '[
			{number:8,user:{id:49699333},head:{ref:"dependabot/go/y",sha:$b,repo:{full_name:"o/r"}},auto_merge:null},
			{number:8,user:{id:49699333},head:{ref:"dependabot/go/y",sha:$b,repo:{full_name:"fork/r"}},auto_merge:null},
			{number:10,user:{id:49699333},head:{ref:"dependabot/go/version",sha:$b,repo:{full_name:"o/r"}},auto_merge:null},
			{number:11,user:{id:1},head:{ref:"dependabot/go/y",sha:$b,repo:{full_name:"o/r"}},auto_merge:null},
			{number:12,user:{id:49699333},head:{ref:"dependabot/go/armed",sha:$a,repo:{full_name:"o/r"}},auto_merge:{merge_method:"merge"}}] +
			(if $foreign == "1" then [{number:13,user:{id:49699333},head:{ref:"dependabot/go/foreign",sha:$c,repo:{full_name:"o/r"}},auto_merge:null}] else [] end)' ;;
	'api --paginate repos/o/r/pulls/8/commits?per_page=100')
		failing commits-fail
		# Numeric ids decide: the logins here are always the right ones, so only the ids can fail.
		jq -nc --arg b "$([ "$mode" = dependabot-head-moved ] && hex 7 || hex b)" --arg mode "$mode" '[{sha:$b,author:{login:"dependabot[bot]",id:(if $mode == "dependabot-author-id" then 1 else 49699333 end)},
			committer:{login:"web-flow",id:(if $mode == "dependabot-committer" then 1 else 19864447 end)},
			commit:{verification:{verified:($mode != "dependabot-unverified")}}}]' ;;
	'api --paginate repos/o/r/pulls/13/commits?per_page=100')
		jq -nc --arg c "$(hex c)" '[{sha:"1111111111111111111111111111111111111111",author:{login:"dependabot[bot]",id:49699333},committer:{login:"web-flow",id:19864447},commit:{verification:{verified:true}}},
			{sha:$c,author:{login:"someone",id:1},committer:{login:"web-flow",id:19864447},commit:{verification:{verified:true}}}]' ;;
	'api repos/o/r/contents/scripts/release/require-signed-tag.sh?ref='*' --jq .content')
		failing authority-fail
		if [ "$mode" = no-authority ] || [ "${FIXTURE_NO_AUTH:-}" = 1 ]; then printf 'set -eu\n# the older rule: the tag signature only\n' | base64; else printf 'set -eu\n# triggering_actor and release_run_jq decide\n' | base64; fi ;;
	'api repos/o/homebrew-tap/contents/Casks/hikyo.rb?ref=main')
		failing cask-fail
		[ "$mode" != cask-missing ] || not_found
		case "$mode" in
			cask-null) echo '{"content":null}'; exit 0 ;;
			cask-empty) echo '{"content":""}'; exit 0 ;;
		esac
		jq -nc --arg c "$(printf 'cask "hikyo" do\n  version "%s"\nend\n' "${FIXTURE_CASK:-1.1.0}" | base64)" '{content:$c}' ;;
	'api repos/o/r/git/ref/tags/v1.1.1')
		failing tag-ref-fail
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
		[ "$mode" != higher-tag ] || printf '[{"ref":"refs/tags/v1.1.2"}]\n'
		# The very version about to be tagged, seen by the listing before its own ref lookup sees it.
		[ "$mode" != same-tag-listed ] || printf '[{"ref":"refs/tags/v1.1.1"}]\n' ;;
	'api repos/o/r/releases/tags/v1.1.1')
		failing release-lookup-fail
		if [ "${FIXTURE_PUBLISHED:-}" = 1 ]; then echo '{"draft":false}'; elif [ "$mode" = release-exists ]; then echo '{"draft":false}'; else not_found; fi ;;
	'api --method POST repos/o/r/git/tags '*) echo '{"sha":"dddddddddddddddddddddddddddddddddddddddd"}' ;;
	'api --method POST repos/o/r/git/refs -f ref=refs/tags/'*)
		if [ "$mode" = tag-race ]; then : >"$FIXTURE/created"; printf 'gh: Reference already exists (HTTP 422)\n' >&2; exit 1; fi
		echo '{}' ;;
	'api --method POST repos/o/r/git/refs '*) echo '{}' ;;
	'release download v1.1.0 --repo o/r --dir '*)
		failing download-fail
		for last; do :; done
		[ "$mode" != baseline-missing ] || { printf 'release not found\n' >&2; exit 1; }
		cp "$FIXTURE/baseline/"* "$last/" ;;
	'release download v1.1.1 --repo o/r --dir '*)
		for last; do :; done
		jq -nc --arg c "$C3" '{source_commit:$c}' >"$last/release-manifest.json"
		for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json; do
			if [ "$mode" = download-empty ] && [ "$name" = catalog.json ]; then : >"$last/$name"; elif [ "$mode" = trust-recorded ]; then cp "$REPO/release/trust/$name" "$last/$name"; else printf 'new %s\n' "$name" >"$last/$name"; fi
		done ;;
	'api repos/o/r/git/ref/heads/main') jq -nc --arg sha "$TIP" '{object:{sha:$sha}}' ;;
	'api repos/o/r/git/ref/heads/release/sync-trust-1.1.1')
		case "$mode" in
			branch-unrelated | branch-related | contents-fail) jq -nc --arg sha "$C2" '{object:{sha:$sha}}' ;;
			branch-has-commit | pr-armed | pr-unarmed-recent | pr-unarmed-old | pr-unarmed-reopened) jq -nc --arg sha "$(hex e)" '{object:{sha:$sha}}' ;;
			*) not_found ;;
		esac ;;
	'api repos/o/r/git/ref/heads/release/sync-trust-1.1.0')
		failing heads-ref-fail
		case "$mode" in cleanup-merged | cleanup-unmerged | cleanup-other-head) jq -nc --arg sha "$(hex 5)" '{object:{sha:$sha}}' ;; *) not_found ;; esac ;;
	'api repos/o/r/git/ref/heads/release/sync-trust-'*) not_found ;;
	'api repos/o/r/compare/'*)
		if [ "$mode" = branch-unrelated ]; then echo '{"files":[{"filename":"release/trust/metadata.json"},{"filename":"scripts/evil.sh"}]}'; else echo '{"files":[{"filename":"release/trust/metadata.json"}]}'; fi ;;
	'api repos/o/r/contents/release/trust/'*)
		failing contents-fail
		name=${2#repos/o/r/contents/release/trust/}
		name=${name%%\?*}
		case "$mode" in
			branch-has-commit | pr-armed | pr-unarmed-recent | pr-unarmed-old | pr-unarmed-reopened) jq -nc --arg c "$(printf 'new %s\n' "$name" | base64)" '{content:$c}' ;;
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
			pr-unarmed-recent | pr-unarmed-reopened) jq -nc --arg now "$now" '[{number:42,created_at:$now,auto_merge:null}]' ;;
			pr-unarmed-old) echo '[{"number":42,"created_at":"2026-01-01T00:00:00Z","auto_merge":null}]' ;;
			*) echo '[]' ;;
		esac ;;
	'api --paginate repos/o/r/pulls?state=all&base=main&head=o:release/sync-trust-1.1.1&per_page=100')
		now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
		case "$mode" in
			pr-unarmed-reopened) jq -nc --arg now "$now" '[{number:42,created_at:$now},{number:41,created_at:"2026-01-01T00:00:00Z"}]' ;;
			pr-unarmed-old) echo '[{"number":42,"created_at":"2026-01-01T00:00:00Z"}]' ;;
			*) jq -nc --arg now "$now" '[{number:42,created_at:$now}]' ;;
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
	'api repos/o/homebrew-tap/pulls/5') jq -nc --arg sha "$(hex f)" '{title:"chore: hikyo 1.1.1",head:{sha:$sha}}' ;;
	'api repos/o/homebrew-tap/commits/'ffff*)
		failing checks-fail
		case "$*" in *'check_name=ci-required&filter=latest'*) ;; *) printf 'the tap check must list the latest ci-required runs: %s\n' "$*" >&2; exit 2 ;; esac
		# The newest run decides: an older success never outranks a newer failure or a re-run in progress.
		case "$mode" in
			tap-pending) echo '{"check_runs":[{"id":1,"started_at":"2026-02-01T00:00:00Z","conclusion":null}]}' ;;
			tap-newest-failed) echo '{"check_runs":[{"id":2,"started_at":"2026-02-01T01:00:00Z","conclusion":"failure"},{"id":1,"started_at":"2026-02-01T00:00:00Z","conclusion":"success"}]}' ;;
			tap-newest-pending) echo '{"check_runs":[{"id":1,"started_at":"2026-02-01T00:00:00Z","conclusion":"success"},{"id":2,"started_at":"2026-02-01T01:00:00Z","conclusion":null}]}' ;;
			# GitHub's run ids need not follow start order: the start time decides, never the id.
		tap-id-skew) echo '{"check_runs":[{"id":9,"started_at":"2026-02-01T00:00:00Z","conclusion":"success"},{"id":3,"started_at":"2026-02-01T01:00:00Z","conclusion":"failure"}]}' ;;
		tap-newest-success) echo '{"check_runs":[{"id":1,"started_at":"2026-02-01T00:00:00Z","conclusion":"failure"},{"id":2,"started_at":"2026-02-01T01:00:00Z","conclusion":"success"}]}' ;;
			*) echo '{"check_runs":[{"id":1,"started_at":"2026-02-01T00:00:00Z","conclusion":"success"}]}' ;;
		esac ;;
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
	*'stable verify '*'--directory '*) [ "${FIXTURE_MODE:-}" != compat-unverifiable ] || exit 1 ;;
	-C*' run ./scripts/release/compatibility '*)
		# Generated at the target commit's own tree, on scratch PostgreSQL.
		[ -f "$2/release/compatibility/sources.json" ] && [ -n "${HIKYO_RELEASE_SCHEMA_POSTGRES_DSN:-}" ] || exit 1
		[ "${FIXTURE_MODE:-}" != compat-gen-fail ] || exit 1
		for last; do :; done
		case "${FIXTURE_MODE:-}" in compat-diff) cp "$FIXTURE/changed-decl.json" "$last" ;; *) cp "$FIXTURE/candidate-decl.json" "$last" ;; esac ;;
esac
SH
cat >"$scratch/bin/green" <<'SH'
#!/bin/sh
if [ "${FIXTURE_MODE:-}" = green-read-fail ]; then printf 'source proof: GitHub read failed for repos/o/r/x: boom\n' >&2; exit 1; fi
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
[ "${FIXTURE_MODE:-}" != synced-fail ] || exit 4
[ "${FIXTURE_MODE:-}" != unsynced ]
SH
cat >"$scratch/bin/postgres" <<'SH'
#!/bin/sh
printf 'postgres %s %s\n' "$POSTGRES_DB" "$POSTGRES_PASSWORD" >>"$FIXTURE/calls"
[ "${FIXTURE_MODE:-}" != postgres-fail ]
SH
cat >"$scratch/bin/homebrew" <<'SH'
#!/bin/sh
printf 'homebrew-helper signoff=%s\n' "${HIKYO_HOMEBREW_SIGNOFF:-}" >>"$FIXTURE/calls"
echo 'homebrew publish: review and merge https://github.com/o/homebrew-tap/pull/5'
SH
cat >"$scratch/bin/publish" <<'SH'
#!/bin/sh
printf 'publish %s run=%s state=%s\n' "$*" "$HIKYO_RELEASE_RUN_ID" "$HIKYO_RELEASE_TRUST_STATE" >>"$FIXTURE/calls"
SH
chmod +x "$scratch"/bin/*
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go" GREEN_BIN="$scratch/bin/green" SYNCED_BIN="$scratch/bin/synced" \
	DOCS_LIVE_BIN="$scratch/bin/docs" FALLBACK_BIN="$scratch/bin/fallback" HOMEBREW_BIN="$scratch/bin/homebrew" PUBLISH_BIN="$scratch/bin/publish" POSTGRES_BIN="$scratch/bin/postgres" \
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
called_f() { grep -qF -- "$1" "$scratch/calls"; }
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
# The proof is Dependabot's own verified commits for exactly the head the queue would merge: a committer
# other than GitHub, an unverified commit, or a head that is not the PR's last commit queue nothing.
for mode in dependabot-committer dependabot-unverified dependabot-head-moved; do
	run "$mode" plan; expect automerge '' "$mode"
done
# Numeric ids decide, never logins: the right logins with a wrong committer id, a wrong author id, or an
# alert whose pull request author has Dependabot's login and another id, queue nothing.
for mode in dependabot-author-id alert-impostor; do
	run "$mode" plan; expect automerge '' "$mode"
done
# A refused security PR is alarmed at once, with the PR named, even when nothing else is waiting; a later
# "close" for an idle plan must never erase the alarm.
(FIXTURE_FOREIGN=1 run pending plan); expect automerge "8:$b" "foreign commit"; expect issue open "a foreign commit in a security PR"
case $(out issue_text) in *'#13'*) ;; *) fail 'the alarm does not name the refused pull request' ;; esac
(FIXTURE_FOREIGN=1 run unmerged plan); expect issue open "an alarm survives an idle plan"; expect action none "an alarm does not decide a tag"
run dependabot-committer plan; expect issue open "a wrong committer id"
case $(out issue_text) in *'#8'*) ;; *) fail 'the committer-id alarm does not name the pull request' ;; esac
# A fixed Dependabot alert that no Dependabot pull request is linked to is never "nothing to release".
run unlinked-alert plan; expect issue open "an unlinked fixed alert"
case $(out issue_text) in *'none is linked'*) ;; *) fail 'the unlinked alert is not described' ;; esac
(FIXTURE_PUBLISHED_AT=2026-11-01T00:00:00Z run unlinked-alert plan); expect issue none "an alert fixed before the latest release is not alarmed"
# An unchanged-schema patch passes the compatibility gate end to end (the real comparison script runs
# against a real-shaped declaration), and the scratch database starts only when a tag is decided.
run pending plan; called '^postgres hikyo_release_schema release-schema-fixture' || fail 'a tag decision did not start scratch PostgreSQL'
called 'release download v1.1.0 --repo o/r --dir' || fail 'the baseline release was not fetched'
# The comparison reads the trust of the commit that would be tagged, which is what the build will read.
called 'stable verify --trust [^ ]*/target-tree/release/trust --trust-only' || fail 'the compatibility gate did not use the trust of the tagged commit'
called 'stable verify .*--directory .*--version 1.1.0 .*--published --latest' || fail 'the baseline was not verified as the signed published latest'
run unmerged plan; called '^postgres' && fail 'scratch PostgreSQL started without a tag decision'
(GREEN="$c2 $c1" run pending plan); expect action tag "red tip"; expect commit "$c2" "red tip"
# c1 holds the security merge but its trust predates the latest release: the build would burn the tag.
(GREEN="$c1" run pending plan); expect action none "stale trust at the only green commit"
(GREEN="$c0" run pending plan); expect action none "no green commit"; expect issue open "no green commit stalled"
(GREEN="$c0" FIXTURE_MERGED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) run pending plan); expect issue none "no green commit, recent"
# Nothing to release: the stall issue closes only when there is one to close.
run unmerged plan; expect action none "unmerged"; expect issue none "no stall issue to close"
run unmerged-issue-open plan; expect issue close "an open stall issue is closed"
# A git that cannot answer is a failed read, not "the commit is not on main".
cat >"$scratch/bin/git" <<SH
#!/bin/sh
case "\$*" in *"merge-base --is-ancestor $c3 $c3") [ "\${FIXTURE_MODE:-}" != merge-base-fail ] || exit 2 ;; esac
exec $(command -v git) "\$@"
SH
chmod +x "$scratch/bin/git"
(GIT_BIN="$scratch/bin/git"; export GIT_BIN; expect_dies 'cannot compare the commit with main history' 'a failed merge-base passed for a refusal' run merge-base-fail plan)
no_writes 'plan with a failed merge-base'
# --- plan: every refusal tag() would hit keeps the plan from deciding tag, and alerts after a day.
for mode in higher-tag same-tag-listed not-patch preflight-fail unsynced release-exists compat-diff compat-unverifiable baseline-missing; do
	expect_stall "$mode" none
done
# A stray tag names its recovery, a changed declaration says a maintainer must release.
run higher-tag plan; case $(out reason) in *'stray tag'*'docs/release/online-signing.md'*) ;; *) fail 'the stray tag refusal lacks its recovery pointer' ;; esac
run compat-diff plan; case $(out reason) in *'a maintainer must release this version'*) ;; *) fail 'the compatibility refusal does not say a maintainer must release' ;; esac
# A failed lookup is not a refusal: the run fails, nothing is decided and nothing is written.
for mode in matching-refs-fail release-lookup-fail synced-fail green-read-fail download-fail postgres-fail compat-gen-fail; do
	expect_dies 'cannot' "plan with $mode" run "$mode" plan
	no_writes "plan with $mode"
	grep -q '^issue ' "$scratch/calls" && fail "$mode: touched issues"
done
(DOCS_FAIL=1; export DOCS_FAIL; expect_stall pending wait)
(FALLBACK_FAIL=1; export FALLBACK_FAIL; expect_stall pending wait)
expect_stall docs-running wait
# --- plan: an existing tag decides by who pushed it and how its run ended.
expect_stall run-not-started wait
expect_stall run-prefix-path wait
expect_stall run-other-commit wait
expect_stall build-running wait
expect_stall no-artifact wait
expect_stall expired-artifact wait
run build-ok plan
expect action publish "built"; expect run_id 77 "built"; expect artifact release-manifest-sha256-77-2 "built"
# A tag at a commit whose require-signed-tag.sh predates the actor-based authority is never published here.
(FIXTURE_NO_AUTH=1 run build-ok plan); expect action none "a tag commit without the actor-based authority"; expect issue open "a tag commit without the actor-based authority"
case $(out reason) in *'actor-based tag authority'*) ;; *) fail 'the missing authority has no reason' ;; esac
# The floor: the draft must hold a pending security merge, and every one merged before the build started.
(FIXTURE_SEC_OID=$c4 GITHUB_SHA=$c4 run build-ok plan); expect action none "a draft lacking the only pending merge"; expect issue open "a draft lacking the only pending merge"
case $(out reason) in *'lacks the security merge'*) ;; *) fail "the stale floor has no reason: $(out reason)" ;; esac
# At least one pending merge must be in the draft, even when the missing one raced the tag.
(FIXTURE_SEC_OID=$c4 FIXTURE_MERGED_AT=2026-02-01T00:00:00Z GITHUB_SHA=$c4 run build-ok plan); expect action none "a draft with none of the pending merges"
case $(out reason) in *'contains none of the security merges'*) ;; *) fail "the empty floor has no reason: $(out reason)" ;; esac
(FIXTURE_SEC_OID=$c0 run build-ok plan); expect action none "no security merge is pending"; expect issue open "no security merge is pending"
(FIXTURE_SEC2_OID=$c4 GITHUB_SHA=$c4 run build-ok plan); expect action none "a known merge the draft lacks"
case $(out reason) in *'lacks the security merge'*) ;; *) fail 'the stale floor has no reason' ;; esac
# A merge that raced the tag (inside the grace) is not required, or the immutable tag would strand the draft.
(FIXTURE_SEC2_OID=$c4 FIXTURE_SEC2_AT=2026-02-01T00:00:00Z GITHUB_SHA=$c4 run build-ok plan); expect action publish "a merge that raced the tag"
# A person's release is the ceremony's: no draft of it is published here and no cannot-retry text is given.
expect_stall human-run none
case $(out issue_text) in *'cannot retry'* | *immutable*) fail 'a human build must not get the cannot-retry text' ;; esac
# A person re-running the app's build leaves a draft that nobody can publish: that is its own state,
# alerted at once, with the recovery steps and without the retry advice of a failed build.
run human-rerun plan; expect action none "re-run by a person"; expect issue open "re-run by a person"
case $(out reason) in *'re-run by a person'*) ;; *) fail 'the re-run state has no re-run reason' ;; esac
case $(out issue_text) in *'re-ran its release-build run 77'*'gh release delete v1.1.1 --repo o/r --yes'*'do not re-run an app build'*) ;; *) fail 'the re-run issue lacks the recovery steps' ;; esac
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
for mode in releases-fail alerts-fail pulls-fail commits-fail cask-fail docs-fail runs-fail artifact-fail tag-ref-fail heads-ref-fail authority-fail; do
	expect_dies 'cannot' "plan with $mode" run "$mode" plan
	no_writes "plan with $mode"
done
expect_dies 'cannot list open issues' 'closing with a failed issue listing' run unmerged-issues-fail plan
expect_dies 'tap cask answer has no content' 'a cask answer without content passed' run cask-null plan
expect_dies 'Homebrew cask is empty' 'an empty cask passed for a missing cask' run cask-empty plan
expect_dies 'cannot inspect tags/v1.1.1' 'a failed tag ref lookup passed for "no tag"' run tag-ref-fail plan
expect_dies 'cannot inspect heads/release/sync-trust-1.1.0' 'a failed branch ref lookup passed for "no branch"' run heads-ref-fail plan

# --- plan: the walk back from the tip stops at the commit holding the security merge, and after 60 commits.
mkrepo() { # dir
	mkdir -p "$1/release/trust" "$1/release/repository" "$1/release/compatibility"
	git -C "$1" init -q -b main
	git -C "$1" config user.email test@example.com
	git -C "$1" config user.name test
	git -C "$1" config commit.gpgsign false
	cp "$script_dir/../../release/compatibility/sources.json" "$1/release/compatibility/sources.json"
	printf '{}\n' >"$1/release/repository/fallback-channel-test.json"
	trust_metadata 1.0.0 1.0.0 >"$1/release/trust/metadata.json"
	for name in metadata.sigstore.json catalog.json catalog.sigstore.json; do printf 'old %s\n' "$name" >"$1/release/trust/$name"; done
	git -C "$1" add -A
	git -C "$1" commit -q -m base --no-gpg-sign
}
repo2="$scratch/repo2"
mkrepo "$repo2"
base2=$(git -C "$repo2" rev-parse HEAD)
trust_metadata 1.1.0 1.0.0 1.1.0 >"$repo2/release/trust/metadata.json"
git -C "$repo2" commit -q -a -m older --no-gpg-sign
older=$(git -C "$repo2" rev-parse HEAD)
git -C "$repo2" commit -q --allow-empty -m security --no-gpg-sign
sec=$(git -C "$repo2" rev-parse HEAD)
git -C "$repo2" commit -q --allow-empty -m t1 --no-gpg-sign
git -C "$repo2" commit -q --allow-empty -m t2 --no-gpg-sign
tip2=$(git -C "$repo2" rev-parse HEAD)
repo2_run() { # green-list mode
	(export GREEN="$1" HIKYO_REPO_ROOT="$repo2" GITHUB_SHA="$tip2" FIXTURE_SEC_OID="$sec" FIXTURE_LATEST_COMMIT="$base2"; run "$2" plan)
}
# Only a commit older than the security merge is green and records the latest release: it lacks the fix.
repo2_run "$older" pending; expect action none "an older green commit without the security merge"
called '^postgres' && fail 'scratch PostgreSQL started for a commit without the security merge'
repo2_run "$sec $older" pending; expect action tag "green security commit"; expect commit "$sec" "green security commit"
repo2="$scratch/repo3"
mkrepo "$repo2"
base2=$(git -C "$repo2" rev-parse HEAD)
trust_metadata 1.1.0 1.0.0 1.1.0 >"$repo2/release/trust/metadata.json"
git -C "$repo2" commit -q -a -m security --no-gpg-sign
sec=$(git -C "$repo2" rev-parse HEAD)
i=0
while [ "$i" -lt 64 ]; do git -C "$repo2" commit -q --allow-empty -m "t$i" --no-gpg-sign; i=$((i + 1)); done
tip2=$(git -C "$repo2" rev-parse HEAD)
# Chain (newest first): 64 empty commits, then the security commit as the 65th. The 60th is the last one checked.
at60=$(git -C "$repo2" rev-list --first-parent "$base2..$tip2" | sed -n 60p)
at61=$(git -C "$repo2" rev-list --first-parent "$base2..$tip2" | sed -n 61p)
# The empty commits carry the latest release's trust through their parent, so only green proof varies.
repo2_run "$at60" pending; expect action tag "the 60th commit is inside the cap"; expect commit "$at60" "the 60th commit"
repo2_run "$at61" pending; expect action none "the 61st commit is beyond the cap"
repo2_run "$sec" pending; expect action none "the security commit is beyond the cap"

# --- plan: published releases decide, not GitHub's latest flag; prereleases count for trust.
# A person's prerelease that main's trust never recorded is synchronized, by the app.
(FIXTURE_EXTRA=v1.2.0-rc.1 run pending plan); expect action sync-trust "an unsynced prerelease"; expect version 1.2.0-rc.1 "an unsynced prerelease"
# A trust that already records the prerelease while the latest stable is older is in sync: no false
# "older than trusted" stall, and no unattended patch on top of a prerelease.
git -C "$repo" checkout -q -- release/trust/metadata.json
trust_metadata 1.2.0-rc.1 1.0.0 1.1.0 1.2.0-rc.1 >"$repo/release/trust/metadata.json"
(FIXTURE_EXTRA=v1.2.0-rc.1 FIXTURE_MERGED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) run pending plan)
expect action none "trust records the prerelease"; expect issue none "trust records the prerelease"
case $(out reason) in *'a prerelease'*) ;; *) fail 'the prerelease state has no prerelease reason' ;; esac
case $(out reason) in *'older than'*) fail 'a recorded prerelease was reported as older than trusted' ;; esac
(FIXTURE_EXTRA=v1.2.0-rc.1 run pending plan); expect action none "trust records the prerelease"; expect issue open "a prerelease blocks a waiting security release for a day"
git -C "$repo" checkout -q -- release/trust/metadata.json
# A trust sync that has been outstanding for a day alerts even with no security merge waiting.
(FIXTURE_LATEST=v1.1.1 run unmerged plan); expect action sync-trust "old unsynced release"; expect issue open "an old unsynced release alerts without a pending merge"
(FIXTURE_LATEST=v1.1.1 FIXTURE_PUBLISHED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) run unmerged plan); expect action sync-trust "recent unsynced release"; expect issue none "a recent unsynced release does not alert"

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
tag_refused same-tag-listed 'stable tag at or above v1.1.1 already exists'
tag_refused not-patch 'patch successor'
tag_refused preflight-fail 'does not authenticate'
tag_refused unsynced 'not synchronized'
tag_refused release-exists 'already has a release'
tag_refused matching-refs-fail 'cannot read'
tag_refused release-lookup-fail 'cannot prove the version is unused'
tag_refused green-read-fail 'cannot read the source proof'
tag_refused synced-fail 'cannot list the public releases'
tag_refused no-authority 'actor-based tag authority'
tag_refused authority-fail 'cannot read the tag authority'
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
for mode in human-run human-rerun run-other-commit run-prefix-path build-failed build-running; do
	(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest"; expect_dies 'not a successful release-build' "published a draft for $mode" run "$mode" publish)
	called '^publish' && fail "the publish helper ran for $mode"
done
# The draft is about to become public, so publish rereads the authority and the floor itself.
(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" FIXTURE_NO_AUTH=1; expect_dies 'actor-based tag authority' 'published a draft whose tag commit lacks the authority' run build-ok publish)
(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" FIXTURE_SEC_OID=$c4 GITHUB_SHA=$c4; expect_dies 'is not published' 'published a draft without the pending security merge' run build-ok publish)
(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" FIXTURE_SEC2_OID=$c4 GITHUB_SHA=$c4; expect_dies 'lacks the security merge' 'published a draft that lacks a known merge' run build-ok publish)
(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" FIXTURE_SEC2_OID=$c4 FIXTURE_SEC2_AT=2026-02-01T00:00:00Z GITHUB_SHA=$c4; run build-ok publish) || fail 'a draft that holds one pending merge and races the other was refused'
called '^publish' || fail 'the raced-merge draft was not published'
(export VERSION=1.1.1 RUN_ID=77 DIGEST_FILE="$scratch/digest" FIXTURE_SEC_OID=$c0; expect_dies 'is not published' 'published with no pending security merge' run build-ok publish)
called '^publish' && fail 'the publish helper ran despite a failed floor'
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
# A prerelease is a release trust must record too (a person's prerelease is synchronized by the app),
# so its version passes validation and reaches the release lookup; a malformed version never does.
(export VERSION=1.2.0-rc.1 APP_SLUG=app; expect_dies 'releases/tags/v1.2.0-rc.1' 'a prerelease version was refused before any lookup' run pending sync-trust)
(export VERSION=1.2 APP_SLUG=app; expect_dies 'is required' 'a malformed version was accepted' run pending sync-trust)
# An empty trust file must never become an empty file in a commit.
must_fail 'committed an empty trust file' sync download-empty
grep -q 'is empty' "$scratch/log" || fail 'the empty trust file was not named'
called 'graphql --input' && fail 'a commit was created from an empty trust file'
# An encoder that exits 0 with no output must not become an empty file in a commit either.
mkdir "$scratch/emptyb64"
printf '#!/bin/sh\ncat >/dev/null\n' >"$scratch/emptyb64/base64"
chmod +x "$scratch/emptyb64/base64"
(PATH="$scratch/emptyb64:$PATH"; export PATH; VERSION=1.1.1 APP_SLUG=app expect_dies 'encoded to nothing' 'committed a trust file the encoder turned into nothing' run pending sync-trust)
called 'graphql --input' && fail 'a commit was created from an empty encoding'
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
# Closing and reopening, or replacing, the pull request never restarts the patience.
must_fail 're-armed a reopened PR whose first version is hours old' sync pr-unarmed-reopened
grep -q 'no longer queued' "$scratch/log" || fail 'wrong error for a reopened trust PR'
called 'pr merge' && fail 'armed a reopened stale trust PR'
# A failed contents read must not become a redundant commit.
must_fail 'went on after a failed contents read' sync contents-fail
called 'graphql --input' && fail 'created a commit after a failed contents read'
# Merged trust branches are deleted by the cleanup lane, only for exactly the merged head.
(BRANCH=release/sync-trust-1.1.0 run cleanup-merged cleanup); called 'DELETE repos/o/r/git/refs/heads/release/sync-trust-1.1.0' || fail 'merged branch not deleted'
for mode in cleanup-unmerged cleanup-other-head; do
	(BRANCH=release/sync-trust-1.1.0 run $mode cleanup); called 'DELETE' && fail "deleted a branch for $mode"
done
(export BRANCH=release/other; expect_dies 'invalid trust-sync branch' 'cleaned an arbitrary branch' run pending cleanup)
(export BRANCH=release/sync-trust-notaversion; expect_dies 'invalid trust-sync branch' 'cleaned a branch whose suffix is no version' run pending cleanup)

# --- homebrew: merge once the tap's required check is green, otherwise wait for the next run.
brew() { (VERSION=1.1.1 APP_SLUG=app run "$1" homebrew); }
brew pending; called 'merge_method=squash -f sha=ffff' || fail 'tap PR not squash-merged'
# The squash commit carries the app's DCO sign-off, and so does the helper's own commit through its environment.
called_f 'commit_title=chore: hikyo 1.1.1 (#5)' || fail 'the squash commit has no title'
called_f 'commit_message=Signed-off-by: app[bot] <900+app[bot]@users.noreply.github.com>' || fail 'the squash commit has no bot sign-off'
called_f 'homebrew-helper signoff=app[bot] <900+app[bot]@users.noreply.github.com>' || fail 'the helper did not receive the bot sign-off'
brew tap-pending; called 'merge_method=squash' && fail 'merged a tap PR with a pending check'
# Only the newest run of the check counts: an older success never outranks a newer failure or re-run.
brew tap-newest-failed; called 'merge_method=squash' && fail 'merged a tap PR whose newest check run failed'
brew tap-newest-pending; called 'merge_method=squash' && fail 'merged a tap PR whose newest check run is still pending'
brew tap-id-skew; called 'merge_method=squash' && fail 'merged a tap PR whose newest run by start time failed, because its id was lower'
brew tap-newest-success; called 'merge_method=squash' || fail 'did not merge a tap PR whose newest check run passed'
(unset APP_SLUG; export VERSION=1.1.1; expect_dies 'APP_SLUG' 'homebrew went on without the app identity' run pending homebrew)
(export VERSION=1.1.1 APP_SLUG=app; expect_dies 'cannot read' 'homebrew went on after a failed check-run read' run checks-fail homebrew)
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

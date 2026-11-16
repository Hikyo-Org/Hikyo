#!/bin/sh
# Fixtures for the security patch planner: what the single issue says in every
# state (ready at the tip, ready below a red tip, blocked, drafted, trust behind,
# alarms), what the maintainer's recheck refuses, and that the commands the issue
# prints are real: each ceremony command is checked against the ceremony's own
# argument counts and the tag block is executed in a clone. Every lookup that
# decides something also has a failing case, because a failed read must never
# look like an empty answer.
# shellcheck disable=SC2030,SC2031,SC2016,SC2154
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
real_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
script=$script_dir/security-patch-plan.sh
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-security-patch-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
repo="$scratch/repo"
mkdir -p "$scratch/bin" "$repo/release/trust" "$repo/release/compatibility" "$repo/release/repository" "$repo/scripts/ci"
fail() {
	printf 'security-patch-plan fixture: %s\n' "$*" >&2
	exit 1
}
git -C "$repo" init -q -b main
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
git -C "$repo" config commit.gpgsign false
trust_metadata() { jq -nc --arg highest "$1" --args '{highest_release:$highest, releases:($ARGS.positional | map({version:., sequence:1, manifest_sha256:("a"*64)}))}' "$@"; }
# c0 is v1.1.0, whose trust is synchronized from c2 on; c3 is the security merge,
# c4 an ordinary commit after it. A build at c0 or c1 would read stale trust.
trust_metadata 1.0.0 1.0.0 >"$repo/release/trust/metadata.json"
old_edge='{"source":{"release":{"profile":"stable/v1","version":"1.0.0"}},"mode":"maintenance"}'
printf '{"schema":"hikyo.dev/upgrade-sources/v1","engines":{"sqlite":[{"source":{"genesis":"fresh/v1"}},%s],"postgres":[{"source":{"genesis":"fresh/v1"}},%s]}}\n' "$old_edge" "$old_edge" >"$repo/release/compatibility/sources.json"
printf '{"schema":"fallback"}\n' >"$repo/release/repository/fallback-channel-test.json"
printf '#!/bin/sh\nexit 0\n' >"$repo/scripts/ci/check-fallback-channel-test.sh"
mkdir -p "$repo/internal/store/migrations/postgres" "$repo/scripts/release" "$repo/scripts/lib" "$repo/scripts/git" "$repo/scripts/ci" "$repo/release/repository"
printf -- '-- 1\n' >"$repo/internal/store/migrations/postgres/00001_x.sql"
# The tools the issue's commands run are the real ones, committed in the repository the way the
# target commit would carry them; the hooks installer and the signature check are stand-ins
# because the fixture's commits are neither signed nor in a real clone.
for tool in security-patch-plan.sh require-green-main.sh require-synced-trust.sh; do cp "$real_root/scripts/release/$tool" "$repo/scripts/release/$tool"; done
cp "$real_root/scripts/lib/release.sh" "$repo/scripts/lib/release.sh"
printf '#!/bin/sh\nexit 0\n' >"$repo/scripts/git/install-hooks.sh"
printf '#!/bin/sh\nprintf "signature check %%s\\n" "$*" >>"${FIXTURE:-/dev/null}/signature-calls"\nexit "${SIGNATURES:-0}"\n' >"$repo/scripts/ci/check-commit-signatures.sh"
printf '#!/bin/sh\nexit 0\n' >"$repo/scripts/release/ceremony.sh"
chmod +x "$repo"/scripts/release/*.sh "$repo/scripts/git/install-hooks.sh" "$repo/scripts/ci/check-commit-signatures.sh" "$repo/scripts/ci/check-fallback-channel-test.sh"
git -C "$repo" add -A
git -C "$repo" commit -q -m c0
git -C "$repo" commit -q --allow-empty -m c1
trust_metadata 1.1.0 1.0.0 1.1.0 >"$repo/release/trust/metadata.json"
git -C "$repo" commit -q -a -m c2
git -C "$repo" commit -q --allow-empty -m c3
git -C "$repo" commit -q --allow-empty -m c4
c0=$(git -C "$repo" rev-parse HEAD~4) c1=$(git -C "$repo" rev-parse HEAD~3) c2=$(git -C "$repo" rev-parse HEAD~2)
c3=$(git -C "$repo" rev-parse HEAD~1) c4=$(git -C "$repo" rev-parse HEAD)
i=0
while [ "$i" -lt 62 ]; do
	git -C "$repo" commit -q --allow-empty -m "later $i"
	i=$((i + 1))
done
long_tip=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" reset -q --hard "$c4"
git -C "$repo" update-ref refs/remotes/origin/main "$c4"
# side holds a security merge that main reaches only through a merge commit (not first-parent);
# m is that merge, x a commit on top of main that main does not contain, cbad one with unreadable trust.
git -C "$repo" switch -q --detach "$c0"
git -C "$repo" commit -q --allow-empty -m side
side=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$c4"
git -C "$repo" merge -q --no-ff -m merge "$side"
m=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$c4"
git -C "$repo" commit -q --allow-empty -m x
x=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$c4"
printf 'not json\n' >"$repo/release/trust/metadata.json"
git -C "$repo" commit -q -a -m cbad
cbad=$(git -C "$repo" rev-parse HEAD)
# ctool changes a release tool the issue's commands run, cother only an unrelated file, cmig adds a migration.
git -C "$repo" switch -q --detach "$c4"
printf '# changed\n' >>"$repo/scripts/release/require-green-main.sh"
git -C "$repo" commit -q -a -m ctool
ctool=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$c4"
printf 'x\n' >"$repo/unrelated.txt"
git -C "$repo" add -A
git -C "$repo" commit -q -m cother
cother=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$c4"
printf -- '-- 2\n' >"$repo/internal/store/migrations/postgres/00002_y.sql"
git -C "$repo" add -A
git -C "$repo" commit -q -m cmig
cmig=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q --detach "$c4"
mkdir -p "$repo/release/compatibility"
jq -c '.changed = 1' "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" && cp "$scratch/sources.tmp" "$repo/release/compatibility/sources.json"
git -C "$repo" add -A
git -C "$repo" commit -q -m ccomp
ccomp=$(git -C "$repo" rev-parse HEAD)
# cedge adds only the reviewed release edge per engine; cedgeplus adds a genesis edge besides.
git -C "$repo" switch -q --detach "$c4"
jq -c '.engines |= map_values(. + [{source:{release:{profile:"stable/v1",version:"1.1.0"}},mode:"maintenance"}])' "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" && cp "$scratch/sources.tmp" "$repo/release/compatibility/sources.json"
git -C "$repo" add -A
git -C "$repo" commit -q -m cedge
cedge=$(git -C "$repo" rev-parse HEAD)
jq -c '.engines.sqlite += [{source:{genesis:"legacy/v1"}}]' "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" && cp "$scratch/sources.tmp" "$repo/release/compatibility/sources.json"
git -C "$repo" add -A
git -C "$repo" commit -q -m cedgeplus
cedgeplus=$(git -C "$repo" rev-parse HEAD)
# A patch may add the one edge from the latest release and nothing else: an older edge that is removed
# or changed, or a second edge naming the latest release, is a change a minor must carry.
new_edge='{source:{release:{profile:"stable/v1",version:"1.1.0"}},mode:"maintenance"}'
for variant in removed swapped double stringrel mixed; do
	git -C "$repo" switch -q --detach "$c4"
	case $variant in
	removed) jq -c ".engines |= map_values(map(select(.source.release.version? != \"1.0.0\")) + [$new_edge])" "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" ;;
	swapped) jq -c ".engines |= map_values(map(if .source.release.version? == \"1.0.0\" then .mode = \"restart\" else . end) + [$new_edge])" "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" ;;
	stringrel) jq -c '.engines |= map_values(. + [{source:{release:"1.1.0"},mode:"maintenance"}])' "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" ;;
	mixed) jq -c '.engines |= map_values(. + [{source:{release:{profile:"stable/v1",version:"1.1.0"},genesis:"fresh/v1"},mode:"maintenance"}])' "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" ;;
	double) jq -c ".engines |= map_values(. + [$new_edge]) | .engines.sqlite += [{source:{release:{profile:\"stable/v1\",version:\"1.1.0\",sequence:9}},mode:\"maintenance\"}]" "$repo/release/compatibility/sources.json" >"$scratch/sources.tmp" ;;
	esac
	cp "$scratch/sources.tmp" "$repo/release/compatibility/sources.json"
	git -C "$repo" add -A
	git -C "$repo" commit -q -m "c$variant"
	eval "c$variant=\$(git -C \"\$repo\" rev-parse HEAD)"
done
git -C "$repo" switch -q main
git -C "$repo" reset -q --hard "$c4"
export FIXTURE="$scratch" C0="$c0" C1="$c1" C2="$c2" C3="$c3" C4="$c4"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
export NOW

cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE/calls"
args="$*"
boom() { printf 'gh: server error (HTTP 500)\n' >&2; exit 1; }
failing() { case ",${FAIL:-}," in *",$1,"*) boom ;; esac; }
hex() { printf "$1%.0s" $(seq 40); }
case "$*" in
	'api --paginate --slurp repos/o/r/releases?per_page=100')
		failing releases
		# The planning token cannot list drafts, the maintainer's login can: REL_DRAFT is seen by
		# both, REL_DRAFT_RECHECK only by the recheck (FIXTURE_VIEW=recheck).
		drafts=${REL_DRAFT:-}
		[ "${FIXTURE_VIEW:-}" != recheck ] || drafts="$drafts ${REL_DRAFT_RECHECK:-}"
		jq -nc --arg extra "${REL_EXTRA:-}" --arg draft "$drafts" '
			[("v1.1.0", "v1.0.0", (if $extra != "" then ($extra | split(" ")[]) else empty end), "v9.9.9-nightly.20260101.1.gabcdef12")] |
			map({tag_name: ., draft: false, published_at: "2026-11-08T23:26:41Z"}) +
			[{tag_name: "v8.0.0", draft: true, published_at: null}] +
			($draft | split(" ") | map(select(. != "") | {tag_name: ., draft: true, published_at: null})) | [.]' ;;
	'api repos/o/r/commits/v1.1.0') failing latest-commit; jq -nc --arg sha "$C0" '{sha:$sha}' ;;
	'api repos/o/r/commits/v1.1.1'|'api repos/o/r/commits/v1.1.2'|'api repos/o/r/commits/v1.2.0'|'api repos/o/r/commits/v1.3.0') failing tag-commit; jq -nc --arg sha "${TAG_COMMIT:-$C4}" '{sha:$sha}' ;;
	'api graphql --paginate '*)
		failing alerts
		case "$*" in *'states:[OPEN,FIXED]'*) ;; *) printf 'the alert query must list its states explicitly\n' >&2; exit 2 ;; esac
		case "$*" in *'pageInfo{hasNextPage endCursor}'*) ;; *) printf 'the alert query must select pageInfo\n' >&2; exit 2 ;; esac
		[ "${ALERTS:-}" != null ] || { echo '{"data":{"repository":{"vulnerabilityAlerts":null}}}'; exit 0; }
		[ "${ALERTS:-}" != empty ] || { echo '{"data":{"repository":{"vulnerabilityAlerts":{"nodes":[]}}}}'; exit 0; }
		bot=49699333
		[ "${ALERTS:-}" != impostor ] || bot=1
		oid=${SEC1:-$C3}
		merged=true
		[ "${ALERTS:-}" != unmerged ] || merged=false
		jq -nc --arg oid "$oid" --argjson merged "$merged" --arg at "${MERGED_AT:-$NOW}" --argjson bot "$bot" --arg open "${OPEN_PRS:-}" \
			--arg oid2 "${SEC2:-}" --arg pfix "${PR_FIXED_AT:-$NOW}" --arg at2 "${MERGED_AT2:-$NOW}" --arg nocommit "${NO_COMMIT:-}" --arg mode "${ALERTS:-}" --arg fixed "${FIXED_AT:-$NOW}" --arg decoys "${DECOYS:-}" '
			def pr($n; $m; $oid; $at; $ref): {state:(if $m then "FIXED" else "OPEN" end),fixedAt:(if $m then $pfix else null end),
				dependabotUpdate:{pullRequest:{number:$n,title:"Bump x",state:(if $m then "MERGED" else "OPEN" end),merged:$m,mergedAt:(if $m then $at else null end),baseRefName:"main",headRefName:$ref,
				author:{login:"dependabot",databaseId:$bot},mergeCommit:(if $m and $nocommit == "" then {oid:$oid} else null end)}}};
			{data:{repository:{vulnerabilityAlerts:{nodes:([
				pr(7; $merged; $oid; $at; "dependabot/npm_and_yarn/x"),
				{state:"OPEN",fixedAt:null,dependabotUpdate:null}
			] + (if $decoys != "" then [
				(pr(9; true; "9999999999999999999999999999999999999999"; $at; "dependabot/go/dev") | .dependabotUpdate.pullRequest.baseRefName = "dev"),
				pr(13; true; "7777777777777777777777777777777777777777"; $at; "feature/z"),
				(pr(10; true; "8888888888888888888888888888888888888888"; $at; "dependabot/go/other") | .dependabotUpdate.pullRequest.author.databaseId = 2)
			] else [] end) + (if $oid2 != "" then [pr(11; true; $oid2; $at2; "dependabot/go/second")] else [] end)
			+ ($open | split(" ") | map(select(. != "") | tonumber | pr(.; false; null; $at; "dependabot/go/open")))
			+ (if $mode == "unlinked" then [{state:"FIXED",fixedAt:$fixed,dependabotUpdate:null}] else [] end)
			+ (if $mode == "unlinked-unmerged" then [{state:"FIXED",fixedAt:$fixed,dependabotUpdate:{pullRequest:pr(12; false; null; $at; "dependabot/go/u").dependabotUpdate.pullRequest}}] else [] end)
			+ (if $mode == "unlinked-nofixedat" then [{state:"FIXED",fixedAt:null,dependabotUpdate:null}] else [] end)
			+ (if $mode == "old-unlinked" then [{state:"FIXED",fixedAt:"2026-10-01T00:00:00Z",dependabotUpdate:null}] else [] end))}}}}' ;;
	'api repos/o/r/dependabot/alerts?state=open,fixed&per_page=1')
		case "${REST:-ok}" in
			ok) echo '[]' ;;
			forbidden) printf 'gh: Resource not accessible by integration (HTTP 403)\n' >&2; exit 1 ;;
			*) echo '[{"number":1}]' ;;
		esac ;;
	'api repos/o/r/actions/workflows/release.yml/runs?event=push&branch=v'*'&per_page=20')
		failing runs
		tag=${args#*branch=}; tag=${tag%%&*}
		case "${BUILD:-none}" in
			failed) jq -nc --arg tag "$tag" --arg url "${RUN_URL:-https://github.com/o/r/actions/runs/4242}" '{workflow_runs:[{head_branch:$tag,event:"push",html_url:$url,status:"completed",conclusion:"failure"}]}' ;;
			running) jq -nc --arg tag "$tag" --arg url "${RUN_URL:-https://github.com/o/r/actions/runs/4242}" '{workflow_runs:[{head_branch:$tag,event:"push",html_url:$url,status:"in_progress",conclusion:null}]}' ;;
			success) jq -nc --arg tag "$tag" --arg url "${RUN_URL:-https://github.com/o/r/actions/runs/4242}" '{workflow_runs:[{head_branch:$tag,event:"push",html_url:$url,status:"completed",conclusion:"success"}]}' ;;
			rerun) jq -nc --arg tag "$tag" --arg url "${RUN_URL:-https://github.com/o/r/actions/runs/4242}" '{workflow_runs:[{head_branch:$tag,event:"push",html_url:$url,status:"completed",conclusion:"failure"},{head_branch:$tag,event:"push",html_url:$url,status:"completed",conclusion:"success"}]}' ;;
			noarray) echo '{"workflow_runs":null}' ;;
			*) echo '{"workflow_runs":[]}' ;;
		esac ;;
	'api --paginate repos/o/r/git/matching-refs/tags/v')
		failing matching-refs
		jq -nc --arg extra "${TAGS_EXTRA:-}" '[{ref:"refs/tags/v1.1.0"},{ref:"refs/tags/v-ruleset-probe"},{ref:"refs/tags/v1.2.0-nightly.20261112.1.gabcdef12"}] +
			(if $extra != "" then ($extra | split(" ") | map({ref: ("refs/tags/" + .)})) else [] end)' ;;
	'api repos/o/r/releases/tags/v1.1.1'|'api repos/o/r/releases/tags/v1.1.2')
		failing release-lookup
		if [ "${RELEASE_EXISTS:-}" = 1 ]; then echo '{"draft":false}'; else printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; fi ;;
	'api repos/o/r/releases/tags/v1.2.0'|'api repos/o/r/releases/tags/v1.3.0')
		failing release-lookup
		if [ "${MINOR_RELEASE_EXISTS:-}" = 1 ]; then echo '{"draft":false}'; else printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; fi ;;
	'api --paginate --slurp repos/o/r/issues?state=open&creator=github-actions%5Bbot%5D&per_page=100')
		failing issue-list
		cat "$FIXTURE/issues.json" ;;
	'api graphql -f query='*)
		# One open security pull request, read by number.
		failing open-pr
		n=${args##*-F number=}
		head=$(printf '%040x' "$n")
		bot=49699333; draft=false; repo=o/r; queue=false; events='[]'
		case " ${PR_BOT:-} " in *" $n "*) bot=7 ;; esac
		case " ${PR_DRAFT:-} " in *" $n "*) draft=true ;; esac
		case " ${PR_FOREIGN:-} " in *" $n "*) repo=fork/r ;; esac
		case " ${PR_INQUEUE:-} " in *" $n "*) queue=true ;; esac
		case " ${PR_REMOVED:-} " in *" $n "*) events='[{"createdAt":"2026-11-12T10:00:00Z"}]' ;; esac
		case " ${PR_REMOVED_BEFORE:-} " in *" $n "*) events='[{"createdAt":"2026-11-01T10:00:00Z"}]' ;; esac
		jq -nc --argjson n "$n" --arg head "$head" --argjson bot "$bot" --argjson draft "$draft" --arg repo "$repo" --argjson queue "$queue" --argjson events "$events" --arg url "${TITLE_URL:-}" '
			{data:{repository:{pullRequest:{number:$n,title:("Bump `x` from 1 to 2 @someone\n<b>" + ($n | tostring) + (if $url == "1" then " see https://evil.example/x ::////" elif $url == "www" then " see wwwww.w.evil.example/x" else "" end)),state:"OPEN",isDraft:$draft,isInMergeQueue:$queue,baseRefName:"main",headRefOid:$head,
			headRepository:{nameWithOwner:$repo},author:{login:"dependabot",databaseId:$bot},timelineItems:{nodes:$events}}}}}' ;;
	'api --paginate --slurp repos/o/r/pulls/'*'/commits?per_page=100')
		failing open-pr
		n=${args#*pulls/}; n=${n%%/*}
		head=$(printf '%040x' "$n")
		author=49699333; committer=19864447; verified=true
		case " ${PR_COMMITS_UNVERIFIED:-} " in *" $n "*) verified=false ;; esac
		case " ${PR_COMMITS_HUMAN:-} " in *" $n "*) author=5 ;; esac
		case " ${PR_COMMITS_NOT_WEBFLOW:-} " in *" $n "*) committer=5 ;; esac
		case " ${PR_COMMITS_STALE:-} " in *" $n "*) head=$(printf '%040x' 999) ;; esac
		jq -nc --arg head "$head" --argjson author "$author" --argjson committer "$committer" --argjson verified "$verified" '
			[[{sha:$head,author:{id:$author},committer:{id:$committer},commit:{committer:{date:"2026-11-10T10:00:00Z"},verification:{verified:$verified}}}]]' ;;
	'api --paginate --slurp repos/o/r/commits/'*'/check-runs?check_name=ci-queue-required&app_id=15368&per_page=100')
		failing open-pr
		n=${args#*commits/}; n=${n%%/*}
		n=$((0x$n))
		runs='[]'
		case " ${PR_ADMISSION_FAILED:-} " in *" $n "*) runs='[{"id":5,"status":"completed","conclusion":"failure"}]' ;; esac
		case " ${PR_ADMISSION_QUEUED:-} " in *" $n "*) runs='[{"id":5,"status":"queued","conclusion":null}]' ;; esac
		case " ${PR_ADMISSION_OLD_FAILED:-} " in *" $n "*) runs='[{"id":5,"status":"completed","conclusion":"failure"},{"id":9,"status":"completed","conclusion":"success"}]' ;; esac
		case " ${PR_ADMISSION_NEW_FAILED:-} " in *" $n "*) runs='[{"id":5,"status":"completed","conclusion":"success"},{"id":9,"status":"completed","conclusion":"failure"}]' ;; esac
		jq -nc --argjson runs "$runs" '[{total_count:($runs | length),check_runs:$runs}]' ;;
	'issue create '*) : >"$FIXTURE/created"; cp "${args##*--body-file }" "$FIXTURE/last-body" ;;
	'issue edit '*) : >"$FIXTURE/edited"; cp "${args##*--body-file }" "$FIXTURE/last-body" ;;
	'issue close '*) : >"$FIXTURE/closed" ;;
	*) printf 'unexpected gh call: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$scratch/bin/go" <<'SH'
#!/bin/sh
set -eu
printf 'go %s\n' "$*" >>"$FIXTURE/calls"
out=
prev=
for arg; do [ "$prev" != --out ] || out=$arg; prev=$arg; done
case "$*" in
	*'stable preflight '*) [ "${STABLE:-}" != untrusted ] || exit 1 ;;
	# Like the real writers (O_EXCL): an output file that exists is an error.
	*'stable candidate '*) [ "${STABLE:-}" != badcandidate ] || exit 1; [ ! -e "$out" ] || { printf 'open %s: file exists\n' "$out" >&2; exit 1; }; printf '{}\n' >"$out" ;;
	*'release/compatibility '*) [ "${STABLE:-}" != nodecl ] || exit 1; [ ! -e "$out" ] || { printf 'open %s: file exists\n' "$out" >&2; exit 1; }; printf '{}\n' >"$out" ;;
	*) printf 'unexpected go call: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$scratch/bin/green" <<'SH'
#!/bin/sh
printf 'green %s\n' "$*" >>"$FIXTURE/calls"
[ "${GREEN_READ:-}" != fail ] || { echo 'green main: could not read GitHub'; exit 4; }
case " ${GREEN:-$C0 $C1 $C2 $C3 $C4} " in *" $2 "*) exit 0 ;; esac
echo 'green main: needs complete authenticated source proof'
exit 1
SH
cat >"$scratch/bin/synced" <<'SH'
#!/bin/sh
case "${SYNCED:-ok}" in ok) exit 0 ;; behind) exit 1 ;; *) exit 2 ;; esac
SH
# The real trust helper, once with a gh that cannot read the releases at all: that is a failed read
# (exit 2, the run fails), never the helper's own "not synchronized" answer (exit 1, a blocked plan).
cat >"$scratch/bin/gh-down" <<'SH'
#!/bin/sh
printf 'gh: server error (HTTP 500)\n' >&2
exit 1
SH
cat >"$scratch/bin/synced-down" <<'SH'
#!/bin/sh
GH_BIN="$FIXTURE/bin/gh-down" exec "$REAL_ROOT/scripts/release/require-synced-trust.sh" "$@"
SH
cat >"$scratch/bin/synced-real" <<'SH'
#!/bin/sh
exec "$REAL_ROOT/scripts/release/require-synced-trust.sh" "$@"
SH
cat >"$scratch/bin/compat" <<'SH'
#!/bin/sh
printf 'compat %s\n' "$*" >>"$FIXTURE/calls"
if [ "${1:-}" = --edges-only ]; then
	case "${EDGES:-ok}" in
		ok) echo 'compatibility check: every engine carries its sound release-source edge from v1.1.0'; exit 0 ;;
		missing) printf '%s\n' 'compatibility check: sqlite: no release-source edge from v1.1.0' 'compatibility check: postgres: no release-source edge from v1.1.0' 'compatibility check: the release-source edge from v1.1.0 is missing or does not match what v1.1.0 published; add the reviewed edge per engine' >&2; exit 1 ;;
		minor-missing) if [ "${4:-}" = 1.1.1 ]; then echo 'compatibility check: every engine carries its sound release-source edge from v1.1.0'; exit 0; fi
			printf '%s\n' 'compatibility check: sqlite: no release-source edge from v1.1.0' 'compatibility check: the release-source edge from v1.1.0 is missing or does not match what v1.1.0 published; add the reviewed edge per engine' >&2; exit 1 ;;
		unverified) echo 'compatibility check: v1.1.0 does not verify against the signed trust' >&2; exit 5 ;;
		*) echo 'compatibility check: cannot download v1.1.0' >&2; exit 4 ;;
	esac
fi
case "${COMPAT:-same}" in
	same) echo 'compatibility check: compatibility is identical to v1.1.0' ;;
	differs) echo 'compatibility check: the compatibility declaration differs from the one v1.1.0 published' >&2; exit 1 ;;
	bigdiff) { i=1; while [ "$i" -le 300 ]; do echo "+line $i"; i=$((i + 1)); done; echo 'compatibility check: the compatibility declaration differs from the one v1.1.0 published'; } >&2; exit 1 ;;
	unverified) echo 'compatibility check: v1.1.0 does not verify against the signed trust' >&2; exit 5 ;;
	incomplete) echo 'compatibility check: the candidate declaration is not a complete two-engine declaration' >&2; exit 1 ;;
	earlier-line) { echo '+  "note": "the compatibility declaration differs from the one v1.1.0 published"'; echo 'compatibility check: the candidate declaration is not a complete two-engine declaration'; } >&2; exit 1 ;;
	minorbad) if [ "${3:-}" = 1.1.1 ]; then echo 'compatibility check: the compatibility declaration differs from the one v1.1.0 published' >&2; exit 1; fi
		echo 'compatibility check: the candidate declaration is not a complete two-engine declaration' >&2; exit 1 ;;
	*) echo 'compatibility check: cannot download v1.1.0' >&2; exit 4 ;;
esac
SH
cat >"$scratch/bin/fallback" <<'SH'
#!/bin/sh
printf 'fallback %s\n' "$*" >>"$FIXTURE/calls"
case "${FALLBACK:-ok}" in
	ok) echo 'fallback channel gate: recent end-to-end notification receipt passed' ;;
	stale) echo 'fallback channel gate: notification test is older than one quarter' >&2; exit 1 ;;
	*) echo 'sh: node: not found' >&2; exit 127 ;;
esac
SH
# Like start-ci-postgres.sh: it refuses to replace an existing container and leaves its own running.
cat >"$scratch/bin/postgres" <<'SH'
#!/bin/sh
[ ! -e "$FIXTURE/pg-container" ] || { echo 'CI PostgreSQL refuses to replace an existing container' >&2; exit 1; }
: >"$FIXTURE/postgres-started"
[ "${POSTGRES:-}" != fail ] || exit 1
: >"$FIXTURE/pg-container"
echo start >>"$FIXTURE/pg-log"
SH
cat >"$scratch/bin/postgres-stop" <<'SH'
#!/bin/sh
rm -f "$FIXTURE/pg-container"
echo stop >>"$FIXTURE/pg-log"
SH
chmod +x "$scratch/bin/"*
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go" GREEN_BIN="$scratch/bin/green" SYNCED_BIN="$scratch/bin/synced" \
	COMPAT_BIN="$scratch/bin/compat" POSTGRES_BIN="$scratch/bin/postgres" FALLBACK_BIN="$scratch/bin/fallback" POSTGRES_STOP_BIN="$scratch/bin/postgres-stop" HIKYO_REPO_ROOT="$repo"
echo '[[]]' >"$scratch/issues.json"

reset() { rm -f "$scratch/last-body" "$scratch/signature-calls" "$scratch/calls" "$scratch/postgres-started" "$scratch/pg-container" "$scratch/pg-log" "$scratch/created" "$scratch/edited" "$scratch/closed" "$scratch/plan.json"; : >"$scratch/calls"; }
# plan NAME WANT_STATUS VAR=value...: run the plan against GITHUB_SHA (default c4).
plan() {
	name=$1 want=$2
	shift 2
	reset
	status=0
	env GH_REPO=o/r GH_TOKEN=t GITHUB_SHA="$c4" PLAN_OUT="$scratch/plan.json" "$@" "$script" plan >"$scratch/out" 2>"$scratch/err" || status=$?
	[ "$status" = "$want" ] || { cat "$scratch/out" "$scratch/err" >&2; fail "$name: exit $status, want $want"; }
}
action() { jq -r '.action' "$scratch/plan.json"; }
body() { jq -r '.body' "$scratch/plan.json"; }
expect_action() { [ "$(action)" = "$2" ] || { body >&2; fail "$1: action $(action), want $2"; }; }
has() { body | grep -F -- "$2" >/dev/null || { body >&2; fail "$1: the issue lacks: $2"; }; }
lacks() { if body | grep -F -- "$2" >/dev/null; then body >&2; fail "$1: the issue must not contain: $2"; fi; }
no_tag_commands() { lacks "$1" 'git tag -s'; lacks "$1" 'ceremony.sh build'; }

# --- the ready issue and its commands --------------------------------------
# The workflow leaves GH_BIN at its default, a bare gh found on PATH.
mkdir -p "$scratch/gh-on-path"
ln -sf "$scratch/bin/gh" "$scratch/gh-on-path/gh"
plan 'the default GH_BIN runs gh from PATH' 0 GH_BIN=gh PATH="$scratch/gh-on-path:$PATH"
expect_action 'default GH_BIN' open
plan 'ready at the green tip' 0
expect_action ready open
has ready '## Security patch v1.1.1 is ready to tag'
has ready "merge commit \`$c3\`"
has ready "Planned release:** \`v1.1.1\` at \`$c4\`"
has ready "scripts/release/security-patch-plan.sh recheck patch 1.1.1 $c4 --tip &&"
has ready 'scripts/release/ceremony.sh build 1.1.1'
lacks ready 'git tag -s'
has ready "identical to v1.1.0's apart from that edge"
has ready 'the release-source edge from v1.1.0 is sound'
has ready 'the fallback channel gate passes'
has ready 'Nothing has been tagged or published'
[ -e "$scratch/postgres-started" ] || fail 'a ready plan runs the compatibility generator on scratch PostgreSQL'
grep -F "compat o/r " "$scratch/calls" | grep -F ' 1.1.1 ' >/dev/null || fail 'the compatibility script must be told the version'
lacks ready 'waited more than 24 hours'
has ready 'REVIEWED_MANIFEST_SHA256'
# The tag block and the ceremony lines are real: argument counts come from the ceremony's own usage checks.
check_ceremony_lines() { # name
	body | sed -n 's/^.*scripts\/release\/ceremony.sh \([a-z-]*\)\(.*\)$/\1\2/p' | sed 's/ *&&$//' >"$scratch/ceremony-lines"
	[ -s "$scratch/ceremony-lines" ] || fail "$1: no ceremony command found"
	# Each follow-up command runs from a fresh origin/main: the one before it may have left another branch.
	body | grep -E 'ceremony\.sh (review|publish|sync-trust|homebrew) ' >"$scratch/followup-lines" || true
	while read -r line; do
		case $line in
		'git fetch origin main && git switch --detach origin/main && scripts/release/ceremony.sh '*) ;;
		*) fail "$1: a follow-up ceremony command does not start from a fresh origin/main: $line" ;;
		esac
	done <"$scratch/followup-lines"
	while read -r sub args; do
		# shellcheck disable=SC2086
		set -- $args
		given=$#
		want=$(awk -v sub_="$sub" 'index($0, "usage: ceremony.sh " sub_ " ") { match($0, /-eq [0-9]+/); print substr($0, RSTART + 4, RLENGTH - 4) }' "$script_dir/stable-ceremony.sh")
		[ -n "$want" ] || fail "$1: the ceremony has no command $sub"
		[ "$((given + 1))" = "$want" ] || fail "$1: ceremony.sh $sub takes $((want - 1)) arguments, the issue gives $given"
	done <"$scratch/ceremony-lines"
	for path in scripts/release/ceremony.sh scripts/release/security-patch-plan.sh scripts/git/install-hooks.sh; do
		[ -x "$real_root/$path" ] || fail "$1: the issue names $path, which is not an executable file"
	done
}
check_ceremony_lines 'ready at the tip'
for sub in review publish sync-trust homebrew; do has ready "scripts/release/ceremony.sh $sub 1.1.1"; done

# Run the tag block for real in a clone, with the fake tools: its first lines
# (fetch, switch, the recheck) are executed; only the final tag/ceremony line is
# replaced because it needs a signing key or asks for confirmation.
run_tag_block() { # name mode [status] (last-line: swap the final line for true; sign: tag without a key; status: fail when the block must stop)
	body | awk '/^```sh/ { n++; on = (n == 1); next } /^```/ { on = 0; next } on' >"$scratch/block.sh"
	clone="$scratch/clone-$1"
	git clone -q "$repo" "$clone"
	# The signature check reads the canonical remote's main; a maintainer's clone has it (CONTRIBUTING.md).
	git -C "$clone" remote add hikyo-upstream "$repo"
	git -C "$clone" config user.email test@example.com
	git -C "$clone" config user.name test
	# The block runs the tools its target commit carries, as it would for a maintainer: no overlay.
	python3 -I - "$scratch/block.sh" "$2" <<'PY'
import sys
path, mode = sys.argv[1:3]
lines = open(path).read().rstrip("\n").split("\n")
if mode == "last-line":
    lines[-1] = "  true"
else:
    lines = [l.replace("git tag -s ", "git tag -a ") for l in lines]
open(path, "w").write("\n".join(lines) + "\n")
PY
	block_status=0
	(cd "$clone" && env HIKYO_REPO_ROOT="$clone" GH_REPO=o/r sh -e "$scratch/block.sh" >"$scratch/block.out" 2>&1) || block_status=$?
	if [ "${3:-}" = fail ]; then
		[ "$block_status" != 0 ] || { cat "$scratch/block.out" >&2; fail "$1: the tag block must stop"; }
	else
		[ "$block_status" = 0 ] || { cat "$scratch/block.out" >&2; fail "$1: the tag block does not run"; }
		grep -F "ok to tag v${BLOCK_VERSION:-1.1.1} at" "$scratch/block.out" >/dev/null || { cat "$scratch/block.out" >&2; fail "$1: the recheck did not pass"; }
	fi
	rm -rf "$clone"
}
run_tag_block tip last-line

# A red tip: the same commit, tagged by hand because the ceremony tags only the tip.
plan 'ready below a red tip' 0 GREEN="$c0 $c1 $c2 $c3"
expect_action 'red tip' open
has 'red tip' "\`v1.1.1\` at \`$c3\`"
has 'red tip' "git switch --detach $c3 &&"
has 'red tip' "recheck patch 1.1.1 $c3 &&"
has 'red tip' 'scripts/ci/check-commit-signatures.sh origin/main HEAD &&'
has 'red tip' 'is never moved or recreated'
has 'red tip' 'use a new version'
has 'red tip' "git tag -s v1.1.1 -m \"Hikyo 1.1.1\" $c3 &&"
has 'red tip' 'git push origin refs/tags/v1.1.1'
has 'red tip' 'scripts/git/install-hooks.sh &&'
has 'red tip' "the main tip $c4 is not green"
lacks 'red tip' 'ceremony.sh build'
lacks 'red tip' ' --tip'
check_ceremony_lines 'red tip'
# The commits being tagged are checked for signatures before the tag is pushed; a failure stops the push.
SIGNATURES=1
export SIGNATURES
run_tag_block red-unsigned sign fail
unset SIGNATURES
if git -C "$repo" rev-parse -q --verify refs/tags/v1.1.1 >/dev/null; then fail 'a failed signature check must stop the tag push'; fi
body | awk '/check-commit-signatures/ { sig = NR } /git push origin refs\/tags/ { push = NR } END { exit !(sig > 0 && push > sig) }' || fail 'the signature check must come before the tag push'
run_tag_block red sign
grep -F 'origin/main HEAD' "$scratch/signature-calls" >/dev/null || fail 'the red-tip block must check the signatures of what it pushes'
[ "$(git -C "$repo" rev-parse 'v1.1.1^{commit}')" = "$c3" ] || fail 'the red-tip block must push the tag at the planned commit'
git -C "$repo" tag -d v1.1.1 >/dev/null
# Below the tip the commands run the release tools from the target's tree: when the tip changed one, no tag is offered.
plan 'the release tools differ between the target and a red tip' 0 GITHUB_SHA="$ctool" GREEN="$c0 $c1 $c2 $c3 $c4"
expect_action 'tools differ' open
has 'tools differ' 'the release tools differ between it and the main tip'
no_tag_commands 'tools differ'
plan 'an unrelated change on a red tip leaves the tools alone' 0 GITHUB_SHA="$cother" GREEN="$c0 $c1 $c2 $c3 $c4"
has 'unrelated tip change' "\`v1.1.1\` at \`$c4\`"
has 'unrelated tip change' 'git tag -s'
plan 'a green tip with changed tools is still tagged by the ceremony' 0 GITHUB_SHA="$ctool" GREEN="$c0 $c1 $c2 $c3 $c4 $ctool"
has 'green tip' 'ceremony.sh build 1.1.1'

# --- blocked states open the issue instead of staying silent -----------------
plan 'no green commit at or after the merge' 0 GREEN="$c0 $c1 $c2"
expect_action 'no green' open
has 'no green' 'cannot be prepared'
has 'no green' 'complete green source proof'
has 'no green' "merge commit \`$c3\`"
no_tag_commands 'no green'
plan 'a newer merge makes an older commit useless' 0 SEC2="$c4" GREEN="$c3"
expect_action 'newer merge' open
has 'newer merge' 'cannot be prepared'
no_tag_commands 'newer merge'
plan 'two merges: the commit holding both is chosen' 0 SEC2="$c4"
has 'two merges' "\`v1.1.1\` at \`$c4\`"
has 'two merges' "merge commit \`$c4\`"
plan 'the 60 commit cap' 0 GITHUB_SHA="$long_tip" GREEN="$c2 $c3 $c4"
expect_action 'cap' open
has 'cap' 'cannot be prepared'
no_tag_commands 'cap'
plan 'the 60th commit is still examined' 0 GITHUB_SHA="$long_tip" GREEN="$(git -C "$repo" rev-list --first-parent "$c4..$long_tip" | sed -n '60p')"
has 'inside cap' 'is ready to tag'
plan 'the 61st commit is not' 0 GITHUB_SHA="$long_tip" GREEN="$(git -C "$repo" rev-list --first-parent "$c4..$long_tip" | sed -n '61p')"
has 'past cap' 'cannot be prepared'
plan 'stale trust at the newest commits' 0 GREEN="$c0 $c1"
expect_action 'stale trust' open
has 'stale trust' 'cannot be prepared'
trust_metadata 1.2.0 1.0.0 1.1.0 1.2.0 >"$repo/release/trust/metadata.json"
plan 'trust names a release that is not published' 0
has 'trust ahead' 'a maintainer must reconcile them'
no_tag_commands 'trust ahead'
trust_metadata 1.2.0-rc.1 1.0.0 1.1.0 1.2.0-rc.1 >"$repo/release/trust/metadata.json"
plan 'a signed latest that is a prerelease' 0 REL_EXTRA=v1.2.0-rc.1
has 'prerelease latest' 'is a prerelease'
no_tag_commands 'prerelease latest'
git -C "$repo" checkout -q -- release/trust/metadata.json
plan 'trust unsynchronized' 0 SYNCED=behind
expect_action 'unsynced' open
has 'unsynced' 'trust is not synchronized'
no_tag_commands 'unsynced'
plan 'an unreadable trust sync stops the run' 1 SYNCED=boom
plan 'a trust helper whose release read fails stops the run, it is not a refusal' 1 SYNCED_BIN="$scratch/bin/synced-down" REAL_ROOT="$real_root"
if [ -s "$scratch/plan.json" ]; then fail 'a failed release read must not write a plan'; fi
plan 'the real trust helper refuses an outstanding draft as a blocked plan' 0 SYNCED_BIN="$scratch/bin/synced-real" REAL_ROOT="$real_root"
has 'real helper' 'trust is not synchronized'
# A helper that cannot even create its scratch file did not look: exit 2, never the refusal's exit 1.
helper_status=0
TMPDIR="$scratch/no-such-directory" GH_BIN="$scratch/bin/gh" "$real_root/scripts/release/require-synced-trust.sh" o/r "$repo/release/trust/metadata.json" >/dev/null 2>&1 || helper_status=$?
[ "$helper_status" = 2 ] || fail "the trust helper must exit 2 when it cannot create its scratch file (got $helper_status)"
plan 'untrusted stable trust' 0 STABLE=untrusted
has 'untrusted' 'stable trust does not authenticate'
plan 'candidate refused' 0 STABLE=badcandidate
has 'candidate refused' 'not a valid successor'
# A compatibility difference: a patch cannot ship it, so the issue proposes the next minor with its prerequisites.
plan 'compatibility differs: a minor is proposed' 0 COMPAT=differs
expect_action 'minor' open
has minor '## Security release v1.2.0 (minor) is ready to tag once you have confirmed its prerequisites'
has minor "Planned release:** \`v1.2.0\` at \`$c4\`"
has minor 'A patch cannot ship it: the compatibility declaration differs from the one v1.1.0 published'
# Two attempts (the patch, then the minor) run in one workspace: each gets its own output files and its own scratch
# database, and the run leaves no container behind. The fake writers and the fake starter refuse what the real ones refuse.
[ "$(grep -c '^start$' "$scratch/pg-log")" = 2 ] && [ "$(grep -c '^stop$' "$scratch/pg-log")" = 2 ] || { cat "$scratch/pg-log" >&2; fail 'each attempt starts and removes its own scratch PostgreSQL'; }
[ ! -e "$scratch/pg-container" ] || fail 'the plan leaves its scratch PostgreSQL container running'
has minor '### Prerequisites that block tagging (a minor release)'
lacks minor 'Dispatch the `release-compatibility-check` workflow'
has minor '- [ ] Read the difference below, which the planning run generated for'
has minor 'compares main'"'"'s tip, which is not this commit unless the tip is the target'
has minor '- [ ] Review the edge modes'
has minor 'carries a sound release-source edge from v1.1.0 for each engine'
has minor 'https://github.com/o/r/blob/main/docs/adr/signed-upgrade-compatibility.md'
has minor '- [ ] Decide that everything on'
lacks minor '- [ ] Write the release notes'
lacks minor 'normally needs no edit'
lacks minor 'no earlier migration was edited'
has minor 'A minor release needs real release notes'
has minor '<summary>The compatibility difference the planning run saw (at most 200 lines)</summary>'
has minor 'the compatibility declaration differs from the one v1.1.0 published'
has minor "recheck minor 1.2.0 $c4 --tip &&"
has minor 'scripts/release/ceremony.sh build 1.2.0'
lacks minor 'recheck patch'
lacks minor 'build 1.1.1'
grep -F 'compat o/r ' "$scratch/calls" | grep -F ' 1.2.0 ' >/dev/null || fail 'the minor is compared as 1.2.0'
check_ceremony_lines minor
[ -e "$scratch/postgres-started" ] || fail 'the minor plan generates the declaration too'
BLOCK_VERSION=1.2.0
export BLOCK_VERSION
run_tag_block minor last-line
unset BLOCK_VERSION
plan 'a long difference is cut in the issue' 0 COMPAT=bigdiff
has 'big diff' '+line 200'
lacks 'big diff' '+line 201'
plan 'a minor below a red tip' 0 COMPAT=differs GREEN="$c0 $c1 $c2 $c3"
has 'minor red tip' 'git fetch hikyo-upstream main &&'
has 'minor red tip' "git tag -s v1.2.0 -m \"Hikyo 1.2.0\" $c3 &&"
has 'minor red tip' "recheck minor 1.2.0 $c3 &&"
plan 'a minor that would be refused is blocked' 0 COMPAT=differs MINOR_RELEASE_EXISTS=1
expect_action 'minor refused' open
has 'minor refused' 'v1.2.0 already has a release'
no_tag_commands 'minor refused'
plan 'an unverifiable baseline is not a difference' 0 COMPAT=unverified
expect_action 'unverified' open
has 'unverified' 'does not verify against the signed trust'
lacks 'unverified' 'Security release v1.2.0'
no_tag_commands 'unverified'
plan 'a staged minor' 0 TAGS_EXTRA=v1.2.0 BUILD=success
has 'staged minor' 'v1.2.0 is tagged and its release-build run succeeded (https://github.com/o/r/actions/runs/4242), so its draft release is staged'
has 'staged minor' 'scripts/release/ceremony.sh review 1.2.0'
has 'staged minor' 'A minor release needs real release notes'
no_tag_commands 'staged minor'
# Only the check's own "differs" verdict is a difference: any other refusal blocks, as a patch and as a minor.
plan 'a patch the check refuses for another reason is blocked, not turned into a minor' 0 COMPAT=incomplete
expect_action 'incomplete patch' open
has 'incomplete patch' 'the tag would be refused: the compatibility check refused the declaration of'
body | grep -F 'the tag would be refused:' | grep -F ' .' >/dev/null && fail 'the refusal reason has no stray space before its full stop'
has 'incomplete patch' 'not a complete two-engine declaration'
lacks 'incomplete patch' 'Security release v1.2.0'
no_tag_commands 'incomplete patch'
plan 'a diff that mentions a difference above another refusal is not a difference' 0 COMPAT=earlier-line
has 'earlier line' 'the tag would be refused: the compatibility check refused the declaration of'
lacks 'earlier line' 'Security release v1.2.0'
no_tag_commands 'earlier line'
plan 'a minor the check refuses for another reason is blocked' 0 COMPAT=minorbad
has 'incomplete minor' 'the tag would be refused: the compatibility check refused the declaration of'
lacks 'incomplete minor' '(minor) is ready'
no_tag_commands 'incomplete minor'
plan 'a compatibility lookup failure stops the run' 1 COMPAT=lookup
plan 'a generator failure stops the run' 1 STABLE=nodecl
[ ! -e "$scratch/pg-container" ] || fail 'a failed generator still removes the scratch PostgreSQL container'
plan 'a scratch database that does not start stops the run' 1 POSTGRES=fail
plan 'a stray higher stable tag' 0 TAGS_EXTRA=v1.1.2
has 'stray tag' 'a stable tag at or above v1.1.1 already exists'
no_tag_commands 'stray tag'
plan 'a release already exists for the version' 0 RELEASE_EXISTS=1
has 'release exists' 'already has a release'
plan 'the green source proof cannot be read' 1 GREEN_READ=fail
[ ! -e "$scratch/postgres-started" ] || fail 'PostgreSQL must start only when a tag is otherwise ready'
plan 'the version is tagged and its run succeeded: the draft is staged' 0 TAGS_EXTRA=v1.1.1 BUILD=success
has 'drafted' 'v1.1.1 is staged'
has 'drafted' 'its release-build run succeeded'
has 'drafted' 'scripts/release/ceremony.sh review 1.1.1'
lacks 'drafted' 'does not hold every security merge'
lacks 'drafted' 'A minor release needs real release notes'
no_tag_commands 'drafted'
check_ceremony_lines 'drafted'
# The draft listing is not trusted (the token may not see drafts): a visible draft alone stages nothing.
plan 'the version is tagged with no run and a visible draft' 0 TAGS_EXTRA=v1.1.1 REL_DRAFT=v1.1.1
has 'tagged' 'no release-build run was found for that tag push'
lacks 'tagged' 'is staged'
lacks 'tagged' 'ceremony.sh review'
has 'tagged' 'a tag with no run at all is never skipped by this plan'
has 'tagged' 'inspect the run first'
has 'tagged' 'delete that draft before anything else'
no_tag_commands 'tagged'
plan 'published prerelease ahead of trust' 0 REL_EXTRA=v1.2.0-rc.1
has 'trust behind' 'synchronize trust first'
has 'trust behind' 'scripts/release/ceremony.sh sync-trust 1.2.0-rc.1'
no_tag_commands 'trust behind'
check_ceremony_lines 'trust behind'

# --- the release-source edge: required for a patch and for a minor -----------------
plan 'a patch whose sources.json lacks the edge is blocked' 0 EDGES=missing
expect_action 'patch no edge' open
has 'patch no edge' '### Prerequisite that blocks tagging: the release-source edge from v1.1.0'
has 'patch no edge' 'sqlite: no release-source edge from v1.1.0'
has 'patch no edge' 'no supported command writes a stable release edge'
has 'patch no edge' 'scripts/release/nightly sources'
has 'patch no edge' '`profile`, `version`, `sequence`, `commit`, `compatibility_sha256`, `manifest_sha256`'
has 'patch no edge' 'gh release download v1.1.0 --repo o/r --dir'
has 'patch no edge' '--historical --published'
has 'patch no edge' 'same-repository `hotfix/` branch'
has 'patch no edge' 'turns this patch into a minor release'
has 'patch no edge' '`restart` is valid only when the release'"'"'s migrations and schema digest are unchanged'
has 'patch no edge' 'cannot be prepared'
lacks 'patch no edge' 'is ready to tag'
no_tag_commands 'patch no edge'
grep -E '^compat --edges-only o/r .*/release/compatibility/sources.json$' "$scratch/calls" >/dev/null || { cat "$scratch/calls" >&2; fail 'the edge check reads sources.json'; }
if grep -E '^compat o/r ' "$scratch/calls" >/dev/null; then fail 'a missing edge stops the plan before the declaration is compared'; fi
[ ! -e "$scratch/postgres-started" ] || fail 'a missing edge is found before PostgreSQL starts'
plan 'a minor whose sources.json lacks the edge is blocked' 0 COMPAT=differs EDGES=missing
has 'minor no edge' '### Prerequisite that blocks tagging: the release-source edge from v1.1.0'
lacks 'minor no edge' 'Security release v1.2.0'
no_tag_commands 'minor no edge'
plan 'a plan that is already a minor does not say a promotion turns it into one' 0 COMPAT=differs EDGES=minor-missing
has 'minor edge text' '### Prerequisite that blocks tagging: the release-source edge from v1.1.0'
lacks 'minor edge text' 'turns this patch into a minor release'
no_tag_commands 'minor edge text'
plan 'an unverifiable baseline stops the edge check without a how-to' 0 EDGES=unverified
has 'edge unverified' 'does not verify against the signed trust'
lacks 'edge unverified' 'no supported command writes'
no_tag_commands 'edge unverified'
plan 'an edge lookup that fails stops the run' 1 EDGES=lookup
# --- the fallback channel gate release.yml asserts before it builds ---------------
plan 'a stale fallback record blocks the tag' 0 FALLBACK=stale
has 'fallback stale' 'the fallback channel gate would fail the build'
has 'fallback stale' 'older than one quarter'
no_tag_commands 'fallback stale'
grep -F 'fallback release/repository/fallback-channel-test.json security@hikyo.app' "$scratch/calls" >/dev/null || fail 'the gate runs on the record release.yml checks'
plan 'a gate that cannot run stops the run' 1 FALLBACK=boom
# --- a version a failed release-build burned is skipped ---------------------------
plan 'a tag whose build failed is burned: the next patch is planned' 0 TAGS_EXTRA=v1.1.1 BUILD=failed
expect_action 'burned' open
has 'burned' '## Security patch v1.1.2 is ready to tag'
has 'burned' 'the release-build run of v1.1.1 (https://github.com/o/r/actions/runs/4242) failed and no published release exists for it'
has 'burned' 'delete that draft first'
has 'burned' 'Then re-run the failed jobs of the run'
has 'burned' '**Skipped versions:** the release-build run of v1.1.1 (https://github.com/o/r/actions/runs/4242) failed'
body | awk '/Skipped versions/ { skipped = NR } /### 1. Tag/ { step1 = NR } END { exit !(skipped > 0 && step1 > skipped) }' || fail 'the skipped version is said above step 1'
has 'burned' 'For v1.1.1: check that `gh release view v1.1.1 --repo o/r --json isDraft -q .isDraft` prints `true`, then run `gh release delete v1.1.1 --repo o/r --yes`. Never pass `--cleanup-tag`'
# The alarm carries the same exact commands, so a blocked issue (which shows only alarms) has them too.
[ "$(body | grep -c -F 'gh release delete v1.1.1 --repo o/r --yes')" -ge 2 ] || fail 'the burned alarm names the exact draft deletion'
has 'burned' 'almost always rerunnable'
has 'burned' 'Only if the byte set cannot be completed safely, use the next version this issue plans'
lacks 'burned' 'so that version is burned'
has 'burned' 'is the next unused patch version of v1.1.0 after the burned v1.1.1'
lacks 'burned' 'is the patch successor of v1.1.0'
has 'burned' "scripts/release/security-patch-plan.sh recheck patch 1.1.2 $c4 --tip &&"
has 'burned' 'is almost always rerunnable'
lacks 'burned' 'build 1.1.1'
for build in running none; do
	plan "a tag whose build is $build is not burned" 0 TAGS_EXTRA=v1.1.1 BUILD=$build
	has "build $build" 'v1.1.1 is tagged'
	has "build $build" 'treat the version as burned'
	has "build $build" 'inspect the run first'
	no_tag_commands "build $build"
done
has 'build none' 'no release-build run was found for that tag push'
plan 'a tag whose build is running shows the run' 0 TAGS_EXTRA=v1.1.1 BUILD=running
has 'running' 'has not finished or ended without a verdict (https://github.com/o/r/actions/runs/4242)'
has 'running' 'Release-build run: https://github.com/o/r/actions/runs/4242'
for build in success rerun; do
	plan "a tag whose build is $build is staged" 0 TAGS_EXTRA=v1.1.1 BUILD=$build
	has "build $build" 'its release-build run succeeded'
	has "build $build" 'scripts/release/ceremony.sh review 1.1.1'
done
# A failed run that already created a draft is burned like any other: drafts never count, so the
# plan (which cannot list them) and the recheck (which can) skip the same version.
plan 'a failed run with a visible draft is burned, as when the draft is hidden' 0 TAGS_EXTRA=v1.1.1 REL_DRAFT=v1.1.1 BUILD=failed
has 'failed draft' '## Security patch v1.1.2 is ready to tag'
has 'failed draft' 'delete that draft first'
lacks 'failed draft' 'is staged'
lacks 'failed draft' 'ceremony.sh review 1.1.1'
plan 'a failed run whose draft only the maintainer sees is burned' 0 TAGS_EXTRA=v1.1.1 REL_DRAFT_RECHECK=v1.1.1 BUILD=failed
has 'hidden draft' '## Security patch v1.1.2 is ready to tag'
# A burned minor tag does not turn a patch into a minor: the patch is planned, and the note names the minor as a minor.
plan 'a burned minor leaves the patch alone' 0 TAGS_EXTRA=v1.2.0 BUILD=failed
expect_action 'burned minor patch' open
has 'burned minor patch' '## Security patch v1.1.1 is ready to tag'
has 'burned minor patch' 'is the patch successor of v1.1.0'
has 'burned minor patch' 'the compatibility declaration is identical to v1.1.0'"'"'s apart from that edge'
has 'burned minor patch' 'the release-build run of v1.2.0 (https://github.com/o/r/actions/runs/4242) failed'
has 'burned minor patch' "recheck patch 1.1.1 $c4 --tip &&"
lacks 'burned minor patch' 'after the burned'
lacks 'burned minor patch' '(minor)'
lacks 'burned minor patch' 'differs'
lacks 'burned minor patch' 'Prerequisites that block tagging'
# When the declaration really differs, the next unused minor after the burned one is planned, with its notes separate.
plan 'a burned minor and a differing declaration plan the next minor' 0 TAGS_EXTRA=v1.2.0 BUILD=failed COMPAT=differs
expect_action 'burned minor differs' open
has 'burned minor differs' '## Security release v1.3.0 (minor) is ready to tag once you have confirmed its prerequisites'
has 'burned minor differs' 'the next unused minor of v1.1.0 after the burned v1.2.0'
has 'burned minor differs' 'A patch cannot ship it: the compatibility declaration differs from the one v1.1.0 published'
has 'burned minor differs' "recheck minor 1.3.0 $c4 --tip &&"
lacks 'burned minor differs' 'recheck patch'
grep -F 'compat o/r ' "$scratch/calls" | grep -F ' 1.3.0 ' >/dev/null || fail 'the skipped minor is compared as 1.3.0'
# A burned patch and a burned minor at once: each note names its own kind.
plan 'a burned patch and a burned minor' 0 TAGS_EXTRA="v1.1.1 v1.2.0" BUILD=failed
has 'both burned' '## Security patch v1.1.2 is ready to tag'
has 'both burned' 'is the next unused patch version of v1.1.0 after the burned v1.1.1'
lacks 'both burned' 'after the burned v1.1.1, v1.2.0'
# A run address is text from GitHub that reaches the issue: only this repository's own run page, whole, passes.
for bad_url in 'https://github.com/evil/x/actions/runs/4242' 'https://github.com/o/r/actions/runs/abc' 'http://github.com/o/r/actions/runs/4242' "$(printf 'https://github.com/o/r/actions/runs/4242\nhttps://evil.example/x')" 'https://github.com/o/r/actions/runs/4242 '; do
	plan 'a bad run address stops the run' 1 TAGS_EXTRA=v1.1.1 BUILD=failed RUN_URL="$bad_url"
	grep -F 'unreadable release-build run address of v1.1.1' "$scratch/err" >/dev/null || { cat "$scratch/err" >&2; fail 'a bad run address is named, not swallowed'; }
done
plan 'a run list without a runs array stops the run' 1 TAGS_EXTRA=v1.1.1 BUILD=noarray
grep -F 'are not a list' "$scratch/err" >/dev/null || { cat "$scratch/err" >&2; fail 'a run list that is not a list is named'; }
# A staged draft whose tag predates a pending merge is not the release that merge needs.
plan 'a staged release whose tag misses a pending merge says so' 0 TAGS_EXTRA=v1.1.1 BUILD=success TAG_COMMIT="$c2"
has 'staged old tag' "Its commit $c2 does not hold every security merge listed below"
has 'staged old tag' 'need a further release'
plan 'a minor tag whose build is running shows the run, the hint and the way out' 0 TAGS_EXTRA=v1.2.0 BUILD=running
has 'running minor' 'v1.2.0 is tagged, and its release-build run has not finished'
has 'running minor' 'Release-build run: https://github.com/o/r/actions/runs/4242'
has 'running minor' 'treat the version as burned'
no_tag_commands 'running minor'
plan 'a burned tag with nothing pending opens nothing' 0 SEC1="$c0" TAGS_EXTRA=v1.1.1 BUILD=failed
expect_action 'burned idle' close
plan 'an unreadable release-build run list stops the run' 1 TAGS_EXTRA=v1.1.1 FAIL=runs
# --- an alert list with no alert at all must be real ------------------------------
# A time that only a lenient parser would read (February 30) must stop the run, not count as a date.
plan 'a fixedAt that only a lenient parser reads' 1 ALERTS=unlinked FIXED_AT=2026-02-30T10:00:00Z
plan 'no alerts at all, and REST agrees' 0 ALERTS=empty
expect_action 'empty alerts' close
grep -F 'api repos/o/r/dependabot/alerts?state=open,fixed&per_page=1' "$scratch/calls" >/dev/null || fail 'an empty alert list is cross-checked over REST'
plan 'no alerts over GraphQL, but REST lists some' 1 ALERTS=empty REST=nonempty
plan 'no alerts over GraphQL, and REST is forbidden' 1 ALERTS=empty REST=forbidden
plan 'a non-empty alert list needs no cross-check' 0
if grep -F 'dependabot/alerts?' "$scratch/calls" >/dev/null; then fail 'only an empty alert list is cross-checked'; fi
# --- an untrusted title never carries a URL into the issue ------------------------
plan 'a title with a link' 0 OPEN_PRS=21 TITLE_URL=1
has 'title url' '- #21 Bump x from 1 to 2 someoneb21 see httpsevil.example/x'
if body | grep '^- #21 ' | grep -F '://' >/dev/null; then body >&2; fail 'a title must never keep a URL scheme, even one that a removal would rebuild'; fi

plan 'a title with a www host' 0 OPEN_PRS=21 TITLE_URL=www
if body | grep '^- #21 ' | grep -i 'www\.' >/dev/null; then body >&2; fail 'a title must never keep a www host, even one that a removal would rebuild'; fi
has 'title www' '- #21 Bump x from 1 to 2 someoneb21 see evil.example/x'

# --- step 0: open security updates are listed for a person to queue, never queued ---
plan 'open security updates are listed, never queued' 0 OPEN_PRS="21 22"
expect_action 'step 0' open
has 'step 0' '### 0. Open security updates: a person queues each one'
has 'step 0' "- #21 Bump x from 1 to 2 someoneb21, head \`$(printf '%040x' 21)\`"
has 'step 0' "- #22 "
has 'step 0' 'click **Merge when ready**'
has 'step 0' 'Qualifying (alert-linked, Dependabot-authored, verified, not draft, admission check not failed)'
has 'step 0' 'is ready to tag'
# A title is untrusted text: markup, mentions and newlines never reach the issue.
lacks 'step 0' '@someone'
lacks 'step 0' '<b>'
# Read-only: nothing is merged, queued or written, and no pull request write is used.
if grep -E ' -X | --method |pr merge|merge-queue|enqueue|issue (create|edit|close)' "$scratch/calls" >/dev/null; then cat "$scratch/calls" >&2; fail 'step 0 must only read'; fi
check_ceremony_lines 'step 0'
plan 'with nothing pending the open updates still open the issue' 0 SEC1="$c0" OPEN_PRS=21
expect_action 'idle with open' open
has 'idle with open' 'Dependabot security updates need a look'
has 'idle with open' '- #21 '
plan 'a draft is not queued by hand' 0 SEC1="$c0" OPEN_PRS=21 PR_DRAFT=21
has 'draft' '- #21 Bump x from 1 to 2 someoneb21: it is a draft'
lacks 'draft' 'Qualifying'
plan 'a branch outside the repository' 0 SEC1="$c0" OPEN_PRS=21 PR_FOREIGN=21
has 'foreign' 'its branch is not in this repository'
plan 'an author that is not Dependabot' 0 SEC1="$c0" OPEN_PRS=21 PR_BOT=21
has 'not dependabot' 'not opened by Dependabot'
plan 'an unverified commit' 0 SEC1="$c0" OPEN_PRS=21 PR_COMMITS_UNVERIFIED=21
has 'unverified commit' 'its commits are not all Dependabot-authored, GitHub-committed and verified'
plan 'a commit authored by a person' 0 SEC1="$c0" OPEN_PRS=21 PR_COMMITS_HUMAN=21
has 'human commit' 'its commits are not all Dependabot-authored'
plan 'a head newer than the commits that were read' 0 SEC1="$c0" OPEN_PRS=21 PR_COMMITS_STALE=21
has 'stale commits' 'its commits are not all Dependabot-authored, GitHub-committed and verified'
plan 'a commit not committed by GitHub' 0 SEC1="$c0" OPEN_PRS=21 PR_COMMITS_NOT_WEBFLOW=21
has 'not web-flow' 'its commits are not all Dependabot-authored'
plan 'a failed admission check' 0 SEC1="$c0" OPEN_PRS=21 PR_ADMISSION_FAILED=21
has 'admission failed' 'the admission check failed'
plan 'a queued admission check has not failed' 0 SEC1="$c0" OPEN_PRS=21 PR_ADMISSION_QUEUED=21
has 'admission queued' 'Qualifying'
plan 'only the newest admission run counts: an old failure' 0 SEC1="$c0" OPEN_PRS=21 PR_ADMISSION_OLD_FAILED=21
has 'old failure' 'Qualifying'
plan 'only the newest admission run counts: a new failure' 0 SEC1="$c0" OPEN_PRS=21 PR_ADMISSION_NEW_FAILED=21
has 'new failure' 'the admission check failed'
plan 'removed from the merge queue after the newest commit' 0 SEC1="$c0" OPEN_PRS=21 PR_REMOVED=21
has 'removed' 'Removed from the merge queue after their newest commit'
has 'removed' '- #21 '
lacks 'removed' 'Qualifying'
plan 'removed before the newest commit is no longer a reason' 0 SEC1="$c0" OPEN_PRS=21 PR_REMOVED_BEFORE=21
has 'removed before' 'Qualifying'
lacks 'removed before' 'Removed from the merge queue'
plan 'a pull request already in the merge queue is not listed' 0 SEC1="$c0" OPEN_PRS=21 PR_INQUEUE=21
expect_action 'queued' close
plan 'an unreadable open pull request stops the run' 1 SEC1="$c0" OPEN_PRS=21 FAIL=open-pr
# --- nothing pending --------------------------------------------------------
plan 'a merge the latest release already holds' 0 SEC1="$c0"
expect_action 'released' close
# A published release that trust does not record keeps the issue open even with nothing pending.
plan 'a release is published but trust is behind' 0 SEC1="$c0" REL_EXTRA=v1.1.1
expect_action 'trust behind idle' open
has 'trust behind idle' 'v1.1.1 is published but main'"'"'s trust ends at v1.1.0'
has 'trust behind idle' 'scripts/release/ceremony.sh sync-trust 1.1.1'
has 'trust behind idle' 'scripts/release/ceremony.sh homebrew 1.1.1'
lacks 'trust behind idle' 'Merged Dependabot security updates waiting'
check_ceremony_lines 'trust behind idle'
plan 'a published prerelease has no Homebrew step' 0 SEC1="$c0" REL_EXTRA=v1.2.0-rc.1
lacks 'prerelease trust' 'ceremony.sh homebrew'
has 'prerelease trust' 'sync-trust 1.2.0-rc.1'
plan 'an unmerged security update is not pending' 0 ALERTS=unmerged
expect_action 'unmerged' open
lacks 'unmerged' 'is ready to tag'
has 'unmerged' '### 0. Open security updates'
has 'unmerged' '- #7 '
no_tag_commands 'unmerged'
plan 'an impostor author is not a security update, and its fix is unexplained' 0 ALERTS=impostor
expect_action 'impostor' open
has 'impostor' '1 Dependabot alert(s) were fixed'
lacks 'impostor' 'is ready to tag'
plan 'a base other than main, a feature branch and a foreign author explain nothing' 0 SEC1="$c0" DECOYS=1
expect_action 'decoys' open
has 'decoys' '3 Dependabot alert(s) were fixed'
plan 'an alert fixed before the latest release is not an alarm' 0 SEC1="$c0" ALERTS=old-unlinked
expect_action 'old alert' close
# --- alarms: whatever else the plan says, an unexplained fix reaches the issue -
plan 'a fixed alert without a pull request' 0 SEC1="$c0" ALERTS=unlinked
expect_action 'unlinked' open
has 'unlinked' 'Dependabot alert(s) were fixed'
has 'unlinked' 'The alarm clears once a stable release is published after the fix'
plan 'a fixed alert whose pull request is not merged' 0 SEC1="$c0" ALERTS=unlinked-unmerged
expect_action 'unlinked unmerged' open
has 'unlinked unmerged' 'Dependabot alert(s) were fixed'
plan 'a fixed alert without a time' 0 SEC1="$c0" ALERTS=unlinked-nofixedat
expect_action 'no fixedAt' open
plan 'an unexplained fix blocks the tag' 0 ALERTS=unlinked
has 'unlinked blocks' 'Also needs a person'
has 'unlinked blocks' 'cannot be prepared'
has 'unlinked blocks' 'are not traced to a merged security pull request'
lacks 'unlinked blocks' 'is ready to tag'
no_tag_commands 'unlinked blocks'
plan 'decoy pull requests beside a ready tag block it too' 0 DECOYS=1
has 'decoys block' '3 Dependabot alert(s) were fixed'
no_tag_commands 'decoys block'
plan 'a fixed alert with an unreadable time stops the run' 1 SEC1="$c0" ALERTS=unlinked FIXED_AT=garbage
plan 'a merge commit that is not in history is raised, not skipped' 0 SEC1="ffffffffffffffffffffffffffffffffffffffff"
expect_action 'missing commit' open
has 'missing commit' 'is not in main'
# A merge that cannot be placed blocks any tag, even when everything else is ready.
has 'missing commit' 'cannot be prepared'
lacks 'missing commit' 'is ready to tag'
# A merge time is validated for every state, even those that never use it for a decision.
plan 'a merge without a merge commit is raised, not skipped' 0 NO_COMMIT=1
expect_action 'no merge commit' open
has 'no merge commit' 'GitHub reports no merge commit'
has 'no merge commit' 'cannot be prepared'
lacks 'no merge commit' 'is ready to tag'
plan 'a merge outside the history of the tip is raised, not skipped' 0 SEC1="$side"
expect_action 'side merge' open
has 'side merge' "not in main's history"
# A merge main reaches only through a side parent is held by the merge commit that brought it: that
# commit is the target, and the older first-parent commits (which lack the merge) are never taken.
plan 'a merge reached through a side parent is held by the merge commit' 0 SEC1="$side" GITHUB_SHA="$m" GREEN="$m"
expect_action 'off first parent' open
has 'off first parent' "Planned release:** \`v1.1.1\` at \`$m\`"
has 'off first parent' 'is ready to tag'
lacks 'off first parent' 'first-parent history'
plan 'a side-parent merge whose merge commit is not green blocks' 0 SEC1="$side" GITHUB_SHA="$m" GREEN="$c3 $c4"
expect_action 'off first parent red' open
has 'off first parent red' 'no main commit among the newest'
no_tag_commands 'off first parent red'
plan 'a commit whose trust metadata is unreadable stops the run' 1 GITHUB_SHA="$cbad" GREEN="$cbad $c4"
plan 'the newest merge sits in a commit that does not record the latest release' 0 SEC1="$c1" GREEN="$c1"
expect_action 'stale trust commit' open
has 'stale trust commit' 'cannot be prepared'
no_tag_commands 'stale trust commit'
plan 'a merge time that is not a timestamp stops the run' 1 MERGED_AT=garbage
plan 'a merge time that is not a timestamp stops a staged release too' 1 MERGED_AT=garbage TAGS_EXTRA=v1.1.1 BUILD=success
plan 'a merge time that date would read as a word stops the run' 1 MERGED_AT=tomorrow
# An impossible date is refused by GNU date and normalized by BSD date; either way it must stop the run.
plan 'a merge time that is not a real date stops the run' 1 MERGED_AT=2026-02-30T00:00:00Z
# A date that reads leniently (GNU date accepts words) must not slip past the strict format.
mkdir "$scratch/gnudate"
cat >"$scratch/gnudate/date" <<'SH'
#!/bin/sh
case "$*" in *" -d ${DATE_FOR:-tomorrow} "*) printf '%s\n' "${DATE_OUT:-1}" ;; *) exec /bin/date "$@" ;; esac
SH
chmod +x "$scratch/gnudate/date"
plan 'a lenient date reading of a word is not accepted' 1 PATH="$scratch/gnudate:$PATH" MERGED_AT=tomorrow
plan 'a date result that is not a number is not accepted' 1 PATH="$scratch/gnudate:$PATH" MERGED_AT=2026-11-14T12:00:00Z DATE_FOR=2026-11-14T12:00:00Z DATE_OUT=soon TAGS_EXTRA=v1.1.1 BUILD=success
plan 'a null alert answer stops the run' 1 ALERTS=null
plan 'an unreadable alert list stops the run' 1 FAIL=alerts
plan 'an unreadable release list stops the run' 1 FAIL=releases
plan 'an unreadable tag list stops the run' 1 FAIL=matching-refs
plan 'an unreadable release lookup stops the run' 1 FAIL=release-lookup
plan 'an unreadable latest commit stops the run' 1 FAIL=latest-commit
# --- how long a merge has waited ---------------------------------------------
plan 'a fresh merge is not overdue' 0
lacks 'fresh' 'waited more than 24 hours'
plan 'an old merge is overdue' 0 MERGED_AT=2026-11-09T00:00:00Z
has 'overdue' 'waited more than 24 hours'
plan 'the oldest of two merges decides' 0 MERGED_AT=2026-11-09T00:00:00Z SEC2="$c4"
has 'oldest of two' 'waited more than 24 hours'

# --- the issue itself --------------------------------------------------------
plan 'ready' 0
cp "$scratch/plan.json" "$scratch/ready-plan.json"
apply() { # plan-file name
	rm -f "$scratch/created" "$scratch/edited" "$scratch/closed"
	: >"$scratch/calls"
	env GH_REPO=o/r GH_TOKEN=t "$@" >"$scratch/out" 2>"$scratch/err"
}
# issue_json TITLE BODY [USER_ID [TYPE [PULL_REQUEST]]]: the REST listing, one page.
issue_json() { jq -nc --arg title "$1" --arg body "$2" --argjson id "${3:-41898282}" --arg type "${4:-Bot}" --arg pr "${5:-}" '[[{number:5,title:$title,body:$body,user:{id:$id,type:$type,login:"github-actions[bot]"},pull_request:(if $pr == "" then null else {url:"x"} end)}]]'; }
title='Security patch release: maintainer action needed'
echo '[[]]' >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/created" ] || fail 'a plan to open creates the issue when none exists'
grep -F -- "--title $title" "$scratch/calls" >/dev/null || fail 'the issue has its fixed title'
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json")" >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
{ [ ! -e "$scratch/created" ] && [ ! -e "$scratch/edited" ]; } || fail 'an unchanged issue is left alone'
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json" | awk '{ printf "%s\r\n", $0 }')" >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ ! -e "$scratch/edited" ] || fail 'a body that only differs by line endings is not rewritten'
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json" | sed 's/ready/stale/')" >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/edited" ] || fail 'a changed issue is rewritten'
[ ! -e "$scratch/created" ] || fail 'one issue only'
issue_json 'Some other title' "$(jq -r .body "$scratch/ready-plan.json")" >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/created" ] || fail 'another issue with a different title is not ours'
issue_json "$title" 'a stranger wrote this without the marker' >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/created" ] || fail 'an issue without our marker is not ours'
# Anyone can open an issue with the title and the marker; only the workflow's own bot owns the issue.
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json")" 5 User >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
{ [ -e "$scratch/created" ] && [ ! -e "$scratch/edited" ]; } || fail 'an issue with our marker from another author is not ours'
# The workflow's bot is bound by its numeric id and its type, never by a login spelling.
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json")" 41898282 User >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/created" ] || fail 'a user with the bot id is not the bot'
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json")" 12345 Bot >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/created" ] || fail 'another app with the same title is not ours'
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json")" 41898282 Bot yes >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/created" ] || fail 'a pull request is never our issue'
issue_json "$title" "$(printf '<!-- security-patch-plan -->\nold')" >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
{ [ -e "$scratch/edited" ] && [ ! -e "$scratch/created" ]; } || fail 'the workflow bot by id and type is ours'
plan 'released' 0 SEC1="$c0"
issue_json "$title" "$(printf '<!-- security-patch-plan -->\nold')" >"$scratch/issues.json"
cp "$scratch/plan.json" "$scratch/close-plan.json"
apply "$script" issue "$scratch/close-plan.json"
[ -e "$scratch/closed" ] || fail 'a plan to close closes our open issue'
echo '[[]]' >"$scratch/issues.json"
apply "$script" issue "$scratch/close-plan.json"
[ ! -e "$scratch/closed" ] || fail 'closing with no open issue does nothing'
issue_json "$title" "$(printf '<!-- security-patch-plan -->\nold')" 5 User >"$scratch/issues.json"
apply "$script" issue "$scratch/close-plan.json"
[ ! -e "$scratch/closed" ] || fail 'another author is not closed'
# A failed or timed-out planning run is reported in the same issue, never left stale or silent.
run_url=https://github.com/o/r/actions/runs/123456789
echo '[[]]' >"$scratch/issues.json"
apply "$script" issue --failed "$run_url"
[ -e "$scratch/created" ] || fail 'a failed run opens the issue when none exists'
grep -F 'Security patch planning failed' "$scratch/last-body" >/dev/null || fail 'the failure issue says what happened'
grep -F "$run_url" "$scratch/last-body" >/dev/null || fail 'the failure issue links the run'
grep -F 'git tag' "$scratch/last-body" >/dev/null && fail 'the failure issue offers no tag command'
issue_json "$title" "$(printf '<!-- security-patch-plan -->\nold')" >"$scratch/issues.json"
apply "$script" issue --failed "$run_url"
{ [ -e "$scratch/edited" ] && [ ! -e "$scratch/created" ]; } || fail 'a failed run rewrites our open issue'
for bad in 'http://github.com/o/r/actions/runs/1' 'https://evil.example/o/r/actions/runs/1' 'https://github.com/o/r/actions/runs/1 x' 'https://github.com/o/r/actions/runs/1?x=1' '' "$(printf 'https://github.com/o/r/actions/runs/1\nx')"; do
	if env GH_REPO=o/r GH_TOKEN=t "$script" issue --failed "$bad" >/dev/null 2>&1; then fail "issue --failed must refuse the URL: $bad"; fi
done
# A trailing newline must not pass the anchored match (jq's $ would accept it), nor another repository's run.
nl_url=$(printf 'https://github.com/o/r/actions/runs/1\n.')
for bad in "${nl_url%.}" 'https://github.com/x/y/actions/runs/1'; do
	if env GH_REPO=o/r GH_TOKEN=t "$script" issue --failed "$bad" >/dev/null 2>&1; then fail "issue --failed must refuse the URL: $bad"; fi
done
if env GH_REPO=o/r GH_TOKEN=t "$script" issue --failed >/dev/null 2>&1; then fail 'issue --failed needs a run URL'; fi
if env FAIL=issue-list GH_REPO=o/r GH_TOKEN=t "$script" issue "$scratch/ready-plan.json" >/dev/null 2>&1; then fail 'an unreadable issue list must stop the run'; fi
printf '{"action":"delete","body":"x"}\n' >"$scratch/bad-plan.json"
if env GH_REPO=o/r GH_TOKEN=t "$script" issue "$scratch/bad-plan.json" >/dev/null 2>&1; then fail 'an unknown action is refused'; fi
printf '{"action":"open","body":""}\n' >"$scratch/bad-plan.json"
if env GH_REPO=o/r GH_TOKEN=t "$script" issue "$scratch/bad-plan.json" >/dev/null 2>&1; then fail 'an empty body is refused'; fi

# --- the maintainer's recheck -------------------------------------------------
recheck() { # name want-status expected-fragment version commit option ('-' for none) [VAR=value...]
	name=$1 want=$2 fragment=$3 version=$4 commit=$5 opt=$6
	shift 6
	[ "$opt" != - ] || opt=
	: >"$scratch/calls"
	status=0
	env GH_REPO=o/r FIXTURE_VIEW=recheck "$@" "$script" recheck "${KIND:-patch}" "$version" "$commit" ${opt:+"$opt"} >"$scratch/out" 2>&1 || status=$?
	[ "$status" = "$want" ] || { cat "$scratch/out" >&2; fail "recheck $name: exit $status, want $want"; }
	grep -F -- "$fragment" "$scratch/out" >/dev/null || { cat "$scratch/out" >&2; fail "recheck $name: expected: $fragment"; }
}
git -C "$repo" checkout -q --detach "$c4"
recheck 'passes at the tip' 0 "ok to tag v1.1.1 at $c4" 1.1.1 "$c4" --tip
recheck 'a checkout at another commit' 3 'this checkout is not at' 1.1.1 "$c3" -
git -C "$repo" checkout -q --detach "$c3"
recheck 'passes at an older green commit' 0 "ok to tag v1.1.1 at $c3" 1.1.1 "$c3" -
if grep -E '^compat ' "$scratch/calls" | grep -v -F -- '--edges-only' >/dev/null || grep -E '^go .*compatibility' "$scratch/calls" >/dev/null; then fail 'the recheck does not rerun the compatibility generator'; fi
grep -F 'compat --edges-only o/r ' "$scratch/calls" >/dev/null || fail 'the recheck enforces the release-source edge'
grep -F 'fallback release/repository/fallback-channel-test.json' "$scratch/calls" >/dev/null || fail 'the recheck runs the fallback channel gate'
recheck '--tip refuses an older commit' 3 'main moved' 1.1.1 "$c3" --tip
recheck 'a newer security merge landed' 3 'does not contain every merged security update' 1.1.1 "$c3" - SEC2="$c4"
recheck 'an unknown merge stops the tag' 3 'cannot be placed' 1.1.1 "$c3" - SEC2=ffffffffffffffffffffffffffffffffffffffff
recheck 'nothing pending any more' 3 'nothing to release' 1.1.1 "$c3" - SEC1="$c0"
recheck 'a stale version' 3 'not the patch successor' 1.1.2 "$c3" -
recheck 'a commit that is not green' 3 'complete green source proof' 1.1.1 "$c3" - GREEN="$c2"
recheck 'trust behind' 3 'trust is not synchronized' 1.1.1 "$c3" - SYNCED=behind
recheck 'a release already exists' 3 'already has a release' 1.1.1 "$c3" - RELEASE_EXISTS=1
recheck 'a stray higher tag' 3 'already exists' 1.1.1 "$c3" - TAGS_EXTRA=v1.1.2
recheck 'an unreadable alert list' 1 'cannot read' 1.1.1 "$c3" - FAIL=alerts
recheck 'the version already tagged' 3 'already exists' 1.1.1 "$c3" - TAGS_EXTRA=v1.1.1
recheck 'a version that is not X.Y.Z' 1 'X.Y.Z' v1.1.1 "$c3" -
recheck 'a version with a second line is not X.Y.Z' 1 'X.Y.Z' "$(printf '1.1.1\nx')" "$c3" -
if env GH_REPO="$(printf 'o/r\nx')" GH_TOKEN=t GITHUB_SHA="$c4" PLAN_OUT="$scratch/plan.json" "$script" plan >/dev/null 2>&1; then fail 'a repository name with a second line is refused'; fi
git -C "$repo" checkout -q --detach "$x"
recheck 'a commit that main does not contain' 3 'not on main history' 1.1.1 "$x" -
git -C "$repo" checkout -q --detach "$c3"
recheck 'a bad option' 1 'only option is --tip' 1.1.1 "$c3" --bogus
git -C "$repo" checkout -q --detach "$c1"
recheck 'trust that does not record the latest release' 3 'trust ends at v1.0.0' 1.1.1 "$c1" - SEC1="$c1"
# The kind the issue planned is checked against the current state.
git -C "$repo" checkout -q --detach "$c3"
KIND='minor'
recheck 'a minor passes with its own version' 0 "ok to tag v1.2.0 at $c3 (minor release)" 1.2.0 "$c3" -
recheck 'a minor request with the patch version' 3 'not the minor successor' 1.1.1 "$c3" -
KIND='patch'
recheck 'a patch request with the minor version' 3 'not the patch successor' 1.2.0 "$c3" -
KIND='major'
recheck 'an unknown kind' 1 'patch or minor' 1.1.1 "$c3" -
KIND='patch'
# A patch must not carry a migration or a compatibility source change; a minor may.
git -C "$repo" checkout -q --detach "$cmig"
recheck 'a patch with a new migration' 3 'changes migrations or compatibility files' 1.1.1 "$cmig" -
git -C "$repo" checkout -q --detach "$ccomp"
recheck 'a patch with a compatibility source change' 3 'beyond adding the release-source edge from v1.1.0' 1.1.1 "$ccomp" -
# Only the reviewed release edge may be added to sources.json; any other addition is a change.
git -C "$repo" update-ref refs/remotes/origin/main "$cedge"
git -C "$repo" checkout -q --detach "$cedge"
recheck 'a patch that only adds the release edge' 0 'ok to tag v1.1.1' 1.1.1 "$cedge" --tip GREEN="$c3 $c4 $cedge"
git -C "$repo" update-ref refs/remotes/origin/main "$cedgeplus"
git -C "$repo" checkout -q --detach "$cedgeplus"
recheck 'a patch that adds a genesis edge besides' 3 'beyond adding the release-source edge from v1.1.0' 1.1.1 "$cedgeplus" --tip GREEN="$c3 $c4 $cedge $cedgeplus"
for variant in removed swapped double stringrel mixed; do
	eval "cv=\$c$variant"
	git -C "$repo" update-ref refs/remotes/origin/main "$cv"
	git -C "$repo" checkout -q --detach "$cv"
	recheck "a patch whose older or second edge is $variant" 3 'beyond adding the release-source edge from v1.1.0' 1.1.1 "$cv" --tip GREEN="$c3 $c4 $cv"
done
git -C "$repo" update-ref refs/remotes/origin/main "$c4"
git -C "$repo" checkout -q --detach "$c3"
recheck 'a recheck refuses when the edge is missing' 3 'lacks the reviewed release-source edge from v1.1.0' 1.1.1 "$c3" - EDGES=missing
recheck 'a recheck refuses when the baseline does not verify' 3 'does not verify against the signed trust' 1.1.1 "$c3" - EDGES=unverified
recheck 'a recheck refuses a stale fallback record' 3 'the fallback channel gate would fail the build' 1.1.1 "$c3" - FALLBACK=stale
# A failed release-build burns its version: the successor is what the recheck accepts.
recheck 'a burned patch version is skipped' 0 'ok to tag v1.1.2' 1.1.2 "$c3" - TAGS_EXTRA=v1.1.1 BUILD=failed
recheck 'a burned patch version is not accepted' 3 'not the patch successor of v1.1.0 (v1.1.2)' 1.1.1 "$c3" - TAGS_EXTRA=v1.1.1 BUILD=failed
KIND='minor'
recheck 'a burned minor version is skipped' 0 'ok to tag v1.3.0' 1.3.0 "$c3" - TAGS_EXTRA=v1.2.0 BUILD=failed
recheck 'a burned minor version is not accepted' 3 'not the minor successor of v1.1.0 (v1.3.0)' 1.2.0 "$c3" - TAGS_EXTRA=v1.2.0 BUILD=failed
KIND='patch'
recheck 'a patch below a burned minor tag is valid' 0 'ok to tag v1.1.1' 1.1.1 "$c3" - TAGS_EXTRA=v1.2.0 BUILD=failed
recheck 'a patch below a minor tag that is still building is refused' 3 'a stable tag at or above v1.1.1 already exists' 1.1.1 "$c3" - TAGS_EXTRA=v1.2.0 BUILD=running
recheck 'a failed tag beyond an unburned gap still orders' 3 'a stable tag at or above' 1.1.2 "$c3" - TAGS_EXTRA='v1.1.1 v1.1.3' BUILD=failed
# The draft a failed run left is seen by the recheck and not by the plan: both skip the same version.
recheck 'a draft only the recheck sees does not make the burned version current again' 0 'ok to tag v1.1.2' 1.1.2 "$c3" - TAGS_EXTRA=v1.1.1 BUILD=failed REL_DRAFT_RECHECK=v1.1.1
recheck 'the burned version is still not accepted when its draft is visible' 3 'not the patch successor of v1.1.0 (v1.1.2)' 1.1.1 "$c3" - TAGS_EXTRA=v1.1.1 BUILD=failed REL_DRAFT_RECHECK=v1.1.1
# The commands run the release tools from the commit; below the tip they must match the tip's.
git -C "$repo" checkout -q --detach "$ctool"
recheck 'the release tools differ from the tip' 3 'release tools differ between' 1.1.1 "$ctool" -
git -C "$repo" checkout -q --detach "$c3"
# An alert fixed without a traceable merge is decided by hand.
recheck 'an unlinked fixed alert' 3 'without a linked merged pull request' 1.1.1 "$c3" - ALERTS=unlinked
recheck 'an old unlinked alert is not counted' 0 'ok to tag' 1.1.1 "$c3" - ALERTS=old-unlinked
# The ceremony and a signed tag start from a clean checkout.
printf 'x\n' >"$repo/untracked.txt"
recheck 'an untracked file' 3 'uncommitted or untracked' 1.1.1 "$c3" -
rm "$repo/untracked.txt"
printf '# edit\n' >>"$repo/README.md" 2>/dev/null || printf 'x\n' >"$repo/README.md"
recheck 'an edited file' 3 'uncommitted or untracked' 1.1.1 "$c3" -
git -C "$repo" checkout -q -- . 2>/dev/null || true
git -C "$repo" clean -fdq
git -C "$repo" checkout -q --detach "$c4"
if "$script" recheck 1.1.1 >/dev/null 2>&1; then fail 'recheck needs a version and a commit'; fi

printf 'security-patch-plan fixture: the plan, the issue, its commands and the recheck behave in every state\n'

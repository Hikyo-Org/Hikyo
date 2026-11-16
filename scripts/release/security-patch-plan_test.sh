#!/bin/sh
# Fixtures for the security patch planner: what the single issue says in every
# state (ready at the tip, ready below a red tip, blocked, drafted, trust behind,
# alarms), what the maintainer's recheck refuses, and that the commands the issue
# prints are real: each ceremony command is checked against the ceremony's own
# argument counts and the tag block is executed in a clone. Every lookup that
# decides something also has a failing case, because a failed read must never
# look like an empty answer.
# shellcheck disable=SC2030,SC2031
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
real_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
script=$script_dir/security-patch-plan.sh
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-security-patch-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
repo="$scratch/repo"
mkdir -p "$scratch/bin" "$repo/release/trust" "$repo/release/compatibility"
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
printf '{}\n' >"$repo/release/compatibility/sources.json"
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
git -C "$repo" switch -q main
git -C "$repo" reset -q --hard "$c4"
export FIXTURE="$scratch" C0="$c0" C1="$c1" C2="$c2" C3="$c3" C4="$c4"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
export NOW

cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE/calls"
boom() { printf 'gh: server error (HTTP 500)\n' >&2; exit 1; }
failing() { case ",${FAIL:-}," in *",$1,"*) boom ;; esac; }
hex() { printf "$1%.0s" $(seq 40); }
case "$*" in
	'api --paginate --slurp repos/o/r/releases?per_page=100')
		failing releases
		jq -nc --arg extra "${REL_EXTRA:-}" --arg draft "${REL_DRAFT:-}" '
			[("v1.1.0", "v1.0.0", (if $extra != "" then ($extra | split(" ")[]) else empty end), "v9.9.9-nightly.20260101.1.gabcdef12")] |
			map({tag_name: ., draft: false, published_at: "2026-11-08T23:26:41Z"}) +
			[{tag_name: "v8.0.0", draft: true, published_at: null}] +
			(if $draft != "" then ($draft | split(" ") | map({tag_name: ., draft: true, published_at: null})) else [] end) | [.]' ;;
	'api repos/o/r/commits/v1.1.0') failing latest-commit; jq -nc --arg sha "$C0" '{sha:$sha}' ;;
	'api graphql --paginate '*)
		failing alerts
		case "$*" in *'states:[OPEN,FIXED]'*) ;; *) printf 'the alert query must list its states explicitly\n' >&2; exit 2 ;; esac
		case "$*" in *'pageInfo{hasNextPage endCursor}'*) ;; *) printf 'the alert query must select pageInfo\n' >&2; exit 2 ;; esac
		[ "${ALERTS:-}" != null ] || { echo '{"data":{"repository":{"vulnerabilityAlerts":null}}}'; exit 0; }
		bot=49699333
		[ "${ALERTS:-}" != impostor ] || bot=1
		oid=${SEC1:-$C3}
		merged=true
		[ "${ALERTS:-}" != unmerged ] || merged=false
		jq -nc --arg oid "$oid" --argjson merged "$merged" --arg at "${MERGED_AT:-$NOW}" --argjson bot "$bot" \
			--arg oid2 "${SEC2:-}" --arg pfix "${PR_FIXED_AT:-$NOW}" --arg at2 "${MERGED_AT2:-$NOW}" --arg nocommit "${NO_COMMIT:-}" --arg mode "${ALERTS:-}" --arg fixed "${FIXED_AT:-$NOW}" '
			def pr($n; $m; $oid; $at; $ref): {state:(if $m then "FIXED" else "OPEN" end),fixedAt:(if $m then $pfix else null end),
				dependabotUpdate:{pullRequest:{number:$n,merged:$m,mergedAt:(if $m then $at else null end),baseRefName:"main",headRefName:$ref,
				author:{login:"dependabot",databaseId:$bot},mergeCommit:(if $m and $nocommit == "" then {oid:$oid} else null end)}}};
			{data:{repository:{vulnerabilityAlerts:{nodes:([
				pr(7; $merged; $oid; $at; "dependabot/npm_and_yarn/x"),
				{state:"OPEN",fixedAt:null,dependabotUpdate:null},
				(pr(9; true; "9999999999999999999999999999999999999999"; $at; "dependabot/go/dev") | .dependabotUpdate.pullRequest.baseRefName = "dev"),
				pr(13; true; "7777777777777777777777777777777777777777"; $at; "feature/z"),
				(pr(10; true; "8888888888888888888888888888888888888888"; $at; "dependabot/go/other") | .dependabotUpdate.pullRequest.author.databaseId = 2)
			] + (if $oid2 != "" then [pr(11; true; $oid2; $at2; "dependabot/go/second")] else [] end)
			+ (if $mode == "unlinked" then [{state:"FIXED",fixedAt:$fixed,dependabotUpdate:null}] else [] end)
			+ (if $mode == "unlinked-unmerged" then [{state:"FIXED",fixedAt:$fixed,dependabotUpdate:{pullRequest:pr(12; false; null; $at; "dependabot/go/u").dependabotUpdate.pullRequest}}] else [] end)
			+ (if $mode == "unlinked-nofixedat" then [{state:"FIXED",fixedAt:null,dependabotUpdate:null}] else [] end)
			+ (if $mode == "old-unlinked" then [{state:"FIXED",fixedAt:"2026-10-01T00:00:00Z",dependabotUpdate:null}] else [] end))}}}}' ;;
	'api --paginate repos/o/r/git/matching-refs/tags/v')
		failing matching-refs
		jq -nc --arg extra "${TAGS_EXTRA:-}" '[{ref:"refs/tags/v1.1.0"},{ref:"refs/tags/v-ruleset-probe"},{ref:"refs/tags/v1.2.0-nightly.20261112.1.gabcdef12"}] +
			(if $extra != "" then ($extra | split(" ") | map({ref: ("refs/tags/" + .)})) else [] end)' ;;
	'api repos/o/r/releases/tags/v1.1.1')
		failing release-lookup
		if [ "${RELEASE_EXISTS:-}" = 1 ]; then echo '{"draft":false}'; else printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; fi ;;
	'issue list '*)
		failing issue-list
		case "$*" in *'--json number,title,body,author'*) ;; *) printf 'the issue listing must include the author\n' >&2; exit 2 ;; esac
		cat "$FIXTURE/issues.json" ;;
	'issue create '*) : >"$FIXTURE/created" ;;
	'issue edit '*) : >"$FIXTURE/edited" ;;
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
	*'stable candidate '*) [ "${STABLE:-}" != badcandidate ] || exit 1; printf '{}\n' >"$out" ;;
	*'release/compatibility '*) [ "${STABLE:-}" != nodecl ] || exit 1; printf '{}\n' >"$out" ;;
	*) printf 'unexpected go call: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$scratch/bin/green" <<'SH'
#!/bin/sh
printf 'green %s\n' "$*" >>"$FIXTURE/calls"
[ "${GREEN_READ:-}" != fail ] || { echo 'green main: GitHub read failed'; exit 1; }
case " ${GREEN:-$C0 $C1 $C2 $C3 $C4} " in *" $2 "*) exit 0 ;; esac
echo 'green main: needs complete authenticated source proof'
exit 1
SH
cat >"$scratch/bin/synced" <<'SH'
#!/bin/sh
case "${SYNCED:-ok}" in ok) exit 0 ;; behind) exit 1 ;; *) exit 2 ;; esac
SH
cat >"$scratch/bin/compat" <<'SH'
#!/bin/sh
printf 'compat %s\n' "$*" >>"$FIXTURE/calls"
case "${COMPAT:-same}" in
	same) echo 'compatibility check: compatibility is identical to v1.1.0' ;;
	differs) echo 'compatibility check: the compatibility declaration differs from the one v1.1.0 published' >&2; exit 1 ;;
	*) echo 'compatibility check: cannot download v1.1.0' >&2; exit 4 ;;
esac
SH
cat >"$scratch/bin/postgres" <<'SH'
#!/bin/sh
: >"$FIXTURE/postgres-started"
[ "${POSTGRES:-}" != fail ] || exit 1
SH
chmod +x "$scratch/bin/"*
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go" GREEN_BIN="$scratch/bin/green" SYNCED_BIN="$scratch/bin/synced" \
	COMPAT_BIN="$scratch/bin/compat" POSTGRES_BIN="$scratch/bin/postgres" HIKYO_REPO_ROOT="$repo"
echo '[]' >"$scratch/issues.json"

reset() { rm -f "$scratch/calls" "$scratch/postgres-started" "$scratch/created" "$scratch/edited" "$scratch/closed" "$scratch/plan.json"; : >"$scratch/calls"; }
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
plan 'ready at the green tip' 0
expect_action ready open
has ready '## Security patch v1.1.1 is ready to tag'
has ready "merge commit \`$c3\`"
has ready "Planned release:** \`v1.1.1\` at \`$c4\`"
has ready "scripts/release/security-patch-plan.sh recheck 1.1.1 $c4 --tip &&"
has ready 'scripts/release/ceremony.sh build 1.1.1'
lacks ready 'git tag -s'
has ready 'identical to v1.1.0'
has ready 'Nothing has been tagged or published'
[ -e "$scratch/postgres-started" ] || fail 'a ready plan runs the compatibility generator on scratch PostgreSQL'
grep -F "compat o/r " "$scratch/calls" | grep -F ' 1.1.1 ' >/dev/null || fail 'the compatibility script must be told the version'
lacks ready 'waited more than 24 hours'
has ready 'REVIEWED_MANIFEST_SHA256'
# The tag block and the ceremony lines are real: argument counts come from the ceremony's own usage checks.
check_ceremony_lines() { # name
	body | sed -n 's/^.*scripts\/release\/ceremony.sh \([a-z-]*\)\(.*\)$/\1\2/p' | sed 's/ *&&$//' >"$scratch/ceremony-lines"
	[ -s "$scratch/ceremony-lines" ] || fail "$1: no ceremony command found"
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
run_tag_block() { # name mode (last-line: swap the final line for true; sign: tag without a key)
	body | awk '/^```sh/ { n++; on = (n == 1); next } /^```/ { on = 0; next } on' >"$scratch/block.sh"
	clone="$scratch/clone-$1"
	git clone -q "$repo" "$clone"
	git -C "$clone" config user.email test@example.com
	git -C "$clone" config user.name test
	cp -R "$real_root/scripts" "$clone/scripts"
	# The real installer needs this repository's hooks; the block names it and the fixture checks it exists.
	printf '#!/bin/sh\nexit 0\n' >"$clone/scripts/git/install-hooks.sh"
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
	(cd "$clone" && env HIKYO_REPO_ROOT="$clone" GH_REPO=o/r sh -e "$scratch/block.sh" >"$scratch/block.out" 2>&1) || { cat "$scratch/block.out" >&2; fail "$1: the tag block does not run"; }
	grep -F "ok to tag v1.1.1 at" "$scratch/block.out" >/dev/null || { cat "$scratch/block.out" >&2; fail "$1: the recheck did not pass"; }
	rm -rf "$clone"
}
run_tag_block tip last-line

# A red tip: the same commit, tagged by hand because the ceremony tags only the tip.
plan 'ready below a red tip' 0 GREEN="$c0 $c1 $c2 $c3"
expect_action 'red tip' open
has 'red tip' "\`v1.1.1\` at \`$c3\`"
has 'red tip' "git switch --detach $c3 &&"
has 'red tip' "recheck 1.1.1 $c3 &&"
has 'red tip' "git tag -s v1.1.1 -m \"Hikyo 1.1.1\" $c3 &&"
has 'red tip' 'git push origin refs/tags/v1.1.1'
has 'red tip' 'scripts/git/install-hooks.sh &&'
has 'red tip' "the main tip $c4 is not green"
lacks 'red tip' 'ceremony.sh build'
lacks 'red tip' ' --tip'
check_ceremony_lines 'red tip'
run_tag_block red sign
[ "$(git -C "$repo" rev-parse 'v1.1.1^{commit}')" = "$c3" ] || fail 'the red-tip block must push the tag at the planned commit'
git -C "$repo" tag -d v1.1.1 >/dev/null

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
plan 'untrusted stable trust' 0 STABLE=untrusted
has 'untrusted' 'stable trust does not authenticate'
plan 'candidate refused' 0 STABLE=badcandidate
has 'candidate refused' 'not a valid successor'
plan 'compatibility differs' 0 COMPAT=differs
expect_action 'compat differs' open
has 'compat differs' 'the compatibility declaration differs'
no_tag_commands 'compat differs'
plan 'a compatibility lookup failure stops the run' 1 COMPAT=lookup
plan 'a generator failure stops the run' 1 STABLE=nodecl
plan 'a scratch database that does not start stops the run' 1 POSTGRES=fail
plan 'a stray higher stable tag' 0 TAGS_EXTRA=v1.1.2
has 'stray tag' 'a stable tag at or above v1.1.1 already exists'
no_tag_commands 'stray tag'
plan 'a release already exists for the version' 0 RELEASE_EXISTS=1
has 'release exists' 'already has a release'
plan 'the green source proof cannot be read' 1 GREEN_READ=fail
[ ! -e "$scratch/postgres-started" ] || fail 'PostgreSQL must start only when a tag is otherwise ready'
plan 'the version is tagged and its draft is staged' 0 TAGS_EXTRA=v1.1.1 REL_DRAFT=v1.1.1
has 'drafted' 'v1.1.1 is staged'
has 'drafted' 'scripts/release/ceremony.sh review 1.1.1'
no_tag_commands 'drafted'
check_ceremony_lines 'drafted'
plan 'the version is tagged without a release' 0 TAGS_EXTRA=v1.1.1
has 'tagged' 'is tagged but has no release'
no_tag_commands 'tagged'
plan 'published prerelease ahead of trust' 0 REL_EXTRA=v1.2.0-rc.1
has 'trust behind' 'synchronize trust first'
has 'trust behind' 'scripts/release/ceremony.sh sync-trust 1.2.0-rc.1'
no_tag_commands 'trust behind'
check_ceremony_lines 'trust behind'

# --- nothing pending --------------------------------------------------------
plan 'a merge the latest release already holds' 0 SEC1="$c0"
expect_action 'released' close
plan 'an unmerged security update is not pending' 0 ALERTS=unmerged
expect_action 'unmerged' close
plan 'an impostor author is not a security update' 0 ALERTS=impostor
expect_action 'impostor' close
plan 'an alert fixed before the latest release is not an alarm' 0 SEC1="$c0" ALERTS=old-unlinked
expect_action 'old alert' close
# --- alarms: whatever else the plan says, an unexplained fix reaches the issue -
plan 'a fixed alert without a pull request' 0 SEC1="$c0" ALERTS=unlinked
expect_action 'unlinked' open
has 'unlinked' 'Dependabot alert(s) were fixed'
plan 'a fixed alert whose pull request is not merged' 0 SEC1="$c0" ALERTS=unlinked-unmerged
expect_action 'unlinked unmerged' open
has 'unlinked unmerged' 'Dependabot alert(s) were fixed'
plan 'a fixed alert without a time' 0 SEC1="$c0" ALERTS=unlinked-nofixedat
expect_action 'no fixedAt' open
plan 'an alarm beside a ready tag' 0 ALERTS=unlinked
has 'alarm beside ready' 'Also needs a person'
has 'alarm beside ready' 'Security patch v1.1.1 is ready'
plan 'a fixed alert with an unreadable time stops the run' 1 SEC1="$c0" ALERTS=unlinked FIXED_AT=garbage
plan 'a merge commit that is not in history is raised, not skipped' 0 SEC1="ffffffffffffffffffffffffffffffffffffffff"
expect_action 'missing commit' open
has 'missing commit' 'is not in main'
# A merge time is validated for every state, even those that never use it for a decision.
plan 'a merge without a merge commit is raised, not skipped' 0 NO_COMMIT=1
expect_action 'no merge commit' open
has 'no merge commit' 'GitHub reports no merge commit'
plan 'a merge outside the history of the tip is raised, not skipped' 0 SEC1="$side"
expect_action 'side merge' open
has 'side merge' "not in main's history"
plan 'a merge that is not on the first-parent history blocks' 0 SEC1="$side" GITHUB_SHA="$m" GREEN="$m"
expect_action 'off first parent' open
has 'off first parent' 'not on main first-parent history'
no_tag_commands 'off first parent'
plan 'a commit whose trust metadata is unreadable stops the run' 1 GITHUB_SHA="$cbad" GREEN="$cbad $c4"
plan 'the newest merge sits in a commit that does not record the latest release' 0 SEC1="$c1" GREEN="$c1"
expect_action 'stale trust commit' open
has 'stale trust commit' 'cannot be prepared'
no_tag_commands 'stale trust commit'
plan 'a merge time that is not a timestamp stops the run' 1 MERGED_AT=garbage
plan 'a merge time that is not a timestamp stops a staged release too' 1 MERGED_AT=garbage TAGS_EXTRA=v1.1.1 REL_DRAFT=v1.1.1
plan 'a merge time that date would read as a word stops the run' 1 MERGED_AT=tomorrow
# A date that reads leniently (GNU date accepts words) must not slip past the strict format.
mkdir "$scratch/gnudate"
cat >"$scratch/gnudate/date" <<'SH'
#!/bin/sh
case "$*" in *" -d ${DATE_FOR:-tomorrow} "*) printf '%s\n' "${DATE_OUT:-1}" ;; *) exec /bin/date "$@" ;; esac
SH
chmod +x "$scratch/gnudate/date"
plan 'a lenient date reading of a word is not accepted' 1 PATH="$scratch/gnudate:$PATH" MERGED_AT=tomorrow
plan 'a date result that is not a number is not accepted' 1 PATH="$scratch/gnudate:$PATH" MERGED_AT=2026-11-14T12:00:00Z DATE_FOR=2026-11-14T12:00:00Z DATE_OUT=soon TAGS_EXTRA=v1.1.1 REL_DRAFT=v1.1.1
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
issue_json() { jq -nc --arg title "$1" --arg body "$2" --arg author "${3:-app/github-actions}" '[{number:5,title:$title,body:$body,author:{login:$author}}]'; }
title='Security patch release: maintainer action needed'
echo '[]' >"$scratch/issues.json"
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
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json")" someone >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
{ [ -e "$scratch/created" ] && [ ! -e "$scratch/edited" ]; } || fail 'an issue with our marker from another author is not ours'
issue_json "$title" "$(jq -r .body "$scratch/ready-plan.json")" fake-github-actions >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
[ -e "$scratch/created" ] || fail 'the author name is matched exactly'
issue_json "$title" "$(printf '<!-- security-patch-plan -->\nold')" 'github-actions[bot]' >"$scratch/issues.json"
apply "$script" issue "$scratch/ready-plan.json"
{ [ -e "$scratch/edited" ] && [ ! -e "$scratch/created" ]; } || fail 'the REST spelling of the bot is ours'
plan 'released' 0 SEC1="$c0"
issue_json "$title" "$(printf '<!-- security-patch-plan -->\nold')" >"$scratch/issues.json"
cp "$scratch/plan.json" "$scratch/close-plan.json"
apply "$script" issue "$scratch/close-plan.json"
[ -e "$scratch/closed" ] || fail 'a plan to close closes our open issue'
echo '[]' >"$scratch/issues.json"
apply "$script" issue "$scratch/close-plan.json"
[ ! -e "$scratch/closed" ] || fail 'closing with no open issue does nothing'
issue_json "$title" "$(printf '<!-- security-patch-plan -->\nold')" someone >"$scratch/issues.json"
apply "$script" issue "$scratch/close-plan.json"
[ ! -e "$scratch/closed" ] || fail 'another author is not closed'
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
	env GH_REPO=o/r "$@" "$script" recheck "$version" "$commit" ${opt:+"$opt"} >"$scratch/out" 2>&1 || status=$?
	[ "$status" = "$want" ] || { cat "$scratch/out" >&2; fail "recheck $name: exit $status, want $want"; }
	grep -F -- "$fragment" "$scratch/out" >/dev/null || { cat "$scratch/out" >&2; fail "recheck $name: expected: $fragment"; }
}
git -C "$repo" checkout -q --detach "$c4"
recheck 'passes at the tip' 0 "ok to tag v1.1.1 at $c4" 1.1.1 "$c4" --tip
recheck 'a checkout at another commit' 3 'this checkout is not at' 1.1.1 "$c3" -
git -C "$repo" checkout -q --detach "$c3"
recheck 'passes at an older green commit' 0 "ok to tag v1.1.1 at $c3" 1.1.1 "$c3" -
if grep -F 'compat' "$scratch/calls" >/dev/null; then fail 'the recheck does not rerun the compatibility generator'; fi
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
git -C "$repo" checkout -q --detach "$x"
recheck 'a commit that main does not contain' 3 'not on main history' 1.1.1 "$x" -
git -C "$repo" checkout -q --detach "$c3"
recheck 'a bad option' 1 'only option is --tip' 1.1.1 "$c3" --bogus
git -C "$repo" checkout -q --detach "$c1"
recheck 'trust that does not record the latest release' 3 'trust ends at v1.0.0' 1.1.1 "$c1" - SEC1="$c1"
git -C "$repo" checkout -q --detach "$c4"
if "$script" recheck 1.1.1 >/dev/null 2>&1; then fail 'recheck needs a version and a commit'; fi

printf 'security-patch-plan fixture: the plan, the issue, its commands and the recheck behave in every state\n'

#!/bin/sh
# Drives nightly-backmerge.sh with a stub gh that records every call. Reads must
# use the workflow token and every write the App token.
# shellcheck disable=SC2089,SC2090 # The stub reads these JSON documents from the environment.
set -eu

script=${NIGHTLY_BACKMERGE_SCRIPT:-"$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/nightly-backmerge.sh"}
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-backmerge-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
main=$(printf 'f%.0s' $(seq 40))
dev=$(printf 'a%.0s' $(seq 40))
merged=$(printf 'd%.0s' $(seq 40))
done_sha=$(printf 'c%.0s' $(seq 40))
other=$(printf 'e%.0s' $(seq 40))
branch=backmerge/auto-20261124-fffffff
old=backmerge/auto-20261120-1234567
now=1793600000
merge='{"parents":[{},{}]}'
plain='{"parents":[{}]}'
content="{\"status\":\"diverged\",\"total_commits\":2,\"commits\":[$merge,$plain]}"
docs='https://github.com/o/r/blob/main/docs/development/branching.md#manual-back-merge'

cat >"$work/gh" <<'STUB'
#!/bin/sh
case "$1" in
api) shift ;;
pr)
	shift
	printf '%s|pr %s\n' "$GH_TOKEN" "$*" >>"$CALLS"
	exit 0
	;;
*) exit 9 ;;
esac
filter=
method=GET
fields=
url=
while [ "$#" -gt 0 ]; do
	case "$1" in
	--paginate) ;;
	--jq) filter=$2; shift ;;
	-X) method=$2; shift ;;
	-f)
		case "$2" in
		body=*) printf '%s' "${2#body=}" >"$BODY"; fields="$fields body=..." ;;
		*) fields="$fields $2" ;;
		esac
		shift
		;;
	*) url=$1 ;;
	esac
	shift
done
fail_with() {
	printf 'gh: %s\n' "$1" >&2
	exit 1
}
if [ "$method" != GET ]; then
	printf '%s|%s %s%s\n' "$GH_TOKEN" "$method" "$url" "$fields" >>"$CALLS"
	case "$method $url" in
	'POST repos/o/r/pulls') printf '{"number":88}' | jq -r "$filter" ;;
	'POST repos/o/r/git/refs') printf '%s' "$DEV_REF" >"$BRANCH_FILE" ;;
	'POST repos/o/r/merges')
		case "$MERGE" in
		409) fail_with 'Merge conflict (HTTP 409)' ;;
		500) fail_with 'Server Error (HTTP 500)' ;;
		201) printf '%s' "$MERGED" >"$BRANCH_FILE" ;;
		esac
		;;
	'DELETE '*)
		[ -z "${DELETE_MISSING:-}" ] || fail_with 'Reference does not exist (HTTP 422)'
		# The fixture has one branch to remember: a delete forgets it.
		printf '404' >"$BRANCH_FILE"
		;;
	esac
	exit 0
fi
printf '%s|GET %s\n' "$GH_TOKEN" "$url" >>"$READS"
case "$url" in
repos/o/r/git/ref/heads/dev)
	[ "$DEV_REF" != 404 ] || fail_with 'Not Found (HTTP 404)'
	json="{\"object\":{\"sha\":\"$DEV_REF\"}}"
	;;
repos/o/r/git/ref/heads/backmerge/auto-*)
	sha=$(cat "$BRANCH_FILE")
	[ "$sha" != 404 ] || fail_with 'Not Found (HTTP 404)'
	json="{\"object\":{\"sha\":\"$sha\"}}"
	;;
repos/o/r/git/matching-refs/heads/backmerge/auto-) json=$REFS ;;
repos/o/r/compare/dev...$MAIN) json=$COMPARE ;;
repos/o/r/compare/dev...$DONE) json='{"status":"behind"}' ;;
repos/o/r/compare/dev...*) json='{"status":"ahead"}' ;;
repos/o/r/pulls\?*) json=$PULLS ;;
repos/o/r/pulls/*) json=$PULL ;;
*) exit 8 ;;
esac
if [ -n "$filter" ]; then
	printf '%s' "$json" | jq -r "$filter"
else
	printf '%s' "$json"
fi
STUB
chmod +x "$work/gh"

fail() {
	printf 'nightly backmerge fixture failed: %s\n' "$1" >&2
	exit 1
}
# Defaults describe an ordinary back-merge; each case overrides what it tests.
reset() {
	DEV_REF=$dev COMPARE=$content PULLS='[]' PULL='{}' REFS='[]' MERGE=201 DELETE_MISSING='' DATE=20261124
	MAIN=$main DONE=$done_sha MERGED=$merged
	printf '404' >"$work/branch"
	export DEV_REF COMPARE PULLS PULL REFS MERGE DELETE_MISSING MAIN DONE MERGED
}
run() {
	for log in calls reads; do : >"$work/$log"; done
	status=0
	GH_BIN=$work/gh CALLS=$work/calls READS=$work/reads BODY=$work/body BRANCH_FILE=$work/branch \
		GH_REPO=o/r GITHUB_SHA=$main GH_TOKEN=read-token APP_TOKEN=app-token \
		BACKMERGE_NOW=$now BACKMERGE_DATE=$DATE GITHUB_STEP_SUMMARY=$work/summary \
		"$script" >"$work/out" 2>"$work/err" || status=$?
	if grep -v '^read-token|' "$work/reads" >/dev/null; then fail 'a read used the App token'; fi
	if grep -v '^app-token|' "$work/calls" >/dev/null; then fail 'a write used the workflow token'; fi
}
expect_ok() {
	run
	[ "$status" -eq 0 ] || fail "$1 exited $status: $(cat "$work/err")"
}
expect_calls() {
	if [ -n "$2" ]; then printf '%s\n' "$2"; fi >"$work/want"
	cmp -s "$work/want" "$work/calls" || fail "$1 made the wrong writes:
$(cat "$work/calls")"
}
# Fails the run: $2 are the writes made before it stopped, $3 must be on stderr.
expect_stuck() {
	run
	[ "$status" -ne 0 ] || fail "$1 was accepted"
	expect_calls "$1" "$2"
	grep -F -- "$3" "$work/err" >/dev/null || fail "$1 did not say '$3': $(cat "$work/err")"
}
expect_refused() { expect_stuck "$1" '' "$2"; }
# Arguments: number, head ref, repository, author id (the release App by default).
pulls() { printf '[{"number":%s,"user":{"id":%s},"head":{"ref":"%s","sha":"%s","repo":{"full_name":"%s"}}}]' "$1" "${4:-320493937}" "$2" "$other" "${3:-O/R}"; }
# Arguments: mergeable, mergeable_state, auto_merge, age in seconds.
detail() {
	created=$(date -u -r $((now - ${4:-60})) +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$((now - ${4:-60}))" +%Y-%m-%dT%H:%M:%SZ)
	printf '{"mergeable":%s,"mergeable_state":"%s","auto_merge":%s,"created_at":"%s","head":{"sha":"%s"}}' "$1" "$2" "$3" "$created" "$other"
}

create_ref="app-token|POST repos/o/r/git/refs ref=refs/heads/$branch sha=$dev"
merge_call="app-token|POST repos/o/r/merges base=$branch head=$main commit_message=Merge main fffffff into dev"
open_call="app-token|POST repos/o/r/pulls title=chore(release): back-merge main into dev head=$branch base=dev body=..."
arm_new="app-token|pr merge 88 --repo o/r --auto --merge --match-head-commit $merged"
fresh="$create_ref
$merge_call
$open_call
$arm_new"

reset
expect_ok 'new back-merge'
expect_calls 'new back-merge' "$fresh"
grep -F "$docs" "$work/body" >/dev/null || fail 'PR body lacks the manual route'
grep -F 'never main' "$work/body" >/dev/null || fail 'PR body lacks the head rule'

# A branch left over without an open pull request is deleted, then recreated.
reset
REFS="[{\"ref\":\"refs/heads/$branch\",\"object\":{\"sha\":\"$other\"}}]"
printf '%s' "$other" >"$work/branch"
expect_ok 'orphan of the same name'
expect_calls 'orphan of the same name' "app-token|DELETE repos/o/r/git/refs/heads/$branch
$create_ref
$merge_call
$open_call
$arm_new"

# A branch that cleanup did not remove is unexpected: stop before touching it.
reset
printf '%s' "$other" >"$work/branch"
expect_refused 'an existing branch' "$branch already exists at $other"

# 204: the branch already contains main, so the head stays at dev.
reset
MERGE=204
expect_ok 'merge 204'
expect_calls 'merge 204' "$create_ref
$merge_call
$open_call
app-token|pr merge 88 --repo o/r --auto --merge --match-head-commit $dev"

reset
MERGE=409
expect_stuck 'a conflict on a new branch' "$create_ref
$merge_call" "https://github.com/o/r/tree/$branch"
grep -F "$docs" "$work/err" >/dev/null || fail 'conflict lacks the manual route'
MERGE=500
printf '404' >"$work/branch"
expect_stuck 'a failing merge' "$create_ref
$merge_call" 'Server Error (HTTP 500)'

reset
COMPARE='{"status":"surprise","total_commits":1,"commits":[]}'
expect_refused 'an unknown compare status' 'unexpected compare status'
DEV_REF=404
expect_ok 'missing dev'
expect_calls 'missing dev' ''
grep -F 'dev does not exist' "$work/out" >/dev/null || fail 'missing dev not reported'

for s in identical behind; do
	reset
	COMPARE="{\"status\":\"$s\",\"total_commits\":0,\"commits\":[]}"
	expect_ok "$s"
	expect_calls "$s" ''
done

# Dev took main another way: an open automatic back-merge is closed, not left behind.
reset
COMPARE='{"status":"identical","total_commits":0,"commits":[]}'
PULLS=$(pulls 70 "$old")
expect_ok 'a redundant back-merge'
expect_calls 'a redundant back-merge' "app-token|POST repos/o/r/issues/70/comments body=...
app-token|PATCH repos/o/r/pulls/70 state=closed"

reset
COMPARE="{\"status\":\"ahead\",\"total_commits\":2,\"commits\":[$merge,$merge]}"
expect_ok 'merges only'
expect_calls 'merges only' ''
# A truncated list may hide a real commit.
COMPARE="{\"status\":\"ahead\",\"total_commits\":300,\"commits\":[$merge,$merge]}"
printf '404' >"$work/branch"
expect_ok 'truncated merges'
expect_calls 'truncated merges' "$fresh"

# An open back-merge takes the new main into its own branch.
upd="app-token|POST repos/o/r/merges base=$old head=$main commit_message=Merge main fffffff into dev"
reset
PULLS=$(pulls 70 "$old")
printf '%s' "$merged" >"$work/branch"
PULL=$(detail true clean '{"x":1}')
expect_ok 'updated back-merge'
expect_calls 'updated back-merge' "$upd"

reset
PULLS=$(pulls 70 "$old")
printf '%s' "$merged" >"$work/branch"
PULL=$(detail true blocked null)
expect_ok 'disarmed back-merge'
expect_calls 'disarmed back-merge' "$upd
app-token|pr merge 70 --repo o/r --auto --merge --match-head-commit $merged"

# GitHub has not computed mergeability yet: unknown, not stuck.
PULL=$(detail null unknown '{"x":1}')
expect_ok 'unknown mergeability'
expect_calls 'unknown mergeability' "$upd"

reset
PULLS=$(pulls 70 "$old")
printf '%s' "$merged" >"$work/branch"
MERGE=409
expect_stuck 'a conflict on an open back-merge' "$upd" 'https://github.com/o/r/pull/70'
PULL=$(detail false dirty '{"x":1}')
MERGE=201
expect_stuck 'a dirty back-merge' "$upd" 'conflicts with dev'
PULL=$(detail true dirty null)
expect_stuck 'a dirty state' "$upd" 'conflicts with dev'

# The re-arm comes before the age check.
reset
PULLS=$(pulls 70 "$old")
printf '%s' "$merged" >"$work/branch"
PULL=$(detail true blocked null 21601)
expect_stuck 'an old disarmed back-merge' "$upd
app-token|pr merge 70 --repo o/r --auto --merge --match-head-commit $merged" 'more than 6 hours'
PULL=$(detail true blocked '{"x":1}' 21601)
expect_stuck 'an old armed back-merge' "$upd" 'more than 6 hours'
PULL=$(detail true blocked '{"x":1}' 21600)
expect_ok 'a back-merge at the age limit'

# Cleanup: every exact-pattern auto branch that no open pull request uses.
del_old="app-token|DELETE repos/o/r/git/refs/heads/$old"
del_nov="app-token|DELETE repos/o/r/git/refs/heads/backmerge/auto-20261101-abcdef0"
refs="[{\"ref\":\"refs/heads/$old\",\"object\":{\"sha\":\"$done_sha\"}},{\"ref\":\"refs/heads/backmerge/auto-20261101-abcdef0\",\"object\":{\"sha\":\"$other\"}},{\"ref\":\"refs/heads/backmerge/auto-notes\",\"object\":{\"sha\":\"$done_sha\"}}]"
reset
REFS=$refs
expect_ok 'cleanup with a new back-merge'
expect_calls 'cleanup with a new back-merge' "$del_old
$del_nov
$fresh"
reset
REFS=$refs
DELETE_MISSING=1
expect_ok 'cleanup of a vanished branch'
reset
REFS=$refs
COMPARE='{"status":"identical","total_commits":0,"commits":[]}'
expect_ok 'cleanup after the back-merge landed'
expect_calls 'cleanup after the back-merge landed' "$del_old
$del_nov"
# The open pull request's own branch is kept.
reset
REFS=$refs
PULLS=$(pulls 70 "$old")
printf '%s' "$merged" >"$work/branch"
PULL=$(detail true clean '{"x":1}')
expect_ok 'cleanup keeps the open branch'
expect_calls 'cleanup keeps the open branch' "$del_nov
$upd"
# A pattern-matching branch from anyone else is ignored, and its branch kept.
reset
REFS=$refs
PULLS=$(pulls 93 "$old" o/r 5)
expect_ok 'a pull request from another author'
expect_calls 'a pull request from another author' "$del_nov
$fresh"
grep -F "$old" "$work/out" >/dev/null && fail 'the foreign branch was touched'

# A human back-merge, or an unrelated pull request into dev.
reset
PULLS=$(pulls 90 backmerge/main-into-dev-20261124)
expect_ok 'human back-merge'
expect_calls 'human back-merge' ''
PULLS=$(pulls 91 feature/x)
printf '404' >"$work/branch"
expect_ok 'unrelated pull request'
expect_calls 'unrelated pull request' "$fresh"
# A fork's branch of the same name is not ours.
PULLS=$(pulls 92 "$old" someone/r)
printf '404' >"$work/branch"
expect_ok 'fork branch'
expect_calls 'fork branch' "$fresh"

# Invalid API values stop the run.
reset
DEV_REF=nothex
expect_refused 'an invalid dev commit' 'invalid dev commit'
reset
DATE=2026-11-24
expect_refused 'an invalid date' 'invalid date'
reset
PULLS='[{"number":"x","user":{"id":320493937},"head":{"ref":"backmerge/auto-20261120-1234567","sha":"","repo":{"full_name":"o/r"}}}]'
expect_refused 'an invalid number' 'invalid pull request number'
reset
PULLS=$(pulls 70 "$old")
printf '%s' "$merged" >"$work/branch"
PULL=$(detail true '##[error]x' null)
expect_stuck 'an invalid merge state' "$upd" 'invalid merge state'
PULL='{"mergeable":true,"mergeable_state":"clean","auto_merge":null,"created_at":"yesterday","head":{"sha":"'$merged'"}}'
expect_stuck 'an invalid timestamp' "$upd" 'invalid creation time'
PULL=$(detail true clean null)
MERGE=204
printf 'nothex' >"$work/branch"
expect_stuck 'an invalid head commit' "$upd" 'invalid head commit'
reset
PULLS="[{\"number\":70,\"user\":{\"id\":320493937},\"head\":{\"ref\":\"$old\",\"sha\":\"$other\",\"repo\":{\"full_name\":\"o/r\"}}},{\"number\":71,\"user\":{\"id\":320493937},\"head\":{\"ref\":\"backmerge/auto-20261121-7654321\",\"sha\":\"$other\",\"repo\":{\"full_name\":\"o/r\"}}}]"
expect_refused 'two automatic back-merges' 'are both automatic back-merges'

printf 'nightly backmerge fixture passed\n'

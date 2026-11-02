#!/bin/sh
# Drives nightly-promote.sh with a stub gh and go that record every call. Reads
# must use the workflow token and every write the App token.
# shellcheck disable=SC2089,SC2090 # The stub reads these JSON documents from the environment.
set -eu

script=${NIGHTLY_PROMOTE_SCRIPT:-"$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/nightly-promote.sh"}
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-promote-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
main=$(printf 'f%.0s' $(seq 40))
dev=$(printf 'a%.0s' $(seq 40))
old=$(printf 'b%.0s' $(seq 40))
branch=release/nightly-20261102-aaaaaaa
stale=release/nightly-20261101-bbbbbbb
now=1793600000
# Compare results from main to the dev commit: a merge and a real commit, only merges.
merge='{"parents":[{},{}]}'
plain='{"parents":[{}]}'
content="{\"status\":\"diverged\",\"total_commits\":2,\"commits\":[$merge,$plain]}"

cat >"$work/gh" <<'STUB'
#!/bin/sh
case "$1" in
api) shift ;;
pr)
	shift
	printf '%s|pr %s\n' "$GH_TOKEN" "$*" >>"$CALLS"
	[ "${MERGE_FAIL:-}" != 1 ] || exit 1
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
	'DELETE '*) [ -z "${DELETE_MISSING:-}" ] || fail_with 'Reference does not exist (HTTP 422)' ;;
	esac
	exit 0
fi
printf '%s|GET %s\n' "$GH_TOKEN" "$url" >>"$READS"
ref() {
	case "$1" in
	404) fail_with 'Not Found (HTTP 404)' ;;
	error) fail_with 'Internal Server Error (HTTP 500)' ;;
	*) json="{\"object\":{\"sha\":\"$1\"}}" ;;
	esac
}
case "$url" in
repos/o/r/git/ref/heads/dev) ref "$DEV_REF" ;;
repos/o/r/git/ref/heads/release/nightly-*) ref "$BRANCH_REF" ;;
repos/o/r/compare/main...*)
	[ "$COMPARE" != error ] || fail_with 'Bad Gateway (HTTP 502)'
	json=$COMPARE
	;;
repos/o/r/commits/*/check-runs\?*) json=$CHECKS ;;
repos/o/r/pulls\?*) json=$PULLS ;;
*) exit 8 ;;
esac
if [ -n "$filter" ]; then
	printf '%s' "$json" | jq -r "$filter"
else
	printf '%s' "$json"
fi
STUB
cat >"$work/go" <<'STUB'
#!/bin/sh
printf '%s\n' "$*" >>"$PROOF_CALLS"
[ "${PROOF_FAIL:-}" != 1 ] || { echo 'source proof: no completed successful dev source' >&2; exit 1; }
printf '{"commit":"%s","run_id":77,"workflow_path":".github/workflows/ci-dev.yml"}\n' "$DEV_COMMIT"
STUB
chmod +x "$work/gh" "$work/go"

fail() {
	printf 'nightly promote fixture failed: %s\n' "$1" >&2
	exit 1
}
# Defaults describe an ordinary promotion; each case overrides what it tests.
reset() {
	DEV_REF=$main COMPARE=$content PULLS='[]' CHECKS='{"check_runs":[]}' BRANCH_REF=404
	DEV_COMMIT=$dev PROOF_FAIL='' MERGE_FAIL='' DELETE_MISSING=''
	export DEV_REF COMPARE CHECKS PULLS BRANCH_REF DEV_COMMIT PROOF_FAIL MERGE_FAIL DELETE_MISSING
}
run() {
	for log in calls reads proof; do : >"$work/$log"; done
	status=0
	GH_BIN=$work/gh GO_BIN=$work/go CALLS=$work/calls READS=$work/reads PROOF_CALLS=$work/proof BODY=$work/body \
		GH_REPO=o/r GITHUB_SHA=$main GH_TOKEN=read-token APP_TOKEN=app-token PROMOTE_DATE=20261102 \
		PROMOTE_NOW=$now GITHUB_STEP_SUMMARY=$work/summary \
		"$script" >"$work/out" 2>"$work/err" || status=$?
	if grep -v '^read-token|' "$work/reads" >/dev/null; then fail 'a read used the App token'; fi
	if grep -v '^app-token|' "$work/calls" >/dev/null; then fail 'a write used the workflow token'; fi
}
expect_ok() {
	run
	[ "$status" -eq 0 ] || fail "$1 exited $status: $(cat "$work/err")"
}
expect_refused() {
	run
	[ "$status" -ne 0 ] || fail "$1 was accepted"
	[ ! -s "$work/calls" ] || fail "$1 wrote: $(cat "$work/calls")"
}
expect_calls() {
	if [ -n "$2" ]; then printf '%s\n' "$2"; fi >"$work/want"
	cmp -s "$work/want" "$work/calls" || fail "$1 made the wrong writes:
$(cat "$work/calls")"
}
# Arguments: number, head, ref, auto_merge, repository, age in seconds.
pull() {
	created=$(date -u -r $((now - ${6:-60})) +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$((now - ${6:-60}))" +%Y-%m-%dT%H:%M:%SZ)
	printf '[{"number":%s,"head":{"sha":"%s","ref":"%s","repo":{"full_name":"%s"}},"auto_merge":%s,"created_at":"%s"}]' "$1" "$2" "$3" "${5:-O/R}" "$4" "$created"
}
check() { printf '{"check_runs":[{"id":1,"status":"%s","conclusion":%s,"output":{"title":"%s","summary":"%s"}}]}' "$1" "$2" "$3" "$4"; }

queue="app-token|pr merge 88 --repo o/r --auto --merge --match-head-commit $dev"
create="app-token|POST repos/o/r/git/refs ref=refs/heads/$branch sha=$dev
app-token|POST repos/o/r/pulls title=chore(release): promote dev aaaaaaa to main for the nightly head=$branch base=main body=...
$queue"

reset
expect_ok 'ordinary promotion'
expect_calls 'ordinary promotion' "$create"
grep -F -- "--mode dev --latest --authority-commit $main" "$work/proof" >/dev/null || fail 'dev commit not selected under main authority'
grep -F "$dev" "$work/body" >/dev/null || fail 'PR body lacks the commit'
grep -F 'https://github.com/o/r/actions/runs/77' "$work/body" >/dev/null || fail 'PR body lacks the proof run'

# Statuses are the dev commit relative to main.
reset
COMPARE="{\"status\":\"ahead\",\"total_commits\":1,\"commits\":[$plain]}"
export COMPARE
expect_ok 'dev ahead of main'
expect_calls 'dev ahead of main' "$create"

for state in behind identical; do
	reset
	COMPARE="{\"status\":\"$state\",\"total_commits\":0,\"commits\":[]}"
	export COMPARE
	expect_ok "main already containing dev ($state)"
	expect_calls "main already containing dev ($state)" ''
done

# A backmerge alone has only a merge commit, whether or not main moved after it.
for state in ahead diverged; do
	reset
	COMPARE="{\"status\":\"$state\",\"total_commits\":1,\"commits\":[$merge]}"
	export COMPARE
	expect_ok "dev with only a backmerge ($state)"
	expect_calls "dev with only a backmerge ($state)" ''
	grep -F 'only merge commits' "$work/out" >/dev/null || fail "no notice for a backmerge-only dev ($state)"
done

reset
COMPARE="{\"status\":\"diverged\",\"total_commits\":3,\"commits\":[$merge,$plain,$merge]}"
export COMPARE
expect_ok 'dev with a real commit between merges'
expect_calls 'dev with a real commit between merges' "$create"

reset
COMPARE="{\"status\":\"diverged\",\"total_commits\":40,\"commits\":[$merge,$merge]}"
export COMPARE
expect_ok 'truncated commit list'
expect_calls 'truncated commit list' "$create"

reset
DEV_REF=404
export DEV_REF
expect_ok 'no dev branch'
expect_calls 'no dev branch' ''
[ ! -s "$work/proof" ] || fail 'source proof ran without a dev branch'
grep -F 'dev does not exist yet' "$work/out" >/dev/null || fail 'no notice for a missing dev branch'

reset
DEV_REF=error
export DEV_REF
expect_refused 'dev lookup failure'

reset
PROOF_FAIL=1
export PROOF_FAIL
expect_refused 'missing green dev proof'

reset
COMPARE=surprise
export COMPARE
expect_refused 'unexpected compare status'
reset
COMPARE=error
export COMPARE
expect_refused 'compare failure'

rearm="app-token|pr merge 41 --repo o/r --auto --merge --match-head-commit $dev"
armed='{"merge_method":"merge"}'
hours4=14400

reset
PULLS=$(pull 41 "$dev" "$branch" null)
export PULLS
expect_ok 'unarmed young promotion of the same commit'
expect_calls 'unarmed young promotion of the same commit' "$rearm"

reset
PULLS=$(pull 41 "$dev" "$branch" "$armed")
export PULLS
expect_ok 'armed young promotion with no check yet'
expect_calls 'armed young promotion with no check yet' ''

reset
PULLS=$(pull 41 "$dev" "$branch" "$armed" O/R 1800)
CHECKS=$(check in_progress null 'Admission pending' 'Waiting for feedback')
export PULLS CHECKS
expect_ok 'armed half-hour-old promotion with a pending check'
expect_calls 'armed half-hour-old promotion with a pending check' ''

reset
PULLS=$(pull 41 "$dev" "$branch" "$armed")
CHECKS=$(check completed '"success"' 'Ready' 'Admitted')
export PULLS CHECKS
expect_ok 'armed young promotion with an admitted check'

reset
PULLS=$(pull 41 "$dev" "$branch" "$armed")
CHECKS=$(check completed '"failure"' 'Admission refused' 'Needs approval\n::error::forged command\nmid ##[error]forged')
export PULLS CHECKS
run
[ "$status" -ne 0 ] || fail 'a refused promotion stayed green'
[ ! -s "$work/calls" ] || fail "a refused promotion wrote: $(cat "$work/calls")"
for text in 'https://github.com/o/r/pull/41' 'ci-queue-required check is completed/failure' 'Admission refused' 'Needs approval'; do
	grep -F "$text" "$work/err" >/dev/null || fail "refusal report lacks: $text"
done
if grep -E '^::' "$work/err" "$work/out" >/dev/null; then fail 'check text can start a workflow command'; fi
if grep -F '##[' "$work/err" "$work/out" >/dev/null; then fail 'check text can embed a workflow command'; fi
if grep -F 'Admission refused' "$work/summary" >/dev/null 2>&1; then fail 'check text reached the step summary'; fi

reset
PULLS=$(pull 41 "$dev" "$branch" null)
CHECKS=$(check completed '"failure"' 'Admission refused' 'Needs approval')
export PULLS CHECKS
run
[ "$status" -ne 0 ] || fail 'a refused disarmed promotion stayed green'
expect_calls 'a refused disarmed promotion' "$rearm"

reset
PULLS=$(pull 41 "$dev" "$branch" "$armed" O/R "$hours4")
CHECKS=$(check in_progress null 'Admission pending' 'Waiting for approval')
export PULLS CHECKS
run
[ "$status" -ne 0 ] || fail 'an old pending promotion stayed green'
[ ! -s "$work/calls" ] || fail 'an old pending promotion wrote'
grep -F 'not landed after more than 3 hours' "$work/err" >/dev/null || fail 'old promotion report lacks its reason'

reset
PULLS=$(pull 41 "$dev" "$branch" "$armed" O/R "$hours4")
export PULLS
run
[ "$status" -ne 0 ] || fail 'an old promotion with no check stayed green'

# A retry is armed, but a promotion failing night after night must still report.
reset
PULLS=$(pull 41 "$dev" "$branch" null O/R "$hours4")
CHECKS=$(check completed '"success"' 'Ready' 'Admitted')
export PULLS CHECKS
run
[ "$status" -ne 0 ] || fail 'an old promotion armed again stayed green'
expect_calls 'an old promotion armed again' "$rearm"

# Replacing a promotion that could not land still fails the run, after replacing it.
reset
PULLS=$(pull 41 "$old" "$stale" "$armed")
CHECKS=$(check completed '"failure"' 'Admission refused' 'Needs approval')
export PULLS CHECKS
run
[ "$status" -ne 0 ] || fail 'replacing a refused promotion stayed green'
grep -F 'promotion #41' "$work/err" >/dev/null || fail 'replaced promotion report lacks its number'
grep -F "app-token|PATCH repos/o/r/pulls/41 state=closed" "$work/calls" >/dev/null || fail 'refused older promotion was not replaced'
grep -F "POST repos/o/r/pulls" "$work/calls" >/dev/null || fail 'refused older promotion left no new promotion'

reset
PULLS=$(pull 41 "$old" "$stale" "$armed" O/R "$hours4")
CHECKS=$(check in_progress null 'Admission pending' 'Waiting for approval')
export PULLS CHECKS
run
[ "$status" -ne 0 ] || fail 'replacing an old pending promotion stayed green'
grep -F 'not landed after more than 3 hours' "$work/err" >/dev/null || fail 'replaced old promotion report lacks its reason'

reset
PULLS=$(pull 41 "$old" "$stale" null)
export PULLS
expect_ok 'promotion of an older commit'
expect_calls 'promotion of an older commit' "app-token|POST repos/o/r/issues/41/comments body=...
app-token|PATCH repos/o/r/pulls/41 state=closed
app-token|DELETE repos/o/r/git/refs/heads/$stale
$create"
DELETE_MISSING=1
export DELETE_MISSING
expect_ok 'promotion of an older commit whose branch is gone'

reset
PULLS=$(pull 41 "$old" "$stale" null fork/o-r)
export PULLS
expect_ok 'fork pull request with a promotion name'
expect_calls 'fork pull request with a promotion name' "$create"

reset
PULLS=$(pull 41 "$old" 'release/nightly-oops' null)
export PULLS
expect_refused 'foreign release/nightly- pull request'

reset
BRANCH_REF=$dev
export BRANCH_REF
expect_ok 'existing branch at the commit'
expect_calls 'existing branch at the commit' "app-token|POST repos/o/r/pulls title=chore(release): promote dev aaaaaaa to main for the nightly head=$branch base=main body=...
$queue"

reset
BRANCH_REF=$old
export BRANCH_REF
expect_refused 'existing branch elsewhere'

reset
MERGE_FAIL=1
export MERGE_FAIL
run
[ "$status" -ne 0 ] || fail 'a failed queue request was reported as success'

printf 'nightly promote fixture: no-ops, refusals, stuck promotions, in-flight handling and the exact write sequence behaved\n'

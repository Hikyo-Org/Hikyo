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
repos/o/r/compare/*)
	[ "$COMPARE" != error ] || fail_with 'Bad Gateway (HTTP 502)'
	json="{\"status\":\"$COMPARE\"}"
	;;
repos/o/r/commits/main) json="{\"commit\":{\"tree\":{\"sha\":\"$MAIN_TREE\"}}}" ;;
repos/o/r/commits/*) json="{\"commit\":{\"tree\":{\"sha\":\"$COMMIT_TREE\"}}}" ;;
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
	DEV_REF=$main COMPARE=diverged COMMIT_TREE=t1 MAIN_TREE=t2 PULLS='[]' BRANCH_REF=404
	DEV_COMMIT=$dev PROOF_FAIL='' MERGE_FAIL='' DELETE_MISSING=''
	export DEV_REF COMPARE COMMIT_TREE MAIN_TREE PULLS BRANCH_REF DEV_COMMIT PROOF_FAIL MERGE_FAIL DELETE_MISSING
}
run() {
	for log in calls reads proof; do : >"$work/$log"; done
	status=0
	GH_BIN=$work/gh GO_BIN=$work/go CALLS=$work/calls READS=$work/reads PROOF_CALLS=$work/proof BODY=$work/body \
		GH_REPO=o/r GITHUB_SHA=$main GH_TOKEN=read-token APP_TOKEN=app-token PROMOTE_DATE=20261102 \
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
pull() { printf '[{"number":%s,"head":{"sha":"%s","ref":"%s","repo":{"full_name":"%s"}},"auto_merge":%s}]' "$1" "$2" "$3" "${5:-O/R}" "$4"; }

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

reset
COMPARE=behind
export COMPARE
expect_ok 'dev ahead of main'
expect_calls 'dev ahead of main' "$create"

for state in ahead identical; do
	reset
	COMPARE=$state
	export COMPARE
	expect_ok "main already containing dev ($state)"
	expect_calls "main already containing dev ($state)" ''
done

reset
COMPARE=behind MAIN_TREE=t1
export COMPARE MAIN_TREE
expect_ok 'dev with the content of main'
expect_calls 'dev with the content of main' ''

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

reset
PULLS=$(pull 41 "$dev" "$branch" null)
export PULLS
expect_ok 'unarmed promotion of the same commit'
expect_calls 'unarmed promotion of the same commit' "app-token|pr merge 41 --repo o/r --auto --merge --match-head-commit $dev"

reset
PULLS=$(pull 41 "$dev" "$branch" '{"merge_method":"merge"}')
export PULLS
expect_ok 'armed promotion of the same commit'
expect_calls 'armed promotion of the same commit' ''

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

printf 'nightly promote fixture: no-ops, refusals, in-flight handling and the exact write sequence behaved\n'

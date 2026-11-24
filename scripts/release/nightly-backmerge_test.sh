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
now=1793600000
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
	[ "$method $url" != 'POST repos/o/r/pulls' ] || printf '%s' "$CREATED" | jq -r "$filter"
	exit 0
fi
printf '%s|GET %s\n' "$GH_TOKEN" "$url" >>"$READS"
case "$url" in
repos/o/r/git/ref/heads/dev)
	[ "$DEV_REF" != 404 ] || fail_with 'Not Found (HTTP 404)'
	[ "$DEV_REF" != error ] || fail_with 'Internal Server Error (HTTP 500)'
	json="{\"object\":{\"sha\":\"$DEV_REF\"}}"
	;;
repos/o/r/compare/dev...main) json=$COMPARE ;;
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
	DEV_REF=$dev COMPARE=$content PULLS='[]' PULL='{}' CREATED='{"number":88}'
	export DEV_REF COMPARE PULLS PULL CREATED
}
run() {
	for log in calls reads; do : >"$work/$log"; done
	status=0
	GH_BIN=$work/gh CALLS=$work/calls READS=$work/reads BODY=$work/body \
		GH_REPO=o/r GITHUB_SHA=$main GH_TOKEN=read-token APP_TOKEN=app-token \
		BACKMERGE_NOW=$now GITHUB_STEP_SUMMARY=$work/summary \
		"$script" >"$work/out" 2>"$work/err" || status=$?
	if grep -v '^read-token|' "$work/reads" >/dev/null; then fail 'a read used the App token'; fi
	if grep -v '^app-token|' "$work/calls" >/dev/null; then fail 'a write used the workflow token'; fi
}
expect_ok() {
	run
	[ "$status" -eq 0 ] || fail "$1 exited $status: $(cat "$work/err")"
}
# Refused cases never write; $2, if set, must appear on stderr.
expect_refused() {
	run
	[ "$status" -ne 0 ] || fail "$1 was accepted"
	[ ! -s "$work/calls" ] || fail "$1 wrote: $(cat "$work/calls")"
	[ -z "${2:-}" ] || grep -F -- "$2" "$work/err" >/dev/null || fail "$1 did not say '$2': $(cat "$work/err")"
}
expect_calls() {
	if [ -n "$2" ]; then printf '%s\n' "$2"; fi >"$work/want"
	cmp -s "$work/want" "$work/calls" || fail "$1 made the wrong writes:
$(cat "$work/calls")"
}
# Arguments: number, head ref, repository.
pulls() { printf '[{"number":%s,"head":{"ref":"%s","repo":{"full_name":"%s"}}}]' "$1" "$2" "${3:-O/R}"; }
# Arguments: mergeable, mergeable_state, auto_merge, age in seconds.
detail() {
	created=$(date -u -r $((now - ${4:-60})) +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$((now - ${4:-60}))" +%Y-%m-%dT%H:%M:%SZ)
	printf '{"mergeable":%s,"mergeable_state":"%s","auto_merge":%s,"created_at":"%s"}' "$1" "$2" "$3" "$created"
}

arm_call='app-token|pr merge 88 --repo o/r --auto --merge'
create="app-token|POST repos/o/r/pulls title=chore(release): back-merge main into dev head=main base=dev body=...
$arm_call"

reset
expect_ok 'ordinary back-merge'
expect_calls 'ordinary back-merge' "$create"
grep -F 'CI-definition' "$work/body" >/dev/null || fail 'PR body lacks its purpose'

reset
DEV_REF=404
expect_ok 'missing dev'
expect_calls 'missing dev' ''
grep -F 'dev does not exist' "$work/out" >/dev/null || fail 'missing dev not reported'
DEV_REF=error
expect_refused 'a dev ref read failure'

for s in identical behind; do
	reset
	COMPARE="{\"status\":\"$s\",\"total_commits\":0,\"commits\":[]}"
	expect_ok "$s"
	expect_calls "$s" ''
done
reset
COMPARE='{"status":"surprise","total_commits":1,"commits":[]}'
expect_refused 'an unknown compare status' 'unexpected compare status'

reset
COMPARE="{\"status\":\"ahead\",\"total_commits\":2,\"commits\":[$merge,$merge]}"
expect_ok 'merges only'
expect_calls 'merges only' ''
# A truncated list may hide a real commit.
COMPARE="{\"status\":\"ahead\",\"total_commits\":300,\"commits\":[$merge,$merge]}"
expect_ok 'truncated merges'
expect_calls 'truncated merges' "$create"

reset
PULLS=$(pulls 70 main)
PULL=$(detail true clean '{"merge_method":"MERGE"}')
expect_ok 'armed back-merge'
expect_calls 'armed back-merge' ''

reset
PULLS=$(pulls 88 main)
PULL=$(detail true blocked null)
expect_ok 'disarmed back-merge'
expect_calls 'disarmed back-merge' "$arm_call"

# GitHub has not computed mergeability yet: unknown, not stuck.
reset
PULLS=$(pulls 88 main)
PULL=$(detail null unknown '{"x":1}')
expect_ok 'unknown mergeability'
expect_calls 'unknown mergeability' ''

reset
PULLS=$(pulls 88 main)
PULL=$(detail false dirty '{"x":1}')
expect_refused 'a conflicting back-merge' 'https://github.com/o/r/pull/88'
grep -F 'backmerge/main-into-dev-<date>' "$work/err" >/dev/null || fail 'conflict lacks the manual route'
PULL=$(detail false dirty null)
expect_refused 'a conflicting disarmed back-merge' 'conflicts with dev'

reset
PULLS=$(pulls 88 main)
PULL=$(detail true blocked '{"x":1}' 21601)
expect_refused 'an over-age back-merge' 'more than 6 hours'
PULL=$(detail true blocked '{"x":1}' 21600)
expect_ok 'a back-merge at the age limit'

# A human back-merge, or an unrelated pull request into dev, changes what is opened.
reset
PULLS=$(pulls 90 backmerge/main-into-dev-20261124)
expect_ok 'human back-merge'
expect_calls 'human back-merge' ''
PULLS=$(pulls 91 feature/x)
expect_ok 'unrelated pull request'
expect_calls 'unrelated pull request' "$create"
# A fork's branch named main is not ours.
PULLS=$(pulls 92 main someone/r)
expect_ok 'fork main'
expect_calls 'fork main' "$create"

# Invalid API values stop the run before any write.
reset
CREATED='{"number":"x"}'
expect_refused_after_create() {
	run
	[ "$status" -ne 0 ] || fail "$1 was accepted"
}
expect_refused_after_create 'an invalid created number'
reset
PULLS=$(pulls 88 main)
PULL=$(detail true '##[error]x' null)
expect_refused 'an invalid merge state' 'invalid merge state'
reset
PULLS=$(pulls 88 main)
PULL='{"mergeable":true,"mergeable_state":"clean","auto_merge":null,"created_at":"yesterday"}'
expect_refused 'an invalid timestamp' 'invalid creation time'
reset
PULLS='[{"number":"x","head":{"ref":"main","repo":{"full_name":"o/r"}}}]'
expect_refused 'an invalid number' 'invalid pull request number'
reset
DEV_REF=nothex
expect_refused 'an invalid dev commit' 'invalid dev commit'

printf 'nightly backmerge fixture passed\n'

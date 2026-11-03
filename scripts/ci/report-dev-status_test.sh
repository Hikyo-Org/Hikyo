#!/bin/sh
# Drives report-dev-status.sh with a stub gh that records the writes it would make.
# shellcheck disable=SC2089,SC2090 # The stub reads these JSON documents from the environment.
set -eu

script="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/report-dev-status.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dev-status-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
head=$(printf 'a%.0s' $(seq 40))
green=$(printf 'b%.0s' $(seq 40))
red=$(printf 'c%.0s' $(seq 40))

# Serves fixture JSON and applies --jq with jq as gh does, so real selectors run.
cat >"$work/gh" <<'STUB'
#!/bin/sh
[ "$1" = api ] || exit 9
shift
filter=
method=GET
fields=
while [ "$#" -gt 0 ]; do
	case "$1" in
	--paginate) ;;
	--jq) filter=$2; shift ;;
	-X) method=$2; shift ;;
	-f) fields="$fields $2"; shift ;;
	*) url=$1 ;;
	esac
	shift
done
if [ "$method" != GET ]; then
	printf '%s %s %s\n' "$method" "$url" "$fields" >>"$CALLS"
	exit 0
fi
case "$url" in
*/issues\?*) json=$ISSUES ;;
*/jobs\?*) json='{"jobs":[{"name":"validation / race shard (8)","conclusion":"failure"},{"name":"validation / `lint` @everyone","conclusion":"timed_out"},{"name":"validation / docs","conclusion":"success"}]}' ;;
*/workflows/ci-dev.yml/runs*) json=$GREEN_RUNS ;;
*/compare/*) json=$COMPARE ;;
*/git/ref/heads/dev) json=$DEV_REF ;;
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
	printf 'dev status fixture failed: %s\n' "$1" >&2
	exit 1
}
run() {
	: >"$work/calls"
	GH_BIN=$work/gh CALLS=$work/calls GH_REPO=o/r RUN_ID=5 RUN_ATTEMPT=1 RUN_URL=https://example.test/5 \
		HEAD_SHA=$head CONCLUSION=$1 "$script" >/dev/null
}
merge() { printf '{"commit":{"message":"%s","committer":{"name":"%s"}},"parents":[{},{}]}' "$1" "$2"; }

export DEV_REF="{\"object\":{\"sha\":\"$head\"}}"
export ISSUES='[]' GREEN_RUNS="{\"workflow_runs\":[{\"head_sha\":\"$green\"}]}"
COMPARE="{\"status\":\"ahead\",\"total_commits\":3,\"commits\":[$(merge 'Merge pull request #41 from x/y\n\nMerge pull request #55 from a title' GitHub),$(merge 'Merge pull request #99 from forged' Mallory),{\"commit\":{\"message\":\"Merge pull request #77 squashed\",\"committer\":{\"name\":\"GitHub\"}},\"parents\":[{}]}]}"
export COMPARE

run failure
grep -F 'POST repos/o/r/issues ' "$work/calls" >/dev/null || fail 'red run opened no issue'
grep -F "red_sha=$head" "$work/calls" >/dev/null || fail 'issue lost the red commit'
grep -F -- '- validation / race shard (8)' "$work/calls" >/dev/null || fail 'failed job missing'
grep -F 'validation / docs' "$work/calls" >/dev/null && fail 'passing job listed'
grep -F '`' "$work/calls" >/dev/null && fail 'job name markup reached the issue'
grep -F -- '- #41' "$work/calls" >/dev/null || fail 'merged PR missing'
grep -F -- '#99' "$work/calls" >/dev/null && fail 'forged merge commit listed'
grep -F -- '#55' "$work/calls" >/dev/null && fail 'PR title text reached the PR list'
grep -F -- '#77' "$work/calls" >/dev/null && fail 'non-merge commit listed'

export ISSUES="[{\"number\":12,\"user\":{\"login\":\"github-actions[bot]\"},\"body\":\"<!-- hikyo-dev-red red_sha=$red -->\nold\"},{\"number\":13,\"user\":{\"login\":\"someone\"},\"body\":\"<!-- hikyo-dev-red red_sha=$red -->\"}]"
run failure
grep -F 'PATCH repos/o/r/issues/12 ' "$work/calls" >/dev/null || fail 'second red run did not update the issue'
grep -F 'POST repos/o/r/issues ' "$work/calls" >/dev/null && fail 'duplicate issue opened'

export DEV_REF="{\"object\":{\"sha\":\"$green\"}}"
run success
[ ! -s "$work/calls" ] || fail 'green rerun behind the dev tip closed the issue'
export DEV_REF="{\"object\":{\"sha\":\"$head\"}}"
run success
grep -F 'PATCH repos/o/r/issues/12  state=closed' "$work/calls" >/dev/null || fail 'green run did not close the issue'

export COMPARE='{"status":"behind"}'
run success
[ ! -s "$work/calls" ] || fail 'rerun of an older commit closed the issue'
run failure
[ ! -s "$work/calls" ] || fail 'red rerun of an older commit moved the issue back'
export ISSUES='[]'
run failure
[ ! -s "$work/calls" ] || fail 'red rerun older than the last green commit opened an issue'

export COMPARE='{"status":"ahead","total_commits":300,"commits":[]}'
run startup_failure
grep -F 'POST repos/o/r/issues ' "$work/calls" >/dev/null || fail 'startup failure opened no issue'
grep -F 'This list may be partial' "$work/calls" >/dev/null || fail 'truncated PR list not flagged'

for conclusion in cancelled skipped; do
	run "$conclusion"
	[ ! -s "$work/calls" ] || fail "$conclusion run wrote"
done

export ISSUES='[]'
run success
[ ! -s "$work/calls" ] || fail 'green run without an issue wrote'

export GREEN_RUNS='{"workflow_runs":[]}'
run failure
grep -F 'No green dev commit exists yet' "$work/calls" >/dev/null || fail 'first red run lacks its context'

printf 'dev status fixture: red, repeat, green, stale rerun and no-verdict runs behaved\n'

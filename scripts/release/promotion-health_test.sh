#!/bin/sh
# Drives promotion-health.sh with a stub gh that returns a fixed run listing.
set -eu

script=${PROMOTION_HEALTH_SCRIPT:-"$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/promotion-health.sh"}
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-promotion-health-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
now=1793600000

cat >"$work/gh" <<'STUB'
#!/bin/sh
[ "$1" = api ] || exit 9
shift
filter=
url=
while [ "$#" -gt 0 ]; do
	case "$1" in
	--paginate) ;;
	--jq) filter=$2; shift ;;
	*) url=$1 ;;
	esac
	shift
done
printf '%s|%s\n' "$GH_TOKEN" "$url" >>"$READS"
if [ "${GH_FAIL:-}" = 1 ]; then
	printf 'gh: Bad Gateway (HTTP 502)\n' >&2
	exit 1
fi
# Like the API: only the URL's event, newest first, one run per page.
event=${url#*event=}
event=${event%%&*}
printf '%s' "$RUNS" | jq --arg event "$event" '.workflow_runs |= (map(select(.event == $event)) | sort_by(.created_at) | reverse | .[:1])' | jq -r "$filter"
STUB
chmod +x "$work/gh"

fail() {
	printf 'promotion health fixture failed: %s\n' "$1" >&2
	exit 1
}
# Arguments: age in hours, id, conclusion, event, and the hours it ran (default 0).
iso() { jq -nr "$((now - $1 * 3600)) | todate"; }
rec() { printf '{"created_at":"%s","updated_at":"%s","id":%s,"conclusion":%s,"event":"%s"}' "$(iso "$1")" "$(iso "$(($1 - ${5:-0}))")" "$2" "$3" "$4"; }
runs() { printf '{"workflow_runs":[%s]}' "$1"; }
run() {
	: >"$work/reads"
	status=0
	RUNS=$1 GH_BIN=$work/gh READS=$work/reads GH_REPO=o/r GH_TOKEN=read-token \
		GITHUB_SERVER_URL=https://example.test PROMOTION_HEALTH_NOW=$now \
		"$script" >"$work/out" 2>"$work/err" || status=$?
	if grep -v '^read-token|' "$work/reads" >/dev/null; then fail 'a read used the wrong token'; fi
}
expect_pass() {
	run "$2"
	[ "$status" -eq 0 ] || fail "$1 exited $status: $(cat "$work/out" "$work/err")"
}
# $3 must appear in the output.
expect_fail() {
	run "$2"
	[ "$status" -ne 0 ] || fail "$1 was accepted"
	grep -F -- '::error::' "$work/out" >/dev/null || fail "$1 raised no error annotation"
	grep -F -- "$3" "$work/out" >/dev/null || fail "$1 did not say '$3': $(cat "$work/out")"
}

expect_pass 'a successful latest run' "$(runs "$(rec 3 101 '"success"' schedule)")"
grep -F 'https://example.test/o/r/actions/runs/101' "$work/out" >/dev/null || fail 'the healthy run is not named'
grep -F 'event=schedule&status=completed&per_page=1' "$work/reads" >/dev/null || fail 'scheduled runs are not read'
grep -F 'event=workflow_dispatch&status=completed&per_page=1' "$work/reads" >/dev/null || fail 'manual runs are not read'

expect_fail 'a failed latest run' "$(runs "$(rec 3 102 '"failure"' schedule)")" 'https://example.test/o/r/actions/runs/102'
expect_fail 'a cancelled latest run' "$(runs "$(rec 3 103 '"cancelled"' workflow_dispatch)")" "ended 'cancelled'"
expect_fail 'no runs' "$(runs '')" 'is not running'
expect_fail 'only landing runs' "$(runs "$(rec 2 104 '"success"' pull_request_target)")" 'is not running'
# The latest run must have finished within 36 hours.
expect_fail 'a run older than the window' "$(runs "$(rec 37 105 '"success"' schedule)")" 'has stopped running'
expect_pass 'a run inside the window' "$(runs "$(rec 35 106 '"success"' schedule)")"
# The window counts from completion: started before it, finished inside it.
expect_pass 'a run that finished inside the window' "$(runs "$(rec 37 109 '"success"' schedule 2)")"

# The newest run by creation time counts, wherever it is listed.
mixed="$(rec 30 110 '"failure"' schedule),$(rec 6 111 '"success"' schedule),$(rec 20 112 '"failure"' schedule)"
expect_pass 'the newest run is a success' "$(runs "$mixed")"
mixed="$(rec 6 120 '"failure"' schedule),$(rec 30 121 '"success"' schedule),$(rec 1 122 '"success"' pull_request_target)"
expect_fail 'the newest run is a failure' "$(runs "$mixed")" 'runs/120'
# The newest promotion counts across events: a manual failure after a scheduled success.
mixed="$(rec 10 130 '"success"' schedule),$(rec 2 131 '"failure"' workflow_dispatch)"
expect_fail 'a newer manual failure' "$(runs "$mixed")" 'runs/131'
mixed="$(rec 10 132 '"failure"' workflow_dispatch),$(rec 2 133 '"success"' schedule)"
expect_pass 'a newer scheduled success' "$(runs "$mixed")"
# A run that queued for long still counts by its completion.
expect_pass 'a run that queued for long' "$(runs "$(rec 48 134 '"success"' schedule 20)")"

expect_fail 'an invalid id' "$(runs "$(rec 3 0 '"success"' schedule)")" 'unexpected run record'
expect_fail 'an invalid conclusion' "$(runs "$(rec 3 107 '"##[error]x"' schedule)")" 'unexpected run record'
expect_fail 'a missing conclusion' "$(runs "$(rec 3 108 null schedule)")" "ended '-'"

GH_FAIL=1
export GH_FAIL
expect_fail 'a gh error' "$(runs '')" 'cannot be read'
grep -F 'HTTP 502' "$work/err" >/dev/null || fail 'the gh error reason is hidden'
unset GH_FAIL
status=0
PROMOTION_HEALTH_NOW=abc GH_BIN=$work/gh READS=$work/reads RUNS='{}' GH_REPO=o/r GH_TOKEN=read-token "$script" >"$work/out" 2>&1 || status=$?
[ "$status" -ne 0 ] || fail 'an invalid clock was accepted'

printf 'promotion health fixture passed\n'

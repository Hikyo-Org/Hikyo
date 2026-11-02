#!/usr/bin/env bash
# Drives nightly-source-newer.sh with a stub gh serving each API response.
set -euo pipefail
script="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/nightly-source-newer.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-newer-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
source_sha=$(printf 'a%.0s' $(seq 40))
previous_sha=$(printf 'b%.0s' $(seq 40))
previous_tag=v1.0.0-nightly.20261101.8.gbbbbbbbb
run_tag=v1.0.0-nightly.20261102.9.gaaaaaaaa

cat >"$work/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CALLS"
case "$*" in
*"/git/matching-refs/tags/v"*) [ "${FAIL_ON:-}" != refs ] || exit 1; printf '%s\n' "$REFS" ;;
*"/releases?per_page=100"*) [ "${FAIL_ON:-}" != releases ] || exit 1; printf '%s\n' "$RELEASES" ;;
*"/git/ref/tags/"*) [ "${FAIL_ON:-}" != tag ] || exit 1; printf '%s\n' "$PREVIOUS_SHA" ;;
*"/compare/$PREVIOUS_SHA...$SOURCE"*) [ "${FAIL_ON:-}" != compare ] || exit 1; printf '%s\n' "$STATUS" ;;
*) exit 8 ;;
esac
STUB
chmod +x "$work/gh"

fail() { printf 'nightly source newer fixture failed: %s\n' "$1" >&2; exit 1; }
run_case() {
	: >"$work/calls"
	: >"$work/output"
	env GH_TOKEN=t GITHUB_REPOSITORY=o/r GITHUB_OUTPUT="$work/output" GITHUB_RUN_NUMBER=9 SOURCE="${SOURCE_OVERRIDE:-$source_sha}" \
		GH_BIN="$work/gh" CALLS="$work/calls" "$script" >"$work/stdout" 2>&1
}
proceeds() {
	local name=$1 want=$2
	run_case || fail "$name failed: $(cat "$work/stdout")"
	[ "$(cat "$work/output")" = "newer=$want" ] || fail "$name wrote '$(cat "$work/output")', want newer=$want"
	grep -F "dev commit $source_sha" "$work/stdout" >/dev/null || fail "$name did not name the selected commit"
	if [ "$want" = true ]; then
		grep -F 'proceeds:' "$work/stdout" >/dev/null || fail "$name did not say it proceeds"
	else
		grep -F 'does not proceed:' "$work/stdout" >/dev/null || fail "$name did not say it stops"
	fi
}
refuses() {
	if run_case; then fail "accepted $1"; fi
	if grep -q 'newer=' "$work/output"; then fail "$1 wrote a newer= line"; fi
}

export REFS RELEASES PREVIOUS_SHA=$previous_sha STATUS='' FAIL_ON=''
REFS='[[{"ref":"refs/tags/v1.0.0-nightly.20261101.8.gbbbbbbbb"}]]'
RELEASES="[[{\"tag_name\":\"$previous_tag\",\"draft\":false,\"prerelease\":true}]]"

RELEASES='[[]]'
proceeds 'no previous nightly' true
grep -F -- '/compare/' "$work/calls" >/dev/null && fail 'compared without a previous nightly'
RELEASES="[[{\"tag_name\":\"$previous_tag\",\"draft\":false,\"prerelease\":true}]]"

STATUS=ahead proceeds 'a source ahead of the previous nightly' true
STATUS=identical proceeds 'the commit the previous nightly published' false
STATUS=behind proceeds 'a source behind the previous nightly' false
STATUS=diverged proceeds 'a divergence from a main bridge nightly' true

# A full rerun resumes publication even when the source equals the nightly it created.
REFS="[[{\"ref\":\"refs/tags/$run_tag\"}]]"
STATUS=identical proceeds 'a run that already has its tag' true
grep -F -- '/compare/' "$work/calls" >/dev/null && fail 'compared despite an existing run tag'
REFS='[[]]'

STATUS=surprise refuses 'an unexpected comparison status'
STATUS='' refuses 'an empty comparison status'
for api in refs releases tag compare; do
	STATUS=ahead FAIL_ON=$api refuses "a failed $api request"
done
PREVIOUS_SHA=not-a-sha STATUS=ahead refuses 'a previous nightly with no commit'
PREVIOUS_SHA=$previous_sha
REFS='[[{"ref":"refs/tags/v1.0.0-nightly.20261102.9.gaaaaaaaa"},{"ref":"refs/tags/v1.0.0-nightly.20261102.9.gcccccccc"}]]'
STATUS=ahead refuses 'ambiguous run tags'
REFS='[[]]'
STATUS=ahead SOURCE_OVERRIDE=short refuses 'an invalid source commit'

printf 'nightly source newer fixture: ahead and diverged proceed, identical and behind stop, failures write nothing\n'

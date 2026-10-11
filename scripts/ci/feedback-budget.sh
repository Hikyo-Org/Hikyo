#!/usr/bin/env bash
# Bound setup commands and reserve headroom for bounded external Actions.
set -euo pipefail
fail() { printf 'fast-feedback: %s\n' "$*" >&2; exit 1; }
[[ ${FAST_FEEDBACK_DEADLINE:-} =~ ^[1-9][0-9]{0,10}$ ]] || fail 'invalid FAST_FEEDBACK_DEADLINE; expected a positive epoch timestamp'
remaining=$((FAST_FEEDBACK_DEADLINE - $(date +%s)))
[ "$remaining" -gt 0 ] || fail 'deadline exhausted during setup; selected checks did not complete'
if [ "${1:-}" = --reserve ]; then
	[[ ${2:-} =~ ^[1-9][0-9]*$ ]] || fail 'invalid setup reserve'
	[ "$remaining" -gt "$2" ] || fail "deadline has insufficient setup headroom (${remaining}s remaining); selected checks did not complete"
	exit 0
fi
[ "$#" -gt 0 ] || fail 'missing setup command'
timeout_tool=$(command -v timeout || command -v gtimeout) || fail 'CI deadline needs GNU timeout (or gtimeout)'
printf 'fast-feedback: setup has %ss remaining: %s\n' "$remaining" "$*"
status=0
"$timeout_tool" --kill-after=5s "${remaining}s" "$@" || status=$?
if { [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; } && [ "$(date +%s)" -ge "$FAST_FEEDBACK_DEADLINE" ]; then
	fail 'deadline exhausted during setup; selected checks did not complete'
fi
if [ "$status" -ne 0 ]; then
	printf 'fast-feedback: setup command failed (exit %s): %s\n' "$status" "$*" >&2
fi
exit "$status"

#!/usr/bin/env bash
set -euo pipefail

check="$(cd -- "$(dirname "$0")" && pwd)/check-security-feed-freshness.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() {
	printf 'check-security-feed-freshness: %s\n' "$1" >&2
	exit 1
}
write() { jq -n --argjson at "$(($(date +%s) + $1))" '{signed: {expires: ($at | todateiso8601)}}' >"$work/timestamp.json"; }

write $((48 * 3600))
"$check" "file://$work/timestamp.json" >/dev/null || fail 'a fresh feed was refused'
write $((35 * 3600))
if "$check" "file://$work/timestamp.json" 2>/dev/null; then fail 'a feed near expiry was accepted'; fi
write -60
if "$check" "file://$work/timestamp.json" 2>/dev/null; then fail 'an expired feed was accepted'; fi
printf '{"signed":{}}' >"$work/timestamp.json"
if "$check" "file://$work/timestamp.json" 2>/dev/null; then fail 'a feed with no expiry was accepted'; fi
if "$check" "file://$work/absent.json" 2>/dev/null; then fail 'an unreachable feed was accepted'; fi

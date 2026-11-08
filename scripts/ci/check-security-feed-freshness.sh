#!/usr/bin/env bash
# Fails when the published security feed is unreachable or close to expiry. A stopped
# publisher otherwise shows up only as every installation going stale. It reads what
# installations read and needs only curl and jq, so it can run outside GitHub too.
# This checks freshness only: installations verify signatures themselves.
set -euo pipefail

url=${1:-https://hikyo.app/security-feed/metadata/timestamp.json}
# The daily publish leaves at least 48 hours, so this allows one missed run some slack.
minimum_hours=${HIKYO_FEED_MINIMUM_HOURS:-36}

body=$(curl --fail --silent --show-error --location --max-redirs 0 --max-time 30 --proto '=https,file' -- "$url") || {
	printf 'security feed: cannot read %s\n' "$url" >&2
	exit 1
}
remaining=$(printf '%s' "$body" | jq -er '.signed.expires | fromdateiso8601 - now | floor') || {
	printf 'security feed: %s has no readable expiry\n' "$url" >&2
	exit 1
}
if [ "$remaining" -lt $((minimum_hours * 3600)) ]; then
	printf 'security feed: %s expires in %s seconds, under the %s hour minimum\n' "$url" "$remaining" "$minimum_hours" >&2
	exit 1
fi
printf 'security feed: fresh, %s hours left\n' $((remaining / 3600))

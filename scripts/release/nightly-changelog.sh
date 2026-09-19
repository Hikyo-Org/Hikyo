#!/bin/sh
set -eu

[ "$#" -eq 2 ] || {
	printf 'usage: %s PREVIOUS_COMMIT CURRENT_COMMIT\n' "$0" >&2
	exit 2
}

previous=$1
current=$(git rev-parse --verify --end-of-options "$2^{commit}")
if [ -n "$previous" ]; then
	previous=$(git rev-parse --verify --end-of-options "$previous^{commit}")
	range="$previous..$current"
	printf '\n## Changes since the previous nightly\n\n'
	if [ -n "${GITHUB_REPOSITORY:-}" ]; then
		printf '[Full comparison](%s/%s/compare/%s...%s)\n\n' \
			"${GITHUB_SERVER_URL:-https://github.com}" "$GITHUB_REPOSITORY" "$previous" "$current"
	fi
else
	range=$current
	printf '\n## Changes in the first nightly\n\n'
	printf 'No previous published nightly exists; listing reachable commit history.\n\n'
fi

changes=$(git log --no-merges --format='- %s (%h)' "$range" --)
if [ -n "$changes" ]; then
	printf '%s\n' "$changes"
else
	printf 'No new non-merge commits.\n'
fi

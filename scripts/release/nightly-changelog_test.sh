#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
script="$repo_root/scripts/release/nightly-changelog.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.test
export GIT_COMMITTER_NAME=Fixture GIT_COMMITTER_EMAIL=fixture@example.test
export GITHUB_REPOSITORY=example/hikyo GITHUB_SERVER_URL=https://github.com
git init -q "$tmp"
cd "$tmp"
git -c commit.gpgsign=false commit -q --allow-empty -m 'old change'
previous=$(git rev-parse HEAD)
git -c commit.gpgsign=false commit -q --allow-empty -m 'feat: new nightly feature'
current=$(git rev-parse HEAD)
git -c commit.gpgsign=false commit -q --allow-empty -m 'later change'

notes=$("$script" "$previous" "$current")
printf '%s\n' "$notes" | grep -F 'feat: new nightly feature' >/dev/null
printf '%s\n' "$notes" | grep -F "https://github.com/example/hikyo/compare/$previous...$current" >/dev/null
if printf '%s\n' "$notes" | grep -E 'old change|later change' >/dev/null; then
	echo 'nightly changelog includes commits outside the release range' >&2
	exit 1
fi
"$script" '' "$current" | grep -F 'old change' >/dev/null
"$script" "$current" "$current" | grep -F 'No new non-merge commits.' >/dev/null
if "$script" missing "$current" >/dev/null 2>&1; then
	echo 'nightly changelog accepts a missing predecessor' >&2
	exit 1
fi
if "$script" "$previous" missing >/dev/null 2>&1; then
	echo 'nightly changelog accepts a missing current commit' >&2
	exit 1
fi
printf 'nightly changelog: exact range, first release, empty range and invalid refs passed\n'

#!/usr/bin/env bash
# Puts the publisher's feed store at /security-feed/ in a built Pages tree. Every
# Pages deployment replaces the whole site, so each one must carry the feed. A
# store that exists but cannot be read fails the deployment instead of dropping it.
set -euo pipefail

if [ "$#" -ne 2 ]; then
	printf 'usage: %s SITE_DIRECTORY STORE_REMOTE\n' "$0" >&2
	exit 2
fi
site=$1
remote=$2

status=0
git ls-remote --exit-code --heads "$remote" security-feed >/dev/null || status=$?
if [ "$status" -eq 2 ]; then
	printf 'No security feed store exists yet; nothing to include.\n'
	exit 0
fi
[ "$status" -eq 0 ] || exit "$status"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git clone --quiet --depth 1 --branch security-feed "$remote" "$work/store"
[ -f "$work/store/metadata/1.root.json" ] || {
	printf 'security feed store has no metadata/1.root.json\n' >&2
	exit 1
}
# The Pages archive dereferences links, so one could publish a runner file.
trees=("$work/store/metadata")
if [ -e "$work/store/targets" ] || [ -L "$work/store/targets" ]; then trees+=("$work/store/targets"); fi
if [ -n "$(find "${trees[@]}" ! -type f ! -type d -print -quit)" ]; then
	printf 'security feed store contains an entry that is not a regular file or directory\n' >&2
	exit 1
fi
rm -rf "$site/security-feed"
mkdir "$site/security-feed"
cp -R "$work/store/metadata" "$site/security-feed/metadata"
if [ -d "$work/store/targets" ]; then
	cp -R "$work/store/targets" "$site/security-feed/targets"
fi

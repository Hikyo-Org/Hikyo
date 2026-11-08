#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname "$0")/../.." && pwd)
merge="$repo_root/scripts/ci/merge-security-feed.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() {
	printf 'merge-security-feed: %s\n' "$1" >&2
	exit 1
}

git init --quiet --bare "$work/remote.git"
git init --quiet "$work/store"
git -C "$work/store" config user.name test
git -C "$work/store" config user.email test@example.invalid
git -C "$work/store" remote add origin "$work/remote.git"
mkdir -p "$work/site/security-feed" "$work/store/metadata" "$work/store/targets/security"
printf 'stale\n' >"$work/site/security-feed/stale.json"

"$merge" "$work/site" "$work/remote.git" >/dev/null
[ -f "$work/site/security-feed/stale.json" ] || fail 'a missing store changed the site'

printf 'wrong\n' >"$work/store/metadata/timestamp.json"
git -C "$work/store" add .
git -C "$work/store" commit --quiet -m rootless
git -C "$work/store" push --quiet origin HEAD:refs/heads/security-feed
if "$merge" "$work/site" "$work/remote.git" 2>/dev/null; then fail 'a store with no root was accepted'; fi
[ -f "$work/site/security-feed/stale.json" ] || fail 'a refused store changed the site'

printf 'root\n' >"$work/store/metadata/1.root.json"
printf 'catalog\n' >"$work/store/targets/security/abc.catalog.json"
git -C "$work/store" add .
git -C "$work/store" commit --quiet -m feed
git -C "$work/store" push --quiet origin HEAD:refs/heads/security-feed
"$merge" "$work/site" "$work/remote.git"
[ -f "$work/site/security-feed/metadata/1.root.json" ] || fail 'metadata was not copied'
[ -f "$work/site/security-feed/targets/security/abc.catalog.json" ] || fail 'targets were not copied'
[ ! -e "$work/site/security-feed/stale.json" ] || fail 'a stale file survived'
[ ! -e "$work/site/security-feed/.git" ] || fail 'store history was published'

for link in metadata/linked.json targets/linked.json; do
	ln -s /etc/hostname "$work/store/$link"
	git -C "$work/store" add .
	git -C "$work/store" commit --quiet -m link
	git -C "$work/store" push --quiet origin HEAD:refs/heads/security-feed
	if "$merge" "$work/site" "$work/remote.git" 2>/dev/null; then fail "a symbolic link in $link was accepted"; fi
	[ -f "$work/site/security-feed/metadata/1.root.json" ] || fail 'a refused link changed the site'
	git -C "$work/store" rm --quiet "$link"
	git -C "$work/store" commit --quiet -m unlink
done
git -C "$work/store" push --quiet origin HEAD:refs/heads/security-feed

if "$merge" "$work/site" "$work/absent.git" 2>/dev/null; then fail 'an unreadable store was accepted'; fi
[ -f "$work/site/security-feed/metadata/1.root.json" ] || fail 'an unreadable store changed the site'

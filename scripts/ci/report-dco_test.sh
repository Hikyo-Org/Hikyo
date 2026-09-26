#!/bin/sh
# Drives report-dco.sh over a real git fixture and a stub gh that records the
# comment calls it would make.
set -eu

script="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/report-dco.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dco-report-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

repo=$work/repo
git -C "$work" init -q repo
git -C "$repo" config user.name 'Fixture Author'
git -C "$repo" config user.email 'fixture@example.com'
git -C "$repo" config commit.gpgsign false
printf 'base\n' >"$repo/file"
git -C "$repo" add file
git -C "$repo" commit -q -s -m base
base=$(git -C "$repo" rev-parse HEAD)
printf 'signed\n' >>"$repo/file"
git -C "$repo" commit -q -s -am signed
signed=$(git -C "$repo" rev-parse HEAD)
printf 'unsigned\n' >>"$repo/file"
git -C "$repo" commit -q -am '@everyone <img src=x> unsigned'
unsigned=$(git -C "$repo" rev-parse HEAD)

mkdir "$work/bin"
# Serves a comments list mixing the marker comment with look-alikes, and
# applies --jq with jq as gh does, so the real selector runs.
cat >"$work/bin/gh" <<'STUB'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$CALLS"
filter=
prev=
for arg in "$@"; do
	[ "$prev" = --jq ] && filter=$arg
	prev=$arg
done
case $* in
*"/issues/7/comments?per_page=100"*)
	jq -r --arg id "${EXISTING:-}" "[{\"id\":1,\"user\":{\"login\":\"github-actions[bot]\"},\"body\":\"unrelated\"},
		{\"id\":2,\"user\":{\"login\":\"mallory\"},\"body\":\"<!-- hikyo-dco-report -->forged\"}]
		+ (if \$id == \"\" then [] else [{\"id\":(\$id|tonumber),\"user\":{\"login\":\"github-actions[bot]\"},\"body\":\"<!-- hikyo-dco-report -->\\nold\"}] end)
		| $filter" -n
	;;
esac
STUB
chmod +x "$work/bin/gh"

run() {
	: >"$work/calls"
	PATH="$work/bin:$PATH" CALLS=$work/calls EXISTING=${2:-} GH_REPO=o/r PR_NUMBER=7 \
		BASE_SHA=$base HEAD_SHA=$1 PR_AUTHOR=someone DCO_REPOSITORY=$repo "$script" >/dev/null 2>&1
}

fail() {
	printf 'DCO report fixture failed: %s\n' "$1" >&2
	exit 1
}

run "$unsigned" && fail 'unsigned commit reported success'
grep -F -- '-X POST repos/o/r/issues/7/comments' "$work/calls" >/dev/null || fail 'no comment posted'
grep -F -- "- \`$unsigned\`" "$work/calls" >/dev/null || fail 'unsigned commit SHA missing from the comment'
grep -F -- "$signed" "$work/calls" >/dev/null && fail 'signed commit listed as unsigned'
grep -F -- '@everyone' "$work/calls" >/dev/null && fail 'commit subject reached the comment'

run "$unsigned" 99 && fail 'unsigned commit reported success with an existing comment'
grep -F -- '-X PATCH repos/o/r/issues/comments/99' "$work/calls" >/dev/null || fail 'existing comment not updated'
grep -F -- '-X POST' "$work/calls" >/dev/null && fail 'duplicate comment posted'

run "$signed" 99 || fail 'signed range reported failure'
grep -F -- '-X DELETE repos/o/r/issues/comments/99' "$work/calls" >/dev/null || fail 'stale comment not removed'

run "$signed" || fail 'signed range without a comment reported failure'
grep -F -- '-X ' "$work/calls" >/dev/null && fail 'signed range wrote a comment'
grep -E -- 'comments/(1|2)( |$)' "$work/calls" >/dev/null && fail 'touched a comment that is not the bot marker comment'

printf 'DCO report fixture: unsigned commits get one SHA-only comment, updated in place and removed once signed\n'

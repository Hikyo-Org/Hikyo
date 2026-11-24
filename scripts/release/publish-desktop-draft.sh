#!/bin/sh
# Publish the staged desktop-v<version> draft after v<version> is public. Run by
# the maintainer's ceremony, from the repository root, after publish-stable-draft.sh.
# With --check-only it verifies the staged draft without publishing anything and
# without requiring v<version> to be public, so the ceremony can refuse early.
set -eu
check_only=false
if [ "${1:-}" = --check-only ]; then check_only=true; shift; fi
[ "$#" -eq 3 ] || { printf 'usage: %s [--check-only] REPOSITORY VERSION TRUST\n' "$0" >&2; exit 2; }
repository=$1 version=$2 trust=$3
: "${GH_BIN:=gh}"
export GH_BIN
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
fail() { printf 'desktop publication: %s\n' "$*" >&2; exit 1; }
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || exit 2
is_semver "$version" || exit 2
case "$version" in *-nightly.*) fail 'nightly version refused' ;; esac
[ "$("$script_dir/release-channel.sh" "v$version")" = stable ] || fail 'prerelease version refused; the desktop app is released for stable versions only'
cli_tag=v$version
desktop_tag=desktop-v$version
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-publish-desktop.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/draft" "$scratch/public"

# Resolve the exact tag across every release page; drafts have no tag endpoint.
read_release() (
	tag=$1 out=$2
	"$GH_BIN" api --paginate --slurp "repos/$repository/releases?per_page=100" >"$out.pages"
	release_id=$(jq -er --arg tag "$tag" '
		if type == "array" and all(.[]; type == "array") then
			if all(.[][];
				type == "object" and
				(.tag_name | type == "string" and length > 0) and
				(.id | type == "number" and . > 0 and . == floor)
			) then
				[.[][] | select(.tag_name == $tag)] |
				if length == 1 then .[0].id
				else error("release tag must identify exactly one release") end
			else error("invalid release row") end
		else error("invalid release pages") end
	' "$out.pages") || fail "$tag must identify exactly one release"
	"$GH_BIN" api "repos/$repository/releases/$release_id" >"$out"
	jq -e --arg tag "$tag" --argjson id "$release_id" '.id == $id and .tag_name == $tag' "$out" >/dev/null ||
		fail 'release lookup identity differs'
)
# The tag must be the exact commit the CLI release was built from.
require_tag_commit() {
	"$GH_BIN" api "repos/$repository/git/ref/tags/$desktop_tag" |
		jq -e --arg sha "$commit" '.object.type == "commit" and .object.sha == $sha' >/dev/null ||
		fail "refs/tags/$desktop_tag does not point at $commit"
}
# Before publication the tag may be absent (publishing creates it from the draft's
# target) but never at another object: the immutable release could not move it.
require_tag_absent_or_commit() {
	if "$GH_BIN" api "repos/$repository/git/ref/tags/$desktop_tag" >"$scratch/tagref.json" 2>"$scratch/tagref.err"; then
		jq -e --arg sha "$commit" '.object.type == "commit" and .object.sha == $sha' "$scratch/tagref.json" >/dev/null ||
			fail "refs/tags/$desktop_tag already exists and does not point at $commit"
	else
		grep -F '(HTTP 404)' "$scratch/tagref.err" >/dev/null || { cat "$scratch/tagref.err" >&2; fail "cannot read refs/tags/$desktop_tag"; }
	fi
}
# Finishing an older version is fine; the desktop series must simply never be Latest.
require_not_latest() {
	"$GH_BIN" api "repos/$repository/releases/latest" | jq -e '.tag_name | type == "string" and (startswith("desktop-v") | not)' >/dev/null ||
		fail 'a desktop-v release became the latest release'
}

"$GH_BIN" api "repos/$repository/immutable-releases" | jq -e '.enabled == true' >/dev/null ||
	fail 'immutable GitHub releases must be enabled'
"$script_dir/check-desktop-rulesets.sh" "$repository"

# Publishing needs the CLI release first: public, final and immutable. A check
# accepts its draft too. Either way it names the commit the desktop draft must target.
read_release "$cli_tag" "$scratch/cli.json"
if [ "$check_only" = true ]; then
	jq -e '.prerelease == false and (.draft == true or .immutable == true)' "$scratch/cli.json" >/dev/null ||
		fail "$cli_tag must be a stable draft or an immutable public release"
else
	jq -e '.draft == false and .prerelease == false and .immutable == true' "$scratch/cli.json" >/dev/null ||
		fail "$cli_tag must be public, stable and immutable before the desktop release"
fi
"$GH_BIN" release download "$cli_tag" --repo "$repository" --pattern release-manifest.json --dir "$scratch/public"
commit=$(jq -er '.source_commit' "$scratch/public/release-manifest.json")
printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$' || fail 'invalid source commit in the CLI manifest'
rm -rf "$scratch/public"
mkdir "$scratch/public"

read_release "$desktop_tag" "$scratch/release.json" ||
	fail "no unique $desktop_tag release; rerun the failed jobs of the $cli_tag release run to stage its draft"
jq -e --arg commit "$commit" '
	(.draft == true or (.draft == false and .immutable == true)) and .prerelease == false and
	.target_commitish == $commit
' "$scratch/release.json" >/dev/null ||
	fail 'desktop release must be a stable draft or immutable public release targeting the CLI release commit'
already_public=$(jq -r '.draft == false' "$scratch/release.json")
"$GH_BIN" release download "$desktop_tag" --repo "$repository" --dir "$scratch/draft"
go run ./scripts/release/stable verify-desktop --trust "$trust" --directory "$scratch/draft" \
	--version "$version" --commit "$commit" >/dev/null || fail 'desktop inventory verification failed'
jq -e --arg commit "$commit" '.source_commit == $commit' "$scratch/draft/desktop-inventory.json" >/dev/null ||
	fail 'inventory commit differs from the CLI release manifest'
if [ "$already_public" = true ]; then
	require_tag_commit
	require_not_latest
	printf 'desktop publication: %s already public and immutable; downloaded bytes verified\n' "$desktop_tag"
	exit 0
fi
require_tag_absent_or_commit

if [ "$check_only" = true ]; then
	printf 'desktop publication: %s draft verified against %s (check only)\n' "$desktop_tag" "$cli_tag"
	exit 0
fi

# Re-fetch immediately before publication to reject asset replacement after verification.
read_release "$desktop_tag" "$scratch/recheck.json"
shape='[.id,.tag_name,.draft,.target_commitish,(.assets | sort_by(.id) | map({id,name,size,updated_at,digest}))]'
jq -S "$shape" "$scratch/release.json" >"$scratch/before"
jq -S "$shape" "$scratch/recheck.json" >"$scratch/after"
cmp -s "$scratch/before" "$scratch/after" || fail 'draft changed during verification'
# Publish by the verified release's ID, never by tag, so a release another actor
# creates under the same tag cannot be the one that goes public.
original_id=$(jq -er '.id' "$scratch/release.json")
"$GH_BIN" api --method PATCH "repos/$repository/releases/$original_id" -F draft=false -F prerelease=false -f make_latest=false >/dev/null
"$GH_BIN" api "repos/$repository/releases/$original_id" >"$scratch/public-release.json"
jq -e --slurpfile original "$scratch/release.json" '
	.id == $original[0].id and .tag_name == $original[0].tag_name and
	.draft == false and .prerelease == false and .immutable == true
' "$scratch/public-release.json" >/dev/null || fail 'public release identity or immutability differs'
require_not_latest
require_tag_commit
"$GH_BIN" release download "$desktop_tag" --repo "$repository" --dir "$scratch/public"
go run ./scripts/release/stable verify-desktop --trust "$trust" --directory "$scratch/public" \
	--version "$version" --commit "$commit" >/dev/null || fail 'public desktop downloads failed verification'
printf 'desktop publication: %s public downloads verified\n' "$desktop_tag"

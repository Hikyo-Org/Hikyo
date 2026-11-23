#!/bin/sh
# Called only by the tag build job. Signs the desktop inventory with the stable
# workflow's keyless identity and stages the desktop-v<version> draft. The run
# never creates the tag: publishing the draft creates it from the target commit.
set -eu
[ "$#" -eq 2 ] || { printf 'usage: %s TRUST DIRECTORY\n' "$0" >&2; exit 2; }
trust=$1 directory=$2
: "${COSIGN_BIN:=cosign}"
: "${GH_BIN:=gh}"
: "${GITHUB_ACTIONS:?}" "${GITHUB_WORKFLOW_REF:?}" "${GITHUB_REPOSITORY:?}" "${GITHUB_REF:?}" "${GITHUB_SHA:?}"
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
fail() { printf 'desktop draft: %s\n' "$*" >&2; exit 1; }
[ "$GITHUB_ACTIONS" = true ] || fail 'GitHub Actions only'
[ "$GITHUB_WORKFLOW_REF" = "$GITHUB_REPOSITORY/.github/workflows/release.yml@$GITHUB_REF" ] || fail 'wrong workflow identity'
case "$GITHUB_REF" in refs/tags/v*) ;; *) fail 'a v tag is required' ;; esac
version=${GITHUB_REF#refs/tags/v}
is_semver "$version" || fail 'invalid version'
[ "$("$script_dir/release-channel.sh" "v$version")" = stable ] || fail 'the desktop app is released for stable versions only'
printf '%s\n' "$GITHUB_SHA" | grep -Eq '^[0-9a-f]{40}$' || fail 'invalid commit'
tag=desktop-v$version
# The build jobs recorded each image's digest; the downloaded artifact must match.
: "${DESKTOP_SHA256_ARM64:?}" "${DESKTOP_SHA256_X86_64:?}"

# Exactly the two disk images, as regular files.
want=$(printf 'Hikyo-Desktop-%s-arm64.dmg\nHikyo-Desktop-%s-x86_64.dmg\n' "$version" "$version")
have=$(ls -A "$directory")
[ "$have" = "$want" ] || fail 'the directory must hold exactly the arm64 and x86_64 disk images'
for name in $want; do
	[ -f "$directory/$name" ] && [ ! -L "$directory/$name" ] && [ -s "$directory/$name" ] || fail "regular non-empty file required: $name"
done

for arch in arm64 x86_64; do
	case $arch in arm64) recorded=$DESKTOP_SHA256_ARM64 ;; *) recorded=$DESKTOP_SHA256_X86_64 ;; esac
	printf '%s\n' "$recorded" | grep -Eq '^[0-9a-f]{64}$' || fail "no recorded digest for $arch"
	[ "$(sha256_file "$directory/Hikyo-Desktop-$version-$arch.dmg")" = "$recorded" ] ||
		fail "the $arch disk image differs from the digest its build job recorded"
done

stage=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-desktop-draft.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir "$stage/upload" "$stage/downloaded"
for name in $want; do cp "$directory/$name" "$stage/upload/$name"; done
{
	for name in $want; do
		printf '%s\t%s\t%s\n' "$name" "$(wc -c <"$stage/upload/$name" | tr -d ' ')" "$(sha256_file "$stage/upload/$name")"
	done
} | jq -R -s --arg version "$version" --arg commit "$GITHUB_SHA" '
	{schema: "hikyo.dev/desktop-inventory/v1", version: $version, source_commit: $commit,
	 tag: ("desktop-v" + $version), cli_tag: ("v" + $version),
	 files: [split("\n")[] | select(length > 0) | split("\t") | {name: .[0], size: (.[1] | tonumber), sha256: .[2]}]}
' >"$stage/upload/desktop-inventory.json"
"$COSIGN_BIN" sign-blob --yes --use-signing-config=false --new-bundle-format=true \
	--rekor-url=https://rekor.sigstore.dev --trusted-root="$trust/stable-trusted-root.json" \
	--bundle "$stage/upload/desktop-inventory.sigstore.json" "$stage/upload/desktop-inventory.json"
go run ./scripts/release/stable verify-desktop --trust "$trust" --directory "$stage/upload" \
	--version "$version" --commit "$GITHUB_SHA" >/dev/null

# A rerun after a failed attempt may find this lane's own unpublished draft. Never
# touch a public release; replace a draft, so only one release holds the tag.
"$GH_BIN" api --paginate --slurp "repos/$GITHUB_REPOSITORY/releases?per_page=100" >"$stage/pages.json"
jq -e --arg tag "$tag" '[.[][] | select(.tag_name == $tag and .draft == false)] | length == 0' "$stage/pages.json" >/dev/null ||
	fail "$tag is already public"
for id in $(jq -r --arg tag "$tag" '.[][] | select(.tag_name == $tag and .draft == true) | .id' "$stage/pages.json"); do
	"$GH_BIN" api --method DELETE "repos/$GITHUB_REPOSITORY/releases/$id" >/dev/null
done

cat >"$stage/notes.md" <<NOTES
Hikyo desktop app $version for macOS, for Apple silicon (arm64) and Intel (x86_64).

The disk images are signed with the maintainer's Developer ID and notarized by Apple. Before you open one, verify the signed inventory in this release as described in https://hikyo.app/docs/desktop-app/#verify-the-download.

This release holds only the app. The hikyo CLI and server are released separately as v$version.
NOTES
# Everything after creation is addressed by the release ID, never by tag, so a
# release another actor creates under the same tag cannot be written to or read.
"$GH_BIN" api --method POST "repos/$GITHUB_REPOSITORY/releases" -f tag_name="$tag" -f target_commitish="$GITHUB_SHA" \
	-f name="Hikyo Desktop $version" -F body=@"$stage/notes.md" -F draft=true -F prerelease=false -f make_latest=false >"$stage/created.json"
release_id=$(jq -er --arg tag "$tag" --arg sha "$GITHUB_SHA" '
	select(.tag_name == $tag and .draft == true and .target_commitish == $sha and (.id | type == "number" and . > 0 and . == floor)) | .id
' "$stage/created.json") || fail 'the created release is not the expected draft'
for name in $want; do
	"$GH_BIN" api --method POST "https://uploads.github.com/repos/$GITHUB_REPOSITORY/releases/$release_id/assets?name=$name" \
		-H 'Content-Type: application/octet-stream' --input "$stage/upload/$name" >/dev/null
done
for name in desktop-inventory.json desktop-inventory.sigstore.json; do
	"$GH_BIN" api --method POST "https://uploads.github.com/repos/$GITHUB_REPOSITORY/releases/$release_id/assets?name=$name" \
		-H 'Content-Type: application/octet-stream' --input "$stage/upload/$name" >/dev/null
done
"$GH_BIN" api "repos/$GITHUB_REPOSITORY/releases/$release_id" |
	jq -e --argjson id "$release_id" --arg tag "$tag" '.id == $id and .tag_name == $tag and .draft == true' >/dev/null ||
	fail 'the draft changed after upload'
"$GH_BIN" api --paginate --slurp "repos/$GITHUB_REPOSITORY/releases/$release_id/assets?per_page=100" >"$stage/assets.json"
jq -e 'all(.[][]; (.name | test("^[A-Za-z0-9._-]+$")) and (.id | type == "number"))' "$stage/assets.json" >/dev/null ||
	fail 'invalid asset listing'
jq -r '.[][] | "\(.id) \(.name)"' "$stage/assets.json" >"$stage/asset-ids"
while read -r asset_id name; do
	"$GH_BIN" api -H 'Accept: application/octet-stream' "repos/$GITHUB_REPOSITORY/releases/assets/$asset_id" >"$stage/downloaded/$name"
done <"$stage/asset-ids"
go run ./scripts/release/stable verify-desktop --trust "$trust" --directory "$stage/downloaded" \
	--version "$version" --commit "$GITHUB_SHA" >/dev/null
printf 'desktop draft: %s (release %s) staged and verified from its downloaded assets\n' "$tag" "$release_id"

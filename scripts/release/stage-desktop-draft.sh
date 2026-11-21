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

# Exactly the two disk images, as regular files.
want=$(printf 'Hikyo-Desktop-%s-arm64.dmg\nHikyo-Desktop-%s-x86_64.dmg\n' "$version" "$version")
have=$(ls -A "$directory")
[ "$have" = "$want" ] || fail 'the directory must hold exactly the arm64 and x86_64 disk images'
for name in $want; do
	[ -f "$directory/$name" ] && [ ! -L "$directory/$name" ] && [ -s "$directory/$name" ] || fail "regular non-empty file required: $name"
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
"$GH_BIN" release create "$tag" --draft --target "$GITHUB_SHA" --latest=false \
	--title "Hikyo Desktop $version" --notes-file "$stage/notes.md"
"$GH_BIN" release upload "$tag" "$stage/upload/"*
"$GH_BIN" release download "$tag" --dir "$stage/downloaded"
go run ./scripts/release/stable verify-desktop --trust "$trust" --directory "$stage/downloaded" \
	--version "$version" --commit "$GITHUB_SHA" >/dev/null
printf 'desktop draft: %s staged and verified from its downloaded assets\n' "$tag"

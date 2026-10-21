#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

fail() {
	printf 'homebrew cask: %s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 4 ] || fail 'usage: render-homebrew-cask.sh MANIFEST BUNDLE OUTPUT REPOSITORY'
manifest=$1
bundle=$2
output=$3
repository=$4

[ -f "$manifest" ] || fail "missing release manifest $manifest"
[ -d "$bundle" ] || fail "missing release bundle $bundle"
[ -d "$(dirname "$output")" ] || fail 'output directory does not exist'
case "$repository" in
	*[!A-Za-z0-9._/-]* | */*/* | /* | */ | '') fail 'invalid source repository' ;;
esac

jq -e '
	.schema == "hikyo.dev/release-manifest/v1" and
	(.version | type == "string") and
	.tag == ("v" + .version) and
	(.artifacts | type == "array")
' "$manifest" >/dev/null || fail 'invalid release manifest'
version=$(jq -r '.version' "$manifest")
is_semver "$version" || fail 'invalid release version'

image_record() {
	architecture=$1
	expected="hikyo_${version}_Darwin_${architecture}.dmg"
	count=$(jq -r --arg name "$expected" \
		'[.artifacts[] | select(.kind == "macos-dmg" and .name == $name and (.sha256 | test("^[0-9a-f]{64}$")))] | length' \
		"$manifest")
	[ "$count" -eq 1 ] || fail "expected one signed Darwin $architecture disk image"
	printf '%s\n' "$expected"
}

arm_image=$(image_record arm64)
intel_image=$(image_record x86_64)
arm_sha=$(jq -r --arg name "$arm_image" '.artifacts[] | select(.name == $name) | .sha256' "$manifest")
intel_sha=$(jq -r --arg name "$intel_image" '.artifacts[] | select(.name == $name) | .sha256' "$manifest")

for image in "$arm_image" "$intel_image"; do
	safe_release_name "$image" || fail "unsafe disk image name $image"
	[ -f "$bundle/$image" ] && [ ! -L "$bundle/$image" ] || fail "missing regular disk image $image"
done
[ "$(sha256_file "$bundle/$arm_image")" = "$arm_sha" ] || fail "disk image hash mismatch: $arm_image"
[ "$(sha256_file "$bundle/$intel_image")" = "$intel_sha" ] || fail "disk image hash mismatch: $intel_image"

tmp=$(mktemp "$output.tmp.XXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM
cat >"$tmp" <<EOF
cask "hikyo" do
  arch arm: "arm64", intel: "x86_64"

  version "$version"
  sha256 arm:   "$arm_sha",
         intel: "$intel_sha"

  url "https://github.com/$repository/releases/download/v#{version}/hikyo_#{version}_Darwin_#{arch}.dmg"
  name "Hikyo"
  desc "Self-hosted control plane for secrets and configuration"
  homepage "https://hikyo.app/"

  binary "hikyo"
end
EOF
mv "$tmp" "$output"
trap - EXIT HUP INT TERM
printf 'homebrew cask: wrote %s for Hikyo %s\n' "$output" "$version"

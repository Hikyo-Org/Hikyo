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

archive_record() {
	architecture=$1
	expected="hikyo_${version}_Darwin_${architecture}.tar.gz"
	count=$(jq -r --arg name "$expected" \
		'[.artifacts[] | select(.kind == "binary" and .name == $name and (.sha256 | test("^[0-9a-f]{64}$")))] | length' \
		"$manifest")
	[ "$count" -eq 1 ] || fail "expected one signed Darwin $architecture archive"
	printf '%s\n' "$expected"
}

arm_archive=$(archive_record arm64)
intel_archive=$(archive_record x86_64)
arm_sha=$(jq -r --arg name "$arm_archive" '.artifacts[] | select(.name == $name) | .sha256' "$manifest")
intel_sha=$(jq -r --arg name "$intel_archive" '.artifacts[] | select(.name == $name) | .sha256' "$manifest")

for archive in "$arm_archive" "$intel_archive"; do
	safe_release_name "$archive" || fail "unsafe archive name $archive"
	[ -f "$bundle/$archive" ] && [ ! -L "$bundle/$archive" ] || fail "missing regular archive $archive"
done
[ "$(sha256_file "$bundle/$arm_archive")" = "$arm_sha" ] || fail "archive hash mismatch: $arm_archive"
[ "$(sha256_file "$bundle/$intel_archive")" = "$intel_sha" ] || fail "archive hash mismatch: $intel_archive"

tmp=$(mktemp "$output.tmp.XXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM
cat >"$tmp" <<EOF
cask "hikyo" do
  arch arm: "arm64", intel: "x86_64"

  version "$version"
  sha256 arm:   "$arm_sha",
         intel: "$intel_sha"

  url "https://github.com/$repository/releases/download/v#{version}/hikyo_#{version}_Darwin_#{arch}.tar.gz"
  name "Hikyo"
  desc "Self-hosted control plane for secrets and configuration"
  homepage "https://hikyo.app/"

  binary "hikyo"
end
EOF
mv "$tmp" "$output"
trap - EXIT HUP INT TERM
printf 'homebrew cask: wrote %s for Hikyo %s\n' "$output" "$version"

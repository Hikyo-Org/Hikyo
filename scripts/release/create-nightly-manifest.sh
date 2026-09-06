#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
[ "$#" -eq 4 ] || { printf 'usage: %s VERSION COMMIT SEQUENCE PAYLOAD_DIRECTORY\n' "$0" >&2; exit 2; }
version=$1 commit=$2 sequence=$3 directory=$4
[ ! -e "$directory/release-manifest.json" ] || { printf 'nightly manifest already exists\n' >&2; exit 1; }
jq -en --arg version "$version" --arg commit "$commit" --arg sequence "$sequence" '
 $version | test("^[0-9]+\\.[0-9]+\\.[0-9]+-nightly\\.[0-9]{8}\\.[1-9][0-9]*\\.g[0-9a-f]{8}$")
' >/dev/null
printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$'
jq -e --arg version "$version" --arg commit "$commit" --arg sequence "$sequence" '
 .schema == "hikyo.dev/upgrade-compatibility/v1" and .profile == "nightly/v1" and
 .version == $version and .commit == $commit and .sequence == ($sequence | tonumber) and
 .sequence >= 1 and .sequence <= 9007199254740991
' "$directory/upgrade-compatibility.json" >/dev/null
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-manifest.XXXXXX")
publication=
trap 'rm -rf "$scratch"; if [ -n "$publication" ]; then rm -f "$publication"; fi' EXIT HUP INT TERM
: >"$scratch/records"
record() {
 name=$1 kind=$2 platform=$3 format=$4 arch=$5
 path=$directory/$name
 [ -f "$path" ] && [ ! -L "$path" ] || { printf 'nightly: missing regular asset %s\n' "$name" >&2; exit 1; }
 jq -nc --arg name "$name" --arg kind "$kind" --arg platform "$platform" --arg format "$format" --arg arch "$arch" --arg sha256 "$(sha256_file "$path")" '
 {name:$name,kind:$kind,sha256:$sha256} +
 (if $platform == "" then {} else {platform:$platform} end) +
 (if $format == "" then {} else {format:$format,arch:$arch} end)
 ' >>"$scratch/records"
}
for os in Linux Darwin Windows; do
 for arch in amd64 arm64; do
  name_arch=$arch
  [ "$arch" != amd64 ] || name_arch=x86_64
  extension=tar.gz
  [ "$os" != Windows ] || extension=zip
  record "hikyo_${version}_${os}_${name_arch}.$extension" binary "$(printf '%s' "$os" | tr '[:upper:]' '[:lower:]')/$arch" '' ''
 done
done
for format in deb rpm apk archlinux; do
 for arch in amd64 arm64; do
  record "$(package_file_name "$version" "$format" "$arch")" package "linux/$arch" "$format" "$arch"
 done
done
record checksums.txt checksum '' '' ''
record binary-provenance.json binary-provenance '' '' ''
record upgrade-compatibility.json upgrade-compatibility '' '' ''
record nightly-policy.json nightly-policy '' '' ''
record sigstore-trusted-root.json sigstore-trusted-root '' '' ''
record NIGHTLY-BUILD.txt release-notes '' '' ''
# Inventory must be exactly these twenty payloads. Never silently omit a file
# which gh release create would subsequently publish.
count=0
for path in "$directory"/* "$directory"/.[!.]* "$directory"/..?*; do
 [ -e "$path" ] || [ -L "$path" ] || continue
 name=$(basename "$path")
 jq -se --arg name "$name" 'map(select(.name == $name)) | length == 1' "$scratch/records" >/dev/null || {
  printf 'nightly: unexpected asset %s\n' "$name" >&2; exit 1;
 }
 count=$((count + 1))
done
[ "$count" -eq 20 ]
publication=$(mktemp "$directory/.nightly-manifest.XXXXXX")
jq -s --arg version "$version" --arg commit "$commit" --argjson sequence "$sequence" '
 {schema:"hikyo.dev/nightly-manifest/v1",profile:"nightly/v1",version:$version,tag:("v"+$version),source_commit:$commit,release_sequence:$sequence,artifacts:sort_by(.name)}
' "$scratch/records" >"$publication"
ln "$publication" "$directory/release-manifest.json"

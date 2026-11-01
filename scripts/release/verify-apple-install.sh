#!/bin/sh
# Native CLI installation from the exact ticketed DMG, without credentials.
set -eu
umask 077
fail() { printf 'Apple install: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 4 ] || fail 'usage: verify-apple-install.sh DIST VERSION ARCH RECEIPT'
dist=$(CDPATH='' cd -P -- "$1" && pwd)
version=$2 arch=$3 output=$4
case "$arch:$(uname -m)" in arm64:arm64 | x86_64:x86_64) ;; *) fail 'native architecture required' ;; esac
: "${APPLE_TEAM_ID:?}" "${APPLE_SIGNING_IDENTITY:?}" "${GITHUB_SHA:?}" "${GITHUB_RUN_ID:?}" "${APPLE_NOTARY_INPUT_ARTIFACT_ID:?}"
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
is_semver "$version" || fail 'invalid version'
source_commit=$GITHUB_SHA
publisher_binding='{}'
case "${APPLE_RELEASE_SOURCE_MODE:-main}" in main | dev) ;; *) fail 'invalid source mode' ;; esac
case "${APPLE_RELEASE_CHANNEL:-stable}" in
  stable)
    [ "${APPLE_RELEASE_SOURCE_MODE:-main}" = main ] || fail 'dev source requires nightly channel'
    [ -z "${APPLE_RELEASE_COMMIT:-}" ] || [ "$APPLE_RELEASE_COMMIT" = "$GITHUB_SHA" ] || fail 'stable source differs from publisher'
    ;;
  nightly)
    [ "${GITHUB_REF:-}" = refs/heads/main ] || fail 'nightly acceptance requires main publisher'
    source_commit=${APPLE_RELEASE_COMMIT:-}
    printf '%s\n' "$source_commit" | grep -Eq '^[0-9a-f]{40}$' || fail 'invalid nightly source'
    if [ "${APPLE_RELEASE_SOURCE_MODE:-main}" = dev ]; then
      printf '%s\n' "$GITHUB_SHA" | grep -Eq '^[0-9a-f]{40}$' || fail 'invalid nightly publisher'
      publisher_binding=$(jq -n --arg commit "$GITHUB_SHA" \
        '{publisher_commit:$commit,publisher_ref:"refs/heads/main"}')
    else
      [ "$source_commit" = "$GITHUB_SHA" ] || fail 'main source differs from publisher'
    fi
    [ -f "$dist/metadata.json" ] && [ ! -L "$dist/metadata.json" ] || fail 'missing regular nightly metadata'
    jq -se --arg commit "$source_commit" --arg version "$version" \
      'length == 1 and (.[0] | .commit == $commit and .version == $version)' "$dist/metadata.json" >/dev/null || \
      fail 'nightly metadata differs from selected source'
    ;;
  *) fail 'invalid release channel' ;;
esac
image=$dist/hikyo_${version}_Darwin_$arch.dmg
archive=$dist/hikyo_${version}_Darwin_$arch.tar.gz
for file in "$image" "$archive"; do
  [ -f "$file" ] && [ ! -L "$file" ] || fail 'missing regular candidate'
done
identity_requirement=$(printf '%s' "$APPLE_SIGNING_IDENTITY" | sed 's/\\/\\\\/g; s/"/\\"/g')
requirement="=anchor apple generic and certificate leaf[subject.OU] = \"$APPLE_TEAM_ID\" and certificate leaf[subject.CN] = \"$identity_requirement\" and certificate leaf[field.1.2.840.113635.100.6.1.13] exists"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-install.XXXXXX")
mounted=false
cleanup() {
  result=$?
  trap - EXIT HUP INT TERM
  if [ "$mounted" = true ]; then
    if hdiutil detach "$scratch/mount" >/dev/null; then mounted=false; else result=1; fi
  fi
  if [ "$mounted" = false ]; then rm -rf "$scratch" || result=1; fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
mkdir "$scratch/archive" "$scratch/mount" "$scratch/bin"
tar -tzf "$archive" | LC_ALL=C sort >"$scratch/entries"
printf 'LICENSE\nhikyo\n' >"$scratch/expected"
cmp -s "$scratch/entries" "$scratch/expected" || fail 'unexpected archive entries'
tar -xzf "$archive" -C "$scratch/archive"
xcrun stapler validate "$image"
codesign --verify --strict -R "$requirement" "$image"
# Actions downloads do not carry a browser quarantine attribute. Add one to
# exercise the distribution boundary; never clear quarantine to obtain a pass.
quarantine="0081;$(printf '%x' "$(date +%s)");HikyoReleaseQualification;"
xattr -w com.apple.quarantine "$quarantine" "$image"
spctl --assess --type open --context context:primary-signature --verbose=2 "$image"
mounted=true
hdiutil attach -readonly -nobrowse -mountpoint "$scratch/mount" "$image" >/dev/null
for file in LICENSE hikyo; do
  [ -f "$scratch/mount/$file" ] && [ ! -L "$scratch/mount/$file" ] &&
    [ -f "$scratch/archive/$file" ] && [ ! -L "$scratch/archive/$file" ] || fail 'nonregular installed payload'
  cmp -s "$scratch/mount/$file" "$scratch/archive/$file" || fail 'installer and archive bytes differ'
done
ditto "$scratch/mount/hikyo" "$scratch/bin/hikyo"
xattr -w com.apple.quarantine "$quarantine" "$scratch/bin/hikyo"
[ "$(lipo -archs "$scratch/bin/hikyo")" = "$arch" ] || fail 'installed architecture mismatch'
codesign --verify --strict -R "$requirement" "$scratch/bin/hikyo"
# A standalone Mach-O CLI is not an app bundle. Apple's notarization test for
# this topology is the explicit code requirement, with an online ticket check.
# The DMG assessment above and quarantined native execution remain mandatory.
codesign --verify --strict -R=notarized --check-notarization "$scratch/bin/hikyo"
"$scratch/bin/hikyo" --version >"$scratch/version"
[ "$(cat "$scratch/version")" = "$version" ] || fail 'installed version mismatch'
"$scratch/bin/hikyo" --help >/dev/null
[ -n "$(xattr -p com.apple.quarantine "$scratch/bin/hikyo")" ] || fail 'quarantine was removed'
installed_sha256=$(sha256_file "$scratch/bin/hikyo")
hdiutil detach "$scratch/mount" >/dev/null
mounted=false
jq -n --arg source "$source_commit" --argjson publisher "$publisher_binding" --arg run "$GITHUB_RUN_ID" --arg architecture "$arch" \
  --arg version "$version" --arg artifact "$APPLE_NOTARY_INPUT_ARTIFACT_ID" \
  --arg image "$(sha256_file "$image")" --arg binary "$installed_sha256" \
  '{schema:"hikyo.dev/apple-install-receipt/v1",source_commit:$source,run_id:$run,architecture:$architecture,
    version:$version,input_artifact_id:$artifact,final_dmg_sha256:$image,installed_binary_sha256:$binary,
    gatekeeper:true,executable_notarization:true,quarantine_retained:true,native_version:true,native_help:true} + $publisher' >"$scratch/receipt.json"
rm -rf "${scratch:?}/archive" "${scratch:?}/bin" "${scratch:?}/mount" || fail 'installation cleanup failed'
mv "$scratch/receipt.json" "$output"
printf 'Apple install: %s native CLI, ticket, Gatekeeper and quarantine checks passed\n' "$arch"

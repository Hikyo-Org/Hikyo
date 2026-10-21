#!/bin/sh
# Apple credentials are confined to this process and a disposable Keychain.
set +x
set -eu
umask 077

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
fail() { printf 'apple signing: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 1 ] || fail 'usage: sign-apple-artifacts.sh DIST'
[ "$(uname -s)" = Darwin ] || fail 'macOS is required'
dist=$(CDPATH='' cd -- "$1" && pwd)
for name in APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD \
  APPLE_SIGNING_IDENTITY APPLE_TEAM_ID APPLE_NOTARY_KEY_P8 APPLE_NOTARY_KEY_ID \
  APPLE_NOTARY_ISSUER_ID GITHUB_SHA GITHUB_REF; do
  printenv "$name" >/dev/null 2>&1 || fail "missing $name"
  [ -n "$(printenv "$name")" ] || fail "empty $name"
done
case "$APPLE_SIGNING_IDENTITY" in
  "Developer ID Application: "*" ($APPLE_TEAM_ID)") ;;
  *) fail 'identity must be Developer ID Application for the configured team' ;;
esac
printf '%s\n' "$APPLE_TEAM_ID" | grep -Eq '^[A-Z0-9]{10}$' || fail 'invalid team ID'
case "${APPLE_RELEASE_CHANNEL:-stable}" in
  stable)
    [ -n "${CANDIDATE_SHA256:-}" ] || fail 'missing CANDIDATE_SHA256'
    check_release_candidate_hash "$dist/release-candidate.json" "$CANDIDATE_SHA256"
    version=$(jq -r '.version' "$dist/release-candidate.json")
    [ "$GITHUB_REF" = "refs/tags/v$version" ] || fail 'tag differs from candidate'
    [ "$(jq -r '.commit' "$dist/release-candidate.json")" = "$GITHUB_SHA" ] || fail 'commit differs from candidate'
    ;;
  nightly)
    [ "$GITHUB_REF" = refs/heads/main ] || fail 'nightly signing requires main'
    version=${APPLE_RELEASE_VERSION:-}
    is_semver "$version" || fail 'invalid nightly version'
    case "$version" in *-nightly.*) ;; *) fail 'nightly version required' ;; esac
    [ "${APPLE_RELEASE_COMMIT:-}" = "$GITHUB_SHA" ] || fail 'nightly source differs from workflow SHA'
    jq -e --arg version "$version" --arg commit "$GITHUB_SHA" \
      '.version == $version and .commit == $commit' "$dist/metadata.json" >/dev/null || \
      fail 'nightly build metadata differs from source identity'
    ;;
  *) fail 'unsupported release channel' ;;
esac
(cd "$dist" && shasum -a 256 --check checksums.txt) || fail 'input archive checksum mismatch'

scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-apple.XXXXXX")
keychain=$scratch/signing.keychain-db
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ -f "$keychain" ]; then
    if ! security delete-keychain "$keychain" >/dev/null; then status=1; fi
  fi
  rm -rf "$scratch"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
printf '%s' "$APPLE_DEVELOPER_ID_P12_BASE64" | base64 --decode >"$scratch/signing.p12"
printf '%s' "$APPLE_NOTARY_KEY_P8" >"$scratch/notary.p8"
keychain_password=$(openssl rand -hex 32)
security create-keychain -p "$keychain_password" "$keychain"
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$keychain_password" "$keychain"
security import "$scratch/signing.p12" -k "$keychain" -P "$APPLE_DEVELOPER_ID_P12_PASSWORD" \
  -T /usr/bin/codesign >/dev/null
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$keychain_password" "$keychain" >/dev/null
rm "$scratch/signing.p12"
unset APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD APPLE_NOTARY_KEY_P8 keychain_password
requirement="=anchor apple generic and certificate leaf[subject.OU] = \"$APPLE_TEAM_ID\" and certificate leaf[field.1.2.840.113635.100.6.1.13] exists"

for arch in arm64 x86_64; do
  archive=$dist/hikyo_${version}_Darwin_$arch.tar.gz
  dmg=$dist/hikyo_${version}_Darwin_$arch.dmg
  [ -f "$archive" ] && [ ! -L "$archive" ] || fail "missing regular archive for $arch"
  [ ! -e "$dmg" ] || fail "disk image already exists for $arch"
  payload=$scratch/$arch
  mkdir "$payload"
  # The canonical GoReleaser archive contains exactly these two regular files.
  tar -tzf "$archive" | LC_ALL=C sort >"$scratch/entries"
  printf 'LICENSE\nhikyo\n' >"$scratch/expected"
  cmp -s "$scratch/entries" "$scratch/expected" || fail "unexpected archive entries for $arch"
  tar -xzf "$archive" -C "$payload"
  for file in hikyo LICENSE; do
    [ -f "$payload/$file" ] && [ ! -L "$payload/$file" ] || fail "invalid $file in $arch archive"
  done
  [ "$(lipo -archs "$payload/hikyo")" = "$arch" ] || fail "wrong executable architecture for $arch"
  chmod 755 "$payload/hikyo"
  codesign --force --sign "$APPLE_SIGNING_IDENTITY" --keychain "$keychain" \
    --identifier app.hikyo.cli --options runtime --timestamp "$payload/hikyo"
  codesign --verify --strict -R "$requirement" "$payload/hikyo"
  # The tarball and disk image contain the same signed executable. Notarizing
  # the outer disk image also submits its contained code to Apple's service.
  COPYFILE_DISABLE=1 tar -czf "$scratch/archive.tar.gz" -C "$payload" LICENSE hikyo
  hdiutil create -volname "Hikyo $version $arch" -srcfolder "$payload" \
    -format UDZO "$dmg" >/dev/null
  codesign --sign "$APPLE_SIGNING_IDENTITY" --keychain "$keychain" \
    --identifier app.hikyo.cli.dmg --timestamp "$dmg"
  codesign --verify --strict -R "$requirement" "$dmg"
  result=$scratch/notary-$arch.json
  if ! xcrun notarytool submit "$dmg" --key "$scratch/notary.p8" \
    --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID" \
    --wait --timeout "${APPLE_NOTARY_TIMEOUT:-30m}" --output-format json >"$result"; then
    jq -r '{id, status}' "$result" >&2 || true
    fail "notarization submission failed for $arch"
  fi
  jq -r '{id, status}' "$result"
  jq -e '.status == "Accepted" and (.id | type == "string" and length > 0)' "$result" >/dev/null || \
    fail "notarization was not accepted for $arch; inspect the submission in Apple's notary service"
  xcrun stapler staple "$dmg"
  xcrun stapler validate "$dmg"
  codesign --verify --strict -R "$requirement" "$dmg"
  spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
  mv "$scratch/archive.tar.gz" "$archive"
  printf 'apple signing: %s accepted, stapled and verified (submission %s)\n' \
    "$arch" "$(jq -r '.id' "$result")"
done

# Preserve the checksum contract: exactly the six canonical platform archives.
(
  cd "$dist"
  : >"$scratch/checksums.txt"
  for os in Darwin Linux Windows; do
    for arch in arm64 x86_64; do
      extension=tar.gz
      [ "$os" != Windows ] || extension=zip
      archive=hikyo_${version}_${os}_$arch.$extension
      [ -f "$archive" ] && [ ! -L "$archive" ] || fail "missing regular archive $archive"
      printf '%s  %s\n' "$(sha256_file "$archive")" "$archive" >>"$scratch/checksums.txt"
    done
  done
)
mv "$scratch/checksums.txt" "$dist/checksums.txt"

#!/bin/sh
# Signing and notarization credentials belong to separate, disposable phases.
set +x
set -eu
umask 077

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
fail() { printf 'apple signing: %s\n' "$*" >&2; exit 1; }
mode=full
case "${1:-}" in
  --sign-only) mode=sign; shift ;;
  --notarize-only) mode=notarize; shift ;;
  --*) fail 'usage: sign-apple-artifacts.sh [--sign-only|--notarize-only] DIST' ;;
esac
[ "$#" -eq 1 ] || fail 'usage: sign-apple-artifacts.sh [--sign-only|--notarize-only] DIST'
[ "$(uname -s)" = Darwin ] || fail 'macOS is required'
dist=$(CDPATH='' cd -- "$1" && pwd)
set -- APPLE_SIGNING_IDENTITY APPLE_TEAM_ID GITHUB_SHA GITHUB_REF
if [ "$mode" != notarize ]; then
  set -- "$@" APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD
fi
if [ "$mode" != sign ]; then
  set -- "$@" APPLE_NOTARY_KEY_P8 APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER_ID APPLE_NOTARY_EVIDENCE_DIR
fi
for name do
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
canonical_archives() {
  for os in Darwin Linux Windows; do
    for archive_arch in arm64 x86_64; do
      extension=tar.gz
      [ "$os" != Windows ] || extension=zip
      printf 'hikyo_%s_%s_%s.%s\n' "$version" "$os" "$archive_arch" "$extension"
    done
  done
}
# Check the complete canonical set before reading checksum-controlled paths.
[ -f "$dist/checksums.txt" ] && [ ! -L "$dist/checksums.txt" ] || fail 'missing regular checksums.txt'
expected_archives=$(canonical_archives | LC_ALL=C sort)
actual_archives=$(awk '{print $2}' "$dist/checksums.txt" | LC_ALL=C sort)
[ "$actual_archives" = "$expected_archives" ] || fail 'checksums must name exactly the six canonical archives'
for archive_name in $expected_archives; do
  [ -f "$dist/$archive_name" ] && [ ! -L "$dist/$archive_name" ] || fail "missing regular archive $archive_name"
done
(cd "$dist" && shasum -a 256 --check checksums.txt) || fail 'input archive checksum mismatch'

# Only notarizing phases need an empty, external evidence directory. Signing
# neither inspects notarization credentials nor accesses this directory.
if [ "$mode" != sign ]; then
  mkdir -p "$APPLE_NOTARY_EVIDENCE_DIR"
  evidence=$(CDPATH='' cd -P -- "$APPLE_NOTARY_EVIDENCE_DIR" && pwd)
  existing_evidence=$(find "$evidence" -mindepth 1 -maxdepth 1 -print) || fail 'cannot inspect notarization evidence directory'
  [ -z "$existing_evidence" ] || fail 'notarization evidence directory must be empty'
fi
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-apple.XXXXXX")
scratch=$(CDPATH='' cd -P -- "$scratch" && pwd)
keychain=
search_list_saved=false
set_keychain_search_list() {
  # security prints one quoted path per line. Read paths without eval or word
  # splitting so existing Keychains with spaces survive setup and cleanup.
  while IFS= read -r search_keychain; do
    [ -n "$search_keychain" ] || continue
    set -- "$@" "$search_keychain"
  done <"$scratch/keychain-search-list" || return 1
  security list-keychains -d user -s "$@"
}
restore_keychain() {
  keychain_status=0
  if [ "$search_list_saved" = true ]; then
    if set_keychain_search_list; then search_list_saved=false; else keychain_status=1; fi
  fi
  if [ -n "$keychain" ] && [ -f "$keychain" ]; then
    if ! security delete-keychain "$keychain" >/dev/null; then keychain_status=1; fi
  fi
  return "$keychain_status"
}
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if ! restore_keychain; then status=1; fi
  if ! rm -rf "$scratch"; then status=1; fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
if [ "$mode" != sign ]; then
  case "$evidence/" in "$scratch/"*) fail 'notarization evidence must be outside credential scratch' ;; esac
  case "$scratch/" in "$evidence/"*) fail 'notarization evidence must not contain credential scratch' ;; esac
fi
identity_requirement=$(printf '%s' "$APPLE_SIGNING_IDENTITY" | sed 's/\\/\\\\/g; s/"/\\"/g')
requirement="=anchor apple generic and certificate leaf[subject.OU] = \"$APPLE_TEAM_ID\" and certificate leaf[subject.CN] = \"$identity_requirement\" and certificate leaf[field.1.2.840.113635.100.6.1.13] exists"

extract_payload() {
  archive=$dist/hikyo_${version}_Darwin_$arch.tar.gz
  payload=$scratch/$arch
  mkdir "$payload"
  # The canonical GoReleaser archive contains exactly these two regular files.
  tar -tzf "$archive" >"$scratch/entries.raw"
  LC_ALL=C sort "$scratch/entries.raw" >"$scratch/entries"
  printf 'LICENSE\nhikyo\n' >"$scratch/expected"
  cmp -s "$scratch/entries" "$scratch/expected" || fail "unexpected archive entries for $arch"
  tar -xzf "$archive" -C "$payload"
  for file in hikyo LICENSE; do
    [ -f "$payload/$file" ] && [ ! -L "$payload/$file" ] || fail "invalid $file in $arch archive"
  done
  [ "$(lipo -archs "$payload/hikyo")" = "$arch" ] || fail "wrong executable architecture for $arch"
}

if [ "$mode" != notarize ]; then
  keychain=$scratch/signing.keychain-db
  printf '%s' "$APPLE_DEVELOPER_ID_P12_BASE64" | base64 --decode >"$scratch/signing.p12"
  keychain_password=$(openssl rand -hex 32)
  security list-keychains -d user >"$scratch/keychain-search-list.raw" || fail 'cannot read user Keychain search list'
  sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//' \
    "$scratch/keychain-search-list.raw" >"$scratch/keychain-search-list"
  security create-keychain -p "$keychain_password" "$keychain"
  security set-keychain-settings -lut 21600 "$keychain"
  security unlock-keychain -p "$keychain_password" "$keychain"
  security import "$scratch/signing.p12" -k "$keychain" -P "$APPLE_DEVELOPER_ID_P12_PASSWORD" \
    -T /usr/bin/codesign >/dev/null || fail 'cannot import Developer ID identity'
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$keychain_password" "$keychain" >/dev/null
  # codesign --keychain restricts identity selection, but certificate-chain lookup
  # still uses the user search list. A preconfigured login Keychain can mask this.
  search_list_saved=true
  set_keychain_search_list "$keychain" || fail 'cannot register signing Keychain in user search list'
  security find-identity -v -p codesigning "$keychain" >"$scratch/identities" || fail 'cannot inspect imported signing identities'
  grep -F -- "\"$APPLE_SIGNING_IDENTITY\"" "$scratch/identities" >/dev/null || \
    fail 'configured Developer ID identity is not valid in the imported Keychain'
  printf 'apple signing: imported Developer ID identity is valid and its Keychain is registered\n'
  rm "$scratch/signing.p12"
  unset APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD keychain_password
  mkdir "$scratch/archives" "$scratch/images"
  for arch in arm64 x86_64; do
    dmg=$scratch/images/hikyo_${version}_Darwin_$arch.dmg
    [ ! -e "$dist/$(basename "$dmg")" ] && [ ! -L "$dist/$(basename "$dmg")" ] || fail "disk image already exists for $arch"
    extract_payload
    chmod 755 "$payload/hikyo"
    codesign --force --sign "$APPLE_SIGNING_IDENTITY" --keychain "$keychain" \
      --identifier app.hikyo.cli --options runtime --timestamp "$payload/hikyo"
    codesign --verify --strict -R "$requirement" "$payload/hikyo"
    # Both outputs contain the same signed executable. Stage them privately until
    # both architectures and Keychain cleanup succeed, independent of Apple API.
    COPYFILE_DISABLE=1 tar -czf "$scratch/archives/$(basename "$archive")" -C "$payload" LICENSE hikyo
    # HFS+ avoids the APFS image attachment/preflight failure with release payloads.
    hdiutil create -volname "Hikyo $version $arch" -srcfolder "$payload" \
      -fs HFS+ -format UDZO "$dmg" >/dev/null
    codesign --sign "$APPLE_SIGNING_IDENTITY" --keychain "$keychain" \
      --identifier app.hikyo.cli.dmg --timestamp "$dmg"
    codesign --verify --strict -R "$requirement" "$dmg"
  done
  restore_keychain || fail 'cannot restore and delete signing Keychain'
  # Preserve exactly the six canonical archives and all Linux/Windows bytes.
  : >"$scratch/checksums.txt"
  for archive_name in $expected_archives; do
    source_archive=$dist/$archive_name
    [ ! -f "$scratch/archives/$archive_name" ] || source_archive=$scratch/archives/$archive_name
    printf '%s  %s\n' "$(sha256_file "$source_archive")" "$archive_name" >>"$scratch/checksums.txt"
  done
  for arch in arm64 x86_64; do
    mv "$scratch/archives/hikyo_${version}_Darwin_$arch.tar.gz" "$dist/"
    mv "$scratch/images/hikyo_${version}_Darwin_$arch.dmg" "$dist/"
  done
  mv "$scratch/checksums.txt" "$dist/checksums.txt"
  printf 'apple signing: both architectures signed and verified; disk images are unstapled\n'
fi
[ "$mode" != sign ] || exit 0

# In a notarization job, verify the entire retained handoff before the first API
# call. This phase never imports a Keychain, signs code or rewrites tarballs.
for arch in arm64 x86_64; do
  dmg=$dist/hikyo_${version}_Darwin_$arch.dmg
  [ -f "$dmg" ] && [ ! -L "$dmg" ] || fail "missing regular disk image for $arch"
  if [ "$mode" = notarize ]; then extract_payload; fi
  codesign --verify --strict -R "$requirement" "$scratch/$arch/hikyo"
  codesign --verify --strict -R "$requirement" "$dmg"
done
printf '%s' "$APPLE_NOTARY_KEY_P8" >"$scratch/notary.p8"
unset APPLE_NOTARY_KEY_P8

write_notary_receipt() {
  jq -n --arg version "$version" --arg commit "$GITHUB_SHA" --arg architecture "$arch" \
    --arg filename "$(basename "$dmg")" --arg sha256 "$submitted_sha256" \
    --arg submission_id "$submission_id" --arg status "$notary_status" \
    --arg availability "$diagnostic_availability" --arg logfile "$log_filename" \
    --arg warnings "$warning_count" '
    {schema:"hikyo.dev/apple-notarization-receipt/v1",version:$version,source_commit:$commit,
     architecture:$architecture,archive_filename:$filename,submitted_sha256:$sha256,
     submission_id:(if $submission_id == "" then null else $submission_id end),status:$status,
     diagnostics:{availability:$availability,
       log_filename:(if $logfile == "" then null else $logfile end),
       warning_count:(if $warnings == "" then null else ($warnings | tonumber) end)}}
  ' >"$scratch/receipt-$arch.json"
  cp "$scratch/receipt-$arch.json" "$evidence/$arch-receipt.json"
}

for arch in arm64 x86_64; do
  dmg=$dist/hikyo_${version}_Darwin_$arch.dmg
  result=$scratch/notary-$arch.json
  submitted_sha256=$(sha256_file "$dmg")
  submitted=true
  if ! xcrun notarytool submit "$dmg" --key "$scratch/notary.p8" \
    --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID" \
    --wait --timeout "${APPLE_NOTARY_TIMEOUT:-30m}" --output-format json \
    >"$result" 2>"$scratch/submit-$arch.err"; then
    submitted=false
  fi
  # Never publish the raw response or command errors. Keep only a bounded UUID
  # and normalized public status; even a timed-out submission gets a receipt.
  submission_id=$(jq -sr '
    (if length == 1 then .[0].id else null end) |
    if type == "string" and test("^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$")
    then ascii_downcase else empty end
  ' "$result" 2>/dev/null) || submission_id=
  notary_status=$(jq -sr '
    (if length == 1 then .[0].status else null end) |
    (if type == "string" then ascii_downcase else "" end) |
    if . == "accepted" then "Accepted" elif . == "invalid" then "Invalid"
    elif . == "rejected" then "Rejected"
    elif . == "in progress" or . == "inprogress" then "In Progress" else "Unknown" end
  ' "$result" 2>/dev/null) || notary_status=Unknown
  diagnostic_availability=unavailable
  log_filename=
  warning_count=
  write_notary_receipt
  case "$notary_status:$submission_id" in
    Accepted:?* | Invalid:?* | Rejected:?*)
      log=$scratch/log-$arch.json
      if xcrun notarytool log "$submission_id" --key "$scratch/notary.p8" \
        --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID" "$log" \
        >"$scratch/log-$arch.out" 2>"$scratch/log-$arch.err"; then
        # Apple may uppercase jobId and omit sha256. The receipt always binds
        # the exact submitted DMG bytes before stapling changes its hash.
        if jq -se --arg id "$submission_id" --arg status "$notary_status" \
          --arg filename "$(basename "$dmg")" --arg sha256 "$submitted_sha256" '
          length == 1 and (.[0] | type == "object" and
            (.jobId | type == "string" and ascii_downcase == $id) and
            (.status | type == "string" and ascii_downcase == ($status | ascii_downcase)) and
            ((has("archiveFilename") | not) or .archiveFilename == $filename) and
            ((has("sha256") | not) or (.sha256 | type == "string" and ascii_downcase == $sha256)) and
            (.issues == null or (.issues | type == "array" and
              all(.[]; type == "object" and (.severity | type == "string")))))
        ' "$log" >/dev/null 2>&1; then
          diagnostic_availability=available
          log_filename=$arch-notary-log.json
          warning_count=$(jq '[.issues[]? | select(.severity | ascii_downcase == "warning")] | length' "$log")
          cp "$log" "$evidence/$log_filename"
        else
          diagnostic_availability=invalid
        fi
      fi
      write_notary_receipt
      ;;
  esac
  printf 'apple signing: %s submission %s status %s; diagnostics %s; warnings %s; log %s\n' \
    "$arch" "${submission_id:-unavailable}" "$notary_status" "$diagnostic_availability" \
    "${warning_count:-unavailable}" "${log_filename:-unavailable}"
  if [ "${warning_count:-0}" -gt 0 ]; then
    # Provider-controlled warning text belongs in the saved log, never in a
    # GitHub command annotation. This annotation contains only our fixed text.
    printf '::warning title=Apple notarization warnings::%s warning(s) for %s; inspect %s in the notarization evidence artifact.\n' \
      "$warning_count" "$arch" "$log_filename"
  fi
  [ "$submitted" = true ] || fail "notarization submission failed for $arch; see its receipt"
  [ "$notary_status" = Accepted ] || \
    fail "notarization was not accepted for $arch; see its receipt"
  [ "$diagnostic_availability" = available ] || \
    fail "accepted notarization has no valid matching diagnostic log for $arch"
  xcrun stapler staple "$dmg"
  xcrun stapler validate "$dmg"
  codesign --verify --strict -R "$requirement" "$dmg"
  spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
  printf 'apple signing: %s accepted, stapled and verified (submission %s)\n' \
    "$arch" "$submission_id"
done

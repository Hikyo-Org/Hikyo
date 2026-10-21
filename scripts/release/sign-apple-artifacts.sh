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
  APPLE_NOTARY_ISSUER_ID APPLE_NOTARY_EVIDENCE_DIR GITHUB_SHA GITHUB_REF; do
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

# Evidence lives outside the disposable credential directory. Only our safe
# receipts and validated public Apple logs are copied here.
mkdir -p "$APPLE_NOTARY_EVIDENCE_DIR"
evidence=$(CDPATH='' cd -P -- "$APPLE_NOTARY_EVIDENCE_DIR" && pwd)
existing_evidence=$(find "$evidence" -mindepth 1 -maxdepth 1 -print) || fail 'cannot inspect notarization evidence directory'
[ -z "$existing_evidence" ] || fail 'notarization evidence directory must be empty'
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-apple.XXXXXX")
scratch=$(CDPATH='' cd -P -- "$scratch" && pwd)
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
case "$evidence/" in "$scratch/"*) fail 'notarization evidence must be outside credential scratch' ;; esac
case "$scratch/" in "$evidence/"*) fail 'notarization evidence must not contain credential scratch' ;; esac
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
  mv "$scratch/archive.tar.gz" "$archive"
  printf 'apple signing: %s accepted, stapled and verified (submission %s)\n' \
    "$arch" "$submission_id"
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

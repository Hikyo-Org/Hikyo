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
  --prepare-only) mode=prepare; shift ;;
  --submit-only) mode=submit; shift ;;
  --notarize-only) mode=notarize; shift ;;
  --*) fail 'usage: sign-apple-artifacts.sh [--sign-only|--prepare-only|--submit-only|--notarize-only] DIST' ;;
esac
[ "$#" -eq 1 ] || fail 'usage: sign-apple-artifacts.sh [--sign-only|--prepare-only|--submit-only|--notarize-only] DIST'
[ "$(uname -s)" = Darwin ] || fail 'macOS is required'
dist=$(CDPATH='' cd -P -- "$1" && pwd)
set -- APPLE_SIGNING_IDENTITY APPLE_TEAM_ID GITHUB_SHA GITHUB_REF
if [ "$mode" = sign ] || [ "$mode" = full ]; then
  set -- "$@" APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD
fi
if [ "$mode" != sign ]; then
  set -- "$@" APPLE_NOTARY_EVIDENCE_DIR GITHUB_REPOSITORY GITHUB_RUN_ID GITHUB_RUN_ATTEMPT APPLE_NOTARY_INPUT_ARTIFACT_ID APPLE_NOTARY_INPUT_SHA256
  if [ "$mode" != prepare ]; then
    set -- "$@" APPLE_NOTARY_KEY_P8 APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER_ID
  fi
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

# Signing neither inspects notarization credentials nor accesses evidence.
lock_owned=false
if [ "$mode" != sign ]; then
  printf '%s\n' "$GITHUB_SHA" | grep -Eq '^[0-9a-f]{40}$' || fail 'invalid source SHA'
  printf '%s\n' "$GITHUB_REPOSITORY" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || fail 'invalid repository'
  for number in "$GITHUB_RUN_ID" "$GITHUB_RUN_ATTEMPT" "$APPLE_NOTARY_INPUT_ARTIFACT_ID"; do
    printf '%s\n' "$number" | grep -Eq '^[1-9][0-9]{0,17}$' || fail 'invalid run or input artifact ID'
  done
  printf '%s\n' "$APPLE_NOTARY_INPUT_SHA256" | grep -Eq '^[0-9a-f]{64}$' || fail 'invalid signed input digest'
  printf '%s\n' "${APPLE_NOTARY_TIMEOUT:-30m}" | grep -Eq '^[1-9][0-9]{0,4}[smh]?$' || fail 'invalid finite notary timeout'
  evidence_component=$APPLE_NOTARY_EVIDENCE_DIR
  case "$evidence_component" in /*) ;; *) evidence_component=$PWD/$evidence_component ;; esac
  while [ "$evidence_component" != / ]; do
    evidence_component=${evidence_component%/}
    [ ! -L "$evidence_component" ] || fail 'symlink evidence directory component'
    evidence_component=$(dirname "$evidence_component")
  done
  mkdir -p "$APPLE_NOTARY_EVIDENCE_DIR"
  evidence=$(CDPATH='' cd -P -- "$APPLE_NOTARY_EVIDENCE_DIR" && pwd)
  case "$evidence/" in "$dist/"*) fail 'evidence must be outside release payloads' ;; esac
  case "$dist/" in "$evidence/"*) fail 'evidence must not contain release payloads' ;; esac
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
  if [ -n "${owned_mount:-}" ]; then
    if hdiutil detach "$owned_mount" >/dev/null 2>&1; then owned_mount=; else status=1; fi
  fi
  if [ -n "${staging:-}" ]; then
    if ! rm -rf "$staging"; then status=1; fi
  fi
  if [ -z "${owned_mount:-}" ]; then
    if ! rm -rf "$scratch"; then status=1; fi
  else
    # Never recursively remove an attached filesystem. Remove private keys and
    # client output, retaining only the owned mountpoint for operator cleanup.
    for private_entry in "$scratch/"*; do
      [ "$private_entry" != "$owned_mount" ] || continue
      if ! rm -rf "$private_entry"; then status=1; fi
    done
    printf 'apple signing: owned image could not be detached; private files removed, mountpoint retained\n' >&2
  fi
  if [ "$lock_owned" = true ]; then
    if ! rmdir "$evidence/.lock"; then status=1; fi
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
if [ "$mode" != sign ]; then
  case "$evidence/" in "$scratch/"*) fail 'notarization evidence must be outside credential scratch' ;; esac
  case "$scratch/" in "$evidence/"*) fail 'notarization evidence must not contain credential scratch' ;; esac
  mkdir "$evidence/.lock" 2>/dev/null || fail 'evidence directory is locked; do not replay an uncertain submission'
  lock_owned=true
  # Only these public evidence files are reusable. Never follow restored links,
  # accept arbitrary old receipts, or silently clean up interrupted writes.
  for entry in "$evidence/"* "$evidence/".[!.]* "$evidence/"..?*; do
    [ -e "$entry" ] || [ -L "$entry" ] || continue
    [ "$entry" != "$evidence/.lock" ] || continue
    [ -f "$entry" ] && [ ! -L "$entry" ] || fail 'nonregular evidence entry'
    case "${entry##*/}" in
      arm64-receipt.json | x86_64-receipt.json | arm64-notary-log.json | x86_64-notary-log.json | arm64-submitted.dmg | x86_64-submitted.dmg) ;;
      *) fail 'unexpected evidence entry' ;;
    esac
  done
  if [ -f "$evidence/arm64-receipt.json" ] || [ -f "$evidence/x86_64-receipt.json" ]; then
    [ -f "$evidence/arm64-receipt.json" ] && [ -f "$evidence/x86_64-receipt.json" ] || fail 'incomplete retained intent pair'
    [ "$mode" != full ] || fail 'full signing cannot reuse a signed handoff; use phased recovery'
  else
    [ -z "$(find "$evidence" -type f -print)" ] || fail 'unbound retained evidence'
    case "$mode" in submit | notarize) fail 'prepare both stage intents before submitting or recovering' ;; esac
  fi
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

if [ "$mode" = sign ] || [ "$mode" = full ]; then
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
    image_attempt=1
    printf 'hdiutil: create failed - Resource busy\n' >"$scratch/image-busy.err"
    while :; do
      if hdiutil create -volname "Hikyo $version $arch" -srcfolder "$payload" \
        -nospotlight -fs HFS+ -format UDZO "$dmg" > /dev/null 2>"$scratch/image-create.err"; then
        cat "$scratch/image-create.err" >&2
        break
      else
        image_status=$?
      fi
      cat "$scratch/image-create.err" >&2
      printf 'apple signing: disk image creation failed for %s (attempt %s/3, status %s)\n' \
        "$arch" "$image_attempt" "$image_status" >&2
      # Match the complete diagnostic, never a generic mention or mixed errors.
      if [ "$image_attempt" -ge 3 ] || ! cmp -s "$scratch/image-busy.err" "$scratch/image-create.err"; then
        exit "$image_status"
      fi
      # hdiutil can leave a partial image even when creation fails.
      rm -f "$dmg"
      sleep 2
      image_attempt=$((image_attempt + 1))
    done
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

# Verify the complete signed handoff and every restored receipt before credentials
# are materialized or the first provider operation can authorize nightly fallback.
input_digest=$("$script_dir/apple-artifact-digest.sh" "$dist" "$version")
[ "$input_digest" = "$APPLE_NOTARY_INPUT_SHA256" ] || fail 'signed Actions input digest mismatch'
channel=${APPLE_RELEASE_CHANNEL:-stable}
candidate_sha256=${CANDIDATE_SHA256:-}
[ "$channel" != nightly ] || candidate_sha256=

atomic_copy() {
  [ ! -L "$2" ] || fail 'symlink evidence destination'
  temporary=$(mktemp "$evidence/.write.XXXXXX")
  if cp "$1" "$temporary" && chmod 600 "$temporary" && mv -f "$temporary" "$2"; then :; else
    fail 'cannot atomically retain evidence'
  fi
}
valid_log() {
  # For a retained UUID Apple's optional filename/hash fields must match when
  # present. Lost-UUID reconciliation requires BOTH fields, never status alone.
  jq -se --arg id "$submission_id" --arg status "$notary_status" \
    --arg filename "$filename" --arg sha256 "$submitted_sha256" --arg strict "$1" '
    length == 1 and (.[0] | type == "object" and
      (.jobId | type == "string" and ascii_downcase == $id) and
      (.status | type == "string" and ascii_downcase == ($status | ascii_downcase)) and
      (if $strict == "true" then .archiveFilename == $filename and
         (.sha256 | type == "string" and ascii_downcase == $sha256)
       else ((has("archiveFilename") | not) or .archiveFilename == $filename) and
         ((has("sha256") | not) or (.sha256 | type == "string" and ascii_downcase == $sha256)) end) and
      (.issues == null or (.issues | type == "array" and
        all(.[]; type == "object" and (.severity | type == "string")))))
  ' "$2" >/dev/null 2>&1
}
load_receipt() {
  receipt=$evidence/$arch-receipt.json
  filename=hikyo_${version}_Darwin_$arch.dmg
  dmg=$dist/$filename
  submitted_sha256=$(sha256_file "$dmg")
  submission_id=$(jq -r '.submission_id // empty' "$receipt")
  notary_status=$(jq -r '.status' "$receipt")
}
write_receipt() {
  jq --arg id "$submission_id" --arg status "$notary_status" --arg phase "$phase" \
    --arg availability "$diagnostic_availability" --arg logfile "$log_filename" --arg warnings "$warning_count" '
    .submission_id = (if $id == "" then null else $id end) | .status = $status | .phase = $phase |
    .diagnostics = {availability:$availability,
      log_filename:(if $logfile == "" then null else $logfile end),
      warning_count:(if $warnings == "" then null else ($warnings | tonumber) end)}
  ' "$receipt" >"$scratch/receipt.json" || fail 'cannot encode notarization receipt'
  atomic_copy "$scratch/receipt.json" "$receipt"
}
for arch in arm64 x86_64; do
  dmg=$dist/hikyo_${version}_Darwin_$arch.dmg
  [ -f "$dmg" ] && [ ! -L "$dmg" ] || fail "missing regular disk image for $arch"
  if [ "$mode" != full ]; then extract_payload; fi
  codesign --verify --strict -R "$requirement" "$scratch/$arch/hikyo"
  codesign --verify --strict -R "$requirement" "$dmg"
  receipt=$evidence/$arch-receipt.json
  [ -f "$receipt" ] || continue
  submitted_sha256=$(sha256_file "$dmg")
  jq -se --arg version "$version" --arg commit "$GITHUB_SHA" --arg ref "$GITHUB_REF" \
    --arg channel "$channel" --arg candidate "$candidate_sha256" --arg arch "$arch" \
    --arg filename "${dmg##*/}" --arg sha "$submitted_sha256" --arg repo "$GITHUB_REPOSITORY" \
    --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" \
    --arg artifact "$APPLE_NOTARY_INPUT_ARTIFACT_ID" --arg digest "$input_digest" \
    --arg team "$APPLE_TEAM_ID" --arg identity "$APPLE_SIGNING_IDENTITY" '
    def uuid: type == "string" and test("^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$");
    length == 1 and (.[0] | type == "object" and
      .schema == "hikyo.dev/apple-notarization-receipt/v2" and
      .version == $version and .source_commit == $commit and .source_ref == $ref and
      .channel == $channel and .candidate_sha256 == $candidate and
      .architecture == $arch and .archive_filename == $filename and .submitted_sha256 == $sha and
      .repository == $repo and .original_run_id == $run and
      (.original_run_attempt | type == "string" and test("^[1-9][0-9]{0,17}$")) and
      (.original_run_attempt | tonumber) <= ($attempt | tonumber) and
      .input_artifact_id == $artifact and .input_sha256 == $digest and
      .team_id == $team and .signing_identity == $identity and
      (.phase == "Prepared" or .phase == "Submitting" or .phase == "Submitted") and
      (if .phase == "Submitted" then (.submission_id | uuid) else .submission_id == null end) and
      (.status == "Unknown" or .status == "In Progress" or .status == "Accepted" or .status == "Invalid" or .status == "Rejected") and
      (if .phase == "Prepared" then .status == "Unknown" else true end) and
      (.final_sha256 == null or (.final_sha256 | type == "string" and test("^[0-9a-f]{64}$"))) and
      (.diagnostics | type == "object" and
        (.availability == "unavailable" or .availability == "invalid" or .availability == "available")) and
      (if .diagnostics.availability == "available" then
        .phase == "Submitted" and (.status == "Accepted" or .status == "Invalid" or .status == "Rejected") and
        .diagnostics.log_filename == ($arch + "-notary-log.json") and
        (.diagnostics.warning_count | type == "number" and . >= 0 and floor == .)
       else .diagnostics.log_filename == null and .diagnostics.warning_count == null end))
  ' "$receipt" >/dev/null 2>&1 || fail "malformed or mismatched retained receipt for $arch"
  [ -f "$evidence/$arch-submitted.dmg" ] && \
    [ "$(sha256_file "$evidence/$arch-submitted.dmg")" = "$submitted_sha256" ] || fail 'retained original bytes mismatch'
  if [ "$(jq -r '.diagnostics.availability' "$receipt")" = available ]; then
    load_receipt
    valid_log false "$evidence/$arch-notary-log.json" || fail 'malformed or mismatched retained diagnostics'
    [ "$(jq '[.issues[]? | select(.severity | ascii_downcase == "warning")] | length' "$evidence/$arch-notary-log.json")" = \
      "$(jq '.diagnostics.warning_count' "$receipt")" ] || fail 'retained warning count mismatch'
  elif [ -e "$evidence/$arch-notary-log.json" ]; then
    # A crash between atomic log and receipt writes is safe to stop, never reuse
    # as authority for a new upload or silently overwrite unrelated diagnostics.
    fail 'unbound retained diagnostic log'
  fi
done
if [ ! -f "$evidence/arm64-receipt.json" ]; then
  for arch in arm64 x86_64; do
    filename=hikyo_${version}_Darwin_$arch.dmg
    submitted_sha256=$(sha256_file "$dist/$filename")
    atomic_copy "$dist/$filename" "$evidence/$arch-submitted.dmg"
    jq -n --arg version "$version" --arg commit "$GITHUB_SHA" --arg ref "$GITHUB_REF" \
      --arg channel "$channel" --arg candidate "$candidate_sha256" --arg arch "$arch" \
      --arg filename "$filename" --arg sha "$submitted_sha256" --arg repo "$GITHUB_REPOSITORY" \
      --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" \
      --arg artifact "$APPLE_NOTARY_INPUT_ARTIFACT_ID" --arg digest "$input_digest" \
      --arg team "$APPLE_TEAM_ID" --arg identity "$APPLE_SIGNING_IDENTITY" '
      {schema:"hikyo.dev/apple-notarization-receipt/v2",version:$version,source_commit:$commit,source_ref:$ref,
       channel:$channel,candidate_sha256:$candidate,architecture:$arch,archive_filename:$filename,
       submitted_sha256:$sha,repository:$repo,original_run_id:$run,original_run_attempt:$attempt,
       input_artifact_id:$artifact,input_sha256:$digest,team_id:$team,signing_identity:$identity,
       phase:"Prepared",submission_id:null,status:"Unknown",final_sha256:null,
       diagnostics:{availability:"unavailable",log_filename:null,warning_count:null}}
    ' >"$scratch/receipt.json"
    atomic_copy "$scratch/receipt.json" "$evidence/$arch-receipt.json"
  done
fi
[ -z "${GITHUB_OUTPUT:-}" ] || printf 'inputs_verified=true\n' >>"$GITHUB_OUTPUT"
[ "$mode" != prepare ] || exit 0
printf '%s' "$APPLE_NOTARY_KEY_P8" >"$scratch/notary.p8"
unset APPLE_NOTARY_KEY_P8
notary() {
  xcrun notarytool "$@" --key "$scratch/notary.p8" \
    --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID" --output-format json
}
read_result() {
  result_id=$(jq -sr '
    (if length == 1 then .[0].id else null end) |
    if type == "string" and test("^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$")
    then ascii_downcase else empty end
  ' "$1" 2>/dev/null) || result_id=
  result_status=$(jq -sr '
    (if length == 1 then .[0].status else null end) |
    (if type == "string" then ascii_downcase else "" end) |
    if . == "accepted" then "Accepted" elif . == "invalid" then "Invalid"
    elif . == "rejected" then "Rejected"
    elif . == "in progress" or . == "inprogress" then "In Progress" else "Unknown" end
  ' "$1" 2>/dev/null) || result_status=Unknown
}
fetch_log() {
  log=$scratch/log-$arch.json
  diagnostic_availability=unavailable
  log_filename=
  warning_count=
  if xcrun notarytool log "$submission_id" --key "$scratch/notary.p8" \
    --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID" "$log" \
    >"$scratch/log.out" 2>"$scratch/log.err"; then
    if valid_log "$1" "$log"; then
      diagnostic_availability=available
      log_filename=$arch-notary-log.json
      warning_count=$(jq '[.issues[]? | select(.severity | ascii_downcase == "warning")] | length' "$log") || return 1
    else
      diagnostic_availability=invalid
    fi
  fi
}
reconcile() {
  # History is a bounded, possibly incomplete view. Empty/lagging history is
  # never evidence of failure. All same-name jobs must have matching diagnostics
  # available before uniqueness can be established, including pending jobs.
  notary history >"$scratch/history.json" 2>"$scratch/history.err" || return 1
  jq -se 'length == 1 and (.[0] | type == "object" and (.history | type == "array" and length <= 1000 and
    all(.[]; type == "object" and (.name | type == "string") and
      (.id | type == "string" and test("^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$")) and
      (.status | type == "string"))))' "$scratch/history.json" >/dev/null 2>&1 || return 1
  jq -r --arg name "$filename" '.history[] | select(.name == $name) | .id | ascii_downcase' \
    "$scratch/history.json" >"$scratch/history-ids" || return 1
  matches=0
  matched_id=
  while IFS= read -r history_id; do
    submission_id=$history_id
    notary info "$submission_id" >"$scratch/info.json" 2>"$scratch/info.err" || return 1
    read_result "$scratch/info.json"
    [ "$result_id" = "$submission_id" ] || return 1
    notary_status=$result_status
    case "$notary_status" in Accepted | Invalid | Rejected) ;; *) return 1 ;; esac
    fetch_log true || return 1
    [ "$diagnostic_availability" = available ] || return 1
    matches=$((matches + 1))
    matched_id=$submission_id
    cp "$log" "$scratch/reconciled-log.json" || return 1
  done <"$scratch/history-ids"
  [ "$matches" -eq 1 ] || return 1
  submission_id=$matched_id
  atomic_copy "$scratch/reconciled-log.json" "$evidence/$arch-notary-log.json"
  phase=Submitted
  write_receipt
}
submission_failed=false
submission_pending=false
for arch in arm64 x86_64; do
  load_receipt
  [ -z "$submission_id" ] || continue
  phase=$(jq -r '.phase' "$receipt")
  prepared_attempt=$(jq -r '.original_run_attempt' "$receipt")
  if [ "$mode" != notarize ] && [ "$phase" = Prepared ] && [ "$prepared_attempt" = "$GITHUB_RUN_ATTEMPT" ]; then
    # Mark the upload uncertain BEFORE invoking the provider. Even the same
    # attempt must reconcile if the client dies without recording a UUID.
    phase=Submitting
    diagnostic_availability=unavailable; log_filename=; warning_count=
    write_receipt
    submit_ok=true
    notary submit "$dmg" --no-wait >"$scratch/submit.json" 2>"$scratch/submit.err" || submit_ok=false
    read_result "$scratch/submit.json"
    submission_id=$result_id
    notary_status=$result_status
    [ -z "$submission_id" ] || phase=Submitted
    write_receipt
    if [ -n "$submission_id" ] && [ "$notary_status" = 'In Progress' ]; then submission_pending=true; fi
    if [ "$submit_ok" != true ] || [ -z "$submission_id" ]; then submission_failed=true; fi
  else
    if ! reconcile; then
      # Preserve the uncertain receipt. No UUID, empty history, an unavailable
      # log or ambiguity cannot truthfully advertise recovery_pending=true.
      submission_failed=true
    fi
  fi
done
if [ "$mode" = submit ]; then
  if [ "$submission_pending" = true ] && [ -n "${GITHUB_OUTPUT:-}" ]; then printf 'recovery_pending=true\n' >>"$GITHUB_OUTPUT"; fi
  [ "$submission_failed" = false ] || fail 'one or more submissions unresolved; preserve both receipts and reconcile'
  [ -z "${GITHUB_OUTPUT:-}" ] || printf 'submitted=true\n' >>"$GITHUB_OUTPUT"
  exit 0
fi

accepted=true
pending=false
for arch in arm64 x86_64; do
  load_receipt
  if [ -z "$submission_id" ]; then accepted=false; continue; fi
  # Always query the retained UUID. A client error never authorizes a new upload.
  if ! notary info "$submission_id" >"$scratch/info.json" 2>"$scratch/info.err"; then
    accepted=false
    # A retained UUID with an unavailable status query is safe to query again,
    # never to upload again. Preserve Unknown as Unknown in the receipt.
    case "$notary_status" in Invalid | Rejected) ;; *) pending=true ;; esac
    continue
  fi
  read_result "$scratch/info.json"
  if [ "$result_id" != "$submission_id" ] || [ "$result_status" = Unknown ]; then accepted=false; continue; fi
  case "$notary_status" in
    Accepted | Invalid | Rejected)
      if [ "$notary_status" != "$result_status" ]; then accepted=false; continue; fi ;;
  esac
  notary_status=$result_status
  if [ "$notary_status" = 'In Progress' ]; then
    # Persist positive pending evidence before a bounded wait can time out.
    phase=Submitted; diagnostic_availability=unavailable; log_filename=; warning_count=
    write_receipt
    notary wait "$submission_id" --timeout "${APPLE_NOTARY_TIMEOUT:-30m}" \
      >"$scratch/wait.json" 2>"$scratch/wait.err" || :
    read_result "$scratch/wait.json"
    if [ "$result_id" = "$submission_id" ] && [ "$result_status" != Unknown ]; then
      notary_status=$result_status
    else
      # Query once after an interrupted wait; never extend the wait indefinitely.
      if notary info "$submission_id" >"$scratch/info.json" 2>"$scratch/info.err"; then
        read_result "$scratch/info.json"
        if [ "$result_id" = "$submission_id" ] && [ "$result_status" != Unknown ]; then notary_status=$result_status; fi
      fi
    fi
  fi
  phase=Submitted; diagnostic_availability=unavailable; log_filename=; warning_count=
  case "$notary_status" in
    Accepted | Invalid | Rejected)
      fetch_log false
      if [ "$notary_status" = Accepted ] && [ "$diagnostic_availability" = unavailable ]; then pending=true; fi
      if [ "$diagnostic_availability" != available ] && [ -f "$evidence/$arch-notary-log.json" ]; then
        # Keep a previously validated receipt/log pair intact on a new provider
        # failure. This invocation still fails; a later retry can fetch again.
        accepted=false
        continue
      fi
      if [ "$diagnostic_availability" = available ]; then atomic_copy "$log" "$evidence/$arch-notary-log.json"; fi
      ;;
    'In Progress') pending=true ;;
  esac
  write_receipt
  printf 'apple signing: %s submission %s status %s; diagnostics %s; warnings %s\n' \
    "$arch" "$submission_id" "$notary_status" "$diagnostic_availability" "${warning_count:-unavailable}"
  if [ "${warning_count:-0}" -gt 0 ]; then
    printf '::warning title=Apple notarization warnings::%s warning(s) for %s; inspect %s in the notarization evidence artifact.\n' \
      "$warning_count" "$arch" "$log_filename"
  fi
  if [ "$notary_status" != Accepted ] || [ "$diagnostic_availability" != available ]; then accepted=false; fi
done
if [ "$pending" = true ]; then
  [ -z "${GITHUB_OUTPUT:-}" ] || printf 'recovery_pending=true\n' >>"$GITHUB_OUTPUT"
fi
[ "$accepted" = true ] || fail 'notarization not accepted with matching diagnostics for both architectures'

# Copy both DMGs on the same filesystem as dist. No staple, assessment, mount,
# detach or private-credential cleanup failure may change the signed originals.
staging=$(mktemp -d "$dist/.apple-final.XXXXXX")
owned_mount=
for arch in arm64 x86_64; do
  load_receipt
  final=$staging/$filename
  cp "$dmg" "$final"
  if xcrun stapler staple "$final" >"$scratch/staple.out" 2>"$scratch/staple.err"; then :; else
    ticket_status=$?
    # Exit 68 alone does not distinguish a missing/invalid ticket from the
    # observed CloudKit connection-loss diagnostic. Only that transport witness
    # permits automatic ticket-only continuation, with the same Accepted UUIDs.
    if [ "$ticket_status" -eq 68 ] && grep -F 'NSURLErrorDomain Code=-1005' "$scratch/staple.err" >/dev/null &&
      [ -n "${GITHUB_OUTPUT:-}" ]; then printf 'recovery_pending=true\n' >>"$GITHUB_OUTPUT"; fi
    fail 'ticket stapling failed; signed originals retained'
  fi
  xcrun stapler validate "$final" >"$scratch/validate.out" 2>"$scratch/validate.err" || fail 'ticket validation failed'
  codesign --verify --strict -R "$requirement" "$final"
  spctl --assess --type open --context context:primary-signature --verbose=2 "$final"
  mountpoint=$scratch/mount-$arch
  mkdir "$mountpoint"
  owned_mount=$mountpoint
  hdiutil attach -readonly -nobrowse -mountpoint "$mountpoint" "$final" >"$scratch/attach.out" 2>"$scratch/attach.err" || fail 'cannot mount staged image'
  for file in hikyo LICENSE; do
    [ -f "$mountpoint/$file" ] && [ ! -L "$mountpoint/$file" ] || fail 'nonregular mounted payload'
    cmp -s "$mountpoint/$file" "$scratch/$arch/$file" || fail 'mounted payload differs from signed archive'
  done
  [ "$(lipo -archs "$mountpoint/hikyo")" = "$arch" ] || fail 'mounted executable architecture mismatch'
  codesign --verify --strict -R "$requirement" "$mountpoint/hikyo"
  hdiutil detach "$owned_mount" >"$scratch/detach.out" 2>"$scratch/detach.err" || fail 'cannot detach owned image'
  owned_mount=
  jq --arg sha "$(sha256_file "$final")" '.final_sha256 = $sha' "$receipt" >"$scratch/receipt.json"
  atomic_copy "$scratch/receipt.json" "$receipt"
done
# Cleanup is part of the promotion gate, and must happen before replacing bytes.
restore_keychain || fail 'Keychain cleanup failed before promotion'
rm -rf "$scratch" || fail 'private cleanup failed before promotion'
# mv of each file is atomic; both candidates have passed every gate before the
# first rename. A filesystem failure can still interrupt the pair of renames.
for arch in arm64 x86_64; do
  mv -f "$staging/hikyo_${version}_Darwin_$arch.dmg" "$dist/"
done
rmdir "$staging"
staging=
printf 'apple signing: both architectures accepted, stapled, mounted and verified\n'
[ -z "${GITHUB_OUTPUT:-}" ] || printf 'notarized=true\nrecovery_pending=false\n' >>"$GITHUB_OUTPUT"

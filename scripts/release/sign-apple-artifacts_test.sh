#!/bin/sh
# Exercise control flow with disposable mock Apple tools, never real credentials.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-apple-fixture.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir "$fixture/bin" "$fixture/scratch" "$fixture/original" "$fixture/payload"
APPLE_FIXTURE_REAL_PRINTENV=$(command -v printenv)
APPLE_FIXTURE_REAL_TAR=$(command -v tar)
export APPLE_FIXTURE_REAL_PRINTENV APPLE_FIXTURE_REAL_TAR
export APPLE_FIXTURE_LOG="$fixture/calls" APPLE_FIXTURE_STATE="$fixture/state"
export APPLE_FIXTURE_SEARCH_LIST="$fixture/search-list"
export APPLE_FIXTURE_ORIGINAL_SEARCH_LIST="$fixture/original-search-list"
export APPLE_NOTARY_EVIDENCE_DIR="$fixture/evidence"
export GITHUB_OUTPUT="$fixture/outputs"
export DEVELOPER_DIR="$fixture/Developer tools"
printf '    "%s"\n' "$fixture/login fixture.keychain-db" "$fixture/secondary.keychain-db" >"$fixture/original-search-list"
cat >"$fixture/bin/printenv" <<'EOF'
#!/bin/sh
set -eu
case "${APPLE_FIXTURE_PHASE:-}:$1" in
  sign:APPLE_NOTARY_* | notarize:APPLE_DEVELOPER_ID_*)
    printf 'forbidden credential read %s\n' "$1" >>"$APPLE_FIXTURE_LOG"; exit 1 ;;
esac
exec "$APPLE_FIXTURE_REAL_PRINTENV" "$@"
EOF
cat >"$fixture/bin/tar" <<'EOF'
#!/bin/sh
set -eu
case "${APPLE_FIXTURE_PHASE:-}:$1" in
  notarize:*c*) printf 'forbidden tar rewrite\n' >>"$APPLE_FIXTURE_LOG"; exit 1 ;;
esac
exec "$APPLE_FIXTURE_REAL_TAR" "$@"
EOF
cat >"$fixture/bin/uname" <<'EOF'
#!/bin/sh
printf 'Darwin\n'
EOF
cat >"$fixture/bin/security" <<'EOF'
#!/bin/sh
set -eu
printf 'security %s\n' "$1" >>"$APPLE_FIXTURE_LOG"
[ "${APPLE_FIXTURE_PHASE:-}" != notarize ] || { printf 'forbidden Keychain access\n' >>"$APPLE_FIXTURE_LOG"; exit 1; }
case "$1" in
  create-keychain) touch "$4" ;;
  delete-keychain) [ "$APPLE_FIXTURE_MODE" != keychain-delete-failure ] && rm "$2" ;;
  import)
    [ "$3" = -k ] || exit 1
    printf '%s\n' "$4" >"$APPLE_FIXTURE_STATE/imported-keychain"
    ;;
  list-keychains)
    if [ "$#" -eq 3 ]; then
      [ "$APPLE_FIXTURE_MODE" != search-list-read-failure ] || exit 1
      cat "$APPLE_FIXTURE_SEARCH_LIST"
    else
      [ "$2" = -d ] && [ "$3" = user ] && [ "$4" = -s ]
      shift 4
      : >"$APPLE_FIXTURE_SEARCH_LIST"
      for path do printf '    "%s"\n' "$path" >>"$APPLE_FIXTURE_SEARCH_LIST"; done
      # A failed update can still have changed the list. Cleanup must restore it.
      case "${1:-}:$APPLE_FIXTURE_MODE" in
        */signing.keychain-db:search-list-update-failure) exit 1 ;;
      esac
      if [ "$APPLE_FIXTURE_MODE" = search-list-restore-failure ]; then
        case "${1:-}" in */signing.keychain-db) ;; *) exit 1 ;; esac
      fi
    fi
    ;;
  find-identity)
    [ "$#" -eq 5 ] || exit 1
    [ "$2" = -v ] && [ "$3" = -p ] && [ "$4" = codesigning ] || exit 1
    IFS= read -r imported_keychain <"$APPLE_FIXTURE_STATE/imported-keychain"
    [ "$5" = "$imported_keychain" ] && [ -f "$5" ] || exit 1
    [ "$APPLE_FIXTURE_MODE" != identity-query-failure ] || exit 1
    case "$APPLE_FIXTURE_MODE" in
      invalid-identity) printf '     0 valid identities found\n' ;;
      wrong-imported-identity) printf '  1) %s "Developer ID Application: Other (AAAAAAAAAA)"\n     1 valid identities found\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
      *) printf '  1) %s "%s"\n     1 valid identities found\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$APPLE_SIGNING_IDENTITY" ;;
    esac
    ;;
esac
EOF
cat >"$fixture/bin/lipo" <<'EOF'
#!/bin/sh
[ "$APPLE_FIXTURE_MODE" != wrong-arch ] || { printf 'i386\n'; exit 0; }
case "$APPLE_FIXTURE_MODE:$2" in second-wrong-arch:*/x86_64/hikyo) printf 'arm64\n'; exit 0 ;; esac
basename "$(dirname "$2")"
EOF
cat >"$fixture/bin/codesign" <<'EOF'
#!/bin/sh
set -eu
printf 'codesign %s\n' "$1" >>"$APPLE_FIXTURE_LOG"
[ "$APPLE_FIXTURE_MODE" != bad-signature ] || [ "$1" != --verify ] || exit 1
sign=false
requirement=false
keychain_next=false
keychain=
for argument do
  if [ "$keychain_next" = true ]; then keychain=$argument; keychain_next=false; fi
  if [ "$requirement" = true ]; then
    case "$argument" in
      '=anchor apple generic '*"certificate leaf[subject.OU] = \"$APPLE_TEAM_ID\""*"certificate leaf[subject.CN] = \"$APPLE_SIGNING_IDENTITY\""*'certificate leaf[field.1.2.840.113635.100.6.1.13] exists') ;;
      *) printf 'invalid Developer ID identity/team requirement\n' >&2; exit 1 ;;
    esac
    case "$argument" in *"certificate leaf[subject.CN] = \"$APPLE_FIXTURE_SIGNED_IDENTITY\""*) ;; *) exit 1 ;; esac
    requirement=false
  fi
  [ "$argument" != -R ] || requirement=true
  [ "$argument" != --sign ] || sign=true
  [ "$argument" != --keychain ] || keychain_next=true
  target=$argument
done
if [ "$sign" = true ]; then
  [ "${APPLE_FIXTURE_PHASE:-}" != notarize ] || { printf 'forbidden codesign signing\n' >>"$APPLE_FIXTURE_LOG"; exit 1; }
  if ! grep -F "\"$keychain\"" "$APPLE_FIXTURE_SEARCH_LIST" >/dev/null; then
    printf 'error: The specified item could not be found in the keychain.\n' >&2
    exit 1
  fi
  while IFS= read -r original_keychain; do
    grep -Fx "$original_keychain" "$APPLE_FIXTURE_SEARCH_LIST" >/dev/null || {
      printf 'original Keychain missing during signing\n' >&2; exit 1;
    }
  done <"$APPLE_FIXTURE_ORIGINAL_SEARCH_LIST"
  printf 'Developer ID signed\n' >>"$target"
else
  grep -Fx 'Developer ID signed' "$target" >/dev/null || exit 1
  if grep -Fx 'tampered' "$target" >/dev/null; then exit 1; fi
  case "$APPLE_FIXTURE_MODE:$target" in
    bad-dmg-signature:*.dmg | second-bad-signature:*/x86_64/hikyo | second-bad-dmg-signature:*_x86_64.dmg) exit 1 ;;
  esac
fi
EOF
cat >"$fixture/bin/hdiutil" <<'EOF'
#!/bin/sh
set -eu
printf 'hdiutil %s\n' "$1" >>"$APPLE_FIXTURE_LOG"
[ "${APPLE_FIXTURE_PHASE:-}" != notarize ] || { printf 'forbidden disk image creation\n' >>"$APPLE_FIXTURE_LOG"; exit 1; }
filesystem=
format=
option=
for argument do
  case "$option" in
    -fs) filesystem=$argument ;;
    -format) format=$argument ;;
  esac
  option=$argument
  target=$argument
done
[ "$filesystem" = HFS+ ] && [ "$format" = UDZO ] || {
  printf 'explicit HFS+ filesystem and compressed UDIF format required\n' >&2
  exit 1
}
printf 'disk image\n' >"$target"
EOF
cat >"$fixture/bin/xcrun" <<'EOF'
#!/bin/sh
set -eu
printf 'xcrun %s %s\n' "$1" "$2" >>"$APPLE_FIXTURE_LOG"
[ "${APPLE_FIXTURE_PHASE:-}" != sign ] || { printf 'forbidden notarization call\n' >>"$APPLE_FIXTURE_LOG"; exit 1; }
[ "$DEVELOPER_DIR" = "$APPLE_FIXTURE_DEVELOPER_DIR" ] || exit 1
case "$1 $2" in
  'notarytool submit')
    # Every invocation, including full mode, must have both signed archives and
    # unstapled signed images ready before the first submission can fail.
    if [ ! -f "$APPLE_FIXTURE_STATE/arm64.json" ]; then
      dist=$(dirname "$3")
      for input_arch in arm64 x86_64; do
        image=${3%_Darwin_*}_Darwin_$input_arch.dmg
        [ -f "$image" ] && [ ! -L "$image" ] || exit 1
        grep -Fx 'Developer ID signed' "$image" >/dev/null || exit 1
        if grep -Fx 'stapled' "$image" >/dev/null; then exit 1; fi
        tar -xOzf "${image%.dmg}.tar.gz" hikyo | grep -Fx 'Developer ID signed' >/dev/null || exit 1
      done
      (cd "$dist" && shasum -a 256 -c checksums.txt >/dev/null) || exit 1
      printf 'both signed architectures ready before submit\n' >>"$APPLE_FIXTURE_LOG"
    fi
    case "$3" in
      *_arm64.dmg) arch=arm64; id=aaaaaaaa-1111-2222-3333-444444444444 ;;
      *_x86_64.dmg) arch=x86_64; id=bbbbbbbb-1111-2222-3333-444444444444 ;;
      *) exit 2 ;;
    esac
    status=Accepted
    case "$APPLE_FIXTURE_MODE:$arch" in
      rejected:* | second-rejected:x86_64) status=Invalid ;;
      terminal-rejected:*) status=rEjEcTeD ;;
      timeout:*) status='In Progress' ;;
    esac
    jq -n --arg filename "$(basename "$3")" --arg sha "$(shasum -a 256 "$3" | awk '{print $1}')" \
      --arg status "$status" '{filename:$filename,sha256:$sha,status:$status}' >"$APPLE_FIXTURE_STATE/$arch.json"
    case "$APPLE_FIXTURE_MODE" in
      timeout-no-id) printf 'fixture-private-key: private command failure\n' >&2; exit 1 ;;
      accepted-no-id) printf '{"status":"Accepted"}\n'; exit 0 ;;
    esac
    printf '{"status":"%s","id":"%s","privateDiagnostic":"fixture-private-key"}\n' "$status" "$id"
    [ "$APPLE_FIXTURE_MODE" != timeout ] || exit 1
    ;;
  'notarytool log')
    case "$3" in
      aaaaaaaa-1111-2222-3333-444444444444) arch=arm64 ;;
      bbbbbbbb-1111-2222-3333-444444444444) arch=x86_64 ;;
      *) exit 2 ;;
    esac
    for argument do logfile=$argument; done
    case "$APPLE_FIXTURE_MODE" in
      log-failure) printf 'fixture-private-key: private log download failure\n' >&2; printf 'incomplete provider response\n' >"$logfile"; exit 1 ;;
      malformed-log) printf 'malformed provider JSON\n' >"$logfile"; exit 0 ;;
    esac
    # Accepted logs may omit sha256 and have null issues. Apple's jobId may be
    # uppercase even though the submission response used a lowercase UUID.
    jq --arg id "$(printf '%s' "$3" | tr '[:lower:]' '[:upper:]')" \
      '{jobId:$id,status:.status,archiveFilename:.filename,issues:null}' "$APPLE_FIXTURE_STATE/$arch.json" >"$logfile"
    case "$APPLE_FIXTURE_MODE" in
      accepted-minimal-log) jq 'del(.archiveFilename) | .status = "accepted"' "$logfile" >"$logfile.new" ;;
      accepted-warnings)
        jq '.issues = [{severity:"WARNING",message:"provider warning ::error title=untrusted::do not execute"}]' "$logfile" >"$logfile.new" ;;
      terminal-rejected) jq '.issues = [{severity:"error",message:"provider rejection details"}]' "$logfile" >"$logfile.new" ;;
      rejected | second-rejected)
        jq 'if .status == "Invalid" then .issues = [{severity:"error",message:"provider rejection details"}] else . end' "$logfile" >"$logfile.new" ;;
      mismatched-job) jq '.jobId = "cccccccc-1111-2222-3333-444444444444"' "$logfile" >"$logfile.new" ;;
      mismatched-status) jq '.status = "Invalid"' "$logfile" >"$logfile.new" ;;
      mismatched-filename) jq '.archiveFilename = "other.dmg"' "$logfile" >"$logfile.new" ;;
      mismatched-hash) jq '.sha256 = ("c" * 64)' "$logfile" >"$logfile.new" ;;
      malformed-issues) jq '.issues = "provider text"' "$logfile" >"$logfile.new" ;;
      *) cp "$logfile" "$logfile.new" ;;
    esac
    mv "$logfile.new" "$logfile"
    cp "$logfile" "$APPLE_FIXTURE_STATE/$arch-returned-log.json"
    ;;
  'stapler staple')
    [ "$APPLE_FIXTURE_MODE" != staple-failure ] || exit 1
    printf 'stapled\n' >>"$3"
    ;;
  'stapler validate') [ "$APPLE_FIXTURE_MODE" != invalid-ticket ] ;;
  *) exit 2 ;;
esac
EOF
cat >"$fixture/bin/spctl" <<'EOF'
#!/bin/sh
printf 'spctl\n' >>"$APPLE_FIXTURE_LOG"
[ "${APPLE_FIXTURE_PHASE:-}" != sign ] || { printf 'forbidden Gatekeeper call\n' >>"$APPLE_FIXTURE_LOG"; exit 1; }
[ "$APPLE_FIXTURE_MODE" != gatekeeper-failure ]
EOF
chmod +x "$fixture/bin/"*
export PATH="$fixture/bin:$PATH" TMPDIR="$fixture/scratch"
export APPLE_FIXTURE_DEVELOPER_DIR="$DEVELOPER_DIR"
export APPLE_DEVELOPER_ID_P12_BASE64=Zml4dHVyZQ== APPLE_DEVELOPER_ID_P12_PASSWORD=fixture-password
export APPLE_SIGNING_IDENTITY='Developer ID Application: Fixture (AAAAAAAAAA)' APPLE_TEAM_ID=AAAAAAAAAA
export APPLE_FIXTURE_SIGNED_IDENTITY="$APPLE_SIGNING_IDENTITY"
export APPLE_NOTARY_KEY_P8=fixture-private-key APPLE_NOTARY_KEY_ID=fixture-key APPLE_NOTARY_ISSUER_ID=fixture-issuer
export GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa GITHUB_REF=refs/tags/v1.2.3
printf '{"commit":"%s","key_id":"primary-1","public_key":"primary-1.pub","sequence":1,"version":"1.2.3"}\n' "$GITHUB_SHA" >"$fixture/original/release-candidate.json"
CANDIDATE_SHA256=$(shasum -a 256 "$fixture/original/release-candidate.json" | awk '{print $1}')
export CANDIDATE_SHA256
printf 'fixture license\n' >"$fixture/payload/LICENSE"
printf 'fixture executable\n' >"$fixture/payload/hikyo"
for arch in arm64 x86_64; do
  tar -czf "$fixture/original/hikyo_1.2.3_Darwin_$arch.tar.gz" -C "$fixture/payload" LICENSE hikyo
  printf 'Linux archive %s\n' "$arch" >"$fixture/original/hikyo_1.2.3_Linux_$arch.tar.gz"
  printf 'Windows archive %s\n' "$arch" >"$fixture/original/hikyo_1.2.3_Windows_$arch.zip"
done
(cd "$fixture/original" && shasum -a 256 hikyo_*.tar.gz hikyo_*.zip >checksums.txt)
submitted_fixture_sha256=$(printf 'disk image\nDeveloper ID signed\n' | shasum -a 256 | awk '{print $1}')
assert_signed_outputs() {
  signed_version=$1
  [ "$(wc -l <"$fixture/dist/checksums.txt" | tr -d ' ')" -eq 6 ]
  (cd "$fixture/dist" && shasum -a 256 -c checksums.txt >/dev/null)
  if grep -F '.dmg' "$fixture/dist/checksums.txt"; then exit 1; fi
  for signed_arch in arm64 x86_64; do
    [ -s "$fixture/dist/hikyo_${signed_version}_Darwin_$signed_arch.dmg" ]
    tar -xOzf "$fixture/dist/hikyo_${signed_version}_Darwin_$signed_arch.tar.gz" hikyo | grep -Fx 'Developer ID signed' >/dev/null
    cmp "$fixture/original/hikyo_1.2.3_Linux_$signed_arch.tar.gz" "$fixture/dist/hikyo_${signed_version}_Linux_$signed_arch.tar.gz"
    cmp "$fixture/original/hikyo_1.2.3_Windows_$signed_arch.zip" "$fixture/dist/hikyo_${signed_version}_Windows_$signed_arch.zip"
  done
}
assert_evidence() {
  evidence_version=$1
  submissions=$(grep -c '^xcrun notarytool submit$' "$APPLE_FIXTURE_LOG" || true)
  if [ "$submissions" -gt 0 ]; then
    grep -Fx 'inputs_verified=true' "$GITHUB_OUTPUT" >/dev/null
  else
    [ ! -s "$GITHUB_OUTPUT" ] || { printf 'invalid Apple inputs authorized fallback\n' >&2; exit 1; }
  fi
  receipts=$(find "$fixture/evidence" -name '*-receipt.json' | wc -l | tr -d ' ')
  logs=$(find "$fixture/evidence" -name '*-notary-log.json' | wc -l | tr -d ' ')
  if [ "$APPLE_FIXTURE_MODE" = stale-evidence ]; then
    [ "$submissions" -eq 0 ] && [ "$receipts" -eq 1 ] && [ "$logs" -eq 0 ]
    printf '{"old":"evidence"}\n' >"$fixture/expected-stale.json"
    cmp "$fixture/expected-stale.json" "$fixture/evidence/x86_64-receipt.json"
    return
  fi
  [ "$submissions" -eq "$receipts" ] || { printf 'apple fixture: missing receipt for %s\n' "$APPLE_FIXTURE_MODE" >&2; exit 1; }
  case "$APPLE_FIXTURE_MODE" in
    accepted | accepted-warnings | accepted-minimal-log | second-rejected) [ "$submissions" -eq 2 ] ;;
    rejected | terminal-rejected | timeout | timeout-no-id | accepted-no-id | log-failure | malformed-log | mismatched-job | mismatched-status | mismatched-filename | mismatched-hash | malformed-issues | staple-failure | invalid-ticket | gatekeeper-failure) [ "$submissions" -eq 1 ] ;;
    *) [ "$submissions" -eq 0 ] ;;
  esac
  if [ "$APPLE_FIXTURE_MODE" = terminal-rejected ]; then
    if ! jq -e '.status == "Rejected" and .diagnostics.availability == "available"' \
      "$fixture/evidence/arm64-receipt.json" >/dev/null || [ "$logs" -ne 1 ]; then
      printf 'apple fixture: terminal Rejected receipt or detailed log missing\n' >&2
      jq '{status,diagnostics}' "$fixture/evidence/arm64-receipt.json" >&2
      exit 1
    fi
    if grep -F 'xcrun stapler' "$APPLE_FIXTURE_LOG" >/dev/null; then
      printf 'apple fixture: rejected submission reached stapling\n' >&2; exit 1
    fi
    assert_signed_outputs "$evidence_version"
  fi
  expected_logs=0
  for evidence_arch in arm64 x86_64; do
    receipt=$fixture/evidence/$evidence_arch-receipt.json
    [ -f "$receipt" ] || continue
    availability=available
    expected_status=Accepted
    expected_id=aaaaaaaa-1111-2222-3333-444444444444
    [ "$evidence_arch" != x86_64 ] || expected_id=bbbbbbbb-1111-2222-3333-444444444444
    case "$APPLE_FIXTURE_MODE:$evidence_arch" in
      rejected:* | second-rejected:x86_64) expected_status=Invalid ;;
      terminal-rejected:*) expected_status=Rejected ;;
      timeout:*) expected_status='In Progress'; availability=unavailable ;;
      timeout-no-id:*) expected_status=Unknown; availability=unavailable; expected_id= ;;
      accepted-no-id:*) availability=unavailable; expected_id= ;;
      log-failure:*) availability=unavailable ;;
      malformed-log:* | mismatched-job:* | mismatched-status:* | mismatched-filename:* | mismatched-hash:* | malformed-issues:*) availability=invalid ;;
    esac
    warnings=0
    [ "$APPLE_FIXTURE_MODE" != accepted-warnings ] || warnings=1
    jq -e --arg version "$evidence_version" --arg commit "$GITHUB_SHA" --arg arch "$evidence_arch" \
      --arg filename "hikyo_${evidence_version}_Darwin_$evidence_arch.dmg" \
      --arg sha "$submitted_fixture_sha256" --arg id "$expected_id" --arg status "$expected_status" \
      --arg availability "$availability" --argjson warnings "$warnings" '
      .schema == "hikyo.dev/apple-notarization-receipt/v1" and .version == $version and
      .source_commit == $commit and .architecture == $arch and .archive_filename == $filename and
      .submitted_sha256 == $sha and .submission_id == (if $id == "" then null else $id end) and
      .status == $status and .diagnostics.availability == $availability and
      (if $availability == "available" then
        .diagnostics.log_filename == ($arch + "-notary-log.json") and .diagnostics.warning_count == $warnings
       else .diagnostics.log_filename == null and .diagnostics.warning_count == null end)
    ' "$receipt" >/dev/null
    if [ "$availability" = available ]; then
      expected_logs=$((expected_logs + 1))
      cmp "$APPLE_FIXTURE_STATE/$evidence_arch-returned-log.json" "$fixture/evidence/$evidence_arch-notary-log.json"
    fi
    case "$APPLE_FIXTURE_MODE:$evidence_arch" in
      accepted:* | accepted-warnings:* | accepted-minimal-log:* | invalid-ticket:* | gatekeeper-failure:* | second-rejected:arm64)
        stapled_sha=$(shasum -a 256 "$fixture/dist/hikyo_${evidence_version}_Darwin_$evidence_arch.dmg" | awk '{print $1}')
        [ "$stapled_sha" != "$submitted_fixture_sha256" ] || { printf 'apple fixture: pre-staple digest not distinguished\n' >&2; exit 1; }
        ;;
    esac
  done
  [ "$logs" -eq "$expected_logs" ]
  [ "$(find "$fixture/evidence" -type f | wc -l | tr -d ' ')" -eq $((receipts + logs)) ]
  for json in "$fixture/evidence/"*.json; do
    [ -f "$json" ] || continue
    jq -e 'type == "object"' "$json" >/dev/null
  done
  case "$APPLE_FIXTURE_MODE" in
    accepted-warnings)
      [ "$(grep -c '^::warning title=Apple notarization warnings::1 warning(s)' "$fixture/out")" -eq 2 ]
      ;;
    *) if grep -F '::warning' "$fixture/out" "$fixture/err" >/dev/null; then exit 1; fi ;;
  esac
  if grep -E 'fixture-password|fixture-private-key|Zml4dHVyZQ==' \
    "$fixture/out" "$fixture/err" "$fixture/evidence/"*.json 2>/dev/null; then
    printf 'apple fixture: leaked private command output or credentials\n' >&2; exit 1
  fi
  if grep -E 'provider warning|provider rejection|malformed provider|incomplete provider' "$fixture/out" "$fixture/err"; then
    printf 'apple fixture: printed provider-controlled diagnostics\n' >&2; exit 1
  fi
}

for APPLE_FIXTURE_MODE in accepted accepted-warnings accepted-minimal-log search-list-read-failure search-list-update-failure search-list-restore-failure keychain-delete-failure invalid-identity wrong-imported-identity identity-query-failure missing-evidence-dir evidence-contains-scratch stale-evidence missing-secret wrong-team wrong-candidate wrong-tag tampered-input unexpected-entry symlink-entry wrong-arch bad-signature rejected terminal-rejected timeout staple-failure invalid-ticket gatekeeper-failure timeout-no-id accepted-no-id log-failure malformed-log mismatched-job mismatched-status mismatched-filename mismatched-hash malformed-issues second-rejected; do
  export APPLE_FIXTURE_MODE
  : >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
  cp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
  rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state"
  mkdir "$fixture/evidence" "$fixture/state"
  cp -R "$fixture/original" "$fixture/dist"
  case "$APPLE_FIXTURE_MODE" in
    stale-evidence) printf '{"old":"evidence"}\n' >"$fixture/evidence/x86_64-receipt.json" ;;
    tampered-input) printf 'tampered\n' >>"$fixture/dist/hikyo_1.2.3_Darwin_arm64.tar.gz" ;;
    unexpected-entry | symlink-entry)
      mkdir "$fixture/malformed"
      cp "$fixture/payload/LICENSE" "$fixture/malformed/"
      if [ "$APPLE_FIXTURE_MODE" = symlink-entry ]; then
        ln -s LICENSE "$fixture/malformed/hikyo"
      else
        cp "$fixture/payload/hikyo" "$fixture/malformed/"
        touch "$fixture/malformed/extra"
      fi
      set -- LICENSE hikyo
      [ "$APPLE_FIXTURE_MODE" != unexpected-entry ] || set -- "$@" extra
      tar -czf "$fixture/dist/hikyo_1.2.3_Darwin_arm64.tar.gz" -C "$fixture/malformed" "$@"
      (cd "$fixture/dist" && shasum -a 256 hikyo_*.tar.gz hikyo_*.zip >checksums.txt)
      cp "$fixture/dist/checksums.txt" "$fixture/expected-input-checksums"
      rm -rf "$fixture/malformed"
      ;;
  esac
  (
    set -- env
    case "$APPLE_FIXTURE_MODE" in
      missing-secret) unset APPLE_NOTARY_KEY_P8 ;;
      missing-evidence-dir) unset APPLE_NOTARY_EVIDENCE_DIR ;;
      evidence-contains-scratch) set -- "$@" APPLE_NOTARY_EVIDENCE_DIR="$fixture/scratch" ;;
      wrong-team) export APPLE_TEAM_ID=BBBBBBBBBB ;;
      wrong-candidate) set -- "$@" CANDIDATE_SHA256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
      wrong-tag) set -- "$@" GITHUB_REF=refs/tags/v2.0.0 ;;
    esac
    "$@" "$script_dir/sign-apple-artifacts.sh" "$fixture/dist"
  ) >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  if [ "$APPLE_FIXTURE_MODE" = accepted ] || [ "$APPLE_FIXTURE_MODE" = accepted-warnings ] || [ "$APPLE_FIXTURE_MODE" = accepted-minimal-log ]; then
    [ "$result" -eq 0 ] || { cat "$fixture/err"; printf 'apple fixture: unexpected result %s for %s\n' "$result" "$APPLE_FIXTURE_MODE" >&2; exit 1; }
    assert_signed_outputs 1.2.3
    [ "$(grep -c '^spctl$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
  else
    [ "$result" -ne 0 ] || { printf 'apple fixture: accepted %s\n' "$APPLE_FIXTURE_MODE" >&2; exit 1; }
    if grep -Fx 'both signed architectures ready before submit' "$APPLE_FIXTURE_LOG" >/dev/null; then
      assert_signed_outputs 1.2.3
    else
      case "$APPLE_FIXTURE_MODE" in
        unexpected-entry | symlink-entry) cmp "$fixture/expected-input-checksums" "$fixture/dist/checksums.txt" ;;
        *) cmp "$fixture/original/checksums.txt" "$fixture/dist/checksums.txt" ;;
      esac
      for arch in arm64 x86_64; do [ ! -e "$fixture/dist/hikyo_1.2.3_Darwin_$arch.dmg" ]; done
    fi
  fi
  case "$APPLE_FIXTURE_MODE" in
    missing-evidence-dir | evidence-contains-scratch | stale-evidence | missing-secret | wrong-team | wrong-candidate | wrong-tag | tampered-input) [ ! -s "$APPLE_FIXTURE_LOG" ] ;;
    search-list-read-failure) ;;
    *) grep -Fx 'security delete-keychain' "$APPLE_FIXTURE_LOG" >/dev/null ;;
  esac
  cmp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
  case "$APPLE_FIXTURE_MODE" in
    invalid-identity | wrong-imported-identity | identity-query-failure | search-list-read-failure | search-list-update-failure)
      if grep -F 'codesign ' "$APPLE_FIXTURE_LOG" >/dev/null; then
        printf 'apple fixture: invalid keychain setup reached signing\n' >&2; exit 1
      fi
      ;;
  esac
  assert_evidence 1.2.3
  [ -z "$(find "$fixture/scratch" -mindepth 1 -print)" ] || { printf 'apple fixture: leaked credentials\n' >&2; exit 1; }
  if grep -E 'fixture-password|fixture-private-key|Zml4dHVyZQ==' "$fixture/out" "$fixture/err"; then
    printf 'apple fixture: printed credentials\n' >&2; exit 1
  fi
done

run_sign_only() (
  unset APPLE_NOTARY_KEY_P8 APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER_ID APPLE_NOTARY_TIMEOUT APPLE_NOTARY_EVIDENCE_DIR
  case "$APPLE_FIXTURE_MODE" in
    missing-signing-secret) unset APPLE_DEVELOPER_ID_P12_BASE64 ;;
    missing-signing-password) unset APPLE_DEVELOPER_ID_P12_PASSWORD ;;
  esac
  APPLE_FIXTURE_PHASE=sign "$script_dir/sign-apple-artifacts.sh" --sign-only "$fixture/dist"
)
run_notarize_only() (
  unset APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD
  set -- env APPLE_FIXTURE_PHASE=notarize
  case "$APPLE_FIXTURE_MODE" in
    missing-secret) unset APPLE_NOTARY_KEY_P8 ;;
    missing-evidence-dir) unset APPLE_NOTARY_EVIDENCE_DIR ;;
    evidence-contains-scratch) set -- "$@" APPLE_NOTARY_EVIDENCE_DIR="$fixture/scratch" ;;
    wrong-candidate) set -- "$@" CANDIDATE_SHA256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
    wrong-tag) set -- "$@" GITHUB_REF=refs/tags/v2.0.0 ;;
    wrong-signing-identity) set -- "$@" APPLE_SIGNING_IDENTITY='Developer ID Application: Other (AAAAAAAAAA)' ;;
    nightly-wrong-ref) set -- "$@" GITHUB_REF=refs/heads/feature ;;
    nightly-wrong-commit) set -- "$@" APPLE_RELEASE_COMMIT=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
    nightly-wrong-version) set -- "$@" APPLE_RELEASE_VERSION=1.2.3 ;;
    nightly-wrong-metadata) printf '{}\n' >"$fixture/dist/metadata.json" ;;
  esac
  "$@" "$script_dir/sign-apple-artifacts.sh" --notarize-only "$fixture/dist"
)
assert_split_cleanup() {
  cmp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
  [ -z "$(find "$fixture/scratch" -mindepth 1 -print)" ] || { printf 'apple fixture: split phase leaked scratch\n' >&2; exit 1; }
  if grep -E 'forbidden' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  if grep -E 'fixture-password|fixture-private-key|Zml4dHVyZQ==' "$fixture/out" "$fixture/err"; then
    printf 'apple fixture: split phase printed credentials\n' >&2; exit 1
  fi
}
assert_sign_only_calls() {
  signing_version=${1:-1.2.3}
  [ ! -s "$GITHUB_OUTPUT" ] || { printf 'signing-only phase authorized a provider failure fallback\n' >&2; exit 1; }
  [ "$(grep -c '^hdiutil create$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
  [ "$(grep -Ec '^codesign --(force|sign)$' "$APPLE_FIXTURE_LOG")" -eq 4 ]
  if grep -E '^xcrun |^spctl$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  for signed_arch in arm64 x86_64; do
    if grep -Fx 'stapled' "$fixture/dist/hikyo_${signing_version}_Darwin_$signed_arch.dmg"; then exit 1; fi
  done
}

# Sign without any notary variables, even with a provider configured to reject.
# Cleanup failures must leave the original artifacts and checksums untouched.
for APPLE_FIXTURE_MODE in rejected search-list-restore-failure keychain-delete-failure missing-signing-secret missing-signing-password; do
  export APPLE_FIXTURE_MODE
  : >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
  cp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
  rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state"
  mkdir "$fixture/state"
  cp -R "$fixture/original" "$fixture/dist"
  run_sign_only >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  if [ "$APPLE_FIXTURE_MODE" = rejected ]; then
    [ "$result" -eq 0 ] || { cat "$fixture/err"; exit 1; }
    assert_signed_outputs 1.2.3
    assert_sign_only_calls
    cp -R "$fixture/dist" "$fixture/signed"
  else
    [ "$result" -ne 0 ] || exit 1
    for file in "$fixture/original/"*; do cmp "$file" "$fixture/dist/$(basename "$file")"; done
    [ -z "$(find "$fixture/dist" -name '*.dmg' -print)" ]
    if grep -E '^xcrun |^spctl$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  fi
  [ ! -e "$fixture/evidence" ]
  assert_split_cleanup
done

# A supplied stale evidence directory also has no meaning to the signing phase.
export APPLE_FIXTURE_MODE=rejected
: >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
rm -rf "$fixture/dist"
cp -R "$fixture/original" "$fixture/dist"
mkdir "$fixture/evidence"
printf 'existing evidence\n' >"$fixture/evidence/sentinel"
(
  unset APPLE_NOTARY_KEY_P8 APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER_ID
  "$script_dir/sign-apple-artifacts.sh" --sign-only "$fixture/dist"
) >"$fixture/out" 2>"$fixture/err"
assert_signed_outputs 1.2.3
assert_sign_only_calls
printf 'existing evidence\n' >"$fixture/expected-sentinel"
cmp "$fixture/expected-sentinel" "$fixture/evidence/sentinel"
[ "$(find "$fixture/evidence" -type f | wc -l | tr -d ' ')" -eq 1 ]
assert_split_cleanup

# Retain the signed job artifact across success, provider failures and invalid
# handoffs. Every notarize-only invocation has no P12 and guards signing tools.
for APPLE_FIXTURE_MODE in accepted accepted-warnings rejected terminal-rejected second-rejected timeout timeout-no-id accepted-no-id mismatched-hash stale-evidence missing-evidence-dir evidence-contains-scratch missing-secret wrong-candidate wrong-tag tampered-input unexpected-entry symlink-entry unsigned-input wrong-arch second-wrong-arch bad-signature bad-dmg-signature second-bad-signature second-bad-dmg-signature tampered-dmg missing-dmg symlink-dmg wrong-signing-identity noncanonical-checksums; do
  export APPLE_FIXTURE_MODE
  : >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
  rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state" "$fixture/before-notarize"
  mkdir "$fixture/evidence" "$fixture/state"
  cp -R "$fixture/signed" "$fixture/dist"
  case "$APPLE_FIXTURE_MODE" in
    stale-evidence) printf '{"old":"evidence"}\n' >"$fixture/evidence/x86_64-receipt.json" ;;
    tampered-input) printf 'tampered\n' >>"$fixture/dist/hikyo_1.2.3_Darwin_x86_64.tar.gz" ;;
    tampered-dmg) printf 'tampered\n' >>"$fixture/dist/hikyo_1.2.3_Darwin_x86_64.dmg" ;;
    missing-dmg) rm "$fixture/dist/hikyo_1.2.3_Darwin_x86_64.dmg" ;;
    symlink-dmg)
      rm "$fixture/dist/hikyo_1.2.3_Darwin_x86_64.dmg"
      ln -s hikyo_1.2.3_Darwin_arm64.dmg "$fixture/dist/hikyo_1.2.3_Darwin_x86_64.dmg"
      ;;
    noncanonical-checksums) printf '%s  unexpected.tar.gz\n' "$submitted_fixture_sha256" >>"$fixture/dist/checksums.txt" ;;
    unexpected-entry | symlink-entry | unsigned-input)
      mkdir "$fixture/malformed"
      tar -xzf "$fixture/dist/hikyo_1.2.3_Darwin_x86_64.tar.gz" -C "$fixture/malformed"
      set -- LICENSE hikyo
      case "$APPLE_FIXTURE_MODE" in
        unexpected-entry) touch "$fixture/malformed/extra"; set -- "$@" extra ;;
        symlink-entry) rm "$fixture/malformed/hikyo"; ln -s LICENSE "$fixture/malformed/hikyo" ;;
        unsigned-input) cp "$fixture/payload/hikyo" "$fixture/malformed/hikyo" ;;
      esac
      tar -czf "$fixture/dist/hikyo_1.2.3_Darwin_x86_64.tar.gz" -C "$fixture/malformed" "$@"
      rm -rf "$fixture/malformed"
      (cd "$fixture/dist" && shasum -a 256 hikyo_*.tar.gz hikyo_*.zip >checksums.txt)
      ;;
  esac
  cp -R "$fixture/dist" "$fixture/before-notarize"
  run_notarize_only >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  case "$APPLE_FIXTURE_MODE" in
    accepted | accepted-warnings)
      [ "$result" -eq 0 ] || { cat "$fixture/err"; exit 1; }
      [ "$(grep -c '^xcrun stapler staple$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
      [ "$(grep -c '^xcrun stapler validate$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
      [ "$(grep -c '^spctl$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
      assert_signed_outputs 1.2.3
      ;;
    *) [ "$result" -ne 0 ] || { printf 'apple fixture: notarize-only accepted %s\n' "$APPLE_FIXTURE_MODE" >&2; exit 1; } ;;
  esac
  # Even partially stapled failures cannot alter any of the six signed archives
  # or their canonical checksums. Also verify the unchanged inputs on refusals.
  for file in "$fixture/before-notarize/"*.tar.gz "$fixture/before-notarize/"*.zip "$fixture/before-notarize/checksums.txt"; do
    cmp "$file" "$fixture/dist/$(basename "$file")"
  done
  if ! grep -F 'xcrun stapler staple' "$APPLE_FIXTURE_LOG" >/dev/null; then
    for arch in arm64 x86_64; do
      [ ! -f "$fixture/before-notarize/hikyo_1.2.3_Darwin_$arch.dmg" ] || \
        cmp "$fixture/before-notarize/hikyo_1.2.3_Darwin_$arch.dmg" "$fixture/dist/hikyo_1.2.3_Darwin_$arch.dmg"
    done
  fi
  if grep -E '^security |^hdiutil |^codesign --(force|sign)$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  assert_evidence 1.2.3
  assert_split_cleanup
done

export APPLE_RELEASE_CHANNEL=nightly APPLE_RELEASE_VERSION=1.2.3-nightly.20261021.1.gaaaaaaaa
export APPLE_RELEASE_COMMIT=$GITHUB_SHA GITHUB_REF=refs/heads/main
prepare_nightly_dist() {
  cp -R "$fixture/original" "$fixture/dist"
  rm "$fixture/dist/release-candidate.json"
  for file in "$fixture/dist/"hikyo_1.2.3_*; do
    mv "$file" "$fixture/dist/hikyo_${APPLE_RELEASE_VERSION}_${file##*1.2.3_}"
  done
  (cd "$fixture/dist" && shasum -a 256 hikyo_*.tar.gz hikyo_*.zip >checksums.txt)
  jq -n --arg version "$APPLE_RELEASE_VERSION" --arg commit "$GITHUB_SHA" \
    '{version:$version,commit:$commit}' >"$fixture/dist/metadata.json"
}
for APPLE_FIXTURE_MODE in accepted nightly-wrong-ref nightly-wrong-commit nightly-wrong-version nightly-wrong-metadata unknown-channel; do
  export APPLE_FIXTURE_MODE
  : >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
  cp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
  rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state"
  mkdir "$fixture/evidence" "$fixture/state"
  prepare_nightly_dist
  (
    unset CANDIDATE_SHA256
    set -- env
    case "$APPLE_FIXTURE_MODE" in
      nightly-wrong-ref) set -- "$@" GITHUB_REF=refs/heads/feature ;;
      nightly-wrong-commit) export APPLE_RELEASE_COMMIT=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
      nightly-wrong-version) set -- "$@" APPLE_RELEASE_VERSION=1.2.3 ;;
      nightly-wrong-metadata) printf '{}\n' >"$fixture/dist/metadata.json" ;;
      unknown-channel) export APPLE_RELEASE_CHANNEL=preview ;;
    esac
    "$@" "$script_dir/sign-apple-artifacts.sh" "$fixture/dist"
  ) >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  if [ "$APPLE_FIXTURE_MODE" = accepted ]; then
    [ "$result" -eq 0 ] || { cat "$fixture/err"; exit 1; }
    (cd "$fixture/dist" && shasum -a 256 -c checksums.txt >/dev/null)
    for arch in arm64 x86_64; do
      [ -s "$fixture/dist/hikyo_${APPLE_RELEASE_VERSION}_Darwin_$arch.dmg" ]
      tar -xOzf "$fixture/dist/hikyo_${APPLE_RELEASE_VERSION}_Darwin_$arch.tar.gz" hikyo | grep -F 'Developer ID signed' >/dev/null
    done
    grep -Fx 'security delete-keychain' "$APPLE_FIXTURE_LOG" >/dev/null
  else
    [ "$result" -ne 0 ] && [ ! -s "$APPLE_FIXTURE_LOG" ] || { printf 'apple fixture: accepted %s\n' "$APPLE_FIXTURE_MODE" >&2; exit 1; }
  fi
  cmp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
  assert_evidence "$APPLE_RELEASE_VERSION"
  [ -z "$(find "$fixture/scratch" -mindepth 1 -print)" ]
  if grep -E 'fixture-password|fixture-private-key|Zml4dHVyZQ==' "$fixture/out" "$fixture/err"; then exit 1; fi
done

# Nightly uses the same isolated phases, with source metadata rather than a tag
# candidate. A failed notarization leaves the signed non-DMG release intact.
export APPLE_FIXTURE_MODE=rejected
: >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state"
mkdir "$fixture/state"
prepare_nightly_dist
run_sign_only >"$fixture/out" 2>"$fixture/err"
assert_signed_outputs "$APPLE_RELEASE_VERSION"
assert_sign_only_calls "$APPLE_RELEASE_VERSION"
[ ! -e "$fixture/evidence" ]
assert_split_cleanup
cp -R "$fixture/dist" "$fixture/nightly-signed"
for APPLE_FIXTURE_MODE in rejected second-rejected accepted nightly-wrong-ref nightly-wrong-commit nightly-wrong-version nightly-wrong-metadata; do
  export APPLE_FIXTURE_MODE
  : >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
  rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state"
  mkdir "$fixture/evidence" "$fixture/state"
  cp -R "$fixture/nightly-signed" "$fixture/dist"
  run_notarize_only >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  if [ "$APPLE_FIXTURE_MODE" = accepted ]; then
    [ "$result" -eq 0 ] || { cat "$fixture/err"; exit 1; }
    [ "$(grep -c '^xcrun stapler validate$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
    [ "$(grep -c '^spctl$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
  else
    [ "$result" -ne 0 ] || { printf 'apple fixture: nightly notarize-only accepted %s\n' "$APPLE_FIXTURE_MODE" >&2; exit 1; }
  fi
  assert_signed_outputs "$APPLE_RELEASE_VERSION"
  for file in "$fixture/nightly-signed/"*.tar.gz "$fixture/nightly-signed/"*.zip "$fixture/nightly-signed/checksums.txt"; do
    cmp "$file" "$fixture/dist/$(basename "$file")"
  done
  if grep -E '^security |^hdiutil |^codesign --(force|sign)$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  assert_evidence "$APPLE_RELEASE_VERSION"
  assert_split_cleanup
done
printf 'apple fixtures: stable/nightly full and split phases, credential/tool isolation, signed handoff validation, both architectures before submission, keychain cleanup, diagnostic matrix, receipts, warnings and unchanged signed archives/Linux/Windows verified\n'

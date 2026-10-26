#!/bin/sh
# Exercise control flow with disposable mock Apple tools, never real credentials.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-apple-fixture.XXXXXX")
fixture=$(CDPATH='' cd -P -- "$fixture" && pwd)
cleanup_fixture() {
  fixture_status=$?
  trap - EXIT HUP INT TERM
  if [ "$fixture_status" -ne 0 ]; then
    printf 'apple fixture failed: %s\n' "${APPLE_FIXTURE_MODE:-setup}" >&2
    [ ! -f "$fixture/err" ] || cat "$fixture/err" >&2
  fi
  rm -rf "$fixture"
  exit "$fixture_status"
}
trap cleanup_fixture EXIT
trap 'exit 1' HUP INT TERM
mkdir "$fixture/bin" "$fixture/scratch" "$fixture/original" "$fixture/payload"
APPLE_FIXTURE_REAL_PRINTENV=$(command -v printenv)
APPLE_FIXTURE_REAL_TAR=$(command -v tar)
APPLE_FIXTURE_REAL_RM=$(command -v rm)
export APPLE_FIXTURE_REAL_PRINTENV APPLE_FIXTURE_REAL_TAR APPLE_FIXTURE_REAL_RM
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
  sign:APPLE_NOTARY_* | notarize:APPLE_DEVELOPER_ID_* | prepare:APPLE_DEVELOPER_ID_* | prepare:APPLE_NOTARY_KEY*)
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
if [ "$1" = -czf ]; then
  output=$2
  shift 2
  "$APPLE_FIXTURE_REAL_TAR" -cf "$output.tar" "$@"
  gzip -n -c "$output.tar" >"$output"
  rm "$output.tar"
  exit 0
fi
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
case "$2" in
  */mount-arm64/hikyo)
    if [ "$APPLE_FIXTURE_MODE" = mounted-wrong-arch ]; then printf 'x86_64\n'; else printf 'arm64\n'; fi ;;
  */mount-x86_64/hikyo) printf 'x86_64\n' ;;
  *) basename "$(dirname "$2")" ;;
esac
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
  if [ "$APPLE_FIXTURE_MODE" = accepted-dynamic-signature ]; then
    case "$target" in *.dmg) ;; *) openssl rand -hex 16 >>"$target" ;; esac
  fi
  touch -t 202001010000 "$target"
else
  grep -Fx 'Developer ID signed' "$target" >/dev/null || exit 1
  if grep -Fx 'tampered' "$target" >/dev/null; then exit 1; fi
  case "$APPLE_FIXTURE_MODE:$target" in
    bad-dmg-signature:*.dmg | second-bad-signature:*/x86_64/hikyo | second-bad-dmg-signature:*_x86_64.dmg) exit 1 ;;
  esac
  if [ "$APPLE_FIXTURE_MODE" = post-staple-signature-failure ] && grep -Fx stapled "$target" >/dev/null; then exit 1; fi
fi
EOF
cat >"$fixture/bin/hdiutil" <<'EOF'
#!/bin/sh
set -eu
printf 'hdiutil %s\n' "$1" >>"$APPLE_FIXTURE_LOG"
case "$1" in
  attach)
    [ "$APPLE_FIXTURE_MODE" != mount-failure ] || exit 1
    [ "$2" = -readonly ] && [ "$3" = -nobrowse ] && [ "$4" = -mountpoint ] || exit 1
    case "$6" in *_arm64.dmg) arch=arm64 ;; *_x86_64.dmg) arch=x86_64 ;; *) exit 1 ;; esac
    for archive in "$APPLE_FIXTURE_DIST/"*"_Darwin_$arch.tar.gz"; do
      tar -xzf "$archive" -C "$5"
    done
    case "$APPLE_FIXTURE_MODE" in
      mounted-license-mismatch) printf 'wrong license\n' >"$5/LICENSE" ;;
      mounted-binary-mismatch) printf 'other binary\n' >>"$5/hikyo" ;;
      mounted-symlink) rm "$5/LICENSE"; ln -s hikyo "$5/LICENSE" ;;
    esac
    touch "$APPLE_FIXTURE_STATE/mounted-$arch"
    exit 0 ;;
  detach)
    [ "$APPLE_FIXTURE_MODE" != persistent-detach-failure ] || exit 1
    if [ "$APPLE_FIXTURE_MODE" = detach-failure ] && [ ! -f "$APPLE_FIXTURE_STATE/detach-failed" ]; then
      touch "$APPLE_FIXTURE_STATE/detach-failed"
      exit 1
    fi
    case "$2" in */mount-arm64) arch=arm64 ;; */mount-x86_64) arch=x86_64 ;; *) exit 1 ;; esac
    rm -f "$APPLE_FIXTURE_STATE/mounted-$arch"
    exit 0 ;;
esac
[ "${APPLE_FIXTURE_PHASE:-}" != notarize ] || { printf 'forbidden disk image creation\n' >>"$APPLE_FIXTURE_LOG"; exit 1; }
filesystem=
format=
source=
spotlight=true
option=
for argument do
  case "$option" in
    -fs) filesystem=$argument ;;
    -format) format=$argument ;;
    -srcfolder) source=$argument ;;
  esac
  [ "$argument" != -nospotlight ] || spotlight=false
  option=$argument
  target=$argument
done
[ "$filesystem" = HFS+ ] && [ "$format" = UDZO ] || {
  printf 'explicit HFS+ filesystem and compressed UDIF format required\n' >&2
  exit 1
}
image_arch=$(basename "$source")
printf 'image options %s spotlight=%s\n' "$image_arch" "$spotlight" >>"$APPLE_FIXTURE_LOG"
attempt_file=$APPLE_FIXTURE_STATE/image-$image_arch
attempt=0
[ ! -f "$attempt_file" ] || attempt=$(cat "$attempt_file")
attempt=$((attempt + 1))
printf '%s\n' "$attempt" >"$attempt_file"
printf 'image attempt %s %s\n' "$image_arch" "$attempt" >>"$APPLE_FIXTURE_LOG"
# hdiutil refuses to overwrite a failed attempt's partial output.
[ ! -e "$target" ] || { printf 'image output already exists\n' >&2; exit 17; }
case "$APPLE_FIXTURE_MODE:$image_arch:$attempt" in
  image-busy-once:*:1 | image-busy-always:arm64:* | image-second-busy-always:x86_64:*)
    printf 'partial disk image\n' >"$target"
    printf 'hdiutil: create failed - Resource busy\n' >&2
    exit 1
    ;;
  image-error:*) printf 'hdiutil: create failed - Permission denied\n' >&2; exit 13 ;;
  image-other-busy:*) printf 'hdiutil: create failed - Permission denied (Resource busy)\n' >&2; exit 13 ;;
  image-prefixed-busy:*) printf 'warning: hdiutil: create failed - Resource busy\n' >&2; exit 13 ;;
  image-mixed-error:*)
    printf 'hdiutil: create failed - Resource busy\nhdiutil: create failed - Permission denied\n' >&2
    exit 13
    ;;
esac
printf 'disk image\n' >"$target"
EOF
cat >"$fixture/bin/rm" <<'EOF'
#!/bin/sh
set -eu
case "${APPLE_FIXTURE_MODE:-}:$*" in
  private-cleanup-failure:*hikyo-apple.*)
    if [ ! -f "$APPLE_FIXTURE_STATE/cleanup-failed" ]; then
      touch "$APPLE_FIXTURE_STATE/cleanup-failed"
      exit 1
    fi ;;
esac
exec "$APPLE_FIXTURE_REAL_RM" "$@"
EOF
cat >"$fixture/bin/sleep" <<'EOF'
#!/bin/sh
set -eu
[ "$#" -eq 1 ] && [ "$1" = 2 ] || exit 1
printf 'sleep %s\n' "$1" >>"$APPLE_FIXTURE_LOG"
EOF
cat >"$fixture/bin/xcrun" <<'EOF'
#!/bin/sh
set -eu
printf 'xcrun %s %s\n' "$1" "$2" >>"$APPLE_FIXTURE_LOG"
[ "${APPLE_FIXTURE_PHASE:-}" != sign ] || { printf 'forbidden notarization call\n' >>"$APPLE_FIXTURE_LOG"; exit 1; }
[ "$DEVELOPER_DIR" = "$APPLE_FIXTURE_DEVELOPER_DIR" ] || exit 1
case "$1 $2" in
  'notarytool submit')
    for argument do [ "$argument" != --wait ] || exit 1; done
    [ -f "$APPLE_NOTARY_EVIDENCE_DIR/arm64-receipt.json" ] && [ -f "$APPLE_NOTARY_EVIDENCE_DIR/x86_64-receipt.json" ] || exit 1
    for receipt in "$APPLE_NOTARY_EVIDENCE_DIR/"*-receipt.json; do
      jq -e '.schema == "hikyo.dev/apple-notarization-receipt/v2"' "$receipt" >/dev/null || exit 1
    done
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
      timeout:* | wait-interrupted:*) status='In Progress' ;;
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
  'notarytool info' | 'notarytool wait')
    case "$3" in
      aaaaaaaa-1111-2222-3333-444444444444) arch=arm64 ;;
      bbbbbbbb-1111-2222-3333-444444444444) arch=x86_64 ;;
      *) exit 2 ;;
    esac
    if [ "$2" = wait ]; then
      case " $* " in *' --timeout '*) ;; *) exit 1 ;; esac
      if [ "$APPLE_FIXTURE_MODE" = wait-interrupted ]; then kill -TERM "$PPID"; exit 1; fi
    fi
    [ "$APPLE_FIXTURE_MODE" != info-failure ] || exit 1
    if [ "$APPLE_FIXTURE_MODE" = malformed-info ]; then printf '{bad\n'; exit 0; fi
    if [ "$APPLE_FIXTURE_MODE" = retry-accepted ]; then
      jq '.status = "Accepted"' "$APPLE_FIXTURE_STATE/$arch.json" >"$APPLE_FIXTURE_STATE/$arch.new"
      mv "$APPLE_FIXTURE_STATE/$arch.new" "$APPLE_FIXTURE_STATE/$arch.json"
    fi
    jq --arg id "$3" '{id:$id,status:.status}' "$APPLE_FIXTURE_STATE/$arch.json"
    ;;
  'notarytool history')
    case "$APPLE_FIXTURE_MODE" in
      history-empty | timeout-no-id | accepted-no-id) printf '{"history":[]}\n' ;;
      history-malformed) printf '{"history":[{"name":"bad"}]}\n' ;;
      history-ambiguous)
        jq -n --arg name "$(jq -r '.filename' "$APPLE_FIXTURE_STATE/arm64.json")" \
          '{history:[{name:$name,id:"aaaaaaaa-1111-2222-3333-444444444444",status:"Accepted"}, {name:$name,id:"aaaaaaaa-1111-2222-3333-444444444444",status:"Accepted"}]}' ;;
      history-both)
        jq -n --arg arm "$(jq -r '.filename' "$APPLE_FIXTURE_STATE/arm64.json")" \
          --arg intel "$(jq -r '.filename' "$APPLE_FIXTURE_STATE/x86_64.json")" \
          '{history:[{name:$arm,id:"aaaaaaaa-1111-2222-3333-444444444444",status:"Accepted"},
                     {name:$intel,id:"bbbbbbbb-1111-2222-3333-444444444444",status:"Accepted"}]}' ;;
      *)
        jq -n --arg name "$(jq -r '.filename' "$APPLE_FIXTURE_STATE/arm64.json")" \
          '{history:[{name:$name,id:"aaaaaaaa-1111-2222-3333-444444444444",status:"Accepted"}]}' ;;
    esac
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
      '{jobId:$id,status:.status,archiveFilename:.filename,sha256:.sha256,issues:null}' "$APPLE_FIXTURE_STATE/$arch.json" >"$logfile"
    case "$APPLE_FIXTURE_MODE" in
      accepted-minimal-log) jq 'del(.archiveFilename, .sha256) | .status = "accepted"' "$logfile" >"$logfile.new" ;;
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
    if [ "$APPLE_FIXTURE_MODE" = ticket-pending ]; then echo 'NSURLErrorDomain Code=-1005' >&2; exit 68; fi
    [ "$APPLE_FIXTURE_MODE" != ticket-unclassified ] || exit 68
    case "$APPLE_FIXTURE_MODE:$3" in second-staple-failure:*_x86_64.dmg) exit 1 ;; esac
    printf 'stapled\n' >>"$3"
    ;;
  'stapler validate')
    if [ "$APPLE_FIXTURE_MODE" = validation-pending ]; then echo 'NSURLErrorDomain Code=-1005' >&2; exit 68; fi
    [ "$APPLE_FIXTURE_MODE" != invalid-ticket ] ;;
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
export GITHUB_REPOSITORY=fixture/hikyo GITHUB_RUN_ID=1234 GITHUB_RUN_ATTEMPT=1 APPLE_NOTARY_INPUT_ARTIFACT_ID=5678
export APPLE_FIXTURE_DIST="$fixture/dist"
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
# Fixed signing timestamps make the synthetic signed handoff deterministic.
export APPLE_FIXTURE_MODE=accepted
mkdir "$fixture/state"
cp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
cp -R "$fixture/original" "$fixture/dist"
"$script_dir/sign-apple-artifacts.sh" --sign-only "$fixture/dist" >"$fixture/out" 2>"$fixture/err"
cp -R "$fixture/dist" "$fixture/reference-signed"
expected_binding() {
  for binding_arch in arm64 x86_64; do
    printf '%s  hikyo_%s_Darwin_%s.tar.gz\n' \
      "$(shasum -a 256 "$fixture/reference-signed/hikyo_1.2.3_Darwin_$binding_arch.tar.gz" | awk '{print $1}')" "$1" "$binding_arch"
    printf '%s  hikyo_%s_Darwin_%s.dmg\n' "$submitted_fixture_sha256" "$1" "$binding_arch"
  done | LC_ALL=C sort | shasum -a 256 | awk '{print $1}'
}
APPLE_NOTARY_INPUT_SHA256=$(expected_binding 1.2.3)
export APPLE_NOTARY_INPUT_SHA256
rm -rf "$fixture/dist" "$fixture/state"
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
  if [ "$submissions" -gt 0 ]; then
    [ "$submissions" -eq 2 ] && [ "$receipts" -eq 2 ]
  else
    [ "$receipts" -eq 0 ] || exit 1
  fi
  if [ "$APPLE_FIXTURE_MODE" = terminal-rejected ]; then
    jq -e '.status == "Rejected" and .diagnostics.availability == "available"' "$fixture/evidence/arm64-receipt.json" >/dev/null
    [ "$logs" -eq 2 ]
    if grep -F 'xcrun stapler' "$APPLE_FIXTURE_LOG" >/dev/null; then exit 1; fi
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
      .schema == "hikyo.dev/apple-notarization-receipt/v2" and .version == $version and
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
      accepted:* | accepted-warnings:* | accepted-minimal-log:* | accepted-dynamic-signature:*)
        stapled_sha=$(shasum -a 256 "$fixture/dist/hikyo_${evidence_version}_Darwin_$evidence_arch.dmg" | awk '{print $1}')
        [ "$stapled_sha" != "$submitted_fixture_sha256" ] || { printf 'apple fixture: pre-staple digest not distinguished\n' >&2; exit 1; }
        ;;
    esac
  done
  case "$APPLE_FIXTURE_MODE" in
    accepted | accepted-warnings | accepted-minimal-log | accepted-dynamic-signature) ;;
    *)
      if [ "$submissions" -gt 0 ]; then
        for retained_arch in arm64 x86_64; do
          cmp "$fixture/evidence/$retained_arch-submitted.dmg" "$fixture/dist/hikyo_${evidence_version}_Darwin_$retained_arch.dmg"
        done
      fi ;;
  esac
  [ "$logs" -eq "$expected_logs" ]
  [ "$(find "$fixture/evidence" -type f | wc -l | tr -d ' ')" -eq $((receipts * 2 + logs)) ]
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

for APPLE_FIXTURE_MODE in accepted accepted-dynamic-signature accepted-warnings accepted-minimal-log search-list-read-failure search-list-update-failure search-list-restore-failure keychain-delete-failure invalid-identity wrong-imported-identity identity-query-failure missing-evidence-dir evidence-contains-scratch stale-evidence missing-secret wrong-team wrong-candidate wrong-tag tampered-input unexpected-entry symlink-entry wrong-arch bad-signature rejected terminal-rejected timeout staple-failure invalid-ticket gatekeeper-failure timeout-no-id accepted-no-id log-failure malformed-log mismatched-job mismatched-status mismatched-filename mismatched-hash malformed-issues second-rejected; do
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
      accepted-dynamic-signature) unset APPLE_NOTARY_INPUT_SHA256 ;;
      missing-secret) unset APPLE_NOTARY_KEY_P8 ;;
      missing-evidence-dir) unset APPLE_NOTARY_EVIDENCE_DIR ;;
      evidence-contains-scratch) set -- "$@" APPLE_NOTARY_EVIDENCE_DIR="$fixture/scratch" ;;
      wrong-team) export APPLE_TEAM_ID=BBBBBBBBBB ;;
      wrong-candidate) set -- "$@" CANDIDATE_SHA256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
      wrong-tag) set -- "$@" GITHUB_REF=refs/tags/v2.0.0 ;;
    esac
    "$@" "$script_dir/sign-apple-artifacts.sh" "$fixture/dist"
  ) >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  if [ "$APPLE_FIXTURE_MODE" = accepted ] || [ "$APPLE_FIXTURE_MODE" = accepted-warnings ] || [ "$APPLE_FIXTURE_MODE" = accepted-minimal-log ] || [ "$APPLE_FIXTURE_MODE" = accepted-dynamic-signature ]; then
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
  case "$APPLE_FIXTURE_MODE" in
    missing-secret | missing-evidence-dir) "$@" "$script_dir/sign-apple-artifacts.sh" --notarize-only "$fixture/dist"; return ;;
  esac
  "$@" "$script_dir/sign-apple-artifacts.sh" --prepare-only "$fixture/dist" || return
  "$@" "$script_dir/sign-apple-artifacts.sh" --submit-only "$fixture/dist" || :
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

# Image creation retries must be bounded independently for each architecture.
# Run every regression even on failure so the red witness covers the whole set.
run_image_creation_case() {
  : >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
  cp "$fixture/original-search-list" "$APPLE_FIXTURE_SEARCH_LIST"
  rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state"
  mkdir "$fixture/evidence" "$fixture/state"
  cp -R "$fixture/original" "$fixture/dist"
  "$script_dir/sign-apple-artifacts.sh" "$fixture/dist" >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  expected_arm64=1
  expected_x86_64=0
  expected_delays=0
  expected_result=13
  case "$APPLE_FIXTURE_MODE" in
    image-busy-once) expected_arm64=2; expected_x86_64=2; expected_delays=2; expected_result=0 ;;
    image-busy-always) expected_arm64=3; expected_delays=2; expected_result=1 ;;
    image-second-busy-always) expected_x86_64=3; expected_delays=2; expected_result=1 ;;
  esac
  for image_arch in arm64 x86_64; do
    expected_attempts=$expected_arm64
    [ "$image_arch" != x86_64 ] || expected_attempts=$expected_x86_64
    attempts=$(grep -c "^image attempt $image_arch " "$APPLE_FIXTURE_LOG" || true)
    printf 'apple image fixture: %s %s attempts=%s expected=%s\n' "$APPLE_FIXTURE_MODE" "$image_arch" "$attempts" "$expected_attempts"
    [ "$attempts" -eq "$expected_attempts" ]
  done
  delays=$(grep -c '^sleep 2$' "$APPLE_FIXTURE_LOG" || true)
  printf 'apple image fixture: %s delays=%s expected=%s status=%s expected=%s\n' "$APPLE_FIXTURE_MODE" "$delays" "$expected_delays" "$result" "$expected_result"
  [ "$delays" -eq "$expected_delays" ]
  [ "$result" -eq "$expected_result" ]
  for image_arch in arm64 x86_64; do
    expected_attempts=$expected_arm64
    [ "$image_arch" != x86_64 ] || expected_attempts=$expected_x86_64
    [ "$(grep -c "^image options $image_arch spotlight=false$" "$APPLE_FIXTURE_LOG" || true)" -eq "$expected_attempts" ]
  done
  if [ "$expected_result" -eq 0 ]; then
    assert_signed_outputs 1.2.3
    [ "$(grep -c '^xcrun notarytool submit$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
    [ "$(grep -c '^xcrun stapler validate$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
    [ "$(grep -c '^spctl$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
  else
    for file in "$fixture/original/"*; do cmp "$file" "$fixture/dist/$(basename "$file")"; done
    [ "$(find "$fixture/dist" -type f | wc -l | tr -d ' ')" -eq "$(find "$fixture/original" -type f | wc -l | tr -d ' ')" ]
    [ -z "$(find "$fixture/dist" -name '*.dmg' -print)" ]
    if grep -E '^xcrun |^spctl$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
    [ ! -s "$GITHUB_OUTPUT" ]
    [ -z "$(find "$fixture/evidence" -mindepth 1 -print)" ]
    case "$APPLE_FIXTURE_MODE" in
      image-busy-always | image-second-busy-always) diagnostic='hdiutil: create failed - Resource busy' ;;
      image-error | image-mixed-error) diagnostic='hdiutil: create failed - Permission denied' ;;
      image-other-busy) diagnostic='hdiutil: create failed - Permission denied (Resource busy)' ;;
      image-prefixed-busy) diagnostic='warning: hdiutil: create failed - Resource busy' ;;
    esac
    grep -Fx "$diagnostic" "$fixture/err" >/dev/null
  fi
  grep -Fx 'security delete-keychain' "$APPLE_FIXTURE_LOG" >/dev/null
  assert_split_cleanup
}
image_failures=0
for APPLE_FIXTURE_MODE in image-busy-once image-busy-always image-second-busy-always image-error image-other-busy image-prefixed-busy image-mixed-error; do
  export APPLE_FIXTURE_MODE
  set +e
  (set -e; run_image_creation_case)
  image_result=$?
  set -e
  if [ "$image_result" -ne 0 ]; then
    printf 'apple image fixture: FAIL %s\n' "$APPLE_FIXTURE_MODE" >&2
    cat "$fixture/err" >&2
    image_failures=$((image_failures + 1))
  else
    printf 'apple image fixture: PASS %s\n' "$APPLE_FIXTURE_MODE"
  fi
done
[ "$image_failures" -eq 0 ] || exit 1

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
  if grep -E '^security |^hdiutil create$|^codesign --(force|sign)$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  assert_evidence 1.2.3
  assert_split_cleanup
done

export APPLE_RELEASE_CHANNEL=nightly APPLE_RELEASE_VERSION=1.2.3-nightly.20261021.1.gaaaaaaaa
export APPLE_RELEASE_COMMIT=$GITHUB_SHA GITHUB_REF=refs/heads/main
APPLE_NOTARY_INPUT_SHA256=$(expected_binding "$APPLE_RELEASE_VERSION")
export APPLE_NOTARY_INPUT_SHA256
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
  if grep -E '^security |^hdiutil create$|^codesign --(force|sign)$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  assert_evidence "$APPLE_RELEASE_VERSION"
  assert_split_cleanup
done
# Durable phase/recovery regressions. These retain the same original signed
# Actions handoff across attempts; no fixture can contact Apple or load a P12.
unset APPLE_RELEASE_CHANNEL APPLE_RELEASE_VERSION APPLE_RELEASE_COMMIT
export GITHUB_REF=refs/tags/v1.2.3
APPLE_NOTARY_INPUT_SHA256=$(expected_binding 1.2.3)
export APPLE_NOTARY_INPUT_SHA256
reset_recovery() {
  export APPLE_FIXTURE_MODE=accepted
  : >"$APPLE_FIXTURE_LOG"
  : >"$GITHUB_OUTPUT"
  rm -rf "$fixture/dist" "$fixture/evidence" "$fixture/state" "$fixture/retained"
  mkdir "$fixture/evidence" "$fixture/state"
  cp -R "$fixture/reference-signed" "$fixture/dist"
}
phase() (
  unset APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD
  if [ "$1" = --prepare-only ]; then
    unset APPLE_NOTARY_KEY_P8 APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER_ID
    export APPLE_FIXTURE_PHASE=prepare
  else
    export APPLE_FIXTURE_PHASE=notarize
  fi
  "$script_dir/sign-apple-artifacts.sh" "$@" "$fixture/dist" >"$fixture/out" 2>"$fixture/err"
)
expect_phase_failure() {
  if phase "$@"; then printf 'unexpected phase success: %s %s\n' "$APPLE_FIXTURE_MODE" "$1" >&2; exit 1; fi
}
assert_original_images() {
  for original_arch in arm64 x86_64; do
    cmp "$fixture/reference-signed/hikyo_1.2.3_Darwin_$original_arch.dmg" "$fixture/dist/hikyo_1.2.3_Darwin_$original_arch.dmg"
    cmp "$fixture/reference-signed/hikyo_1.2.3_Darwin_$original_arch.dmg" "$fixture/evidence/$original_arch-submitted.dmg"
  done
  [ -z "$(find "$fixture/dist" -name '.apple-final.*' -print)" ]
}
assert_no_resubmission() {
  [ "$(grep -c '^xcrun notarytool submit$' "$APPLE_FIXTURE_LOG" || true)" -eq 2 ]
}
reset_recovery
phase --prepare-only
[ "$(find "$fixture/evidence" -type f | wc -l | tr -d ' ')" -eq 4 ]
if grep -E '^security |^xcrun ' "$APPLE_FIXTURE_LOG"; then exit 1; fi
for arch in arm64 x86_64; do
  jq -e '.phase == "Prepared" and .submission_id == null and .original_run_attempt == "1" and
    .input_artifact_id == "5678" and .original_run_id == "1234" and .repository == "fixture/hikyo"' "$fixture/evidence/$arch-receipt.json" >/dev/null
done
phase --submit-only
assert_no_resubmission
if grep -E '^xcrun notarytool wait$|^xcrun stapler|^spctl$' "$APPLE_FIXTURE_LOG"; then exit 1; fi
phase --submit-only
assert_no_resubmission
phase --notarize-only
assert_no_resubmission
for arch in arm64 x86_64; do
  jq -e '.status == "Accepted" and .final_sha256 != .submitted_sha256 and .final_sha256 != null' "$fixture/evidence/$arch-receipt.json" >/dev/null
  cmp "$fixture/reference-signed/hikyo_1.2.3_Darwin_$arch.dmg" "$fixture/evidence/$arch-submitted.dmg"
done
assert_split_cleanup
printf 'apple recovery fixture: credential-free intents, split submission, UUID reuse and retained originals PASS\n'

# ARM's UUID is checkpointable before Intel is even called. On a new attempt,
# an explicit GitHub skipped-step proof may authorize the still-unstarted Intel
# intent; the ARM UUID is always reused.
reset_recovery
phase --prepare-only
phase --submit-only --architecture arm64
[ "$(grep -c '^xcrun notarytool submit$' "$APPLE_FIXTURE_LOG")" -eq 1 ]
[ ! -f "$APPLE_FIXTURE_STATE/x86_64.json" ]
cp -R "$fixture/evidence" "$fixture/retained"
export GITHUB_RUN_ATTEMPT=2 APPLE_NOTARY_UNSTARTED_ARCHITECTURES=x86_64
phase --submit-only --architecture arm64
phase --submit-only --architecture x86_64
assert_no_resubmission
phase --notarize-only
assert_split_cleanup
unset APPLE_NOTARY_UNSTARTED_ARCHITECTURES
export GITHUB_RUN_ATTEMPT=1
printf 'apple recovery fixture: per-architecture checkpoint and proven-unstarted continuation PASS\n'

# Losing the runner during Intel upload cannot erase the already retained ARM
# UUID. Intel requires strict history reconciliation, never another upload.
reset_recovery
phase --prepare-only
phase --submit-only --architecture arm64
cp -R "$fixture/evidence" "$fixture/retained"
export APPLE_FIXTURE_MODE=accepted-no-id
expect_phase_failure --submit-only --architecture x86_64
rm -rf "$fixture/evidence"
cp -R "$fixture/retained" "$fixture/evidence"
export GITHUB_RUN_ATTEMPT=2 APPLE_FIXTURE_MODE=history-both
phase --submit-only --architecture arm64
phase --submit-only --architecture x86_64
assert_no_resubmission
phase --notarize-only
assert_split_cleanup
export GITHUB_RUN_ATTEMPT=1
printf 'apple recovery fixture: runner loss during second upload retains first UUID and reconciles second PASS\n'

reset_recovery
phase --prepare-only
export APPLE_FIXTURE_MODE=wait-interrupted
phase --submit-only
: >"$GITHUB_OUTPUT"
expect_phase_failure --notarize-only
jq -e '.status == "In Progress" and .submission_id != null' "$fixture/evidence/arm64-receipt.json" >/dev/null
if grep -E '^fallback_authorized=true$|^recovery_pending=true$' "$GITHUB_OUTPUT"; then exit 1; fi
assert_no_resubmission
assert_split_cleanup
printf 'apple recovery fixture: interrupted wait retains pending UUID without relying on step outputs PASS\n'

# A failed finite wait is pending, never success. Two retries query the exact
# same UUIDs without another upload, and can finish after Apple accepts.
reset_recovery
phase --prepare-only
export APPLE_FIXTURE_MODE=timeout
expect_phase_failure --submit-only
for retry in 1 2; do
  : >"$GITHUB_OUTPUT"
  expect_phase_failure --notarize-only
  grep -Fx 'recovery_pending=true' "$GITHUB_OUTPUT" >/dev/null
  assert_no_resubmission
  assert_original_images
  printf 'apple recovery fixture: finite pending retry %s PASS\n' "$retry"
done
export APPLE_FIXTURE_MODE=retry-accepted
phase --notarize-only
assert_no_resubmission
assert_split_cleanup

# Every binding/schema/log check covers BOTH stages before any provider call.
# Restore a pristine Submitted pair between corruptions and retain the corrupt
# witness byte-for-byte on refusal.
for corruption in candidate input-digest input-id source ref channel repository run future-attempt architecture filename submitted-hash identity malformed multi-json schema bad-uuid invalid-phase malformed-diagnostics symlink-receipt symlink-original missing-original extra-file locked symlink-directory; do
  reset_recovery
  phase --prepare-only
  phase --submit-only
  : >"$APPLE_FIXTURE_LOG"
  receipt=$fixture/evidence/x86_64-receipt.json
  filter=
  case "$corruption" in
    candidate) filter='.candidate_sha256 = ("b" * 64)' ;;
    input-digest) filter='.input_sha256 = ("b" * 64)' ;;
    input-id) filter='.input_artifact_id = "9999"' ;;
    source) filter='.source_commit = ("b" * 40)' ;;
    ref) filter='.source_ref = "refs/tags/v9.0.0"' ;;
    channel) filter='.channel = "nightly"' ;;
    repository) filter='.repository = "other/hikyo"' ;;
    run) filter='.original_run_id = "4321"' ;;
    future-attempt) filter='.original_run_attempt = "2"' ;;
    architecture) filter='.architecture = "arm64"' ;;
    filename) filter='.archive_filename = "other.dmg"' ;;
    submitted-hash) filter='.submitted_sha256 = ("b" * 64)' ;;
    identity) filter='.team_id = "BBBBBBBBBB"' ;;
    malformed) printf '{broken\n' >"$receipt" ;;
    multi-json) printf '{}\n' >>"$receipt" ;;
    schema) filter='.schema = "hikyo.dev/apple-notarization-receipt/v1"' ;;
    bad-uuid) filter='.submission_id = "unsafe/path"' ;;
    invalid-phase) filter='.phase = "Prepared"' ;;
    malformed-diagnostics) filter='.diagnostics.warning_count = "untrusted"' ;;
    symlink-receipt) mv "$receipt" "$fixture/symlink-target"; ln -s "$fixture/symlink-target" "$receipt" ;;
    symlink-original) rm "$fixture/evidence/x86_64-submitted.dmg"; ln -s "$fixture/dist/hikyo_1.2.3_Darwin_x86_64.dmg" "$fixture/evidence/x86_64-submitted.dmg" ;;
    missing-original) rm "$fixture/evidence/x86_64-submitted.dmg" ;;
    extra-file) printf 'arbitrary state\n' >"$fixture/evidence/unbound" ;;
    locked) mkdir "$fixture/evidence/.lock" ;;
    symlink-directory) mv "$fixture/evidence" "$fixture/retained"; ln -s "$fixture/retained" "$fixture/evidence" ;;
  esac
  if [ -n "$filter" ]; then jq "$filter" "$receipt" >"$fixture/mutated.json"; mv "$fixture/mutated.json" "$receipt"; fi
  cp "$receipt" "$fixture/witness"
  expect_phase_failure --notarize-only
  cmp "$receipt" "$fixture/witness"
  if grep '^xcrun ' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  printf 'apple recovery fixture: pre-provider refusal %s PASS\n' "$corruption"
done

# Restored diagnostics are checked before either architecture can contact Apple.
for log_corruption in malformed job status filename hash issues warnings; do
  reset_recovery
  phase --prepare-only
  phase --submit-only
  phase --notarize-only
  cp "$fixture/reference-signed/"* "$fixture/dist/"
  log=$fixture/evidence/x86_64-notary-log.json
  filter=
  case "$log_corruption" in
    malformed) printf '{broken\n' >"$log" ;;
    job) filter='.jobId = "cccccccc-1111-2222-3333-444444444444"' ;;
    status) filter='.status = "Invalid"' ;;
    filename) filter='.archiveFilename = "other.dmg"' ;;
    hash) filter='.sha256 = ("b" * 64)' ;;
    issues) filter='.issues = "untrusted"' ;;
    warnings) filter='.issues = [{severity:"warning"}]' ;;
  esac
  if [ -n "$filter" ]; then jq "$filter" "$log" >"$fixture/mutated.json"; mv "$fixture/mutated.json" "$log"; fi
  cp "$log" "$fixture/witness"
  : >"$APPLE_FIXTURE_LOG"
  expect_phase_failure --notarize-only
  cmp "$log" "$fixture/witness"
  if grep '^xcrun ' "$APPLE_FIXTURE_LOG"; then exit 1; fi
  assert_original_images
  assert_split_cleanup
  printf 'apple recovery fixture: retained log refusal %s PASS\n' "$log_corruption"
done

# A lost UUID or a retained intent from a prior attempt is never blindly
# submitted. Recovery requires exactly one diagnostic with name AND digest.
for recovery in history-empty history-malformed history-ambiguous history-match history-minimal-log history-wrong-hash history-pending old-prepared old-prepared-empty; do
  reset_recovery
  phase --prepare-only
  phase --submit-only
  receipt=$fixture/evidence/arm64-receipt.json
  jq '.phase = "Submitting" | .submission_id = null | .status = "Unknown"' "$receipt" >"$fixture/mutated.json"
  if [ "$recovery" = old-prepared ] || [ "$recovery" = old-prepared-empty ]; then
    jq '.phase = "Prepared"' "$fixture/mutated.json" >"$fixture/old-prepared.json"
    mv "$fixture/old-prepared.json" "$fixture/mutated.json"
  fi
  mv "$fixture/mutated.json" "$receipt"
  cp "$receipt" "$fixture/witness"
  export APPLE_FIXTURE_MODE=$recovery
  case "$recovery" in
    old-prepared-empty) export APPLE_FIXTURE_MODE=history-empty ;;
    history-minimal-log) export APPLE_FIXTURE_MODE=accepted-minimal-log ;;
    history-wrong-hash) export APPLE_FIXTURE_MODE=mismatched-hash ;;
    history-pending)
      jq '.status = "In Progress"' "$fixture/state/arm64.json" >"$fixture/mutated.json"
      mv "$fixture/mutated.json" "$fixture/state/arm64.json" ;;
  esac
  export GITHUB_RUN_ATTEMPT=2
  : >"$GITHUB_OUTPUT"
  if [ "$recovery" = history-match ] || [ "$recovery" = old-prepared ]; then
    phase --submit-only
    jq -e '.submission_id == "aaaaaaaa-1111-2222-3333-444444444444" and .original_run_attempt == "1"' "$receipt" >/dev/null
  else
    expect_phase_failure --submit-only
    cmp "$receipt" "$fixture/witness"
    if grep -Fx 'recovery_pending=true' "$GITHUB_OUTPUT"; then exit 1; fi
  fi
  export GITHUB_RUN_ATTEMPT=1
  assert_no_resubmission
  assert_original_images
  assert_split_cleanup
  printf 'apple recovery fixture: lost UUID %s PASS\n' "$recovery"
done

for APPLE_FIXTURE_MODE in info-failure malformed-info; do
  failure_mode=$APPLE_FIXTURE_MODE
  reset_recovery
  phase --prepare-only
  phase --submit-only
  export APPLE_FIXTURE_MODE=$failure_mode
  : >"$GITHUB_OUTPUT"
  expect_phase_failure --notarize-only
  assert_no_resubmission
  assert_original_images
  if grep -Fx 'notarized=true' "$GITHUB_OUTPUT"; then exit 1; fi
  if grep -Fx 'fallback_authorized=true' "$GITHUB_OUTPUT"; then exit 1; fi
  if [ "$APPLE_FIXTURE_MODE" = info-failure ]; then
    grep -Fx 'recovery_pending=true' "$GITHUB_OUTPUT" >/dev/null
  elif grep -Fx 'recovery_pending=true' "$GITHUB_OUTPUT"; then exit 1; fi
  assert_split_cleanup
  printf 'apple recovery fixture: query refusal %s PASS\n' "$APPLE_FIXTURE_MODE"
done

# All post-acceptance failures retain BOTH original disk images, including an
# accepted first architecture followed by a failed second architecture.
for APPLE_FIXTURE_MODE in staple-failure second-staple-failure invalid-ticket gatekeeper-failure post-staple-signature-failure mount-failure mounted-license-mismatch mounted-binary-mismatch mounted-symlink mounted-wrong-arch detach-failure private-cleanup-failure ticket-pending validation-pending ticket-unclassified; do
  failure_mode=$APPLE_FIXTURE_MODE
  reset_recovery
  phase --prepare-only
  phase --submit-only
  export APPLE_FIXTURE_MODE=$failure_mode
  : >"$GITHUB_OUTPUT"
  expect_phase_failure --notarize-only
  assert_no_resubmission
  assert_original_images
  if grep -Fx 'fallback_authorized=true' "$GITHUB_OUTPUT"; then exit 1; fi
  if [ "$APPLE_FIXTURE_MODE" = ticket-pending ] || [ "$APPLE_FIXTURE_MODE" = validation-pending ]; then
    grep -Fx 'recovery_pending=true' "$GITHUB_OUTPUT" >/dev/null
  elif grep -Fx 'recovery_pending=true' "$GITHUB_OUTPUT"; then exit 1; fi
  [ -z "$(find "$fixture/state" -name 'mounted-*' -print)" ]
  assert_split_cleanup
  printf 'apple recovery fixture: promotion blocked %s PASS\n' "$APPLE_FIXTURE_MODE"
done

# A persistent detach failure must scrub credentials without walking into an
# attached filesystem. This mock creates ordinary files, never a real mount.
reset_recovery
phase --prepare-only
phase --submit-only
export APPLE_FIXTURE_MODE=persistent-detach-failure
expect_phase_failure --notarize-only
assert_original_images
[ "$(find "$fixture/state" -name 'mounted-*' | wc -l | tr -d ' ')" -eq 1 ]
if find "$fixture/scratch" -type f | grep -Ev '/mount-arm64/(hikyo|LICENSE)$'; then exit 1; fi
[ "$(grep -c '^hdiutil detach$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
rm -rf "$fixture/scratch/"*
printf 'apple recovery fixture: persistent detach failure scrubs private files PASS\n'
for terminal in rejected terminal-rejected second-rejected; do
  reset_recovery
  export APPLE_FIXTURE_MODE=$terminal
  phase --prepare-only
  phase --submit-only
  expect_phase_failure --notarize-only
  grep -Fx 'fallback_authorized=true' "$GITHUB_OUTPUT" >/dev/null
  assert_no_resubmission
  assert_original_images
  assert_split_cleanup
  printf 'apple recovery fixture: explicit terminal fallback %s PASS\n' "$terminal"
done
printf 'apple fixtures: stable/nightly full and split phases, credential/tool isolation, signed handoff validation, both intents before submission, keychain cleanup, diagnostic matrix, receipts, warnings, recovery, mounted payload and unchanged original images/archives/Linux/Windows verified\n'

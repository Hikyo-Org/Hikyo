#!/bin/sh
# Synthetic command contracts only, no Apple signing or acceptance claim.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-native-fixture.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir "$fixture/bin" "$fixture/payload" "$fixture/dist" "$fixture/tmp"
export NATIVE_FIXTURE_ROOT="$fixture"
cat >"$fixture/payload/hikyo" <<'EOF'
#!/bin/sh
case "$1" in
  --version) if [ "$NATIVE_FIXTURE_MODE" = wrong-version ]; then echo 0.0.0; else echo "${NATIVE_FIXTURE_VERSION:-1.2.3}"; fi ;;
  --help) [ "$NATIVE_FIXTURE_MODE" != failed-help ] ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$fixture/payload/hikyo"
echo 'fixture license' >"$fixture/payload/LICENSE"
tar -czf "$fixture/dist/hikyo_1.2.3_Darwin_arm64.tar.gz" -C "$fixture/payload" LICENSE hikyo
echo 'synthetic DMG' >"$fixture/dist/hikyo_1.2.3_Darwin_arm64.dmg"
cat >"$fixture/bin/mock" <<'EOF'
#!/bin/sh
set -eu
command=${0##*/}
printf '%s %s\n' "$command" "$*" >>"$NATIVE_FIXTURE_ROOT/log"
case "$command" in
  uname | lipo)
    if [ "$NATIVE_FIXTURE_MODE" = wrong-arch ]; then echo x86_64; else echo arm64; fi ;;
  codesign)
    [ "$NATIVE_FIXTURE_MODE" != bad-signature ]
    case "$NATIVE_FIXTURE_MODE:$*" in unnotarized-executable:*--check-notarization*) exit 1 ;; esac ;;
  xcrun) [ "$NATIVE_FIXTURE_MODE" != bad-ticket ] ;;
  spctl)
    case "$NATIVE_FIXTURE_MODE:$*" in
      rejected-image:*'type open'*) exit 1 ;;
    esac ;;
  xattr)
    case "$1" in
      -w) printf '%s\n' "$3" >"$NATIVE_FIXTURE_ROOT/quarantine" ;;
      -p) [ "$NATIVE_FIXTURE_MODE" != cleared-quarantine ] || exit 1
        cat "$NATIVE_FIXTURE_ROOT/quarantine" ;;
      *) exit 1 ;;
    esac ;;
  hdiutil)
    case "$1" in
      attach)
        [ "$2:$3:$4" = '-readonly:-nobrowse:-mountpoint' ]
        cp "$NATIVE_FIXTURE_ROOT/payload/hikyo" "$NATIVE_FIXTURE_ROOT/payload/LICENSE" "$5/"
        if [ "$NATIVE_FIXTURE_MODE" = changed-payload ]; then echo changed >>"$5/hikyo"; fi ;;
      detach) [ "$NATIVE_FIXTURE_MODE" != failed-detach ] ;;
      *) exit 1 ;;
    esac ;;
  ditto) cp "$1" "$2" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$fixture/bin/mock"
for command in uname lipo codesign xcrun spctl xattr hdiutil ditto; do ln -s mock "$fixture/bin/$command"; done
export PATH="$fixture/bin:$PATH" TMPDIR="$fixture/tmp"
export APPLE_TEAM_ID=ABCDEFGHIJ APPLE_SIGNING_IDENTITY='Developer ID Application: Fixture (ABCDEFGHIJ)'
export GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa GITHUB_RUN_ID=42 APPLE_NOTARY_INPUT_ARTIFACT_ID=7
for NATIVE_FIXTURE_MODE in accepted wrong-arch bad-signature bad-ticket rejected-image unnotarized-executable changed-payload wrong-version failed-help cleared-quarantine failed-detach; do
  export NATIVE_FIXTURE_MODE
  rm -f "$fixture/receipt.json"
  : >"$fixture/log"
  if "$script_dir/verify-apple-install.sh" "$fixture/dist" 1.2.3 arm64 "$fixture/receipt.json" >"$fixture/out" 2>"$fixture/err"; then
    [ "$NATIVE_FIXTURE_MODE" = accepted ] || { cat "$fixture/err"; exit 1; }
    jq -e '.gatekeeper and .quarantine_retained and .native_version and .native_help and .architecture == "arm64"' "$fixture/receipt.json" >/dev/null
    grep -F 'codesign --verify --strict -R=notarized --check-notarization' "$fixture/log" >/dev/null
  else
    [ "$NATIVE_FIXTURE_MODE" != accepted ] || { cat "$fixture/err"; exit 1; }
    [ ! -e "$fixture/receipt.json" ] || exit 1
  fi
  if grep -E '^xattr -(c|d)' "$fixture/log"; then exit 1; fi
  printf 'Apple native fixture: %s PASS\n' "$NATIVE_FIXTURE_MODE"
done

export APPLE_RELEASE_CHANNEL=nightly APPLE_RELEASE_SOURCE_MODE=dev
export APPLE_RELEASE_COMMIT=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb GITHUB_REF=refs/heads/main
export NATIVE_FIXTURE_VERSION=1.2.3-nightly.1 NATIVE_FIXTURE_MODE=accepted
for extension in dmg tar.gz; do
  cp "$fixture/dist/hikyo_1.2.3_Darwin_arm64.$extension" "$fixture/dist/hikyo_${NATIVE_FIXTURE_VERSION}_Darwin_arm64.$extension"
done
jq -n --arg version "$NATIVE_FIXTURE_VERSION" --arg commit "$APPLE_RELEASE_COMMIT" \
  '{version:$version,commit:$commit}' >"$fixture/metadata.json"
for source_case in accepted implicit-main wrong-source wrong-version wrong-metadata missing-metadata symlink-metadata wrong-ref stable-channel unknown-mode; do
  rm -f "$fixture/receipt.json" "$fixture/dist/metadata.json"
  cp "$fixture/metadata.json" "$fixture/dist/metadata.json"
  : >"$fixture/log"
  set -- env
  case "$source_case" in
    implicit-main) set -- "$@" APPLE_RELEASE_SOURCE_MODE=main ;;
    wrong-source) set -- "$@" APPLE_RELEASE_COMMIT=cccccccccccccccccccccccccccccccccccccccc ;;
    wrong-version) jq '.version = "0.0.0"' "$fixture/metadata.json" >"$fixture/dist/metadata.json" ;;
    wrong-metadata) printf '{}\n' >"$fixture/dist/metadata.json" ;;
    missing-metadata) rm "$fixture/dist/metadata.json" ;;
    symlink-metadata) rm "$fixture/dist/metadata.json"; ln -s "$fixture/metadata.json" "$fixture/dist/metadata.json" ;;
    wrong-ref) set -- "$@" GITHUB_REF=refs/heads/dev ;;
    stable-channel) set -- "$@" APPLE_RELEASE_CHANNEL=stable ;;
    unknown-mode) set -- "$@" APPLE_RELEASE_SOURCE_MODE=unknown ;;
  esac
  if "$@" "$script_dir/verify-apple-install.sh" "$fixture/dist" "$NATIVE_FIXTURE_VERSION" arm64 "$fixture/receipt.json" >"$fixture/out" 2>"$fixture/err"; then
    [ "$source_case" = accepted ] || { cat "$fixture/err"; exit 1; }
    jq -e --arg source "$APPLE_RELEASE_COMMIT" --arg publisher "$GITHUB_SHA" '
      .source_commit == $source and .publisher_commit == $publisher and .publisher_ref == "refs/heads/main" and
      .input_artifact_id == "7" and .run_id == "42" and .native_version and .native_help and .quarantine_retained
    ' "$fixture/receipt.json" >/dev/null
  else
    [ "$source_case" != accepted ] || { cat "$fixture/err"; exit 1; }
    [ ! -e "$fixture/receipt.json" ] || exit 1
    if grep -Ev '^uname -m$' "$fixture/log"; then
      printf 'native source refusal reached installation: %s\n' "$source_case" >&2; exit 1
    fi
  fi
  printf 'Apple native dev source fixture: %s PASS\n' "$source_case"
done
jq -n --arg version "$NATIVE_FIXTURE_VERSION" --arg commit "$GITHUB_SHA" \
  '{version:$version,commit:$commit}' >"$fixture/dist/metadata.json"
env APPLE_RELEASE_SOURCE_MODE=main APPLE_RELEASE_COMMIT="$GITHUB_SHA" \
  "$script_dir/verify-apple-install.sh" "$fixture/dist" "$NATIVE_FIXTURE_VERSION" arm64 "$fixture/receipt.json" >"$fixture/out" 2>"$fixture/err"
jq -e --arg source "$GITHUB_SHA" '
  .source_commit == $source and (has("publisher_commit") | not) and (has("publisher_ref") | not)
' "$fixture/receipt.json" >/dev/null
printf 'Apple native main nightly source fixture: legacy receipt PASS\n'

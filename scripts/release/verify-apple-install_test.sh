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
  --version) if [ "$NATIVE_FIXTURE_MODE" = wrong-version ]; then echo 0.0.0; else echo 1.2.3; fi ;;
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
  codesign) [ "$NATIVE_FIXTURE_MODE" != bad-signature ] ;;
  xcrun) [ "$NATIVE_FIXTURE_MODE" != bad-ticket ] ;;
  spctl)
    case "$NATIVE_FIXTURE_MODE:$*" in
      rejected-image:*'type open'* | rejected-executable:*'type execute'*) exit 1 ;;
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
for NATIVE_FIXTURE_MODE in accepted wrong-arch bad-signature bad-ticket rejected-image rejected-executable changed-payload wrong-version failed-help cleared-quarantine failed-detach; do
  export NATIVE_FIXTURE_MODE
  rm -f "$fixture/receipt.json"
  : >"$fixture/log"
  if "$script_dir/verify-apple-install.sh" "$fixture/dist" 1.2.3 arm64 "$fixture/receipt.json" >"$fixture/out" 2>"$fixture/err"; then
    [ "$NATIVE_FIXTURE_MODE" = accepted ] || { cat "$fixture/err"; exit 1; }
    jq -e '.gatekeeper and .quarantine_retained and .native_version and .native_help and .architecture == "arm64"' "$fixture/receipt.json" >/dev/null
    grep -F 'spctl --assess --type execute' "$fixture/log" >/dev/null
  else
    [ "$NATIVE_FIXTURE_MODE" != accepted ] || { cat "$fixture/err"; exit 1; }
    [ ! -e "$fixture/receipt.json" ] || exit 1
  fi
  if grep -E '^xattr -(c|d)' "$fixture/log"; then exit 1; fi
  printf 'Apple native fixture: %s PASS\n' "$NATIVE_FIXTURE_MODE"
done

#!/bin/sh
# Exercise control flow with disposable mock Apple tools, never real credentials.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-apple-fixture.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir "$fixture/bin" "$fixture/scratch" "$fixture/original" "$fixture/payload"
export APPLE_FIXTURE_LOG="$fixture/calls"
cat >"$fixture/bin/uname" <<'EOF'
#!/bin/sh
printf 'Darwin\n'
EOF
cat >"$fixture/bin/security" <<'EOF'
#!/bin/sh
set -eu
printf 'security %s\n' "$1" >>"$APPLE_FIXTURE_LOG"
case "$1" in
  create-keychain) touch "$4" ;;
  delete-keychain) rm "$2" ;;
esac
EOF
cat >"$fixture/bin/lipo" <<'EOF'
#!/bin/sh
[ "$APPLE_FIXTURE_MODE" != wrong-arch ] || { printf 'i386\n'; exit 0; }
basename "$(dirname "$2")"
EOF
cat >"$fixture/bin/codesign" <<'EOF'
#!/bin/sh
set -eu
printf 'codesign %s\n' "$1" >>"$APPLE_FIXTURE_LOG"
[ "$APPLE_FIXTURE_MODE" != bad-signature ] || [ "$1" != --verify ] || exit 1
sign=false
requirement=false
for argument do
  if [ "$requirement" = true ]; then
    case "$argument" in '=anchor apple generic '*) ;; *) printf 'invalid inline requirement\n' >&2; exit 1 ;; esac
    requirement=false
  fi
  [ "$argument" != -R ] || requirement=true
  [ "$argument" != --sign ] || sign=true
  target=$argument
done
if [ "$sign" = true ]; then printf 'Developer ID signed\n' >>"$target"; fi
EOF
cat >"$fixture/bin/hdiutil" <<'EOF'
#!/bin/sh
set -eu
for argument do target=$argument; done
printf 'disk image\n' >"$target"
EOF
cat >"$fixture/bin/xcrun" <<'EOF'
#!/bin/sh
set -eu
printf 'xcrun %s %s\n' "$1" "$2" >>"$APPLE_FIXTURE_LOG"
case "$1 $2" in
  'notarytool submit')
    [ "$APPLE_FIXTURE_MODE" != timeout ] || exit 1
    status=Accepted
    [ "$APPLE_FIXTURE_MODE" != rejected ] || status=Invalid
    printf '{"status":"%s","id":"fixture-submission"}\n' "$status"
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
[ "$APPLE_FIXTURE_MODE" != gatekeeper-failure ]
EOF
chmod +x "$fixture/bin/"*
export PATH="$fixture/bin:$PATH" TMPDIR="$fixture/scratch"
export APPLE_DEVELOPER_ID_P12_BASE64=Zml4dHVyZQ== APPLE_DEVELOPER_ID_P12_PASSWORD=fixture-password
export APPLE_SIGNING_IDENTITY='Developer ID Application: Fixture (AAAAAAAAAA)' APPLE_TEAM_ID=AAAAAAAAAA
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
for APPLE_FIXTURE_MODE in accepted missing-secret wrong-team wrong-candidate wrong-tag tampered-input unexpected-entry symlink-entry wrong-arch bad-signature rejected timeout staple-failure invalid-ticket gatekeeper-failure; do
  export APPLE_FIXTURE_MODE
  : >"$APPLE_FIXTURE_LOG"
  rm -rf "$fixture/dist"
  cp -R "$fixture/original" "$fixture/dist"
  case "$APPLE_FIXTURE_MODE" in
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
      wrong-team) export APPLE_TEAM_ID=BBBBBBBBBB ;;
      wrong-candidate) export CANDIDATE_SHA256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
      wrong-tag) set -- "$@" GITHUB_REF=refs/tags/v2.0.0 ;;
    esac
    "$@" "$script_dir/sign-apple-artifacts.sh" "$fixture/dist"
  ) >"$fixture/out" 2>"$fixture/err" && result=0 || result=$?
  if [ "$APPLE_FIXTURE_MODE" = accepted ]; then
    [ "$result" -eq 0 ] || { cat "$fixture/err"; exit 1; }
    [ "$(wc -l <"$fixture/dist/checksums.txt" | tr -d ' ')" -eq 6 ]
    (cd "$fixture/dist" && shasum -a 256 -c checksums.txt >/dev/null)
    for arch in arm64 x86_64; do
      [ -s "$fixture/dist/hikyo_1.2.3_Darwin_$arch.dmg" ]
      tar -xOzf "$fixture/dist/hikyo_1.2.3_Darwin_$arch.tar.gz" hikyo | grep -F 'Developer ID signed' >/dev/null
      cmp "$fixture/original/hikyo_1.2.3_Linux_$arch.tar.gz" "$fixture/dist/hikyo_1.2.3_Linux_$arch.tar.gz"
      cmp "$fixture/original/hikyo_1.2.3_Windows_$arch.zip" "$fixture/dist/hikyo_1.2.3_Windows_$arch.zip"
    done
    [ "$(grep -c '^spctl$' "$APPLE_FIXTURE_LOG")" -eq 2 ]
  else
    [ "$result" -ne 0 ] || { printf 'apple fixture: accepted %s\n' "$APPLE_FIXTURE_MODE" >&2; exit 1; }
    case "$APPLE_FIXTURE_MODE" in
      unexpected-entry | symlink-entry) cmp "$fixture/expected-input-checksums" "$fixture/dist/checksums.txt" ;;
      *) cmp "$fixture/original/checksums.txt" "$fixture/dist/checksums.txt" ;;
    esac
  fi
  case "$APPLE_FIXTURE_MODE" in
    missing-secret | wrong-team | wrong-candidate | wrong-tag | tampered-input) [ ! -s "$APPLE_FIXTURE_LOG" ] ;;
    *) grep -Fx 'security delete-keychain' "$APPLE_FIXTURE_LOG" >/dev/null ;;
  esac
  [ -z "$(find "$fixture/scratch" -mindepth 1 -print)" ] || { printf 'apple fixture: leaked credentials\n' >&2; exit 1; }
  if grep -E 'fixture-password|fixture-private-key|Zml4dHVyZQ==' "$fixture/out" "$fixture/err"; then
    printf 'apple fixture: printed credentials\n' >&2; exit 1
  fi
done
export APPLE_RELEASE_CHANNEL=nightly APPLE_RELEASE_VERSION=1.2.3-nightly.20261021.1.gaaaaaaaa
export APPLE_RELEASE_COMMIT=$GITHUB_SHA GITHUB_REF=refs/heads/main
for APPLE_FIXTURE_MODE in accepted nightly-wrong-ref nightly-wrong-commit nightly-wrong-version nightly-wrong-metadata unknown-channel; do
  export APPLE_FIXTURE_MODE
  : >"$APPLE_FIXTURE_LOG"
  rm -rf "$fixture/dist"
  cp -R "$fixture/original" "$fixture/dist"
  rm "$fixture/dist/release-candidate.json"
  for file in "$fixture/dist/"hikyo_1.2.3_*; do
    mv "$file" "$fixture/dist/hikyo_${APPLE_RELEASE_VERSION}_${file##*1.2.3_}"
  done
  (cd "$fixture/dist" && shasum -a 256 hikyo_*.tar.gz hikyo_*.zip >checksums.txt)
  jq -n --arg version "$APPLE_RELEASE_VERSION" --arg commit "$GITHUB_SHA" \
    '{version:$version,commit:$commit}' >"$fixture/dist/metadata.json"
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
  [ -z "$(find "$fixture/scratch" -mindepth 1 -print)" ]
  if grep -E 'fixture-password|fixture-private-key|Zml4dHVyZQ==' "$fixture/out" "$fixture/err"; then exit 1; fi
done
printf 'apple fixtures: stable/nightly success, 19 refusal paths, cleanup and unchanged Linux/Windows verified\n'

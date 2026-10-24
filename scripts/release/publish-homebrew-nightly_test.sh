#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-homebrew-nightly.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir "$fixture/bin" "$fixture/bundle" "$fixture/public"
export BREW_FIXTURE_VERSION=0.0.1-nightly.20261024.92.gaaaaaaaa
export BREW_FIXTURE_CALLS="$fixture/calls"
export BREW_FIXTURE_PUBLIC="$fixture/public"
for arch in arm64 x86_64; do
  printf '%s image\n' "$arch" >"$fixture/bundle/hikyo_${BREW_FIXTURE_VERSION}_Darwin_$arch.dmg"
done
jq -n --arg version "$BREW_FIXTURE_VERSION" \
  --arg arm "$(shasum -a 256 "$fixture/bundle/"*arm64.dmg | awk '{print $1}')" \
  --arg intel "$(shasum -a 256 "$fixture/bundle/"*x86_64.dmg | awk '{print $1}')" '
  {schema:"hikyo.dev/nightly-manifest/v1",profile:"nightly/v1",version:$version,tag:("v"+$version),
   artifacts:[{name:("hikyo_"+$version+"_Darwin_arm64.dmg"),kind:"installer",sha256:$arm},
              {name:("hikyo_"+$version+"_Darwin_x86_64.dmg"),kind:"installer",sha256:$intel}]}
' >"$fixture/bundle/release-manifest.json"
cat >"$fixture/bin/go" <<'EOF'
#!/bin/sh
set -eu
[ "$1" = run ] && [ "$2" = ./scripts/release/nightly ] && [ "$3" = verify ]
[ "$4" = --trust ] && [ "$6" = --directory ] && [ -f "$7/release-manifest.json" ]
printf 'authenticate\n' >>"$BREW_FIXTURE_CALLS"
[ "${BREW_FIXTURE_AUTH_FAIL:-false}" = false ]
EOF
cat >"$fixture/bin/gh" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$BREW_FIXTURE_CALLS"
case "$1 $2" in
  'release view') printf '{"isDraft":false,"isPrerelease":true,"tagName":"v%s"}\n' "$BREW_FIXTURE_VERSION" ;;
  'release download')
    [ "$3" = "v$BREW_FIXTURE_VERSION" ]
    shift 3
    [ "$1" = --repo ] && [ "$2" = Hikyo-Org/Hikyo ]
    [ "$3" = --dir ] && [ -d "$4" ] && [ "$5" = --pattern ]
    case "$6" in
      "hikyo_${BREW_FIXTURE_VERSION}_Darwin_arm64.dmg" | "hikyo_${BREW_FIXTURE_VERSION}_Darwin_x86_64.dmg") ;;
      *) exit 2 ;;
    esac
    cp "$BREW_FIXTURE_PUBLIC/$6" "$4/$6"
    ;;
  'api repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo@nightly.rb?ref='*) printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1 ;;
  'api repos/Hikyo-Org/homebrew-tap/git/ref/heads/main') printf '%040d\n' 1 ;;
  'api repos/Hikyo-Org/homebrew-tap/git/ref/heads/release/hikyo@nightly-'*) printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1 ;;
  'api --method')
    case "$*" in
      *'POST repos/Hikyo-Org/homebrew-tap/git/refs '*) ;;
      *'PUT repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo@nightly.rb '*) ;;
      *) exit 2 ;;
    esac
    printf '{"commit":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}\n'
    ;;
  'pr list') printf '[]\n' ;;
  'pr create') printf 'https://github.com/Hikyo-Org/homebrew-tap/pull/2\n' ;;
  *) printf 'unexpected nightly publisher call: %s\n' "$*" >&2; exit 2 ;;
esac
EOF
chmod +x "$fixture/bin/"*
export PATH="$fixture/bin:$PATH" GH_BIN="$fixture/bin/gh"
for scenario in authentication-failure missing-public-arm64 missing-public-x86_64 tampered-public-arm64 tampered-public-x86_64 success missing-image; do
  : >"$BREW_FIXTURE_CALLS"
  export BREW_FIXTURE_AUTH_FAIL=false
  cp "$fixture/bundle/"*.dmg "$fixture/public/"
  case "$scenario" in
    authentication-failure) BREW_FIXTURE_AUTH_FAIL=true ;;
    missing-public-*) rm "$fixture/public/hikyo_${BREW_FIXTURE_VERSION}_Darwin_${scenario#missing-public-}.dmg" ;;
    tampered-public-*) printf 'different public bytes\n' >>"$fixture/public/hikyo_${BREW_FIXTURE_VERSION}_Darwin_${scenario#tampered-public-}.dmg" ;;
    missing-image) rm "$fixture/bundle/"*x86_64.dmg ;;
  esac
  status=0
  "$script_dir/publish-homebrew-cask.sh" --nightly Hikyo-Org/Hikyo "v$BREW_FIXTURE_VERSION" \
    "$fixture/bundle" Hikyo-Org/homebrew-tap >"$fixture/out" 2>"$fixture/err" || status=$?
  if [ "$scenario" = success ]; then
    [ "$status" -eq 0 ] || { cat "$fixture/err" >&2; exit 1; }
    grep -F 'PUT repos/Hikyo-Org/homebrew-tap/contents/Casks/hikyo@nightly.rb' "$BREW_FIXTURE_CALLS" >/dev/null
    grep -F -- "--head release/hikyo@nightly-$BREW_FIXTURE_VERSION" "$BREW_FIXTURE_CALLS" >/dev/null
    [ "$(grep -c '^release download ' "$BREW_FIXTURE_CALLS")" -eq 2 ]
  else
    [ "$status" -ne 0 ] || { printf 'nightly publisher unexpectedly accepted %s\n' "$scenario" >&2; exit 1; }
    if grep -E 'api --method|pr create' "$BREW_FIXTURE_CALLS"; then exit 1; fi
    case "$scenario" in
      missing-public-*) grep -F 'cannot download public disk image' "$fixture/err" >/dev/null ;;
      tampered-public-*) grep -F 'public disk image hash mismatch' "$fixture/err" >/dev/null ;;
    esac
  fi
  grep -Fx authenticate "$BREW_FIXTURE_CALLS" >/dev/null
  if grep -E 'Casks/hikyo\.rb|pr merge' "$BREW_FIXTURE_CALLS"; then exit 1; fi
done
printf 'Homebrew nightly publisher fixture: authenticated DMG pair, separate cask, no merge\n'

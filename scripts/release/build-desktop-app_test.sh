#!/bin/sh
# Argument and refusal logic of build-desktop-app.sh. No Apple credentials, no signing.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
script=$script_dir/build-desktop-app.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-desktop-build-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
fail() { printf 'desktop app fixture failed: %s\n' "$*" >&2; exit 1; }

refuses() {
	label=$1
	shift
	if env -u APPLE_SIGNING_IDENTITY -u APPLE_TEAM_ID -u APPLE_NOTARY_KEY_P8 -u APPLE_DEVELOPER_ID_P12_BASE64 \
		"$script" "$@" >"$work/out" 2>&1; then
		fail "$label accepted"
	fi
}
refuses_with() {
	label=$1 message=$2
	shift 2
	refuses "$label" "$@"
	grep -F "$message" "$work/out" >/dev/null || fail "$label refused without '$message': $(cat "$work/out")"
}

# Usage errors.
refuses 'no arguments'
refuses 'unknown subcommand' nonsense
refuses 'build with too few arguments' build 1.4.0 arm64
refuses 'verify with too few arguments' verify 1.4.0 arm64 app

# The app ships for stable versions only.
for version in 1.4.0-rc.1 0.9.0 1.4.0-nightly.20261101.1.gabcdef12 not-a-version; do
	refuses "build $version" build "$version" arm64 "$work/Hikyo.app"
	grep -E 'stable versions only|invalid version' "$work/out" >/dev/null || fail "build $version not refused for its version"
	[ ! -e "$work/Hikyo.app" ] || fail "build $version created output"
	refuses "image $version" image "$version" arm64 "$work/Hikyo.app" "$work/Hikyo-Desktop-$version-arm64.dmg"
done
refuses_with 'unknown architecture' 'architecture must be' build 1.4.0 riscv64 "$work/Hikyo.app"
refuses_with 'unknown architecture for image' 'architecture must be' image 1.4.0 ppc "$work/Hikyo.app" "$work/Hikyo-Desktop-1.4.0-ppc.dmg"
[ ! -e "$work/Hikyo.app" ] || fail 'architecture refusal created output'

# Credentials are checked before any keychain or notary action.
mkdir "$work/Hikyo.app"
refuses 'sign-app without identity' sign-app "$work/Hikyo.app"
refuses 'sign-image without identity' sign-image "$work/x.dmg"
refuses 'notarize without notary key' notarize "$work/x.dmg"
if [ "$(uname -s)" = Darwin ]; then
	refuses_with 'image with misnamed DMG' 'disk image name must be' image 1.4.0 arm64 "$work/Hikyo.app" "$work/other.dmg"
	refuses_with 'build with misnamed app' 'must end in Hikyo.app' build 1.4.0 arm64 "$work/Other.app"
	refuses_with 'sign-app without identity names it' 'missing APPLE_SIGNING_IDENTITY' sign-app "$work/Hikyo.app"
fi

# Entitlements are an allowlist: exactly an empty dictionary. The check is
# portable (python3 plistlib), so it runs on Linux CI as well as macOS.
"$script" check-entitlements "$repo_root/desktop/build/darwin/entitlements.plist" >/dev/null || fail 'committed entitlements refused'
for body in '<key>com.apple.security.get-task-allow</key><true/>' \
	'<key>com.apple.security.cs.disable-library-validation</key><true/>' \
	'<key>com.apple.security.network.client</key><true/>' \
	'<key>com.apple.security.cs.debugger</key><false/>'; do
	printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><dict>%s</dict></plist>\n' "$body" >"$work/entitlements.plist"
	refuses_with "entitlement $body" 'exactly an empty dictionary' check-entitlements "$work/entitlements.plist"
done
printf 'not a plist' >"$work/entitlements.plist"
refuses_with 'unreadable entitlements' 'exactly an empty dictionary' check-entitlements "$work/entitlements.plist"
: >"$work/entitlements.plist"
refuses_with 'empty entitlements file' 'exactly an empty dictionary' check-entitlements "$work/entitlements.plist"
printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><array/></plist>\n' >"$work/entitlements.plist"
refuses_with 'non-dictionary entitlements' 'exactly an empty dictionary' check-entitlements "$work/entitlements.plist"
printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><dict/></plist>\n' >"$work/entitlements.plist"
"$script" check-entitlements "$work/entitlements.plist" >/dev/null || fail 'an empty dictionary was refused'
refuses 'missing entitlements file' check-entitlements "$work/absent.plist"

# Signing refuses any bundle member that is not the plist and the executable, a
# symlink included. The check runs before any credential or Keychain work.
good=$work/good/Hikyo.app
mkdir -p "$good/Contents/MacOS"
: >"$good/Contents/Info.plist"
: >"$good/Contents/MacOS/hikyo-desktop"
"$script" check-bundle "$good" >/dev/null || fail 'a well-formed bundle was refused'
for mutation in symlink extra-file missing-executable symlinked-plist; do
	bundle=$work/$mutation/Hikyo.app
	mkdir -p "$work/$mutation"
	cp -R "$good" "$bundle"
	case $mutation in
		symlink) ln -s hikyo-desktop "$bundle/Contents/MacOS/alias" ;;
		extra-file) : >"$bundle/Contents/Resources" ;;
		missing-executable) rm "$bundle/Contents/MacOS/hikyo-desktop" ;;
		symlinked-plist) rm "$bundle/Contents/Info.plist"; ln -s MacOS/hikyo-desktop "$bundle/Contents/Info.plist" ;;
	esac
	refuses_with "bundle with $mutation" 'unexpected files in the app bundle' check-bundle "$bundle"
done
ln -s "$good" "$work/linked.app"
refuses_with 'symlinked bundle' 'app bundle required' check-bundle "$work/linked.app"

# check_app also runs as an if condition, where set -e is off: each mandatory
# check must return its own failure.
# shellcheck disable=SC2016 # literal source text to find, not expansions
for check in 'codesign --verify' 'codesign -dvv' 'check_entitlements "$scratch' 'stapler validate "$1"'; do
	awk -v c="$check" '/^\t\tcheck_app\(\) \{/{f=1} f&&index($0,c){found=1; if(index($0,"|| return 1"))ok=1} f&&/^\t\t\}/{exit} END{exit !(found&&ok)}' "$script" ||
		fail "check_app must return the failure of: $check"
done
# sign-app checks the bundle before any credential is imported or used.
awk '/^\tsign-app\)/{f=1} f&&/check_bundle "\$app"/{if(!i)c=1} f&&/import_identity/{i=1} f&&/;;/{exit} END{exit !(c&&i)}' "$script" ||
	fail 'sign-app must check the bundle before importing the signing identity'
# The release script never re-introduces the ad-hoc, deep signing the Taskfile uses.
if grep -E -e '--deep|--sign -( |$)' "$script" | grep -v '^[[:space:]]*#' >/dev/null; then fail 'script signs ad hoc or with --deep'; fi
printf 'desktop app fixture: stable-only, argument, credential and entitlement refusals hold\n'

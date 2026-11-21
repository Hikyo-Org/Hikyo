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

# Entitlements are an allowlist: exactly an empty dictionary. The check reads
# them with plutil, so it runs on macOS only (as does signing).
if [ "$(uname -s)" = Darwin ]; then
	"$script" check-entitlements "$repo_root/desktop/build/darwin/entitlements.plist" >/dev/null || fail 'committed entitlements refused'
	plutil -lint "$repo_root/desktop/build/darwin/entitlements.plist" >/dev/null || fail 'entitlements are not a valid plist'
	for body in '<key>com.apple.security.get-task-allow</key><true/>' \
		'<key>com.apple.security.cs.disable-library-validation</key><true/>' \
		'<key>com.apple.security.network.client</key><true/>' \
		'<key>com.apple.security.cs.debugger</key><false/>'; do
		printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><dict>%s</dict></plist>\n' "$body" >"$work/entitlements.plist"
		refuses_with "entitlement $body" 'exactly an empty dictionary' check-entitlements "$work/entitlements.plist"
	done
	printf 'not a plist' >"$work/entitlements.plist"
	refuses_with 'unreadable entitlements' 'exactly an empty dictionary' check-entitlements "$work/entitlements.plist"
	printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><array/></plist>\n' >"$work/entitlements.plist"
	refuses_with 'non-dictionary entitlements' 'exactly an empty dictionary' check-entitlements "$work/entitlements.plist"
fi
refuses 'missing entitlements file' check-entitlements "$work/absent.plist"

# Signing refuses any bundle member that is not the plist and the executable, a
# symlink included, before any Keychain work (dummy identity, no credentials).
if [ "$(uname -s)" = Darwin ]; then
	bundle=$work/extra/Hikyo.app
	mkdir -p "$bundle/Contents/MacOS"
	: >"$bundle/Contents/Info.plist"
	: >"$bundle/Contents/MacOS/hikyo-desktop"
	ln -s hikyo-desktop "$bundle/Contents/MacOS/alias"
	APPLE_SIGNING_IDENTITY='Developer ID Application: Test (ABCDE12345)' APPLE_TEAM_ID=ABCDE12345 \
		APPLE_DEVELOPER_ID_P12_BASE64=dummy APPLE_DEVELOPER_ID_P12_PASSWORD=dummy \
		"$script" sign-app "$bundle" >"$work/out" 2>&1 && fail 'bundle with a symlink accepted'
	grep -F 'unexpected files in the app bundle' "$work/out" >/dev/null || fail "symlink not refused for its bundle: $(cat "$work/out")"
fi

# The release script never re-introduces the ad-hoc, deep signing the Taskfile uses.
if grep -E -e '--deep|--sign -( |$)' "$script" | grep -v '^[[:space:]]*#' >/dev/null; then fail 'script signs ad hoc or with --deep'; fi
printf 'desktop app fixture: stable-only, argument, credential and entitlement refusals hold\n'

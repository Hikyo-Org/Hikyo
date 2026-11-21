#!/bin/sh
# Build, sign, notarize and package the desktop app for one architecture. Each
# subcommand needs only its own credentials, so the workflow gives a step the
# signing identity or the notary key, never both.
set +x
set -eu
umask 077

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

fail() { printf 'desktop app: %s\n' "$*" >&2; exit 1; }
usage() {
	printf '%s\n' \
		'usage: build-desktop-app.sh build VERSION ARCH APP' \
		'       build-desktop-app.sh sign-app APP' \
		'       build-desktop-app.sh notarize APP_OR_DMG' \
		'       build-desktop-app.sh image VERSION ARCH APP DMG' \
		'       build-desktop-app.sh sign-image DMG' \
		'       build-desktop-app.sh verify VERSION ARCH APP DMG' \
		'       build-desktop-app.sh check-bundle APP' \
		'       build-desktop-app.sh check-entitlements PLIST' >&2
	exit 2
}

entitlements=$repo_root/desktop/build/darwin/entitlements.plist
# Allowlist: the app asks for no entitlement at all, so the signed set must be
# exactly an empty dictionary. Anything else, or anything unparseable, fails.
# plistlib keeps this check portable; only reading the signed set needs codesign.
check_entitlements() {
	[ -f "$1" ] && [ ! -L "$1" ] || fail 'entitlements file is not a regular file'
	python3 -I -c '
import plistlib, sys
with open(sys.argv[1], "rb") as f:
    value = plistlib.load(f)
sys.exit(0 if isinstance(value, dict) and not value else 1)
' "$1" 2>/dev/null || fail 'entitlements must be exactly an empty dictionary'
}

# The bundle has one executable and no nested code. Refuse anything else (symlinks
# and other non-directory entries included), so signing without --deep can never
# leave a nested binary unsigned.
check_bundle() {
	[ -d "$1" ] && [ ! -L "$1" ] || fail 'app bundle required'
	new_scratch
	find "$1" ! -type d | LC_ALL=C sort >"$scratch/files"
	printf '%s\n%s\n' "$1/Contents/Info.plist" "$1/Contents/MacOS/$app_executable" | LC_ALL=C sort >"$scratch/expected"
	cmp -s "$scratch/files" "$scratch/expected" || fail 'unexpected files in the app bundle'
	[ -f "$1/Contents/Info.plist" ] && [ ! -L "$1/Contents/Info.plist" ] && [ -f "$1/Contents/MacOS/$app_executable" ] &&
		[ ! -L "$1/Contents/MacOS/$app_executable" ] || fail 'unexpected files in the app bundle'
}

# The desktop app ships in the stable channel only: never a prerelease or nightly.
require_stable() {
	is_semver "$1" || fail 'invalid version'
	[ "$("$script_dir/release-channel.sh" "v$1")" = stable ] || fail 'the desktop app is released for stable versions only'
}

require_arch() {
	case "$1" in arm64 | x86_64) ;; *) fail 'architecture must be arm64 or x86_64' ;; esac
}

require_macos() { [ "$(uname -s)" = Darwin ] || fail 'macOS is required'; }

require_env() {
	for name do
		[ -n "$(printenv "$name" 2>/dev/null || true)" ] || fail "missing $name"
	done
}

require_identity() {
	require_env APPLE_SIGNING_IDENTITY APPLE_TEAM_ID
	printf '%s\n' "$APPLE_TEAM_ID" | grep -Eq '^[A-Z0-9]{10}$' || fail 'invalid team ID'
	case "$APPLE_SIGNING_IDENTITY" in
		"Developer ID Application: "*" ($APPLE_TEAM_ID)") ;;
		*) fail 'identity must be Developer ID Application for the configured team' ;;
	esac
	identity_requirement=$(printf '%s' "$APPLE_SIGNING_IDENTITY" | sed 's/\\/\\\\/g; s/"/\\"/g')
	requirement="=anchor apple generic and certificate leaf[subject.OU] = \"$APPLE_TEAM_ID\" and certificate leaf[subject.CN] = \"$identity_requirement\" and certificate leaf[field.1.2.840.113635.100.6.1.13] exists"
}

scratch=
keychain=
search_list_saved=false
mounted=
restore_keychain() {
	restore_status=0
	if [ "$search_list_saved" = true ]; then
		# security prints one quoted path per line; read them without eval.
		set --
		while IFS= read -r entry; do
			[ -n "$entry" ] || continue
			set -- "$@" "$entry"
		done <"$scratch/keychain-search-list" || restore_status=1
		if security list-keychains -d user -s "$@"; then search_list_saved=false; else restore_status=1; fi
	fi
	if [ -n "$keychain" ] && [ -f "$keychain" ]; then
		security delete-keychain "$keychain" >/dev/null || restore_status=1
	fi
	return "$restore_status"
}
cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if [ -n "$mounted" ]; then
		if hdiutil detach "$mounted" >/dev/null; then mounted=; else status=1; fi
	fi
	restore_keychain || status=1
	# Never recursively remove a filesystem that is still attached.
	if [ -z "$mounted" ] && [ -n "$scratch" ]; then rm -rf "$scratch" || status=1; fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

new_scratch() {
	scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-desktop.XXXXXX")
	scratch=$(CDPATH='' cd -P -- "$scratch" && pwd)
}

import_identity() {
	require_env APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD
	keychain=$scratch/signing.keychain-db
	printf '%s' "$APPLE_DEVELOPER_ID_P12_BASE64" | base64 --decode >"$scratch/signing.p12"
	keychain_password=$(openssl rand -hex 32)
	security list-keychains -d user >"$scratch/keychain-search-list.raw" || fail 'cannot read user Keychain search list'
	sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//' "$scratch/keychain-search-list.raw" >"$scratch/keychain-search-list"
	security create-keychain -p "$keychain_password" "$keychain"
	security set-keychain-settings -lut 21600 "$keychain"
	security unlock-keychain -p "$keychain_password" "$keychain"
	security import "$scratch/signing.p12" -k "$keychain" -P "$APPLE_DEVELOPER_ID_P12_PASSWORD" \
		-T /usr/bin/codesign >/dev/null || fail 'cannot import Developer ID identity'
	security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$keychain_password" "$keychain" >/dev/null
	# Certificate-chain lookup uses the user search list, not only --keychain.
	search_list_saved=true
	security list-keychains -d user -s "$keychain" || fail 'cannot register signing Keychain'
	security find-identity -v -p codesigning "$keychain" >"$scratch/identities" || fail 'cannot inspect signing identities'
	grep -F -- "\"$APPLE_SIGNING_IDENTITY\"" "$scratch/identities" >/dev/null ||
		fail 'configured Developer ID identity is not valid in the imported Keychain'
	rm "$scratch/signing.p12"
	unset APPLE_DEVELOPER_ID_P12_BASE64 APPLE_DEVELOPER_ID_P12_PASSWORD keychain_password
}

app_executable=hikyo-desktop
app_name=Hikyo.app
quarantine="0081;$(printf '%x' "$(date +%s)");HikyoDesktopQualification;"

cmd=${1:-}
[ "$#" -ge 1 ] || usage
shift
case "$cmd" in
	check-bundle)
		[ "$#" -eq 1 ] || usage
		check_bundle "$1"
		printf 'desktop app: bundle holds exactly its plist and executable\n'
		;;
	check-entitlements)
		[ "$#" -eq 1 ] || usage
		check_entitlements "$1"
		printf 'desktop app: entitlements are exactly an empty dictionary\n'
		;;
	build)
		[ "$#" -eq 3 ] || usage
		version=$1 arch=$2 app=$3
		require_stable "$version"
		require_arch "$arch"
		require_macos
		[ "$(basename "$app")" = "$app_name" ] || fail "app path must end in $app_name"
		[ ! -e "$app" ] && [ ! -L "$app" ] || fail 'app output already exists'
		goarch=amd64
		[ "$arch" != arm64 ] || goarch=arm64
		mkdir -p "$app/Contents/MacOS"
		# Absolute, so the go build below writes here and not under desktop/.
		app=$(CDPATH='' cd -- "$app" && pwd)
		# Plain go build: the Taskfile's ad-hoc codesign --deep must never reach a release.
		(
			cd "$repo_root/desktop"
			CGO_ENABLED=1 GOOS=darwin GOARCH=$goarch \
				CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 \
				go build -tags production -trimpath -buildvcs=false \
				-ldflags "-w -s -X main.version=$version" -o "$app/Contents/MacOS/$app_executable" .
		)
		[ "$(lipo -archs "$app/Contents/MacOS/$app_executable")" = "$arch" ] || fail 'executable architecture mismatch'
		cp "$repo_root/desktop/build/darwin/Info.plist" "$app/Contents/Info.plist"
		for key in CFBundleShortVersionString CFBundleVersion; do
			/usr/libexec/PlistBuddy -c "Set :$key $version" "$app/Contents/Info.plist"
		done
		plutil -lint "$app/Contents/Info.plist" >/dev/null
		[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$app/Contents/Info.plist")" = "$app_executable" ] ||
			fail 'Info.plist executable differs from the built binary'
		xattr -cr "$app"
		printf 'desktop app: built %s for %s (unsigned)\n' "$app" "$arch"
		;;
	sign-app)
		[ "$#" -eq 1 ] || usage
		app=$1
		require_macos
		require_identity
		check_entitlements "$entitlements"
		check_bundle "$app"
		import_identity
		codesign --force --sign "$APPLE_SIGNING_IDENTITY" --keychain "$keychain" \
			--options runtime --timestamp --entitlements "$entitlements" "$app"
		restore_keychain || fail 'cannot restore and delete signing Keychain'
		codesign --verify --strict --verbose=2 -R "$requirement" "$app"
		printf 'desktop app: signed %s with the hardened runtime\n' "$app"
		;;
	notarize)
		[ "$#" -eq 1 ] || usage
		target=$1
		require_macos
		require_env APPLE_NOTARY_KEY_P8 APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER_ID
		new_scratch
		submission=$target
		if [ -d "$target" ] && [ ! -L "$target" ]; then
			submission=$scratch/$(basename "$target").zip
			ditto -c -k --keepParent "$target" "$submission"
		elif [ ! -f "$target" ] || [ -L "$target" ]; then
			fail 'regular DMG or app bundle required'
		fi
		printf '%s' "$APPLE_NOTARY_KEY_P8" >"$scratch/notary.p8"
		unset APPLE_NOTARY_KEY_P8
		notary() {
			xcrun notarytool "$@" --key "$scratch/notary.p8" --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID"
		}
		# A failed run is recovered by rerunning the failed jobs, which submits again.
		notary submit "$submission" --wait --timeout "${APPLE_NOTARY_TIMEOUT:-30m}" --output-format json >"$scratch/result.json" ||
			fail 'notarization submission failed'
		id=$(jq -er '.id' "$scratch/result.json")
		if [ -n "${APPLE_NOTARY_EVIDENCE_DIR:-}" ]; then
			mkdir -p "$APPLE_NOTARY_EVIDENCE_DIR"
			cp "$scratch/result.json" "$APPLE_NOTARY_EVIDENCE_DIR/$(basename "$target")-submission.json"
			notary log "$id" "$APPLE_NOTARY_EVIDENCE_DIR/$(basename "$target")-log.json" >/dev/null 2>&1 || true
		fi
		[ "$(jq -er '.status' "$scratch/result.json")" = Accepted ] || fail "notarization was not accepted (submission $id)"
		xcrun stapler staple "$target"
		xcrun stapler validate "$target"
		printf 'desktop app: notarized and stapled %s\n' "$(basename "$target")"
		;;
	image)
		[ "$#" -eq 4 ] || usage
		version=$1 arch=$2 app=$3 dmg=$4
		require_stable "$version"
		require_arch "$arch"
		require_macos
		[ "$(basename "$dmg")" = "Hikyo-Desktop-$version-$arch.dmg" ] || fail 'disk image name must be Hikyo-Desktop-VERSION-ARCH.dmg'
		[ ! -e "$dmg" ] && [ ! -L "$dmg" ] || fail 'disk image output already exists'
		[ -d "$app" ] && [ ! -L "$app" ] || fail 'app bundle required'
		mkdir -p "$(dirname "$dmg")"
		new_scratch
		mkdir "$scratch/stage"
		ditto "$app" "$scratch/stage/$app_name"
		ln -s /Applications "$scratch/stage/Applications"
		attempt=1
		printf 'hdiutil: create failed - Resource busy\n' >"$scratch/busy.err"
		while :; do
			if hdiutil create -volname "Hikyo Desktop $version" -srcfolder "$scratch/stage" \
				-nospotlight -fs HFS+ -format UDZO "$dmg" >/dev/null 2>"$scratch/create.err"; then
				break
			else
				code=$?
			fi
			cat "$scratch/create.err" >&2
			if [ "$attempt" -ge 3 ] || ! cmp -s "$scratch/busy.err" "$scratch/create.err"; then exit "$code"; fi
			rm -f "$dmg"
			sleep 2
			attempt=$((attempt + 1))
		done
		printf 'desktop app: created %s\n' "$(basename "$dmg")"
		;;
	sign-image)
		[ "$#" -eq 1 ] || usage
		dmg=$1
		require_macos
		require_identity
		[ -f "$dmg" ] && [ ! -L "$dmg" ] || fail 'regular DMG required'
		new_scratch
		import_identity
		codesign --force --sign "$APPLE_SIGNING_IDENTITY" --keychain "$keychain" \
			--identifier app.hikyo.desktop.dmg --timestamp "$dmg"
		restore_keychain || fail 'cannot restore and delete signing Keychain'
		codesign --verify --strict -R "$requirement" "$dmg"
		printf 'desktop app: signed %s\n' "$(basename "$dmg")"
		;;
	verify)
		[ "$#" -eq 4 ] || usage
		version=$1 arch=$2 app=$3 dmg=$4
		require_stable "$version"
		require_arch "$arch"
		require_macos
		require_identity
		[ -d "$app" ] && [ ! -L "$app" ] && [ -f "$dmg" ] && [ ! -L "$dmg" ] || fail 'app bundle and DMG required'
		new_scratch
		# check_app also runs as an if condition, where set -e does not apply, so
		# every mandatory check returns its failure explicitly.
		check_app() {
			codesign --verify --strict --verbose=2 -R "$requirement" "$1" || return 1
			codesign -dvv "$1" >"$scratch/describe" 2>&1 || return 1
			grep -E 'flags=0x[0-9a-f]+\(.*runtime' "$scratch/describe" >/dev/null || fail 'hardened runtime flag is absent'
			if grep -F 'Signature=adhoc' "$scratch/describe" >/dev/null; then fail 'ad-hoc signature'; fi
			codesign -d --entitlements - --xml "$1" >"$scratch/entitlements.xml" 2>/dev/null || fail 'cannot read the signed entitlements'
			check_entitlements "$scratch/entitlements.xml" || return 1
			xcrun stapler validate "$1" || return 1
			[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$1/Contents/Info.plist")" = "$version" ] ||
				fail 'bundle version differs'
			[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$1/Contents/Info.plist")" = "$version" ] ||
				fail 'bundle build version differs'
			[ "$(lipo -archs "$1/Contents/MacOS/$app_executable")" = "$arch" ] || fail 'executable architecture differs'
		}
		check_app "$app"
		spctl --assess --type execute --verbose=4 "$app"
		xcrun stapler validate "$dmg"
		codesign --verify --strict -R "$requirement" "$dmg"
		# Assess a quarantined copy so the uploaded bytes stay untouched. A DMG is
		# assessed as a primary-signature open, not as an installer package.
		cp "$dmg" "$scratch/assess.dmg"
		xattr -w com.apple.quarantine "$quarantine" "$scratch/assess.dmg"
		spctl --assess --type open --context context:primary-signature --verbose=2 "$scratch/assess.dmg"
		mkdir "$scratch/mount"
		hdiutil attach -readonly -nobrowse -mountpoint "$scratch/mount" "$dmg" >/dev/null
		mounted=$scratch/mount
		if ditto "$scratch/mount/$app_name" "$scratch/installed.app" && check_app "$scratch/installed.app"; then
			result=0
		else
			result=1
		fi
		hdiutil detach "$scratch/mount" >/dev/null || fail 'cannot detach verification mount'
		mounted=
		[ "$result" -eq 0 ] || fail 'application inside the disk image failed verification'
		printf 'desktop app: %s %s verified (signature, hardened runtime, ticket, Gatekeeper)\n' "$version" "$arch"
		;;
	*) usage ;;
esac

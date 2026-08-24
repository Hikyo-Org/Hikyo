#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
identity="$script_dir/package-identity.sh"

assert_identity() {
	version=$1
	format=$2
	arch=$3
	expected=$4
	actual=$($identity "$version" "$format" "$arch")
	[ "$actual" = "$expected" ] || {
		printf 'package identity fixture: %s %s %s expected %s, got %s\n' \
			"$version" "$format" "$arch" "$expected" "$actual" >&2
		exit 1
	}
}

tab=$(printf '\t')
assert_identity 1.2.3 deb amd64 "hikyo_1.2.3_amd64.deb${tab}0:1.2.3${tab}amd64"
assert_identity 1.2.3 rpm arm64 "hikyo-1.2.3-1.aarch64.rpm${tab}0:1.2.3-1${tab}aarch64"
assert_identity 1.2.3 apk amd64 "hikyo_1.2.3_x86_64.apk${tab}1.2.3${tab}x86_64"
assert_identity 1.2.3 archlinux arm64 "hikyo-1.2.3-1-aarch64.pkg.tar.zst${tab}0:1.2.3-1${tab}aarch64"

assert_identity 1.2.3-rc.1 deb arm64 "hikyo_1.2.3_rc.1_arm64.deb${tab}0:1.2.3~rc.1${tab}arm64"
assert_identity 1.2.3-rc-1 rpm amd64 "hikyo-1.2.3_rc_1-1.x86_64.rpm${tab}0:1.2.3~rc_1-1${tab}x86_64"
assert_identity 1.2.3-rc.1 apk arm64 "hikyo_1.2.3_rc.1_aarch64.apk${tab}1.2.3_rc.1${tab}aarch64"
assert_identity 1.2.3-rc-1 archlinux amd64 "hikyo-1.2.3rc_1-1-x86_64.pkg.tar.zst${tab}0:1.2.3rc_1-1${tab}x86_64"

if "$identity" 1.2.3+build.1 deb amd64 >/dev/null 2>&1; then
	printf 'package identity fixture: build metadata unexpectedly accepted\n' >&2
	exit 1
fi
if "$identity" 1.2.3-4 archlinux amd64 >/dev/null 2>&1; then
	printf 'package identity fixture: collision-prone numeric prerelease unexpectedly accepted\n' >&2
	exit 1
fi

printf 'package identity fixture: exact filenames and native metadata are version-bound\n'

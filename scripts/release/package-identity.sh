#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

[ "$#" -eq 3 ] || {
	printf 'usage: package-identity.sh VERSION FORMAT ARCH\n' >&2
	exit 1
}

version=$1
format=$2
arch=$3
filename=$(package_file_name "$version" "$format" "$arch") || {
	printf 'package identity: unsupported version, format, or architecture\n' >&2
	exit 1
}
metadata_version=$(package_metadata_version "$version" "$format") || {
	printf 'package identity: unsupported version or format\n' >&2
	exit 1
}
native_arch=$(package_native_arch "$format" "$arch") || {
	printf 'package identity: unsupported format or architecture\n' >&2
	exit 1
}

printf '%s\t%s\t%s\n' "$filename" "$metadata_version" "$native_arch"

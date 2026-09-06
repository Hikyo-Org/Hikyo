#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-manifest-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
version=0.0.1-nightly.20260906.24.g90b4ca6a
commit=90b4ca6a5d22438e751cf9af83aa4fd077a6a61c
mkdir "$scratch/payloads"
for os in Linux Darwin Windows; do
 for arch in x86_64 arm64; do
  extension=tar.gz
  [ "$os" != Windows ] || extension=zip
  printf 'fixture archive\n' >"$scratch/payloads/hikyo_${version}_${os}_${arch}.$extension"
 done
done
for format in deb rpm apk archlinux; do
 for arch in amd64 arm64; do
  printf 'fixture package\n' >"$scratch/payloads/$(package_file_name "$version" "$format" "$arch")"
 done
done
for name in checksums.txt binary-provenance.json nightly-policy.json sigstore-trusted-root.json NIGHTLY-BUILD.txt; do
 printf 'fixture public bytes\n' >"$scratch/payloads/$name"
done
jq -n --arg version "$version" --arg commit "$commit" '{schema:"hikyo.dev/upgrade-compatibility/v1",profile:"nightly/v1",version:$version,commit:$commit,sequence:24}' >"$scratch/payloads/upgrade-compatibility.json"
printf 'unbound\n' >"$scratch/payloads/extra.exe"
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'extra executable accepted\n' >&2; exit 1
fi
rm "$scratch/payloads/extra.exe"
mv "$scratch/payloads/checksums.txt" "$scratch/checksums.txt"
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'missing payload accepted\n' >&2; exit 1
fi
mv "$scratch/checksums.txt" "$scratch/payloads/checksums.txt"
"$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads"
jq -e '(.artifacts | length) == 20 and ([.artifacts[] | select(.kind == "binary")] | length) == 6 and ([.artifacts[] | select(.kind == "package")] | length) == 8' "$scratch/payloads/release-manifest.json" >/dev/null
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'existing manifest overwritten\n' >&2; exit 1
fi
printf 'nightly manifest: complete inventory bound; extra, missing and overwrite refused\n'

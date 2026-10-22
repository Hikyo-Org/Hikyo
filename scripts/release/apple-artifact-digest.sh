#!/usr/bin/env bash
# Bind the exact signed macOS tarball/DMG pair for both release architectures.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
[ "$#" -eq 2 ] || { printf 'usage: apple-artifact-digest.sh DIST VERSION\n' >&2; exit 2; }
is_semver "$2" || { printf 'invalid Apple artifact version\n' >&2; exit 1; }
cd -- "$1"
files=()
for arch in arm64 x86_64; do
  for extension in tar.gz dmg; do
    name=hikyo_${2}_Darwin_$arch.$extension
    [ -f "$name" ] && [ ! -L "$name" ] || { printf 'missing regular Apple artifact %s\n' "$name" >&2; exit 1; }
    files+=("$name")
  done
done
digest=$(shasum -a 256 "${files[@]}" | LC_ALL=C sort | shasum -a 256)
printf '%s\n' "${digest%% *}"

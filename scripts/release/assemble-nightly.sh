#!/bin/sh
set -eu
[ "$#" -ge 3 ] || { printf 'usage: %s TRUST OUTPUT NIGHTLY_DIRECTORY...\n' "$0" >&2; exit 2; }
trust=$1 output=$2
shift 2
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-assembly.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/snapshot" "$scratch/keys"
for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json; do
 cp "$trust/$name" "$scratch/snapshot/$name"
done
jq -r '.primary_keys[].public_key' "$trust/metadata.json" >"$scratch/key-names"
while IFS= read -r name; do
 case "$name" in '' | *..* | */* | *\\*) printf 'unsafe key name\n' >&2; exit 1 ;; esac
 cp "$trust/$name" "$scratch/keys/$name"
done <"$scratch/key-names"
# Build argv without eval or shell interpolation of metadata.
for directory in "$@"; do
 set -- "$@" --nightly "$directory"
 shift
done
recovery=$(jq -r '.recovery.public_key' "$trust/root.json")
case "$recovery" in '' | *..* | */* | *\\*) printf 'unsafe recovery key name\n' >&2; exit 1 ;; esac
for digest in $(jq -r '.bridges[]' "$trust/catalog.json"); do
 printf '%s\n' "$digest" | grep -Eq '^[0-9a-f]{64}$'
 set -- "$@" --bridge "$trust/bridges/$digest"
done
go run ./scripts/release/assemble-upgrade \
 --root "$trust/root.json" --recovery-key "$trust/$recovery" \
 --snapshot "$scratch/snapshot" --keys "$scratch/keys" --out "$output" "$@"

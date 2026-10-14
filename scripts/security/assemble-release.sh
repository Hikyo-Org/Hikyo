#!/usr/bin/env bash
# Existing authenticated upgrade assembly, with only stable public proof inputs.
set -euo pipefail
[[ $# == 3 ]] || { echo 'usage: assemble-release.sh TRUST DOWNLOAD OUTPUT' >&2; exit 2; }
trust=$1 download=$2 output=$3
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-security-assembly.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/snapshot" "$scratch/keys" "$scratch/release"
for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json stable-policy.json stable-policy.sigstore.json stable-trusted-root.json; do
  cp "$trust/$name" "$scratch/snapshot/$name"
done
while IFS= read -r name; do
  [[ "$name" =~ ^[A-Za-z0-9_-][A-Za-z0-9_.-]*$ && "$name" != *..* ]]
  cp "$trust/$name" "$scratch/keys/$name"
done < <(jq -r '.primary_keys[].public_key' "$trust/metadata.json")
for name in release-manifest.json release-manifest.sigstore.json release-candidate.json upgrade-compatibility.json; do
  cp "$download/$name" "$scratch/release/$name"
done
if [ -f "$download/build-provenance.json" ]; then
  cp "$download/build-provenance.json" "$download/build-provenance.json.sigstore.json" "$scratch/release/"
fi
recovery=$(jq -r '.recovery.public_key' "$trust/root.json")
[[ "$recovery" =~ ^[A-Za-z0-9_-][A-Za-z0-9_.-]*$ && "$recovery" != *..* ]]
bridges=()
while IFS= read -r digest; do
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]]
  bridges+=(--bridge "$trust/bridges/$digest")
done < <(jq -r '.bridges[]' "$trust/catalog.json")
go run ./scripts/release/assemble-upgrade --root "$trust/root.json" \
  --recovery-key "$trust/$recovery" --snapshot "$scratch/snapshot" \
  --keys "$scratch/keys" --release "$scratch/release" --out "$output" "${bridges[@]}"

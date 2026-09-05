#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-catalog-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/policies" "$scratch/bridges"
printf '{"fixture":"metadata"}\n' >"$scratch/metadata.json"
printf '{"fixture":"policy"}\n' >"$scratch/policies/policy.json"
printf '{"fixture":"bridge"}\n' >"$scratch/bridges/bridge.json"
"$script_dir/create-upgrade-catalog.sh" "$scratch/metadata.json" 1 "$scratch/policies" "$scratch/bridges" "$scratch/catalog.json" >/dev/null
metadata_sha=$(shasum -a 256 "$scratch/metadata.json" | awk '{print $1}')
policy_sha=$(shasum -a 256 "$scratch/policies/policy.json" | awk '{print $1}')
bridge_sha=$(shasum -a 256 "$scratch/bridges/bridge.json" | awk '{print $1}')
jq -e --arg metadata "$metadata_sha" --arg policy "$policy_sha" --arg bridge "$bridge_sha" '
 .schema == "hikyo.dev/upgrade-trust/v1" and .sequence == 1 and
 .stable_metadata_sha256 == $metadata and .nightly_policies == [$policy] and .bridges == [$bridge]
' "$scratch/catalog.json" >/dev/null
if "$script_dir/create-upgrade-catalog.sh" "$scratch/metadata.json" 2 "$scratch/policies" "$scratch/bridges" "$scratch/catalog.json" >/dev/null 2>&1; then
 printf 'catalog fixture: existing output overwritten\n' >&2; exit 1
fi
ln -s "$scratch/missing" "$scratch/policies/dangling.json"
if "$script_dir/create-upgrade-catalog.sh" "$scratch/metadata.json" 2 "$scratch/policies" "$scratch/bridges" "$scratch/rejected.json" >/dev/null 2>&1; then
 printf 'catalog fixture: dangling evidence ignored\n' >&2; exit 1
fi
printf 'catalog fixture: exact inventory bound; overwrite and symlink refused\n'

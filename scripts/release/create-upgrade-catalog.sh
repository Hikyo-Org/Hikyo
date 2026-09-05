#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

if [ "$#" -ne 5 ]; then
	printf 'usage: %s METADATA SEQUENCE POLICY_DIRECTORY BRIDGE_DIRECTORY OUTPUT\n' "$0" >&2
	exit 2
fi
metadata=$1
sequence=$2
policies=$3
bridges=$4
output=$5
case "$sequence" in '' | *[!0-9]* | 0) printf 'catalog: positive sequence required\n' >&2; exit 2 ;; esac
jq -en --arg sequence "$sequence" '$sequence | tonumber | . >= 1 and . <= 9007199254740991' >/dev/null || exit 2
[ -f "$metadata" ] && [ -d "$policies" ] && [ -d "$bridges" ] || exit 2
[ ! -e "$output" ] || { printf 'catalog: output already exists\n' >&2; exit 1; }
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-upgrade-catalog.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
for kind in policy bridge; do
	if [ "$kind" = policy ]; then directory=$policies; limit=256; else directory=$bridges; limit=1024; fi
	: >"$scratch/$kind.digests"
	count=0
	for path in "$directory"/*; do
		[ -e "$path" ] || [ -L "$path" ] || continue
		[ ! -L "$path" ] && [ -f "$path" ] || { printf 'catalog: only regular evidence files permitted\n' >&2; exit 1; }
		case "$path" in *.sigstore.json) continue ;; *.json) ;; *) printf 'catalog: unrecognized evidence file\n' >&2; exit 1 ;; esac
		count=$((count + 1))
		[ "$count" -le "$limit" ] || { printf 'catalog: evidence bound exceeded\n' >&2; exit 1; }
		sha256_file "$path" >>"$scratch/$kind.digests"
	done
	LC_ALL=C sort "$scratch/$kind.digests" >"$scratch/$kind.sorted"
	[ "$(uniq "$scratch/$kind.sorted" | wc -l | tr -d ' ')" = "$count" ] || { printf 'catalog: duplicate evidence digest\n' >&2; exit 1; }
done
jq -n --argjson sequence "$sequence" --arg metadata "$(sha256_file "$metadata")" \
	--rawfile policies "$scratch/policy.sorted" --rawfile bridges "$scratch/bridge.sorted" '
	{schema:"hikyo.dev/upgrade-trust/v1", sequence:$sequence,stable_metadata_sha256:$metadata,
	nightly_policies:($policies | split("\n") | map(select(length > 0))),
	bridges:($bridges | split("\n") | map(select(length > 0)))}
' >"$scratch/catalog.json"
# Non-overwrite publication; root signing is a separate offline ceremony.
ln "$scratch/catalog.json" "$output"
printf 'catalog: wrote unsigned review artifact %s; recovery signature required\n' "$output"

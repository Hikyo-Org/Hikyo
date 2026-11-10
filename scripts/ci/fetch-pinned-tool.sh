#!/usr/bin/env bash
# Fetch one checksum-pinned file described by pinned-tools/<name>.pin.
#
#   fetch-pinned-tool.sh <name> <output>
#   fetch-pinned-tool.sh --prefetch <name>
#
# The pin file is read next to this script, so a caller that runs a trusted copy
# of the script also reads the trusted pin. A cached copy (PINNED_TOOL_CACHE,
# default ~/.cache/hikyo-pinned-tools) is used only after its SHA-256 matches;
# a missing or corrupt copy is downloaded with bounded exponential backoff and
# must match too. Nothing unverified is cached or written to <output>.
set -euo pipefail

die() {
	printf 'fetch-pinned-tool: %s\n' "$1" >&2
	exit 1
}

prefetch=false
if [[ ${1:-} == --prefetch ]]; then
	prefetch=true
	shift
fi
if [[ $prefetch == true ]]; then
	[[ $# -eq 1 ]] || die 'usage: fetch-pinned-tool.sh --prefetch <name>'
else
	[[ $# -eq 2 ]] || die 'usage: fetch-pinned-tool.sh <name> <output>'
fi
name=$1
output=${2:-}
[[ $name =~ ^[a-z0-9][a-z0-9._-]*$ ]] || die "invalid tool name: $name"

pin="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/pinned-tools/$name.pin"
[[ -f $pin ]] || die "no pin file for $name"
# Exactly two lines, url= then sha256=, nothing else.
# read -d '' consumes the rest of the file, so trailing text without a newline is refused too.
{ IFS= read -r url_line && IFS= read -r sha_line && ! IFS= read -r -d '' trailing && [[ -z $trailing ]]; } <"$pin" ||
	die "malformed pin file for $name"
[[ $url_line =~ ^url=(https://[^[:space:]]+)$ ]] || die "pin url for $name must be an https URL"
url=${BASH_REMATCH[1]}
[[ $sha_line =~ ^sha256=([0-9a-f]{64})$ ]] || die "pin sha256 for $name must be 64 lowercase hex digits"
sha=${BASH_REMATCH[1]}

# Checked once up front: a die inside $(sha256_of) would only end the subshell and
# an empty digest would be taken for a mismatch, deleting a good cached file.
command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 ||
	die 'no SHA-256 tool (sha256sum or shasum) found'

sha256_of() {
	local digest
	if command -v sha256sum >/dev/null 2>&1; then
		digest=$(sha256sum "$1")
	else
		digest=$(shasum -a 256 "$1")
	fi
	printf '%s' "${digest%% *}"
}

deliver() {
	[[ $prefetch == true ]] || cp "$1" "$output"
}

dir="${PINNED_TOOL_CACHE:-$HOME/.cache/hikyo-pinned-tools}/$name"
cached="$dir/$sha"
mkdir -p "$dir"

if [[ -f $cached ]]; then
	if [[ $(sha256_of "$cached") == "$sha" ]]; then
		printf 'fetch-pinned-tool: verified cached %s\n' "$name" >&2
		deliver "$cached"
		exit 0
	fi
	printf 'fetch-pinned-tool: cached %s failed its checksum; discarding and downloading\n' "$name" >&2
	rm -f "$cached"
fi

tmp=$(mktemp "$dir/.download.XXXXXX")
trap 'rm -f "$tmp"' EXIT
# No --retry-delay: curl then doubles its wait (1s, 2s, 4s, ... up to 128s)
# and --retry-max-time bounds the whole retry window.
curl --fail --location --proto '=https' --tlsv1.2 --silent --show-error \
	--retry 8 --retry-all-errors --retry-max-time 300 \
	--connect-timeout 20 --max-time 120 \
	--output "$tmp" "$url"
[[ $(sha256_of "$tmp") == "$sha" ]] || die "downloaded $name does not match its pinned sha256"
mv -f "$tmp" "$cached"
printf 'fetch-pinned-tool: downloaded and verified %s\n' "$name" >&2
deliver "$cached"

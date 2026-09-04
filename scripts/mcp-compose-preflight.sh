#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
compose="$root/deploy/mcp/compose.yaml"

fail() {
	printf 'MCP Compose preflight: %s\n' "$1" >&2
	exit 1
}

require_regular_file() {
	label=$1
	path=$2
	if [ ! -f "$path" ] || [ -L "$path" ]; then
		fail "$label must name an existing regular file, not a symlink"
	fi
}

require_owner_only_file() {
	label=$1
	path=$2
	require_regular_file "$label" "$path"
	if mode=$(stat -f '%Lp' "$path" 2>/dev/null); then
		:
	else
		mode=$(stat -c '%a' "$path")
	fi
	case "$mode" in
		*00) ;;
		*) fail "$label must not grant group or other permissions" ;;
	esac
}

if ! printf '%s\n' "${HIKYO_IMAGE:-}" | grep -Eq '^[^@[:space:]]+@sha256:[0-9a-f]{64}$'; then
	fail "HIKYO_IMAGE must use an immutable @sha256 digest"
fi

require_owner_only_file HIKYO_ROOT_KEY_FILE "${HIKYO_ROOT_KEY_FILE:-}"
require_regular_file HIKYO_TLS_CERT_FILE "${HIKYO_TLS_CERT_FILE:-}"
require_owner_only_file HIKYO_TLS_KEY_FILE "${HIKYO_TLS_KEY_FILE:-}"

docker compose -f "$compose" config --quiet
printf 'MCP Compose preflight: immutable image, private keys, and Compose configuration passed\n'

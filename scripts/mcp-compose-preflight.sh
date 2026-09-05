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

for directory in "${HIKYO_UPGRADE_PUBLIC_DIR:-}" "${HIKYO_UPGRADE_INSTALLATION_DIR:-}"; do
	if [ ! -d "$directory" ] || [ -L "$directory" ]; then
		fail "upgrade public and installation paths must be existing directories, not symlinks"
	fi
done
require_regular_file operator.pub "$HIKYO_UPGRADE_PUBLIC_DIR/operator.pub"
require_regular_file bundle/index.json "$HIKYO_UPGRADE_PUBLIC_DIR/bundle/index.json"
# The gate authenticates the complete bundle and checks runtime ownership. This
# preflight rejects obvious unsafe custody before Docker can create bind paths.
if mode=$(stat -f '%Lp' "$HIKYO_UPGRADE_INSTALLATION_DIR" 2>/dev/null); then
	:
else
	mode=$(stat -c '%a' "$HIKYO_UPGRADE_INSTALLATION_DIR")
fi
[ "$mode" = 700 ] || fail "installation custody directory must have mode 0700"

docker compose -f "$compose" config --quiet
printf 'MCP Compose preflight: immutable image, private keys, upgrade paths, and Compose configuration passed\n'

#!/bin/sh
set -eu

# The 2026-07-28 requirement set first appeared on this exact conformance
# prerelease. Keep the version literal here so an upstream tag cannot change
# the protocol gate underneath a Hikyo pull request.
addr=127.0.0.1:18080
repo_root=$(CDPATH='' cd "$(dirname "$0")/../.." && pwd)
tool_dir=$repo_root/scripts/mcp-conformance
baseline=$repo_root/scripts/ci/mcp-conformance-baseline.yml
log_file=$(mktemp "${TMPDIR:-/tmp}/hikyo-mcp-conformance.XXXXXX")
inspector_config=$(mktemp "${TMPDIR:-/tmp}/hikyo-mcp-inspector.XXXXXX")
server_binary=$(mktemp "${TMPDIR:-/tmp}/hikyo-mcp-conformance-server.XXXXXX")
server_pid=

cleanup() {
	if [ -n "$server_pid" ]; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	rm -f "$log_file" "$inspector_config" "$server_binary"
}
trap cleanup EXIT HUP INT TERM

# Own the serving process directly: killing `go run` can leave its child alive.
cd "$repo_root"
go build -o "$server_binary" ./scripts/ci/mcp-conformance-server
HIKYO_MCP_CONFORMANCE_ADDR=$addr "$server_binary" >"$log_file" 2>&1 &
server_pid=$!

ready=false
attempt=0
while [ "$attempt" -lt 100 ]; do
	if curl --silent --output /dev/null "http://$addr/mcp"; then
		ready=true
		break
	fi
	if ! kill -0 "$server_pid" 2>/dev/null; then
		cat "$log_file" >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep 0.1
done
if [ "$ready" != true ]; then
	cat "$log_file" >&2
	printf 'MCP conformance fixture did not become ready\n' >&2
	exit 1
fi

# The Inspector smoke is intentionally unauthenticated and tenant-free. A
# production bearer belongs only in Inspector's transient header field; the
# operations runbook explains why it must not be saved in a catalog.
printf '{"mcpServers":{"hikyo":{"type":"streamable-http","url":"http://%s/mcp","protocolEra":"modern"}}}\n' "$addr" >"$inspector_config"
# Corepack resolves packageManager from its working directory before pnpm sees
# --dir. Enter the owning package so the registry's latest pnpm cannot win.
cd "$tool_dir"
pnpm exec mcp-inspector --cli \
	--config "$inspector_config" --server hikyo --method tools/list >/dev/null

for scenario in server-stateless tools-list caching; do
	pnpm exec conformance server \
		--url "http://$addr/mcp" \
		--scenario "$scenario" \
		--spec-version 2026-07-28 \
		--expected-failures "$baseline"
done

#!/bin/sh
# New local disposable runtime only. No remote deployment or publication.
set -eu
umask 077
if [ "$#" -ne 1 ]; then
  printf 'usage: scripts/acceptance/mcp-fixture/local.sh NEW_ABSOLUTE_RUNTIME\n' >&2
  exit 2
fi
case "$1" in /*) ;; *) printf 'runtime must be absolute\n' >&2; exit 2 ;; esac
repo=$(git rev-parse --show-toplevel)
source_dir=$repo/scripts/acceptance/mcp-fixture
runtime=$1
parent=$(dirname "$runtime")
physical_parent=$(CDPATH='' cd -P "$parent" && pwd -P) || exit 1
if [ "$parent" != "$physical_parent" ]; then
  printf 'runtime parent must already exist and be canonical\n' >&2
  exit 2
fi
mkdir -m 700 "$runtime"
mkdir -m 700 "$runtime/bin" "$runtime/clients" "$runtime/run"
# Resolve the actual Node executable before isolating HOME, avoiding a version
# manager shim that tries to download a toolchain into the disposable home.
MCP651_NODE=$(node -p 'process.execPath')
export MCP651_NODE
cp "$source_dir/clients/package.json" "$source_dir/clients/package-lock.json" "$runtime/clients/"
npm ci --prefix "$runtime/clients" --ignore-scripts --no-audit --no-fund >"$runtime/client-install.log" 2>&1
case "$(uname -s):$(uname -m)" in
  Darwin:arm64) codex_platform=darwin-arm64; codex_target=aarch64-apple-darwin ;;
  Linux:aarch64|Linux:arm64) codex_platform=linux-arm64; codex_target=aarch64-unknown-linux-musl ;;
  Linux:x86_64) codex_platform=linux-x64; codex_target=x86_64-unknown-linux-musl ;;
  *) printf 'unsupported local client platform\n' >&2; exit 2 ;;
esac
MCP651_CODEX=$runtime/clients/node_modules/@openai/codex-$codex_platform/vendor/$codex_target/bin/codex
MCP651_INSPECTOR_ENTRY=$runtime/clients/node_modules/@modelcontextprotocol/inspector/clients/cli/build/index.js
MCP651_INSPECTOR_WRAPPER=$source_dir/inspector.mjs
MCP651_SMOKE=$runtime/bin/mcp-public-smoke
export MCP651_CODEX MCP651_INSPECTOR_ENTRY MCP651_INSPECTOR_WRAPPER MCP651_SMOKE
cd "$repo"
GOMAXPROCS=2 go build -p 2 -o "$runtime/bin/hikyo" ./cmd/hikyo
GOMAXPROCS=2 go build -p 2 -o "$runtime/bin/mcp-fixture" ./scripts/acceptance/mcp-fixture
GOMAXPROCS=2 go build -p 2 -o "$MCP651_SMOKE" ./scripts/mcp-public-smoke
"$runtime/bin/mcp-fixture" local --runtime "$runtime/run" --server "$runtime/bin/hikyo"
printf 'Local rehearsal passed. Redacted receipts: %s/run/\n' "$runtime"
printf 'Retain the pinned clients/builds only through review, then remove this exact owned runtime.\n'

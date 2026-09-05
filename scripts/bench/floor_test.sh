#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/docker" <<'EOF'
#!/bin/sh
case "$1" in
  info) printf '%s\n' "${FLOOR_TEST_DAEMON:?}" ;;
  *) echo 'floor refusal unexpectedly reached Docker mutation' >&2; exit 90 ;;
esac
EOF
chmod 0755 "$work/bin/docker"
refuse() {
	local label=$1 expected=$2
	shift 2
	if PATH="$work/bin:$PATH" ./scripts/bench/floor.sh "$@" >"$work/log" 2>&1; then
		echo "floor preflight accepted $label" >&2; exit 1
	fi
	if ! grep -Fq "$expected" "$work/log"; then
		cat "$work/log" >&2
		echo "floor preflight refused $label for the wrong reason" >&2; exit 1
	fi
}
refuse 'missing destination' 'usage:'
refuse 'stale destination' 'existing evidence destination' "$work"
FLOOR_TEST_DAEMON='x86_64 linux 2' refuse 'emulated ARM target' 'native ARM64 Linux' "$work/new-amd64"
FLOOR_TEST_DAEMON='aarch64 linux 1' refuse 'missing cgroup v2' 'native ARM64 Linux' "$work/new-v1"
FLOOR_TEST_DAEMON='aarch64 darwin 2' refuse 'non-Linux daemon' 'native ARM64 Linux' "$work/new-os"
GITHUB_ACTIONS=true refuse 'raw CI acceptance' 'required CI refuses raw mode' --raw "$work/new-raw"
echo 'floor preflight: stale, emulated, unconstrained and raw-CI acceptance refused'

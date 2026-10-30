#!/usr/bin/env bash
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
cleanup() {
	status=$?
	if [ "$status" -ne 0 ] && [ -f "$work/output" ]; then cat "$work/output" >&2; fi
	rm -rf "$work"
	exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
mkdir "$work/bin" "$work/workspace"
export GO_DISK_TEST_WORK="$work"
cat >"$work/bin/uname" <<'EOF'
#!/bin/sh
printf 'Linux\n'
EOF
cat >"$work/bin/df" <<'EOF'
#!/bin/sh
set -eu
[ "$1" = -Pk ] && [ "$2" = "$GO_DISK_TEST_WORK/go-cache" ]
[ "${GO_DISK_TEST_DF_ERROR:-false}" != true ] || exit 1
printf 'Filesystem 1024-blocks Used Available Capacity Mounted\n'
printf 'fixture 99999999 0 %s 1%% /\n' "$(cat "$GO_DISK_TEST_WORK/free")"
EOF
cat >"$work/bin/go" <<'EOF'
#!/bin/sh
set -eu
[ "$*" = 'env GOCACHE' ]
[ "${GO_DISK_TEST_GO_ERROR:-false}" != true ] || exit 1
if [ "${GO_DISK_TEST_CACHE_INVALID:-false}" = true ]; then printf 'off\n'; else printf '%s/go-cache\n' "$GO_DISK_TEST_WORK"; fi
EOF
cat >"$work/bin/sudo" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$GO_DISK_TEST_WORK/deletions"
[ "${GO_DISK_TEST_SUDO_ERROR:-false}" != true ] || exit 1
case "$*" in
	'rm -rf -- /usr/local/lib/android')
		[ "${GO_DISK_TEST_RECLAIM:-}" != android ] || printf '20971520\n' >"$GO_DISK_TEST_WORK/free" ;;
	'rm -rf -- /usr/share/dotnet')
		[ "${GO_DISK_TEST_RECLAIM:-}" != dotnet ] || printf '20971520\n' >"$GO_DISK_TEST_WORK/free" ;;
	*) exit 1 ;;
esac
EOF
chmod +x "$work/bin/"*
export PATH="$work/bin:$PATH"
export GITHUB_ACTIONS=true RUNNER_ENVIRONMENT=github-hosted RUNNER_OS=Linux GITHUB_WORKSPACE="$work/workspace"
helper="$script_dir/prepare-go-cache-disk.sh"
reset() {
	printf '%s\n' "$1" >"$work/free"
	rm -f "$work/deletions"
}
accept() { bash "$helper" >"$work/output" 2>&1; }
refuse() { if bash "$helper" >"$work/output" 2>&1; then echo 'accepted refused disk preparation' >&2; exit 1; fi; }
reset 20971520
accept
test ! -e "$work/deletions"
GITHUB_ACTIONS=false refuse
test ! -e "$work/deletions"
RUNNER_ENVIRONMENT=self-hosted refuse
test ! -e "$work/deletions"
RUNNER_OS=macOS refuse
test ! -e "$work/deletions"
GITHUB_WORKSPACE="$work/missing" refuse
test ! -e "$work/deletions"
reset malformed
refuse
test ! -e "$work/deletions"
reset 20971520
GO_DISK_TEST_DF_ERROR=true refuse
test ! -e "$work/deletions"
GO_DISK_TEST_GO_ERROR=true refuse
test ! -e "$work/deletions"
GO_DISK_TEST_CACHE_INVALID=true refuse
test ! -e "$work/deletions"
reset 20971519
GO_DISK_TEST_RECLAIM=android accept
test "$(wc -l <"$work/deletions" | tr -d ' ')" = 1
grep -Fx 'rm -rf -- /usr/local/lib/android' "$work/deletions" >/dev/null
reset 1048576
GO_DISK_TEST_RECLAIM=dotnet accept
test "$(wc -l <"$work/deletions" | tr -d ' ')" = 2
grep -Fx 'rm -rf -- /usr/share/dotnet' "$work/deletions" >/dev/null
reset 1048576
refuse
grep -F 'insufficient free disk' "$work/output" >/dev/null
test "$(wc -l <"$work/deletions" | tr -d ' ')" = 2
reset 1048576
GO_DISK_TEST_SUDO_ERROR=true refuse
test "$(wc -l <"$work/deletions" | tr -d ' ')" = 1
printf 'Go cache disk: hosted-runner boundary, capacity, exact SDK cleanup, malformed reads and refusal fixtures passed\n'

#!/usr/bin/env bash
# Shared cache archives coexist with their expanded contents during restore.
# Keep capacity for that peak and subsequent compilation on disposable runners.
set -euo pipefail

fail() { printf 'Go cache disk: %s\n' "$*" >&2; exit 1; }
[[ "$(uname -s)" = Linux && "${GITHUB_ACTIONS:-}" = true &&
	"${RUNNER_ENVIRONMENT:-}" = github-hosted && "${RUNNER_OS:-}" = Linux ]] ||
	fail 'SDK cleanup requires a GitHub-hosted Linux runner'
[[ -n "${GITHUB_WORKSPACE:-}" && -d "$GITHUB_WORKSPACE" ]] ||
	fail 'GITHUB_WORKSPACE must name the current checkout'
go_cache=$(go env GOCACHE) || fail 'cannot locate the Go compiler cache'
[[ "$go_cache" = /* && "$go_cache" != / ]] || fail 'invalid Go compiler cache directory'
mkdir -p -- "$go_cache"

required_kib=$((20 * 1024 * 1024))
available_kib() {
	df -Pk "$go_cache" | awk '
		NR == 2 && NF == 6 && $4 ~ /^[0-9]+$/ { available = $4 }
		END { if (NR != 2 || available == "") exit 1; print available }
	'
}
measure() {
	available=$(available_kib) || fail 'cannot measure Go cache filesystem capacity'
	[[ "$available" =~ ^[0-9]{1,12}$ ]] || fail 'invalid filesystem capacity'
	printf 'Go cache disk: %s KiB available; require %s KiB\n' "$available" "$required_kib"
}
measure
if ((10#$available >= required_kib)); then exit 0; fi

# These jobs use Go/Node/Docker, never Android or .NET. Do not touch Go/Node
# toolchains, shared Actions caches, Docker resources, checkout or artifacts.
for sdk in /usr/local/lib/android /usr/share/dotnet; do
	printf 'Go cache disk: reclaiming unused SDK %s\n' "$sdk"
	sudo rm -rf -- "$sdk"
	measure
	if ((10#$available >= required_kib)); then exit 0; fi
done
fail 'insufficient free disk after reclaiming the two unused SDK directories'

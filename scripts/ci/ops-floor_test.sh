#!/usr/bin/env bash
# Preflight refusals must happen before compilation or container creation.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == info ]] || { echo 'unexpected Docker mutation' >&2; exit 93; }
printf '%s\n' "$OPS_FLOOR_TEST_DAEMON"
EOF
cat >"$work/bin/go" <<'EOF'
#!/usr/bin/env bash
echo 'unexpected compilation' >&2
exit 94
EOF
chmod +x "$work/bin/docker" "$work/bin/go"
export PATH="$work/bin:$PATH"
export OPS_FLOOR_TEST_DAEMON='aarch64 linux 2'
refused() {
	local expected=$1 architecture=$2 output=$3 log="$work/refusal.log"
	if "$root/scripts/ci/ops-floor.sh" "$architecture" "$output" >"$log" 2>&1; then
		echo 'ops-floor fixture: invalid preflight succeeded' >&2; exit 1
	fi
	grep -F "$expected" "$log" >/dev/null || { cat "$log"; exit 1; }
}
refused 'only native arm64 and amd64' other "$work/unknown"
refused 'architecture differs' amd64 "$work/wrong-architecture"
export OPS_FLOOR_TEST_DAEMON='aarch64 linux 1'
refused 'cgroup v2 is required' arm64 "$work/old-cgroup"
mkdir "$work/occupied"
printf 'retained\n' >"$work/occupied/marker"
refused 'destination exists' arm64 "$work/occupied"
grep -Fx retained "$work/occupied/marker" >/dev/null
ln -s "$work/absent" "$work/symlink"
refused 'destination exists' arm64 "$work/symlink"
printf 'ops-floor preflight: invalid architecture, cgroup and occupied evidence refused\n'

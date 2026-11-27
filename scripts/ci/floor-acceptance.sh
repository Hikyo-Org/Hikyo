#!/usr/bin/env bash
# Release-only K2/K3 evidence. Native arm64 Docker; measured processes are inside
# one 4 CPU / 4 GiB / zero-swap cgroup. No secret enters an image or artifact.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
: "${FLOOR_BACKUP_CUSTODY:?set the dedicated TEST backup custody JSON secret}"
: "${FLOOR_ROOT_CUSTODY:?set the distinct dedicated TEST root custody secret}"
output=${1:?usage: floor-acceptance.sh NEW_EVIDENCE_DIRECTORY}
if [ -e "$output" ]; then
    echo 'floor acceptance: evidence directory already exists; refusing stale evidence' >&2
    exit 1
fi
if [ "$(docker info --format '{{.Architecture}} {{.OSType}} {{.CgroupVersion}}')" != 'aarch64 linux 2' ]; then
    echo 'floor acceptance: requires a native arm64 Linux Docker daemon with cgroup v2' >&2
    exit 1
fi
work=$(mktemp -d)
container_id=''
cleanup() {
    if [ -n "$container_id" ]; then docker rm -f "$container_id" >/dev/null 2>&1 || true; fi
    rm -rf "$work"
}
trap cleanup EXIT
umask 077
mkdir -p "$work/backup" "$work/root" "$work/image" "$output"
output=$(cd "$output" && pwd)
printf '%s' "$FLOOR_BACKUP_CUSTODY" | jq -er '.identity | strings | select(test("^AGE-SECRET-KEY-1[0-9A-Z]+$"))' >"$work/backup/identity"
printf '%s' "$FLOOR_BACKUP_CUSTODY" | jq -er '.recipient | strings | select(test("^age1[0-9a-z]+$"))' >"$work/backup/recipient"
if [[ ! "$FLOOR_ROOT_CUSTODY" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo 'floor acceptance: root custody must be a 64-hex-character TEST key' >&2
    exit 1
fi
printf '%s\n' "$FLOOR_ROOT_CUSTODY" >"$work/root/rootkey"
unset FLOOR_BACKUP_CUSTODY FLOOR_ROOT_CUSTODY
export CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOMAXPROCS=2
# Compilation is outside the measured cgroup; the runtime is the floor gate.
go build -p 1 -trimpath -o "$work/image/hikyo" ./cmd/hikyo
go test -p 1 -c -tags flooracceptance -o "$work/image/floor.test" ./internal/isolation
# COPY owns image files as root; the non-root runtime still needs execute access.
chmod 0555 "$work/image/hikyo" "$work/image/floor.test"
cat >"$work/image/Dockerfile" <<'EOF'
FROM scratch
COPY hikyo /hikyo
COPY floor.test /floor.test
ENTRYPOINT ["/floor.test"]
EOF
docker build --quiet --iidfile "$work/image-id" "$work/image" >/dev/null
jq -n --arg commit "$(git rev-parse HEAD)" --arg run_url "${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-local}/actions/runs/${GITHUB_RUN_ID:-local}" \
    --arg image "$(cat "$work/image-id")" --arg source_dirty "$(git status --porcelain --untracked-files=normal)" \
    '{commit:$commit,run_url:$run_url,image:$image,source_dirty:($source_dirty != ""),custody_sources:["FLOOR_BACKUP_CUSTODY","FLOOR_ROOT_CUSTODY"],limits:{cpus:4,memory_bytes:4294967296,swap_bytes:0}}' >"$output/provenance.json"
container_id=$(docker create --network none --read-only --cpus 4 --memory 4g --memory-swap 4g --pids-limit 512 \
    --user "$(id -u):$(id -g)" --tmpfs /tmp:rw,nosuid,nodev,size=3g,mode=1777 \
    --mount "type=bind,src=$work/backup,dst=/custody/backup,readonly" \
    --mount "type=bind,src=$work/root,dst=/custody/root,readonly" \
    --mount "type=bind,src=$output,dst=/evidence" \
    "$(cat "$work/image-id")" -test.v -test.count=1 -test.timeout=30m -test.run '^TestFloorBackupRestoreAcceptance$')
set +e
docker start -a "$container_id" >"$output/tests.log" 2>&1
start_status=$?
set -e
docker inspect --format '{{json .State}}' "$container_id" >"$output/container-state.json"
cat "$output/tests.log"
if [ "$start_status" -ne 0 ] || ! jq -e '.ExitCode == 0 and .OOMKilled == false' "$output/container-state.json" >/dev/null; then
    echo 'floor acceptance: constrained runtime failed; inspect evidence' >&2
    exit 1
fi
jq -e '.status == "pass"' "$output/result.json" >/dev/null
jq -e '.ok == true and .rto_met == true and .values_readable == true and .credential == true' "$output/cli-drill.json" >/dev/null
printf 'floor acceptance: K2/K3 and CLI runbook passed; evidence: %s\n' "$output"

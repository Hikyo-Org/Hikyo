#!/usr/bin/env bash
# O2 resource evidence, separate from K2/K3 recovery and operator-fit lanes.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
architecture=${1:?usage: ops-floor.sh arm64\|amd64 NEW_EVIDENCE_DIRECTORY}
output=${2:?usage: ops-floor.sh arm64\|amd64 NEW_EVIDENCE_DIRECTORY}
case "$architecture" in
	arm64) cpus=4 ;;
	amd64) cpus=2 ;;
	*) echo 'ops-floor: only native arm64 and amd64 are supported' >&2; exit 1 ;;
esac
if [[ -e "$output" || -L "$output" ]]; then
	echo 'ops-floor: evidence destination exists; refusing stale evidence' >&2
	exit 1
fi
daemon=$(docker info --format '{{.Architecture}} {{.OSType}} {{.CgroupVersion}}')
case "$daemon" in
	'aarch64 linux 2'|'arm64 linux 2') native=arm64 ;;
	'x86_64 linux 2'|'amd64 linux 2') native=amd64 ;;
	*) echo 'ops-floor: native Linux Docker with cgroup v2 is required' >&2; exit 1 ;;
esac
if [[ "$native" != "$architecture" ]]; then
	echo 'ops-floor: Docker daemon architecture differs; emulated acceptance is refused' >&2
	exit 1
fi
umask 077
work=$(mktemp -d)
container_id=''
cleanup() {
	if [[ -n "$container_id" ]]; then docker rm -f "$container_id" >/dev/null 2>&1 || true; fi
	rm -rf "$work"
}
trap cleanup EXIT
mkdir "$work/image" "$work/runtime"
chmod 0700 "$work/runtime"
mkdir -p "$output"
output=$(cd "$output" && pwd)
export CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" GOMAXPROCS=2
go build -p 1 -trimpath -o "$work/image/hikyo" ./cmd/hikyo
go build -p 1 -trimpath -o "$work/image/ops-floor" ./scripts/ops-floor
go test -p 1 -c -tags opsfloor -o "$work/image/isolation.test" ./internal/isolation
go test -p 1 -c -o "$work/image/admission.test" ./internal/admission
chmod 0555 "$work/image/hikyo" "$work/image/ops-floor" "$work/image/isolation.test" "$work/image/admission.test"
cat >"$work/image/Dockerfile" <<'EOF'
FROM scratch
COPY hikyo /hikyo
COPY ops-floor /ops-floor
COPY isolation.test /isolation.test
COPY admission.test /admission.test
ENTRYPOINT ["/ops-floor"]
EOF
docker build --quiet --iidfile "$work/image-id" "$work/image" >/dev/null
jq -n --arg commit "$(git rev-parse HEAD)" --arg source_dirty "$(git status --porcelain --untracked-files=normal)" \
	--arg image "$(cat "$work/image-id")" --arg architecture "$architecture" --argjson cpus "$cpus" \
	--arg binary_sha256 "$(shasum -a 256 "$work/image/hikyo" | awk '{print $1}')" \
	--arg tests_sha256 "$(shasum -a 256 "$work/image/isolation.test" | awk '{print $1}')" \
	--arg runner_sha256 "$(shasum -a 256 "$work/image/ops-floor" | awk '{print $1}')" \
	--arg admission_sha256 "$(shasum -a 256 "$work/image/admission.test" | awk '{print $1}')" \
	--arg run_url "${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-local}/actions/runs/${GITHUB_RUN_ID:-local}" \
	'{schema:"hikyo.dev/ops-floor-provenance/v1",commit:$commit,source_dirty:($source_dirty != ""),image:$image,architecture:$architecture,cpus:$cpus,memory_bytes:4294967296,swap_bytes:0,binary_sha256:$binary_sha256,tests_sha256:$tests_sha256,runner_sha256:$runner_sha256,admission_sha256:$admission_sha256,run_url:$run_url,physical_pi:false,derating:"not claimed",sqlite_storage:"private disposable host-backed directory"}' >"$output/provenance.json"
container_id=$(docker create --network none --read-only --cpus "$cpus" --memory 4g --memory-swap 4g --pids-limit 512 \
	--user "$(id -u):$(id -g)" --mount "type=bind,src=$work/runtime,dst=/tmp" \
	--env "HIKYO_OPS_FLOOR_ARCH=$architecture" \
	--mount "type=bind,src=$output,dst=/evidence" "$(cat "$work/image-id")")
set +e
docker start -a "$container_id" >"$output/runner.log" 2>&1
start_status=$?
set -e
docker inspect --format '{{json .State}}' "$container_id" >"$output/container-state.json"
if [[ "$start_status" -ne 0 ]] || ! jq -e '.ExitCode == 0 and .OOMKilled == false' "$output/container-state.json" >/dev/null; then
	echo "ops-floor: measured runtime failed; inspect $output" >&2
	exit 1
fi
jq -e '.status == "pass" and (.doctor_states | length == 8) and (.checks | length == 2)' "$output/result.json" >/dev/null
printf 'ops-floor: native %s doctor and selected bounds passed; evidence %s\n' "$architecture" "$output"

#!/usr/bin/env bash
# Build outside measurement. Run real workloads in a native floor cgroup.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
raw=0
if [[ ${1:-} == --raw ]]; then raw=1; shift; fi
if [[ $# != 1 ]]; then
	echo 'usage: scripts/bench/floor.sh [--raw] NEW_EVIDENCE_DIRECTORY' >&2
	exit 2
fi
output=$1
if [[ ${GITHUB_ACTIONS:-} == true ]] && { [[ "$raw" == 1 ]] || [[ -n $(git status --porcelain --untracked-files=normal) ]]; }; then
	echo 'floor-bench: required CI refuses raw mode or a dirty candidate source' >&2; exit 1
fi
if [[ -e "$output" || -L "$output" ]]; then
	echo 'floor-bench: refusing an existing evidence destination' >&2; exit 1
fi
case $(docker info --format '{{.Architecture}} {{.OSType}} {{.CgroupVersion}}') in
	'aarch64 linux 2'|'arm64 linux 2') ;;
	*) echo 'floor-bench: native ARM64 Linux Docker with cgroup v2 is required; emulation refused' >&2; exit 1 ;;
esac
umask 077
work=$(mktemp -d)
container_id=''
scanner_id=''
cleanup() {
	if [[ -n "$scanner_id" ]]; then docker rm -f "$scanner_id" >/dev/null 2>&1 || true; fi
	if [[ -n "$container_id" ]]; then docker rm -f "$container_id" >/dev/null 2>&1 || true; fi
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$output"
output=$(cd "$output" && pwd)
mkdir "$work/image" "$work/runtime"
export CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOMAXPROCS=2
go build -p 1 -trimpath -o "$work/image/hikyo" ./cmd/hikyo
go build -p 1 -trimpath -o "$work/image/bench-scan" ./cmd/bench-scan
go test -p 1 -c -tags floorbench -o "$work/image/isolation.test" ./internal/isolation
go test -p 1 -c -tags floorbench -o "$work/image/floor.test" ./internal/bench
cp docs/release/measurements/derate.json "$work/image/derate.json"
chmod 0444 "$work/image/derate.json"
chmod 0555 "$work/image/hikyo" "$work/image/bench-scan" "$work/image/isolation.test" "$work/image/floor.test"
cat >"$work/image/Dockerfile" <<'EOF'
FROM scratch
COPY hikyo /hikyo
COPY bench-scan /bench-scan
COPY isolation.test /isolation.test
COPY floor.test /floor.test
COPY derate.json /derate.json
ENTRYPOINT ["/floor.test", "-test.run=^TestFloorBench$", "-test.v", "-test.timeout=12m"]
EOF
docker build --quiet --iidfile "$work/image-id" "$work/image" >/dev/null
if [[ ${GITHUB_ACTIONS:-} == true && -n $(git status --porcelain --untracked-files=normal) ]]; then
	echo 'floor-bench: build changed the exact CI candidate source' >&2; exit 1
fi
git diff HEAD --binary >"$output/source.patch"
git status --porcelain --untracked-files=normal >"$output/source-status.txt"
jq -n --arg commit "$(git rev-parse HEAD)" --arg dirty "$(cat "$output/source-status.txt")" \
	--arg diff "$(shasum -a 256 "$output/source.patch" | awk '{print $1}')" \
	--arg image "$(cat "$work/image-id")" \
	--arg hikyo "$(shasum -a 256 "$work/image/hikyo" | awk '{print $1}')" \
	--arg isolation "$(shasum -a 256 "$work/image/isolation.test" | awk '{print $1}')" \
	--arg floor "$(shasum -a 256 "$work/image/floor.test" | awk '{print $1}')" \
	--arg scan "$(shasum -a 256 "$work/image/bench-scan" | awk '{print $1}')" \
	--arg run "${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-local}/actions/runs/${GITHUB_RUN_ID:-local}" \
	'{source_commit:$commit,source_dirty:($dirty!=""),source_diff_sha256:$diff,image:$image,run_url:$run,binary_sha256:{hikyo:$hikyo,"isolation.test":$isolation,"floor.test":$floor,"bench-scan":$scan}}' >"$output/provenance.json"
# Reuse only a freshly measured actual operator lane from the identical source
# and binary. The Go verifier checks both hashes; no historic proof is adopted.
if [[ -n ${HIKYO_FLOOR_OPERATOR_EVIDENCE:-} ]]; then
	cp -R "$HIKYO_FLOOR_OPERATOR_EVIDENCE" "$output/operator"
else
	# The existing operator runner builds a host helper too. Do not leak the
	# cross-compilation environment into that host build.
	(umask 022; env -u GOOS -u GOARCH HIKYO_OPERATOR_FLOOR_OUTPUT="$output/operator" ./scripts/ci/operator-floor.sh)
fi
# Direct PID1 scanner launch keeps its startup RSS independent of the later
# Argon2 parent's large pre-exec address space. Limits and image are identical.
scanner_id=$(docker create --network none --read-only --cpus 4 --memory 4g --memory-swap 4g --pids-limit 512 \
	--user "$(id -u):$(id -g)" --mount "type=bind,src=$output,dst=/evidence" \
	--env GOMAXPROCS=4 --entrypoint /bench-scan "$(cat "$work/image-id")" \
	-o /evidence/scanner.json -host native-arm64-ci-floor)
set +e
docker start -a "$scanner_id" >"$output/scanner.log" 2>&1
scanner_status=$?
set -e
docker inspect "$scanner_id" | jq '.[0] | {image:.Image,entrypoint:.Config.Entrypoint,status:.State.Status,exit_code:.State.ExitCode,oom_killed:.State.OOMKilled,nano_cpus:.HostConfig.NanoCpus,memory:.HostConfig.Memory,memory_swap:.HostConfig.MemorySwap}' >"$output/scanner-run.json"
if [[ "$scanner_status" != 0 ]] || ! jq -e '.status=="exited" and .exit_code==0 and .oom_killed==false' "$output/scanner-run.json" >/dev/null; then
	echo "floor-bench: direct scanner process refused; inspect $output" >&2; exit 1
fi
container_id=$(docker create --network none --read-only --cpus 4 --memory 4g --memory-swap 4g --pids-limit 512 \
	--user "$(id -u):$(id -g)" --mount "type=bind,src=$work/runtime,dst=/tmp" \
	--mount "type=bind,src=$output,dst=/evidence" \
	--env HIKYO_FLOOR_EVIDENCE=/evidence --env "HIKYO_FLOOR_RAW=$raw" \
	--env GOMAXPROCS=4 "$(cat "$work/image-id")")
set +e
docker start -a "$container_id" >"$output/runner.log" 2>&1
start_status=$?
set -e
docker inspect --format '{{json .State}}' "$container_id" >"$output/container-state.json"
if [[ "$start_status" != 0 ]] || ! jq -e '.ExitCode==0 and .OOMKilled==false' "$output/container-state.json" >/dev/null; then
	echo "floor-bench: measured run refused; inspect $output" >&2; exit 1
fi
if [[ ${GITHUB_ACTIONS:-} == true && -n $(git status --porcelain --untracked-files=normal) ]]; then
	echo 'floor-bench: measurement changed the exact CI candidate source' >&2; exit 1
fi
expected=pass
if [[ "$raw" == 1 ]]; then expected='raw-measurement-only'; fi
jq -e --arg expected "$expected" '.status==$expected' "$output/floor-$(git rev-parse HEAD).json" >/dev/null
printf 'floor-bench: %s; evidence %s\n' "$expected" "$output"

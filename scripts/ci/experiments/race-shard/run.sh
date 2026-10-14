#!/usr/bin/env bash
# Temporary, one cold AB pair. All caches and receipts are outside source.
set -euo pipefail
if [[ $# != 2 ]]; then echo 'usage: run.sh SOURCE_CHECKOUT OUTPUT_DIRECTORY' >&2; exit 2; fi
source_root=$(cd "$1" && pwd)
tools_root=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$2"
out=$(cd "$2" && pwd)
if [[ $(uname -s) != Linux || $(uname -m) != x86_64 ]]; then echo 'trial requires native Linux amd64' >&2; exit 1; fi
if [[ $(id -u) == 0 || -n ${COSIGN_BIN:-} ]]; then echo 'trial requires nonroot and absent COSIGN_BIN' >&2; exit 1; fi
if [[ -n $(git -C "$source_root" status --porcelain) ]]; then echo 'trial source must be clean' >&2; exit 1; fi
if docker container inspect hikyo-ci-postgres >/dev/null 2>&1; then echo 'trial refuses an existing PostgreSQL container' >&2; exit 1; fi
# shellcheck disable=SC2329 # EXIT trap
cleanup() { if [[ ${owns_postgres:-false} == true ]]; then docker container rm --force hikyo-ci-postgres >/dev/null 2>&1 || true; fi; }
trap cleanup EXIT
export TRIAL_REAL_GO
TRIAL_REAL_GO=$(command -v go)
export CGO_ENABLED=1 GOFLAGS=-mod=readonly GOTOOLCHAIN=local CI=true
export HIKYO_TEST_POSTGRES_DSN=postgres://hikyo:hikyo@127.0.0.1:5432/hikyo_test
export GOCACHE="$out/bootstrap-cache"
mkdir "$GOCACHE"
setup_start=$(date +%s.%N)
"$TRIAL_REAL_GO" version >"$out/go-version.txt"
"$TRIAL_REAL_GO" env GOOS GOARCH CGO_ENABLED GOFLAGS GOTOOLCHAIN GOMODCACHE >"$out/go-environment.txt"
printf 'Source=%s\nSchedulerSHA256=%s\n' "$(git -C "$source_root" rev-parse HEAD)" "$(sha256sum "$source_root/scripts/ci/test-race-packages.sh")" >"$out/provenance.txt"
(cd "$source_root" && "$TRIAL_REAL_GO" mod download) >"$out/module-prefetch.log" 2>&1
"$TRIAL_REAL_GO" build -o "$out/trial" "$tools_root/main.go"
export TRIAL_MANIFEST="$out/manifest.json" TRIAL_BINARIES="$out/binaries" TRIAL_TOOL="$out/trial"
"$TRIAL_TOOL" prepare "$source_root" "$TRIAL_MANIFEST"
# Prove the frozen plan still is the complete predetermined planner shard.
(cd "$source_root" && "$TRIAL_REAL_GO" run ./scripts/ci/analysis-shards-go/main.go race --root . --shards 12 --shard 0) >"$out/current-shard0.tsv"
cmp "$tools_root/shard0.tsv" "$out/current-shard0.tsv"
setup_end=$(date +%s.%N)
jq -n --arg start "$setup_start" --arg end "$setup_end" '{SetupSeconds:(($end|tonumber)-($start|tonumber)),Order:["baseline","candidate"],ColdBuildCaches:true,SinglePair:true}' >"$out/setup.json"
# Only scheduler shell calls are routed. Native child test processes have this
# function removed by the Go handler, so product subprocesses use the real Go.
# shellcheck disable=SC2329 # exported into unchanged scheduler
go() {
  if [[ ${1:-} == test ]]; then "$TRIAL_TOOL" invoke "$@"; else "$TRIAL_REAL_GO" "$@"; fi
}
export -f go
export TRIAL_SOURCE="$source_root"
# shellcheck disable=SC2329 # invoked in a bounded child shell
run_variant() {
  local code=0 compile_start compile_end
  if [[ "$TRIAL_MODE" == candidate ]]; then
    compile_start=$(date +%s.%N)
    "$TRIAL_TOOL" compile "$TRIAL_MANIFEST" "$TRIAL_BINARIES" >"$TRIAL_VARIANT_DIR/compile.jsonl" 2>"$TRIAL_VARIANT_DIR/compile.stderr" || code=$?
    compile_end=$(date +%s.%N)
    jq -n --arg start "$compile_start" --arg end "$compile_end" --argjson code "$code" '{CompileSeconds:(($end|tonumber)-($start|tonumber)),ExitCode:$code}' >"$TRIAL_VARIANT_DIR/compile-timing.json"
  fi
  if [[ "$code" == 0 ]]; then
    (cd "$TRIAL_SOURCE" && bash ./scripts/ci/test-race-packages.sh "$TRIAL_PLAN") >"$TRIAL_VARIANT_DIR/scheduler.log" 2>&1 || code=$?
  fi
  return "$code"
}
export -f run_variant
export TRIAL_PLAN="$out/current-shard0.tsv"
status=0
for variant in baseline candidate; do
  if [[ -n $(git -C "$source_root" status --porcelain) ]]; then echo "source changed before $variant" >&2; exit 1; fi
  export TRIAL_VARIANT_DIR="$out/$variant"
  export TRIAL_MODE="$variant" TRIAL_RECORDS="$out/$variant/records" RACE_TIMINGS_DIR="$out/$variant/scheduler-timings"
  export GOCACHE="$out/$variant/go-cache"
  mkdir -p "$TRIAL_RECORDS" "$RACE_TIMINGS_DIR"
  mkdir "$GOCACHE"
  if [[ -n $(find "$GOCACHE" -mindepth 1 -maxdepth 1 -print -quit) ]]; then echo 'cache is not initially empty' >&2; exit 1; fi
  pg_start=$(date +%s.%N)
  (cd "$source_root" && ./scripts/ci/start-ci-postgres.sh) >"$out/$variant/postgres-setup.log" 2>&1
  owns_postgres=true
  pg_end=$(date +%s.%N)
  start=$(date +%s.%N)
  code=0
  timeout --signal=TERM --kill-after=30s 30m bash -euo pipefail -c 'run_variant' || code=$?
  end=$(date +%s.%N)
  jq -n --arg start "$start" --arg end "$end" --arg pgstart "$pg_start" --arg pgend "$pg_end" --argjson code "$code" \
    '{WallSeconds:(($end|tonumber)-($start|tonumber)),PostgresSetupSeconds:(($pgend|tonumber)-($pgstart|tonumber)),ExitCode:$code}' >"$out/$variant/timing.json"
  docker logs hikyo-ci-postgres >"$out/$variant/postgres.log" 2>&1
  docker container rm --force hikyo-ci-postgres >"$out/$variant/postgres-remove.log"
  owns_postgres=false
  "$TRIAL_TOOL" validate "$TRIAL_MANIFEST" "$TRIAL_RECORDS" "$out/$variant/result.json" >"$out/$variant/validation.log" 2>&1 || code=1
  if [[ "$code" != 0 ]]; then status=1; fi
  # Keep initial failures and receipts, and still measure the other cold variant.
  # Discard only this trial-owned build cache to bound hosted disk use.
  rm -rf -- "$GOCACHE"
done
if [[ -n $(git -C "$source_root" status --porcelain) ]]; then echo "source changed after candidate" >&2; status=1; fi
if [[ "$status" == 0 ]]; then
  "$TRIAL_TOOL" compare "$out/baseline/result.json" "$out/candidate/result.json" >"$out/comparison.log" 2>&1 || status=1
fi
if [[ "$status" == 0 ]]; then
  jq -n --slurpfile a "$out/baseline/timing.json" --slurpfile b "$out/candidate/timing.json" \
    --slurpfile outcomes "$out/candidate/result.json" \
    '{Baseline:$a[0],Candidate:$b[0],CandidateOverBaseline:($b[0].WallSeconds/$a[0].WallSeconds),Outcomes:$outcomes[0],Evidence:"one cold AB pair; complete batched compile included; not a production latency forecast"}' >"$out/summary.json"
fi
exit "$status"

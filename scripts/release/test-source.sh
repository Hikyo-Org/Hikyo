#!/usr/bin/env bash
# Keep the same coverage as go test ./..., using the prepared core schedule
# and complete isolation partitions instead of one cumulative package alarm.
set -euo pipefail
[[ $# == 0 ]] || { echo 'release tests: no arguments accepted' >&2; exit 1; }
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
cd "$script_dir/../.."
export GOMAXPROCS=2
export GOFLAGS="${GOFLAGS:-} -p=2"
export LC_ALL=C
umask 077
if [[ -n ${RELEASE_SOURCE_TEST_OUTPUT:-} ]]; then
  output=$RELEASE_SOURCE_TEST_OUTPUT
  # Never overwrite evidence from another invocation.
  mkdir "$output"
else
  output=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-release-tests.XXXXXX")
  trap 'rm -rf "$output"' EXIT
fi

# Compare the source planner with the compiled binary's runnable inventory.
# isolation-race includes examples and fuzz seeds as well as Test roots; the
# execution below remains ordinary (non-race), like the original release gate.
go test -p 2 -list '^(Test|Fuzz|Example)' ./internal/isolation >"$output/inventory.log"
grep -E '^(Test|Fuzz|Example)' "$output/inventory.log" | sort >"$output/inventory.txt"
[[ -s "$output/inventory.txt" ]] || { echo 'release tests: empty inventory' >&2; exit 1; }
for shard in 0 1 2; do
  go run -p 2 ./scripts/ci/analysis-shards-go isolation-race \
    --root . --shard "$shard" --shards 3 >"$output/shard-$shard.txt"
  [[ -s "$output/shard-$shard.txt" ]] || { echo 'release tests: empty shard' >&2; exit 1; }
done
cat "$output"/shard-*.txt | sort >"$output/planned.txt"
if grep -Ev '^(Test|Fuzz|Example)[[:alnum:]_]*$' "$output/planned.txt" ||
  [[ -n $(uniq -d "$output/planned.txt") ]] ||
  ! cmp -s "$output/inventory.txt" "$output/planned.txt"; then
  echo 'release tests: partitions must cover each runnable root exactly once' >&2
  exit 1
fi

status=0
run_group() {
  local name=$1
  shift
  if "$@" 2>&1 | tee "$output/$name.log"; then
    printf '%s\tPASS\n' "$name" >>"$output/results.tsv"
  else
    printf '%s\tFAIL\n' "$name" >>"$output/results.tsv"
    status=1
  fi
}
# The existing driver prepares all lint contexts and isolates app/lint from
# concurrent database suites. It excludes isolation, which is covered below.
run_group core ./scripts/ci/test-core-packages.sh
for shard in 0 1 2; do
  tests=$(paste -sd '|' "$output/shard-$shard.txt")
  run_group "isolation-$shard" go test -json -p 2 -parallel 2 -count=1 \
    -timeout=18m ./internal/isolation/ -run "^(${tests})$"
done
# Keep fixture coverage even after a failed core group or isolation partition.
run_group release-fixtures ./scripts/release/test-fixtures.sh
exit "$status"

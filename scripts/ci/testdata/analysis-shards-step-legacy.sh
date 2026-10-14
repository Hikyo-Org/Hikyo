set -o pipefail
trusted_planner="$RUNNER_TEMP/analysis-shards.go"
if [[ "$EVENT_NAME" =~ ^(workflow_dispatch|schedule|repository_dispatch)$ ]]; then
  test "$GITHUB_REF" = refs/heads/main || { echo 'Full CI requires the trusted main ref.' >&2; exit 1; }
  cp ./scripts/ci/analysis-shards-go/main.go "$trusted_planner"
else
  git show "$BASE_SHA:scripts/ci/analysis-shards-go/main.go" >"$trusted_planner"
fi

race_plan='[]'
fuzz_plan='[]'
isolation_plan='[]'
shard_count=3
# Refresh against hosted timings: eight shards still reached 26m.
# Leave room for cold compiler caches without extending timeouts.
race_shard_count=12
shard=0
while [ "$shard" -lt "$race_shard_count" ]; do
  race_shard=$(go run "$trusted_planner" race --root . \
    --shard "$shard" --shards "$race_shard_count" | jq -Rsc 'split("\n") | map(select(length > 0))')
  race_plan=$(jq -cn --argjson plan "$race_plan" --argjson shard "$race_shard" \
    '$plan + [$shard]')
  shard=$((shard + 1))
done
shard=0
while [ "$shard" -lt "$shard_count" ]; do
  fuzz_shard=$(go run "$trusted_planner" fuzz --root . \
    --shard "$shard" --shards "$shard_count" | jq -Rsc 'split("\n") | map(select(length > 0))')
  isolation_shard=$(go run "$trusted_planner" isolation --root . \
    --shard "$shard" --shards "$shard_count" | jq -Rsc 'split("\n") | map(select(length > 0))')
  fuzz_plan=$(jq -cn --argjson plan "$fuzz_plan" --argjson shard "$fuzz_shard" \
    '$plan + [$shard]')
  isolation_plan=$(jq -cn --argjson plan "$isolation_plan" --argjson shard "$isolation_shard" \
    '$plan + [$shard]')
  shard=$((shard + 1))
done

if [ "$(printf '%s' "$race_plan" | jq 'flatten | length')" -eq 0 ] ||
  [ "$(printf '%s' "$fuzz_plan" | jq 'flatten | length')" -eq 0 ] ||
  [ "$(printf '%s' "$isolation_plan" | jq 'flatten | length')" -eq 0 ]; then
  echo 'analysis shards: test, race, or fuzz plan was empty' >&2
  exit 1
fi
{
  echo "race_plan=$race_plan"
  echo "fuzz_plan=$fuzz_plan"
  echo "isolation_plan=$isolation_plan"
  echo "race_shards=$(printf '%s' "$race_plan" | jq -c 'keys')"
  echo "shards=$(printf '%s' "$isolation_plan" | jq -c 'keys')"
} >>"$GITHUB_OUTPUT"

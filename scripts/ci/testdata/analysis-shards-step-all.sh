#!/usr/bin/env bash
set -euo pipefail
trusted_planner="$RUNNER_TEMP/analysis-shards.go"
if [[ "$EVENT_NAME" =~ ^(workflow_dispatch|schedule|repository_dispatch|push)$ ]]; then
  [[ "$GITHUB_REF" = refs/heads/main || "$EVENT_NAME" = push ]] || { echo 'Full CI requires the trusted main ref.' >&2; exit 1; }
  cp ./scripts/ci/analysis-shards-go/main.go "$trusted_planner"
else
  git show "$BASE_SHA:scripts/ci/analysis-shards-go/main.go" >"$trusted_planner"
fi
planner="$RUNNER_TEMP/analysis-shards"
go build -o "$planner" "$trusted_planner"
race_count=12
shard_count=3
if "$planner" --supports-all-v1 >"$RUNNER_TEMP/planner-capability" 2>"$RUNNER_TEMP/planner-capability-error"; then
  test "$(cat "$RUNNER_TEMP/planner-capability")" = analysis-shards-all-v1
  plan=$("$planner" all --root . --race-shards "$race_count" --shards "$shard_count")
else
  probe_status=$?
  # The first candidate still plans with its trusted pre-upgrade base. Only
  # this exact legacy source may use the unchanged individual-mode contract.
  test "$probe_status" = 2
  test "$(sha256sum "$trusted_planner" | awk '{print $1}')" = 1349c193de1ca689aa322bb71affdea4e287a972af38d0d5530a6978e22b2d3c
  race_plan='[]'
  fuzz_plan='[]'
  isolation_plan='[]'
  for kind in race fuzz isolation; do
    count=$shard_count
    if [ "$kind" = race ]; then count=$race_count; fi
    family='[]'
    shard=0
    while [ "$shard" -lt "$count" ]; do
      targets=$("$planner" "$kind" --root . --shard "$shard" --shards "$count" | jq -Rsc 'split("\n") | map(select(length > 0))')
      family=$(jq -cn --argjson plan "$family" --argjson targets "$targets" '$plan + [$targets]')
      shard=$((shard + 1))
    done
    case "$kind" in race) race_plan=$family ;; fuzz) fuzz_plan=$family ;; isolation) isolation_plan=$family ;; esac
  done
  plan=$(jq -cn --argjson race_plan "$race_plan" --argjson fuzz_plan "$fuzz_plan" --argjson isolation_plan "$isolation_plan" \
    '{race_plan:$race_plan,fuzz_plan:$fuzz_plan,isolation_plan:$isolation_plan,race_shards:($race_plan|keys),shards:($isolation_plan|keys)}')
fi
# Both protocols must prove a complete, nonempty, correctly indexed matrix.
# No failed bulk call or unknown legacy source can fall back to a green plan.
plan=$(printf '%s\n' "$plan" | jq -ces --argjson race "$race_count" --argjson shards "$shard_count" '
  if length != 1 then error("ambiguous shard plan") else .[0] end |
  if type != "object" or keys != ["fuzz_plan","isolation_plan","race_plan","race_shards","shards"] then error("invalid shard plan fields") else . end |
  if (.race_plan | length) != $race or (.fuzz_plan | length) != $shards or (.isolation_plan | length) != $shards or
     .race_shards != [range(0;$race)] or .shards != [range(0;$shards)] or
     ([.race_plan,.fuzz_plan,.isolation_plan] | all(.[]; type == "array" and all(.[]; type == "array" and length > 0 and all(.[]; type == "string" and length > 0)))) != true
  then error("incomplete shard plan") else . end')
for key in race_plan fuzz_plan isolation_plan race_shards shards; do
  printf '%s=%s\n' "$key" "$(printf '%s\n' "$plan" | jq -c --arg key "$key" '.[$key]')"
done >>"$GITHUB_OUTPUT"

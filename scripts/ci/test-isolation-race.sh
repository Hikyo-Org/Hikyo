#!/usr/bin/env bash
set -euo pipefail

tests=$(printf '%s' "$ISOLATION_TARGETS" | jq -er \
  'if type == "array" and length > 0 and all(.[]; type == "string" and test("^[A-Za-z0-9_]+$"))
    then join("|") else error("invalid isolation target inventory") end')
started=$SECONDS
status=0
go test -json -race -count=1 -timeout 150m ./internal/isolation/ -run "^(${tests})$" |
  tee "$RUNNER_TEMP/isolation-race.json" |
  jq --unbuffered -rj 'select(.Action == "output" or .Action == "build-output") | .Output' || status=$?
jq -cn --argjson seconds "$((SECONDS - started))" --argjson status "$status" \
  '{WallSeconds: $seconds, ExitCode: $status}' >"$RUNNER_TEMP/isolation-race.wall.json"
exit "$status"

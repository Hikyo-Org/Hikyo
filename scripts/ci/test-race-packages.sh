#!/usr/bin/env bash
# Each shard retains its assigned packages. Run app after its peers so its
# database-drop cleanup does not wait on their PostgreSQL checkpoint/fsync work.
set -euo pipefail
if [[ "$#" != 1 || ! -f "$1" || ! -r "$1" ]]; then
  echo 'test race: expected one readable shard package file' >&2
  exit 1
fi
work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-test-race.XXXXXX")
trap 'rm -rf "$work"' EXIT
cp "$1" "$work/shard"
isolation_package=$(go list ./internal/isolation)
app_package=$(go list ./internal/app)
go list ./... >"$work/all"
if [[ "$isolation_package" == "$app_package" ]] ||
  [[ $(grep -Fxc "$isolation_package" "$work/all" || true) != 1 ]] ||
  [[ $(grep -Fxc "$app_package" "$work/all" || true) != 1 ]] ||
  [[ -n $(sort "$work/all" | uniq -d) ]]; then
  echo 'test race: package inventory is missing or duplicates an expected package' >&2
  exit 1
fi
if [[ ! -s "$work/shard" || -n $(sort "$work/shard" | uniq -d) ]]; then
  echo 'test race: shard inventory is empty or contains duplicate packages' >&2
  exit 1
fi
: >"$work/concurrent"
has_app=false
while IFS= read -r package || [[ -n "$package" ]]; do
  if [[ -z "$package" || "$package" == "$isolation_package" ]] ||
    ! grep -Fxq -- "$package" "$work/all"; then
    echo 'test race: shard contains an unknown, empty or isolation package' >&2
    exit 1
  fi
  if [[ "$package" == "$app_package" ]]; then
    has_app=true
  else
    printf '%s\n' "$package" >>"$work/concurrent"
  fi
done <"$work/shard"
status=0
if [[ -s "$work/concurrent" ]]; then
  xargs go test -race -p 2 -timeout=20m -vet=off -count=1 <"$work/concurrent" || status=1
fi
# Execute even after a peer failure, preserving the assigned coverage on red runs.
if [[ "$has_app" == true ]]; then
  go test -race -p 2 -timeout=20m -vet=off -count=1 "$app_package" || status=1
fi
exit "$status"

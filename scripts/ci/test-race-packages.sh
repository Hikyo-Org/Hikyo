#!/usr/bin/env bash
# Each shard retains its assigned packages. Run filtered app/service suites
# sequentially after peers to avoid PostgreSQL checkpoint/fsync contention.
# Peers, including filtered store and upgrade suites, share a two-wide pool
# with one go test per entry, in the planner's longest-first order.
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
service_package=$(go list ./internal/service)
store_package=$(go list ./internal/store)
store_upgrade_package=$(go list ./internal/store/upgrade)
upgradegate_package=$(go list ./internal/upgradegate)
go list ./... >"$work/all"
printf '%s\n' "$isolation_package" "$app_package" "$service_package" "$store_package" "$store_upgrade_package" "$upgradegate_package" >"$work/expected"
if [[ -n $(sort "$work/expected" | uniq -d) || -n $(sort "$work/all" | uniq -d) ]] ||
  [[ $(grep -Fxc -f "$work/expected" "$work/all" || true) != 6 ]]; then
  echo 'test race: package inventory is missing or duplicates an expected package' >&2
  exit 1
fi
if [[ ! -s "$work/shard" || -n $(cut -f1 "$work/shard" | sort | uniq -d) ]]; then
  echo 'test race: shard inventory is empty or contains duplicate packages' >&2
  exit 1
fi
: >"$work/concurrent"
: >"$work/filtered"
while IFS= read -r line || [[ -n "$line" ]]; do
  package=${line%%$'\t'*}
  filter=
  if [[ "$line" == *$'\t'* ]]; then
    filter=${line#*$'\t'}
  fi
  if [[ -z "$package" || "$package" == "$isolation_package" ]] ||
    ! grep -Fxq -- "$package" "$work/all"; then
    echo 'test race: shard contains an unknown, empty or isolation package' >&2
    exit 1
  fi
  if [[ "$package" == "$app_package" || "$package" == "$service_package" ||
    "$package" == "$store_package" || "$package" == "$store_upgrade_package" ||
    "$package" == "$upgradegate_package" ]]; then
    # Only these suites may carry an anchored alternation of target names.
    if [[ ! "$filter" =~ ^\^\([A-Za-z0-9_]+(\|[A-Za-z0-9_]+)*\)\$$ ]]; then
      echo 'test race: split suite requires exactly one anchored target-name filter' >&2
      exit 1
    fi
    if [[ "$package" == "$app_package" || "$package" == "$service_package" ]]; then
      printf '%s\t%s\n' "$package" "$filter" >>"$work/filtered"
    else
      printf '%s\t%s\n' "$package" "$filter" >>"$work/concurrent"
    fi
  elif [[ "$line" == *$'\t'* ]]; then
    echo 'test race: only split suites carry target filters' >&2
    exit 1
  else
    printf '%s\n' "$package" >>"$work/concurrent"
  fi
done <"$work/shard"
status=0
if [[ -s "$work/concurrent" ]]; then
  # A validated filter holds no blank, quote or backslash, so xargs passes it
  # through intact as the second argument.
  # shellcheck disable=SC2016
  xargs -P 2 -L 1 sh -c 'if [ "$#" = 2 ]; then set -- -run "$2" "$1"; fi; exec go test -race -p 2 -timeout=20m -vet=off -count=1 "$@"' race <"$work/concurrent" || status=1
fi
while IFS=$'\t' read -r package filter; do
  go test -race -p 2 -timeout=20m -vet=off -count=1 -run "$filter" "$package" || status=1
done <"$work/filtered"
exit "$status"

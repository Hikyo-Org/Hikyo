#!/bin/sh
# Actual packaged binary, production mode, fresh datastore and same-build restart.
set -eu
[ "$#" -eq 2 ] || { printf 'usage: %s BINARY AUTHENTICATED_BUNDLE\n' "$0" >&2; exit 2; }
binary=$(realpath "$1") bundle=$(realpath "$2")
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-production-smoke.XXXXXX")
scratch=$(realpath "$scratch")
pid=
cleanup() {
 if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; fi
 rm -rf "$scratch"
}
trap cleanup EXIT HUP INT TERM
umask 077
openssl rand -hex 32 >"$scratch/root.key"
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$scratch/operator.key" 2>/dev/null
openssl pkey -in "$scratch/operator.key" -pubout -out "$scratch/operator.pub" 2>/dev/null
for engine in sqlite postgres; do
 if [ "$engine" = postgres ] && [ -z "${HIKYO_SMOKE_POSTGRES_DSN:-}" ]; then continue; fi
 mkdir "$scratch/$engine"
 datastore=sqlite:$scratch/$engine/hikyo.db
 if [ "$engine" = postgres ]; then datastore=$HIKYO_SMOKE_POSTGRES_DSN; fi
 for attempt in 1 2; do
  log=$scratch/$engine/boot-$attempt.log
  env -i PATH="$PATH" HOME="$scratch" \
   HIKYO_DB="$datastore" HIKYO_EXTERNAL_ORIGIN=https://nightly.fixture.invalid \
   HIKYO_UPGRADE_BUNDLE="$bundle" HIKYO_UPGRADE_STATE_DIR="$scratch/$engine" \
   HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY="$scratch/operator.pub" HIKYO_UPDATE_CHANNEL=off \
   "$binary" server --root-key-file="$scratch/root.key" \
   --listen=localhost:0 --operational-listen=127.0.0.1:0 >"$log" 2>&1 &
  pid=$!
  ready=false count=0
  while [ "$count" -lt 60 ]; do
   count=$((count + 1))
   if ! kill -0 "$pid" 2>/dev/null; then break; fi
   address=$(jq -Rr 'fromjson? | select(.msg == "server ready") | .operational_addr' "$log" | tail -1)
   if [ -n "$address" ] && curl -fsS --max-time 2 "http://$address/readyz" >"$scratch/ready.json"; then ready=true; break; fi
   sleep 1
  done
  if [ "$ready" != true ]; then cat "$log" >&2; exit 1; fi
  kill "$pid"
  wait "$pid"
  pid=
  printf 'production startup: %s boot %s ready\n' "$engine" "$attempt"
 done
done

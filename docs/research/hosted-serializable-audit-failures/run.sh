#!/bin/bash
# usage: run.sh <db> <script> <clients> <rate> <secs> [label]
PGB=${PGBIN:-$(dirname "$(command -v pgbench)")}  # PostgreSQL 18 client binaries
out=$($PGB/pgbench -h "${PGHOST:-127.0.0.1}" -p "${PGPORT:-55432}" -U "${PGUSER:-postgres}" -n -M prepared -f "$2" -c "$3" -j 4 -R "$4" -T "$5" --max-tries=4 --failures-detailed "$1" 2>&1)
tx=$(echo "$out" | sed -n 's/^number of transactions actually processed: \([0-9]*\).*/\1/p')
fail=$(echo "$out" | sed -n 's/^number of failed transactions: \([0-9]*\) (\(.*\)%).*/\1 (\2%)/p')
ser=$(echo "$out" | sed -n 's/^number of serialization failures: \([0-9]*\).*/\1/p')
retried=$(echo "$out" | sed -n 's/^number of transactions retried: \([0-9]*\).*/\1/p')
retries=$(echo "$out" | sed -n 's/^total number of retries: \([0-9]*\).*/\1/p')
tps=$(echo "$out" | sed -n 's/^tps = \([0-9.]*\).*/\1/p')
lat=$(echo "$out" | sed -n 's/^latency average = \(.*\)/\1/p')
printf '%-28s c=%-3s R=%-5s ok=%-6s failed=%-14s retried=%-6s retries=%-6s tps=%-8s lat=%s\n' "${6:-$2}" "$3" "$4" "$tx" "${fail:-0}" "$retried" "$retries" "$tps" "$lat"
[ -z "$tx" ] && echo "$out" | tail -20

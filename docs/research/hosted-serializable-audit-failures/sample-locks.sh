#!/bin/bash
# usage: sample-locks.sh <db>; run while a load runs against <db>. Prints SIRead lock targets summed over 40 samples.
cd "$(dirname "$0")"
for i in $(seq 1 40); do
  ./q.sh -d "$1" -Atc "SELECT l.locktype || ' ' || c.relname FROM pg_locks l LEFT JOIN pg_class c ON c.oid = l.relation WHERE l.mode = 'SIReadLock'"
  sleep 0.1
done | sort | uniq -c | sort -rn

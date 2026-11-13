#!/bin/bash
# usage: matrix2.sh <template> <commit_delay_us|""> <secs>; every case runs on a fresh copy of the template
# WARNING: force-drops and recreates the database audit_bench_scratch on the server PGHOST/PGPORT point to.
cd "$(dirname "$0")"; T=$1; CD=$2; S=$3
one(){ # label variant_sql script clients rate
  ./q.sh -q -c "drop database if exists audit_bench_scratch with (force)" -c "create database audit_bench_scratch template $T" >/dev/null 2>&1
  [ -n "$CD" ] && ./q.sh -q -c "alter database audit_bench_scratch set commit_delay=$CD" -c "alter database audit_bench_scratch set commit_siblings=0"
  [ -n "$2" ] && ./q.sh -q -d audit_bench_scratch -f "$2"
  ./run.sh audit_bench_scratch "$3" "$4" "$5" "$S" "$1"
}
echo "# template=$T commit_delay=${CD:-0} secs=$S"
for c in 16 64; do for r in 200 400 800; do
  one "baseline" "" audit_ser.sql $c $r
  one "pk-plan" v_pk.sql audit_ser.sql $c $r
done; done
one "pk-plan+per-org-lock" v_pk_orglock.sql audit_ser.sql 64 400
one "no-deferred-trigger" v_notrig.sql audit_ser.sql 64 400
one "REPEATABLE READ" "" audit_rr.sql 64 400

#!/bin/sh
# AFTER reserve, manual fresh-PV preparation and resource approval only.
# Two disposable cross-node checks. No release/operator authority is changed.
# Remote sh must expand the deliberately single-quoted command bodies.
# shellcheck disable=SC2016
set -eu
umask 077
test "$#" -eq 3
runtime=$1
fixture=$2
operator_receipt=$3
case "$runtime:$fixture" in /*:/*) ;; *) exit 2 ;; esac
test -n "$operator_receipt"
command -v jq >/dev/null
kube() { "$fixture" authorize --runtime "$runtime"; kubectl --context picluster --namespace hikyo-mcp-651 "$@"; }
test "$(kube get namespace hikyo-mcp-651 -o jsonpath='{.metadata.uid}')" = "$(jq -r .namespace_uid "$runtime/reservation.json")"
test ! -e "$runtime/storage-verification.json"
pvc_uid=$(kube get pvc installation-authority -o jsonpath='{.metadata.uid}')
pv_name=$(kube get pvc installation-authority -o jsonpath='{.spec.volumeName}')
test -n "$pvc_uid"
test -n "$pv_name"
holder=
cleanup() {
  if [ -n "$holder" ]; then
    kube exec custody-check-0 -- touch /work/release >/dev/null 2>&1 || true
    wait "$holder" || true
  fi
  kube delete -f "$runtime/storage-check.json" --ignore-not-found=true --wait=true >/dev/null || printf 'HOLD: owner must remove only custody-check-0/1\n' >&2
}
trap cleanup EXIT HUP INT TERM
kube apply -f "$runtime/storage-check.json"
kube wait --for=condition=Ready pod/custody-check-0 pod/custody-check-1 --timeout=120s
verify() {
  kube exec "$1" -- sh -eu -c 'test "$(stat -c "%a %g" /installation)" = "2770 65532"; test "$(stat -c "%a %u" /installation/operator-state)" = "700 65532"'
}
verify custody-check-0
verify custody-check-1
kube exec custody-check-0 -- sh -eu -c 'umask 077; test ! -e /installation/operator-state/.fixture-remount-proof; printf "%s" "$1" > /installation/operator-state/.fixture-remount-proof' sh "$pvc_uid"
kube exec custody-check-0 -- sh -eu -c 'umask 077; exec 9>/installation/operator-state/.fixture-lock-proof; flock -x 9; touch /work/held; i=0; while [ ! -e /work/release ] && [ "$i" -lt 200 ]; do sleep 0.1; i=$((i+1)); done; test -e /work/release' &
holder=$!
attempt=0
until kube exec custody-check-0 -- test -e /work/held; do
  attempt=$((attempt+1))
  test "$attempt" -lt 15
  sleep 0.1
done
kube exec custody-check-1 -- sh -eu -c 'exec 9>/installation/operator-state/.fixture-lock-proof; if flock -xn 9 2>/work/lock-error; then exit 1; else code=$?; test "$code" -eq 1; test ! -s /work/lock-error; fi'
kube exec custody-check-0 -- touch /work/release
wait "$holder"
holder=
for pod in custody-check-0 custody-check-1; do
  kube exec "$pod" -- sh -eu -c 'exec 9>/installation/operator-state/.fixture-lock-proof; flock -xn 9; test "$(cat /installation/operator-state/.fixture-remount-proof)" = "$1"; test "$(stat -c "%a %u" /installation/operator-state/.fixture-remount-proof)" = "600 65532"' sh "$pvc_uid"
done
kube delete pod custody-check-0 custody-check-1 --wait=true
kube apply -f "$runtime/storage-check.json"
kube wait --for=condition=Ready pod/custody-check-0 pod/custody-check-1 --timeout=120s
for pod in custody-check-0 custody-check-1; do
  verify "$pod"
  kube exec "$pod" -- sh -eu -c 'test "$(cat /installation/operator-state/.fixture-remount-proof)" = "$1"; test "$(stat -c "%a %u" /installation/operator-state/.fixture-remount-proof)" = "600 65532"; exec 9>/installation/operator-state/.fixture-lock-proof; flock -xn 9' sh "$pvc_uid"
done
kube exec custody-check-0 -- rm /installation/operator-state/.fixture-remount-proof /installation/operator-state/.fixture-lock-proof
kube delete -f "$runtime/storage-check.json" --wait=true
jq -n --arg uid "$pvc_uid" --arg pv "$pv_name" --arg reference "$operator_receipt" '{pvc_uid:$uid,pv_name:$pv,root_mode:"2770",root_group:65532,private_child_mode:"0700",private_child_owner:65532,remount_preserves_private_child:true,cross_node_exclusive_locks_passed:true,operator_receipt_ref:$reference}' > "$runtime/storage-verification.json"
printf 'Storage observations recorded. Owner reviews this receipt and binds its SHA-256 in authorization.json before deploy.sh continue.\n'

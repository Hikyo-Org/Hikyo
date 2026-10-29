#!/bin/sh
# Invoke only AFTER resource approval and final artifact selection.
# This preparation task does not invoke this script.
set -eu
umask 077
if [ "$#" -ne 2 ]; then
  printf 'usage: deploy.sh ABSOLUTE_RUNTIME ABSOLUTE_LOCAL_FIXTURE_EXECUTABLE\n' >&2
  exit 2
fi
runtime=$1
fixture=$2
case "$runtime:$fixture" in /*:/*) ;; *) exit 2 ;; esac
"$fixture" authorize --runtime "$runtime"
kube() { "$fixture" authorize --runtime "$runtime"; kubectl --context picluster --namespace hikyo-mcp-651 "$@"; }
if [ -n "$(kube get namespace hikyo-mcp-651 --ignore-not-found=true -o name)" ]; then
  printf 'HOLD: namespace exists; inspect its owner and custody, do not reset or redeploy it\n' >&2
  exit 1
fi
for file in root-key password dsn backend.crt backend.key backend-ca.crt postgres.crt postgres.key postgres-ca.crt public.crt public.key settings.json; do
  test -f "$runtime/custody/$file"
done
kube create namespace hikyo-mcp-651
kube label namespace hikyo-mcp-651 hikyo.dev/fixture=mcp-651 pod-security.kubernetes.io/enforce=restricted
kube create secret generic fixture-root --from-file="root-key=$runtime/custody/root-key"
kube create secret generic postgres-password --from-file="password=$runtime/custody/password"
kube create secret generic fixture-dsn --from-file="dsn=$runtime/custody/dsn"
kube create secret generic postgres-tls --from-file="server.crt=$runtime/custody/postgres.crt" --from-file="server.key=$runtime/custody/postgres.key"
kube create secret tls backend-tls --cert="$runtime/custody/backend.crt" --key="$runtime/custody/backend.key"
kube create secret tls public-tls --cert="$runtime/custody/public.crt" --key="$runtime/custody/public.key"
kube create configmap db-ca --from-file="ca.crt=$runtime/custody/postgres-ca.crt"
kube create configmap backend-ca --from-file="ca.crt=$runtime/custody/backend-ca.crt"
kube apply -f "$runtime/resources.yaml"
kube wait --for=condition=Ready pod/db-0 pod/tooling --timeout=120s
stage_helper() {
  kube exec -i "$1" -- sh -eu -c 'umask 077; cat > /tools/mcp-fixture.next; chmod 700 /tools/mcp-fixture.next; mv /tools/mcp-fixture.next /tools/mcp-fixture' < "$runtime/helper-linux-arm64"
}
stage_helper tooling
kube exec -i tooling -- sh -eu -c 'umask 077; cat > /work/custody/settings.json' < "$runtime/custody/settings.json"
kube exec tooling -- /tools/mcp-fixture seed --runtime /work/custody
kube cp tooling:/work/custody/fixture.json "$runtime/fixture.json"
chmod 600 "$runtime/fixture.json"
kube scale statefulset/hikyo --replicas=2
kube rollout status statefulset/hikyo --timeout=120s
kube scale deployment/gateway --replicas=1
kube wait --for=create pod -l app=gateway --timeout=120s
kube wait --for=jsonpath='{.status.phase}'=Running pod -l app=gateway --timeout=120s
stage_helper deployment/gateway
kube rollout status deployment/gateway --timeout=120s
printf 'Fixture staged. HOLD before bearer calls: verify approved edge route, public TLS, redirects, runtime identities and network enforcement.\n'
printf 'Public DNS, certificate issuance and OPNsense configuration are separate owner operations from this launcher.\n'

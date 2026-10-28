#!/usr/bin/env bash
# Called only by chart-kind.sh inside its owned, disposable enforcing cluster.
# Transport fixtures model destinations, never real provider credentials.
set -euo pipefail

if [[ $# != 3 || -z "${HIKYO_NETWORK_POLICY_FIXTURE_IMAGE:-}" || -z "${KUBECONFIG:-}" ]]; then
    echo 'usage: chart-network-policy.sh NAMESPACE RELEASE WORK (from chart-kind.sh)' >&2
    exit 2
fi
ns=$1
release=$2
work=$3
mkdir "$work"
evidence=${HIKYO_CHART_NETWORK_POLICY_EVIDENCE:-$work/evidence}
# Refuse to overwrite evidence from another candidate.
mkdir "$evidence"
results="$evidence/probes.jsonl"
probe_image=nicolaka/netshoot:v0.14@sha256:7f08c4aff13ff61a35d30e30c5c1ea8396eac6ab4ce19fd02d5a4b3b5d0d09a2
approved="$ns-approved"
targets="$ns-targets"
forward_pid=
cleanup() {
    local status=$?
    if [[ -n "$forward_pid" ]]; then
        kill "$forward_pid" >/dev/null 2>&1 || true
        wait "$forward_pid" >/dev/null 2>&1 || true
    fi
    # chart-kind owns cluster, namespaces, pods and images, even on failure.
    return "$status"
}
trap cleanup EXIT

kubectl get namespace kube-system -o json | jq '{clusterUID:.metadata.uid}' >"$evidence/cluster.json"
kubectl version -o json >"$evidence/kubernetes.json"
kubectl -n kube-system get daemonset calico-node -o json |
    jq '{uid:.metadata.uid,images:[.spec.template.spec.containers[].image]}' >"$evidence/cni.json"
kubectl -n kube-system get pods -l k8s-app=calico-node -o json |
    jq '[.items[] | {uid:.metadata.uid,images:[.status.containerStatuses[] | {image,imageID}]}]' >"$evidence/cni-images.json"
git rev-parse HEAD >"$evidence/source-head"
git diff --binary HEAD -- chart/hikyo scripts/ci internal/isolation/network_policy_cleanup_test.go >"$evidence/source.patch"
kubectl create namespace "$approved" >/dev/null
kubectl create namespace "$targets" >/dev/null

security='{"runAsNonRoot":true,"runAsUser":65532,"runAsGroup":65532,"allowPrivilegeEscalation":false,"readOnlyRootFilesystem":true,"capabilities":{"drop":["ALL"]},"seccompProfile":{"type":"RuntimeDefault"}}'
run_probe() {
    local namespace=$1 name=$2 labels=$3
    kubectl -n "$namespace" run "$name" --restart=Never --labels "$labels" \
        --image="$probe_image" --overrides="$(jq -cn --argjson security "$security" \
        '{spec:{automountServiceAccountToken:false,securityContext:{runAsNonRoot:true,runAsUser:65532,runAsGroup:65532,seccompProfile:{type:"RuntimeDefault"}},containers:[{name:"probe",image:$ARGS.positional[0],command:["sleep","1800"],securityContext:$security}]}}' --args "$probe_image")" >/dev/null
    kubectl -n "$namespace" wait --for=condition=Ready "pod/$name" --timeout=120s >/dev/null
}
run_probe "$approved" proxy role=proxy
run_probe "$approved" wrong-label role=unapproved
run_probe "$ns" unapproved role=proxy
run_probe "$ns" workload role=workload
run_probe "$ns" operator "app.kubernetes.io/name=hikyo-operator,app.kubernetes.io/instance=$release"

for role in idp adapter remote collector management; do
    kubectl -n "$targets" run "$role" --restart=Never --labels "role=$role" \
        --image="$HIKYO_NETWORK_POLICY_FIXTURE_IMAGE" --image-pull-policy=Never \
        --overrides="$(jq -cn --argjson security "$security" --arg image "$HIKYO_NETWORK_POLICY_FIXTURE_IMAGE" '{spec:{automountServiceAccountToken:false,containers:[{name:"target",image:$image,imagePullPolicy:"Never",securityContext:$security,env:[{name:"POD_NAMESPACE",valueFrom:{fieldRef:{fieldPath:"metadata.namespace"}}}]}]}}')" >/dev/null
done
if ! kubectl -n "$targets" wait --for=condition=Ready pods --all --timeout=90s; then
    kubectl -n "$targets" get pods -o json >"$evidence/targets-failure.json"
    jq '[.items[] | {name:.metadata.name,status:.status.containerStatuses}]' "$evidence/targets-failure.json" >&2
    kubectl -n "$targets" get events --sort-by=.lastTimestamp >&2 || true
    for role in idp adapter remote collector management; do
        kubectl -n "$targets" logs "$role" --tail=30 >&2 || true
    done
    exit 1
fi
for role in idp adapter remote collector management; do
    kubectl -n "$targets" expose pod "$role" --port=8080 >/dev/null
done
kubectl -n "$targets" patch service adapter --type=merge --patch \
    '{"spec":{"ports":[{"name":"http","port":8080,"targetPort":8080},{"name":"tls","port":8443,"targetPort":8443}]}}' >/dev/null
pod=$(kubectl -n "$ns" get pods -l "app.kubernetes.io/name=hikyo,app.kubernetes.io/instance=$release" -o jsonpath='{.items[0].metadata.name}')
pod_ip=$(kubectl -n "$ns" get pod "$pod" -o jsonpath='{.status.podIP}')
server="https://$release-hikyo.$ns.svc:8080/"
attach_probe() {
    printf '%s\n' "{\"securityContext\":$security}" >"$work/probe-container.json"
    kubectl -n "$ns" debug "$pod" --container=np-probe --image="$probe_image" \
        --profile=restricted --custom="$work/probe-container.json" --attach=false -- sleep 1800 >/dev/null
    kubectl -n "$ns" wait --for='jsonpath={.status.ephemeralContainerStatuses[?(@.name=="np-probe")].state.running}' \
        "pod/$pod" --timeout=120s >/dev/null
}
attach_probe

http_probe() {
    local name=$1 namespace=$2 source=$3 container=$4 expected=$5 url=$6
    shift 6
    local result rc attempt limit=1
    # Policy programming is asynchronous. Retain each failed convergence
    # attempt, and require the final positive controls below as well.
    [[ "$expected" == deny || "$name" == recovered-* ]] && limit=15
    for ((attempt=1; attempt<=limit; attempt++)); do
        # Hikyo serves its SPA only for an explicit HTML Accept header.
        if result=$(kubectl -n "$namespace" exec "$source" -c "$container" -- \
            curl --silent --insecure --connect-timeout 2 --max-time 4 \
            --header 'Accept: text/html' \
            --output /dev/null --write-out '%{http_code}' "$@" "$url" 2>"$work/curl-error"); then
            rc=0
        else
            rc=$?
        fi
        local passed=false
        if [[ "$expected" == deny && ( "$rc" == 7 || "$rc" == 28 ) ]]; then passed=true; fi
        if [[ "$expected" != deny && "$rc" == 0 && "$result" == "$expected" ]]; then passed=true; fi
        jq -cn --arg name "$name" --arg expected "$expected" --arg http "$result" \
            --argjson exit "$rc" --argjson attempt "$attempt" --argjson passed "$passed" \
            '{name:$name,expected:$expected,http:$http,exit:$exit,attempt:$attempt,passed:$passed}' >>"$results"
        if [[ "$passed" == true ]]; then
            echo "network-policy: PASS $name"
            return
        fi
        [[ "$attempt" == "$limit" ]] || sleep 1
    done
    echo "network-policy: FAIL $name (exit $rc, HTTP $result)" >&2
    cat "$work/curl-error" >&2
    return 1
}
snapshot() {
    kubectl get networkpolicy --all-namespaces -o json >"$evidence/$1-all-standard-policies.json"
    kubectl get globalnetworkpolicies.crd.projectcalico.org -o json >"$evidence/$1-calico-global-policies.json"
    kubectl get networkpolicies.crd.projectcalico.org --all-namespaces -o json >"$evidence/$1-calico-policies.json"
    # An unexpected administrative allowance invalidates this isolated proof.
    jq -e '.items | length == 0' "$evidence/$1-calico-global-policies.json" >/dev/null
    jq -e '.items | length == 0' "$evidence/$1-calico-policies.json" >/dev/null
    kubectl -n "$ns" get networkpolicy -o json >"$evidence/$1-policies.json"
    jq -e --arg name "$release-hikyo-server" \
        '.items | length == 1 and .[0].metadata.name == $name' "$evidence/$1-policies.json" >/dev/null
}
snapshot default
policy_uid=$(jq -r '.items[0].metadata.uid' "$evidence/default-policies.json")

# Every denied target is first proven reachable. Listener/image/DNS failures
# cannot masquerade as policy enforcement.
http_probe default-unapproved "$ns" unapproved probe 200 "$server"
http_probe default-wrong-label "$approved" wrong-label probe 200 "$server"
for role in idp adapter remote collector management; do
    http_probe "default-egress-$role" "$ns" "$pod" np-probe 200 "http://$role.$targets.svc:8080/"
done

remote_ip=$(kubectl -n "$targets" get pod remote -o jsonpath='{.status.podIP}')
jq -n --arg ns "$ns" --arg approved "$approved" --arg targets "$targets" --arg remote "$remote_ip/32" '
    def pod($role): {podSelector:{matchLabels:{role:$role}}};
    def peer($role): pod($role) + {namespaceSelector:{matchLabels:{"kubernetes.io/metadata.name":$targets}}};
    def destination($role): {to:[peer($role)],ports:[{protocol:"TCP",port:8080}]};
    {operator:{enabled:true},networkPolicy:{
      ingress:{restricted:true,allowOperator:true,from:[
        {namespaceSelector:{matchLabels:{"kubernetes.io/metadata.name":$approved}},podSelector:{matchLabels:{role:"proxy"}}},
        pod("workload")]},
      egress:{restricted:true,datastore:{to:[{podSelector:{matchLabels:{app:"postgres"}}}]},
        identityProviders:destination("idp"),adapters:(destination("adapter") | .ports += [{protocol:"TCP",port:8443}]),collector:destination("collector"),
        extraRules:[{to:[{ipBlock:{cidr:$remote}}],ports:[{protocol:"TCP",port:8080}]}]}}}
' >"$work/restricted.json"
helm upgrade "$release" chart/hikyo -n "$ns" --reuse-values -f "$work/restricted.json" >/dev/null
snapshot restricted
[[ "$(jq -r '.items[0].metadata.uid' "$evidence/restricted-policies.json")" == "$policy_uid" ]]

http_probe approved-proxy "$approved" proxy probe 200 "$server"
http_probe approved-workload "$ns" workload probe 200 "$server"
http_probe approved-operator "$ns" operator probe 200 "$server"
http_probe denied-same-namespace "$ns" unapproved probe deny "$server"
http_probe denied-wrong-label "$approved" wrong-label probe deny "$server"
http_probe denied-pod-ip "$ns" unapproved probe deny "https://$pod_ip:8080/"
http_probe denied-operational "$approved" proxy probe deny "http://$pod_ip:8081/metrics"
for role in idp adapter remote collector; do
    # Use the actual pod IP for ipBlock, avoiding CNI-specific Service DNAT.
    host="$role.$targets.svc"
    [[ "$role" != remote ]] || host="${remote_ip%/32}"
    http_probe "restricted-egress-$role" "$ns" "$pod" np-probe 200 "http://$host:8080/"
done
http_probe provider-cleanup-transport "$ns" "$pod" np-probe 200 "http://adapter.$targets.svc:8080/cleanup" --request DELETE
http_probe denied-management-sentinel "$ns" "$pod" np-probe deny "http://management.$targets.svc:8080/"
# A real service/outbox/provider lifecycle in a pod selected by this chart
# policy, with direct provider readback and both datastore engines.
kubectl -n "$ns" exec "$pod" -c np-probe -- curl --fail --silent \
    "http://adapter.$targets.svc:8080/fixture/ca" >"$work/adapter-ca.crt"
kubectl -n "$ns" create configmap np-provider-ca --from-file=ca.crt="$work/adapter-ca.crt" >/dev/null
adapter_service_ip=$(kubectl -n "$targets" get service adapter -o jsonpath='{.spec.clusterIP}')
jq -n --arg ns "$ns" --arg release "$release" --arg image "$HIKYO_NETWORK_POLICY_FIXTURE_IMAGE" \
    --arg origin "https://adapter.$targets.svc:8443" --arg allowed "$adapter_service_ip/32" --argjson security "$security" '
    {apiVersion:"batch/v1",kind:"Job",metadata:{name:"np-functional",namespace:$ns},spec:{backoffLimit:0,activeDeadlineSeconds:180,
      template:{metadata:{labels:{"app.kubernetes.io/name":"hikyo","app.kubernetes.io/instance":$release}},spec:{restartPolicy:"Never",automountServiceAccountToken:false,
        securityContext:{runAsNonRoot:true,runAsUser:65532,runAsGroup:65532,fsGroup:65532,seccompProfile:{type:"RuntimeDefault"}},
        containers:[{name:"functional",image:$image,imagePullPolicy:"Never",securityContext:$security,
          command:["/functional.test","-test.v","-test.parallel=2","-test.run=^TestNetworkPolicySyntheticProviderCleanup$","-test.timeout=150s"],
          env:[{name:"HIKYO_NETWORK_POLICY_FIXTURE",value:"isolated"},{name:"HIKYO_TEST_GITLAB_REQUIRED",value:"1"},
            {name:"HIKYO_TEST_GITLAB_URL",value:$origin},{name:"HIKYO_TEST_GITLAB_TOKEN",value:"synthetic-network-policy"},
            {name:"HIKYO_TEST_GITLAB_PROJECT",value:"fixture/project"},{name:"HIKYO_TEST_GITLAB_GROUP",value:"fixture"},
            {name:"HIKYO_TEST_GITLAB_ALLOWED_CIDR",value:$allowed},{name:"HIKYO_TEST_GITLAB_CA_FILE",value:"/run/provider-ca/ca.crt"},
            {name:"HIKYO_TEST_POSTGRES_DSN",valueFrom:{secretKeyRef:{name:"hikyo-database",key:"HIKYO_DB"}}}],
          volumeMounts:[{name:"tmp",mountPath:"/tmp"},{name:"database-ca",mountPath:"/run/hikyo-database-ca",readOnly:true},{name:"provider-ca",mountPath:"/run/provider-ca",readOnly:true}]}],
        volumes:[{name:"tmp",emptyDir:{}},{name:"database-ca",secret:{secretName:"hikyo-database-ca"}},{name:"provider-ca",configMap:{name:"np-provider-ca"}}]}}}}
' >"$work/functional.json"
kubectl apply -f "$work/functional.json" >/dev/null
if ! kubectl -n "$ns" wait --for=condition=Complete job/np-functional --timeout=185s; then
    kubectl -n "$ns" logs job/np-functional >"$evidence/functional.log" 2>&1 || true
    cat "$evidence/functional.log" >&2
    exit 1
fi
kubectl -n "$ns" logs job/np-functional | tee "$evidence/functional.log"
# The Job shares chart selector labels. Delete it before subsequent server
# discovery so a completed functional fixture cannot masquerade as the server.
kubectl -n "$ns" delete job np-functional --wait=true >/dev/null
http_probe loopback-collector "$ns" "$pod" np-probe 200 http://127.0.0.1:8081/metrics
for transport in +notcp +tcp; do
    kubectl -n "$ns" exec "$pod" -c np-probe -- dig "$transport" +short +time=2 +tries=1 \
        "postgres.$ns.svc.cluster.local" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' >/dev/null
    echo "network-policy: PASS DNS $transport"
done
kubectl -n "$ns" exec "$pod" -c np-probe -- nc -z -w 4 "postgres.$ns.svc" 5432
echo 'network-policy: PASS datastore TCP'

# Reboot the exact server under restriction. Real boot/readiness requires DNS
# and PostgreSQL, and preserves the chart's nonroot/read-only/seccomp/caps.
kubectl -n "$ns" rollout restart "deployment/$release-hikyo" >/dev/null
kubectl -n "$ns" rollout status "deployment/$release-hikyo" --timeout=180s
pod=$(kubectl -n "$ns" get pods -l "app.kubernetes.io/name=hikyo,app.kubernetes.io/instance=$release" -o json |
    jq -r '.items[] | select(.metadata.deletionTimestamp == null) | .metadata.name')
attach_probe
http_probe restricted-reboot "$approved" proxy probe 200 "$server"
http_probe reboot-loopback-collector "$ns" "$pod" np-probe 200 http://127.0.0.1:8081/metrics
http_probe reboot-denied-ingress "$ns" unapproved probe deny "$server"
http_probe reboot-denied-management "$ns" "$pod" np-probe deny "http://management.$targets.svc:8080/"

kubectl -n "$ns" port-forward "deployment/$release-hikyo" :8080 :8081 >"$work/forward.log" 2>&1 &
forward_pid=$!
for _ in {1..30}; do
    app_port=$(sed -n 's/Forwarding from 127.0.0.1:\([0-9]*\) -> 8080/\1/p' "$work/forward.log")
    ops_port=$(sed -n 's/Forwarding from 127.0.0.1:\([0-9]*\) -> 8081/\1/p' "$work/forward.log")
    [[ -z "$app_port" || -z "$ops_port" ]] || break
    sleep 1
done
curl --fail --silent --insecure --header 'Accept: text/html' "https://127.0.0.1:$app_port/" >/dev/null
curl --fail --silent "http://127.0.0.1:$ops_port/metrics" >/dev/null
echo 'network-policy: PASS authenticated-cluster port-forward recovery and loopback metrics'

# Reversing only the policy opt-ins restores the legacy transport. Provider
# cleanup is exercised before and after this policy-only recovery.
helm upgrade "$release" chart/hikyo -n "$ns" --reuse-values \
    --set networkPolicy.ingress.restricted=false --set networkPolicy.egress.restricted=false >/dev/null
snapshot recovered
[[ "$(jq -r '.items[0].metadata.uid' "$evidence/recovered-policies.json")" == "$policy_uid" ]]
http_probe recovered-ingress "$ns" unapproved probe 200 "$server"
http_probe recovered-egress "$ns" "$pod" np-probe 200 "http://management.$targets.svc:8080/"
http_probe recovered-cleanup "$ns" "$pod" np-probe 200 "http://adapter.$targets.svc:8080/cleanup" --request DELETE
jq -n --arg uid "$policy_uid" --arg image "$probe_image" \
    '{status:"PASS",policyUID:$uid,probeImage:$image,scope:"isolated synthetic CNI fixture; not real-provider or production acceptance",providerLifecycle:"real services, sealed values, worker, durable ledger, provider readback, scrub and credential revocation on SQLite and PostgreSQL",dns:["UDP","TCP"],datastore:"real PostgreSQL boot and reboot",recovery:"policy-only Helm upgrade and API-server port-forward"}' >"$evidence/result.json"
cat "$evidence/result.json"

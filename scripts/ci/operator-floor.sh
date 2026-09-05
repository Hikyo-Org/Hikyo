#!/usr/bin/env bash
# Native arm64 acceptance of the actual operator process, its informers and
# leader election. Only the newly created kind cluster may be mutated.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
for tool in docker kind kubectl helm jq go; do
	command -v "$tool" >/dev/null || { echo "operator-floor: missing $tool" >&2; exit 1; }
done
[[ $(docker info --format '{{.Architecture}}') == aarch64 ]] || { echo 'operator-floor: native arm64 Docker required' >&2; exit 1; }
[[ $(docker info --format '{{.CgroupVersion}}') == 2 ]] || { echo 'operator-floor: cgroup v2 required' >&2; exit 1; }
output=${HIKYO_OPERATOR_FLOOR_OUTPUT:-artifacts/operator-floor}
mkdir -p "$output"
output=$(cd "$output" && pwd)
rm -f "$output/result.json" "$output/load.json" "$output/operator-cgroup.txt" "$output/node-cgroup.txt" "$output/pods.json" "$output/operator-process.txt" "$output/operator-memory-stat.txt" "$output/resource-verification.json"
work=$(mktemp -d)
cluster="hikyo-operator-floor-$$"
node="$cluster-control-plane"
kubeconfig="$work/kubeconfig"
created=false
captured=false
capture() {
	[[ "$captured" == false ]] || return 0
	captured=true
	if [[ "$created" == true ]]; then
		kubectl --kubeconfig "$kubeconfig" -n operator-floor logs deployment/floor-hikyo-operator -c operator >"$output/operator.log" 2>&1 || true
		kubectl --kubeconfig "$kubeconfig" -n operator-floor get pods -o json >"$output/pods.json" 2>/dev/null || true
		docker exec "$node" sh -c 'cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.swap.max /sys/fs/cgroup/memory.peak /sys/fs/cgroup/memory.events' >"$output/node-cgroup.txt" 2>/dev/null || true
		# Read from the node, not with kubectl exec in the measured container:
		# exec would charge the measurement process to the operator's memory.
		local id cgroup
		id=$(jq -r '.items[].status.containerStatuses[]? | select(.name == "operator") | .containerID // empty' "$output/pods.json" 2>/dev/null) || return 0
		id=${id#containerd://}
		[[ "$id" =~ ^[0-9a-f]{64}$ ]] || return 0
		# Resolve the exact CRI container scope in the node's mounted hierarchy.
		# /proc/PID/cgroup can be relative to another cgroup namespace in nested kind.
		cgroup=$(docker exec "$node" find /sys/fs/cgroup -type d -name "cri-containerd-$id.scope") || return 0
		[[ "$cgroup" == /sys/fs/cgroup/* && "$cgroup" != *$'\n'* && "$cgroup" != *..* ]] || { echo 'operator-floor: exact container cgroup unavailable' >&2; return 0; }
		docker exec "$node" sh -c 'for file in cpu.max memory.max memory.swap.max memory.peak memory.events; do cat "$1/$file"; done' sh "$cgroup" >"$output/operator-cgroup.txt" 2>/dev/null || true
		docker exec "$node" sh -c 'cat "$1/memory.stat"; cat "$1/memory.current"' sh "$cgroup" >"$output/operator-memory-stat.txt" 2>/dev/null || true
		docker exec -i "$node" sh -s -- "$cgroup" "$id" <scripts/ci/operator-process-capture.sh >"$output/operator-process.txt" 2>/dev/null || true
	fi
}
cleanup() {
	capture
	if [[ "$created" == true ]]; then kind delete cluster --name "$cluster" >/dev/null 2>&1 || true; fi
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if kind get clusters 2>/dev/null | grep -Fxq "$cluster"; then
	echo 'operator-floor: refusing an existing cluster' >&2; exit 1
fi
node_image='kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5'
base_image='alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b'
chart_flags=(--namespace operator-floor --set operator.enabled=true --set 'operator.namespaces={operator-floor}' --set image.digest="sha256:$(printf '%064d' 0)" --set rootKey.existingSecret=unused --set database.existingSecret=unused --set tls.existingSecret=unused --set externalOrigin=https://floor.invalid --set upgrade.existingClaim=unused-public --set upgrade.stateExistingClaim=unused-installation)
# Validate chart inputs before provisioning any cluster. Helm validates the
# complete chart even when only operator templates are selected.
# Upgrade claims are schema placeholders only: no server template is rendered
# or deployed by this operator-only measurement.
helm template floor chart/hikyo "${chart_flags[@]}" --show-only templates/operator-rbac.yaml --show-only templates/operator-serviceaccount.yaml >"$work/rbac.yaml"
helm template floor chart/hikyo "${chart_flags[@]}" --show-only templates/operator-deployment.yaml >"$work/deployment.yaml"
echo 'operator-floor: building binaries outside the measurement cgroup'
GOMAXPROCS=2 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -p 1 -trimpath -o "$work/hikyo" ./cmd/hikyo
GOMAXPROCS=2 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -p 1 -trimpath -o "$work/fixture" ./scripts/operator-floor
GOMAXPROCS=2 go build -p 1 -trimpath -o "$work/driver" ./scripts/operator-floor
binary_sha=$(shasum -a 256 "$work/hikyo" | awk '{print $1}')
git diff HEAD --binary >"$output/source.patch"
git status --porcelain >"$output/source-status.txt"
cat >"$work/Dockerfile" <<EOF
FROM $base_image
COPY hikyo /hikyo
COPY fixture /fixture
USER 65532:65532
ENTRYPOINT ["/hikyo"]
EOF
image="hikyo-operator-floor:$$"
docker build --platform linux/arm64 -q -t "$image" "$work" >/dev/null
kind create cluster --name "$cluster" --image "$node_image" --kubeconfig "$kubeconfig" --wait 120s
created=true
# The whole node, including API server, fixture, operator and kubelet, shares
# the floor envelope. Operator receives the narrower shipped limit below.
docker update --cpus 4 --memory 4g --memory-swap 4g "$node" >/dev/null
kind load docker-image --name "$cluster" "$image" >/dev/null
kubectl --kubeconfig "$kubeconfig" apply -f chart/hikyo/crds >/dev/null
kubectl --kubeconfig "$kubeconfig" wait --for=condition=Established crd --all --timeout=60s >/dev/null
kubectl --kubeconfig "$kubeconfig" create namespace operator-floor >/dev/null
# Use the shipped RBAC, watches, security context, resources and leader election.
kubectl --kubeconfig "$kubeconfig" -n operator-floor apply -f "$work/rbac.yaml" >/dev/null
kubectl --kubeconfig "$kubeconfig" -n operator-floor create --dry-run=client -f "$work/deployment.yaml" -o json >"$work/deployment.json"
jq --arg image "$image" '
	.spec.template.spec.containers[0].image = $image |
	.spec.template.spec.containers[0].imagePullPolicy = "Never" |
	.spec.template.spec.containers += [{name:"fixture",image:$image,imagePullPolicy:"Never",command:["/fixture","serve"],resources:{requests:{cpu:"10m",memory:"32Mi"},limits:{cpu:"200m",memory:"128Mi"}},securityContext:{allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]},readOnlyRootFilesystem:true},volumeMounts:[{name:"fixture-tmp",mountPath:"/tmp"}]}] |
	.spec.template.spec.volumes = [{name:"fixture-tmp",emptyDir:{medium:"Memory",sizeLimit:"1Mi"}}]
' "$work/deployment.json" >"$output/deployment.json"
jq -e '.spec.template.spec.containers[0].resources == {requests:{cpu:"50m",memory:"64Mi"},limits:{cpu:"200m",memory:"128Mi"}}' "$output/deployment.json" >/dev/null
kubectl --kubeconfig "$kubeconfig" -n operator-floor apply -f "$output/deployment.json" >/dev/null
kubectl --kubeconfig "$kubeconfig" -n operator-floor rollout status deployment/floor-hikyo-operator --timeout=180s
kubectl --kubeconfig "$kubeconfig" -n operator-floor exec deployment/floor-hikyo-operator -c fixture -- cat /tmp/ca.pem >"$work/ca.pem"
"$work/driver" "$kubeconfig" "$work/ca.pem" "$output/load.json"
capture
"$work/driver" verify-resources "$output" "$binary_sha"
peak=$(jq -er '.operator_memory_peak_bytes' "$output/resource-verification.json")
rss_peak=$(jq -er '.process.rss_peak_bytes' "$output/resource-verification.json")

jq -n --arg commit "$(git rev-parse HEAD)" --arg binary "$binary_sha" --arg diff "$(shasum -a 256 "$output/source.patch" | awk '{print $1}')" --arg image "$(docker image inspect "$image" --format '{{.Id}}')" --arg node "$node_image" --argjson peak "$peak" --argjson rss_peak "$rss_peak" --slurpfile resources "$output/resource-verification.json" --slurpfile load "$output/load.json" '{schema:"hikyo.dev/operator-floor-evidence/v1",source_commit:$commit,source_diff_sha256:$diff,operator_binary_sha256:$binary,operator_image:$image,node_image:$node,architecture:"arm64",outer_cpu:4,outer_memory_bytes:4294967296,operator_cpu_millicores:200,operator_memory_limit_bytes:134217728,operator_memory_peak_bytes:$peak,operator_rss_peak_bytes:$rss_peak,rss_measurement:{source:"kind node /proc/PID/status VmHWM (kB converted to bytes)",reader_location:"outside operator cgroup",process:$resources[0].process},swap_bytes:0,load:$load[0],passed:true}' >"$output/result.json"
echo "operator-floor: passed; operator peak RSS $rss_peak bytes, cgroup peak $peak bytes; evidence $output/result.json"

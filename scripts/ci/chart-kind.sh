#!/usr/bin/env bash
# Install the exact chart and candidate image into a disposable kind cluster.
# The test proves boot, semantic probes, and readiness loss/recovery while the
# liveness endpoint remains healthy and the server container does not restart.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

CLUSTER=hikyo-chart-e2e
NAMESPACE=hikyo-chart-e2e
RELEASE=hikyo
NODE_IMAGE="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"
POSTGRES_IMAGE="postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941"
REGISTRY_IMAGE="registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
REGISTRY_NAME=hikyo-chart-registry
REGISTRY_PORT=5001
IMAGE_REPOSITORY="localhost:$REGISTRY_PORT/hikyo-chart-e2e"
IMAGE_TAG="$IMAGE_REPOSITORY:local"
BINARY=${HIKYO_CHART_KIND_BINARY:-}
PUBLIC_DIR=${HIKYO_CHART_KIND_PUBLIC_DIR:-}

if [[ -z "$BINARY" || ! -f "$BINARY" ]]; then
	echo "chart-kind: HIKYO_CHART_KIND_BINARY must name the candidate Linux binary" >&2
	exit 2
fi
if [[ -z "$PUBLIC_DIR" || ! -f "$PUBLIC_DIR/bundle/index.json" || ! -f "$PUBLIC_DIR/operator.pub" ]]; then
	echo "chart-kind: HIKYO_CHART_KIND_PUBLIC_DIR must name the matching signed fixture bundle and operator public key" >&2
	exit 2
fi
for command in kind kubectl helm docker jq openssl curl go; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "chart-kind: $command not found on PATH" >&2
		exit 2
	fi
done
target_arch=$(docker info --format '{{.Architecture}}')
case "$target_arch" in
	aarch64 | arm64) target_arch=arm64 ;;
	x86_64 | amd64) target_arch=amd64 ;;
	*) echo "chart-kind: unsupported Docker node architecture $target_arch" >&2; exit 2 ;;
esac
binary_arch=$(go version -m "$BINARY" | awk '$1 == "build" && $2 ~ /^GOARCH=/ {sub(/^GOARCH=/,"",$2); print $2}')
if [[ "$binary_arch" != "$target_arch" ]]; then
	echo "chart-kind: candidate binary architecture $binary_arch does not match Docker/kind node $target_arch" >&2
	exit 2
fi
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
	echo "chart-kind: cluster '$CLUSTER' already exists; refusing to reuse or delete it" >&2
	exit 1
fi
if docker container inspect "$REGISTRY_NAME" >/dev/null 2>&1; then
	echo "chart-kind: container '$REGISTRY_NAME' already exists; refusing to reuse or delete it" >&2
	exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-chart-kind.XXXXXX")
kubeconfig="$work/kubeconfig"
kind_config="$work/kind.yaml"
port_forward_pid=
created=false
registry_created=false
cleanup() {
	if [[ -n "$port_forward_pid" ]]; then
		kill "$port_forward_pid" >/dev/null 2>&1 || true
		wait "$port_forward_pid" >/dev/null 2>&1 || true
	fi
	if [[ "$created" == true ]]; then
		kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
	fi
	if [[ "$registry_created" == true ]]; then
		docker rm --force "$REGISTRY_NAME" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

cat >"$kind_config" <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
nodes:
  - role: control-plane
EOF

docker run --detach --restart=always \
	--publish "127.0.0.1:$REGISTRY_PORT:5000" \
	--name "$REGISTRY_NAME" "$REGISTRY_IMAGE" >/dev/null
registry_created=true
echo "chart-kind: creating cluster $CLUSTER"
kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" \
	--config "$kind_config" --kubeconfig "$kubeconfig" --wait 120s
created=true
export KUBECONFIG="$kubeconfig"
docker network connect kind "$REGISTRY_NAME"
registry_dir="/etc/containerd/certs.d/localhost:$REGISTRY_PORT"
while IFS= read -r node; do
	# Copy regular public files into this run's disposable node; projected
	# ConfigMap symlinks deliberately do not bypass the gate's filesystem checks.
	docker exec "$node" mkdir -p /var/lib/hikyo-chart-public /var/lib/hikyo-chart-state
	docker cp "$PUBLIC_DIR/." "$node:/var/lib/hikyo-chart-public/"
	docker exec "$node" chown 65532:65532 /var/lib/hikyo-chart-state
	# Match kubelet's fsGroup root check, including setgid, so a CSI mount
	# never recursively loosens the private custody child's permissions.
	docker exec "$node" chmod 2770 /var/lib/hikyo-chart-state
	docker exec "$node" mkdir -p /var/lib/hikyo-chart-state/operator-custody
	docker exec "$node" chown 65532:65532 /var/lib/hikyo-chart-state/operator-custody
	docker exec "$node" chmod 0700 /var/lib/hikyo-chart-state/operator-custody
	docker exec "$node" mkdir -p "$registry_dir"
	cat <<EOF | docker exec --interactive "$node" sh -c "cat >'$registry_dir/hosts.toml'"
[host."http://$REGISTRY_NAME:5000"]
  capabilities = ["pull", "resolve"]
EOF
done < <(kind get nodes --name "$CLUSTER")
cat <<EOF | kubectl apply --filename - >/dev/null
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:$REGISTRY_PORT"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

mkdir -p "$work/build/image-root/$target_arch"
cp "$BINARY" "$work/build/image-root/$target_arch/hikyo"
cp Dockerfile.release "$work/build/Dockerfile.release"
chmod 0755 "$work/build/image-root/$target_arch/hikyo"
echo "chart-kind: building candidate image"
docker buildx build --load --platform "linux/$target_arch" \
	--build-arg TARGETARCH="$target_arch" \
	--metadata-file "$work/image-metadata.json" \
	--tag "$IMAGE_TAG" \
	--file "$work/build/Dockerfile.release" "$work/build" >/dev/null
image_digest=$(jq -er '."containerimage.digest" | select(test("^sha256:[0-9a-f]{64}$"))' \
	"$work/image-metadata.json")
docker push "$IMAGE_TAG" >/dev/null
pushed_ref=$(docker image inspect --format '{{join .RepoDigests "\n"}}' "$IMAGE_TAG" |
	grep -E "^$IMAGE_REPOSITORY@sha256:[0-9a-f]{64}$" | head -n 1)
image_digest=${pushed_ref##*@}

mkdir -p "$work/tls"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
	-keyout "$work/tls/ca.key" -out "$work/tls/ca.crt" \
	-subj '/CN=Hikyo chart-kind CA' >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes \
	-keyout "$work/tls/tls.key" -out "$work/tls/server.csr" \
	-subj "/CN=postgres.$NAMESPACE.svc" >/dev/null 2>&1
cat >"$work/tls/server.ext" <<EOF
subjectAltName=DNS:postgres,DNS:postgres.$NAMESPACE,DNS:postgres.$NAMESPACE.svc,DNS:postgres.$NAMESPACE.svc.cluster.local
extendedKeyUsage=serverAuth
EOF
openssl x509 -req -days 1 -sha256 \
	-in "$work/tls/server.csr" \
	-CA "$work/tls/ca.crt" -CAkey "$work/tls/ca.key" -CAcreateserial \
	-extfile "$work/tls/server.ext" -out "$work/tls/tls.crt" >/dev/null 2>&1
chmod 0400 "$work/tls/tls.key"
openssl rand -hex 32 >"$work/root-key"
chmod 0400 "$work/root-key"

kubectl create namespace "$NAMESPACE" >/dev/null
for custody in public state; do
	cat <<EOF | kubectl apply --filename - >/dev/null
apiVersion: v1
kind: PersistentVolume
metadata:
  name: hikyo-chart-upgrade-$custody
spec:
  capacity: {storage: 1Gi}
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: hikyo-chart-kind
  hostPath:
    path: /var/lib/hikyo-chart-$custody
    type: Directory
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: hikyo-upgrade-$custody
  namespace: $NAMESPACE
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: hikyo-chart-kind
  volumeName: hikyo-chart-upgrade-$custody
  resources:
    requests: {storage: 1Gi}
EOF
done
kubectl --namespace "$NAMESPACE" create secret generic postgres-auth \
	--from-literal=username=hikyo \
	--from-literal=password=hikyo \
	--from-literal=database=hikyo >/dev/null
kubectl --namespace "$NAMESPACE" create secret generic postgres-tls \
	--from-file=tls.crt="$work/tls/tls.crt" \
	--from-file=tls.key="$work/tls/tls.key" >/dev/null
kubectl --namespace "$NAMESPACE" create secret generic hikyo-database-ca \
	--from-file=ca.crt="$work/tls/ca.crt" >/dev/null
kubectl --namespace "$NAMESPACE" create secret generic hikyo-root-key \
	--from-file=root-key="$work/root-key" >/dev/null
database_dsn="postgres://hikyo:hikyo@postgres.$NAMESPACE.svc:5432/hikyo?sslmode=verify-full&sslrootcert=/run/hikyo-database-ca/ca.crt"
kubectl --namespace "$NAMESPACE" create secret generic hikyo-database \
	--from-literal=HIKYO_DB="$database_dsn" >/dev/null

cat >"$work/postgres.yaml" <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: hikyo-chart-postgres
spec:
  capacity:
    storage: 1Gi
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: hikyo-chart-kind
  hostPath:
    path: /var/lib/hikyo-chart-postgres
    type: DirectoryOrCreate
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-data
  namespace: $NAMESPACE
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: hikyo-chart-kind
  volumeName: hikyo-chart-postgres
  resources:
    requests:
      storage: 1Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
  namespace: $NAMESPACE
spec:
  replicas: 1
  selector:
    matchLabels: {app: postgres}
  template:
    metadata:
      labels: {app: postgres}
    spec:
      securityContext:
        fsGroup: 999
        fsGroupChangePolicy: OnRootMismatch
      containers:
        - name: postgres
          image: $POSTGRES_IMAGE
          args:
            - -c
            - ssl=on
            - -c
            - ssl_cert_file=/etc/postgres-tls/tls.crt
            - -c
            - ssl_key_file=/etc/postgres-tls/tls.key
          env:
            - name: POSTGRES_USER
              valueFrom: {secretKeyRef: {name: postgres-auth, key: username}}
            - name: POSTGRES_PASSWORD
              valueFrom: {secretKeyRef: {name: postgres-auth, key: password}}
            - name: POSTGRES_DB
              valueFrom: {secretKeyRef: {name: postgres-auth, key: database}}
          ports:
            - {name: postgres, containerPort: 5432}
          readinessProbe:
            exec: {command: [pg_isready, -U, hikyo, -d, hikyo]}
            periodSeconds: 2
            failureThreshold: 30
          volumeMounts:
            - {name: data, mountPath: /var/lib/postgresql}
            - {name: tls, mountPath: /etc/postgres-tls, readOnly: true}
      volumes:
        - name: data
          persistentVolumeClaim: {claimName: postgres-data}
        - name: tls
          secret:
            secretName: postgres-tls
            defaultMode: 0440
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: $NAMESPACE
spec:
  selector: {app: postgres}
  ports:
    - {name: postgres, port: 5432, targetPort: postgres}
EOF
kubectl apply --filename "$work/postgres.yaml" >/dev/null
kubectl --namespace "$NAMESPACE" rollout status deployment/postgres --timeout=120s

chart_values=(
	--set operator.enabled=false \
	--set image.repository="$IMAGE_REPOSITORY" \
	--set image.digest="$image_digest" \
	--set database.existingSecret=hikyo-database \
	--set database.tls.existingSecret=hikyo-database-ca \
	--set rootKey.existingSecret=hikyo-root-key \
	--set upgrade.existingClaim=hikyo-upgrade-public \
	--set upgrade.stateExistingClaim=hikyo-upgrade-state \
	--set externalOrigin=http://127.0.0.1:18080 \
	--set network.allowPlaintextOrigin=true \
	--set 'network.trustedProxyCIDRs={10.0.0.0/8}'
)

# Retained broken-shape fixture: removing the root-key file argument recreates
# the pre-#200 chart boot failure. The real cluster must refuse readiness and
# logs must name the missing source before the fixed chart is installed.
cp -R chart/hikyo "$work/broken-chart"
sed '/--root-key-file=\/run\/hikyo-root-key\/root-key/d' \
	"$work/broken-chart/templates/deployment.yaml" >"$work/broken-deployment.yaml"
mv "$work/broken-deployment.yaml" "$work/broken-chart/templates/deployment.yaml"
helm install broken "$work/broken-chart" --namespace "$NAMESPACE" "${chart_values[@]}" >/dev/null
if kubectl --namespace "$NAMESPACE" wait --for=condition=Ready pod \
	--selector app.kubernetes.io/instance=broken --timeout=20s >/dev/null 2>&1; then
	echo "chart-kind: pre-#200 deployment unexpectedly became Ready" >&2
	exit 1
fi
broken_log=
for _ in {1..20}; do
	broken_log=$(kubectl --namespace "$NAMESPACE" logs deployment/broken-hikyo \
		--container server --tail=30 2>&1 || true)
	if grep -Fq 'no root key configured' <<<"$broken_log"; then
		break
	fi
	sleep 1
done
if ! grep -Fq 'no root key configured' <<<"$broken_log"; then
	echo "chart-kind: broken-shape fixture failed for an unexpected reason" >&2
	echo "$broken_log" >&2
	kubectl --namespace "$NAMESPACE" get pods --selector app.kubernetes.io/instance=broken -o json |
		jq '[.items[] | {phase:.status.phase,init:[.status.initContainerStatuses[]? | {name,state}]}]' >&2
	exit 1
fi
helm uninstall broken --namespace "$NAMESPACE" --wait >/dev/null

echo "chart-kind: installing exact chart and image $IMAGE_REPOSITORY@$image_digest"
helm install "$RELEASE" chart/hikyo --namespace "$NAMESPACE" "${chart_values[@]}" >/dev/null
kubectl --namespace "$NAMESPACE" rollout status deployment/$RELEASE-hikyo --timeout=180s
kubectl --namespace "$NAMESPACE" wait --for=condition=Ready pod \
	--selector app.kubernetes.io/instance="$RELEASE" --timeout=180s >/dev/null

deployed_image=$(kubectl --namespace "$NAMESPACE" get deployment "$RELEASE-hikyo" \
	-o jsonpath='{.spec.template.spec.containers[?(@.name=="server")].image}')
if [[ "$deployed_image" != "$IMAGE_REPOSITORY@$image_digest" ]]; then
	echo "chart-kind: deployed image $deployed_image does not match candidate" >&2
	exit 1
fi

kubectl --namespace "$NAMESPACE" port-forward deployment/$RELEASE-hikyo \
	18080:8080 18081:8081 >"$work/port-forward.log" 2>&1 &
port_forward_pid=$!
for _ in {1..30}; do
	if curl --fail --silent http://127.0.0.1:18081/readyz >/dev/null; then
		break
	fi
	sleep 1
done
curl --fail --silent http://127.0.0.1:18081/readyz >/dev/null
curl --fail --silent http://127.0.0.1:18081/healthz >/dev/null

# Bootstrap actual chart authority through the supported local-admin command.
# Reuse the candidate image, datastore, and protected mounts in short-lived
# Jobs. admin takes the existing root source through a Secret reference; no
# root value or establishment authority appears in command arguments or logs.
doctor_private="$work/doctor-private"
mkdir -m 0700 "$doctor_private"
kubectl --namespace "$NAMESPACE" get deployment "$RELEASE-hikyo" -o json >"$work/doctor-deployment.json"
chart_admin() {
	local name=$1 args=$2
	jq --arg name "$name" --arg namespace "$NAMESPACE" --argjson args "$args" '
		.spec.template as $template |
		{apiVersion:"batch/v1",kind:"Job",metadata:{name:$name,namespace:$namespace},
		 spec:{backoffLimit:0,activeDeadlineSeconds:90,template:($template |
			.metadata={labels:{app:"hikyo-chart-doctor"}} |
			.spec.restartPolicy="Never" |
			del(.spec.initContainers) |
			.spec.containers |= map(select(.name=="server") |
				.args=$args |
				.env += [{name:"HIKYO_ROOT_KEY",valueFrom:{secretKeyRef:{name:"hikyo-root-key",key:"root-key"}}}] |
				del(.ports,.livenessProbe,.readinessProbe,.startupProbe)))}}
	' "$work/doctor-deployment.json" >"$work/$name.json"
	kubectl apply --filename "$work/$name.json" >/dev/null
	if ! kubectl --namespace "$NAMESPACE" wait --for=condition=Complete "job/$name" --timeout=100s >/dev/null; then
		kubectl --namespace "$NAMESPACE" logs "job/$name" >"$doctor_private/$name.log" 2>&1 || true
		echo "chart-kind: supported admin bootstrap job $name failed" >&2
		return 1
	fi
	kubectl --namespace "$NAMESPACE" logs "job/$name" >"$doctor_private/$name.log" 2>&1
}
chart_admin chart-doctor-create \
	'["admin","create","--username","chart-doctor","--display-name","Chart Doctor","--output-file","/var/lib/hikyo-upgrade/operator-custody/chart-doctor-authority"]'
doctor_principal=$(sed -n 's/.*principal \([^)]*\)).*/\1/p' "$doctor_private/chart-doctor-create.log")
if [[ -z "$doctor_principal" ]]; then
	echo 'chart-kind: admin bootstrap did not report a principal' >&2
	exit 1
fi
chart_admin chart-doctor-grant "$(jq -cn --arg principal "$doctor_principal" \
	'["admin","grant","--principal",$principal,"--capability","instance-config"]')"
# The authority was written 0600 into this run's owned node-backed private
# custody directory. Copy it privately; the distroless image needs no shell.
doctor_node=$(kind get nodes --name "$CLUSTER")
umask 077
docker exec "$doctor_node" cat /var/lib/hikyo-chart-state/operator-custody/chart-doctor-authority \
	>"$doctor_private/authority"
go build -trimpath -o "$work/hikyo-cli" ./cmd/hikyo
go run ./scripts/ci/chartdoctor --origin http://127.0.0.1:18080 \
	--private-dir "$doctor_private" --binary "$work/hikyo-cli"
if ! jq -e --arg engine postgres --arg volume_severity unknown -f scripts/ci/assert-doctor-findings.jq "$doctor_private/doctor.json" >/dev/null; then
	echo 'chart-kind: instance doctor did not report the complete measured finding set' >&2
	jq '{status,findings:[.findings[]|{code,severity}]}' "$doctor_private/doctor.json" >&2
	exit 1
fi
echo 'chart-kind: authenticated instance doctor reported all 12 operational finding families'

if ! document=$(curl --fail --silent --show-error \
	--header 'Accept: text/html' http://127.0.0.1:18080/); then
	echo "chart-kind: UI document request failed" >&2
	cat "$work/port-forward.log" >&2
	exit 1
fi
if [[ ! "$document" =~ \<\!doctype[[:space:]]+html|\<html ]]; then
	echo "chart-kind: UI root did not return an HTML document" >&2
	printf '%s\n' "$document" >&2
	exit 1
fi

pod=$(kubectl --namespace "$NAMESPACE" get pod \
	--selector app.kubernetes.io/instance="$RELEASE" -o jsonpath='{.items[0].metadata.name}')
restarts_before=$(kubectl --namespace "$NAMESPACE" get pod "$pod" \
	-o jsonpath='{.status.containerStatuses[?(@.name=="server")].restartCount}')
kubectl --namespace "$NAMESPACE" scale deployment/postgres --replicas=0 >/dev/null
for _ in {1..30}; do
	ready=$(kubectl --namespace "$NAMESPACE" get pod "$pod" \
		-o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')
	[[ "$ready" == "False" ]] && break
	sleep 1
done
if [[ "$ready" != "False" ]]; then
	echo "chart-kind: database outage did not remove pod readiness within 30 seconds" >&2
	exit 1
fi
if ! ready_status=$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
	http://127.0.0.1:18081/readyz); then
	echo "chart-kind: /readyz during database outage failed before returning an HTTP status" >&2
	cat "$work/port-forward.log" >&2
	exit 1
fi
if [[ "$ready_status" != 503 ]]; then
	echo "chart-kind: /readyz during database outage returned $ready_status, want 503" >&2
	exit 1
fi
curl --fail --silent http://127.0.0.1:18081/healthz >/dev/null
restarts_during_outage=$(kubectl --namespace "$NAMESPACE" get pod "$pod" \
	-o jsonpath='{.status.containerStatuses[?(@.name=="server")].restartCount}')
if [[ "$restarts_during_outage" != "$restarts_before" ]]; then
	echo "chart-kind: liveness restarted server during database outage" >&2
	exit 1
fi

kubectl --namespace "$NAMESPACE" scale deployment/postgres --replicas=1 >/dev/null
kubectl --namespace "$NAMESPACE" rollout status deployment/postgres --timeout=120s
kubectl --namespace "$NAMESPACE" wait --for=condition=Ready pod "$pod" --timeout=90s >/dev/null
curl --fail --silent http://127.0.0.1:18081/readyz >/dev/null
restarts_after=$(kubectl --namespace "$NAMESPACE" get pod "$pod" \
	-o jsonpath='{.status.containerStatuses[?(@.name=="server")].restartCount}')
if [[ "$restarts_after" != "$restarts_before" ]]; then
	echo "chart-kind: server restarted across readiness failure/recovery" >&2
	exit 1
fi

echo "chart-kind: exact chart/image booted; database outage toggled readiness without liveness restart"

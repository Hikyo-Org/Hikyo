#!/usr/bin/env bash
# Actual distroless singleton replacement with PostgreSQL in an owned kind cluster.
# Acquisition fixture: signed test releases and offline descriptors are supplied
# by scripts/ci/unattendedfixture, never production release credentials.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
fixture=${HIKYO_UNATTENDED_FIXTURE_OUTPUT:?set the signed A/B/C fixture directory}
for tool in kind kubectl helm docker jq openssl python3 grep; do
	command -v "$tool" >/dev/null || { echo "unattended-kind: missing $tool" >&2; exit 2; }
done
python3 -c 'import yaml' || { echo 'unattended-kind: Python requires PyYAML' >&2; exit 2; }
for release in a b c; do
	test -f "$fixture/release-$release/hikyo"
	test -f "$fixture/descriptor-$release.json"
done
test -f "$fixture/root.key"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-unattended-kind.XXXXXX")
CLUSTER="hikyo-unattended-$(date +%s)-$$"
NAMESPACE=hikyo-unattended
NODE_IMAGE='kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5'
POSTGRES_IMAGE='postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941'
kubeconfig="$work/kubeconfig"
export KUBECONFIG="$kubeconfig"
created=false
built_images=()
cleanup() {
	result=$?
	if [[ "$created" == true ]]; then
		if [[ "$result" != 0 ]]; then
			kubectl --request-timeout=5s -n "$NAMESPACE" get pods >&2 || true
			kubectl --request-timeout=5s -n "$NAMESPACE" logs deployment/hikyo-hikyo -c server --tail=80 >&2 || true
		fi
		kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
	fi
	for image in "${built_images[@]:-}"; do
		if [[ -n "$image" ]]; then docker image rm "$image" >/dev/null 2>&1 || true; fi
	done
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
if kind get clusters 2>/dev/null | grep -Fxq "$CLUSTER"; then
	echo 'unattended-kind: refusing existing cluster' >&2; exit 1
fi
case "$(docker info --format '{{.Architecture}}')" in
	arm64|aarch64) arch=arm64 ;;
	amd64|x86_64) arch=amd64 ;;
	*) echo 'unattended-kind: unsupported Docker architecture' >&2; exit 2 ;;
esac
cat >"$work/kind.yaml" <<'YAML'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
YAML
# Mark ownership before create so a partially created cluster is also reclaimed.
created=true
kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" --config "$work/kind.yaml" --kubeconfig "$kubeconfig" --wait 180s
node=$(kind get nodes --name "$CLUSTER")
docker exec "$node" mkdir -p /var/lib/hikyo-unattended-public /var/lib/hikyo-unattended-state/operator-custody/unattended
docker cp "$fixture/public/." "$node:/var/lib/hikyo-unattended-public/" >/dev/null
docker exec "$node" chown -R 65532:65532 /var/lib/hikyo-unattended-public
docker exec "$node" chown 0:65532 /var/lib/hikyo-unattended-state
docker exec "$node" chown -R 65532:65532 /var/lib/hikyo-unattended-state/operator-custody
# Root-owned sticky parent protects private custody from other fsGroup members;
# setgid and group permissions also satisfy kubelet's OnRootMismatch check.
docker exec "$node" chmod 3770 /var/lib/hikyo-unattended-state
docker exec "$node" chmod 0700 /var/lib/hikyo-unattended-state/operator-custody /var/lib/hikyo-unattended-state/operator-custody/unattended
for release in a b c; do
	mkdir -p "$work/build/image-root/$arch"
	cp "$fixture/release-$release/hikyo" "$work/build/image-root/$arch/hikyo"
	chmod 0755 "$work/build/image-root/$arch/hikyo"
	cp Dockerfile.release "$work/build/Dockerfile.release"
	docker buildx build --load --platform "linux/$arch" --build-arg TARGETARCH="$arch" --tag "$CLUSTER:$release" --file "$work/build/Dockerfile.release" "$work/build" >/dev/null
	built_images+=("$CLUSTER:$release")
	kind load docker-image --name "$CLUSTER" "$CLUSTER:$release" >/dev/null
done
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
cp "$fixture/root.key" "$work/root-key"
chmod 0400 "$work/root-key"


kubectl create namespace "$NAMESPACE" >/dev/null
for custody in public state; do
cat <<YAML | kubectl apply -f - >/dev/null
apiVersion: v1
kind: PersistentVolume
metadata: {name: hikyo-unattended-$custody}
spec:
  capacity: {storage: 1Gi}
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: hikyo-unattended-kind
  hostPath: {path: /var/lib/hikyo-unattended-$custody, type: Directory}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: hikyo-upgrade-$custody, namespace: $NAMESPACE}
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: hikyo-unattended-kind
  volumeName: hikyo-unattended-$custody
  resources: {requests: {storage: 1Gi}}
YAML
done
kubectl -n "$NAMESPACE" create secret generic postgres-auth --from-literal=username=hikyo --from-literal=password=hikyo --from-literal=database=hikyo >/dev/null
kubectl -n "$NAMESPACE" create secret generic postgres-tls --from-file=tls.crt="$work/tls/tls.crt" --from-file=tls.key="$work/tls/tls.key" >/dev/null
kubectl -n "$NAMESPACE" create secret generic hikyo-database-ca --from-file=ca.crt="$work/tls/ca.crt" >/dev/null
kubectl -n "$NAMESPACE" create secret generic hikyo-root-key --from-file=root-key="$work/root-key" >/dev/null
database_dsn="postgres://hikyo:hikyo@postgres.$NAMESPACE.svc:5432/hikyo?sslmode=verify-full&sslrootcert=/run/hikyo-database-ca/ca.crt"
scratch_dsn="postgres://hikyo:hikyo@postgres.$NAMESPACE.svc:5432/hikyo_scratch?sslmode=verify-full&sslrootcert=/run/hikyo-database-ca/ca.crt"
kubectl -n "$NAMESPACE" create secret generic hikyo-database --from-literal=HIKYO_DB="$database_dsn" >/dev/null
kubectl -n "$NAMESPACE" create secret generic hikyo-scratch --from-literal=HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN="$scratch_dsn" >/dev/null
cat >"$work/postgres.yaml" <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: hikyo-unattended-postgres
spec:
  capacity:
    storage: 1Gi
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: hikyo-unattended-kind
  hostPath:
    path: /var/lib/hikyo-unattended-postgres
    type: DirectoryOrCreate
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-data
  namespace: $NAMESPACE
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: hikyo-unattended-kind
  volumeName: hikyo-unattended-postgres
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
            # -h 127.0.0.1 forces a TCP check: postgres' initdb bootstrap runs a
            # temporary socket-only server (listen_addresses='') that a socket
            # probe accepts, marking the pod Ready before the real server is up —
            # then the createdb below races the socket and fails. TCP can only see
            # the real server.
            exec: {command: [pg_isready, -h, 127.0.0.1, -U, hikyo, -d, hikyo]}
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


kubectl -n "$NAMESPACE" exec deployment/postgres -- createdb -U hikyo hikyo_scratch
chart_values=(
 --set operator.enabled=false
 --set image.repository="$CLUSTER"
 --set image.digest="sha256:$(printf '%064d' 0)"
 --set database.existingSecret=hikyo-database
 --set database.tls.existingSecret=hikyo-database-ca
 --set rootKey.existingSecret=hikyo-root-key
 --set upgrade.stateExistingClaim=hikyo-upgrade-state
 --set upgrade.unattended.enabled=true
 --set upgrade.unattended.scratchDatabaseExistingSecret=hikyo-scratch
 --set externalOrigin=http://127.0.0.1:18080
 --set network.allowPlaintextOrigin=true
 --set 'network.trustedProxyCIDRs={10.0.0.0/8}'
)
helm template hikyo chart/hikyo --namespace "$NAMESPACE" "${chart_values[@]}" >"$work/chart.yaml"
# Fixture-only overlay: locally loaded tagged image plus read-only authenticated
# release acquisition cache. Runtime args, probes, security and PVCs remain chart-owned.
python3 scripts/ci/unattended-kind-render.py "$work/chart.yaml" "$CLUSTER:a" "$work/deployment.json"
kubectl -n "$NAMESPACE" apply -f "$work/deployment.json" >/dev/null
seed_descriptor() {
 local release=$1
 docker exec "$node" mkdir -p /var/lib/hikyo-unattended-state/operator-custody/unattended/downloads
 docker cp "$fixture/descriptor-$release.json" "$node:/var/lib/hikyo-unattended-state/operator-custody/unattended/image.json" >/dev/null
 docker exec "$node" chown -R 65532:65532 /var/lib/hikyo-unattended-state/operator-custody
 docker exec "$node" chmod 0600 /var/lib/hikyo-unattended-state/operator-custody/unattended/image.json
 docker exec "$node" chmod 0700 /var/lib/hikyo-unattended-state/operator-custody/unattended/downloads
}
read_state() {
 kubectl -n "$NAMESPACE" exec deployment/postgres -- psql -U hikyo -d hikyo -Atc  "SELECT json_build_object('instance',c.instance_id,'applied',c.applied_json::json,'generation',c.generation,'maintenance',(c.maintenance=1),'pending',p.operation_json::json) FROM upgrade_control c JOIN upgrade_pending p USING(singleton)"
}
assert_ready() {
 local release=$1
 kubectl -n "$NAMESPACE" rollout status deployment/hikyo-hikyo --timeout=240s
 kubectl -n "$NAMESPACE" wait --for=condition=Ready pod -l app.kubernetes.io/instance=hikyo --timeout=120s >/dev/null
 read_state >"$work/state-$release.json"
 jq -e --slurpfile expected "$fixture/descriptor-$release.json" '.maintenance == false and .pending.phase == "healthy" and .applied.release == $expected[0].Identity' "$work/state-$release.json" >/dev/null
 kubectl -n "$NAMESPACE" get deployment hikyo-hikyo -o json | jq -e '.spec.replicas == 1 and .spec.strategy.type == "Recreate" and .spec.template.spec.automountServiceAccountToken == false' >/dev/null
 kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/instance=hikyo -o json | jq -e '.items | length == 1' >/dev/null
}
replace() {
 local release=$1
 kubectl -n "$NAMESPACE" scale deployment/hikyo-hikyo --replicas=0 >/dev/null
 kubectl -n "$NAMESPACE" wait --for=delete pod -l app.kubernetes.io/instance=hikyo --timeout=120s >/dev/null
 seed_descriptor "$release"
 kubectl -n "$NAMESPACE" set image deployment/hikyo-hikyo server="$CLUSTER:$release" root-key-stage="$CLUSTER:$release" >/dev/null
 kubectl -n "$NAMESPACE" scale deployment/hikyo-hikyo --replicas=1 >/dev/null
 assert_ready "$release"
}
replace a
instance=$(jq -r .instance "$work/state-a.json")
custody_before=$(docker exec "$node" sha256sum /var/lib/hikyo-unattended-state/operator-custody/unattended/custody/operator.age | cut -d ' ' -f1)
# Same image pod recreation retains instance/custody and never starts a new upgrade.
kubectl -n "$NAMESPACE" rollout restart deployment/hikyo-hikyo >/dev/null
assert_ready a
jq -e --arg instance "$instance" '.instance == $instance' "$work/state-a.json" >/dev/null
for release in b c; do
 replace "$release"
 jq -e --arg instance "$instance" '.instance == $instance' "$work/state-$release.json" >/dev/null
 docker exec "$node" cat /var/lib/hikyo-unattended-state/operator-custody/unattended/operation.json >"$work/journal-$release.json"
 jq -e '.phase == "complete" and .runtime.CiphertextPath != ""' "$work/journal-$release.json" >/dev/null
 ciphertext=$(jq -r '.runtime.CiphertextPath' "$work/journal-$release.json")
 # Map only the fixed chart state mount into this disposable node's owned PV.
 [[ "$ciphertext" == /var/lib/hikyo-upgrade/operator-custody/unattended/backup-*/* ]]
 docker exec "$node" test -s "${ciphertext/\/var\/lib\/hikyo-upgrade/\/var\/lib\/hikyo-unattended-state}"
done
custody_after=$(docker exec "$node" sha256sum /var/lib/hikyo-unattended-state/operator-custody/unattended/custody/operator.age | cut -d ' ' -f1)
[[ "$custody_before" == "$custody_after" ]]
kubectl -n "$NAMESPACE" exec deployment/postgres -- psql -U hikyo -d hikyo_scratch -Atc 'SELECT count(*) FROM upgrade_control' | grep -Fxq '1'
echo 'unattended-kind: distroless A restart and A->B->C passed; root/custody PVC retained; PostgreSQL scratch restore and encrypted backups verified'

#!/usr/bin/env bash
# Automatic security updates on a real arm64 Kubernetes cluster (issue #1068), from the Helm chart.
#
#   scripts/ci/security-pi-rehearsal.sh KUBECONFIG
#
# Everything runs in ONE new namespace, named below, and the script refuses to start if it exists.
# Not wired into CI. It needs: kubectl, helm, docker (buildx, arm64), go, jq, openssl, python3 with
# PyYAML, curl, ssh to the node, and a containerd the node's user may import images into.
#
# Settings (environment, all optional):
#   HIKYO_PI_NAMESPACE   namespace to create (default hikyo-1068-pi)
#   HIKYO_PI_NODE        node name to run on (default: the first Ready, schedulable arm64 worker)
#   HIKYO_PI_SSH         ssh target of that node (default: the node name)
#   HIKYO_PI_CTR         containerd client on the node (default: k3s ctr -n k8s.io)
#   HIKYO_PI_HEAVY       command prefix for builds, e.g. "agent-heavy run --label x --"
#   HIKYO_PI_WORK        work directory; an existing fixture/ and ca/ inside it are reused
#   HIKYO_PI_DEADLINE_SECONDS  the coordinator's operation deadline in seconds (default 240; the product's is 1800)
#   HIKYO_PI_PORT        local port of the port-forward to the server (default: a free port)
#   HIKYO_PI_KEEP=1      keep the namespace and imported images for inspection (remove them by name)
#
# REAL: the Helm chart (a real `helm install --wait` with unattended upgrades and security maintenance
#   at their chart defaults), the coordinator CronJob, its ServiceAccount and RBAC, the enrollment hook
#   (rendered by `helm template --show-only`, because Helm gives hooks to no post-renderer, and applied
#   with kubectl),
#   the server Deployment, PostgreSQL with TLS, the chart's staging Pod and rollback one-shot, the
#   server and coordinator binaries (stamped nightly release binaries, built for arm64), release
#   verification, the feed publisher commands (`security-maintenance feed ...` signing under
#   ephemeral feed keys), the feed client, the control row, the backup, scratch restore and rollback.
# SUBSTITUTED: the trust (an ephemeral release root and an ephemeral feed root, never a production
#   key), GitHub, ghcr.io and hikyo.app (one stand-in Pod answers all three names under a private CA,
#   reached through hostAliases that a Helm post-renderer adds to the chart's pod templates), the
#   registry of the node (the images are imported with containerd, because the registry fronted by
#   Cloudflare caps an upload at about 100 MB), a coordinator deadline of TIMEOUT seconds instead of
#   1800, the release channel (nightly, for the fixture releases), a maintenance window that is
#   closed so that only the critical bypass opens it, and the forced failures: a watcher on the node
#   that reads the update journal from the state volume and stops the server container with
#   `crictl stop -t 0` at the first schema-applied or hop-healthy phase (a failure after the database
#   write), then keeps deleting the target's Pods; and a killed coordinator container (crictl stop -t 0, then its Pod deleted). The detection
#   and the rollback are real. The coordinator's retry backoff is moved by editing its status file
#   from a helper Pod. The stand-in answers on 11.0.0.2 (a Service with externalIPs), because the
#   server refuses non-public destinations. Memory-backed emptyDirs hold the stand-in's work files,
#   because a node whose kubelet directory is NFS cannot fsync a directory there.
# Helper Pods, all removed with the namespace: hx (operator commands from the Deployment's pod spec,
#   because the server image has no shell), bundle-probe and rewind-backoff (the state volumes).
# HIKYO_PI_SKIP_S3, _S4B, _S4C and _S5 =1 skip those scenarios (S4 alone: HIKYO_PI_SKIP_S3=1 HIKYO_PI_SKIP_S4B=1
#   HIKYO_PI_SKIP_S4C=1 HIKYO_PI_SKIP_S5=1 HIKYO_PI_SKIP_TICK=1).
# HIKYO_PI_SKIP_TICK=1 skips the wait for a natural CronJob tick at the end of the install scenario.
# S4 (wedged target), S4b (a target that cannot be observed) and S4c (a coordinator killed while the rollback
#   one-shot runs) are the later-run rollbacks: the deadline passes, a later run ends the operation, and each ends
#   with the source serving, the operation unpinned, the reason operation_rolled_back and the data intact.
# The stop proof of every rollback one-shot has two sources: one API-server watch stream (stop_proof), and the
#   node's container runtime polled about twice a second (watch_start), which also sees a force-deleted Pod's container.
# S5 freezes a target that passed the final gate, so that it is not Ready past the deadline, and only then lets it recover.
# The coordinator's state is a k3s local-path PVC, whose root is 0777 without the sticky bit, so the
#   rehearsal sets securityMaintenance.coordinator.acceptWorldWritableVolumeRoot=true, as a k3s operator would.
# NOT PROVEN: a pin to a second node (everything runs on one node, because the state volume is
#   node-local), another storage class, NFS file locking, and amd64.
#
# Resources, by exact name, all in the namespace unless said otherwise:
#   Secrets      postgres-auth postgres-tls hikyo-database-ca hikyo-database hikyo-scratch
#                hikyo-root-key standin-ca
#   PVCs         postgres-data hikyo-upgrade-state hikyo-hikyo-security-maintenance-state
#   Deployments  postgres, hikyo-hikyo        Pods  standin, hikyo-hikyo-security-stage (transient)
#   Services     postgres, standin, hikyo-hikyo
#   Job          state-contract; derived coordinator Jobs hikyo-security-maintenance-op-*
#   Helm release hikyo (chart/hikyo): CronJob/ServiceAccount/Role/RoleBinding/ConfigMaps
#                hikyo-hikyo-security-maintenance and hikyo-hikyo-security-status
#   On the node  images ghcr.io/hikyo-1068-pi/hikyo:1.1.0-nightly.{1,2,3} (and their @sha256 names)
#                and localhost/hikyo-1068-pi/standin:1, recorded when imported and removed by
#                exact reference; the local-path directories of the PVCs go with the namespace.
set -euo pipefail

NS=${HIKYO_PI_NAMESPACE:-hikyo-1068-pi}
REPO=ghcr.io/hikyo-1068-pi/hikyo
STANDIN_IMAGE=localhost/hikyo-1068-pi/standin:1
POSTGRES_IMAGE='docker.io/library/postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941'
BUSYBOX_IMAGE='docker.io/library/busybox:1.37'
RELEASE=hikyo
SERVER=hikyo-hikyo
CRON=hikyo-hikyo-security-maintenance
STAGE=hikyo-hikyo-security-stage
COORD_PVC=hikyo-hikyo-security-maintenance-state
TIMEOUT=${HIKYO_PI_DEADLINE_SECONDS:-240}
REPO_ROOT=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
SELF_DIR=$REPO_ROOT/scripts/ci

PASSES=0
DEFECTS=0
log() { printf '\n== %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
defect() { printf 'DEFECT: %s\n' "$*"; DEFECTS=$((DEFECTS + 1)); }
# expect WHAT WANT GOT: an assertion that prints what it compared.
expect() {
	if [ "$2" = "$3" ]; then PASSES=$((PASSES + 1)); printf 'PASS: %s (%s)\n' "$1" "$3"; else defect "$1: want '$2', got '$3'"; fi
}
# check WHAT COMMAND...: an assertion on a command's status.
check() {
	local what=$1
	shift
	if "$@" >/dev/null 2>&1; then PASSES=$((PASSES + 1)); printf 'PASS: %s\n' "$what"; else defect "$what"; fi
}

kc() { kubectl --kubeconfig "$KC" -n "$NS" "$@"; }
kca() { kubectl --kubeconfig "$KC" "$@"; }
node_ssh() { ssh -o BatchMode=yes "$SSH" "$@"; }
ctr() { node_ssh "$CTR $*"; }
iso() { python3 -c 'import sys,datetime;print((datetime.datetime.now(datetime.UTC)+datetime.timedelta(seconds=int(sys.argv[1]))).strftime("%Y-%m-%dT%H:%M:%SZ"))' "$1"; }
version_of() { jq -r .Identity.version "$FIXTURE/descriptor-$1.json"; }
digest_of() { tr -d '\n' <"$FIXTURE/public/release-$1/image-index.digest"; }
image_of() { printf '%s@%s' "$REPO" "$(digest_of "$1")"; }
psql_hikyo() { kc exec deployment/postgres -- psql -U hikyo -d hikyo -Atc "$1"; }

# ---------------------------------------------------------------------------------------------
# Preflight: the namespace must not exist, and the node must be a Ready arm64 one.
# ---------------------------------------------------------------------------------------------
preflight() {
	for tool in kubectl helm docker go jq openssl python3 curl ssh; do
		command -v "$tool" >/dev/null || fail "missing $tool"
	done
	python3 -c 'import yaml' || fail 'python3 needs PyYAML'
	[ -f "$KC" ] || fail "kubeconfig $KC does not exist"
	if kca get namespace "$NS" >/dev/null 2>&1; then fail "namespace $NS already exists; this script only runs in a namespace it creates"; fi
	# Flux ignores a namespace it did not create, but a Kustomization naming this one would still claim it.
	if kca get kustomizations,helmreleases -A -o json 2>/dev/null | jq -e --arg ns "$NS" '[.items[] | select(([.. | strings] | index($ns)) != null)] | length > 0' >/dev/null; then
		fail "a Flux object names namespace $NS"
	fi
	if [ -z "${HIKYO_PI_NODE:-}" ]; then
		HIKYO_PI_NODE=$(kca get nodes -o json | jq -r '[.items[] | select(.status.conditions[] | select(.type == "Ready" and .status == "True")) | select((.spec.unschedulable // false) | not) | select(.status.nodeInfo.architecture == "arm64") | select((.metadata.labels["node-role.kubernetes.io/control-plane"] // "") == "")][0].metadata.name // empty')
	fi
	[ -n "$HIKYO_PI_NODE" ] || fail 'no Ready, schedulable arm64 worker node'
	NODE=$HIKYO_PI_NODE
	SSH=${HIKYO_PI_SSH:-$NODE}
	CTR=${HIKYO_PI_CTR:-k3s ctr -n k8s.io}
	[ "$(kca get node "$NODE" -o jsonpath='{.status.nodeInfo.architecture}')" = arm64 ] || fail "node $NODE is not arm64"
	node_ssh true || fail "cannot ssh to $SSH"
	printf 'cluster: %s, node %s (%s), namespace %s\n' "$(kca config view --minify -o jsonpath='{.clusters[0].cluster.server}')" "$NODE" "$(kca get node "$NODE" -o jsonpath='{.status.nodeInfo.kubeletVersion} {.status.nodeInfo.containerRuntimeVersion}')" "$NS"
}

# ---------------------------------------------------------------------------------------------
# Cleanup: the namespace, the imported images by exact reference, and the local work.
# ---------------------------------------------------------------------------------------------
NS_CREATED=false
IMPORTED=()
IMPORT_STARTED=false
cleanup() {
	local status=$?
	trap - EXIT
	jobs -p | xargs kill 2>/dev/null || true
	# The node-side watchers exit on these files; one left running would freeze a later run's containers.
	if [ -n "${SSH:-}" ]; then
		if node_ssh "touch /tmp/hikyo-1068-stop /tmp/hikyo-1068-containers-stop; i=0; while pgrep -f '[s]h -s .*$NS' >/dev/null && [ \$i -lt 30 ]; do sleep 1; i=\$((i + 1)); done; rm -f /tmp/hikyo-1068-stop /tmp/hikyo-1068-containers-stop; ! pgrep -f '[s]h -s .*$NS' >/dev/null" 2>/dev/null; then
			echo 'cleanup: no node watcher is left'
		else
			echo 'cleanup: a node watcher outlived its stop file; killing it and failing the run' >&2
			node_ssh "pkill -f '[s]h -s .*$NS'; sleep 1; ! pgrep -f '[s]h -s .*$NS' >/dev/null" || echo 'cleanup: a node watcher is STILL RUNNING' >&2
			status=1
		fi
	fi
	pkill -f "port-forward deployment/$SERVER $PF_PORT:8080" 2>/dev/null || true
	if [ "${HIKYO_PI_KEEP:-}" = 1 ]; then
		echo "kept: namespace $NS, images ${IMPORTED[*]:-none}, work $WORK" >&2
		exit "$status"
	fi
	if [ "$NS_CREATED" = true ]; then
		if [ "$status" != 0 ]; then
			kc get pods,jobs -o wide >&2 || true
			kc logs deployment/"$SERVER" -c server --tail=60 >&2 || true
		fi
		log "cleanup: deleting namespace $NS"
		kca delete namespace "$NS" --wait=true --timeout=300s >&2 || true
		if kca get namespace "$NS" >/dev/null 2>&1; then echo "cleanup: namespace $NS STILL EXISTS" >&2; else echo "cleanup: namespace $NS is gone"; fi
	fi
	local ref
	# The node's list says what was added over the whole run: an import that failed half way, and the image IDs the
	# CRI names once a container starts, after import_images took its own list.
	if [ "${IMPORT_STARTED:-false}" = true ]; then
		if added_images >"$WORK/images-now"; then
			while IFS= read -r ref; do IMPORTED+=("$ref"); done < <(rehearsal_images <"$WORK/images-now")
			mapfile -t IMPORTED < <(printf '%s\n' "${IMPORTED[@]}" | sed '/^$/d' | sort -u)
		else
			echo "cleanup: could not list the images on $NODE; rehearsal images may remain" >&2
			status=1
		fi
		rm -f "$WORK/images-now"
	fi
	if [ -s "$WORK/unresolved-images" ]; then
		echo "cleanup: these images were added during the run and could not be resolved; check them by hand:" >&2
		sed 's/^/  /' "$WORK/unresolved-images" >&2
		status=1
	fi
	rm -f "$WORK/unresolved-images"
	for ref in "${IMPORTED[@]:-}"; do
		[ -n "$ref" ] || continue
		ctr images rm --sync "$ref" >/dev/null 2>&1 || echo "cleanup: could not remove $ref" >&2
	done
	[ "${#IMPORTED[@]}" = 0 ] || echo "cleanup: imported images left on $NODE: $(ctr images ls -q | grep -cxF -f <(printf '%s\n' "${IMPORTED[@]}") || true)"
	# What the run added and is still there, whoever owns it, so a ref this script failed to claim is seen.
	if [ "${IMPORT_STARTED:-false}" = true ] && added_images >"$WORK/images-end"; then
		sed 's/^/cleanup: added during the run and still on the node: /' "$WORK/images-end"
		rm -f "$WORK/images-end"
	fi
	# A watcher that outlived its scenario. Only this run's own command line matches; the ones stopped above get a moment.
	local watcher="kubectl --kubeconfig $KC -n $NS get "
	for _ in 1 2 3 4 5; do pgrep -f -- "$watcher" >/dev/null || break; sleep 1; done
	if pgrep -f -- "$watcher" >/dev/null; then
		echo "cleanup: a kubectl watcher was still running; stopping it" >&2
		pkill -f -- "$watcher" || true
		status=1
	fi
	# Generated keys never outlive the run. The private CA key is the exception for a caller-supplied work
	# directory whose fixture is reused: that fixture trusts this CA, so it needs the key again.
	rm -rf "$WORK/keys"
	rm -f "$WORK/jar" "$WORK/totp-uri" "$WORK/root-key" "$WORK/login.json" "$WORK/establish.json" "$WORK/security-feed-root.json"
	if [ "$WORK_OWNED" = true ]; then rm -rf "$WORK/ca/ca.key"; else echo "cleanup: kept $WORK/ca/ca.key for the reused fixture; delete $WORK/ca and $WORK/fixture together when finished"; fi
	if [ "${PLACEHOLDER:-false}" = true ]; then rm -rf "$REPO_ROOT/internal/webui/dist"; fi
	if [ "$WORK_OWNED" = true ]; then rm -rf "$WORK"; fi
	exit "$status"
}

# ---------------------------------------------------------------------------------------------
# Artifacts: everything built on this machine. Nothing here touches the cluster.
# ---------------------------------------------------------------------------------------------
heavy() { if [ -n "${HIKYO_PI_HEAVY:-}" ]; then ${HIKYO_PI_HEAVY} "$@"; else "$@"; fi; }
build_artifacts() {
	log 'artifacts: stand-in harness, private CA, signed arm64 fixture releases A, B and C'
	mkdir -p "$WORK/build" "$WORK/ca"
	cd "$REPO_ROOT"
	# The `ui` build tag needs internal/webui/dist; a placeholder stands in when it is absent.
	if [ ! -d internal/webui/dist ]; then
		PLACEHOLDER=true
		mkdir internal/webui/dist
		echo '<!doctype html>placeholder' >internal/webui/dist/index.html
	fi
	if [ ! -x "$WORK/build/harness" ]; then
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 heavy go build -p 2 -trimpath -o "$WORK/build/harness" ./scripts/ci/securitycomposerehearsal
	fi
	if [ ! -f "$WORK/ca/ca.crt" ]; then go run ./scripts/ci/securitycomposerehearsal ca "$WORK/ca"; fi
	if [ ! -f "$FIXTURE/architecture" ]; then
		HIKYO_UNATTENDED_FIXTURE_OUTPUT=$FIXTURE HIKYO_UNATTENDED_FIXTURE_GOARCH=arm64 \
			HIKYO_UNATTENDED_FIXTURE_IMAGE_REPOSITORY=$REPO HIKYO_UNATTENDED_FIXTURE_CA=$WORK/ca/ca.crt \
			heavy go test -p 2 -parallel 2 -count=1 -run '^TestWriteUnattendedContainerFixture$' ./scripts/ci/unattendedfixture
	fi
	[ "$(tr -d '\n' <"$FIXTURE/architecture")" = arm64 ] || fail 'the fixture is not arm64'
	# The stand-in image: busybox (a shell, tar and sleep) with the stand-in server, the release A
	# binary as the feed publisher, the private CA as the only trust bundle, and the signed releases.
	local ctx=$WORK/standin label
	rm -rf "$ctx"
	mkdir -p "$ctx/fixture/public"
	cp "$WORK/build/harness" "$ctx/harness"
	cp "$FIXTURE/release-a/hikyo" "$ctx/hikyo"
	cp "$WORK/ca/ca.crt" "$ctx/ca-bundle.crt"
	for label in a b c; do
		cp -R "$FIXTURE/public/release-$label" "$ctx/fixture/public/"
		cp "$FIXTURE/descriptor-$label.json" "$ctx/fixture/"
	done
	mkdir -p "$ctx/fixture/public/bundle-c/keys"
	cp "$FIXTURE"/public/bundle-c/*.json "$ctx/fixture/public/bundle-c/"
	cp "$FIXTURE/public/bundle-c/keys/test-primary.pub" "$ctx/fixture/public/bundle-c/keys/"
	cp -R "$FIXTURE/images" "$ctx/fixture/images"
	cat >"$ctx/Dockerfile" <<'DOCKERFILE'
FROM busybox@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
COPY harness /harness
COPY hikyo /hikyo
COPY ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
COPY fixture /fixture
DOCKERFILE
	# The Pod runs as an unprivileged user, and the fixture files are written 0600.
	chmod -R a+rX "$ctx"
	heavy docker buildx build --quiet --provenance=false --platform linux/arm64 --output "type=oci,dest=$WORK/standin.tar,name=$STANDIN_IMAGE" "$ctx"
	for label in a b c; do tar -C "$FIXTURE/images/$label" -cf "$WORK/image-$label.tar" .; done
	du -sh "$WORK/standin.tar" "$WORK"/image-?.tar
}

# added_images: the node's image references that were not there before this run.
added_images() { ctr images ls -q 2>/dev/null | sort | comm -13 "$WORK/images-before" -; }

# rehearsal_images: of the images added during this run, the ones it imported. The kubelet may pull for other
# workloads on this shared node meanwhile, and those are not ours to remove. Refs named for this rehearsal are
# ours by name. ctr's import-DATE@ names and the CRI's bare sha256: image IDs could also be someone else's, so
# they count only when they point at a blob from our tars. A ref that cannot be resolved is recorded, and
# cleanup fails the run for it.
rehearsal_images() {
	local ref target
	while IFS= read -r ref; do
		case "$ref" in
		ghcr.io/hikyo-1068-pi/* | localhost/hikyo-1068-pi/*) echo "$ref" ;;
		sha256:* | import-* | docker.io/library/import-*)
			if ! target=$(ctr images ls "name==$ref" </dev/null | awk 'NR == 2 { print $3 }') || [ -z "$target" ]; then
				echo "$ref" >>"$WORK/unresolved-images"
				continue
			fi
			if grep -qxF "${target#sha256:}" "$WORK/imported-blobs"; then echo "$ref"; fi
			;;
		esac
	done
	return 0
}

# archive_digests: every digest this run's archives can be named by on the node: their blobs, and each
# archive's own index.json, which is not a blob but is what ctr names an import by.
archive_digests() {
	local tarball blobs
	blobs=$(for tarball in "$WORK"/image-?.tar "$WORK/standin.tar"; do tar -tf "$tarball"; done | sed -n 's|^\(\./\)\{0,1\}blobs/sha256/\([0-9a-f]\{64\}\)$|\2|p' | sort -u)
	[ -n "$blobs" ] || fail 'the image archives list no blobs'
	printf '%s\n' "$blobs"
	for tarball in "$WORK"/image-?.tar "$WORK/standin.tar"; do
		{ tar -xOf "$tarball" ./index.json 2>/dev/null || tar -xOf "$tarball" index.json; } | python3 -c 'import hashlib, sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())'
	done
}

# Imports with the node's containerd, because the registry in front of the images' real home caps an
# upload at about 100 MB. What was added is recorded by diffing the node's image list, so that
# cleanup removes those exact references and nothing else.
import_images() {
	log "images: importing into containerd on $NODE"
	ctr images ls -q | sort >"$WORK/images-before"
	rm -f "$WORK/unresolved-images"
	archive_digests >"$WORK/imported-blobs"
	IMPORT_STARTED=true
	local label
	for label in a b c; do
		node_ssh "$CTR images import --digests --index-name $REPO:$(version_of "$label") -" <"$WORK/image-$label.tar" >/dev/null
	done
	node_ssh "$CTR images import --digests -" <"$WORK/standin.tar" >/dev/null
	# `ctr import` names the index import-DATE@digest; the Deployment pins REPO@digest, so that name is added.
	local source
	for label in a b c; do
		source=$(ctr images ls -q | grep -F "@$(digest_of "$label")" | grep -v "^$REPO@" | head -1)
		[ -n "$source" ] || fail "no imported index with digest $(digest_of "$label")"
		ctr images tag "$source" "$(image_of "$label")" >/dev/null
	done
	ctr images ls -q | sort >"$WORK/images-after"
	local ref
	while IFS= read -r ref; do IMPORTED+=("$ref"); done < <(comm -13 "$WORK/images-before" "$WORK/images-after" | rehearsal_images)
	printf 'imported references:\n'
	printf '  %s\n' "${IMPORTED[@]}"
	local missing=0
	for label in a b c; do
		printf '%s\n' "${IMPORTED[@]}" | grep -Fxq "$(image_of "$label")" || { echo "missing $(image_of "$label")"; missing=1; }
	done
	printf '%s\n' "${IMPORTED[@]}" | grep -Fxq "$STANDIN_IMAGE" || { echo "missing $STANDIN_IMAGE"; missing=1; }
	[ "$missing" = 0 ] || fail 'a release image is not addressable by digest on the node'
}

# ---------------------------------------------------------------------------------------------
# The namespace, PostgreSQL with TLS, the state volume contract and the stand-in.
# ---------------------------------------------------------------------------------------------
create_namespace() {
	log "namespace $NS"
	kca create namespace "$NS"
	NS_CREATED=true
	kca label namespace "$NS" app.kubernetes.io/part-of=hikyo-1068-rehearsal
}

deploy_infra() {
	log 'infra: state volume with the documented contract, PostgreSQL with TLS and the dead-session settings the operations guide recommends (tcp_user_timeout 30s, keepalives 60/10/3), scratch database'
	local tls=$WORK/tls
	mkdir -p "$tls"
	openssl req -x509 -newkey rsa:2048 -nodes -days 1 -keyout "$tls/ca.key" -out "$tls/ca.crt" -subj '/CN=Hikyo pi rehearsal CA' >/dev/null 2>&1
	openssl req -newkey rsa:2048 -nodes -keyout "$tls/tls.key" -out "$tls/server.csr" -subj "/CN=postgres.$NS.svc" >/dev/null 2>&1
	printf 'subjectAltName=DNS:postgres,DNS:postgres.%s,DNS:postgres.%s.svc,DNS:postgres.%s.svc.cluster.local\nextendedKeyUsage=serverAuth\n' "$NS" "$NS" "$NS" >"$tls/server.ext"
	openssl x509 -req -days 1 -sha256 -in "$tls/server.csr" -CA "$tls/ca.crt" -CAkey "$tls/ca.key" -CAcreateserial -extfile "$tls/server.ext" -out "$tls/tls.crt" >/dev/null 2>&1
	openssl rand -hex 32 >"$WORK/root-key"
	local dsn="postgres://hikyo:hikyo@postgres.$NS.svc:5432/hikyo?sslmode=verify-full&sslrootcert=/run/hikyo-database-ca/ca.crt"
	local scratch="postgres://hikyo:hikyo@postgres.$NS.svc:5432/hikyo_scratch?sslmode=verify-full&sslrootcert=/run/hikyo-database-ca/ca.crt"
	kc create secret generic postgres-auth --from-literal=username=hikyo --from-literal=password=hikyo --from-literal=database=hikyo >/dev/null
	kc create secret generic postgres-tls --from-file=tls.crt="$tls/tls.crt" --from-file=tls.key="$tls/tls.key" >/dev/null
	kc create secret generic hikyo-database-ca --from-file=ca.crt="$tls/ca.crt" >/dev/null
	kc create secret generic hikyo-root-key --from-file=root-key="$WORK/root-key" >/dev/null
	kc create secret generic hikyo-database --from-literal=HIKYO_DB="$dsn" >/dev/null
	kc create secret generic hikyo-scratch --from-literal=HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN="$scratch" >/dev/null
	rm -rf "$tls" "$WORK/root-key"
	kc apply -f - >/dev/null <<YAML
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: hikyo-upgrade-state}
spec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 1Gi}}}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: postgres-data}
spec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 1Gi}}}
---
apiVersion: batch/v1
kind: Job
metadata: {name: state-contract}
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      nodeSelector: {kubernetes.io/hostname: $NODE}
      containers:
        - name: contract
          image: $BUSYBOX_IMAGE
          imagePullPolicy: IfNotPresent
          securityContext: {runAsUser: 0, runAsGroup: 0}
          resources: {requests: {cpu: 10m, memory: 16Mi}}
          command:
            - sh
            - -c
            - |
              set -e
              mkdir -p /v/operator-custody/unattended
              chown -R 65532:65532 /v/operator-custody
              chown 0:65532 /v
              chmod 0700 /v/operator-custody /v/operator-custody/unattended
              chmod 3770 /v
              stat -c '%u:%g %a %n' /v /v/operator-custody /v/operator-custody/unattended
          volumeMounts: [{name: v, mountPath: /v}]
      volumes: [{name: v, persistentVolumeClaim: {claimName: hikyo-upgrade-state}}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: postgres}
spec:
  replicas: 1
  strategy: {type: Recreate}
  selector: {matchLabels: {app: postgres}}
  template:
    metadata: {labels: {app: postgres}}
    spec:
      nodeSelector: {kubernetes.io/hostname: $NODE}
      securityContext: {fsGroup: 999, fsGroupChangePolicy: OnRootMismatch}
      containers:
        - name: postgres
          image: $POSTGRES_IMAGE
          imagePullPolicy: IfNotPresent
          args: [-c, ssl=on, -c, ssl_cert_file=/etc/postgres-tls/tls.crt, -c, ssl_key_file=/etc/postgres-tls/tls.key, -c, shared_buffers=32MB, -c, max_connections=40, -c, tcp_user_timeout=30000, -c, tcp_keepalives_idle=60, -c, tcp_keepalives_interval=10, -c, tcp_keepalives_count=3]
          env:
            - {name: POSTGRES_USER, valueFrom: {secretKeyRef: {name: postgres-auth, key: username}}}
            - {name: POSTGRES_PASSWORD, valueFrom: {secretKeyRef: {name: postgres-auth, key: password}}}
            - {name: POSTGRES_DB, valueFrom: {secretKeyRef: {name: postgres-auth, key: database}}}
          ports: [{name: postgres, containerPort: 5432}]
          resources: {requests: {cpu: 50m, memory: 128Mi}, limits: {memory: 512Mi}}
          readinessProbe:
            # -h 127.0.0.1: initdb's temporary socket-only server must not count as ready.
            exec: {command: [pg_isready, -h, 127.0.0.1, -U, hikyo, -d, hikyo]}
            periodSeconds: 2
            failureThreshold: 60
          volumeMounts:
            - {name: data, mountPath: /var/lib/postgresql}
            - {name: tls, mountPath: /etc/postgres-tls, readOnly: true}
      volumes:
        - name: data
          persistentVolumeClaim: {claimName: postgres-data}
        - name: tls
          secret: {secretName: postgres-tls, defaultMode: 0440}
---
apiVersion: v1
kind: Service
metadata: {name: postgres}
spec:
  selector: {app: postgres}
  ports: [{name: postgres, port: 5432, targetPort: postgres}]
YAML
	kc wait --for=condition=complete job/state-contract --timeout=180s
	kc logs job/state-contract
	kc rollout status deployment/postgres --timeout=300s
	kc exec deployment/postgres -- createdb -U hikyo hikyo_scratch
}

STANDIN_IP=11.0.0.2
STANDIN_NAMES=(api.github.com github.com raw.githubusercontent.com ghcr.io hikyo.app)
deploy_standin() {
	log 'stand-in: GitHub, ghcr.io and hikyo.app answered by one Pod under the private CA'
	kc create secret generic standin-ca --from-file=ca.crt="$WORK/ca/ca.crt" --from-file=ca.key="$WORK/ca/ca.key" >/dev/null
	local names
	names=$(printf '"%s", ' "${STANDIN_NAMES[@]}")
	kc apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Pod
metadata: {name: standin, labels: {app: standin}}
spec:
  restartPolicy: Never
  nodeSelector: {kubernetes.io/hostname: $NODE}
  # The same names point at the service address, so the feed publisher commands run in this Pod reach the
  # stand-in as the server does (a loopback address would be refused by the publisher's netpolicy too).
  hostAliases: [{ip: $STANDIN_IP, hostnames: [${names%, }]}]
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    sysctls: [{name: net.ipv4.ip_unprivileged_port_start, value: "0"}]
  # The feed publisher refuses key files below a directory that others can write, which a fresh
  # emptyDir is (mode 0777). The work directory is handed to the stand-in's user, mode 0755.
  initContainers:
    - name: work-owner
      image: $BUSYBOX_IMAGE
      imagePullPolicy: IfNotPresent
      securityContext: {runAsUser: 0, runAsNonRoot: false, allowPrivilegeEscalation: false}
      command: [sh, -c, 'chown 65532:65532 /work && chmod 0755 /work']
      resources: {requests: {cpu: 10m, memory: 16Mi}}
      volumeMounts: [{name: work, mountPath: /work}]
  containers:
    - name: standin
      image: $STANDIN_IMAGE
      imagePullPolicy: IfNotPresent
      command: [/harness, github, /fixture, /ca, "0.0.0.0:443", /work/feed, $REPO]
      env: [{name: GOMEMLIMIT, value: 350MiB}]
      ports: [{containerPort: 443}]
      resources: {requests: {cpu: 20m, memory: 64Mi}, limits: {memory: 1Gi}}
      volumeMounts:
        - {name: ca, mountPath: /ca, readOnly: true}
        - {name: work, mountPath: /work}
        - {name: tmp, mountPath: /tmp}
  volumes:
    - name: ca
      secret: {secretName: standin-ca, defaultMode: 0444}
    # Memory: on netbooted nodes the kubelet directory is NFS, where the publisher's directory sync fails with EINVAL.
    - name: work
      emptyDir: {medium: Memory, sizeLimit: 768Mi}
    - name: tmp
      emptyDir: {}
---
apiVersion: v1
kind: Service
metadata: {name: standin}
spec:
  selector: {app: standin}
  # The server refuses to dial private addresses (netpolicy), so the three names must resolve to a public one.
  # kube-proxy answers it for the Pods of the cluster while this Service exists.
  externalIPs: [$STANDIN_IP]
  ports: [{name: https, port: 443, targetPort: 443}]
YAML
	kc wait --for=condition=Ready pod/standin --timeout=240s
	kc logs standin --tail=3
	echo "stand-in service address: $STANDIN_IP"
}

# ---------------------------------------------------------------------------------------------
# The feed: the real publisher commands, run in the stand-in Pod under ephemeral feed keys.
# ---------------------------------------------------------------------------------------------
LAST='' NEXT=0 TUFV=0
pub() { kc exec standin -- env GOMEMLIMIT=400MiB TMPDIR=/work/tmp /hikyo security-maintenance feed "$@"; }
# advisory ARGS: authors the next catalog from the last one. publish: signs and checks it.
advisory() {
	NEXT=$((NEXT + 1))
	pub advisory --output "/work/publisher/catalog-$NEXT.json" --cache /work/publisher/cache ${LAST:+--previous-catalog "$LAST"} "$@" || return 1
	LAST=/work/publisher/catalog-$NEXT.json
}
publish() {
	TUFV=$((TUFV + 1))
	pub publish --catalog "$LAST" --root /work/feed/metadata/1.root.json --output /work/feed --cache /work/publisher/cache --version "$TUFV" \
		--targets-expires "$(iso 2592000)" --snapshot-expires "$(iso 1728000)" --timestamp-expires "$(iso 864000)" \
		--targets-key /work/keys/targets.pem --snapshot-key /work/keys/snapshot.pem --timestamp-key /work/keys/timestamp.pem
}
feed_init() {
	log 'feed: ephemeral trust, real init-root, first empty catalog'
	local role keys=$WORK/keys
	install -d -m 0700 "$keys"
	for role in root targets snapshot timestamp; do
		openssl genpkey -algorithm ed25519 -out "$keys/$role.pem" 2>/dev/null
		openssl pkey -in "$keys/$role.pem" -pubout -out "$keys/$role.pub"
	done
	jq -n --arg expires "$(iso 31536000)" '{version: 1, expires: $expires, signer_files: ["/work/keys/root.pem"],
		roles: (["root", "targets", "snapshot", "timestamp"] | map({key: ., value: {public_key_files: ["/work/keys/" + . + ".pub"], threshold: 1}}) | from_entries)}' >"$keys/root-config.json"
	kc exec standin -- sh -c 'install -d -m 0700 /work/feed /work/feed/metadata /work/keys /work/tmp /work/publisher /work/publisher/cache'
	tar -C "$keys" -cf - . | kc exec -i standin -- tar -C /work/keys -xf -
	pub init-root --config /work/keys/root-config.json --output /work/feed/metadata/1.root.json
	kc exec standin -- cat /work/feed/metadata/1.root.json >"$WORK/security-feed-root.json"
	advisory
	publish
	kc exec standin -- sh -c 'ls -l /work/feed/metadata | head'
}

# ---------------------------------------------------------------------------------------------
# The chart.
# ---------------------------------------------------------------------------------------------
# chart_values: only what the fixtures need differs from the chart: the image, the database, the root
# key, the state claim, the origin, the fixture feed root, the nightly channel of the fixture releases
# a maintenance window that is closed (a critical advisory bypasses it) and the acceptance of the
# local-path volume root, which k3s leaves world-writable. Schedule, enrollment,
# concurrency, RBAC and the rest are the chart's defaults.
chart_values() {
	local minute=$((10#$(date -u +%H) * 60 + 10#$(date -u +%M)))
	WINDOW_START=$(((minute + 720) % 1440))
	CHART_ARGS=(
		--set operator.enabled=false
		--set image.repository="$REPO" --set image.digest="$(digest_of a)"
		--set database.existingSecret=hikyo-database --set database.tls.existingSecret=hikyo-database-ca
		--set rootKey.existingSecret=hikyo-root-key
		--set upgrade.stateExistingClaim=hikyo-upgrade-state
		--set upgrade.unattended.enabled=true --set upgrade.unattended.scratchDatabaseExistingSecret=hikyo-scratch
		--set externalOrigin=https://hikyo.example.com --set 'network.trustedProxyCIDRs={127.0.0.1/32}'
		--set resources.requests.cpu=50m --set resources.requests.memory=128Mi
		--set-file securityMaintenance.coordinator.feedRoot="$WORK/security-feed-root.json"
		--set securityMaintenance.coordinator.policy.channel=nightly
		--set securityMaintenance.coordinator.policy.window_start_minute="$WINDOW_START"
		--set securityMaintenance.coordinator.policy.window_duration_minutes=5
		--set securityMaintenance.coordinator.acceptWorldWritableVolumeRoot=true
	)
}
helm_install() {
	log 'chart: helm install --wait, unattended upgrades on, security maintenance at its chart defaults'
	chart_values
	export REHEARSAL_STANDIN_IP=$STANDIN_IP REHEARSAL_NODE=$NODE REHEARSAL_TIMEOUT_SECONDS=$TIMEOUT
	# Helm does not pass hooks through a post-renderer, so the enrollment hook is applied separately.
	helm --kubeconfig "$KC" -n "$NS" install "$RELEASE" "$REPO_ROOT/chart/hikyo" "${CHART_ARGS[@]}" \
		--post-renderer "$SELF_DIR/security-pi-rehearsal-render.py" --no-hooks --wait --timeout 20m
	echo "installed with image $(image_of a)"
}
# enroll_hook: the chart's own post-install enrollment Job, rendered by helm template, given the same
# overlay as the rest and applied here. It is the owner's consent, and it retries until the server is healthy.
enroll_hook() {
	log 'enrollment: the chart post-install hook Job (rendered by helm template, applied by this script)'
	local manifest attempt=0
	manifest=$(helm --kubeconfig "$KC" -n "$NS" template "$RELEASE" "$REPO_ROOT/chart/hikyo" "${CHART_ARGS[@]}" --show-only templates/security-maintenance-coordinator.yaml |
		REHEARSAL_HOOK=1 python3 "$SELF_DIR/security-pi-rehearsal-render.py")
	while :; do
		attempt=$((attempt + 1))
		printf '%s\n' "$manifest" | kc apply -f - >/dev/null
		if job_wait "job/$CRON-enroll" 900; then break; fi
		kc logs "job/$CRON-enroll" --tail=30 || true
		# Helm would run the hook before the first scheduled run; this script applies it afterwards, so a
		# quarter hour can fall in between, and the scheduled run then holds the coordinator lock.
		if [ "$attempt" -lt 5 ] && kc logs "job/$CRON-enroll" 2>&1 | grep -q 'another maintenance coordinator owns this installation'; then
			echo "enrollment: a scheduled run held the coordinator lock, trying again (attempt $attempt)"
			kc delete "job/$CRON-enroll" --wait=true >/dev/null
			sleep 20
			continue
		fi
		fail 'the enrollment hook Job did not succeed'
	done
	kc logs "job/$CRON-enroll" --tail=5
}

# ---------------------------------------------------------------------------------------------
# Coordinator commands and runs.
# ---------------------------------------------------------------------------------------------
# job_wait JOB [SECONDS]: waits for success or failure and returns the Job's outcome.
job_wait() {
	local job=$1 limit=${2:-900} waited=0 succeeded failed
	while [ "$waited" -lt "$limit" ]; do
		succeeded=$(kc get "$job" -o jsonpath='{.status.succeeded}' 2>/dev/null || true)
		failed=$(kc get "$job" -o jsonpath='{.status.failed}' 2>/dev/null || true)
		[ "${succeeded:-0}" -lt 1 ] || return 0
		[ "${failed:-0}" -lt 1 ] || return 1
		sleep 3
		waited=$((waited + 3))
	done
	echo "job $job did not finish in ${limit}s" >&2
	return 2
}
# job_finished JOB [SECONDS]: waits like job_wait; the Job may fail, but a Job still running is a defect.
job_finished() {
	local rc=0
	job_wait "$@" || rc=$?
	[ "$rc" != 2 ] || defect "$1 did not finish"
}
# coordinator ARGS...: a one-off Job derived from the CronJob (the runbook's hikyo_coordinator),
# whose log is printed. Arguments must not contain spaces.
coordinator() {
	local job
	job=$(kc get cronjob "$CRON" -o json | jq --arg args "$*" '{apiVersion: "batch/v1", kind: "Job",
		metadata: {generateName: "hikyo-security-maintenance-op-"},
		spec: (.spec.jobTemplate.spec | .backoffLimit = 0 | .ttlSecondsAfterFinished = 3600
			| .template.spec.containers[0].args = (["security-maintenance"] + ($args | split(" ")) + ["--config=/etc/hikyo/security-maintenance.json"]))}' | kc create -f - -o name)
	LAST_JOB=$job
	job_wait "$job" 300 || true
	kc logs "$job" 2>&1
}
# coordinator_json ARGS...: the JSON object a command printed.
coordinator_json() { coordinator "$@" | jq -c 'select(type == "object")' | tail -1; }
SUMMARY='{phase,reason,detail,selected:.selected.identity.version,running:.running.identity.version,healthy:(.running.ready and .running.phase=="healthy"),operation:(.operation!=null),retry_attempts,retry_disruptive,enrolled:(.enrollment!=null)}'

# ---------------------------------------------------------------------------------------------
# Data and a signed-in session, through the server's own commands and API.
# ---------------------------------------------------------------------------------------------
# A free local port: a fixed one can belong to another server on the workstation, which answers 401.
PF_PORT=${HIKYO_PI_PORT:-$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')}
# pf_ensure: the port-forward follows one Pod, so a replaced server needs a new one.
pf_ensure() {
	if curl -fsS -m 3 -o /dev/null "http://127.0.0.1:$PF_PORT/api/v1/meta" 2>/dev/null; then return 0; fi
	# Called inside $(...) too, so the forward is found by its command line, not by a variable.
	pkill -f "port-forward deployment/$SERVER $PF_PORT:8080" 2>/dev/null || true
	kc port-forward "deployment/$SERVER" "$PF_PORT:8080" >"$WORK/port-forward.log" 2>&1 &
	local attempt=0
	until curl -fsS -m 3 -o /dev/null "http://127.0.0.1:$PF_PORT/api/v1/meta" 2>/dev/null; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 40 ] || { head -5 "$WORK/port-forward.log"; fail 'the server does not answer through the port-forward'; }
		sleep 1
	done
}
base() { pf_ensure; printf 'http://127.0.0.1:%s' "$PF_PORT"; }
cookie_header() { printf 'Cookie: __Host-hikyo=%s; __Host-hikyo-csrf=%s' "$(awk '$6=="__Host-hikyo"{print $7}' "$1")" "$(awk '$6=="__Host-hikyo-csrf"{print $7}' "$1")"; }
# api METHOD PATH [JSON] with the browser session in $JAR.
api() {
	curl -sS -X "$1" -H "$(cookie_header "$JAR")" -H "X-Hikyo-CSRF: $(awk '$6=="__Host-hikyo-csrf"{print $7}' "$JAR")" \
		-H 'Content-Type: application/json' ${3:+--data "$3"} "$(base)/api/v1$2"
}
# TOTP codes are single-use per 30 second step.
LASTSTEP=0
wait_step() { while [ $(($(date +%s) / 30)) -le "$LASTSTEP" ]; do sleep 1; done; }
totp() { kc exec standin -- /harness totp "$(cat "$WORK/totp-uri")"; }
mfa_login() {
	rm -f "$1"
	wait_step
	curl -sS -c "$1" -o "$WORK/login.json" -H 'Content-Type: application/json' --data "{\"username\":\"rehearsal-admin\",\"password\":\"$PASSWORD\",\"artifact\":\"browser\"}" "$(base)/api/v1/auth/local/login"
	curl -sS -b "$1" -c "$1" -o /dev/null -H 'Content-Type: application/json' --data "{\"code\":\"$(totp)\"}" \
		"$(base)/api/v1/auth/login/challenge/$(jq -r .challenge_id "$WORK/login.json")/totp"
	LASTSTEP=$(($(date +%s) / 30))
}
whoami_code() { curl -sS -H "$(cookie_header "$1")" -o /dev/null -w '%{http_code}' "$(base)/api/v1/auth/whoami"; }
# The server's operator commands need the unattended runtime's own bundle and operator key in the
# environment, and the image has no shell and kubectl exec cannot set variables. So each command runs
# as a Pod made from the Deployment's own pod spec (same image, volumes and variables), without the
# labels that select the server's Pods.
bundle_dir() {
	kc delete pod bundle-probe --ignore-not-found --wait=true >/dev/null
	kc apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Pod
metadata: {name: bundle-probe}
spec:
  restartPolicy: Never
  nodeSelector: {kubernetes.io/hostname: $NODE}
  containers:
    - name: probe
      image: $BUSYBOX_IMAGE
      imagePullPolicy: IfNotPresent
      resources: {requests: {cpu: 10m, memory: 16Mi}}
      command: [sh, -c, 'ls /s/operator-custody/unattended/downloads | grep "^bundle-" | head -1']
      volumeMounts: [{name: s, mountPath: /s, readOnly: true}]
  volumes: [{name: s, persistentVolumeClaim: {claimName: hikyo-upgrade-state, readOnly: true}}]
YAML
	kc wait --for=jsonpath='{.status.phase}'=Succeeded pod/bundle-probe --timeout=120s >/dev/null
	kc logs pod/bundle-probe
	kc delete pod bundle-probe --wait=true >/dev/null
}
hx() {
	local bundle args phase attempt=0
	bundle=$(bundle_dir)
	[ -n "$bundle" ] || fail 'the unattended runtime has no verified bundle yet'
	args=$(printf '%s\n' "$@" | jq -R . | jq -s .)
	kc delete pod hx --ignore-not-found --wait=true >/dev/null
	kc get deployment "$SERVER" -o json | jq --argjson args "$args" --arg bundle "/var/lib/hikyo-upgrade/operator-custody/unattended/downloads/$bundle" \
		'{apiVersion: "v1", kind: "Pod", metadata: {name: "hx", labels: {app: "hx"}},
		spec: (.spec.template.spec | .restartPolicy = "Never"
			| .containers |= map(select(.name == "server") | del(.startupProbe, .readinessProbe, .livenessProbe, .ports) | .command = ["/usr/local/bin/hikyo"] | .args = $args
				| .env += [{name: "HIKYO_UPGRADE_BUNDLE", value: $bundle}, {name: "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY", value: "/var/lib/hikyo-upgrade/operator-custody/unattended/operator.pub"}]))}' | kc apply -f - >/dev/null
	until phase=$(kc get pod hx -o jsonpath='{.status.phase}') && { [ "$phase" = Succeeded ] || [ "$phase" = Failed ]; }; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 120 ] || { kc delete pod hx --wait=false >/dev/null; return 1; }
		sleep 1
	done
	kc logs pod/hx | grep -v 'unknown environment key'
	kc delete pod hx --wait=true >/dev/null
	[ "$phase" = Succeeded ]
}
seed_data() {
	log 'data: first administrator, a TOTP factor, an organisation, a project, a key and a signed-in session'
	PASSWORD=rehearsal-password-long-enough
	JAR=$WORK/jar
	local created principal org project
	created=$(hx admin create --username rehearsal-admin --dangerously-print 2>&1) || fail "admin create: $created"
	principal=$(printf '%s' "$created" | sed -n 's/.*principal \([^)]*\)).*/\1/p')
	hx admin grant --principal "$principal" --capability instance-config >/dev/null 2>&1 || fail 'admin grant failed'
	local attempt=0
	until [ "$(kc exec deployment/"$SERVER" -c server -- /usr/local/bin/hikyo security-maintenance inspect 2>/dev/null | jq -r .ready)" = true ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 30 ] || fail 'the server did not become ready after the first administrator'
		sleep 2
	done
	sleep 5
	local code
	code=$(curl -sS -o "$WORK/establish.json" -w '%{http_code}' -H 'Content-Type: application/json' --data "$(jq -cn --arg a "$(printf '%s\n' "$created" | grep '^hik_')" --arg p "$PASSWORD" '{authority:$a,password:$p}')" "$(base)/api/v1/auth/credential/establish")
	case "$code" in 2??) ;; *) fail "credential establish answered $code: $(head -c 300 "$WORK/establish.json"); admin create printed: $(printf '%s' "$created" | sed 's/hik_[A-Za-z0-9_.-]*/hik_REDACTED/g' | head -c 600)" ;; esac
	curl -sS -c "$JAR" -o /dev/null -H 'Content-Type: application/json' --data "{\"username\":\"rehearsal-admin\",\"password\":\"$PASSWORD\",\"artifact\":\"browser\"}" "$(base)/api/v1/auth/local/login"
	local csrf
	csrf=$(awk '$6=="__Host-hikyo-csrf"{print $7}' "$JAR")
	curl -fsS -H "$(cookie_header "$JAR")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"password\":\"$PASSWORD\"}" \
		"$(base)/api/v1/auth/totp/enrol/start" | jq -r .otpauth_uri >"$WORK/totp-uri"
	curl -fsS -o /dev/null -H "$(cookie_header "$JAR")" -H "X-Hikyo-CSRF: $csrf" -H 'Content-Type: application/json' --data "{\"code\":\"$(totp)\"}" "$(base)/api/v1/auth/totp/enrol/confirm"
	LASTSTEP=$(($(date +%s) / 30))
	mfa_login "$JAR"
	# Creating the organisation ends the session, so it signs in again.
	org=$(api POST /orgs '{"name":"rehearsal-org"}' | jq -er .id)
	mfa_login "$JAR"
	project=$(api POST "/orgs/$org/projects" '{"name":"rehearsal-project"}' | jq -er .id)
	api POST "/orgs/$org/projects/$project/keys" '{"name":"REHEARSAL_KEY","classification":"config","declaration":{"rule":{"type":"string","allow_empty":true}}}' | jq -c '{name}'
	ORG=$org PROJECT=$project
	expect 'the seeded session works' 200 "$(whoami_code "$JAR")"
	DATA_BEFORE=$(data_summary)
	echo "data: $DATA_BEFORE"
}
# data_summary: what the rehearsal wrote, read back through the API.
data_summary() { printf 'orgs=%s keys=%s' "$(api GET /orgs | jq -c '[.items[].name]')" "$(api GET "/orgs/$ORG/projects/$PROJECT/keys" | jq -c '[.. | .name? // empty]')"; }
# identity_snapshot: the installation's identity, credential epoch, incarnation and restore epoch.
identity_snapshot() {
	psql_hikyo "SELECT identity FROM instance_identity; SELECT credential_epoch||'/'||restore_epoch FROM auth_instance_state; SELECT incarnation||'/'||restore_epoch FROM upgrade_control;" | tr '\n' ' '
}

# ---------------------------------------------------------------------------------------------
# Observation helpers for the scenarios.
# ---------------------------------------------------------------------------------------------
SELECTOR='app.kubernetes.io/name=hikyo,app.kubernetes.io/instance=hikyo'
dep_image() { kc get deployment "$SERVER" -o jsonpath='{.spec.template.spec.containers[?(@.name=="server")].image}'; }
dep_replicas() { kc get deployment "$SERVER" -o jsonpath='{.spec.replicas}'; }
server_pod() { kc get pods -l "$SELECTOR" -o jsonpath='{.items[0].metadata.uid} {.items[0].spec.nodeName}'; }
wait_serving() { # IMAGE: the Deployment runs IMAGE with one Ready Pod
	local want=$1 attempt=0
	until [ "$(dep_image)" = "$want" ] && [ "$(dep_replicas)" = 1 ] && [ "$(kc get deployment "$SERVER" -o jsonpath='{.status.readyReplicas}.{.status.replicas}.{.status.updatedReplicas}')" = 1.1.1 ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 150 ] || return 1
		sleep 2
	done
}
suspend_cron() { kc patch cronjob "$CRON" -p "{\"spec\":{\"suspend\":$1}}" >/dev/null; }

# Three recordings of one scenario. CONTAINERS_FILE is what the node's container runtime says runs, polled about
# twice a second: R: a rollback one-shot, I: another stage Pod, S: a server. PODS_FILE holds the API server's own watch events for the Pods as JSON, in
# the order the API server issued them, with the timestamps the server and the kubelet put into the objects
# (stop_proof reads nothing else about Pods). TIMELINE holds the Deployment's replica rows with local arrival
# times; it only tells that the Deployment was scaled to zero.
watch_start() { # LABEL
	TIMELINE=$WORK/timeline-$1.log
	PODS_FILE=$WORK/pods-$1.json
	CONTAINERS_FILE=$WORK/containers-$1.log
	: >"$TIMELINE"
	: >"$PODS_FILE"
	node_ssh 'rm -f /tmp/hikyo-1068-containers-stop' || fail 'the node cannot be reached'
	node_ssh sh -s "$NS" >"$CONTAINERS_FILE" 2>&1 <<'NODE' &
while [ ! -e /tmp/hikyo-1068-containers-stop ]; do
	line=
	for c in $(k3s crictl ps -q --name '^server$' --label "io.kubernetes.pod.namespace=$1"); do
		name=$(k3s crictl inspect -o go-template --template '{{index .status.labels "io.kubernetes.pod.name"}}' "$c" 2>/dev/null) || continue
		case "$name" in
		*security-stage*) case "$(k3s crictl inspect "$c" 2>/dev/null)" in *'"rollback"'*) line="$line R:$name" ;; *) line="$line I:$name" ;; esac ;;
		*) line="$line S:$name" ;;
		esac
	done
	echo "$(date -u +%T.%N)$line"
	sleep 0.5
done
NODE
	WATCH_NODE=$!
	local stamp='import sys, time
for line in sys.stdin:
    sys.stdout.write("%.3f\t%s" % (time.time(), line))
    sys.stdout.flush()'
	# kubectl itself, not kc: $! of a backgrounded function is its subshell, and killing that leaves kubectl watching.
	kubectl --kubeconfig "$KC" -n "$NS" get pods -w --output-watch-events -o json >"$PODS_FILE" 2>/dev/null &
	WATCH_POD=$!
	kubectl --kubeconfig "$KC" -n "$NS" get deployment "$SERVER" -w --output-watch-events -o go-template='deploy|{{.type}}|{{.object.spec.replicas}}|{{or .object.status.replicas 0}}|{{or .object.status.terminatingReplicas 0}}|{{(index .object.spec.template.spec.containers 0).image}}
' >> >(python3 -u -c "$stamp" >>"$TIMELINE") 2>/dev/null &
	WATCH_DEPLOY=$!
	sleep 2
}
watch_stop() {
	sleep 3
	node_ssh 'touch /tmp/hikyo-1068-containers-stop' || kill "$WATCH_NODE" 2>/dev/null || true
	kill "$WATCH_POD" "$WATCH_DEPLOY" 2>/dev/null || true
	wait "$WATCH_POD" "$WATCH_DEPLOY" "$WATCH_NODE" 2>/dev/null || true
	node_ssh 'rm -f /tmp/hikyo-1068-containers-stop' || true
}

# kill_target IMAGE [stop|wedge|wedge-now]: wedge-now freezes the target at once, without waiting for a journal phase.
# Otherwise it is the forced failure AFTER the database write, as when the target's process dies or
# its node loses it. The target writes "write-intent", "schema-applied" and "hop-healthy" to its
# journal on the state volume before the final boot gate commits "healthy", and that window is a
# fraction of a second. So a watcher on the NODE reads the journal from the volume's directory and
# stops the server container (crictl stop, no grace) at the first of the last two phases. This script
# then keeps deleting the server Pods while the Deployment runs IMAGE, so the target never reaches the
# gate. It ends with kill_target_stop. SUBSTITUTED: the failure is injected, the detection is real.
kill_target() {
	local mode=${2:-stop}
	rm -f "$WORK/killer.stop" "$WORK/killer.phase"
	local pv
	pv=$(kca get pv "$(kc get pvc hikyo-upgrade-state -o jsonpath='{.spec.volumeName}')" -o jsonpath='{.spec.local.path}{.spec.hostPath.path}')
	[ -n "$pv" ] || fail 'the state volume has no node path (this rehearsal needs a node-local volume)'
	node_ssh 'rm -f /tmp/hikyo-1068-stop' || true
	node_ssh sh -s "$pv" "$NS" "$mode" "$1" >"$WORK/node-kill.log" 2>&1 <<'NODE' &
journal=$1/operator-custody/unattended/operation.json
waited=0
if [ "$3" != wedge-now ]; then
	until hit=$(grep -Eo '"phase": *"(schema-applied|hop-healthy)"' "$journal" 2>/dev/null | head -1) && [ -n "$hit" ]; do
		[ ! -e /tmp/hikyo-1068-stop ] || { echo "STOPPED without a hit"; exit 0; }
		waited=$((waited + 1))
		[ "$waited" -lt 90000 ] || { echo "TIMEOUT"; exit 1; }
		sleep 0.01
	done
	echo "HIT $hit $(date -u +%T.%N)"
fi
if [ "$3" = stop ]; then
	for c in $(k3s crictl ps -q --name '^server$' --label "io.kubernetes.pod.namespace=$2"); do k3s crictl stop -t 0 "$c" >/dev/null && echo "stopped container $c $(date -u +%T.%N)"; done
	exit 0
fi
# wedge: SIGSTOP the server process, again after every restart by the kubelet's probes. The container
# stays up, so the coordinator can still run `inspect` in it and read a control row that is not healthy.
first=1
digest=${4#*@}
while [ ! -e /tmp/hikyo-1068-stop ]; do
	for c in $(k3s crictl ps -q --name '^server$' --label "io.kubernetes.pod.namespace=$2"); do
		# The rollback one-shot's container is also named server; freezing it would hang the rollback under test.
		case "$(k3s crictl inspect -o go-template --template '{{index .status.labels "io.kubernetes.pod.name"}}' "$c" 2>/dev/null)" in *security-stage*) continue ;; esac
		# Only the target is frozen: the source image, written back by the rollback, must be able to start.
		case "$(k3s crictl inspect "$c" 2>/dev/null)" in *"$digest"*) ;; *) continue ;; esac
		pid=$(k3s crictl inspect -o go-template --template '{{.info.pid}}' "$c" 2>/dev/null) || continue
		[ -n "$pid" ] && [ "$pid" != 0 ] && kill -STOP "$pid" && { [ "$first" = 1 ] && echo "stopped container $c $(date -u +%T.%N)"; first=0; }
	done
	sleep 0.3
done
NODE
	NODE_KILL=$!
	(
		until grep -q '^stopped container' "$WORK/node-kill.log" 2>/dev/null; do
			[ ! -e "$WORK/killer.stop" ] || exit 0
			sleep 0.2
		done
		grep -h '^HIT' "$WORK/node-kill.log" >"$WORK/killer.phase" || true
		echo "journal at the first kill: $(cat "$WORK/killer.phase")"
		while [ ! -e "$WORK/killer.stop" ] && [ "$mode" = stop ]; do
			if [ "$(dep_image)" = "$1" ] && [ "$(dep_replicas)" = 1 ]; then
				kc delete pod -l "$SELECTOR" --grace-period=2 --wait=false >/dev/null 2>&1 || true
			fi
			sleep 1
		done
	) &
	KILLER=$!
}
kill_target_stop() {
	touch "$WORK/killer.stop"
	node_ssh 'touch /tmp/hikyo-1068-stop' || true
	wait "$NODE_KILL" "$KILLER" 2>/dev/null || true
	node_ssh 'rm -f /tmp/hikyo-1068-stop' || true
}

# thaw_target: lets a frozen server process run again.
thaw_target() {
	node_ssh sh -s "$NS" <<'NODE'
for c in $(k3s crictl ps -q --name '^server$' --label "io.kubernetes.pod.namespace=$1"); do
	pid=$(k3s crictl inspect -o go-template --template '{{.info.pid}}' "$c" 2>/dev/null) || continue
	[ -z "$pid" ] || [ "$pid" = 0 ] || kill -CONT "$pid" || true
done
NODE
}

# kill_pod_hard POD: kills the container on the node first (crictl stop -t 0, no grace) and then removes the Pod
# object. A force delete alone leaves the process running until the kubelet gets to it, and a coordinator that
# keeps running is not a killed one.
kill_pod_hard() {
	local id
	id=$(kc get pod "$1" -o jsonpath='{.status.containerStatuses[0].containerID}' | sed 's|.*://||') || fail "cannot read the container of Pod $1"
	[ -n "$id" ] || fail "Pod $1 has no container to kill"
	node_ssh "k3s crictl stop -t 0 $id" >/dev/null || fail "could not kill the container of Pod $1, so a scenario would run with it alive"
	kc delete pod "$1" --grace-period=0 --force >/dev/null 2>&1 || true
}

# kill_coordinator IMAGE: waits until the coordinator has written IMAGE into the Deployment, then
# kills it with kill_pod_hard, as when a node loses power.
kill_coordinator() {
	local attempt=0 pod
	until [ "$(dep_image)" = "$1" ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 600 ] || fail 'the target never replaced the source'
		sleep 0.5
	done
	pod=$(kc get pods -l "job-name=$(basename "$LAST_JOB")" -o jsonpath='{.items[0].metadata.name}')
	kill_pod_hard "$pod"
	echo "coordinator pod $pod killed with the target written into the Deployment"
}

# rewind_backoff: the retry backoff counts real time, so the rehearsal moves the deadline in the
# coordinator's status file instead of the clock. SUBSTITUTED. No fsGroup on purpose: the kubelet would
# make the coordinator's private state directory group-writable, and the coordinator refuses that.
rewind_backoff() {
	# The retry counters belong to the target, and every scenario below tries the same target C: the status file
	# is edited so that each scenario starts from a coordinator that has not given up on it (retry_exhausted
	# after three disruptive attempts). The scenarios that follow are scored on their own rollback.
	kc delete pod rewind-backoff --ignore-not-found --wait=true >/dev/null
	kc apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Pod
metadata: {name: rewind-backoff}
spec:
  restartPolicy: Never
  nodeSelector: {kubernetes.io/hostname: $NODE}
  securityContext: {runAsUser: 65532, runAsGroup: 65532}
  containers:
    - name: rewind
      image: $BUSYBOX_IMAGE
      imagePullPolicy: IfNotPresent
      resources: {requests: {cpu: 10m, memory: 16Mi}}
      command: [sh, -c, 'sed -i "s/\"retry_after\": *\"[^\"]*\"/\"retry_after\":\"2000-01-01T00:00:00Z\"/;s/\"retry_disruptive\": *[0-9]*/\"retry_disruptive\":0/;s/\"retry_attempts\": *[0-9]*/\"retry_attempts\":0/" /s/state/status.json && chmod 600 /s/state/status.json && grep -oE "\"retry_[a-z]+\": *(\"[^\"]*\"|[0-9]+)" /s/state/status.json']
      volumeMounts: [{name: s, mountPath: /s}]
  volumes: [{name: s, persistentVolumeClaim: {claimName: $COORD_PVC}}]
YAML
	kc wait --for=jsonpath='{.status.phase}'=Succeeded pod/rewind-backoff --timeout=120s >/dev/null
	kc logs pod/rewind-backoff
	kc delete pod rewind-backoff --wait=true >/dev/null
}

# run_cron NAME: a Job made from the CronJob exactly as the scheduler makes one. Prints the log.
run_cron() {
	kc create job "$1" --from="cronjob/$CRON" >/dev/null
	LAST_JOB=job.batch/$1
}
run_log() { kc logs "job/$(basename "$LAST_JOB")" 2>&1; }

# stop_proof PODS_FILE SOURCE_NODE: the stop proof of every rollback one-shot in one recorded watch stream of the
# API server's Pods. A one-shot is a stage Pod whose container runs "security-maintenance rollback". For each one
# the check reads, in the order the API server issued the events, which server Pods were still in the API when it
# was created, and compares the creationTimestamp the API server gave the one-shot with every finishedAt a server
# container reports anywhere in the stream. The API server stamps the first and the kubelet the second, both to the
# second, so an overlap under a second is not visible here; the node's own recording (watch_start) samples for it.
stop_proof() {
	python3 -I - "$1" "$2" "$STAGE" <<'PY'
import json, sys
path, node, stage = sys.argv[1:4]
text = open(path).read()
decoder = json.JSONDecoder()
events, i = [], 0
while True:
    while i < len(text) and text[i].isspace():
        i += 1
    if i >= len(text):
        break
    try:
        event, i = decoder.raw_decode(text, i)
    except ValueError:
        # Only the event kubectl was writing when it was stopped may be cut short.
        if "\n{" in text[i:]:
            raise SystemExit("the watch stream is corrupt before its last event")
        break
    events.append(event)

def labels(pod):
    return pod["metadata"].get("labels") or {}

def args_of(pod):
    return (pod["spec"]["containers"][0].get("args") or [])

def finished(pod):
    return [c["state"]["terminated"]["finishedAt"] for c in (pod.get("status", {}).get("containerStatuses") or []) if ((c.get("state") or {}).get("terminated") or {}).get("finishedAt")]

last, present, terminating, one_shots, stage_kinds = {}, set(), set(), [], []
created_at, finishes = {}, {}
for event in events:
    pod, kind = event["object"], event["type"]
    name = pod["metadata"]["name"]
    if labels(pod).get("app.kubernetes.io/name") == "hikyo":
        last[name] = pod
        created_at[name] = pod["metadata"]["creationTimestamp"]
        finishes.setdefault(name, set()).update(finished(pod))
        if kind == "DELETED":
            present.discard(name)
        else:
            present.add(name)
            if pod["metadata"].get("deletionTimestamp"):
                terminating.add(name)
    elif name == stage and kind == "ADDED":
        args = args_of(pod)
        stage_kinds.append(args[1] if len(args) > 1 else "?")
        if "rollback" in args:
            created = pod["metadata"]["creationTimestamp"]
            one_shots.append({
                "created": created,
                "node": pod["spec"].get("nodeName", "-"),
                "open": sorted(n for n in present if last[n].get("status", {}).get("phase") not in ("Succeeded", "Failed")),
                "finished_in_api": sorted(n for n in present if last[n].get("status", {}).get("phase") in ("Succeeded", "Failed")),
                "terminating_before": sorted(terminating),
            })
# After the whole stream: a container's finishedAt reaches the API after the one-shot was created.
for shot in one_shots:
    created = shot["created"]
    shot["late"] = sorted(n for n, seen in finishes.items() if created_at[n] <= created and any(f > created for f in seen))
    # Informational: a Pod the harness deleted reaches DELETED with no finishedAt. The node's recording answers for those.
    shot["unfinished"] = sorted(n for n, seen in finishes.items() if created_at[n] <= created and n not in present and not seen)
print("STOPPROOF stage_pods=%s" % (",".join(stage_kinds) or "none"))
print("STOPPROOF one_shots=%d" % len(one_shots))
for k, shot in enumerate(one_shots, 1):
    print("STOPPROOF r%d created=%s node=%s node_ok=%s" % (k, shot["created"], shot["node"], shot["node"] == node))
    print("STOPPROOF r%d terminating_seen=%s" % (k, ("%d pods" % len(shot["terminating_before"])) if shot["terminating_before"] else "none"))
    print("STOPPROOF r%d live_server_pods_in_api_when_created=%d %s" % (k, len(shot["open"]), ",".join(shot["open"])))
    print("STOPPROOF r%d finished_server_pods_still_in_api_when_created=%d %s" % (k, len(shot["finished_in_api"]), ",".join(shot["finished_in_api"])))
    print("STOPPROOF r%d server_containers_finished_after_creation=%d %s" % (k, len(shot["late"]), ",".join(shot["late"])))
    print("STOPPROOF r%d deleted_pods_without_finishedAt=%d" % (k, len(shot["unfinished"])))
PY
}

# ---------------------------------------------------------------------------------------------
# Scenarios.
# ---------------------------------------------------------------------------------------------
PHRASE='the earlier release is serving again and the security fix is not applied'
status_json() { coordinator_json status; }
disruptive() { status_json | jq -r '.retry_disruptive // 0'; }
critical() { # VERSION-LETTER ID
	advisory --id "$2" --summary 'critical fixture flaw' --severity critical --component hikyo --fixed-version "$(version_of "$1")" --platform linux/arm64
	publish >/dev/null
}
# settled LABEL: a scenario starts from the source serving with nothing pinned. One that inherited an earlier
# scenario's open operation would score that operation's recovery as its own, so it is skipped as a defect.
settled() {
	local pinned image
	pinned=$(status_json | jq -r '.operation != null')
	image=$(dep_image)
	[ "$pinned" = false ] && [ "$image" = "$(image_of b)" ] && return 0
	defect "$1: not run, the installation is unsettled (operation pinned=$pinned, Deployment on $image)"
	return 1
}
# assert_rolled_back LABEL SOURCE_IMAGE DISRUPTIVE_BEFORE
assert_rolled_back() {
	local label=$1 src=$2 before=$3 st
	wait_serving "$src" || defect "$label: the source never became Ready"
	expect "$label: the Deployment runs the source release again" "$src" "$(dep_image)"
	expect "$label: instance, credential epoch, incarnation and restore epoch unchanged" "$IDENTITY_BEFORE" "$(identity_snapshot)"
	expect "$label: the session from before the update still works" 200 "$(whoami_code "$JAR")"
	expect "$label: data intact" "$DATA_BEFORE" "$(data_summary)"
	st=$(status_json)
	echo "$st" | jq -c "$SUMMARY"
	expect "$label: the operation is not pinned" false "$(echo "$st" | jq -r '.operation != null')"
	expect "$label: the reason is operation_rolled_back" operation_rolled_back "$(echo "$st" | jq -r .reason)"
	local stage_left
	stage_left=$(kc get pod "$STAGE" --ignore-not-found -o name) || fail 'cannot list the stage Pod'
	expect "$label: no stage Pod is left behind" '' "$stage_left"
	check "$label: status says the earlier release serves and the fix is not applied" test "$(echo "$st" | jq -r '.detail // ""' | grep -c "$PHRASE")" -ge 1
	check "$label: the rollback counted as a disruptive attempt" test "$(echo "$st" | jq -r '.retry_disruptive // 0')" -gt "$before"
}
# assert_stop_proof LABEL [terminating]: every rollback one-shot in the recordings. A failure here is a product
# defect. Pass terminating only where the harness deletes no Pods itself, because its own deletes would satisfy that check.
assert_stop_proof() {
	local out
	out=$(stop_proof "$PODS_FILE" "$NODE")
	echo "$out"
	check "$1: the Deployment was scaled to zero" test "$(grep -Ec '^[0-9.]+	deploy\|[A-Z]+\|0\|' "$TIMELINE")" -ge 1
	check "$1: a rollback one-shot Pod was created" test "$(echo "$out" | grep -c 'one_shots=[1-9]')" = 1
	if [ "${2:-}" = terminating ]; then
		check "$1: a terminating server Pod was observed during the stop" test "$(echo "$out" | grep -c 'terminating_seen=none')" = 0
	fi
	check "$1: the node saw a rollback one-shot's container run" test "$(grep -c ' R:' "$CONTAINERS_FILE")" -ge 1
	check "$1: the node saw no server container beside a rollback one-shot" test "$(grep ' R:' "$CONTAINERS_FILE" | grep -c ' S:')" = 0
	check "$1: every rollback one-shot ran on the recorded node" test "$(echo "$out" | grep -c 'node_ok=False')" = 0
	check "$1: no live server Pod was in the API when a one-shot was created (a Failed or Succeeded Pod has stopped, as awaitNoPods counts it)" test "$(echo "$out" | grep -Ec ' live_server_pods_in_api_when_created=[1-9]')" = 0
	check "$1: no server container finished after a one-shot was created" test "$(echo "$out" | grep -Ec 'finished_after_creation=[1-9]')" = 0
}

# oneshot_collect LABEL: evidence for a rollback one-shot (the stage Pod) that fails or does not finish: its
# own log, and what Postgres shows meanwhile. Stopped by oneshot_collect_stop, which prints the tails.
oneshot_collect() {
	: >"$WORK/$1-oneshot.log"
	: >"$WORK/$1-pg.log"
	(
		for _ in $(seq 1 400); do
			kc logs -f "pod/$STAGE" >>"$WORK/$1-oneshot.log" 2>&1 || true
			{ date -u +%T; psql_hikyo "SELECT datname, pid, state, wait_event_type, wait_event, client_addr, backend_start, application_name, left(query, 70) FROM pg_stat_activity WHERE datname IS NOT NULL AND state <> 'idle' AND pid <> pg_backend_pid()"; } >>"$WORK/$1-pg.log" 2>&1 || true
			sleep 3
		done
	) &
	COLLECTOR=$!
}
oneshot_collect_stop() {
	pkill -P "$COLLECTOR" 2>/dev/null || true
	kill "$COLLECTOR" 2>/dev/null || true
	wait "$COLLECTOR" 2>/dev/null || true
	echo "$1 rollback one-shot log:"
	grep -v -e 'unknown environment' -e NotFound "$WORK/$1-oneshot.log" | tail -15 || true
	echo "$1 postgres sessions while it ran (tail):"
	grep -v NotFound "$WORK/$1-pg.log" | tail -25 || true
}
scenario_install() {
	log 'S1: the chart installed with unattended upgrades on'
	expect 'the CronJob has the default schedule' '*/15 * * * *' "$(kc get cronjob "$CRON" -o jsonpath='{.spec.schedule}')"
	check 'the coordinator ServiceAccount exists' kc get serviceaccount "$CRON"
	check 'the coordinator Role exists' kc get role "$CRON"
	check 'the coordinator RoleBinding exists' kc get rolebinding "$CRON"
	expect 'the server Deployment runs release A' "$(image_of a)" "$(dep_image)"
	wait_serving "$(image_of a)" || fail 'the server never became Ready on release A'
	local ticked
	ticked=$(kc get cronjob "$CRON" -o jsonpath='{.status.lastScheduleTime}')
	pf_ensure
	seed_data
	IDENTITY_BEFORE=$(identity_snapshot)
	echo "identity: $IDENTITY_BEFORE"
	local st
	st=$(status_json)
	echo "$st" | jq -c "$SUMMARY"
	expect 'enrollment succeeded' true "$(echo "$st" | jq -r '.enrollment != null')"
	expect 'the coordinator observes release A, healthy' true "$(echo "$st" | jq -r '.running.ready and .running.phase == "healthy"')"
	natural_tick "$ticked"
}
# natural_tick BEFORE: the scheduler itself (the */15 schedule) starts a Job, and it succeeds.
natural_tick() {
	[ "${HIKYO_PI_SKIP_TICK:-}" != 1 ] || { echo 'natural tick skipped (HIKYO_PI_SKIP_TICK=1)'; return 0; }
	local waited=0 job
	until [ "$(kc get cronjob "$CRON" -o jsonpath='{.status.lastScheduleTime}')" != "$1" ]; do
		waited=$((waited + 5))
		[ "$waited" -le 1000 ] || { defect 'the CronJob did not start a Job within 16 minutes'; return 0; }
		sleep 5
	done
	job=$(kc get jobs --sort-by=.metadata.creationTimestamp -o name | grep "^job.batch/$CRON-[0-9]" | tail -1)
	if job_wait "$job" 300; then expect 'the scheduled coordinator Job succeeded' ok ok; else defect "the scheduled coordinator Job $job did not succeed"; fi
	kc logs "$job" | jq -c "select(type == \"object\")" | tail -1 | jq -c "$SUMMARY"
}

scenario_update() {
	log 'S2: a real critical advisory through the feed carries A to B'
	suspend_cron true
	critical b HIKYO-PI-1
	watch_start s2
	run_cron s2-run
	job_wait "job/s2-run" 900 || defect 'the update run did not succeed'
	run_log | tail -5
	wait_serving "$(image_of b)" || defect 'the Deployment never served B'
	watch_stop
	expect 'the Deployment serves release B' "$(image_of b)" "$(dep_image)"
	expect 'the credential epoch, instance and incarnation survive the update' "$IDENTITY_BEFORE" "$(identity_snapshot)"
	expect 'data intact after the update' "$DATA_BEFORE" "$(data_summary)"
	expect 'the session survives the update' 200 "$(whoami_code "$JAR")"
	local st
	st=$(status_json)
	echo "$st" | jq -c "$SUMMARY"
	expect 'the coordinator reports B healthy' "$(version_of b)" "$(echo "$st" | jq -r .running.identity.version)"
	expect 'no pinned operation after the update' false "$(echo "$st" | jq -r '.operation != null')"
}

scenario_forced_failure() {
	log 'S3: C fails after the write intent and is rolled back automatically in the same run'
	settled S3 || return 0
	critical c HIKYO-PI-2
	local before
	before=$(disruptive)
	watch_start s3
	oneshot_collect s3
	kill_target "$(image_of c)"
	run_cron s3-run
	local outcome=0
	job_wait "job/s3-run" 900 || outcome=$?
	run_log | tail -8
	expect 'S3: run --once exited nonzero after the rollback' 1 "$outcome"
	kill_target_stop
	wait_serving "$(image_of b)" || true
	watch_stop
	oneshot_collect_stop s3
	check 'S3: the target was killed only after it crossed the write intent' grep -q HIT "$WORK/killer.phase"
	assert_rolled_back 'S3' "$(image_of b)" "$before"
	assert_stop_proof 'S3'
}

# later_runs LABEL: past the deadline, runs the coordinator again until the pinned operation is gone (at most four
# runs, each a Job made from the CronJob). A later run reads the target through a short exec or a stage Pod; the
# kubelet's own restarts can leave a gap, so a few runs are allowed and the count is reported.
later_runs() {
	log "$1: waiting past the $TIMEOUT second deadline"
	sleep $((TIMEOUT + 20))
	local later=0
	while [ "$later" -lt 4 ]; do
		later=$((later + 1))
		run_cron "$1-later-$later"
		job_finished "job/$1-later-$later" 900
		run_log | tail -8
		[ "$(status_json | jq -r '.operation != null')" = true ] || break
		sleep 5
	done
	echo "$1: later runs needed: $later"
}

# journal_text: the executor's deployment journal (deployment.json, which holds the rolling_back marker) as the
# coordinator's state volume holds it; the node reads the volume's directory.
journal_text() {
	local pv
	pv=$(kca get pv "$(kc get pvc "$COORD_PVC" -o jsonpath='{.spec.volumeName}')" -o jsonpath='{.spec.local.path}{.spec.hostPath.path}')
	node_ssh "cat $pv/state/deployment/deployment.json"
}

# S4 holds the promised outcome: a rollback by a later run (docs/design/security-maintenance.md "What triggers it").
# The target is wedged (SIGSTOP), so it stays observable and the later run reads its control row by exec.
scenario_killed_coordinator() {
	log 'S4: the coordinator is killed mid-operation, and a later run after the deadline rolls back'
	settled S4 || return 0
	rewind_backoff
	local before
	before=$(disruptive)
	watch_start s4
	kill_target "$(image_of c)" wedge
	run_cron s4-first
	kill_coordinator "$(image_of c)"
	status_json | jq -c "$SUMMARY"
	expect 'S4: the killed coordinator left a pinned operation' true "$(status_json | jq -r '.operation != null')"
	oneshot_collect s4
	later_runs s4
	oneshot_collect_stop s4
	kill_target_stop
	wait_serving "$(image_of b)" || true
	watch_stop
	check 'S4: the target was killed only after it crossed the write intent' grep -q HIT "$WORK/killer.phase"
	assert_rolled_back 'S4' "$(image_of b)" "$before"
	assert_stop_proof 'S4' terminating
}

# S4b: the target cannot be observed at all. Its Pods are killed again and again (mode stop), so `Observe` fails,
# and the coordinator is killed. After the deadline a later run has to read the control row through the stage Pod
# (the source image's `inspect --control-row`), decide that no final gate was crossed, and roll back.
scenario_unobservable_target() {
	log 'S4b: the target crash-loops and the coordinator is killed; a later run reads the row through a stage Pod and rolls back'
	settled S4b || return 0
	rewind_backoff
	local before
	before=$(disruptive)
	watch_start s4b
	kill_target "$(image_of c)" stop
	run_cron s4b-first
	kill_coordinator "$(image_of c)"
	status_json | jq -c "$SUMMARY"
	expect 'S4b: the killed coordinator left a pinned operation' true "$(status_json | jq -r '.operation != null')"
	oneshot_collect s4b
	later_runs s4b
	oneshot_collect_stop s4b
	kill_target_stop
	wait_serving "$(image_of b)" || true
	watch_stop
	check 'S4b: the target was killed only after it crossed the write intent' grep -q HIT "$WORK/killer.phase"
	assert_rolled_back 'S4b' "$(image_of b)" "$before"
	assert_stop_proof 'S4b'
	check 'S4b: the later run read the row through an inspect stage Pod' test "$(stop_proof "$PODS_FILE" "$NODE" | grep -c 'stage_pods=.*inspect')" = 1
}

# kill_coordinator_at_oneshot: waits for the rollback one-shot Pod (a stage Pod running "security-maintenance
# rollback") to exist and not be finished, then kills the coordinator with kill_pod_hard. The Deployment is at zero by then.
kill_coordinator_at_oneshot() {
	local attempt=0 args phase pod
	until args=$(kc get pod "$STAGE" -o jsonpath='{.spec.containers[0].args}' 2>/dev/null) && [[ "$args" == *rollback* ]] && phase=$(kc get pod "$STAGE" -o jsonpath='{.status.phase}' 2>/dev/null) && [ "$phase" != Succeeded ] && [ "$phase" != Failed ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 1500 ] || fail 'the rollback one-shot never ran'
		sleep 0.2
	done
	pod=$(kc get pods -l "job-name=$(basename "$LAST_JOB")" -o jsonpath='{.items[0].metadata.name}')
	kill_pod_hard "$pod"
	echo "coordinator pod $pod killed while the rollback one-shot was $phase"
}

# S4c: the coordinator dies while the rollback one-shot runs. The Deployment stays at zero, the journal carries the
# rolling_back marker, and a later run after the deadline must resume the rollback and bring the source back.
scenario_interrupted_rollback() {
	log 'S4c: the coordinator is killed while the rollback one-shot runs; a later run resumes the rollback'
	settled S4c || return 0
	rewind_backoff
	local before
	before=$(disruptive)
	watch_start s4c
	oneshot_collect s4c
	kill_target "$(image_of c)" stop
	run_cron s4c-first
	kill_coordinator_at_oneshot
	kill_target_stop
	expect 'S4c: the interrupted rollback left the Deployment at zero' 0 "$(dep_replicas)"
	check 'S4c: the journal carries the rolling_back marker' test "$(journal_text | grep -Ec '"rolling_back": *true')" -ge 1
	status_json | jq -c "$SUMMARY"
	expect 'S4c: the killed coordinator left a pinned operation' true "$(status_json | jq -r '.operation != null')"
	later_runs s4c
	oneshot_collect_stop s4c
	wait_serving "$(image_of b)" || true
	watch_stop
	check 'S4c: the target was killed only after it crossed the write intent' grep -q HIT "$WORK/killer.phase"
	assert_rolled_back 'S4c' "$(image_of b)" "$before"
	assert_stop_proof 'S4c'
	check 'S4c: the resumed rollback ran its own one-shot' test "$(stop_proof "$PODS_FILE" "$NODE" | grep -c 'one_shots=[2-9]')" = 1
	# NoFinalGate reads the row first (an inspect stage Pod); the rolling_back marker only answers when the row is unreadable.
	check 'S4c: the later run read the row through an inspect stage Pod before the resumed one-shot' test "$(stop_proof "$PODS_FILE" "$NODE" | grep -c 'stage_pods=.*rollback,inspect.*rollback')" = 1
}

scenario_past_final_gate() {
	log 'S5: a target past the final gate is not stopped by the deadline'
	settled S5 || return 0
	rewind_backoff
	watch_start s5
	run_cron s5-first
	kill_coordinator "$(image_of c)"
	local attempt=0
	# The row names its target: a row left healthy by an earlier operation must not count for C.
	local row="SELECT COALESCE((SELECT (operation_json::json->'target'->>'version')||'/'||(operation_json::json->>'phase') FROM upgrade_pending), '')"
	until [ "$(psql_hikyo "$row")" = "$(version_of c)/healthy" ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 90 ] || break
		sleep 2
	done
	expect 'the target reached healthy by itself' "$(version_of c)/healthy" "$(psql_hikyo "$row")"
	local uid
	uid=$(server_pod)
	# A Ready target would leave through the completed check and never meet the deadline, so it is frozen first:
	# healthy in the row, not Ready, past the deadline. That is the case the deadline must leave running.
	kill_target "$(image_of c)" wedge-now
	attempt=0
	local ready
	while ready=$(kc get deployment "$SERVER" -o jsonpath='{.status.readyReplicas}') || fail 'S5: the Deployment could not be read'; [ -n "$ready" ]; do
		attempt=$((attempt + 1))
		[ "$attempt" -lt 90 ] || fail 'the frozen target stayed Ready'
		sleep 2
	done
	log "S5: waiting past the $TIMEOUT second deadline"
	sleep $((TIMEOUT + 20))
	run_cron s5-frozen
	job_finished "job/s5-frozen" 900
	run_log | tail -8
	status_json | jq -c "$SUMMARY"
	expect 'S5: the deadline left the operation pinned' true "$(status_json | jq -r '.operation != null')"
	check 'S5: status reports the pin' test "$(status_json | jq -r .reason | grep -Ec '^(operation_requires_reconciliation|installation_unavailable)$')" = 1
	expect 'S5: the deadline left the Deployment on the target' "$(image_of c)" "$(dep_image)"
	expect 'S5: the deadline left the Deployment at one replica' 1 "$(dep_replicas)"
	expect 'S5: the stop proof counted no rollback one-shot' 1 "$(stop_proof "$PODS_FILE" "$NODE" | grep -cE 'one_shots=0( |$)')"
	# The frozen target still runs, so a poller that can see containers records it.
	check 'S5: the node container poller saw the server container' grep -q ' S:' "$CONTAINERS_FILE"
	check 'S5: the Pod watch recorded events' test -s "$PODS_FILE"
	# One-sided: the poller can miss a short one-shot (it did in S5 at 23a0af5fe), but never invents one.
	expect 'S5: the node saw no rollback one-shot container' 0 "$(grep -c ' R:' "$CONTAINERS_FILE" || true)"
	kill_target_stop
	thaw_target
	wait_serving "$(image_of c)" || defect 'S5: the target did not recover after the freeze'
	run_cron s5-later
	job_finished "job/s5-later" 900
	run_log | tail -8
	watch_stop
	expect 'the same server Pod still serves C' "$uid" "$(server_pod)"
	expect 'the Deployment still runs C' "$(image_of c)" "$(dep_image)"
	local st
	st=$(status_json)
	echo "$st" | jq -c "$SUMMARY"
	expect 'the session still works' 200 "$(whoami_code "$JAR")"
	expect 'data intact' "$DATA_BEFORE" "$(data_summary)"
	expect 'the deadline did not scale the Deployment to zero' 0 "$(grep -Ec '^[0-9.]+	deploy\|[A-Z]+\|0\|' "$TIMELINE" || true)"
	# The design pins a target that passed the gate and cannot be matched; one that is the selected release
	# is simply accepted by the next run. Either way nothing may stop it.
	echo "after the later run: operation pinned=$(echo "$st" | jq -r '.operation != null')"
	expect 'the coordinator sees the target running and healthy' "$(version_of c)" "$(echo "$st" | jq -r '.running.identity.version')"
}

# ---------------------------------------------------------------------------------------------
main() {
	[ "$#" = 1 ] || { echo "usage: $0 KUBECONFIG" >&2; exit 2; }
	KC=$1
	WORK_OWNED=false
	PLACEHOLDER=false
	if [ -n "${HIKYO_PI_WORK:-}" ]; then
		WORK=$HIKYO_PI_WORK
		mkdir -p "$WORK"
		chmod 700 "$WORK"
	else
		WORK=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-1068-pi.XXXXXX")
		WORK_OWNED=true
	fi
	FIXTURE=$WORK/fixture
	trap cleanup EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM HUP
	preflight
	build_artifacts
	create_namespace
	import_images
	deploy_infra
	deploy_standin
	feed_init
	helm_install
	enroll_hook
	scenario_install
	scenario_update
	[ "${HIKYO_PI_SKIP_S3:-}" = 1 ] || scenario_forced_failure
	scenario_killed_coordinator
	[ "${HIKYO_PI_SKIP_S4B:-}" = 1 ] || scenario_unobservable_target
	[ "${HIKYO_PI_SKIP_S4C:-}" = 1 ] || scenario_interrupted_rollback
	[ "${HIKYO_PI_SKIP_S5:-}" = 1 ] || scenario_past_final_gate
	log "result: $PASSES assertions passed, $DEFECTS defects"
	[ "$PASSES" -gt 0 ] || fail 'no assertion ran'
	[ "$DEFECTS" = 0 ]
}

# Sourcing the file (for development) defines the functions and runs nothing.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then main "$@"; fi

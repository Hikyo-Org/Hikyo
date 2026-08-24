#!/usr/bin/env bash
# Provision ephemeral kind clusters and run both the operator suite and the
# real Helm install/readiness suite. Keeping both behind the existing k8s-e2e
# required job lets trusted base-branch CI execute a PR-head chart gate even in
# the same PR that introduces or changes the gate. The `kind`
# binary comes from PATH; the node image is digest-pinned to the default for the
# pinned kind release (v0.32.0), so the API-server version is reproducible.
#
# The cluster is created fresh and deleted in a trap. It never adopts or deletes
# a pre-existing `hikyo-e2e` cluster: a parallel session might own it, and
# deleting something this script did not create is the one move that is never
# safe.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

CLUSTER=hikyo-e2e
# kindest/node for kind v0.32.0 (kubernetes-sigs/kind v0.32.0 release notes).
# Digest-pinned: the tag alone is not a stable identity for a given kind build.
NODE_IMAGE="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"

if ! command -v kind >/dev/null 2>&1; then
	echo "k8s-e2e: kind not found on PATH" >&2
	exit 1
fi

# Refuse to touch a cluster we did not create.
if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
	echo "k8s-e2e: a kind cluster named '$CLUSTER' already exists; refusing to reuse or delete it" >&2
	exit 1
fi

kubeconfig="$(mktemp -t hikyo-e2e-kubeconfig.XXXXXX)"
config="$(mktemp -t hikyo-e2e-kind.XXXXXX)"
image_root="$(mktemp -d -t hikyo-e2e-image.XXXXXX)"
created=false
cleanup() {
	if [ "$created" = true ]; then
		kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
	fi
	rm -f "$kubeconfig" "$config"
	rm -rf "$image_root"
}
trap cleanup EXIT HUP INT TERM

cat >"$config" <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
EOF

echo "k8s-e2e: creating kind cluster '$CLUSTER' ($NODE_IMAGE)"
kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" \
	--config "$config" --kubeconfig "$kubeconfig" --wait 120s
created=true

export HIKYO_K8S_E2E_KUBECONFIG="$kubeconfig"

echo "k8s-e2e: building local Hikyo server image"
CGO_ENABLED=0 GOOS=linux go build -trimpath -o "$image_root/hikyo" ./cmd/hikyo
cat >"$image_root/Dockerfile" <<'EOF'
FROM scratch
COPY --chown=65532:65532 hikyo /hikyo
USER 65532:65532
ENTRYPOINT ["/hikyo"]
EOF
export HIKYO_K8S_E2E_SERVER_IMAGE="hikyo-k8s-e2e:local"
docker build -q -t "$HIKYO_K8S_E2E_SERVER_IMAGE" "$image_root" >/dev/null
kind load docker-image --name "$CLUSTER" "$HIKYO_K8S_E2E_SERVER_IMAGE" >/dev/null

echo "k8s-e2e: running operator and server-probe kind e2e suite"
go test -count=1 -tags k8se2e -run 'TestK8sOperator' ./internal/isolation/ -timeout 25m

# Release the first cluster before chart-kind creates its separately named
# cluster. Two simultaneous control planes add memory pressure without adding
# coverage, and each runner owns only the cluster it created.
kind delete cluster --name "$CLUSTER" >/dev/null
created=false

echo "k8s-e2e: building release-shaped UI binary for Helm chart gate"
if [ "$(uname -s)" = Linux ]; then
	# This compatibility bridge runs inside the existing base-controlled job,
	# which predates setup-node/setup-helm for the chart gate. Install both
	# exact toolchains from pinned official archives before executing PR code.
	tool_dir="$image_root/tools"
	mkdir -p "$tool_dir"
	node_archive="$tool_dir/node.tar.xz"
	curl --fail --location --proto '=https' --tlsv1.2 --silent --show-error \
		--retry 4 --retry-delay 2 --retry-all-errors \
		https://nodejs.org/dist/v26.7.0/node-v26.7.0-linux-x64.tar.xz \
		--output "$node_archive"
	printf '%s  %s\n' \
		982aa24dd8be4c889c6a8ab337ddff3b0896645b20f4239356e80552c16277ee \
		"$node_archive" | sha256sum --check --strict
	tar -xJf "$node_archive" -C "$tool_dir"
	export PATH="$tool_dir/node-v26.7.0-linux-x64/bin:$PATH"

	helm_archive="$tool_dir/helm.tar.gz"
	curl --fail --location --proto '=https' --tlsv1.2 --silent --show-error \
		--retry 4 --retry-delay 2 --retry-all-errors \
		https://get.helm.sh/helm-v4.2.3-linux-amd64.tar.gz \
		--output "$helm_archive"
	printf '%s  %s\n' \
		e9b88b4ee95b18c706839c28d3a0220e5bc470e9cd9262410c90793c45ff8b7c \
		"$helm_archive" | sha256sum --check --strict
	tar -xzf "$helm_archive" -C "$tool_dir"
	export PATH="$tool_dir/linux-amd64:$PATH"
fi
./scripts/ci/install-corepack.sh
./scripts/ci/build-spa.sh --verify
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -tags ui \
	-o "$image_root/hikyo-ui" ./cmd/hikyo
HIKYO_CHART_KIND_BINARY="$image_root/hikyo-ui" ./scripts/ci/chart-kind.sh

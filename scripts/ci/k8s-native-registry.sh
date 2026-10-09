#!/usr/bin/env bash
# Sourced by a caller that owns its ephemeral kind cluster. This fixture owns
# only its uniquely named registry container, image tag and temporary files.
# Generated test credentials never touch the user's Docker credential store.

# Ownership is local state, never authority inherited from the environment.
HIKYO_NATIVE_REGISTRY_CREATED=false
HIKYO_NATIVE_REGISTRY_NAME=
HIKYO_NATIVE_REGISTRY_ROOT=
HIKYO_NATIVE_PUSH_TAG=

native_registry_setup() {
	if [ "$HIKYO_NATIVE_REGISTRY_CREATED" = true ]; then
		echo "native registry: fixture already owns a registry; clean it up before creating another" >&2
		return 1
	fi
	local cluster=$1
	HIKYO_NATIVE_REGISTRY_NAME="hikyo-native-registry-${cluster}"
	if docker container inspect "$HIKYO_NATIVE_REGISTRY_NAME" >/dev/null 2>&1; then
		echo "native registry: refusing to reuse existing container $HIKYO_NATIVE_REGISTRY_NAME" >&2
		return 1
	fi
	HIKYO_NATIVE_REGISTRY_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-native-registry.XXXXXX")
	go run ./scripts/ci/registryfixture "$HIKYO_NATIVE_REGISTRY_ROOT"
	docker run -d --name "$HIKYO_NATIVE_REGISTRY_NAME" --network kind \
		-p 127.0.0.1::5000 \
		-v "$HIKYO_NATIVE_REGISTRY_ROOT/htpasswd:/auth/htpasswd:ro" \
		-e REGISTRY_AUTH=htpasswd -e REGISTRY_AUTH_HTPASSWD_REALM=hikyo-native-test \
		-e REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd \
		registry:3@sha256:1be55279f18a2fe1a74edf2664cac61c1bea305b7b4642dab412e7affdcb3e33 >/dev/null
	HIKYO_NATIVE_REGISTRY_CREATED=true
	local address docker_host node
	address=$(docker port "$HIKYO_NATIVE_REGISTRY_NAME" 5000/tcp)
	docker_host=$(docker context inspect "$(docker context show)" --format '{{(index .Endpoints "docker").Host}}')
	mkdir -p "$HIKYO_NATIVE_REGISTRY_ROOT/docker"
	for _ in $(seq 1 30); do
		if curl --silent --output /dev/null "http://$address/v2/"; then break; fi
		sleep 1
	done
	docker --host "$docker_host" --config "$HIKYO_NATIVE_REGISTRY_ROOT/docker" login "$address" \
		--username hikyo-test --password-stdin <"$HIKYO_NATIVE_REGISTRY_ROOT/password" >/dev/null
	docker pull registry.k8s.io/pause:3.10 >/dev/null
	HIKYO_NATIVE_PUSH_TAG="$address/private/pause:native-test"
	docker tag registry.k8s.io/pause:3.10 "$HIKYO_NATIVE_PUSH_TAG"
	docker --host "$docker_host" --config "$HIKYO_NATIVE_REGISTRY_ROOT/docker" push "$HIKYO_NATIVE_PUSH_TAG" >/dev/null
	export HIKYO_K8S_E2E_REGISTRY="$HIKYO_NATIVE_REGISTRY_NAME:5000"
	export HIKYO_K8S_E2E_PRIVATE_IMAGE="$HIKYO_K8S_E2E_REGISTRY/private/pause:native-test"
	export HIKYO_K8S_E2E_REGISTRY_PASSWORD_FILE="$HIKYO_NATIVE_REGISTRY_ROOT/password"
	cat >"$HIKYO_NATIVE_REGISTRY_ROOT/hosts.toml" <<EOF
[host."http://$HIKYO_K8S_E2E_REGISTRY"]
  capabilities = ["pull", "resolve"]
EOF
	while IFS= read -r node; do
		docker exec "$node" mkdir -p "/etc/containerd/certs.d/$HIKYO_K8S_E2E_REGISTRY"
		docker cp "$HIKYO_NATIVE_REGISTRY_ROOT/hosts.toml" "$node:/etc/containerd/certs.d/$HIKYO_K8S_E2E_REGISTRY/hosts.toml" >/dev/null
	done < <(kind get nodes --name "$cluster")
}

native_registry_cleanup() {
	if [ "${HIKYO_NATIVE_REGISTRY_CREATED:-false}" = true ]; then
		docker rm -f "$HIKYO_NATIVE_REGISTRY_NAME" >/dev/null 2>&1 || true
	fi
	if [ -n "${HIKYO_NATIVE_PUSH_TAG:-}" ]; then
		docker image rm "$HIKYO_NATIVE_PUSH_TAG" >/dev/null 2>&1 || true
	fi
	if [ -n "${HIKYO_NATIVE_REGISTRY_ROOT:-}" ]; then
		rm -rf "$HIKYO_NATIVE_REGISTRY_ROOT"
	fi
	HIKYO_NATIVE_REGISTRY_CREATED=false
	HIKYO_NATIVE_REGISTRY_NAME=
	HIKYO_NATIVE_REGISTRY_ROOT=
	HIKYO_NATIVE_PUSH_TAG=
}

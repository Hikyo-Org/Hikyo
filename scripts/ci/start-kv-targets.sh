#!/usr/bin/env bash
# Start current Vault and OpenBao dev servers over TLS for the vault-kv adapter
# contract (#162). Each runs from a digest-pinned image with a generated dev
# CA; only the public CA leaves the container.
#
# Exports HIKYO_TEST_{VAULT,OPENBAO}_{ADDR,TOKEN,CACERT} and
# HIKYO_TEST_KV_REQUIRED=1 into $GITHUB_ENV, so the contract in this job fails
# loud if a server did not come up, while jobs that never set the flag skip it.
set -euo pipefail

: "${GITHUB_ENV:?GITHUB_ENV must name the step environment file}"

ca_dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-kv-ca.XXXXXX")
started=()
ready=0
cleanup() {
	if [ "$ready" != 1 ]; then
		for id in "${started[@]}"; do
			docker rm -f "$id" >/dev/null 2>&1 || true
		done
	fi
}
trap cleanup EXIT

# start NAME IMAGE BINARY: run one dev server, publish its loopback port, copy
# its CA out, and export the contract variables under HIKYO_TEST_<NAME>_*.
start() {
	local name=$1 image=$2 binary=$3
	local token container_id binding port
	token=$(openssl rand -hex 24)
	container_id=$(docker run -d --cap-add IPC_LOCK -p 127.0.0.1::8200 --entrypoint "$binary" "$image" \
		server -dev -dev-tls -dev-tls-cert-dir=/tmp -dev-listen-address=0.0.0.0:8200 -dev-root-token-id="$token")
	started+=("$container_id")

	# Docker owns allocation; refuse anything but one IPv4 loopback binding.
	binding=$(docker port "$container_id" 8200/tcp)
	if [[ ! "$binding" =~ ^127\.0\.0\.1:([0-9]{1,5})$ ]]; then
		echo "$name dev server has no unique IPv4 loopback port binding" >&2
		exit 1
	fi
	port=$((10#${BASH_REMATCH[1]}))

	local up=0
	for _ in $(seq 1 60); do
		if docker cp "$container_id:/tmp/vault-ca.pem" "$ca_dir/$name-ca.pem" >/dev/null 2>&1 &&
			curl -fsS --cacert "$ca_dir/$name-ca.pem" "https://127.0.0.1:$port/v1/sys/health" >/dev/null 2>&1; then
			up=1
			break
		fi
		sleep 1
	done
	if [ "$up" != 1 ]; then
		echo "$name dev server did not become ready" >&2
		docker logs "$container_id" >&2 || true
		exit 1
	fi
	{
		echo "HIKYO_TEST_${name}_CONTAINER=$container_id"
		echo "HIKYO_TEST_${name}_ADDR=https://127.0.0.1:$port"
		echo "HIKYO_TEST_${name}_TOKEN=$token"
		echo "HIKYO_TEST_${name}_CACERT=$ca_dir/$name-ca.pem"
	} >>"$GITHUB_ENV"
	echo "$name dev server ready on 127.0.0.1:$port"
}

start VAULT mirror.gcr.io/hashicorp/vault:2.1.1@sha256:47f14a6acb98f48d798a07df7c83f23a6e636e1cf724c5f8ff165cb32667a1e2 vault
start OPENBAO mirror.gcr.io/openbao/openbao:2.7.0@sha256:71156a1c6623a5fa3f5e61b0c6a8ead0faf0df29a778339188443551995d1315 bao
echo "HIKYO_TEST_KV_REQUIRED=1" >>"$GITHUB_ENV"
ready=1

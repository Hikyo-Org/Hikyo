#!/bin/sh
# Start the isolated AWS-compatible harness for the AWS Secrets Manager
# adapter's emulator lane (#158): a digest-pinned Floci on HTTPS with a
# throwaway self-signed certificate, reachable on 127.0.0.1:5444. The adapter
# refuses plaintext HTTP, so the harness must speak TLS like AWS does. IAM
# policy enforcement is on, so the tests provision their own identities.
#
# Usage: start-floci-emulator.sh <certificate-directory>
# Prints the environment assignments the isolation tests read.
set -eu
"$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)/configure-docker-mirror.sh"

# floci/floci 2.4.0 multi-arch index (linux/amd64 and linux/arm64).
FLOCI_IMAGE='floci/floci@sha256:5ee85db604c4aa74ac76e07ea5cc83086f37bb472bf3dbb97e34019e48efae9c'
PORT=5444

if [ "$#" -ne 1 ]; then
	printf 'usage: %s <certificate-directory>\n' "$0" >&2
	exit 2
fi
cert_dir=$1
mkdir -p "$cert_dir"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
	-keyout "$cert_dir/key.pem" -out "$cert_dir/cert.pem" \
	-subj '/CN=127.0.0.1' \
	-addext 'subjectAltName=IP:127.0.0.1' \
	-addext 'basicConstraints=critical,CA:TRUE' >/dev/null 2>&1
# The container runs as its own user; the key is throwaway and test-only.
chmod 0644 "$cert_dir/key.pem"
# Only the TLS listener is published. State is in memory and dies with the
# container. The account matches HIKYO_TEST_FLOCI_ACCOUNT below.
docker run --detach --rm --name hikyo-floci-emulator \
	--publish "127.0.0.1:$PORT:$PORT" \
	--volume "$cert_dir:/certs:ro" \
	--env "QUARKUS_HTTP_SSL_PORT=$PORT" \
	--env QUARKUS_HTTP_SSL_CERTIFICATE_FILES=/certs/cert.pem \
	--env QUARKUS_HTTP_SSL_CERTIFICATE_KEY_FILES=/certs/key.pem \
	--env FLOCI_DEFAULT_ACCOUNT_ID=123456789012 \
	--env FLOCI_SERVICES_IAM_ENFORCEMENT_ENABLED=true \
	--env FLOCI_STORAGE_MODE=memory \
	"$FLOCI_IMAGE" >/dev/null
attempt=0
until curl --silent --fail --cacert "$cert_dir/cert.pem" "https://127.0.0.1:$PORT/_floci/health" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 60 ]; then
		docker logs hikyo-floci-emulator >&2 || true
		printf 'aws emulator did not become ready on port %s\n' "$PORT" >&2
		exit 1
	fi
	sleep 1
done
printf 'HIKYO_TEST_FLOCI_ENDPOINT=https://127.0.0.1:%s\n' "$PORT"
printf 'HIKYO_TEST_FLOCI_ACCOUNT=123456789012\n'
printf 'HIKYO_TEST_FLOCI_REGION=us-east-1\n'
printf 'HIKYO_TEST_FLOCI_ALLOWED_CIDR=127.0.0.1/32\n'
printf 'HIKYO_TEST_FLOCI_CA_FILE=%s/cert.pem\n' "$cert_dir"
printf 'HIKYO_TEST_FLOCI_REQUIRED=1\n'

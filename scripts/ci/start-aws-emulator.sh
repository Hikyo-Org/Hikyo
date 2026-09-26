#!/bin/sh
# Start the isolated AWS-compatible harness for the AWS Secrets Manager
# adapter's emulator leg (#158): a digest-pinned moto server on HTTPS with a
# throwaway self-signed certificate, reachable on 127.0.0.1:5443. The adapter
# refuses plaintext HTTP, so the harness must speak TLS like AWS does.
#
# Usage: start-aws-emulator.sh <certificate-directory>
# Prints the environment assignments the isolation test reads.
set -eu

MOTO_IMAGE='motoserver/moto@sha256:91fd602a21f49cf9eb82fdf474015a3c131d40104c8297ea6a2ca920708ae32c'
PORT=5443

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
docker run --detach --rm --name hikyo-aws-emulator \
	--publish "127.0.0.1:$PORT:$PORT" \
	--volume "$cert_dir:/certs:ro" \
	"$MOTO_IMAGE" -H 0.0.0.0 -p "$PORT" -s -c /certs/cert.pem -k /certs/key.pem >/dev/null
attempt=0
until curl --silent --fail --cacert "$cert_dir/cert.pem" "https://127.0.0.1:$PORT/moto-api/" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 60 ]; then
		docker logs hikyo-aws-emulator >&2 || true
		printf 'aws emulator did not become ready on port %s\n' "$PORT" >&2
		exit 1
	fi
	sleep 1
done
printf 'HIKYO_TEST_AWS_ENDPOINT=https://127.0.0.1:%s\n' "$PORT"
printf 'HIKYO_TEST_AWS_ACCOUNT=123456789012\n'
printf 'HIKYO_TEST_AWS_REGION=us-east-1\n'
printf 'HIKYO_TEST_AWS_ALLOWED_CIDR=127.0.0.1/32\n'
printf 'HIKYO_TEST_AWS_CA_FILE=%s/cert.pem\n' "$cert_dir"
printf 'HIKYO_TEST_AWS_EMULATOR_REQUIRED=1\n'

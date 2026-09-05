#!/usr/bin/env bash
# Start a TLS (sslmode=verify-full) PostgreSQL target for the dynamic-secret
# provider integration test (#147). Hikyo's own datastore is a separate,
# non-TLS service container; the dynamic provider mints roles at THIS target
# over verify-full, so it needs its own TLS server and a CA the test trusts.
#
# Exports HIKYO_TEST_DYNAMIC_PG_{DSN,PASSWORD,CA,GRANT_ROLE} + the required
# flag into $GITHUB_ENV so the test in this job runs (and fails loud if the
# target did not come up), while other jobs that never set the flag skip it.
set -euo pipefail

dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-dynpg.XXXXXX")
container="hikyo-dyn-tls-$$"
ready=0
created=0
cleanup() {
	if [ "$created" = 1 ] && [ "$ready" != 1 ]; then
		docker rm -f "$container" >/dev/null 2>&1 || true
	fi
	rm -rf "$dir"
}
trap cleanup EXIT

openssl req -new -x509 -days 2 -nodes \
	-out "$dir/server.crt" -keyout "$dir/server.key" \
	-subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1" >/dev/null 2>&1
chmod 600 "$dir/server.key"
printf '%s\n' 'local all all trust' 'hostssl all all all scram-sha-256' 'hostnossl all all all reject' >"$dir/pg_hba.conf"

# Copy into the disposable container before startup. Ownership is set inside
# it, so this works without sudo or host/container UID assumptions on macOS.
docker create --name "$container" \
	-e POSTGRES_PASSWORD=adminpw -e POSTGRES_USER=leaseadmin -e POSTGRES_DB=app \
	-p 127.0.0.1:55432:5432 --entrypoint /bin/sh \
	postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941 \
	-c 'set -eu; chown postgres:postgres /tmp/server.key; chmod 600 /tmp/server.key; exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tmp/server.crt -c ssl_key_file=/tmp/server.key -c hba_file=/tmp/pg_hba.conf' >/dev/null
created=1
docker cp "$dir/server.crt" "$container:/tmp/server.crt"
docker cp "$dir/server.key" "$container:/tmp/server.key"
docker cp "$dir/pg_hba.conf" "$container:/tmp/pg_hba.conf"
docker start "$container" >/dev/null

for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready -h 127.0.0.1 -U leaseadmin -d app >/dev/null 2>&1; then
    started=1
    break
  fi
  sleep 1
done
if [ "${started:-0}" != "1" ]; then
  echo "dynamic-secret TLS PostgreSQL target did not become ready" >&2
  docker logs "$container" >&2 || true
  exit 1
fi

# A non-TLS network connection must fail even with the correct password.
if docker exec -e PGPASSWORD=adminpw "$container" psql \
	"postgres://leaseadmin@127.0.0.1/app?sslmode=disable" -c 'SELECT 1' >/dev/null 2>&1; then
	echo 'dynamic-secret target accepted a non-TLS connection' >&2
	exit 1
fi
docker exec "$container" psql -U leaseadmin -d app -c "CREATE ROLE app_reader" >/dev/null

# Keep only the public CA outside the container after setup.
ca_dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-dynpg-ca.XXXXXX")
cp "$dir/server.crt" "$ca_dir/server.crt"
{
  echo "HIKYO_TEST_DYNAMIC_PG_CONTAINER=$container"
  echo "HIKYO_TEST_DYNAMIC_PG_DSN=postgres://leaseadmin@127.0.0.1:55432/app"
  echo "HIKYO_TEST_DYNAMIC_PG_PASSWORD=adminpw"
  echo "HIKYO_TEST_DYNAMIC_PG_CA=$ca_dir/server.crt"
  echo "HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE=app_reader"
  echo "HIKYO_DYNAMIC_PG_REQUIRED=1"
} >>"$GITHUB_ENV"

ready=1
echo "dynamic-secret TLS PostgreSQL target ready on 127.0.0.1:55432"

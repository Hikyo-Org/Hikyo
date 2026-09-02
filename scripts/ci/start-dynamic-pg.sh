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

dir="${RUNNER_TEMP:-/tmp}/dynpg"
mkdir -p "$dir"

openssl req -new -x509 -days 2 -nodes \
  -out "$dir/server.crt" -keyout "$dir/server.key" \
  -subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1" >/dev/null 2>&1

# PostgreSQL refuses a key readable by group/other; it must be 0600 owned by the
# container's postgres user (uid 999 in the official image).
sudo chown 999:999 "$dir/server.key"
sudo chmod 600 "$dir/server.key"
chmod 644 "$dir/server.crt"

docker rm -f hikyo-dyn-tls >/dev/null 2>&1 || true
docker run -d --name hikyo-dyn-tls \
  -e POSTGRES_PASSWORD=adminpw -e POSTGRES_USER=leaseadmin -e POSTGRES_DB=app \
  -p 55432:5432 \
  -v "$dir/server.crt:/etc/pg/server.crt:ro" \
  -v "$dir/server.key:/etc/pg/server.key:ro" \
  postgres:18 \
  -c ssl=on -c ssl_cert_file=/etc/pg/server.crt -c ssl_key_file=/etc/pg/server.key >/dev/null

for _ in $(seq 1 60); do
  if docker exec hikyo-dyn-tls pg_isready -U leaseadmin -d app >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ "${ready:-0}" != "1" ]; then
  echo "dynamic-secret TLS PostgreSQL target did not become ready" >&2
  docker logs hikyo-dyn-tls >&2 || true
  exit 1
fi

# Confirm TLS is actually on: a plain no-ssl connection must be refused.
docker exec hikyo-dyn-tls psql -U leaseadmin -d app -c "CREATE ROLE app_reader" >/dev/null

{
  echo "HIKYO_TEST_DYNAMIC_PG_DSN=postgres://leaseadmin@127.0.0.1:55432/app"
  echo "HIKYO_TEST_DYNAMIC_PG_PASSWORD=adminpw"
  echo "HIKYO_TEST_DYNAMIC_PG_CA=$dir/server.crt"
  echo "HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE=app_reader"
  echo "HIKYO_DYNAMIC_PG_REQUIRED=1"
} >>"$GITHUB_ENV"

echo "dynamic-secret TLS PostgreSQL target ready on 127.0.0.1:55432"

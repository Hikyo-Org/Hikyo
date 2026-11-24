#!/usr/bin/env bash
# Start a disposable PostgreSQL 17 so the suite proves Hikyo refuses a server
# older than 18 (#1552). It is never a datastore: the matrix stays on the pinned
# 18 image. Exports HIKYO_TEST_POSTGRES_OLD_DSN into $GITHUB_ENV; the tests that
# read it fail under CI when it is missing. The runner is discarded afterwards.
set -euo pipefail
"$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)/configure-docker-mirror.sh"

image=postgres:17-alpine@sha256:f9cf3e1f0066f0eb0c48707d864f0ac5941e9ff454a072077d97a4d278d07d73
container_id=$(docker run --detach --publish 127.0.0.1::5432 \
	--env POSTGRES_USER=hikyo --env POSTGRES_PASSWORD=hikyo --env POSTGRES_DB=hikyo_old \
	"$image")
ready=0
cleanup() {
	if [ "$ready" != 1 ]; then docker rm -fv "$container_id" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

for _ in $(seq 1 60); do
	if docker exec "$container_id" pg_isready -h 127.0.0.1 -U hikyo -d hikyo_old >/dev/null 2>&1; then
		ready=1
		break
	fi
	sleep 1
done
if [ "$ready" != 1 ]; then
	docker logs "$container_id" >&2 || true
	echo 'PostgreSQL 17 refusal fixture did not become ready' >&2
	exit 1
fi
binding=$(docker port "$container_id" 5432/tcp | head -n 1)
if [[ ! "$binding" =~ ^127\.0\.0\.1:([0-9]{1,5})$ ]]; then
	echo 'PostgreSQL 17 refusal fixture has no IPv4 loopback port binding' >&2
	exit 1
fi
echo "HIKYO_TEST_POSTGRES_OLD_DSN=postgres://hikyo:hikyo@127.0.0.1:${BASH_REMATCH[1]}/hikyo_old?sslmode=disable" >>"$GITHUB_ENV"

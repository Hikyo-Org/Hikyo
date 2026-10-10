#!/bin/sh
# Start after checkout so daemon mirror configuration precedes the pull.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
"$script_dir/configure-docker-mirror.sh"
container=hikyo-ci-postgres
if docker container inspect "$container" >/dev/null 2>&1; then
	printf 'CI PostgreSQL refuses to replace an existing container\n' >&2
	exit 1
fi
database=${POSTGRES_DB:-hikyo_test}
case "$database" in hikyo_test | hikyo_release_schema) ;; *) exit 2 ;; esac
password=${POSTGRES_PASSWORD:-hikyo}
retries=${POSTGRES_HEALTH_RETRIES:-10}
case "$retries" in 10 | 12) ;; *) exit 2 ;; esac
image=postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941
ready=false
created=false
cleanup() {
	if [ "$created" = true ] && [ "$ready" != true ]; then docker container rm --force "$container" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT HUP INT TERM
docker pull "$image"
created=true
docker run --detach --rm --name "$container" \
	--publish 127.0.0.1:5432:5432 \
	--env POSTGRES_USER=hikyo --env "POSTGRES_PASSWORD=$password" --env "POSTGRES_DB=$database" \
	--health-cmd "pg_isready -U hikyo -d $database" \
	--health-interval 5s --health-timeout 5s --health-retries "$retries" "$image" >/dev/null
attempt=0
while [ "$attempt" -lt "$((retries + 2))" ]; do
	status=$(docker inspect --format '{{.State.Health.Status}}' "$container")
	case "$status" in
		healthy) ready=true; exit 0 ;;
		unhealthy) break ;;
	esac
	attempt=$((attempt + 1))
	sleep 5
done
docker logs "$container" >&2
printf 'CI PostgreSQL failed its health gate\n' >&2
cleanup
exit 1

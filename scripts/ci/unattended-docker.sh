#!/usr/bin/env bash
# Actual distroless server replacements with persisted SQLite and custody.
# Artifact acquisition alone is a preseeded, authenticated offline cache fixture.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
: "${HIKYO_UNATTENDED_FIXTURE_OUTPUT:?generate signed fixtures first}"
fixture=$HIKYO_UNATTENDED_FIXTURE_OUTPUT
arch=$(cat "$fixture/architecture")
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-unattended-docker.XXXXXX")
suffix=$(basename "$work" | tr '[:upper:]' '[:lower:]')
data_volume=$suffix-data
state_volume=$suffix-state
container=$suffix-server
helper_image=postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941
created_data=false
created_state=false
cleanup() {
	docker rm --force "$container" >/dev/null 2>&1 || true
	if [ "$created_data" = true ]; then docker volume rm "$data_volume" >/dev/null 2>&1 || true; fi
	if [ "$created_state" = true ]; then docker volume rm "$state_volume" >/dev/null 2>&1 || true; fi
	for label in a b c; do docker image rm "$suffix:$label" >/dev/null 2>&1 || true; done
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
for volume in "$data_volume" "$state_volume"; do
	if docker volume inspect "$volume" >/dev/null 2>&1; then
		echo "unattended Docker: refusing an existing volume $volume" >&2
		exit 1
	fi
done
docker volume create "$data_volume" >/dev/null
created_data=true
docker volume create "$state_volume" >/dev/null
created_state=true
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$work/inspect" ./scripts/ci/unattendedfixture/inspect
for label in a b c; do
	mkdir -p "$work/image-root/$arch"
	cp "$fixture/release-$label/hikyo" "$work/image-root/$arch/hikyo"
	docker build --platform "linux/$arch" --file Dockerfile.release --tag "$suffix:$label" "$work" >/dev/null
done
helper() {
	docker run --rm --user 0:0 --entrypoint /bin/sh \
		--volume "$state_volume:/state" --volume "$data_volume:/data" \
		--volume "$fixture:/input:ro" --volume "$work:/tools:ro" "$helper_image" -ceu "$1"
}
helper 'mkdir -p /state/unattended/downloads; cp /input/root.key /state/root.key; chmod 700 /state /state/unattended /state/unattended/downloads /data; chmod 600 /state/root.key; chown -R 65532:65532 /state /data'
seed_descriptor() {
	local label=$1
	helper "cp /input/descriptor-$label.json /state/unattended/image.json; chmod 600 /state/unattended/image.json; chown 65532:65532 /state/unattended/image.json"
}
start() {
	local label=$1
	docker run --detach --name "$container" --read-only --cap-drop ALL \
		--tmpfs /tmp:rw,nosuid,nodev,size=128m \
		--volume "$data_volume:/var/lib/hikyo/data" \
		--volume "$state_volume:/var/lib/hikyo/upgrade" \
		--volume "$fixture/public:/fixtures:ro" \
		--env HIKYO_DB=sqlite:/var/lib/hikyo/data/hikyo.db \
		--env HIKYO_STATE_DIR=/var/lib/hikyo/data \
		--env HIKYO_UPGRADE_STATE_DIR=/var/lib/hikyo/upgrade \
		--env HIKYO_UPGRADE_UNATTENDED=true \
		--env HIKYO_ROOT_KEY_FILE=/var/lib/hikyo/upgrade/root.key \
		--env HIKYO_EXTERNAL_ORIGIN=https://unattended.fixture.invalid \
		--env HIKYO_TRUSTED_PROXY_CIDRS=127.0.0.1/32 \
		--env HIKYO_UPDATE_CHANNEL=off \
		--publish 127.0.0.1::8081 "$suffix:$label" server \
		--listen=0.0.0.0:8080 --operational-listen=0.0.0.0:8081 >/dev/null
}
healthy() {
	local port attempt
	port=$(docker port "$container" 8081/tcp)
	for ((attempt=0; attempt<90; attempt++)); do
		if curl --fail --silent --max-time 2 "http://127.0.0.1:${port##*:}/readyz" >/dev/null; then return; fi
		if [ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]; then break; fi
		sleep 1
	done
	docker logs "$container" >&2
	echo 'unattended Docker: server did not become ready' >&2
	return 1
}
stop() { docker stop --time 10 "$container" >/dev/null; docker rm "$container" >/dev/null; }
state() { helper '/tools/inspect /data/hikyo.db'; }
operator_pin() { helper 'sha256sum /state/unattended/operator.pub' | awk '{print $1}'; }
instance=
pin=
for label in a b c; do
	case "$label" in a) version=1.1.0-nightly.1 ;; b) version=1.1.0-nightly.2 ;; c) version=1.1.0-nightly.3 ;; esac
	seed_descriptor "$label"
	start "$label"
	healthy
	actual=$(state)
	if [ -z "$instance" ]; then instance=$(jq -er '.instance' <<<"$actual"); pin=$(operator_pin); fi
	jq -e --arg instance "$instance" --arg version "$version" '.instance == $instance and .version == $version and .maintenance == false and .phase == "healthy"' <<<"$actual" >/dev/null
	[ "$(operator_pin)" = "$pin" ]
	if [ "$label" != a ]; then
		helper 'cat /state/unattended/operation.json' | jq -e --arg version "$version" '.phase == "complete" and .target.version == $version' >/dev/null
	fi
	stop
	# Ordinary same-image restart uses persisted cache without any restaging.
	start "$label"
	healthy
	jq -e --arg instance "$instance" --arg version "$version" '.instance == $instance and .version == $version and .maintenance == false' <<<"$(state)" >/dev/null
	[ "$(operator_pin)" = "$pin" ]
	stop
	printf 'unattended Docker: %s replacement + restart preserve instance and custody\n' "$label"
done
seed_descriptor a
start a
for ((attempt=0; attempt<30; attempt++)); do
	[ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ] || break
	sleep 1
done
[ "$(docker inspect --format '{{.State.Running}}' "$container")" = false ] || { echo 'older image unexpectedly remained running' >&2; exit 1; }
[ "$(docker inspect --format '{{.State.ExitCode}}' "$container")" != 0 ]
jq -e --arg instance "$instance" '.instance == $instance and .version == "1.1.0-nightly.3" and .maintenance == false' <<<"$(state)" >/dev/null
[ "$(operator_pin)" = "$pin" ]
printf 'unattended Docker: older image refused; target data and custody unchanged\n'

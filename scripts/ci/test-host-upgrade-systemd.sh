#!/bin/sh
# Actual systemd runs only inside a disposable container with private namespaces.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir/../.."
case "$(docker info --format '{{.Architecture}} {{.OSType}}')" in
	'aarch64 linux' | 'arm64 linux') architecture=arm64 ;;
	'x86_64 linux' | 'amd64 linux') architecture=amd64 ;;
	*) printf 'systemd acceptance requires Linux amd64/arm64 Docker\n' >&2; exit 1 ;;
esac
work=$(mktemp -d)
container_id=
image_name="hikyo-systemd-acceptance:$$"
cleanup() {
	if [ -n "$container_id" ]; then docker rm -f "$container_id" >/dev/null; fi
	docker image rm "$image_name" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" go test -c -o "$work/hostupgrade.test" ./internal/hostupgrade
CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" go build -o "$work/host-upgrade-helper" ./scripts/ci/host-upgrade-helper
cp scripts/ci/host-upgrade-systemd.Dockerfile "$work/Dockerfile"
docker build --quiet -t "$image_name" "$work"
# systemd needs writable cgroups and mount namespaces to exercise the original
# hardened unit. No host cgroup bind, Docker socket, or source mounts.
container_id=$(docker run --detach --privileged --cgroupns=private --network=none \
	--pids-limit=128 --memory=512m --tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
	"$image_name")
attempt=0
until docker exec "$container_id" systemctl show --property=Version >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 30 ]; then docker logs "$container_id"; exit 1; fi
	sleep 1
done
if ! docker exec --env HIKYO_REAL_SYSTEMD_ACCEPTANCE=1 "$container_id" \
	/hostupgrade.test -test.v -test.run '^TestRealSystemdUpgrade$' -test.timeout=2m; then
	docker exec "$container_id" journalctl --unit hikyo.service --no-pager -n 100 || true
	exit 1
fi

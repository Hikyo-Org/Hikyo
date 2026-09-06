#!/bin/sh
# Root permission/credential/fencing acceptance in an isolated Linux container.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir/../.."
case "$(docker info --format '{{.Architecture}} {{.OSType}}')" in
	'aarch64 linux' | 'arm64 linux') architecture=arm64 ;;
	'x86_64 linux' | 'amd64 linux') architecture=amd64 ;;
	*) printf 'host-upgrade tests require a Linux amd64/arm64 Docker engine\n' >&2; exit 1 ;;
esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" go test -c -o "$work/hostupgrade.test" ./internal/hostupgrade
docker run --rm --network=none --read-only --pids-limit=64 --memory=256m \
	--tmpfs /root:mode=0700 --tmpfs /tmp:mode=1777 \
	--mount "type=bind,source=$work/hostupgrade.test,target=/hostupgrade.test,readonly" \
	alpine@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 \
	/hostupgrade.test -test.v -test.timeout=2m

#!/bin/sh
# Only configure ephemeral GitHub-hosted daemons. Local Docker settings belong
# to the operator. Canonical image names retain Docker Hub fallback on misses.
set -eu

[ "${GITHUB_ACTIONS:-}" = true ] || exit 0
[ "${RUNNER_ENVIRONMENT:-}" = github-hosted ] && [ "${RUNNER_OS:-}" = Linux ] || {
	printf 'Docker mirror setup requires an ephemeral Linux GitHub-hosted runner\n' >&2
	exit 1
}
mirror=https://mirror.gcr.io
mirrors=$(docker info --format '{{json .RegistryConfig.Mirrors}}')
if printf '%s\n' "$mirrors" | jq -e --arg mirror "$mirror" 'index($mirror) != null or index($mirror + "/") != null' >/dev/null; then
	exit 0
fi
# Restart only before fixtures start; never disrupt a running test container.
running=$(docker ps --quiet)
[ -z "$running" ] || {
	printf 'Docker mirror setup refuses to restart a daemon with running containers\n' >&2
	exit 1
}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
if sudo test -f /etc/docker/daemon.json; then
	sudo cat /etc/docker/daemon.json | tee "$work/current.json" >/dev/null
else
	printf '{}\n' >"$work/current.json"
fi
jq --arg mirror "$mirror" '."registry-mirrors" |= ((. // []) | if index($mirror) != null then . else . + [$mirror] end)' "$work/current.json" >"$work/daemon.json"
sudo dockerd --validate --config-file "$work/daemon.json"
sudo install -m 0600 "$work/daemon.json" /etc/docker/daemon.json
sudo systemctl restart docker
docker info --format '{{json .RegistryConfig.Mirrors}}' | jq -e --arg mirror "$mirror" 'index($mirror) != null or index($mirror + "/") != null' >/dev/null
printf 'Docker daemon cache configured with canonical Docker Hub fallback\n'

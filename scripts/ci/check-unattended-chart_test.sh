#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
chart="$script_dir/../../chart/hikyo"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-unattended-chart.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM

render() {
	helm template unattended "$chart" \
		--set database.existingSecret=fixture-database \
		--set rootKey.existingSecret=fixture-root \
		--set upgrade.stateExistingClaim=fixture-state \
		--set upgrade.unattended.enabled=true \
		--set upgrade.unattended.scratchDatabaseExistingSecret=fixture-scratch \
		--set externalOrigin=https://hikyo.example.com \
		--set 'network.trustedProxyCIDRs={10.42.0.0/16}' "$@"
}

render >"$scratch/rendered.yaml"
python3 - "$scratch/rendered.yaml" <<'PY'
import sys
import yaml

objects = list(yaml.safe_load_all(open(sys.argv[1], encoding="utf-8")))
deployment = next(item for item in objects if item and item["kind"] == "Deployment"
                  and any(container["name"] == "server" for container in item["spec"]["template"]["spec"]["containers"]))
assert deployment["spec"]["replicas"] == 1
assert deployment["spec"]["strategy"] == {"type": "Recreate"}
pod = deployment["spec"]["template"]["spec"]
assert pod["automountServiceAccountToken"] is False
server = next(item for item in pod["containers"] if item["name"] == "server")
env = {item["name"]: item for item in server["env"]}
assert env["HIKYO_UPGRADE_UNATTENDED"]["value"] == "true"
assert env["HIKYO_UPGRADE_STATE_DIR"]["value"] == "/var/lib/hikyo-upgrade/operator-custody"
assert env["HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"]["valueFrom"]["secretKeyRef"] == {
    "name": "fixture-scratch", "key": "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"
}
assert "value" not in env["HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"]
assert "HIKYO_UPGRADE_BUNDLE" not in env
assert "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY" not in env
assert server["startupProbe"]["httpGet"]["path"] == "/healthz"
assert server["startupProbe"]["failureThreshold"] == 180
assert server["readinessProbe"]["httpGet"]["path"] == "/readyz"
assert server["livenessProbe"]["httpGet"]["path"] == "/healthz"
volumes = {item["name"]: item for item in pod["volumes"]}
assert volumes["upgrade-state"]["persistentVolumeClaim"]["claimName"] == "fixture-state"
assert "upgrade-public" not in volumes
assert all("hostPath" not in item for item in volumes.values())
PY

for setting in 'replicaCount=2' 'ha.enabled=true' 'rollout.enabled=true' \
	'upgrade.evidence=true' 'upgrade.legacyWritersStopped=true' \
	'upgrade.existingClaim=manual-public' 'upgrade.unattended.scratchDatabaseExistingSecret=' \
	'upgrade.unattended.startupFailureThreshold=0'; do
	if render --set "$setting" >"$scratch/refused.yaml" 2>"$scratch/refused.error"; then
		printf 'Unattended chart accepted incompatible setting: %s\n' "$setting" >&2
		exit 1
	fi
done
printf 'Unattended chart: persistent state, secret reference, probes and incompatible settings verified\n'

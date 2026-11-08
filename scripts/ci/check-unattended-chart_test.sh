#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_chart="$script_dir/../../chart/hikyo"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-unattended-chart.XXXXXX")
fake=''
trap '[ -z "$fake" ] || kill "$fake" 2>/dev/null || true; rm -rf "$scratch"' EXIT HUP INT TERM
# The cases run against a copy without the shipped feed root, so they hold before and
# after the maintainers' key ceremony commits one.
cp -R "$repo_chart" "$scratch/chart"
rm -f "$scratch/chart/files/security-feed-root.json"
chart="$scratch/chart"

render() {
	helm template "${release:-unattended}" "$chart" \
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
names = [item["name"] for item in server["env"]]
# The Kubernetes security adapter refuses a server Deployment that names a variable twice, so a
# default install would be unsupported for automatic updates.
assert len(names) == len(set(names)), [name for name in names if names.count(name) > 1]
env = {item["name"]: item for item in server["env"]}
assert env["HIKYO_UPGRADE_UNATTENDED"]["value"] == "true"
assert env["HIKYO_UPGRADE_STATE_DIR"]["value"] == "/var/lib/hikyo-upgrade/operator-custody"
assert env["HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"]["valueFrom"]["secretKeyRef"] == {
    "name": "fixture-scratch", "key": "HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"
}
assert "value" not in env["HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"]
# Stage and inspect replace the server arguments, so env must carry the root key.
assert env["HIKYO_ROOT_KEY_FILE"]["value"] == "/run/hikyo-root-key/root-key"
assert deployment["metadata"]["labels"]["hikyo.dev/security-maintenance"] == "helm"
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
render --set securityMaintenance.enabled=false >"$scratch/plain.yaml"
python3 - "$scratch/plain.yaml" "$scratch/rendered.yaml" <<'PY'
import sys
import yaml

def server_deployment(path):
    return next(item for item in yaml.safe_load_all(open(path, encoding="utf-8")) if item and item["kind"] == "Deployment"
                and any(container["name"] == "server" for container in item["spec"]["template"]["spec"]["containers"]))

plain, maintained = server_deployment(sys.argv[1]), server_deployment(sys.argv[2])
assert "hikyo.dev/security-maintenance" not in plain["metadata"]["labels"]
assert maintained["metadata"]["labels"].pop("hikyo.dev/security-maintenance") == "helm"
assert "hikyo.dev/security-maintenance" not in maintained["spec"]["template"]["metadata"]["labels"]
# With no live Deployment (helm template) and no feed root, the default changes only that label.
assert plain == maintained
PY

# Defaults without unattended upgrades: security maintenance is not in effect, so the
# render is byte-identical to switching it off. No label, no coordinator, no failure,
# even with restricted egress and no coordinator API server address.
manual() {
	helm template manual "$chart" \
		--set database.existingSecret=fixture-database --set rootKey.existingSecret=fixture-root \
		--set upgrade.existingClaim=fixture-public --set upgrade.stateExistingClaim=fixture-state \
		--set externalOrigin=https://hikyo.example.com --set 'network.trustedProxyCIDRs={10.42.0.0/16}' "$@"
}
for restricted in false true; do
	set --
	[ "$restricted" = false ] ||
		set -- --set networkPolicy.egress.restricted=true --set-json 'networkPolicy.egress.datastore.to=[{"ipBlock":{"cidr":"10.20.0.15/32"}}]'
	manual "$@" >"$scratch/manual.yaml"
	manual "$@" --set securityMaintenance.enabled=false >"$scratch/manual-off.yaml"
	cmp -s "$scratch/manual.yaml" "$scratch/manual-off.yaml" || { printf 'Security maintenance changed a render without unattended upgrades\n' >&2; exit 1; }
	if grep -q 'security-maintenance\|security-status\|security-stage' "$scratch/manual.yaml"; then
		printf 'Security maintenance rendered without unattended upgrades\n' >&2
		exit 1
	fi
done

# The in-cluster coordinator. The root ships in the chart or comes from feedRoot; with
# neither it renders nothing and never fails. Enrollment is on by default, on install only.
printf '{"signed":{"_type":"root"}}\n' >"$scratch/feed-root.json"
coordinator() {
	render --set-file securityMaintenance.coordinator.feedRoot="$scratch/feed-root.json" "$@"
}
# No root: the coordinator renders nothing, the label stays, and the render succeeds.
render >"$scratch/noroot.yaml"
render --set networkPolicy.egress.restricted=true \
	--set-json 'networkPolicy.egress.datastore.to=[{"ipBlock":{"cidr":"10.20.0.15/32"}}]' >"$scratch/restricted-plain.yaml"
coordinator >"$scratch/coordinator.yaml"
coordinator --set securityMaintenance.coordinator.enroll=false >"$scratch/withheld.yaml"
coordinator --set securityMaintenance.coordinator.enrollOnUpgrade=true >"$scratch/upgrade-enroll.yaml"
coordinator --set securityMaintenance.coordinator.enabled=false >"$scratch/disabled.yaml"
coordinator --set securityMaintenance.enabled=false >"$scratch/off.yaml"
coordinator --set securityMaintenance.coordinator.acceptWorldWritableVolumeRoot=true >"$scratch/world-writable.yaml"
# Two releases in one namespace whose names share their first 30 characters keep apart.
release=hikyo-production-europe-west-1a coordinator >"$scratch/long-a.yaml"
release=hikyo-production-europe-west-1b coordinator >"$scratch/long-b.yaml"
release=unattended
python3 - "$scratch/long-a.yaml" "$scratch/long-b.yaml" "$scratch/coordinator.yaml" <<'PY'
import sys
import yaml

def names(path):
    return {(item["kind"], item["metadata"]["name"]) for item in yaml.safe_load_all(open(path, encoding="utf-8")) if item and "security-" in item["metadata"]["name"]}

a, b = names(sys.argv[1]), names(sys.argv[2])
assert a and len(a) == len(b) and not a & b, (a, b)
assert all(len(name) <= 52 for kind, name in a | b if kind == "CronJob"), a | b
assert ("CronJob", "unattended-hikyo-security-maintenance") in names(sys.argv[3])
PY
coordinator --set networkPolicy.egress.restricted=true \
	--set-json 'networkPolicy.egress.datastore.to=[{"ipBlock":{"cidr":"10.20.0.15/32"}}]' \
	--set-json 'securityMaintenance.coordinator.egress.apiServer.to=[{"ipBlock":{"cidr":"172.18.0.0/16"}}]' >"$scratch/restricted.yaml"
if coordinator --set networkPolicy.egress.restricted=true \
	--set-json 'networkPolicy.egress.datastore.to=[{"ipBlock":{"cidr":"10.20.0.15/32"}}]' >"$scratch/refused.yaml" 2>"$scratch/refused.error"; then
	printf 'Restricted egress rendered a coordinator that cannot reach the API server\n' >&2
	exit 1
fi
grep -q 'egress.apiServer.to' "$scratch/refused.error" || { printf 'Restricted egress refusal does not name the value\n' >&2; exit 1; }
python3 - "$scratch/noroot.yaml" "$scratch/coordinator.yaml" "$scratch/withheld.yaml" "$scratch/restricted.yaml" "$scratch/restricted-plain.yaml" "$scratch/feed-root.json" \
	"$scratch/upgrade-enroll.yaml" "$scratch/disabled.yaml" "$scratch/off.yaml" "$scratch/world-writable.yaml" <<'PY'
import json
import sys
import yaml

def load(path):
    return [item for item in yaml.safe_load_all(open(path, encoding="utf-8")) if item]

def of(objects, kind):
    return [item for item in objects if item["kind"] == kind]

def server(objects):
    return next(item for item in of(objects, "Deployment")
                if any(c["name"] == "server" for c in item["spec"]["template"]["spec"]["containers"]))

plain, coordinator, withheld, restricted, restricted_plain = (load(path) for path in sys.argv[1:6])
upgrade_enroll, disabled, off, world_writable = (load(path) for path in sys.argv[7:11])
root = open(sys.argv[6], encoding="utf-8").read()
name, status, stage = "unattended-hikyo-security-maintenance", "unattended-hikyo-security-status", "unattended-hikyo-security-stage"

def coordinator_objects(objects):
    return [item for item in objects if "security-" in item["metadata"]["name"] and item["kind"] != "Deployment"]

# Without a root, with the coordinator disabled, or with maintenance off, none of it renders.
for objects in (plain, disabled, off):
    assert not coordinator_objects(objects)
    assert "security-status" not in {v["name"] for v in server(objects)["spec"]["template"]["spec"]["volumes"]}
assert "hikyo.dev/security-maintenance" not in server(off)["metadata"]["labels"]
assert server(disabled)["metadata"]["labels"]["hikyo.dev/security-maintenance"] == "helm"

cron = of(coordinator, "CronJob")[0]
assert cron["metadata"]["name"] == name and cron["spec"]["concurrencyPolicy"] == "Forbid"
pod = cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]
assert pod["serviceAccountName"] == name and pod["automountServiceAccountToken"] is True
assert pod["securityContext"]["runAsNonRoot"] is True and pod["securityContext"]["runAsUser"] == 65532
container = pod["containers"][0]
assert container["args"] == ["security-maintenance", "run", "--once", "--config=/etc/hikyo/security-maintenance.json"]
assert container["securityContext"]["readOnlyRootFilesystem"] is True
# The coordinator refuses symbolic links, and a ConfigMap directory is made of them.
config_mounts = [m for m in container["volumeMounts"] if m["name"] == "config"]
assert len(config_mounts) == 2 and all(m.get("subPath") and m["readOnly"] for m in config_mounts)
assert {v["name"] for v in pod["volumes"]} == {"state", "config", "status", "tmp"}
# The public status is a few KiB; memory keeps it off a full node disk.
assert next(v for v in pod["volumes"] if v["name"] == "status")["emptyDir"] == {"medium": "Memory", "sizeLimit": "1Mi"}

# Consent: enroll is on by default and runs on install only; enrollOnUpgrade adds upgrade.
assert not of(withheld, "Job")
assert len(of(coordinator, "Job")) == 1
hook = of(coordinator, "Job")[0]
assert hook["metadata"]["annotations"]["helm.sh/hook"] == "post-install"
assert of(upgrade_enroll, "Job")[0]["metadata"]["annotations"]["helm.sh/hook"] == "post-install,post-upgrade"
assert hook["spec"]["backoffLimit"] > 0
assert hook["spec"]["template"]["spec"]["containers"][0]["args"][:2] == ["security-maintenance", "enroll"]
assert hook["spec"]["template"]["spec"]["serviceAccountName"] == name

# The server gains no token and no RBAC; it only reads the published status.
for objects in (coordinator, withheld):
    deployment = server(objects)
    spec = deployment["spec"]["template"]["spec"]
    assert spec["automountServiceAccountToken"] is False and "serviceAccountName" not in spec
    mounts = {m["name"]: m for m in spec["containers"][0]["volumeMounts"]}
    assert mounts["security-status"]["readOnly"] is True and "subPath" not in mounts["security-status"]
    volumes = {v["name"]: v for v in spec["volumes"]}
    assert volumes["security-status"]["configMap"] == {"name": status, "optional": True}
    env = {e["name"]: e for e in spec["containers"][0]["env"]}
    assert env["HIKYO_SECURITY_MAINTENANCE_STATUS_FILE"]["value"] == "/run/hikyo-security-status/status.json"
    for binding in of(objects, "RoleBinding"):
        assert all(s["name"] != "unattended-hikyo" for s in binding["subjects"])
plain_spec = server(plain)["spec"]["template"]["spec"]
assert "security-status" not in {v["name"] for v in plain_spec["volumes"]}
assert "HIKYO_SECURITY_MAINTENANCE_STATUS_FILE" not in {e["name"] for e in plain_spec["containers"][0]["env"]}

role = next(item for item in of(coordinator, "Role") if item["metadata"]["name"] == name)
assert role["rules"] == [
    {"apiGroups": ["apps"], "resources": ["deployments"], "resourceNames": ["unattended-hikyo"], "verbs": ["get", "update"]},
    {"apiGroups": ["apps"], "resources": ["replicasets"], "verbs": ["list"]},
    {"apiGroups": [""], "resources": ["pods"], "verbs": ["list", "create"]},
    {"apiGroups": [""], "resources": ["pods/exec"], "verbs": ["create", "get"]},
    {"apiGroups": [""], "resources": ["pods"], "resourceNames": [stage], "verbs": ["get", "delete"]},
    {"apiGroups": [""], "resources": ["pods/log"], "resourceNames": [stage], "verbs": ["get"]},
    {"apiGroups": ["batch"], "resources": ["cronjobs"], "resourceNames": [name], "verbs": ["get", "patch"]},
    {"apiGroups": [""], "resources": ["configmaps"], "resourceNames": [status], "verbs": ["get", "update"]},
], role["rules"]
binding = next(item for item in of(coordinator, "RoleBinding") if item["metadata"]["name"] == name)
assert binding["roleRef"]["name"] == name and binding["subjects"] == [{"kind": "ServiceAccount", "name": name, "namespace": "default"}]

configs = {item["metadata"]["name"]: item for item in of(coordinator, "ConfigMap")}
assert "data" not in configs[status]
rendered = configs[name]["data"]
assert rendered["security-feed-root.json"] == root
config = json.loads(rendered["security-maintenance.json"])
kubernetes = config["deployment"]["kubernetes"]
assert config["kind"] == "kubernetes" and config["feed"]["root_file"] == "/etc/hikyo/security-feed-root.json"
assert "kubeconfig" not in kubernetes and "deployment_uid" not in kubernetes and "kubectl" not in kubernetes
assert kubernetes["deployment"] == "unattended-hikyo" and kubernetes["cron_job"] == name
assert kubernetes["status_config_map"] == status and kubernetes["stage_pod"] == stage
assert config["state_directory"].startswith(kubernetes["volume_root"] + "/")
assert config["deployment"]["state_directory"] == config["state_directory"] + "/deployment"
# A world-writable volume root is refused unless the operator accepts it, and only then is the key rendered.
assert "accept_world_writable_volume_root" not in kubernetes
accepted = json.loads(next(item for item in of(world_writable, "ConfigMap") if item["metadata"]["name"] == name)["data"]["security-maintenance.json"])["deployment"]["kubernetes"]
assert accepted == dict(kubernetes, accept_world_writable_volume_root=True), accepted
assert config["feed"]["metadata_url"] == "https://hikyo.app/security-feed/metadata/"
assert config["policy"]["mode"] == "automatic"
pvc = next(item for item in of(coordinator, "PersistentVolumeClaim") if item["metadata"]["name"] == name + "-state")
assert pvc["spec"]["accessModes"] == ["ReadWriteOnce"]

# Restricted egress: the server's policy is the same with and without the coordinator,
# and the coordinator pods carry labels that policy does not select.
def policies(objects):
    return {item["metadata"]["name"]: item for item in of(objects, "NetworkPolicy")}
with_coordinator, without = policies(restricted), policies(restricted_plain)
assert with_coordinator["unattended-hikyo-server"] == without["unattended-hikyo-server"]
mine = with_coordinator[name]["spec"]
selector = mine["podSelector"]["matchLabels"]
server_labels = server(restricted)["spec"]["template"]["metadata"]["labels"]
assert not all(server_labels.get(k) == v for k, v in selector.items())
assert not all(selector.get(k) == v for k, v in without["unattended-hikyo-server"]["spec"]["podSelector"]["matchLabels"].items())
assert mine["policyTypes"] == ["Ingress", "Egress"] and mine["ingress"] == []
destinations = [rule["to"][0] for rule in mine["egress"]]
assert {"ipBlock": {"cidr": "172.18.0.0/16"}} in destinations
assert all(rule["ports"] for rule in mine["egress"])
assert name not in policies(coordinator)
PY
# helm template cannot answer `lookup`, so the live-image policy is checked against a
# stub API server that serves discovery and one Deployment the coordinator has replaced.
live="ghcr.io/hikyo-org/hikyo@sha256:$(printf 'b%.0s' $(seq 64))"
requested="sha256:$(printf 'c%.0s' $(seq 64))"
cat >"$scratch/fake-kube-api.py" <<'PY'
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

live, namespace, name, coordinator = sys.argv[1:5]
kinds = {
    "apps/v1": [("deployments", "Deployment")],
    "networking.k8s.io/v1": [("networkpolicies", "NetworkPolicy")],
    "rbac.authorization.k8s.io/v1": [("clusterroles", "ClusterRole"), ("clusterrolebindings", "ClusterRoleBinding"),
                                      ("roles", "Role"), ("rolebindings", "RoleBinding")],
    "v1": [("configmaps", "ConfigMap"), ("services", "Service"), ("serviceaccounts", "ServiceAccount"),
           ("secrets", "Secret"), ("persistentvolumeclaims", "PersistentVolumeClaim")],
    "batch/v1": [("cronjobs", "CronJob"), ("jobs", "Job")],
}

def resources(group_version):
    return {"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": group_version, "resources": [
        {"name": plural, "singularName": kind.lower(), "namespaced": not kind.startswith("Cluster"), "kind": kind,
         "verbs": ["get", "list", "create", "update", "patch", "delete"]} for plural, kind in kinds[group_version]]}

deployment = {"apiVersion": "apps/v1", "kind": "Deployment",
              "metadata": {"name": name, "namespace": namespace, "labels": {"app.kubernetes.io/managed-by": "Helm"},
                           "annotations": {"meta.helm.sh/release-name": "unattended", "meta.helm.sh/release-namespace": namespace}},
              "spec": {"template": {"metadata": {"annotations": {"hikyo.dev/security-release-manifest": "a" * 64}},
                                    "spec": {"containers": [{"name": "server", "image": live}]}}}}
cronjob = {"apiVersion": "batch/v1", "kind": "CronJob", "metadata": {"name": name + "-security-maintenance", "namespace": namespace, "labels": {"app.kubernetes.io/managed-by": "Helm"},
                                                      "annotations": {"meta.helm.sh/release-name": "unattended", "meta.helm.sh/release-namespace": namespace}},
           "spec": {"schedule": "0 * * * *", "jobTemplate": {"spec": {"template": {"spec": {"containers": [{"name": "coordinator", "image": coordinator}]}}}}}}
routes = {
    "/version": {"major": "1", "minor": "30", "gitVersion": "v1.30.0"},
    "/api": {"kind": "APIVersions", "versions": ["v1"]},
    "/api/v1": resources("v1"),
    "/apis": {"kind": "APIGroupList", "apiVersion": "v1", "groups": [
        {"name": gv.split("/")[0], "versions": [{"groupVersion": gv, "version": "v1"}],
         "preferredVersion": {"groupVersion": gv, "version": "v1"}} for gv in kinds if "/" in gv]},
    "/apis/apps/v1/namespaces/%s/deployments/%s" % (namespace, name): deployment,
    "/apis/batch/v1/namespaces/%s/cronjobs/%s-security-maintenance" % (namespace, name): cronjob,
}
routes.update({"/apis/" + gv: resources(gv) for gv in kinds if "/" in gv})

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = routes.get(self.path.split("?")[0])
        self.send_response(200 if body else 404)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(body or {"kind": "Status", "apiVersion": "v1", "status": "Failure", "code": 404, "reason": "NotFound"}).encode())

    def log_message(self, *args):
        pass

server = HTTPServer(("127.0.0.1", 0), Handler)
print(server.server_port, flush=True)
server.serve_forever()
PY
# The Deployment already runs a target the coordinator has not proven; its CronJob runs the proven release.
proven="ghcr.io/hikyo-org/hikyo@sha256:$(printf 'd%.0s' $(seq 64))"
python3 "$scratch/fake-kube-api.py" "$live" default unattended-hikyo "$proven" >"$scratch/fake-port" &
fake=$!
waited=0
until [ -s "$scratch/fake-port" ]; do
	waited=$((waited + 1))
	if [ "$waited" -gt 100 ] || ! kill -0 "$fake" 2>/dev/null; then
		printf 'The stub Kubernetes API server did not start\n' >&2
		exit 1
	fi
	sleep 0.1
done
cat >"$scratch/kubeconfig" <<EOF
apiVersion: v1
kind: Config
clusters: [{name: stub, cluster: {server: "http://127.0.0.1:$(cat "$scratch/fake-port")"}}]
contexts: [{name: stub, context: {cluster: stub, user: stub}}]
users: [{name: stub, user: {}}]
current-context: stub
EOF
chmod 600 "$scratch/kubeconfig"
release=unattended
live_render() {
	helm install "$release" "$chart" --dry-run=server --disable-openapi-validation --kubeconfig "$scratch/kubeconfig" --namespace default \
		--set database.existingSecret=fixture-database --set rootKey.existingSecret=fixture-root \
		--set upgrade.stateExistingClaim=fixture-state --set upgrade.unattended.enabled=true \
		--set upgrade.unattended.scratchDatabaseExistingSecret=fixture-scratch --set externalOrigin=https://hikyo.example.com \
		--set 'network.trustedProxyCIDRs={10.42.0.0/16}' --set securityMaintenance.enabled=true "$@"
}
coordinator_live() {
	live_render --set securityMaintenance.coordinator.enabled=true --set-file securityMaintenance.coordinator.feedRoot="$scratch/feed-root.json" "$@"
}
coordinator_images() { grep -A2 'name: coordinator$' "$1" | sed -n 's/^ *image: "\(.*\)"$/\1/p' | sort -u; }
coordinator_live --set image.digest="$requested" >"$scratch/coordinator-kept.txt"
coordinator_live --set image.digest="$requested" --set securityMaintenance.keepLiveImage=false >"$scratch/coordinator-override.txt"
release=fresh
coordinator_live --set image.digest="$requested" >"$scratch/coordinator-fresh.txt"
release=unattended
live_render --set image.digest="$requested" >"$scratch/live-kept.txt"
live_render --set image.digest="$requested" --set securityMaintenance.keepLiveImage=false >"$scratch/live-override.txt"
live_render --set image.digest="${live#*@}" >"$scratch/live-same.txt"
server_image() { grep -A12 'name: server$' "$1" | sed -n 's/^ *image: "\(.*\)"$/\1/p' | head -n 1; }
[ "$(server_image "$scratch/live-kept.txt")" = "$live" ] || { printf 'helm upgrade did not keep the live image\n' >&2; exit 1; }
[ "$(server_image "$scratch/live-override.txt")" = "ghcr.io/hikyo-org/hikyo@$requested" ] || { printf 'keepLiveImage=false did not apply image.digest\n' >&2; exit 1; }
# The Deployment runs an unproven image, so the coordinator must keep the CronJob's proven one.
[ "$(coordinator_images "$scratch/coordinator-kept.txt")" = "$proven" ] || { printf 'helm upgrade moved the coordinator off its proven image\n' >&2; exit 1; }
[ "$(coordinator_images "$scratch/coordinator-override.txt")" = "ghcr.io/hikyo-org/hikyo@$requested" ] || { printf 'keepLiveImage=false did not apply image.digest to the coordinator\n' >&2; exit 1; }
[ "$(coordinator_images "$scratch/coordinator-fresh.txt")" = "ghcr.io/hikyo-org/hikyo@$requested" ] || { printf 'A coordinator without a CronJob did not take the server image\n' >&2; exit 1; }
grep -q 'WARNING: image.digest was NOT applied' "$scratch/live-kept.txt" || { printf 'A kept image differing from image.digest was not warned about\n' >&2; exit 1; }
grep -q "running   $live" "$scratch/live-kept.txt" || { printf 'The warning does not name the running image\n' >&2; exit 1; }
if grep -q 'NOT applied' "$scratch/live-override.txt" "$scratch/live-same.txt"; then
	printf 'Warned although the requested image was applied\n' >&2
	exit 1
fi

# NOTES.txt is not part of helm template, so it is read from the server dry run.
release=fresh
live_render >"$scratch/notes-noroot.txt"
grep -q 'Automatic security updates are OFF: this chart ships no feed trust root' "$scratch/notes-noroot.txt" || { printf 'NOTES does not say why automatic security updates are off without a root\n' >&2; exit 1; }
coordinator_live >"$scratch/notes-root.txt"
if ! grep -q 'authorizes automatic replacement of the server image' "$scratch/notes-root.txt" || ! grep -q 'on install only' "$scratch/notes-root.txt"; then
	printf 'NOTES does not state the enrollment consent\n' >&2
	exit 1
fi
if grep -q 'Automatic security updates are OFF' "$scratch/notes-root.txt"; then
	printf 'NOTES says security updates are off although a root is set\n' >&2
	exit 1
fi
helm install manual "$chart" --dry-run=server --disable-openapi-validation --kubeconfig "$scratch/kubeconfig" --namespace default \
	--set database.existingSecret=fixture-database --set rootKey.existingSecret=fixture-root \
	--set upgrade.existingClaim=fixture-public --set upgrade.stateExistingClaim=fixture-state \
	--set externalOrigin=https://hikyo.example.com --set 'network.trustedProxyCIDRs={10.42.0.0/16}' >"$scratch/notes-manual.txt"
grep -q 'Automatic security updates are off: they need upgrade.unattended.enabled=true' "$scratch/notes-manual.txt" || { printf 'NOTES does not say unattended upgrades are off\n' >&2; exit 1; }
release=unattended

# A root shipped in the chart renders without feedRoot, and feedRoot overrides it. The
# repository ships none before the key ceremony, so the file is added to a copy.
cp -R "$chart" "$scratch/rooted-chart"
printf '{"signed":{"_type":"root","shipped":true}}\n' >"$scratch/rooted-chart/files/security-feed-root.json"
helm template unattended "$scratch/rooted-chart" \
	--set database.existingSecret=fixture-database --set rootKey.existingSecret=fixture-root \
	--set upgrade.stateExistingClaim=fixture-state --set upgrade.unattended.enabled=true \
	--set upgrade.unattended.scratchDatabaseExistingSecret=fixture-scratch --set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' >"$scratch/shipped-root.yaml"
helm template unattended "$scratch/rooted-chart" \
	--set database.existingSecret=fixture-database --set rootKey.existingSecret=fixture-root \
	--set upgrade.stateExistingClaim=fixture-state --set upgrade.unattended.enabled=true \
	--set upgrade.unattended.scratchDatabaseExistingSecret=fixture-scratch --set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
	--set-file securityMaintenance.coordinator.feedRoot="$scratch/feed-root.json" >"$scratch/override-root.yaml"
python3 - "$scratch/shipped-root.yaml" "$scratch/override-root.yaml" "$scratch/feed-root.json" <<'PY'
import sys
import yaml

def root(path):
    objects = [item for item in yaml.safe_load_all(open(path, encoding="utf-8")) if item]
    assert [item for item in objects if item["kind"] == "CronJob"]
    return next(item for item in objects if item["kind"] == "ConfigMap" and item["metadata"]["name"].endswith("-security-maintenance"))["data"]["security-feed-root.json"]

assert '"shipped":true' in root(sys.argv[1])
assert root(sys.argv[2]) == open(sys.argv[3], encoding="utf-8").read()
PY

# The chart's root and the installer's are one file once the key ceremony commits them; a
# root in only one place fails cmp.
if [ -e "$repo_chart/files/security-feed-root.json" ] || [ -e "$script_dir/../../install/security-maintenance/security-feed-root.json" ]; then
	cmp "$repo_chart/files/security-feed-root.json" "$script_dir/../../install/security-maintenance/security-feed-root.json" || {
		printf 'chart/hikyo/files/security-feed-root.json differs from install/security-maintenance/security-feed-root.json\n' >&2
		exit 1
	}
fi

# The runbook's one-off Job derivation is run as written, with kubectl standing in for
# the cluster and the stub API server supplying discovery.
if command -v kubectl >/dev/null && command -v jq >/dev/null; then
	python3 - "$scratch/coordinator.yaml" "$scratch/cronjob.json" <<'PY'
import json
import sys
import yaml

cron = next(item for item in yaml.safe_load_all(open(sys.argv[1], encoding="utf-8")) if item and item["kind"] == "CronJob")
json.dump(cron, open(sys.argv[2], "w"))
PY
	mkdir "$scratch/bin"
	cat >"$scratch/bin/kubectl" <<EOF
#!/bin/sh
case "\$*" in
*"get cronjob"*) cat "$scratch/cronjob.json" ;;
*" create "*) tee "$scratch/job.json" | KUBECONFIG="$scratch/kubeconfig" "$(command -v kubectl)" "\$@" --dry-run=client --validate=false ;;
*) exit 9 ;;
esac
EOF
	chmod +x "$scratch/bin/kubectl"
	sed -n '/^hikyo_coordinator() {/,/^}/p' "$script_dir/../../docs/operations/security-maintenance.md" >"$scratch/runbook.sh"
	[ -s "$scratch/runbook.sh" ] || { printf 'The runbook no longer defines hikyo_coordinator\n' >&2; exit 1; }
	PATH="$scratch/bin:$PATH" NS=hikyo CRON=unattended-hikyo-security-maintenance sh -c '. "$1"; hikyo_coordinator reconcile --confirm abc' sh "$scratch/runbook.sh" >"$scratch/job.name"
	grep -q '^job.batch/' "$scratch/job.name" || { printf 'The runbook command did not create a Job\n' >&2; exit 1; }
	python3 - "$scratch/job.json" "$scratch/cronjob.json" <<'PY'
import json
import sys

job, cron = json.load(open(sys.argv[1])), json.load(open(sys.argv[2]))
assert job["kind"] == "Job" and job["metadata"]["generateName"] and "name" not in job["metadata"]
pod, source = job["spec"]["template"], cron["spec"]["jobTemplate"]["spec"]["template"]
container, original = pod["spec"]["containers"][0], source["spec"]["containers"][0]
assert container["args"] == ["security-maintenance", "reconcile", "--confirm", "abc", "--config=/etc/hikyo/security-maintenance.json"]
# Everything but the arguments is the CronJob's: identity, state, config and network labels.
assert {k: v for k, v in container.items() if k != "args"} == {k: v for k, v in original.items() if k != "args"}
assert pod["spec"]["serviceAccountName"] == source["spec"]["serviceAccountName"] and pod["spec"]["volumes"] == source["spec"]["volumes"]
assert pod["metadata"]["labels"] == source["metadata"]["labels"]
assert job["spec"]["backoffLimit"] == 0
PY
else
	printf 'kubectl or jq missing: the runbook Job command was not exercised\n' >&2
fi
printf 'Security maintenance chart: live image kept and deliberate override verified\n'
printf 'Security maintenance coordinator: shipped or overridden root, consent, RBAC scope, server isolation and egress verified\n'
printf 'Unattended chart: persistent state, secret reference, probes and incompatible settings verified\n'

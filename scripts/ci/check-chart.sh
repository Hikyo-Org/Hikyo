#!/bin/sh
set -eu

# Structured chart check: renders the operator chart across modes and asserts the
# RBAC rules, security contexts, container args and env EXACTLY, by parsing the
# rendered YAML per document (python3 + PyYAML) rather than substring-grepping —
# a substring check passes a chart whose verbs, resourceNames or env drifted.

if [ "$#" -gt 1 ]; then
	printf 'usage: %s [CHART]\n' "$0" >&2
	exit 2
fi

chart=${1:-chart/hikyo}
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-chart-check.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

if ! python3 -c 'import yaml' >/dev/null 2>&1; then
	printf 'Chart check: python3 with PyYAML is required\n' >&2
	exit 2
fi

fail() {
	printf 'Chart check: %s\n' "$1" >&2
	exit 1
}

render_mode() {
	name=$1
	shift
	helm lint "$chart" \
		--set database.existingSecret=fixture \
		--set rootKey.existingSecret=fixture-root-key \
		--set externalOrigin=https://hikyo.example.com \
		--set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
		"$@" >/dev/null
	helm template fixture "$chart" \
		--set database.existingSecret=fixture \
		--set rootKey.existingSecret=fixture-root-key \
		--set externalOrigin=https://hikyo.example.com \
		--set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
		"$@" >"$tmp/$name.yaml"
}

render_mode cluster-wide \
	--set 'operator.designatedServiceAccounts.ns-a={sa-a,sa-shared}' \
	--set 'operator.designatedServiceAccounts.ns-b={sa-b,sa-shared}'
render_mode namespaced \
	--set 'operator.namespaces={ns-a,ns-b}' \
	--set 'operator.designatedServiceAccounts.ns-a={sa-a,sa-shared}' \
	--set 'operator.designatedServiceAccounts.ns-b={sa-b}'
render_mode no-rollouts --set operator.triggerRollouts=false
render_mode native-tls \
	--set 'network.trustedProxyCIDRs={}' \
	--set tls.existingSecret=fixture-tls

python3 - "$tmp/cluster-wide.yaml" "$tmp/namespaced.yaml" "$tmp/no-rollouts.yaml" "$tmp/native-tls.yaml" <<'PY' || exit 1
import sys, yaml

cluster_wide, namespaced, no_rollouts, native_tls = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]

def load(path):
    with open(path) as f:
        return [d for d in yaml.safe_load_all(f) if d]

def fail(msg):
    print(f"Chart check: {msg}", file=sys.stderr)
    sys.exit(1)

def by(docs, kind, name=None, namespace=None):
    out = []
    for d in docs:
        if d.get("kind") != kind:
            continue
        m = d.get("metadata", {})
        if name is not None and m.get("name") != name:
            continue
        if namespace is not None and m.get("namespace") != namespace:
            continue
        out.append(d)
    return out

def one(docs, kind, name=None, namespace=None):
    m = by(docs, kind, name, namespace)
    if len(m) != 1:
        fail(f"expected exactly one {kind} name={name} ns={namespace}, found {len(m)}")
    return m[0]

OP = "fixture-hikyo-operator"

# Exact rule model: normalize each rendered rule to a canonical, order-independent
# tuple, then compare the WHOLE rule set of every operator (Cluster)Role to an
# expected list per mode. A set comparison — not per-rule spot checks — is what
# catches a stray rule, a widened verb, or a dropped restriction; nothing may be
# present that is not expected, and nothing expected may be missing.
def norm(r):
    return (
        tuple(sorted(r.get("apiGroups", []))),
        tuple(sorted(r.get("resources", []))),
        tuple(sorted(r.get("verbs", []))),
        tuple(sorted(r.get("resourceNames", []))),
    )

def rule(groups, resources, verbs, names=()):
    return (tuple(sorted(groups)), tuple(sorted(resources)), tuple(sorted(verbs)), tuple(sorted(names)))

def expect_rules(rules, expected, where):
    got = sorted(norm(r) for r in rules)
    want = sorted(expected)
    if got != want:
        missing = [r for r in want if r not in got]
        extra = [r for r in got if r not in want]
        fail(f"{where}: RBAC rule set mismatch; missing={missing} extra={extra}")

# Rule building blocks (the operator's full ADR verb surface, § 0.10).
INSTANCES = rule(["hikyo.dev"], ["hikyoinstances"], ["get", "list", "watch"])
CRD = rule(["apiextensions.k8s.io"], ["customresourcedefinitions"], ["get"],
           ["hikyoinstances.hikyo.dev", "hikyosecrets.hikyo.dev"])
HIKYOSECRETS = rule(["hikyo.dev"], ["hikyosecrets"], ["get", "list", "watch", "patch"])
STATUS = rule(["hikyo.dev"], ["hikyosecrets/status"], ["update", "patch"])
FINALIZERS = rule(["hikyo.dev"], ["hikyosecrets/finalizers"], ["update"])
EVENTS = rule([""], ["events"], ["create", "patch"])
SECRETS = rule([""], ["secrets"], ["get", "create", "update", "patch"])
WORKLOAD = rule(["apps"], ["deployments", "statefulsets", "daemonsets"], ["get", "list", "watch", "patch"])
SERVICEACCOUNTS = rule([""], ["serviceaccounts"], ["get"])

# Cluster-scoped reads that always live on the ClusterRole.
CLUSTER_READS = [INSTANCES, CRD]
# Per-CR converge rules; secrets is get/create/update/patch ONLY (never list/watch).
CONVERGE = [HIKYOSECRETS, STATUS, FINALIZERS, EVENTS, SECRETS, SERVICEACCOUNTS]

def assert_rbac_inventory(docs, expected, mode):
    # Closure over WHICH RBAC objects exist, not only their rules: a brand-new
    # stray Role/ClusterRole (e.g. a rogue secrets list/watch grant under an
    # unexpected name) passes every per-role rule check because nothing looks at it.
    # Compare the full (kind, name, namespace) inventory to the expected set.
    got = set()
    for d in docs:
        if d.get("kind") in ("Role", "ClusterRole"):
            m = d.get("metadata", {})
            got.add((d["kind"], m.get("name"), m.get("namespace")))
    want = set(expected)
    if got != want:
        fail(f"{mode}: RBAC object inventory mismatch; missing={sorted(want - got)} extra={sorted(got - want)}")

def assert_leader_election(docs, mode):
    le = one(docs, "Role", f"{OP}-leader-election", "default")
    expect_rules(le["rules"], [
        rule(["coordination.k8s.io"], ["leases"], ["get", "create", "update"]),
    ], f"{mode} leader-election Role")

def assert_token_role(docs, ns, want_names):
    role = one(docs, "Role", f"{OP}-token", ns)
    rules = role["rules"]
    if len(rules) != 1:
        fail(f"token Role {ns}: expected exactly one rule, got {len(rules)}")
    r = rules[0]
    if r.get("resources") != ["serviceaccounts/token"] or r.get("verbs") != ["create"]:
        fail(f"token Role {ns}: rule = {r}")
    if sorted(r.get("resourceNames", [])) != sorted(want_names):
        fail(f"token Role {ns}: resourceNames = {r.get('resourceNames')}, want {want_names}")
    # Bound to the operator SA in the release namespace.
    rb = one(docs, "RoleBinding", f"{OP}-token", ns)
    subj = rb["subjects"][0]
    if subj["namespace"] != "default" or subj["name"] != OP:
        fail(f"token RoleBinding {ns}: subject = {subj}")

def assert_hardened(docs, mode):
    deploys = by(docs, "Deployment")
    if len(deploys) != 2:
        fail(f"{mode}: expected 2 Deployments, got {len(deploys)}")
    op = None
    for d in deploys:
        pod = d["spec"]["template"]["spec"]
        psc = pod.get("securityContext", {})
        if not psc.get("runAsNonRoot"):
            fail(f"{mode}: a pod securityContext is not runAsNonRoot")
        if psc.get("seccompProfile", {}).get("type") != "RuntimeDefault":
            fail(f"{mode}: a pod is missing seccompProfile RuntimeDefault")
        c = pod["containers"][0]
        csc = c.get("securityContext", {})
        if not csc.get("readOnlyRootFilesystem"):
            fail(f"{mode}: a container is not readOnlyRootFilesystem")
        if csc.get("allowPrivilegeEscalation") is not False:
            fail(f"{mode}: a container allows privilege escalation")
        if csc.get("capabilities", {}).get("drop") != ["ALL"]:
            fail(f"{mode}: a container does not drop ALL capabilities")
        if c["name"] == "operator":
            op = c
    if op is None:
        fail(f"{mode}: no operator container")
    if op.get("args") != ["operator"]:
        fail(f"{mode}: operator args = {op.get('args')}, want [operator]")
    assert_env_allowlist(op, mode)
    return op

def assert_server_network(docs, mode, tls):
    server = None
    deployment = None
    for d in by(docs, "Deployment"):
        for c in d["spec"]["template"]["spec"]["containers"]:
            if c["name"] == "server":
                deployment, server = d, c
    if server is None:
        fail(f"{mode}: no server container")
    want_args = [
        "server",
        "--listen=0.0.0.0:8080",
        "--operational-listen=0.0.0.0:8081",
        "--root-key-file=/run/hikyo-root-key/root-key",
    ]
    if tls:
        want_args += ["--tls-cert-file=/run/hikyo-tls/tls.crt", "--tls-key-file=/run/hikyo-tls/tls.key"]
    if server.get("args") != want_args:
        fail(f"{mode}: server args = {server.get('args')}, want {want_args}")
    ports = {p["name"]: p["containerPort"] for p in server.get("ports", [])}
    if ports != {"http": 8080, "ops": 8081}:
        fail(f"{mode}: server ports = {ports}")
    liveness = server.get("livenessProbe", {})
    if liveness != {
        "httpGet": {"path": "/healthz", "port": "ops"},
        "initialDelaySeconds": 5,
        "periodSeconds": 10,
        "failureThreshold": 3,
    }:
        fail(f"{mode}: liveness probe = {liveness}")
    readiness = server.get("readinessProbe", {})
    if readiness != {
        "httpGet": {"path": "/readyz", "port": "ops"},
        "periodSeconds": 5,
        "failureThreshold": 3,
    }:
        fail(f"{mode}: readiness probe = {readiness}")
    startup = server.get("startupProbe", {})
    if startup != {
        "httpGet": {"path": "/readyz", "port": "ops"},
        "periodSeconds": 5,
        "failureThreshold": 30,
    }:
        fail(f"{mode}: startup probe = {startup}")
    env_names = {e["name"] for e in server.get("env", [])}
    if "HIKYO_EXTERNAL_ORIGIN" not in env_names or "HIKYO_ROOT_KEY" in env_names:
        fail(f"{mode}: server origin/root-key env boundary = {env_names}")
    if ("HIKYO_TRUSTED_PROXY_CIDRS" in env_names) == tls:
        fail(f"{mode}: trusted-proxy env presence does not match transport mode: {env_names}")
    mounts = {m["name"]: m for m in server.get("volumeMounts", [])}
    if mounts.get("root-key", {}).get("mountPath") != "/run/hikyo-root-key" or not mounts["root-key"].get("readOnly"):
        fail(f"{mode}: staged root-key mount = {mounts.get('root-key')}")
    if mounts.get("tmp", {}).get("mountPath") != "/tmp":
        fail(f"{mode}: writable tmp mount = {mounts.get('tmp')}")
    pod = deployment["spec"]["template"]["spec"]
    volumes = {v["name"]: v for v in pod.get("volumes", [])}
    root_source = volumes.get("root-key-source", {}).get("secret", {})
    if root_source.get("secretName") != "fixture-root-key" or root_source.get("defaultMode") != 0o400:
        fail(f"{mode}: root-key source volume = {root_source}")
    if root_source.get("items") != [{"key": "root-key", "path": "root-key"}]:
        fail(f"{mode}: root-key source item = {root_source.get('items')}")
    if volumes.get("root-key", {}).get("emptyDir") != {}:
        fail(f"{mode}: staged root-key emptyDir = {volumes.get('root-key')}")
    if volumes.get("tmp", {}).get("emptyDir") != {}:
        fail(f"{mode}: writable tmp emptyDir = {volumes.get('tmp')}")
    if pod.get("securityContext", {}).get("fsGroup") != 65532:
        fail(f"{mode}: Secret sources are not readable through fsGroup")
    root_init = {c["name"]: c for c in pod.get("initContainers", [])}.get("root-key-stage", {})
    if root_init.get("args") != ["__hikyo-stage-root-key"]:
        fail(f"{mode}: root-key staging init args = {root_init.get('args')}")
    if tls:
        if mounts.get("tls", {}).get("mountPath") != "/run/hikyo-tls" or not mounts["tls"].get("readOnly"):
            fail(f"{mode}: TLS mount = {mounts.get('tls')}")
        secret = volumes.get("tls-source", {}).get("secret", {})
        if secret.get("secretName") != "fixture-tls" or secret.get("defaultMode") != 0o400:
            fail(f"{mode}: TLS volume = {secret}")
        if volumes.get("tls", {}).get("emptyDir") != {}:
            fail(f"{mode}: staged TLS emptyDir = {volumes.get('tls')}")
        init = {c["name"]: c for c in pod.get("initContainers", [])}.get("tls-stage", {})
        if init.get("args") != ["__hikyo-stage-tls", "--once"]:
            fail(f"{mode}: TLS staging init args = {init.get('args')}")
        watcher = None
        for c in pod.get("containers", []):
            if c.get("name") == "tls-stage-watch":
                watcher = c
        if watcher is None or watcher.get("args") != ["__hikyo-stage-tls"]:
            fail(f"{mode}: TLS staging watcher missing or misconfigured")

# The operator container's env is an EXACT allowlist: only the operator's own
# scoping/config vars, never database or listener config. Set equality catches
# both an extra variable (leak) and a missing one (drift).
ALLOWED_ENV = {
    "HIKYO_OPERATOR_NAMESPACES",
    "HIKYO_OPERATOR_TRIGGER_ROLLOUTS",
    "HIKYO_OPERATOR_NAMESPACE",
    "POD_NAMESPACE",
}

def assert_env_allowlist(op, mode):
    names = [e["name"] for e in op.get("env", [])]
    if len(names) != len(set(names)):
        fail(f"{mode}: duplicate operator env names: {names}")
    if set(names) != ALLOWED_ENV:
        fail(f"{mode}: operator env = {sorted(names)}, want exactly {sorted(ALLOWED_ENV)}")

# ---- cluster-wide ----
# The single ClusterRole carries cluster reads + all converge rules + workload.
cw = load(cluster_wide)
assert_rbac_inventory(cw, [
    ("ClusterRole", OP, None),
    ("Role", f"{OP}-token", "ns-a"),
    ("Role", f"{OP}-token", "ns-b"),
    ("Role", f"{OP}-leader-election", "default"),
], "cluster-wide")
cr = one(cw, "ClusterRole", OP)
expect_rules(cr["rules"], CLUSTER_READS + CONVERGE + [WORKLOAD], "cluster-wide ClusterRole")
# TokenRequest is per-namespace even under cluster-wide watch.
assert_token_role(cw, "ns-a", ["sa-a", "sa-shared"])
assert_token_role(cw, "ns-b", ["sa-b", "sa-shared"])
# No stamp-root Role in cluster-wide mode (the ClusterRole covers Secrets).
if by(cw, "Role", f"{OP}-stamp-root"):
    fail("cluster-wide: unexpected stamp-root Role (ClusterRole already covers Secrets)")
assert_leader_election(cw, "cluster-wide")
assert_hardened(cw, "cluster-wide")
assert_server_network(cw, "cluster-wide", False)

# ---- namespaced ----
# The ClusterRole is cluster-scoped reads ONLY; each per-namespace Role carries the
# converge rules + workload (no cluster reads, no token rule).
ns = load(namespaced)
assert_rbac_inventory(ns, [
    ("ClusterRole", OP, None),
    ("Role", OP, "ns-a"),
    ("Role", OP, "ns-b"),
    ("Role", f"{OP}-token", "ns-a"),
    ("Role", f"{OP}-token", "ns-b"),
    ("Role", f"{OP}-stamp-root", "default"),
    ("Role", f"{OP}-leader-election", "default"),
], "namespaced")
cr = one(ns, "ClusterRole", OP)
expect_rules(cr["rules"], CLUSTER_READS, "namespaced ClusterRole")
for n in ("ns-a", "ns-b"):
    role = one(ns, "Role", OP, n)
    expect_rules(role["rules"], CONVERGE + [WORKLOAD], f"namespaced Role {n}")
assert_token_role(ns, "ns-a", ["sa-a", "sa-shared"])
assert_token_role(ns, "ns-b", ["sa-b"])
# Stamp-root Role lives in the release namespace: name-restricted get/update on the
# fixed stamp-root Secret + an unrestricted create, and NOTHING else.
sr = one(ns, "Role", f"{OP}-stamp-root", "default")
expect_rules(sr["rules"], [
    rule([""], ["secrets"], ["get", "update"], ["hikyo-operator-stamp-root"]),
    rule([""], ["secrets"], ["create"]),
], "stamp-root Role")
assert_leader_election(ns, "namespaced")
op = assert_hardened(ns, "namespaced")
env = {e["name"]: e.get("value") for e in op.get("env", [])}
if env.get("HIKYO_OPERATOR_NAMESPACES") != "ns-a,ns-b":
    fail(f"operator watch env = {env.get('HIKYO_OPERATOR_NAMESPACES')}, want ns-a,ns-b")

# ---- no-rollouts ----
# triggerRollouts=false drops the workload rule ENTIRELY and nothing else changes.
nr = load(no_rollouts)
assert_rbac_inventory(nr, [
    ("ClusterRole", OP, None),
    ("Role", f"{OP}-leader-election", "default"),
], "no-rollouts")
cr = one(nr, "ClusterRole", OP)
expect_rules(cr["rules"], CLUSTER_READS + CONVERGE, "no-rollouts ClusterRole")
assert_leader_election(nr, "no-rollouts")
assert_hardened(nr, "no-rollouts")
assert_server_network(nr, "no-rollouts", False)

tls_docs = load(native_tls)
assert_hardened(tls_docs, "native-tls")
assert_server_network(tls_docs, "native-tls", True)

print("Chart check: every RBAC rule set, TokenRequest scope, stamp-root grant, hardening, args and the exact env allowlist asserted")
PY

if grep -Eh '[0-9a-f]{64}' "$tmp"/*.yaml | grep -Ev 'sha256:[0-9a-f]{64}' >/dev/null; then
	fail 'rendered chart contains a raw 64-hex value outside an image digest'
fi

# Refusal fixtures: the server listener is invalid without a database Secret and
# without either a native TLS Secret or explicit trusted proxy CIDRs.
required_server_values='--set database.existingSecret=fixture --set rootKey.existingSecret=fixture-root-key --set externalOrigin=https://hikyo.example.com'

# shellcheck disable=SC2086 # Deliberately expand fixed test flags into argv.
if helm template fixture "$chart" $required_server_values >/dev/null 2>&1; then
	fail 'chart accepted a server listener without trusted proxy CIDRs'
fi
# shellcheck disable=SC2086 # Deliberately expand fixed test flags into argv.
if helm template fixture "$chart" $required_server_values \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
	--set rootKey.existingSecret= >/dev/null 2>&1; then
	fail 'chart accepted a server without rootKey.existingSecret'
fi
if helm template fixture "$chart" \
	--set rootKey.existingSecret=fixture-root-key \
	--set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' >/dev/null 2>&1; then
	fail 'chart accepted a server without database.existingSecret'
fi
if helm template fixture "$chart" \
	--set database.existingSecret=fixture \
	--set rootKey.existingSecret=fixture-root-key \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' >/dev/null 2>&1; then
	fail 'chart accepted a server without externalOrigin'
fi
if helm template fixture "$chart" \
	--set rootKey.existingSecret=fixture-root-key \
	--set externalOrigin=https://hikyo.example.com \
	--set tls.existingSecret=fixture-tls >/dev/null 2>&1; then
	fail 'chart accepted native TLS without database.existingSecret'
fi
if helm template fixture "$chart" \
	--set database.existingSecret=fixture \
	--set rootKey.existingSecret=fixture-root-key \
	--set externalOrigin=http://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' >/dev/null 2>&1; then
	fail 'chart accepted plaintext externalOrigin without network.allowPlaintextOrigin'
fi
for invalid_origin in \
	'https://hikyo.example.com/path' \
	'https://:' \
	'https://example.com\evil' \
	'https://EXAMPLE.com' \
	'https://example.com:443' \
	'https://example.com:99999'; do
	origin_json=$(jq -Rn --arg origin "$invalid_origin" '$origin')
	if helm template fixture "$chart" \
		--set database.existingSecret=fixture \
		--set rootKey.existingSecret=fixture-root-key \
		--set-json externalOrigin="$origin_json" \
		--set 'network.trustedProxyCIDRs={10.42.0.0/16}' >/dev/null 2>&1; then
		fail "chart accepted noncanonical externalOrigin $invalid_origin"
	fi
done
# A designated ServiceAccount for a namespace outside the watch set is refused.
if helm template fixture "$chart" \
	--set database.existingSecret=fixture \
	--set rootKey.existingSecret=fixture-root-key \
	--set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
	--set 'operator.namespaces={ns-a}' \
	--set 'operator.designatedServiceAccounts.ns-b={sa-b}' >/dev/null 2>&1; then
	fail 'chart accepted a TokenRequest grant for an unwatched namespace'
fi

# Multi-node HA mode (#146): the replica count, HA env (node id from the pod
# name), PodDisruptionBudget, graceful shutdown, and topology spread all appear.
ha_render=$(helm template fixture "$chart" \
	--set database.existingSecret=fixture \
	--set rootKey.existingSecret=fixture-root-key \
	--set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
	--set ha.enabled=true 2>/dev/null)
for want in \
	'replicas: 3' \
	'name: HIKYO_HA' \
	'fieldPath: metadata.name' \
	'kind: PodDisruptionBudget' \
	'minAvailable: 2' \
	'terminationGracePeriodSeconds: 30' \
	'topologySpreadConstraints'; do
	printf '%s\n' "$ha_render" | grep -qF "$want" || fail "HA render missing: $want"
done
# The single-node render carries none of the HA machinery.
default_render=$(helm template fixture "$chart" \
	--set database.existingSecret=fixture \
	--set rootKey.existingSecret=fixture-root-key \
	--set externalOrigin=https://hikyo.example.com \
	--set 'network.trustedProxyCIDRs={10.42.0.0/16}' 2>/dev/null)
for unwanted in 'name: HIKYO_HA' 'kind: PodDisruptionBudget'; do
	if printf '%s\n' "$default_render" | grep -qF "$unwanted"; then
		fail "single-node render leaked HA machinery: $unwanted"
	fi
done
# HA is refused when minAvailable exceeds the replica count (the PDB would then
# block every voluntary disruption), and when the replica count is below two.
for bad in '--set ha.replicaCount=2 --set ha.minAvailable=3' '--set ha.replicaCount=1'; do
	if helm template fixture "$chart" \
		--set database.existingSecret=fixture \
		--set rootKey.existingSecret=fixture-root-key \
		--set externalOrigin=https://hikyo.example.com \
		--set 'network.trustedProxyCIDRs={10.42.0.0/16}' \
		--set ha.enabled=true $bad >/dev/null 2>&1; then
		fail "chart accepted an invalid HA configuration: $bad"
	fi
done

printf 'Chart check: cluster-wide, namespaced, no-rollout, hardening, HA, and refusal assertions passed\n'

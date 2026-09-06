#!/usr/bin/env python3
"""Render fixed rollout custody; optionally prove admission in disposable kind.

Run with --live-context kind-NAME only for a dedicated disposable test cluster.
The live fixture owns a unique namespace and removes its resources on exit.
"""
import argparse
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

import yaml


def run(*args, allowed=True, data=None):
    result = subprocess.run(args, input=data, text=True, capture_output=True)
    if (result.returncode == 0) != allowed:
        raise RuntimeError(f"{args[0]}: unexpected result\n{result.stderr}")
    return result.stdout


parser = argparse.ArgumentParser()
parser.add_argument("--live-context")
parser.add_argument("--chart", type=Path)
options = parser.parse_args()
if options.live_context and not options.live_context.startswith("kind-"):
    parser.error("live fixture requires an explicitly named disposable kind context")
root = Path(__file__).resolve().parents[2]
chart = options.chart or root / "chart/hikyo"
release = "rollout-fixture-" + str(os.getpid())
namespace = release
name = release + "-hikyo"
executor = name + "-rollout"
values = {
    "database": {"existingSecret": "fixture-db"},
    "rootKey": {"existingSecret": "fixture-root"},
    "upgrade": {"existingClaim": "fixture-public", "stateExistingClaim": "fixture-custody"},
    "externalOrigin": "https://hikyo.example.com",
    "network": {"trustedProxyCIDRs": ["10.42.0.0/16"]},
    "operator": {"enabled": False},
    "rollout": {
        "enabled": True, "enrolled": True, "enrollmentID": "fixture-enrollment",
        "ownerInstanceID": "fixture-owner", "incarnation": "fixture-incarnation",
        "deploymentUID": "fixture-deployment", "commandSecretUID": "fixture-command",
        "responseSecretUID": "fixture-response", "journalSecretUID": "fixture-journal",
        "leaseUID": "fixture-lease", "authorityPublicKey": "public-key-fixture",
        "authorityExistingSecret": "fixture-authority",
        "initialDatabaseSource": "current", "initialRootSource": "current",
        "databaseSources": {"current": {"name": "fixture-db", "key": "HIKYO_DB"}, "next": {"name": "fixture-db-next", "key": "HIKYO_DB"}},
        "rootSources": {"current": {"name": "fixture-root", "key": "root-key"}, "next": {"name": "fixture-root-next", "key": "root-key"}},
    },
}

with tempfile.TemporaryDirectory(prefix="hikyo-rollout-chart-") as temporary:
    directory = Path(temporary)
    settings = directory / "values.yaml"

    def render(v, release_namespace=namespace):
        settings.write_text(yaml.safe_dump(v))
        raw = run("helm", "template", release, str(chart), "--namespace", release_namespace,
                  "--kube-version", "1.36.1", "-f", str(settings))
        return [item for item in yaml.safe_load_all(raw) if item]

    docs = render(values)

    def one(kind, wanted=None):
        result = [d for d in docs if d["kind"] == kind and (wanted is None or d["metadata"]["name"] == wanted)]
        assert len(result) == 1, (kind, wanted)
        return result[0]

    enrollment = json.loads(one("ConfigMap", executor + "-enrollment")["data"]["enrollment.json"])
    assert enrollment["executor_pod"] == executor + "-0"
    assert enrollment["target"]["stable_node_id"] == name + "-server"
    stateful = one("StatefulSet")["spec"]
    assert stateful["replicas"] == 1 and stateful["podManagementPolicy"] == "OrderedReady"
    assert stateful["updateStrategy"]["type"] == "OnDelete"
    assert stateful["template"]["spec"]["automountServiceAccountToken"] is False
    assert {v["name"] for v in stateful["template"]["spec"]["volumes"]} == {"enrollment", "api-token"}
    for role in [one("Role", executor), one("Role", executor + "-server")]:
        for rule in role["rules"]:
            assert rule.get("resourceNames"), "unnamed deployment authority"
            assert set(rule["verbs"]) <= {"get", "update"}, "broadened deployment verbs"
            assert "*" not in rule["resources"]
    assert one("Lease")["spec"] == {}, "custody must not expire"
    assert len([d for d in docs if d["kind"] == "ValidatingAdmissionPolicy"]) == 2
    policy_names = {d["metadata"]["name"] for d in docs if d["kind"] == "ValidatingAdmissionPolicy"}
    other_namespace = namespace + "-independent"
    other_docs = render(values, other_namespace)
    other_names = {d["metadata"]["name"] for d in other_docs if d["kind"] == "ValidatingAdmissionPolicy"}
    assert len(other_names) == 2 and policy_names.isdisjoint(other_names), "cluster policy collision across namespaces"
    for rendered, expected_namespace, expected_policies in [(docs, namespace, policy_names), (other_docs, other_namespace, other_names)]:
        bindings = [d for d in rendered if d["kind"] == "ValidatingAdmissionPolicyBinding"]
        assert len(bindings) == 2
        assert {d["spec"]["policyName"] for d in bindings} == expected_policies
        assert all(d["spec"]["paramRef"]["namespace"] == expected_namespace for d in bindings)
    print("rollout chart: identical release names have separate cluster policies and exact namespace bindings")
    server = one("Deployment")["spec"]["template"]["spec"]["containers"][0]
    assert next(e for e in server["env"] if e["name"] == "HIKYO_NODE_ID")["value"] == name + "-server"
    assert "--config-rollout-enrollment=/run/hikyo/rollout/enrollment/enrollment.json" in server["args"]
    assert "--config-rollout-signing-key=/run/hikyo/rollout/authority/authority.key" in server["args"]
    for change in [{"replicaCount": 2}, {"ha": {"enabled": True, "replicaCount": 2}}]:
        changed = copy.deepcopy(values)
        changed.update(change)
        settings.write_text(yaml.safe_dump(changed))
        run("helm", "template", release, str(chart), "--kube-version", "1.36.1", "-f", str(settings), allowed=False)
    print("rollout chart: fixed names, isolated authority, private projections and stable singleton identity pass")

    if not options.live_context:
        raise SystemExit(0)

    kubectl = ["kubectl", "--context", options.live_context]

    def kube(*args, **kwargs):
        return run(*kubectl, *args, **kwargs)

    kube("get", "namespace", namespace, allowed=False)
    kube("create", "namespace", namespace)
    policies = []
    try:
        base = [d for d in docs if d["kind"] in {"Secret", "Lease", "Role", "RoleBinding", "ServiceAccount", "Deployment"}]
        kube("apply", "-n", namespace, "-f", "-", data=yaml.safe_dump_all(base))
        deployment = json.loads(kube("get", "deployment", name, "-n", namespace, "-o", "json"))
        values["rollout"]["deploymentUID"] = deployment["metadata"]["uid"]
        docs = render(values)
        policies = [d for d in docs if d["kind"].startswith("ValidatingAdmissionPolicy")]
        kube("apply", "-f", "-", data=yaml.safe_dump_all(policies))
        for _ in range(50):
            checked = [json.loads(kube("get", "validatingadmissionpolicy", policy["metadata"]["name"], "-o", "json")) for policy in policies if policy["kind"] == "ValidatingAdmissionPolicy"]
            if all(p.get("status", {}).get("observedGeneration") == p["metadata"]["generation"] for p in checked):
                break
            time.sleep(0.1)
        assert all(p.get("status", {}).get("observedGeneration") == p["metadata"]["generation"] for p in checked)
        assert not any(p.get("status", {}).get("typeChecking", {}).get("expressionWarnings") for p in checked)
        holder = executor + "-0:fixture-pod"
        kube("patch", "lease", executor, "-n", namespace, "--type", "merge", "-p", json.dumps({"spec": {"holderIdentity": holder, "leaseTransitions": 1}}))
        deployment = json.loads(kube("get", "deployment", name, "-n", namespace, "-o", "json"))
        deployment["metadata"].setdefault("annotations", {})["hikyo.dev/rollout-custody"] = holder + ":1"
        subject = "system:serviceaccount:" + namespace + ":" + executor

        # observedGeneration confirms typechecking, not admission cache
        # activation. Match the executor's forbidden dry-run startup probe.
        probe = copy.deepcopy(deployment)
        probe["spec"]["template"]["spec"]["containers"][0]["image"] = "invalid.example/hikyo-admission-probe:v1"
        for _ in range(50):
            result = subprocess.run([*kubectl, "--as", subject, "replace", "--raw", f"/apis/apps/v1/namespaces/{namespace}/deployments/{name}?dryRun=All", "-f", "-"], input=json.dumps(probe), text=True, capture_output=True)
            if result.returncode != 0 and "Container authority outside configuration fields is immutable." in result.stderr:
                break
            time.sleep(0.1)
        assert result.returncode != 0 and "Container authority outside configuration fields is immutable." in result.stderr

        def server_of(obj):
            return obj["spec"]["template"]["spec"]["containers"][0]

        def env(obj, key):
            return next(e for e in server_of(obj)["env"] if e["name"] == key)

        def root_volume(obj):
            return next(v for v in obj["spec"]["template"]["spec"]["volumes"] if v["name"] == "root-key-source")

        cases = [
            ("unchanged", lambda o: None, True),
            ("listener", lambda o: server_of(o)["args"].__setitem__(1, "--listen=0.0.0.0:9080"), True),
            ("database-alias", lambda o: env(o, "HIKYO_DB")["valueFrom"]["secretKeyRef"].__setitem__("name", "fixture-db-next"), True),
            ("root-alias", lambda o: root_volume(o)["secret"].__setitem__("secretName", "fixture-root-next"), True),
            ("image", lambda o: server_of(o).__setitem__("image", "busybox:1.36"), False),
            ("service-account", lambda o: o["spec"]["template"]["spec"].__setitem__("serviceAccountName", "default"), False),
            ("root-bytes", lambda o: server_of(o)["env"].append({"name": "HIKYO_ROOT_KEY", "value": "fixture-canary"}), False),
            ("root-argument", lambda o: server_of(o)["args"].__setitem__(3, "--root-key-file=/tmp/foreign"), False),
            ("undeclared-root", lambda o: root_volume(o)["secret"].__setitem__("secretName", "foreign-root"), False),
            ("stale-custody", lambda o: o["metadata"]["annotations"].__setitem__("hikyo.dev/rollout-custody", "old-pod:0"), False),
            ("node-identity", lambda o: env(o, "HIKYO_NODE_ID").__setitem__("value", "other-node"), False),
        ]
        for label, mutate, allowed in cases:
            candidate = copy.deepcopy(deployment)
            mutate(candidate)
            # Raw PUT avoids kubectl replace rewriting last-applied annotation.
            kube("--as", subject, "replace", "--raw", f"/apis/apps/v1/namespaces/{namespace}/deployments/{name}?dryRun=All", "-f", "-", data=json.dumps(candidate), allowed=allowed)
            print("rollout admission: " + label + (" allowed" if allowed else " denied"))
        for actor in [executor, executor + "-server"]:
            for verb, resource in [("get", "secret/fixture-root"), ("list", "secrets"), ("create", "secrets")]:
                kube("--as", "system:serviceaccount:" + namespace + ":" + actor, "auth", "can-i", verb, resource, "-n", namespace, allowed=False)
        kube("--as", subject + "-server", "auth", "can-i", "update", "deployment/" + name, "-n", namespace, allowed=False)
        print("rollout RBAC: both actors denied root reads, secret listing/creation; server denied Deployment mutation")
    finally:
        if policies:
            kube("delete", "--ignore-not-found", "-f", "-", data=yaml.safe_dump_all(policies))
        kube("delete", "namespace", namespace, "--wait=false")

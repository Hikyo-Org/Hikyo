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


def run(*args, allowed=True, data=None, refusal=None):
    result = subprocess.run(args, input=data, text=True, capture_output=True)
    if (result.returncode == 0) != allowed:
        raise RuntimeError(f"{args[0]}: unexpected result\n{result.stderr}")
    if refusal is not None and refusal not in result.stderr:
        raise RuntimeError(f"{args[0]}: missing expected refusal {refusal!r}\n{result.stderr}")
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
UPGRADE_ENV_FIELDS = {
    "HIKYO_UPGRADE_BUNDLE": "bundle_directory",
    "HIKYO_UPGRADE_STATE_DIR": "state_directory",
    "HIKYO_UPGRADE_EVIDENCE": "evidence_directory",
    "HIKYO_UPGRADE_BACKUP": "ciphertext_path",
    "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY": "operator_public_key_file",
    "HIKYO_UPGRADE_TARGET_MANIFEST": "target_manifest_sha256",
    "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED": "legacy_writers_stopped",
}
UPGRADE_SOURCE_ANNOTATION = "hikyo.dev/configuration-upgrade-source"
UPGRADE_PROOF_ANNOTATION = "hikyo.dev/configuration-upgrade-proof"
initial_upgrade = {
    "bundle_directory": "/run/hikyo-upgrade/bundle",
    "state_directory": "/var/lib/hikyo-upgrade/operator-custody",
    "evidence_directory": "",
    "ciphertext_path": "",
    "operator_public_key_file": "/run/hikyo-upgrade/operator.pub",
    "target_manifest_sha256": "",
    "legacy_writers_stopped": False,
}
next_upgrade = {
    "bundle_directory": "/run/hikyo-upgrade/next/bundle",
    "state_directory": "/var/lib/hikyo-upgrade/aliases/next",
    "evidence_directory": "/run/hikyo-upgrade/next/evidence",
    "ciphertext_path": "/run/hikyo-upgrade/next/backup.age",
    "operator_public_key_file": "/run/hikyo-upgrade/next/operator.pub",
    "target_manifest_sha256": "1" * 64,
    "legacy_writers_stopped": True,
}


def upgrade_environment(source):
    return {key: str(source[field]).lower() if isinstance(source[field], bool) else source[field]
            for key, field in UPGRADE_ENV_FIELDS.items()}


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
        "initialUpgradeSource": "current",
        "upgradeSources": {"current": initial_upgrade, "next": next_upgrade},
        "upgradeStateAliases": ["next"],
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
    assert enrollment["target"]["upgrade_sources"] == values["rollout"]["upgradeSources"]
    assert enrollment["target"]["initial_upgrade_source"] == "current"
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

    template = one("Deployment")["spec"]["template"]
    assert template["metadata"]["annotations"][UPGRADE_SOURCE_ANNOTATION] == "current"
    assert template["metadata"]["annotations"][UPGRADE_PROOF_ANNOTATION] == ""
    actual_upgrade = {e["name"]: e["value"] for e in server["env"] if e["name"] in UPGRADE_ENV_FIELDS}
    assert actual_upgrade == upgrade_environment(initial_upgrade)
    volumes = template["spec"]["volumes"]
    selection = next(v for v in volumes if v["name"] == "rollout-selection")
    fields = {v["path"]: v["fieldRef"]["fieldPath"] for v in selection["downwardAPI"]["items"]}
    assert fields["upgrade-alias"] == "metadata.annotations['" + UPGRADE_SOURCE_ANNOTATION + "']"
    assert fields["upgrade-proof"] == "metadata.annotations['" + UPGRADE_PROOF_ANNOTATION + "']"
    mounts = {m["name"]: m for m in server["volumeMounts"]}
    assert mounts["upgrade-public"]["mountPath"] == "/run/hikyo-upgrade" and mounts["upgrade-public"]["readOnly"] is True
    state_mounts = [m for m in server["volumeMounts"] if m["name"] == "upgrade-state"]
    assert state_mounts == [
        {"name": "upgrade-state", "mountPath": "/var/lib/hikyo-upgrade"},
        {"name": "upgrade-state", "mountPath": "/var/lib/hikyo-upgrade/aliases/next", "subPath": "operator-custody"},
    ]

    populated = copy.deepcopy(values)
    populated["upgrade"].update({"evidence": True, "targetManifestSHA256": "3" * 64, "legacyWritersStopped": True})
    populated["rollout"]["upgradeSources"]["current"].update({
        "evidence_directory": "/run/hikyo-upgrade/evidence",
        "ciphertext_path": "/run/hikyo-upgrade/backup.age",
        "target_manifest_sha256": "3" * 64,
        "legacy_writers_stopped": True,
    })
    populated_docs = render(populated)
    populated_server = next(d for d in populated_docs if d["kind"] == "Deployment")["spec"]["template"]["spec"]["containers"][0]
    assert {e["name"]: e["value"] for e in populated_server["env"] if e["name"] in UPGRADE_ENV_FIELDS} == upgrade_environment(populated["rollout"]["upgradeSources"]["current"])

    # Optional enrollment must preserve the existing bootstrap-only chart.
    no_sources = copy.deepcopy(values)
    no_sources["rollout"]["upgradeSources"] = {}
    no_sources["rollout"]["upgradeStateAliases"] = []
    no_sources["rollout"]["initialUpgradeSource"] = ""
    without_sources = render(no_sources)
    old_template = next(d for d in without_sources if d["kind"] == "Deployment")["spec"]["template"]
    assert old_template["spec"]["volumes"] == volumes, "source descriptors added mount authority"
    assert UPGRADE_SOURCE_ANNOTATION not in old_template["metadata"]["annotations"]
    assert {e["name"] for e in old_template["spec"]["containers"][0]["env"] if e["name"] in UPGRADE_ENV_FIELDS} == {
        "HIKYO_UPGRADE_BUNDLE", "HIKYO_UPGRADE_STATE_DIR", "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY"}
    no_sources["rollout"] = {"enabled": False}
    legacy = render(no_sources)
    assert not any(d["kind"].startswith("ValidatingAdmissionPolicy") for d in legacy)

    # Every incomplete/ambiguous descriptor and initial-selection mismatch fails
    # at chart input admission, before Kubernetes objects can be installed.
    invalid = []
    for field in UPGRADE_ENV_FIELDS.values():
        bad = copy.deepcopy(values)
        del bad["rollout"]["upgradeSources"]["next"][field]
        invalid.append(("missing-" + field, bad))
    for field, wrong in [
        ("bundle_directory", "/tmp/foreign"),
        ("bundle_directory", "/run/hikyo-upgrade"),
        ("bundle_directory", "/run/hikyo-upgrade/../foreign"),
        ("bundle_directory", "/run/hikyo-upgrade/a/./bundle"),
        ("bundle_directory", "/run/hikyo-upgrade/a//bundle"),
        ("bundle_directory", "/run/hikyo-upgrade/a/"),
        ("bundle_directory", "/run/hikyo-upgrade/a\x00bundle"),
        ("state_directory", "/run/hikyo-upgrade/state"),
        ("state_directory", "/var/lib/hikyo-upgrade"),
        ("operator_public_key_file", "relative.pub"),
        ("evidence_directory", ""),
        ("ciphertext_path", ""),
        ("target_manifest_sha256", "A" * 64),
        ("target_manifest_sha256", "abc"),
        ("legacy_writers_stopped", "false"),
        ("undeclared_field", "value"),
    ]:
        bad = copy.deepcopy(values)
        bad["rollout"]["upgradeSources"]["next"][field] = wrong
        invalid.append(("invalid-" + field, bad))
    for alias in ["Bad", "../foreign", "a" * 32]:
        bad = copy.deepcopy(values)
        bad["rollout"]["upgradeSources"][alias] = copy.deepcopy(next_upgrade)
        invalid.append(("invalid-alias", bad))
    bad = copy.deepcopy(values)
    bad["rollout"]["upgradeSources"] = {}
    invalid.append(("selected-empty-map", bad))
    bad = copy.deepcopy(values)
    bad["rollout"]["upgradeSources"]["current"]["legacy_writers_stopped"] = True
    invalid.append(("legacy-assertion-without-evidence", bad))
    for aliases in [[], ["missing"], ["next", "next"], ["../next"]]:
        bad = copy.deepcopy(values)
        bad["rollout"]["upgradeStateAliases"] = aliases
        invalid.append(("invalid-state-alias-mount", bad))
    bad = copy.deepcopy(values)
    bad["rollout"]["upgradeSources"]["next"]["state_directory"] = "/var/lib/hikyo-upgrade/aliases/other"
    invalid.append(("mismatched-state-alias-mount", bad))
    for alias in ["", "missing", "next"]:
        bad = copy.deepcopy(values)
        bad["rollout"]["initialUpgradeSource"] = alias
        invalid.append(("invalid-initial-selection", bad))
    for field in UPGRADE_ENV_FIELDS.values():
        bad = copy.deepcopy(values)
        replacement = next_upgrade[field]
        if field == "state_directory":
            replacement = "/var/lib/hikyo-upgrade/different-custody"
        bad["rollout"]["upgradeSources"]["current"][field] = replacement
        invalid.append(("initial-tuple-mismatch-" + field, bad))
    for label, bad in invalid:
        settings.write_text(yaml.safe_dump(bad))
        run("helm", "template", release, str(chart), "--kube-version", "1.36.1", "-f", str(settings), allowed=False)
    print("rollout chart: complete upgrade tuples, exact initial selection, closed paths and legacy startup pass")

    if not options.live_context:
        raise SystemExit(0)

    kubectl = ["kubectl", "--context", options.live_context]

    def kube(*args, **kwargs):
        return run(*kubectl, *args, **kwargs)

    kube("get", "namespace", namespace, allowed=False)
    kube("create", "namespace", namespace)
    policies = []
    try:
        # Prove the rendered old/new paths are bind mounts of one persistent
        # custody object. This pinned existing fixture image supplies only sh,
        # stat and file operations; it does not start PostgreSQL.
        helper_image = "postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941"
        custody_claim = {"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": {"name": values["upgrade"]["stateExistingClaim"]}, "spec": {"accessModes": ["ReadWriteOnce"], "resources": {"requests": {"storage": "16Mi"}}}}
        same_object_script = r"""
set -eu
old="$1"
new="$2"
test "$old" != "$new"
for path in /var /var/lib /var/lib/hikyo-upgrade /var/lib/hikyo-upgrade/aliases "$old" "$new"; do
  test ! -L "$path"
done
old_id=$(stat -c '%d:%i' "$old")
new_id=$(stat -c '%d:%i' "$new")
test "$old_id" = "$new_id"
printf '%s' custody-written-through-primary > "$old/probe"
test "$(cat "$new/probe")" = custody-written-through-primary
printf '%s' custody-written-through-alias > "$new/probe"
test "$(cat "$old/probe")" = custody-written-through-alias
test "$(stat -c '%d:%i' "$old/probe")" = "$(stat -c '%d:%i' "$new/probe")"
printf 'same persistent custody object: %s = %s; device:inode %s; bidirectional writes passed\n' "$old" "$new" "$old_id"
"""
        state_volume = next(v for v in volumes if v["name"] == "upgrade-state")
        mount_proof = {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": "upgrade-state-mount-proof"}, "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 90, "template": {"spec": {
            "restartPolicy": "Never", "automountServiceAccountToken": False,
            "securityContext": {"fsGroup": 65532, "fsGroupChangePolicy": "OnRootMismatch"},
            "initContainers": [{"name": "private-custody", "image": helper_image, "command": ["sh", "-ec", "mkdir -p /fixture/operator-custody /fixture/aliases; chown 65532:65532 /fixture /fixture/operator-custody /fixture/aliases; chmod 2770 /fixture; chmod 0700 /fixture/operator-custody /fixture/aliases"], "volumeMounts": [{"name": "upgrade-state", "mountPath": "/fixture"}]}],
            "containers": [{"name": "same-object", "image": helper_image, "command": ["sh", "-ec", same_object_script, "mount-proof", initial_upgrade["state_directory"], next_upgrade["state_directory"]], "securityContext": {"runAsUser": 65532, "runAsGroup": 65532, "allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}}, "volumeMounts": state_mounts}],
            "volumes": [state_volume],
        }}}}
        kube("apply", "-n", namespace, "-f", "-", data=yaml.safe_dump_all([custody_claim, mount_proof]))
        # Bounded polls leave room for progress reporting by the parent runner.
        for attempt in range(12):
            observed = json.loads(kube("get", "job", "upgrade-state-mount-proof", "-n", namespace, "-o", "json"))
            if observed.get("status", {}).get("succeeded") == 1:
                break
            if observed.get("status", {}).get("failed", 0):
                raise RuntimeError("actual same-object custody mount fixture failed")
            time.sleep(5)
        assert observed.get("status", {}).get("succeeded") == 1, "same-object custody mount fixture did not finish"
        print(kube("logs", "job/upgrade-state-mount-proof", "-c", "same-object", "-n", namespace).strip())

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
        def select_upgrade(obj, alias):
            source = values["rollout"]["upgradeSources"][alias]
            server_of(obj)["env"] = [e for e in server_of(obj)["env"] if e["name"] not in UPGRADE_ENV_FIELDS]
            server_of(obj)["env"].extend({"name": key, "value": value} for key, value in upgrade_environment(source).items())
            annotations = obj["spec"]["template"]["metadata"].setdefault("annotations", {})
            annotations[UPGRADE_SOURCE_ANNOTATION] = alias
            annotations[UPGRADE_PROOF_ANNOTATION] = "2" * 64 if alias == "next" else ""

        upgrade_refusal = "Upgrade settings must exactly match one enrolled source alias and its complete literal tuple."
        upgrade_cases = [
            ("selected-tuple", lambda o: select_upgrade(o, "next"), True, None),
            ("noninitial-empty-proof", lambda o: (select_upgrade(o, "next"), o["spec"]["template"]["metadata"]["annotations"].__setitem__(UPGRADE_PROOF_ANNOTATION, "")), False, "Upgrade proof must be an exact lowercase SHA-256; only the enrolled initial source may have an empty proof."),
            ("noninitial-missing-proof", lambda o: (select_upgrade(o, "next"), o["spec"]["template"]["metadata"]["annotations"].pop(UPGRADE_PROOF_ANNOTATION)), False, "Upgrade proof must be an exact lowercase SHA-256; only the enrolled initial source may have an empty proof."),
            ("state-subpath-authority", lambda o: next(m for m in server_of(o)["volumeMounts"] if m.get("subPath") == "operator-custody").__setitem__("subPath", "foreign"), False, "Container authority outside configuration fields is immutable."),
            ("state-mount-authority", lambda o: next(m for m in server_of(o)["volumeMounts"] if m.get("subPath") == "operator-custody").__setitem__("mountPath", "/var/lib/hikyo-upgrade/aliases/foreign"), False, "Container authority outside configuration fields is immutable."),
            ("initial-restore-tuple", lambda o: select_upgrade(o, "current"), True, None),
            ("annotation-only", lambda o: o["spec"]["template"]["metadata"]["annotations"].__setitem__(UPGRADE_SOURCE_ANNOTATION, "next"), False, upgrade_refusal),
            ("undeclared-alias", lambda o: o["spec"]["template"]["metadata"]["annotations"].__setitem__(UPGRADE_SOURCE_ANNOTATION, "missing"), False, upgrade_refusal),
            ("remove-alias", lambda o: o["spec"]["template"]["metadata"]["annotations"].pop(UPGRADE_SOURCE_ANNOTATION), False, upgrade_refusal),
            ("partial-tuple", lambda o: env(o, "HIKYO_UPGRADE_BUNDLE").__setitem__("value", next_upgrade["bundle_directory"]), False, upgrade_refusal),
            ("raw-path", lambda o: env(o, "HIKYO_UPGRADE_STATE_DIR").__setitem__("value", "/var/lib/hikyo-upgrade/foreign"), False, upgrade_refusal),
            ("remove-empty-input", lambda o: server_of(o)["env"].remove(env(o, "HIKYO_UPGRADE_EVIDENCE")), False, upgrade_refusal),
            ("legacy-assertion-only", lambda o: env(o, "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED").__setitem__("value", "true"), False, upgrade_refusal),
            ("secret-indirection", lambda o: env(o, "HIKYO_UPGRADE_EVIDENCE").update({"valueFrom": {"secretKeyRef": {"name": executor + "-config", "key": "HIKYO_UPGRADE_EVIDENCE"}}}), False, upgrade_refusal),
            ("invalid-proof", lambda o: o["spec"]["template"]["metadata"]["annotations"].__setitem__(UPGRADE_PROOF_ANNOTATION, "not-a-digest"), False, "Upgrade proof must be an exact lowercase SHA-256; only the enrolled initial source may have an empty proof."),
            ("volume-authority", lambda o: next(v for v in o["spec"]["template"]["spec"]["volumes"] if v["name"] == "upgrade-public")["persistentVolumeClaim"].__setitem__("claimName", "foreign"), False, "Only the installed root-key-source Secret name/key may change."),
        ]
        for label, mutate, allowed, refusal in upgrade_cases:
            candidate = copy.deepcopy(deployment)
            # Keep each tuple-negative test independent of the proof guard.
            # Proof-specific cases replace/remove this digest explicitly.
            candidate["spec"]["template"]["metadata"]["annotations"][UPGRADE_PROOF_ANNOTATION] = "2" * 64
            mutate(candidate)
            kube("--as", subject, "replace", "--raw", f"/apis/apps/v1/namespaces/{namespace}/deployments/{name}?dryRun=All", "-f", "-", data=json.dumps(candidate), allowed=allowed, refusal=refusal)
            print("rollout admission: upgrade-" + label + (" allowed" if allowed else " denied"))

        for label, mutate, allowed in cases:
            candidate = copy.deepcopy(deployment)
            mutate(candidate)
            # Raw PUT avoids kubectl replace rewriting last-applied annotation.
            kube("--as", subject, "replace", "--raw", f"/apis/apps/v1/namespaces/{namespace}/deployments/{name}?dryRun=All", "-f", "-", data=json.dumps(candidate), allowed=allowed)
            print("rollout admission: " + label + (" allowed" if allowed else " denied"))
        # Exercise Restore admission with a different enrolled tuple as oldObject,
        # not merely a no-op carrying the initial tuple on the original object.
        def put_upgrade_selection(alias, dry_run=False):
            for attempt in range(5):
                candidate = json.loads(kube("get", "deployment", name, "-n", namespace, "-o", "json"))
                candidate["metadata"].setdefault("annotations", {})["hikyo.dev/rollout-custody"] = holder + ":1"
                select_upgrade(candidate, alias)
                suffix = "?dryRun=All" if dry_run else ""
                try:
                    kube("--as", subject, "replace", "--raw", f"/apis/apps/v1/namespaces/{namespace}/deployments/{name}{suffix}", "-f", "-", data=json.dumps(candidate))
                    return
                except RuntimeError as error:
                    # Kubernetes may update readiness/status between GET and PUT.
                    # Only an exact API resource-version conflict is retried;
                    # admission and every other failure remain immediate errors.
                    if attempt == 4 or "Error from server (Conflict)" not in str(error) or "the object has been modified" not in str(error):
                        raise
        put_upgrade_selection("next")
        put_upgrade_selection("current", dry_run=True)
        print("rollout admission: upgrade-next-to-initial Restore allowed")

        for actor in [executor, executor + "-server"]:
            for verb, resource in [("get", "secret/fixture-root"), ("list", "secrets"), ("create", "secrets")]:
                kube("--as", "system:serviceaccount:" + namespace + ":" + actor, "auth", "can-i", verb, resource, "-n", namespace, allowed=False)
        kube("--as", subject + "-server", "auth", "can-i", "update", "deployment/" + name, "-n", namespace, allowed=False)
        print("rollout RBAC: both actors denied root reads, secret listing/creation; server denied Deployment mutation")
    finally:
        if policies:
            kube("delete", "--ignore-not-found", "-f", "-", data=yaml.safe_dump_all(policies))
        kube("delete", "namespace", namespace, "--wait=false")

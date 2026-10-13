"""Check custody isolation and the restricted workload manifest contract."""
from __future__ import annotations

import importlib.util
import json
import io
import sys
from contextlib import redirect_stderr
import os
import stat
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("hosted_stack", HERE / "setup.py")
if spec is None or spec.loader is None:
    raise RuntimeError("stack_module_not_found")
stack = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stack)


class FreshCustodyTests(unittest.TestCase):
    def test_independent_runs_have_private_unshared_custody(self) -> None:
        old_mask = os.umask(0o077)
        try:
            with tempfile.TemporaryDirectory() as directory:
                states = [Path(directory) / name for name in ("first", "second")]
                for state in states:
                    state.mkdir(mode=0o700)
                    documents = stack.fresh_custody(state)
                    custody = next(document for document in documents
                                   if document["metadata"]["name"] == "hikyo-lab-custody")
                    self.assertEqual(set(custody["data"]), {"root-key", "tls.key", "tls.crt", "ca.crt"})
                    for path in state.iterdir():
                        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600, path.name)
                    stack.command(["openssl", "verify", "-CAfile", str(state / "ca.crt"),
                                   str(state / "tls.crt"), str(state / "openaev.crt")])
                    credentials = json.loads((state / "credentials.json").read_text())
                    self.assertEqual((state / "openaev-token").read_text().strip(),
                                     credentials["OPENAEV_ADMIN_TOKEN"])
                for name in ("root-key", "ca.key", "ca.crt", "tls.key", "operator.key", "openaev-token"):
                    self.assertNotEqual((states[0] / name).read_bytes(), (states[1] / name).read_bytes(), name)
        finally:
            os.umask(old_mask)


class SetupFailureTests(unittest.TestCase):
    def test_reachable_nonruntime_failures_emit_diagnostics_without_private_details(self) -> None:
        diagnostic_spec = importlib.util.spec_from_file_location("diagnostics", HERE / "diagnostics.py")
        if diagnostic_spec is None or diagnostic_spec.loader is None:
            raise RuntimeError("diagnostics_module_not_found")
        diagnostic_module = importlib.util.module_from_spec(diagnostic_spec)
        diagnostic_spec.loader.exec_module(diagnostic_module)
        for error in (KeyError("private-token"), IndexError("private-token"), StopIteration("private-token"),
                      json.JSONDecodeError("private-token", "private-token", 0), yaml.YAMLError("private-token")):
            stderr = io.StringIO()
            with patch.dict(sys.modules, {"diagnostics": diagnostic_module}), patch.object(stack, "main", side_effect=error), \
                 patch.object(stack, "STATE", Path("/absent-runner-state")), patch.object(stack, "STAGE", "import-injector"), redirect_stderr(stderr):
                self.assertEqual(stack.entrypoint(), 1)
            text = stderr.getvalue()
            self.assertNotIn("private-token", text)
            self.assertNotIn("Traceback", text)
            self.assertIn("stack_startup_diagnostics", text)
            self.assertIn("stack_setup_failed:import-injector:", text)

    def test_runtime_messages_and_stage_require_closed_codes(self) -> None:
        stages = frozenset({"import-injector"})
        self.assertEqual(stack.safe_setup_error(RuntimeError("private-token"), "private-token", stages),
                         "stack_setup_failed:unknown:RuntimeError")
        self.assertEqual(stack.safe_setup_error(RuntimeError("stack_command_failed:import-injector:kubectl"), "import-injector", stages),
                         "stack_command_failed:import-injector:kubectl")


class BackendStartupOrderTests(unittest.TestCase):
    def test_frontends_are_created_only_after_native_backend_readiness(self) -> None:
        documents = list(yaml.safe_load_all((HERE / "workloads.yaml").read_text()))
        ready = set()
        frontend_created = False

        def create(native):
            nonlocal frontend_created
            names = {document["metadata"]["name"] for document in native}
            if names & {"openaev", "http-injector"}:
                self.assertEqual(ready, {"rabbitmq", "pgsql", "elasticsearch", "silo"})
                frontend_created = True
            else:
                self.assertEqual(names, {"rabbitmq", "pgsql", "elasticsearch", "silo"})

        def wait(arguments, **kwargs):
            resource = arguments[3]
            if resource.startswith("statefulset/"):
                self.assertFalse(frontend_created)
                ready.add(resource.split("/")[1])
            else:
                self.assertTrue(frontend_created)
            return b"ready"

        with patch.object(stack, "apply", side_effect=create), patch.object(stack, "command", side_effect=wait):
            stack.start_workloads(documents)
        self.assertTrue(frontend_created)

    def test_failed_backend_never_launches_openaev(self) -> None:
        documents = list(yaml.safe_load_all((HERE / "workloads.yaml").read_text()))
        created = set()

        def create(native):
            created.update(document["metadata"]["name"] for document in native)

        def wait(arguments, **kwargs):
            if "statefulset/rabbitmq" in arguments:
                raise RuntimeError("backend_listener_not_ready")
            return b"ready"

        with patch.object(stack, "apply", side_effect=create), patch.object(stack, "command", side_effect=wait):
            with self.assertRaisesRegex(RuntimeError, "backend_listener_not_ready"):
                stack.start_workloads(documents)
        self.assertEqual(created, {"rabbitmq", "pgsql", "elasticsearch", "silo"})

    def test_rabbit_readiness_requires_application_and_tcp_listener_checks(self) -> None:
        rabbit = next(document for document in yaml.safe_load_all((HERE / "workloads.yaml").read_text())
                      if document["kind"] == "StatefulSet" and document["metadata"]["name"] == "rabbitmq")
        container = rabbit["spec"]["template"]["spec"]["containers"][0]
        command = container["startupProbe"]["exec"]["command"]
        self.assertEqual(command[:2], ["sh", "-ec"])
        self.assertIn("rabbitmq-diagnostics -q check_running", command[2])
        self.assertIn("rabbitmq-diagnostics -q check_port_connectivity", command[2])
        self.assertNotIn(" ping", command[2])
        self.assertEqual(container["readinessProbe"]["tcpSocket"], {"port": 5672})


class RemountReplacementTests(unittest.TestCase):
    def test_missing_replacement_is_created_before_readiness_with_shared_budget(self) -> None:
        created = False
        calls = []

        def native(arguments, **kwargs):
            nonlocal created
            calls.append((arguments, kwargs))
            if "--for=create" in arguments:
                # Reproduce the API gap: no Pod exists until creation wait ends.
                created = True
                return b"created"
            if not created:
                raise RuntimeError("pod_not_found")
            return b"ready"

        with patch.object(stack, "command", side_effect=native), patch.object(
            stack.time, "monotonic", side_effect=[100.0, 100.0, 151.0]
        ):
            stack.wait_for_rabbitmq_remount()
        self.assertIn("--for=create", calls[0][0])
        self.assertIn("--timeout=180s", calls[0][0])
        self.assertEqual(calls[0][1]["timeout"], 180)
        self.assertIn("--for=condition=Ready", calls[1][0])
        self.assertIn("--timeout=129s", calls[1][0])
        self.assertEqual(calls[1][1]["timeout"], 129)

    def test_creation_consuming_budget_does_not_start_a_fresh_readiness_budget(self) -> None:
        with patch.object(stack, "command") as native, patch.object(
            stack.time, "monotonic", side_effect=[100.0, 100.0, 280.1]
        ):
            with self.assertRaisesRegex(RuntimeError, "rabbitmq_remount_wait_expired"):
                stack.wait_for_rabbitmq_remount()
        self.assertEqual(native.call_count, 1)


class WorkloadBoundaryTests(unittest.TestCase):
    def test_pinned_images_and_restricted_pod_identity(self) -> None:
        for document in yaml.safe_load_all((HERE / "workloads.yaml").read_text()):
            if document["kind"] not in ("Deployment", "StatefulSet"):
                continue
            pod = document["spec"]["template"]["spec"]
            self.assertFalse(pod["automountServiceAccountToken"])
            self.assertTrue(pod["securityContext"]["runAsNonRoot"])
            self.assertNotIn("imagePullSecrets", pod)
            for container in pod.get("containers", []) + pod.get("initContainers", []):
                image = container["image"]
                if image != "INJECTOR_IMAGE":
                    self.assertRegex(image, r"@sha256:[a-f0-9]{64}$")
                self.assertNotIn("git.went.io", image)
                self.assertEqual(container["securityContext"]["capabilities"]["drop"], ["ALL"])
                self.assertTrue(container["securityContext"]["readOnlyRootFilesystem"])
                self.assertFalse(container["securityContext"]["allowPrivilegeEscalation"])

    def test_every_manifest_image_is_in_preloaded_inventory(self) -> None:
        inventory = json.loads((HERE / "images.json").read_text())
        def normalize(image):
            if image.startswith(("ghcr.io/", "docker.io/", "docker.elastic.co/")):
                return image
            return "docker.io/" + ("library/" if "/" not in image.split("@")[0] else "") + image
        expected = {normalize(image) for image in inventory.values()}
        def images(value):
            if isinstance(value, dict):
                for key, item in value.items():
                    if key == "image" and isinstance(item, str):
                        yield item
                    elif key == "helperPod.yaml" and isinstance(item, str):
                        yield from images(yaml.safe_load(item))
                    else:
                        yield from images(item)
            elif isinstance(value, list):
                for item in value:
                    yield from images(item)
        for manifest in ("workloads.yaml", "system.yaml"):
            for image in images(list(yaml.safe_load_all((HERE / manifest).read_text()))):
                if image != "INJECTOR_IMAGE":
                    self.assertIn(normalize(image), expected, manifest + ": " + image)

    def test_system_dns_is_authoritative_without_external_forwarders(self) -> None:
        documents = list(yaml.safe_load_all((HERE / "system.yaml").read_text()))
        config = next(document for document in documents if document["kind"] == "ConfigMap" and document["metadata"]["name"] == "coredns")
        self.assertNotIn("forward", config["data"]["Corefile"])
        self.assertNotIn("import", config["data"]["Corefile"])
        self.assertIn("hosts /etc/coredns/NodeHosts", config["data"]["Corefile"])
        self.assertIn("file /etc/coredns/blocked.zone .", config["data"]["Corefile"])
        self.assertIn("IN SOA", config["data"]["blocked.zone"])

    def test_storage_has_no_wildcards_or_cluster_pod_mutation(self) -> None:
        documents = list(yaml.safe_load_all((HERE / "system.yaml").read_text()))
        for role in (document for document in documents if document["kind"] in ("Role", "ClusterRole") and document["metadata"]["name"].startswith("local-path")):
            for rule in role["rules"]:
                self.assertNotIn("*", rule["verbs"])
                if role["kind"] == "ClusterRole":
                    self.assertNotIn("pods", rule["resources"])
        helper = next(document for document in documents if document["kind"] == "Role" and document["metadata"]["name"] == "local-path-helper-role")
        self.assertEqual(helper["metadata"]["namespace"], "kube-system")
        self.assertEqual(helper["rules"][0]["verbs"], ["get", "create", "delete"])
        config = next(rule for rule in helper["rules"] if rule["resources"] == ["configmaps"])
        self.assertEqual(config["verbs"], ["get"])
        self.assertEqual(config["resourceNames"], ["local-path-config"])

    def test_policy_requires_namespace_and_identity_for_target(self) -> None:
        documents = list(yaml.safe_load_all((HERE / "networkpolicy.yaml").read_text()))
        for namespace in ("openaev-lab", "hikyo-lab"):
            denial = next(document for document in documents
                          if document["metadata"] == {"name": "default-deny", "namespace": namespace})
            self.assertEqual(denial["spec"]["egress"], [])
            self.assertEqual(denial["spec"]["ingress"], [])
        inbound = next(document for document in documents
                       if document["metadata"]["name"] == "http-injector-only")
        rule = inbound["spec"]["ingress"][0]
        self.assertEqual(rule["from"], [{"namespaceSelector": {"matchLabels": {
            "kubernetes.io/metadata.name": "openaev-lab"}}, "podSelector": {
                "matchLabels": {"app": "http-injector"}}}])
        self.assertEqual(rule["ports"], [{"protocol": "TCP", "port": 8443}])


if __name__ == "__main__":
    unittest.main()

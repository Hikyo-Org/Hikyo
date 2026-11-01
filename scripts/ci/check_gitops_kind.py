#!/usr/bin/env python3
"""Exercise both GitOps recipes in an owned disposable Kind cluster, without a database.

The exact chart is installed at zero replicas. Local unsigned OCI fixtures test
controller mechanics, not production signature acceptance or SQL migrations.
"""

import argparse
import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "deploy"))
import render_gitops as gitops
from nightly_chart import prepare


NODE = "kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"
REGISTRY = "registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
CONTROLLERS = {
    "flux": ("https://github.com/fluxcd/flux2/releases/download/v2.10.0/install.yaml",
             "a8c838ccd9be5d30eba9df87cd229b7c4eb80c0049fba1bc3b9840829f267f28"),
    "argo-cd": ("https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.6/manifests/core-install.yaml",
                "d3d9d3555ad77eb4b6d27b1ec0d2af7978a91537e84d54631f983c227f0a3d31"),
}


class Campaign:
    def __init__(self, work: Path):
        self.work = work
        self.cluster = "hikyo-gitops-" + work.name.rsplit("-", 1)[-1].lower().replace("_", "a")
        self.registry = self.cluster + "-registry"
        self.environment = {**os.environ, "KUBECONFIG": str(work / "kubeconfig")}
        self.created = False
        self.registry_created = False
        self.evidence = {"scope": "zero-replica controller mechanics; no database or application runtime",
                         "controllers": {"flux": "v2.10.0", "argo-cd": "v3.5.6"}, "checks": []}
        self.inputs = [ROOT / "scripts/ci/check_gitops_kind.py", ROOT / "scripts/deploy/render_gitops.py",
                       ROOT / "scripts/release/nightly_chart.py", *sorted((ROOT / "install/gitops").glob("*.yaml")),
                       *sorted(path for path in (ROOT / "chart/hikyo").rglob("*") if path.is_file())]
        self.evidence["sourceFiles"] = self.fingerprint()

    def fingerprint(self) -> dict:
        return {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in self.inputs}

    def run(self, *args: str, data: str | None = None, timeout: int = 180) -> str:
        result = subprocess.run(args, input=data, env=self.environment, text=True,
                                capture_output=True, check=True, timeout=timeout, cwd=ROOT)
        return result.stdout

    def kubectl(self, *args: str, data: str | None = None) -> str:
        return self.run("kubectl", *args, data=data)

    def get(self, namespace: str, resource: str) -> dict:
        return json.loads(self.kubectl("-n", namespace, "get", resource, "-o", "json"))

    def apply(self, documents: list[dict], namespace: str | None = None) -> None:
        args = ["apply", "--server-side", "--field-manager=hikyo-gitops-fixture", "-f", "-"]
        if namespace:
            args += ["-n", namespace]
        self.kubectl(*args, data=yaml.safe_dump_all(documents, sort_keys=False))

    def patch(self, namespace: str, resource: str, document: dict) -> None:
        self.kubectl("-n", namespace, "patch", resource, "--type=merge", "-p", json.dumps(document))

    def wait(self, namespace: str, resource: str, predicate, description: str, seconds: int = 180) -> dict:
        deadline = time.monotonic() + seconds
        latest = {}
        while time.monotonic() < deadline:
            latest = self.get(namespace, resource)
            if predicate(latest):
                print("gitops-kind:", description, flush=True)
                return latest
            time.sleep(1)
        raise RuntimeError(f"{description}: deadline exceeded; status={json.dumps(latest.get('status', {}))[:2500]}")

    def unchanged(self, namespace: str, deployment: dict, seconds: int = 5) -> None:
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            actual = self.get(namespace, "deployment/hikyo-hikyo")
            if (actual["metadata"]["uid"] != deployment["metadata"]["uid"]
                    or actual["spec"] != deployment["spec"]):
                raise RuntimeError("controller changed a workload without approval")
            time.sleep(1)

    def setup(self) -> None:
        existing = self.run("kind", "get", "clusters").splitlines()
        if self.cluster in existing:
            raise RuntimeError("refusing to reuse an existing cluster")
        if self.run("docker", "ps", "--all", "--format", "{{.Names}}").splitlines().count(self.registry):
            raise RuntimeError("refusing to reuse an existing registry")
        self.created = True  # Also clean partial Kind creation after an error.
        print("gitops-kind: creating owned cluster", self.cluster, flush=True)
        self.run("kind", "create", "cluster", "--name", self.cluster, "--image", NODE,
                 "--kubeconfig", self.environment["KUBECONFIG"], "--wait", "120s", timeout=240)
        self.run("docker", "run", "--detach", "--rm", "--name", self.registry,
                 "--network", "kind", "--publish", "127.0.0.1::5000", REGISTRY)
        self.registry_created = True
        self.host = self.run("docker", "port", self.registry, "5000/tcp").strip()
        address = self.run("docker", "inspect", self.registry, "--format",
                           '{{(index .NetworkSettings.Networks "kind").IPAddress}}').strip()
        self.source = "oci://" + address + ":5000/charts/hikyo"
        self.evidence["kubernetes"] = json.loads(self.kubectl("version", "-o", "json"))["serverVersion"]["gitVersion"]
        self.evidence["helm"] = self.run("helm", "version", "--short").strip()

    def package(self, sequence: int) -> tuple[str, str, dict]:
        commit = self.run("git", "rev-parse", "HEAD").strip()
        version = f"0.0.1-nightly.20261101.{sequence}.g{commit[:8]}"
        image = {"repository": gitops.IMAGE, "digest": "sha256:" + str(sequence) * 64}
        chart = self.work / f"chart-{sequence}"
        prepare(ROOT / "chart/hikyo", chart, "Hikyo-Org/Hikyo", version, commit, image["digest"])
        self.run("helm", "package", str(chart), "--destination", str(self.work))
        result = subprocess.run(["helm", "push", str(self.work / f"hikyo-{version}.tgz"),
                                 f"oci://{self.host}/charts", "--plain-http"], env=self.environment,
                                text=True, check=True, capture_output=True, timeout=60)
        digests = re.findall(r"Digest:\s*(sha256:[0-9a-f]{64})", result.stdout + result.stderr)
        if len(digests) != 1:
            raise RuntimeError("registry did not report one immutable chart digest")
        return digests[0], version, image

    def install_controller(self, provider: str) -> None:
        url, digest = CONTROLLERS[provider]
        raw = urllib.request.urlopen(url, timeout=60).read()
        if hashlib.sha256(raw).hexdigest() != digest:
            raise RuntimeError("controller install manifest checksum mismatch")
        documents = [item for item in yaml.safe_load_all(raw) if item]
        namespace = "flux-system" if provider == "flux" else "argocd"
        self.kubectl("create", "namespace", namespace)
        if provider == "flux":
            # Run only the two components this recipe consumes on small hosts.
            documents = [item for item in documents if item["kind"] != "Namespace"
                         and (item["kind"] != "Deployment"
                              or item["metadata"]["name"] in ("source-controller", "helm-controller"))]
        else:
            documents = [item for item in documents
                         if "applicationset" not in item["metadata"]["name"]]
        self.apply(documents, namespace)
        workloads = [item for item in documents if item["kind"] in ("Deployment", "StatefulSet")]
        self.evidence[provider + "Images"] = {
            item["metadata"]["name"]: [c["image"] for c in item["spec"]["template"]["spec"]["containers"]]
            for item in workloads}
        for item in workloads:
            self.kubectl("-n", namespace, "rollout", "status",
                         item["kind"].lower() + "/" + item["metadata"]["name"], "--timeout=180s")
        pods = json.loads(self.kubectl("-n", namespace, "get", "pods", "-o", "json"))["items"]
        self.evidence[provider + "ImageIDs"] = {
            pod["metadata"]["name"]: {container["name"]: container.get("imageID")
                                      for container in pod.get("status", {}).get("containerStatuses", [])}
            for pod in pods}

    def recipe(self, provider: str, namespace: str, package: tuple) -> list[dict]:
        digest, version, image = package
        commit = self.run("git", "rev-parse", "HEAD").strip()
        values = yaml.safe_load((ROOT / "chart/hikyo/examples/singleton-manual.yaml").read_text())
        values["replicaCount"] = 0
        documents = gitops.manifests(provider, digest, commit, version, image, values, namespace, "argocd")
        for item in documents:
            item["metadata"]["annotations"]["hikyo.dev/test-fixture"] = "unsigned-local-OCI; no-runtime"
            if item["kind"] == "OCIRepository":
                item["spec"]["url"] = self.source
                item["spec"]["insecure"] = True
            elif item["kind"] == "HelmRelease":
                item["spec"]["timeout"] = "15s"
            elif item["kind"] == "AppProject":
                item["spec"]["sourceRepos"] = [self.source]
            elif item["kind"] == "Application":
                item["spec"]["source"]["repoURL"] = self.source
        return documents

    def namespace(self, namespace: str) -> dict:
        self.kubectl("create", "namespace", namespace)
        # External custody is deliberately not owned by either controller recipe.
        self.apply([{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": {
            "name": "external-custody", "namespace": namespace}, "spec": {
                "accessModes": ["ReadWriteOnce"], "resources": {"requests": {"storage": "1Mi"}}}}])
        return self.get(namespace, "pvc/external-custody")

    def flux(self, first: tuple, second: tuple) -> None:
        self.install_controller("flux")
        ns = "hikyo-flux-fixture"
        custody = self.namespace(ns)
        documents = self.recipe("flux", ns, first)
        self.apply(documents)
        self.wait(ns, "ocirepository/hikyo", lambda obj: any(
            c["type"] == "SourceVerified" and c["status"] == "False"
            for c in obj.get("status", {}).get("conditions", [])), "Flux refuses the unsigned fixture")
        if json.loads(self.kubectl("-n", ns, "get", "deployments", "-o", "json"))["items"]:
            raise RuntimeError("Flux installed before signature verification and approval")
        # Only the isolated test fixture drops verification; the shipped template
        # and generator always require keyless verification against the real source.
        self.patch(ns, "ocirepository/hikyo", {"spec": {"verify": None}})
        self.wait(ns, "ocirepository/hikyo", lambda obj: ready(obj), "Flux fetched the exact local digest")
        self.patch(ns, "helmrelease/hikyo", {"spec": {"suspend": False}})
        self.wait(ns, "helmrelease/hikyo", lambda obj: ready(obj), "Flux installed the zero-replica chart")
        installed = self.get(ns, "deployment/hikyo-hikyo")
        self.assert_image(installed, first[2])
        self.patch(ns, "helmrelease/hikyo", {"spec": {"suspend": True}})
        self.patch(ns, "ocirepository/hikyo", {"spec": {"ref": {"digest": second[0]}}})
        self.wait(ns, "ocirepository/hikyo", lambda obj: ready(obj) and
                  obj["status"]["artifact"]["revision"].endswith(second[0]), "Flux selected the candidate while suspended")
        self.unchanged(ns, installed)
        self.patch(ns, "helmrelease/hikyo", {"spec": {"suspend": False, "values": {"replicaCount": 1}}})
        failed = self.wait(ns, "helmrelease/hikyo", lambda obj: any(
            c["type"] == "Stalled" and c["status"] == "True"
            for c in obj.get("status", {}).get("conditions", [])), "Flux latched the failed candidate without rollback")
        self.assert_image(self.get(ns, "deployment/hikyo-hikyo"), second[2])
        failure = next(c for c in failed["status"]["conditions"] if c["type"] == "Ready")
        if failure["reason"] != "UpgradeFailed" or not any(
                word in failure["message"].lower() for word in ("deadline", "timeout", "timed out")):
            raise RuntimeError("candidate failed for an unexpected reason: " + failure["message"])
        history = json.loads(self.run("helm", "history", "hikyo", "-n", ns, "-o", "json"))
        if len(history) != 2 or history[-1]["status"] != "failed":
            raise RuntimeError("failed Flux candidate was rolled back, uninstalled or retried")
        if any(c["type"] == "Remediated" and c["status"] == "True" for c in failed["status"]["conditions"]):
            raise RuntimeError("Flux performed forbidden failure remediation")
        self.patch(ns, "helmrelease/hikyo", {"spec": {"suspend": True}})
        if self.get(ns, "pvc/external-custody")["metadata"]["uid"] != custody["metadata"]["uid"]:
            raise RuntimeError("Flux changed externally owned custody")
        self.evidence["checks"] += ["Flux unsigned refusal", "Flux exact-digest install at zero replicas",
                                    "Flux suspension", "Flux failed upgrade retained with no remediation"]
        # Finalize the release while its deployer Role/Binding and controller
        # still exist. Deleting their namespaces together strands finalizers.
        self.kubectl("-n", ns, "delete", "helmrelease/hikyo", "--wait=true", "--timeout=90s")
        if self.get(ns, "pvc/external-custody")["metadata"]["uid"] != custody["metadata"]["uid"]:
            raise RuntimeError("HelmRelease uninstall changed externally owned custody")
        self.kubectl("-n", ns, "delete", "ocirepository/hikyo", "--wait=true", "--timeout=60s")
        self.kubectl("delete", "namespace", ns, "--wait=true", "--timeout=120s")
        self.kubectl("delete", "namespace", "flux-system", "--wait=true", "--timeout=120s")

    @staticmethod
    def assert_image(deployment: dict, image: dict) -> None:
        server = next(c for c in deployment["spec"]["template"]["spec"]["containers"] if c["name"] == "server")
        if server["image"] != image["repository"] + "@" + image["digest"]:
            raise RuntimeError("workload did not retain the selected exact image")

    def argo(self, first: tuple, second: tuple) -> None:
        self.install_controller("argo-cd")
        ns = "hikyo-argo-fixture"
        custody = self.namespace(ns)
        self.apply([{"apiVersion": "v1", "kind": "Secret", "metadata": {
            "name": "local-fixture-registry", "namespace": "argocd",
            "labels": {"argocd.argoproj.io/secret-type": "repository"}},
            "stringData": {"type": "oci", "url": self.source, "insecureOCIForceHttp": "true"}}])
        self.apply(self.recipe("argo-cd", ns, first))
        self.wait("argocd", "application/hikyo", lambda obj: obj.get("status", {}).get("sync", {}).get("status") == "OutOfSync",
                  "Argo compared the chart without automatically installing")
        if json.loads(self.kubectl("-n", ns, "get", "deployments", "-o", "json"))["items"]:
            raise RuntimeError("Argo installed before explicit manual synchronization")
        self.patch("argocd", "application/hikyo", {"operation": {"sync": {"revision": first[0], "prune": False}}})
        self.wait("argocd", "application/hikyo", lambda obj: obj.get("status", {}).get("operationState", {}).get("phase") == "Succeeded",
                  "Argo manually synchronized the zero-replica chart")
        installed = self.get(ns, "deployment/hikyo-hikyo")
        self.assert_image(installed, first[2])
        self.patch("argocd", "application/hikyo", {"spec": {"source": {"targetRevision": second[0]}}})
        self.wait("argocd", "application/hikyo", lambda obj: obj.get("status", {}).get("sync", {}).get("status") == "OutOfSync"
                  and obj["status"]["sync"].get("revision") == second[0], "Argo retained a new target for operator approval")
        self.unchanged(ns, installed)
        # A Kubernetes validation failure is immediate, with no SQL writes.
        self.patch("argocd", "application/hikyo", {"spec": {"source": {"helm": {"valuesObject": {
            "resources": {"requests": {"memory": "-1Mi"}}}}}},
            "operation": {"sync": {"revision": second[0], "prune": False}}})
        self.wait("argocd", "application/hikyo", lambda obj: obj.get("status", {}).get("operationState", {}).get("phase") == "Failed",
                  "Argo retained an explicitly failed synchronization")
        self.kubectl("-n", "argocd", "delete", "application/hikyo", "--wait=true")
        if self.get(ns, "deployment/hikyo-hikyo")["metadata"]["uid"] != installed["metadata"]["uid"]:
            raise RuntimeError("Application deletion cascaded into the retained workload")
        if self.get(ns, "pvc/external-custody")["metadata"]["uid"] != custody["metadata"]["uid"]:
            raise RuntimeError("Application deletion changed externally owned custody")
        self.evidence["checks"] += ["Argo manual exact-digest sync at zero replicas", "Argo target change does not auto-sync",
                                    "Argo failed sync retained", "Argo Application deletion retains workload and external PVC"]

    def cleanup(self) -> None:
        errors = []
        if self.created:
            try:
                self.run("kind", "delete", "cluster", "--name", self.cluster)
            except subprocess.SubprocessError as error:
                errors.append(str(error))
        if self.registry_created:
            try:
                self.run("docker", "stop", self.registry)
            except subprocess.SubprocessError as error:
                errors.append(str(error))
        if errors:
            raise RuntimeError("owned resource cleanup failed: " + "; ".join(errors))


def ready(document: dict) -> bool:
    return document.get("status", {}).get("observedGeneration") == document["metadata"]["generation"] and any(
        item["type"] == "Ready" and item["status"] == "True" for item in document.get("status", {}).get("conditions", []))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--receipt", required=True, type=Path)
    args = parser.parse_args()
    if args.receipt.exists():
        parser.error("receipt already exists; retain the previous observation")
    def interrupted(signum, frame):
        raise KeyboardInterrupt("controller campaign interrupted")
    signal.signal(signal.SIGTERM, interrupted)
    with tempfile.TemporaryDirectory(prefix="hikyo-gitops-") as temporary:
        campaign = Campaign(Path(temporary))
        try:
            campaign.setup()
            first, second = campaign.package(1), campaign.package(2)
            campaign.flux(first, second)
            campaign.argo(first, second)
            if campaign.fingerprint() != campaign.evidence["sourceFiles"]:
                raise RuntimeError("controller input bytes changed during qualification")
        except Exception:
            if campaign.created:
                try:
                    print(campaign.kubectl("get", "pods", "--all-namespaces", "-o", "wide"), file=sys.stderr)
                    print(campaign.kubectl("get", "events", "--all-namespaces", "--sort-by=.lastTimestamp"), file=sys.stderr)
                except subprocess.SubprocessError as diagnostic_error:
                    print("gitops-kind: diagnostics unavailable:", diagnostic_error, file=sys.stderr)
            raise
        finally:
            campaign.cleanup()
        args.receipt.write_text(json.dumps(campaign.evidence, indent=2) + "\n")
        print("gitops-kind: controller campaign passed; application/migration acceptance NOTRUN", flush=True)


if __name__ == "__main__":
    main()

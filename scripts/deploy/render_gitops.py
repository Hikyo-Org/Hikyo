#!/usr/bin/env python3
"""Authenticate a stable or nightly chart/image pair and generate an approval-gated GitOps install."""

import argparse
import copy
import ipaddress
import json
import re
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path
from urllib.parse import urlsplit

import yaml

# Share the publisher's bounded, non-extracting chart archive inspection.
ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "release"))
from nightly_chart import package_files


CHART = "oci://ghcr.io/hikyo-org/charts/hikyo"
IMAGE = "ghcr.io/hikyo-org/hikyo"
WORKFLOWS = "https://github.com/Hikyo-Org/Hikyo/.github/workflows/"
IDENTITY = WORKFLOWS + "nightly.yml@refs/heads/main"
ISSUER = "https://token.actions.githubusercontent.com"
# release-channel.sh classifies 0.x tags as prereleases.
STABLE_VERSION = r"[1-9][0-9]*\.[0-9]+\.[0-9]+"
K8S_NAME = r"(?=.{1,253}\Z)[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*"
NIGHTLY_VERSION = r"[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]{8}\.[1-9][0-9]*\.g[0-9a-f]{8}"


def run(*args: str) -> str:
    result = subprocess.run(args, check=True, text=True, capture_output=True, timeout=180)
    return result.stdout


def is_stable(version: str) -> bool:
    return re.fullmatch(STABLE_VERSION, version) is not None


def signing_identity(version: str) -> str:
    return f"{WORKFLOWS}release.yml@refs/tags/v{version}" if is_stable(version) else IDENTITY


def verify(reference: str, commit: str, version: str) -> None:
    run("cosign", "verify", reference,
        "--certificate-identity", signing_identity(version),
        "--certificate-oidc-issuer", ISSUER,
        "--certificate-github-workflow-sha", commit)


def check_published(version: str) -> None:
    # The tag job signs before draft review, so a signature alone does not prove release.
    tag = "v" + version
    release = json.loads(run("gh", "release", "view", tag, "-R", "github.com/Hikyo-Org/Hikyo",
                             "--json", "tagName,isDraft,isPrerelease"))
    if release != {"tagName": tag, "isDraft": False, "isPrerelease": False}:
        raise ValueError(f"{tag} is not a published stable release")


def inspect_chart(package: Path, commit: str, version: str) -> dict:
    files = package_files(package)
    metadata = yaml.safe_load(files["hikyo/Chart.yaml"])
    defaults = yaml.safe_load(files["hikyo/values.yaml"])
    expected = {
        "org.opencontainers.image.source": "https://github.com/Hikyo-Org/Hikyo",
        "org.opencontainers.image.revision": commit,
        "hikyo.dev/release-profile": "nightly/v1",
    }
    # Stable packaging stamps no annotations: the tag identity and workflow SHA
    # checked by Cosign bind its source, and the channel excludes a nightly.
    profile = (defaults.get("updates", {}).get("channel") == "stable" if is_stable(version)
               else all(metadata.get("annotations", {}).get(k) == v for k, v in expected.items()))
    if (metadata.get("name") != "hikyo" or metadata.get("type") != "application"
            or metadata.get("version") != version or metadata.get("appVersion") != version
            or not profile):
        raise ValueError("chart version, profile or source differs from the selected release")
    image = defaults["image"]
    if image.get("repository") != IMAGE or not re.fullmatch(r"sha256:[0-9a-f]{64}", image.get("digest", "")):
        raise ValueError("chart does not pin the canonical image digest")
    return image


def validate_values(values: dict) -> None:
    # This recipe has one supported custody/topology contract. Explicit checks
    # prevent a convenient generator from quietly enrolling a different mode.
    if not isinstance(values, dict) or "image" in values:
        raise ValueError("values must be a mapping without image overrides")
    allowed = {
        "replicaCount": None, "ha": {"enabled": None}, "operator": {"enabled": None},
        "rollout": {"enabled": None},
        "database": {"existingSecret": None, "tls": {"existingSecret": None, "key": None}},
        "rootKey": {"existingSecret": None, "key": None}, "tls": {"existingSecret": None},
        "externalOrigin": None, "network": {"trustedProxyCIDRs": None, "allowPlaintextOrigin": None},
        "upgrade": {"existingClaim": None, "stateExistingClaim": None, "evidence": None,
                    "targetManifestSHA256": None, "legacyWritersStopped": None,
                    "unattended": {"enabled": None}},
        "resources": {"requests": {"cpu": None, "memory": None, "ephemeral-storage": None},
                      "limits": {"cpu": None, "memory": None, "ephemeral-storage": None}},
        "releaseTrust": {"admissionPolicy": {"enabled": None}},
    }

    def check_keys(current: dict, schema: dict, prefix: str = "") -> None:
        if not isinstance(current, dict) or current.keys() - schema.keys():
            raise ValueError(f"unsupported values at {prefix or 'root'}; this recipe accepts references, not secret data")
        for key, child in current.items():
            if schema[key] is not None:
                check_keys(child, schema[key], prefix + key + ".")
            elif isinstance(child, (dict, list)) and prefix + key != "network.trustedProxyCIDRs":
                raise ValueError(f"{prefix + key} must be a scalar reference or setting")

    check_keys(values, allowed)
    if type(values.get("replicaCount")) is not int or values["replicaCount"] not in (0, 1):
        raise ValueError("the singleton recipe requires replicaCount 0 or 1")
    for mode in ("ha", "operator", "rollout"):
        if values.get(mode, {}).get("enabled") is not False:
            raise ValueError(f"the singleton recipe requires {mode}.enabled=false")
    upgrade = values.get("upgrade", {})
    if upgrade.get("unattended", {}).get("enabled") is not False:
        raise ValueError("the GitOps recipe uses manual custody, not unattended enrollment")
    if (not upgrade.get("existingClaim") or not upgrade.get("stateExistingClaim")
            or upgrade["existingClaim"] == upgrade["stateExistingClaim"]):
        raise ValueError("separate public-bundle and private installation claims are required")
    for owner in ("database", "rootKey", "tls"):
        if not values.get(owner, {}).get("existingSecret"):
            raise ValueError(f"{owner}.existingSecret is required; keep secret values outside Git")
    # A chart template may render these unquoted, so a crafted name could add pod spec fields.
    names = [upgrade["existingClaim"], upgrade["stateExistingClaim"],
             *(values[owner]["existingSecret"] for owner in ("database", "rootKey", "tls")),
             values["database"].get("tls", {}).get("existingSecret", "")]
    if not all(isinstance(name, str) and (not name or re.fullmatch(K8S_NAME, name)) for name in names):
        raise ValueError("claim and secret references must be Kubernetes resource names")
    origin = urlsplit(str(values.get("externalOrigin", "")))
    if (origin.scheme != "https" or not origin.hostname or origin.username or origin.password
            or origin.path not in ("", "/") or origin.query or origin.fragment
            or values.get("network", {}).get("allowPlaintextOrigin") is not False):
        raise ValueError("the native-TLS recipe requires an HTTPS origin")
    cidrs = values.get("network", {}).get("trustedProxyCIDRs", [])
    if not isinstance(cidrs, list) or not all(isinstance(cidr, str) for cidr in cidrs):
        raise ValueError("trustedProxyCIDRs must be a list of CIDR strings")
    for cidr in cidrs:
        ipaddress.ip_network(cidr, strict=False)
    if values.get("releaseTrust", {}).get("admissionPolicy", {}).get("enabled") is not False:
        raise ValueError("cluster-wide admission policy installation is separately owned")


def check_rendered(raw: str, namespace: str, image: dict, replicas: int) -> None:
    documents = [item for item in yaml.safe_load_all(raw) if item]
    allowed = {("apps/v1", "Deployment"), ("v1", "Service"), ("v1", "ConfigMap"),
               ("networking.k8s.io/v1", "NetworkPolicy")}
    if any((item.get("apiVersion"), item.get("kind")) not in allowed for item in documents):
        raise ValueError("rendered chart exceeds the namespace-scoped recipe permissions")
    deployments = [item for item in documents if item["kind"] == "Deployment"]
    if len(deployments) != 1:
        raise ValueError("expected one singleton Deployment")
    deployment = deployments[0]
    if (deployment["metadata"]["name"] != "hikyo-hikyo"
            or deployment["spec"]["replicas"] != replicas
            or deployment["spec"]["strategy"] != {"type": "Recreate"}):
        raise ValueError("unexpected release name, replica count or replacement strategy")
    pod = deployment["spec"]["template"]["spec"]
    expected = image["repository"] + "@" + image["digest"]
    if pod.get("automountServiceAccountToken") is not False:
        raise ValueError("the server must not receive controller credentials")
    if any(container["image"] != expected for container in pod.get("initContainers", []) + pod["containers"]):
        raise ValueError("rendered chart changes the verified image")
    # `admin` has no root-key flag, so without this the chart offers no first-administrator route.
    server = next((c for c in pod["containers"] if c["name"] == "server"), {})
    if not any(item.get("name") == "HIKYO_ROOT_KEY_FILE" for item in server.get("env", [])):
        raise ValueError("chart gives in-pod operator commands no root key, so `hikyo admin create` "
                         "cannot run; select a release that sets HIKYO_ROOT_KEY_FILE")
    if any(item["metadata"].get("namespace", namespace) != namespace for item in documents):
        raise ValueError("rendered chart escapes the target namespace")


def manifests(provider: str, digest: str, commit: str, version: str,
              image: dict, values: dict, namespace: str, controller_namespace: str) -> list[dict]:
    filename = "flux.yaml" if provider == "flux" else "argo-cd.yaml"
    documents = list(yaml.safe_load_all((ROOT / "install" / "gitops" / filename).read_text()))
    annotations = {
        "hikyo.dev/chart-digest": digest,
        "hikyo.dev/source-commit": commit,
        "hikyo.dev/chart-version": version,
        "hikyo.dev/image": image["repository"] + "@" + image["digest"],
    }
    for item in documents:
        item["metadata"]["namespace"] = namespace if provider == "flux" else controller_namespace
        item["metadata"].setdefault("annotations", {}).update(annotations)
        if item["kind"] == "RoleBinding":
            item["subjects"][0]["namespace"] = namespace
        elif item["kind"] == "OCIRepository":
            item["spec"]["ref"] = {"digest": digest}
            item["spec"]["verify"]["matchOIDCIdentity"][0]["subject"] = (
                "^" + signing_identity(version).replace(".", r"\.") + "$")
        elif item["kind"] == "HelmRelease":
            item["spec"]["targetNamespace"] = namespace
            item["spec"]["values"] = copy.deepcopy(values)
        elif item["kind"] == "AppProject":
            item["spec"]["destinations"][0]["namespace"] = namespace
        elif item["kind"] == "Application":
            item["spec"]["source"]["targetRevision"] = digest
            item["spec"]["source"]["helm"]["valuesObject"] = copy.deepcopy(values)
            item["spec"]["destination"]["namespace"] = namespace
    return documents


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--provider", required=True, choices=("flux", "argo-cd"))
    parser.add_argument("--chart-digest", required=True)
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--chart-version", required=True)
    parser.add_argument("--values", required=True, type=Path)
    parser.add_argument("--namespace", default="hikyo")
    parser.add_argument("--controller-namespace")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", args.chart_digest):
        parser.error("chart-digest must be an exact SHA-256 OCI manifest digest")
    if not re.fullmatch(r"[0-9a-f]{40}", args.source_commit):
        parser.error("source-commit must be the full reviewed source commit")
    if not is_stable(args.chart_version) and (
            not re.fullmatch(NIGHTLY_VERSION, args.chart_version)
            or not args.chart_version.endswith(".g" + args.source_commit[:8])):
        parser.error("chart-version must be a stable X.Y.Z or identify the selected nightly source")
    controller_namespace = args.controller_namespace or ("flux-system" if args.provider == "flux" else "argocd")
    for name in (args.namespace, controller_namespace):
        if len(name) > 63 or not re.fullmatch(r"[a-z0-9](?:[-a-z0-9]*[a-z0-9])?", name):
            parser.error("namespace must be a DNS label")
    if args.output.exists():
        parser.error("output already exists; preserve the prior reviewed installation record")
    values = yaml.safe_load(args.values.read_text())
    validate_values(values)
    if is_stable(args.chart_version):
        check_published(args.chart_version)
    with tempfile.TemporaryDirectory(prefix="hikyo-gitops-") as temporary:
        work = Path(temporary)
        reference = CHART.removeprefix("oci://") + "@" + args.chart_digest
        verify(reference, args.source_commit, args.chart_version)
        run("helm", "pull", "oci://" + reference, "--destination", str(work))
        packages = list(work.glob("*.tgz"))
        if len(packages) != 1:
            raise ValueError("expected one downloaded chart")
        image = inspect_chart(packages[0], args.source_commit, args.chart_version)
        verify(image["repository"] + "@" + image["digest"], args.source_commit, args.chart_version)
        normalized_values = work / "operator-values.yaml"
        normalized_values.write_text(yaml.safe_dump(values, sort_keys=False))
        rendered = run("helm", "template", "hikyo", str(packages[0]),
                       "--namespace", args.namespace, "--values", str(normalized_values))
        check_rendered(rendered, args.namespace, image, values["replicaCount"])
        documents = manifests(args.provider, args.chart_digest, args.source_commit,
                              args.chart_version, image, values, args.namespace, controller_namespace)
        with args.output.open("x") as output:
            # open("x") just created this file, so a retry would otherwise refuse the partial record.
            try:
                output.write("# Verified exact chart; controller activation requires operator approval.\n")
                yaml.safe_dump_all(documents, output, sort_keys=False)
            except BaseException:
                args.output.unlink(missing_ok=True)
                raise
    print(f"Wrote {args.output}; review before applying. No cluster was changed.")


def entry() -> None:
    try:
        main()
    except (ValueError, KeyError, TypeError, AttributeError, EOFError, tarfile.TarError,
            yaml.YAMLError, OSError, subprocess.SubprocessError) as error:
        detail = getattr(error, "stderr", None) or ""
        print(f"gitops: refused: {error} {detail}".rstrip(), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    entry()

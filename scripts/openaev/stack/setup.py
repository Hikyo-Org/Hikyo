#!/usr/bin/env python3
"""Create a disposable hosted ARM64 cluster without importing persistent lab state."""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import re
import secrets
import shlex
import shutil
import subprocess
import sys
import time
import uuid
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
STAGE = "preflight"
STATE: Path | None = None
K3D_VERSION = "v5.8.3"
K3D_CHECKSUMS = {
    "linux-arm64": "0b8110f2229631af7402fb828259330985918b08fefd38b7f1b788a1c8687216",
    "darwin-arm64": "8da468daa7dc7cf7cdd4735f90a9bb05179fa27858250f62e3d8cdf5b5ca0698",
}


def command(arguments: list[str], *, payload: bytes | None = None, timeout: int = 1200,
            environment: dict[str, str] | None = None) -> bytes:
    """Capture all subprocess output: errors must never print credential-bearing logs."""
    try:
        return subprocess.run(arguments, input=payload, check=True, capture_output=True,
                              timeout=timeout, env=environment).stdout
    except (OSError, subprocess.SubprocessError) as error:
        name = Path(arguments[0]).name
        raise RuntimeError(f"stack_command_failed:{STAGE}:{name}") from error


def apply(documents: list[dict[str, object]]) -> None:
    command(["kubectl", "apply", "-f", "-"], payload=json.dumps({
        "apiVersion": "v1", "kind": "List", "items": documents,
    }).encode())


def secret(name: str, namespace: str, data: dict[str, bytes]) -> dict[str, object]:
    return {"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
            "metadata": {"name": name, "namespace": namespace},
            "data": {key: base64.b64encode(value).decode() for key, value in data.items()}}


def certificate(state: Path, prefix: str, sans: str) -> None:
    command(["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes",
             "-keyout", str(state / f"{prefix}.key"), "-out", str(state / f"{prefix}.csr"),
             "-subj", f"/CN=Disposable {prefix}"])
    extension = state / f"{prefix}.ext"
    extension.write_text("basicConstraints=critical,CA:FALSE\n"
                         "keyUsage=critical,digitalSignature,keyEncipherment\n"
                         "extendedKeyUsage=serverAuth\nsubjectAltName=" + sans + "\n")
    command(["openssl", "x509", "-req", "-in", str(state / f"{prefix}.csr"),
             "-CA", str(state / "ca.crt"), "-CAkey", str(state / "ca.key"),
             "-CAcreateserial", "-days", "2", "-extfile", str(extension),
             "-out", str(state / f"{prefix}.crt")])


def fresh_custody(state: Path) -> list[dict[str, object]]:
    command(["openssl", "req", "-x509", "-newkey", "rsa:3072", "-nodes", "-days", "2",
             "-keyout", str(state / "ca.key"), "-out", str(state / "ca.crt"),
             "-subj", "/CN=Ephemeral hosted OpenAEV Hikyo CA",
             "-addext", "basicConstraints=critical,CA:TRUE",
             "-addext", "keyUsage=critical,keyCertSign,cRLSign"])
    certificate(state, "tls", "DNS:hikyo.hikyo-lab.svc.cluster.local,"
                "DNS:hikyo.hikyo-lab.svc,DNS:hikyo,DNS:localhost,IP:127.0.0.1")
    certificate(state, "openaev", "DNS:openaev.openaev-lab.svc.cluster.local,"
                "DNS:openaev.openaev-lab.svc,DNS:openaev,DNS:localhost,IP:127.0.0.1")
    command(["openssl", "ecparam", "-name", "prime256v1", "-genkey", "-noout",
             "-out", str(state / "operator.key")])
    command(["openssl", "pkey", "-in", str(state / "operator.key"), "-pubout",
             "-out", str(state / "operator.pub")])
    (state / "root-key").write_text(secrets.token_hex(32) + "\n")
    password = secrets.token_urlsafe(32)
    minio_password = secrets.token_urlsafe(32)
    rabbit_password = secrets.token_urlsafe(32)
    p12_password = secrets.token_urlsafe(32)
    credentials = {
        "POSTGRES_PASSWORD": password, "SPRING_DATASOURCE_PASSWORD": password,
        "MINIO_ROOT_USER": "openaev", "MINIO_ACCESS_KEY": "openaev",
        "MINIO_ROOT_PASSWORD": minio_password, "MINIO_ACCESS_SECRET": minio_password,
        "RABBITMQ_DEFAULT_PASS": rabbit_password, "OPENAEV_RABBITMQ_PASS": rabbit_password,
        "RABBITMQ_ERLANG_COOKIE": secrets.token_hex(32),
        "OPENAEV_ADMIN_EMAIL": "hosted-lab@example.org",
        "OPENAEV_ADMIN_PASSWORD": secrets.token_urlsafe(32),
        "OPENAEV_ADMIN_TOKEN": str(uuid.uuid4()),
        "OPENAEV_HEALTHCHECK_KEY": secrets.token_urlsafe(32),
        "OPENAEV_ADMIN_ENCRYPTION_KEY": secrets.token_hex(32),
        "OPENAEV_ADMIN_ENCRYPTION_SALT": secrets.token_hex(16),
        "SERVER_SSL_KEY_STORE_PASSWORD": p12_password,
        "INJECTOR_ID": str(uuid.uuid4()),
    }
    (state / "credentials.json").write_text(json.dumps(credentials))
    (state / "openaev-token").write_text(credentials["OPENAEV_ADMIN_TOKEN"] + "\n")
    environment = dict(os.environ, OPENAEV_P12_PASSWORD=p12_password)
    command(["openssl", "pkcs12", "-export", "-name", "openaev", "-in",
             str(state / "openaev.crt"), "-inkey", str(state / "openaev.key"),
             "-certfile", str(state / "ca.crt"), "-out", str(state / "openaev.p12"),
             "-passout", "env:OPENAEV_P12_PASSWORD"], environment=environment)
    custody = secret("hikyo-lab-custody", "hikyo-lab", {
        name: (state / name).read_bytes() for name in ("root-key", "tls.crt", "tls.key", "ca.crt")
    })
    (state / "custody.json").write_text(json.dumps(custody))
    return [custody, secret("credentials", "openaev-lab", {
        key: value.encode() for key, value in credentials.items()
    }), secret("openaev-tls", "openaev-lab", {"openaev.p12": (state / "openaev.p12").read_bytes()}),
        {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {
            "name": "lab-ca", "namespace": "openaev-lab"}, "data": {
                "ca.crt": (state / "ca.crt").read_text()}},
        {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {
            "name": "hikyo-lab-config", "namespace": "hikyo-lab"}, "data": {
                "operator.pub": (state / "operator.pub").read_text()}}]


def namespaces() -> list[dict[str, object]]:
    return [{"apiVersion": "v1", "kind": "Namespace", "metadata": {
        "name": name, "labels": {"app.kubernetes.io/part-of": "openaev-hosted",
                                  "pod-security.kubernetes.io/enforce": "restricted",
                                  "pod-security.kubernetes.io/enforce-version": "v1.36"}}}
            for name in ("openaev-lab", "hikyo-lab", "openaev-forbidden", "openaev-control")]


def canaries(image: str) -> list[dict[str, object]]:
    context = {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
               "capabilities": {"drop": ["ALL"]}}
    pod_context = {"runAsNonRoot": True, "runAsUser": 10001, "runAsGroup": 10001,
                   "seccompProfile": {"type": "RuntimeDefault"}}
    documents: list[dict[str, object]] = []
    for name, namespace, program in (
        ("canary", "openaev-forbidden", "from http.server import HTTPServer,BaseHTTPRequestHandler;"
         "HTTPServer(('0.0.0.0',8080),BaseHTTPRequestHandler).serve_forever()"),
        ("control", "openaev-control", "import time; time.sleep(86400)"),
    ):
        documents.append({"apiVersion": "apps/v1", "kind": "Deployment",
            "metadata": {"name": name, "namespace": namespace}, "spec": {"replicas": 1,
            "selector": {"matchLabels": {"app": name}}, "template": {
                "metadata": {"labels": {"app": name}}, "spec": {
                    "automountServiceAccountToken": False, "securityContext": pod_context,
                    "containers": [{"name": name, "image": image, "imagePullPolicy": "Never",
                        "command": ["python", "-c", program], "securityContext": context,
                        "resources": {"requests": {"cpu": "10m", "memory": "16Mi"},
                                      "limits": {"cpu": "100m", "memory": "64Mi"}},
                        **({"readinessProbe": {"tcpSocket": {"port": 8080}}}
                           if name == "canary" else {})}]}}}})
    for name, port in (("canary", 8080), ("canary-ingress", 443)):
        documents.append({"apiVersion": "v1", "kind": "Service", "metadata": {
            "name": name, "namespace": "openaev-forbidden"}, "spec": {
                "selector": {"app": "canary"}, "ports": [{"port": port, "targetPort": 8080}]}})
    documents.append({"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
        "metadata": {"name": "control-no-ingress", "namespace": "openaev-control"},
        "spec": {"podSelector": {}, "policyTypes": ["Ingress"], "ingress": []}})
    return documents


def start_workloads(documents: list[dict[str, object]]) -> None:
    """Native backend readiness must precede the first OpenAEV Java process."""
    global STAGE
    backend_names = ("rabbitmq", "pgsql", "elasticsearch", "silo")
    frontend_names = ("openaev", "http-injector")
    backends: list[dict[str, object]] = []
    frontends: list[dict[str, object]] = []
    for document in documents:
        metadata = document.get("metadata")
        name = metadata.get("name") if isinstance(metadata, dict) else None
        if not isinstance(name, str):
            raise RuntimeError("invalid_stack_workload_name")
        if name in backend_names:
            backends.append(document)
        elif name in frontend_names:
            frontends.append(document)
        else:
            raise RuntimeError("invalid_stack_workload_name")
    STAGE = "apply-backend-workloads"
    apply(backends)
    print("Waiting for four native backend readiness probes before OpenAEV starts", flush=True)
    for name in backend_names:
        STAGE = "wait-statefulset-" + name
        command(["kubectl", "rollout", "status", "statefulset/" + name, "-n", "openaev-lab", "--timeout=900s"])
    STAGE = "apply-openaev-workloads"
    apply(frontends)
    print("Starting OpenAEV and the HTTP injector with all backends ready", flush=True)
    for name in frontend_names:
        STAGE = "wait-deployment-" + name
        command(["kubectl", "rollout", "status", "deployment/" + name, "-n", "openaev-lab", "--timeout=900s"])


def wait_for_rabbitmq_remount() -> None:
    """A StatefulSet replacement may not exist yet after deletion completes."""
    global STAGE
    deadline = time.monotonic() + 180
    for condition, stage in (("create", "verify-rabbitmq-remount-create"),
                             ("condition=Ready", "verify-rabbitmq-remount-ready")):
        STAGE = stage
        remaining = int(deadline - time.monotonic())
        if remaining <= 0:
            raise RuntimeError("rabbitmq_remount_wait_expired")
        command(["kubectl", "wait", "--for=" + condition, "pod/rabbitmq-0", "-n", "openaev-lab",
                 "--timeout=" + str(remaining) + "s"], timeout=remaining)


def install_k3d(state: Path) -> str:
    system = "linux" if sys.platform == "linux" else "darwin" if sys.platform == "darwin" else ""
    platform = system + "-arm64"
    if platform not in K3D_CHECKSUMS:
        raise RuntimeError("stack_requires_arm64_linux_runner")
    binary = state / "bin/k3d"
    binary.parent.mkdir()
    command(["curl", "--fail", "--silent", "--show-error", "--location", "--retry", "3",
             f"https://github.com/k3d-io/k3d/releases/download/{K3D_VERSION}/k3d-{platform}",
             "--output", str(binary)])
    if hashlib.sha256(binary.read_bytes()).hexdigest() != K3D_CHECKSUMS[platform]:
        raise RuntimeError("k3d_binary_checksum_mismatch")
    binary.chmod(0o700)
    return str(binary)


def main() -> None:
    global STAGE, STATE
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("state", type=Path)
    parser.add_argument("image", help="Already signature-verified immutable Hikyo release image")
    args = parser.parse_args()
    if not re.fullmatch(r"ghcr\.io/hikyo-org/hikyo@sha256:[a-f0-9]{64}", args.image):
        raise RuntimeError("immutable_hikyo_image_required")
    for executable in ("docker", "kubectl", "openssl", "curl", "go"):
        if shutil.which(executable) is None:
            raise RuntimeError("stack_tool_missing:" + executable)
    if command(["docker", "info", "--format", "{{.Architecture}} {{.OSType}}"]).decode().strip() != "aarch64 linux":
        raise RuntimeError("stack_requires_arm64_linux_docker")
    os.umask(0o077)
    state = args.state.resolve()
    state.mkdir(parents=True, mode=0o700, exist_ok=False)
    STATE = state
    STAGE = "install-k3d"
    cluster = "openaev-" + secrets.token_hex(5)
    (state / "cluster-name").write_text(cluster)
    k3d = install_k3d(state)
    images = json.loads((HERE / "images.json").read_text())
    os.environ["K3D_IMAGE_TOOLS"] = images["k3d_tools"]
    STAGE = "build-injector"
    print("Building pinned HTTP injector for ephemeral cluster", flush=True)
    archive = state / "injector.oci.tar"
    command(["docker", "buildx", "build", "--platform", "linux/arm64", "--provenance=false",
             "--output", f"type=oci,dest={archive},name=docker.io/openaev-hosted/injector:source",
             str(HERE / "injector-image")])
    STAGE = "create-cluster"
    print("Creating fresh network-policy-enforced K3s cluster", flush=True)
    command([k3d, "cluster", "create", cluster, "--image", images["k3s"], "--servers", "1",
             "--agents", "0", "--no-lb", "--api-port", "127.0.0.1:0",
             "--k3s-arg", "--disable=traefik,servicelb,metrics-server,coredns,local-storage@server:0",
             "--k3s-arg", "--pause-image=" + images["pause"].replace("@", "\\@") + "@server:0",
             "--kubeconfig-update-default=false", "--kubeconfig-switch-context=false", "--wait"])
    node = "k3d-" + cluster + "-server-0"
    mounts = json.loads(command(["docker", "inspect", node, "--format", "{{json .Mounts}}"] ))
    (state / "node-volumes.json").write_text(json.dumps([mount["Name"] for mount in mounts if mount["Type"] == "volume"]))
    config = yaml.safe_load(command([k3d, "kubeconfig", "get", cluster]))
    ports = json.loads(command(["docker", "inspect", node, "--format", "{{json .NetworkSettings.Ports}}"]))
    config["clusters"][0]["cluster"]["server"] = "https://127.0.0.1:" + ports["6443/tcp"][0]["HostPort"]
    (state / "kubeconfig").write_text(yaml.safe_dump(config))
    os.environ["KUBECONFIG"] = str(state / "kubeconfig")
    command(["kubectl", "label", "node", node, "fleet.went.io/tier=main"])
    command(["kubectl", "wait", "--for=condition=Ready", "node", "--all", "--timeout=120s"])
    STAGE = "pull-pinned-images"
    # Import only pinned images before any restricted application starts.
    for image in [images[key] for key in ("rabbitmq", "pgsql", "elasticsearch", "silo", "openaev", "preparer", "coredns", "local_path", "storage_helper", "pause")] + [args.image]:
        reference = image if image.startswith(("ghcr.io/", "docker.io/", "docker.elastic.co/")) else "docker.io/" + ("library/" if "/" not in image.split("@")[0] else "") + image
        command(["docker", "exec", node, "ctr", "images", "pull", "--platform", "linux/arm64", reference])
    STAGE = "import-injector"
    command(["docker", "cp", str(archive), node + ":/tmp/injector.oci.tar"])
    command(["docker", "exec", node, "ctr", "images", "import", "/tmp/injector.oci.tar"])
    imported = command(["docker", "exec", node, "ctr", "images", "list"]).decode().splitlines()
    injector_digest = next(line.split()[2] for line in imported
                           if line.split()[0] == "docker.io/openaev-hosted/injector:source")
    injector = "docker.io/openaev-hosted/injector@" + injector_digest
    command(["docker", "exec", node, "ctr", "images", "tag", "docker.io/openaev-hosted/injector:source", injector])
    command(["docker", "exec", node, "rm", "/tmp/injector.oci.tar"])
    archive.unlink()
    STAGE = "install-system-services"
    system_documents = list(yaml.safe_load_all((HERE / "system.yaml").read_text()))
    apply([document for document in system_documents if document.get("metadata", {}).get("namespace") not in ("openaev-lab", "hikyo-lab")])
    command(["kubectl", "rollout", "status", "deployment/coredns", "-n", "kube-system", "--timeout=120s"])
    command(["kubectl", "rollout", "status", "deployment/local-path-provisioner", "-n", "kube-system", "--timeout=120s"])
    STAGE = "create-private-custody"
    apply(namespaces())
    apply([document for document in system_documents if document.get("metadata", {}).get("namespace") in ("openaev-lab", "hikyo-lab")])
    apply([{"apiVersion": "scheduling.k8s.io/v1", "kind": "PriorityClass", "metadata": {
        "name": "openaev-lab"}, "value": 1000, "globalDefault": False,
        "description": "Disposable hosted OpenAEV lab"}])
    apply(fresh_custody(state))
    apply(list(yaml.safe_load_all((HERE / "networkpolicy.yaml").read_text())))
    STAGE = "start-isolation-controls"
    apply(canaries(injector))
    command(["kubectl", "rollout", "status", "deployment/control", "-n", "openaev-control", "--timeout=120s"])
    control = json.loads(command(["kubectl", "get", "pods", "-n", "openaev-control", "-l", "app=control", "-o", "json"]))
    control_ip = control["items"][0]["status"]["podIP"]
    STAGE = "enforce-node-fence"
    # Fence only the two lab namespaces, using the policy controller's live
    # source sets for their all-pod DNS rules. A blanket PodCIDR block would
    # break CoreDNS's API watch. Controller-managed sets also track lifecycle
    # pods and restarts without stale per-pod IP exceptions.
    deadline = time.monotonic() + 60
    source_sets: dict[str, str] = {}
    while time.monotonic() < deadline:
        rules = command(["docker", "exec", node, "iptables-save"]).decode()
        for namespace in ("openaev-lab", "hikyo-lab"):
            matches = {match.group(1) for line in rules.splitlines()
                       if "policy name dns-only namespace " + namespace + '"' in line
                       for match in [re.search(r"--match-set (KUBE-SRC-[A-Z0-9]+) src", line)]
                       if match is not None}
            if len(matches) == 1:
                source_sets[namespace] = matches.pop()
        if len(source_sets) == 2:
            break
        time.sleep(1)
    if len(source_sets) != 2:
        raise RuntimeError("lab_policy_source_sets_missing")
    for source_set in source_sets.values():
        command(["docker", "exec", node, "iptables", "-I", "INPUT", "1",
                 "-m", "set", "--match-set", source_set, "src", "-j", "REJECT"])
    command(["docker", "exec", node, "ip", "address", "add", "169.254.169.254/32", "dev", "lo"])
    canary = state / "node-canary"
    command(["go", "build", "-trimpath", "-o", str(canary), str(HERE / "node_canary.go")],
            environment=dict(os.environ, GOOS="linux", GOARCH="arm64", CGO_ENABLED="0"))
    command(["docker", "cp", str(canary), node + ":/tmp/node-canary"])
    command(["docker", "exec", "--detach", node, "/tmp/node-canary"])
    apply([{"apiVersion": "v1", "kind": "Service", "metadata": {"name": "hikyo", "namespace": "hikyo-lab"},
            "spec": {"type": "ClusterIP", "selector": {"app": "hikyo-lab"}, "ports": [{
                "name": "https", "port": 8443, "targetPort": "https"}]}}])
    workloads = list(yaml.safe_load_all((HERE / "workloads.yaml").read_text().replace("INJECTOR_IMAGE", injector)))
    for document in workloads:
        if document["kind"] in ("Deployment", "StatefulSet"):
            for container in document["spec"]["template"]["spec"].get("containers", []) + document["spec"]["template"]["spec"].get("initContainers", []):
                container["imagePullPolicy"] = "Never"
    start_workloads(workloads)
    command(["kubectl", "rollout", "status", "deployment/canary", "-n", "openaev-forbidden", "--timeout=120s"])
    # Preserve cold-start/restart evidence even when all services become ready.
    from diagnostics import collect
    print(json.dumps(collect(state, "wait-deployment-openaev"), sort_keys=True), flush=True)
    STAGE = "verify-rabbitmq-remount"
    # Exercise RabbitMQ remount lifecycle: fsGroup can re-add group access at mount,
    # so the owner-only repair must survive the next container start too.
    rabbit_uid = json.loads(command(["kubectl", "get", "pod", "rabbitmq-0", "-n", "openaev-lab", "-o", "json"]))["metadata"]["uid"]
    command(["kubectl", "delete", "pod", "rabbitmq-0", "-n", "openaev-lab", "--wait=true"])
    wait_for_rabbitmq_remount()
    rabbit = json.loads(command(["kubectl", "get", "pod", "rabbitmq-0", "-n", "openaev-lab", "-o", "json"]))
    mode = command(["kubectl", "exec", "-n", "openaev-lab", "rabbitmq-0", "--", "stat", "-c", "%u:%g:%a", "/var/lib/rabbitmq/.erlang.cookie"]).decode().strip()
    if rabbit["metadata"]["uid"] == rabbit_uid or mode != "999:999:600":
        raise RuntimeError("rabbitmq_cookie_remount_verification_failed")
    node_info = json.loads(command(["kubectl", "get", "node", node, "-o", "json"]))
    node_ip = next(address["address"] for address in node_info["status"]["addresses"] if address["type"] == "InternalIP")
    cluster_info = {"name": cluster, "node": node, "node_ip": node_ip, "injector_image": injector,
                    "candidate_image": args.image, "control_ip": control_ip,
                    "metadata_canary": "169.254.169.254", "rabbitmq_cookie_remount": True}
    (state / "cluster.json").write_text(json.dumps(cluster_info, indent=2) + "\n")
    environment = {"KUBECONFIG": str(state / "kubeconfig"), "OPENAEV_KUBE_CONTEXT": config["current-context"], "OPENAEV_CA_FILE": str(state / "ca.crt"),
                   "OPENAEV_STACK_STATE": str(state), "OPENAEV_CLUSTER_FILE": str(state / "cluster.json"),
                   "OPENAEV_PLATFORM_TOKEN_FILE": str(state / "openaev-token"), "OPENAEV_PLATFORM_ORIGIN": "https://127.0.0.1:18080",
                   "OPENAEV_HIKYO_ORIGIN": "https://127.0.0.1:18081"}
    (state / "environment.sh").write_text("\n".join("export " + key + "=" + shlex.quote(value) for key, value in environment.items()) + "\n")
    print("Fresh hosted lab ready; custody remains in owner-only runner state", flush=True)


SETUP_ERRORS = (RuntimeError, OSError, ValueError, TypeError, KeyError, IndexError, StopIteration, yaml.YAMLError)
SETUP_CODES = frozenset({
    "invalid_stack_workload_name", "rabbitmq_remount_wait_expired", "stack_requires_arm64_linux_runner",
    "k3d_binary_checksum_mismatch", "immutable_hikyo_image_required", "stack_requires_arm64_linux_docker",
    "lab_policy_source_sets_missing", "rabbitmq_cookie_remount_verification_failed",
})
SETUP_TOOLS = frozenset({"docker", "kubectl", "openssl", "curl", "go", "k3d"})
SETUP_CLASSES = frozenset({"RuntimeError", "OSError", "FileNotFoundError", "FileExistsError", "PermissionError",
    "ValueError", "JSONDecodeError", "TypeError", "KeyError", "IndexError", "StopIteration", "YAMLError",
    "ParserError", "ScannerError", "ReaderError", "ComposerError", "ConstructorError"})


def safe_setup_error(error: BaseException, stage: str, stages: frozenset[str]) -> str:
    stage = stage if stage in stages else "unknown"
    text = str(error)
    if isinstance(error, RuntimeError):
        if text in SETUP_CODES:
            return text
        if text.startswith("stack_tool_missing:") and text.removeprefix("stack_tool_missing:") in SETUP_TOOLS:
            return text
        parts = text.split(":")
        if len(parts) == 3 and parts[0] == "stack_command_failed" and parts[1] in stages and parts[2] in SETUP_TOOLS:
            return text
    name = type(error).__name__
    return "stack_setup_failed:" + stage + ":" + (name if name in SETUP_CLASSES else "other")


def entrypoint() -> int:
    try:
        main()
        return 0
    except SETUP_ERRORS as error:
        from diagnostics import STAGES, collect
        if STATE is not None:
            try:
                print(json.dumps(collect(STATE, STAGE), sort_keys=True), file=sys.stderr)
            except SETUP_ERRORS:
                print("stack_startup_diagnostics_unavailable", file=sys.stderr)
        print(safe_setup_error(error, STAGE, STAGES), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(entrypoint())

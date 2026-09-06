#!/usr/bin/env python3
"""Exercise actual Hikyo configuration reloads in an existing owned kind cluster.

Requires a Linux candidate binary and its matching signed chartfixture bundle.
Creates only uniquely named fixture resources; never creates/deletes a cluster.
Credentials and command output remain in the private work directory. Evidence
contains process/resource identities and authorization outcomes, never secret values.
"""

import argparse
import base64
import copy
from datetime import datetime, timezone
import hashlib
import hmac
import json
import os
from pathlib import Path
import re
import secrets
import select
import shlex
import shutil
import socket
import struct
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request


class IsolatedDatabaseForward:
    """One opaque TCP stream per kubectl process; never replay a connection.

    Some local kind/container runtimes end the entire port-forward after a
    PostgreSQL TLS close resets one stream. Separate processes keep that close
    local to its connection. TLS still terminates only at client/PostgreSQL.
    """

    def __init__(self, context, namespace, pod, port, log):
        self.command = ["kubectl", "--context", context, "-n", namespace,
                        "port-forward", "--address=127.0.0.1", "pod/" + pod, "0:5432"]
        self.log = log
        self.stop = threading.Event()
        self.lock = threading.Lock()
        self.slots = threading.BoundedSemaphore(8)
        self.children = set()
        self.sockets = set()
        self.workers = []
        self.accepted = 0
        self.errors = []
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", port))
        self.listener.listen(8)
        self.listener.settimeout(0.5)
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()

    def accept(self):
        deadline = time.monotonic() + 600
        while not self.stop.is_set() and time.monotonic() < deadline:
            try:
                client, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            if self.accepted >= 64 or not self.slots.acquire(blocking=False):
                client.close()
                self.errors.append("connection limit exceeded")
                continue
            self.accepted += 1
            worker = threading.Thread(target=self.connection, args=(client,), daemon=True)
            self.workers.append(worker)
            worker.start()
        self.listener.close()

    def connection(self, client):
        child = None
        upstream = None
        deadline = time.monotonic() + 180
        try:
            child = subprocess.Popen(self.command, stdout=subprocess.PIPE, stderr=self.log)
            with self.lock:
                self.children.add(child)
                self.sockets.add(client)
            output = b""
            ready_deadline = time.monotonic() + 10
            while time.monotonic() < ready_deadline and not self.stop.is_set():
                if child.poll() is not None:
                    raise RuntimeError("port-forward exited before listening")
                readable, _, _ = select.select([child.stdout], [], [], 0.2)
                if readable:
                    output += os.read(child.stdout.fileno(), 4096)
                    match = re.search(rb"Forwarding from 127\.0\.0\.1:(\d+) -> 5432", output)
                    if match:
                        upstream = socket.create_connection(("127.0.0.1", int(match[1])), timeout=5)
                        break
                    if len(output) > 16384:
                        raise RuntimeError("unexpected port-forward output")
            if upstream is None:
                raise RuntimeError("port-forward did not listen before deadline")
            with self.lock:
                self.sockets.add(upstream)
            client.settimeout(180)
            upstream.settimeout(180)
            # Blocking sendall provides backpressure. Each EOF half-closes the
            # opposite socket; the other direction can still drain its reply.
            def pump(source, destination):
                try:
                    while not self.stop.is_set() and time.monotonic() < deadline:
                        readable, _, _ = select.select([source], [], [], 0.5)
                        if not readable:
                            continue
                        data = source.recv(65536)
                        if not data:
                            destination.shutdown(socket.SHUT_WR)
                            return
                        destination.sendall(data)
                except OSError:
                    # Reset/EOF belongs to this stream, never another client.
                    pass
                finally:
                    try:
                        destination.shutdown(socket.SHUT_WR)
                    except OSError:
                        pass
            pumps = [threading.Thread(target=pump, args=pair, daemon=True)
                     for pair in ((client, upstream), (upstream, client))]
            for thread in pumps:
                thread.start()
            for thread in pumps:
                thread.join(max(0, deadline - time.monotonic()))
            if any(thread.is_alive() for thread in pumps):
                self.errors.append("connection lifetime exceeded")
        except (OSError, RuntimeError) as error:
            self.errors.append(type(error).__name__)
        finally:
            for connection in (client, upstream):
                if connection is not None:
                    connection.close()
            if child is not None:
                if child.poll() is None:
                    child.terminate()
                try:
                    child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait(timeout=5)
                child.stdout.close()
            with self.lock:
                self.children.discard(child)
                self.sockets.discard(client)
                self.sockets.discard(upstream)
            self.slots.release()

    def terminate(self):
        self.stop.set()
        self.listener.close()
        with self.lock:
            for connection in self.sockets:
                try:
                    connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
            for child in self.children:
                if child.poll() is None:
                    child.terminate()

    def wait(self, timeout=10):
        deadline = time.monotonic() + timeout
        self.thread.join(max(0, deadline - time.monotonic()))
        for thread in self.workers:
            thread.join(max(0, deadline - time.monotonic()))
        if any(thread.is_alive() for thread in [self.thread, *self.workers]):
            raise RuntimeError("database relay did not clean up within deadline")


# -trimpath removes linker flags from Go build metadata. Read the actual Go
# string symbols with the standard executable readers instead of inferring
# embedded trust from absent metadata or coincidental substring matches.
LINKED_STRINGS_READER = r'''
package main
import ("debug/elf"; "debug/macho"; "encoding/binary"; "encoding/json"; "fmt"; "os")
type section struct { address uint64; data []byte }
func main() {
    symbols := map[string]uint64{}
    sections := []section{}
    var order binary.ByteOrder
    if file, err := elf.Open(os.Args[1]); err == nil {
        defer file.Close(); order = file.ByteOrder
        table, err := file.Symbols(); if err != nil { panic(err) }
        for _, symbol := range table { symbols[symbol.Name] = symbol.Value }
        for _, s := range file.Sections {
            if s.Type == elf.SHT_NOBITS { continue }
            if data, err := s.Data(); err == nil { sections = append(sections, section{s.Addr, data}) }
        }
    } else if file, err := macho.Open(os.Args[1]); err == nil {
        defer file.Close(); order = file.ByteOrder
        if file.Symtab == nil { panic("candidate must retain Go string symbols") }
        for _, symbol := range file.Symtab.Syms { symbols[symbol.Name] = symbol.Value }
        for _, s := range file.Sections {
            if data, err := s.Data(); err == nil { sections = append(sections, section{s.Addr, data}) }
        }
    } else { panic("candidate must be a supported ELF or Mach-O executable") }
    read := func(address, size uint64) []byte {
        if size > 4 << 20 { panic("oversized linked string") }
        for _, s := range sections {
            if address >= s.address && address - s.address <= uint64(len(s.data)) {
                offset := address - s.address
                if size <= uint64(len(s.data)) - offset { return s.data[offset:offset+size] }
            }
        }
        panic("linked string points outside executable sections")
    }
    values := map[string]string{}
    for _, name := range os.Args[2:] {
        address, exists := symbols[name]; if !exists { panic(fmt.Sprintf("missing linked symbol %s", name)) }
        header := read(address, 16)
        values[name] = string(read(order.Uint64(header[:8]), order.Uint64(header[8:])))
    }
    if err := json.NewEncoder(os.Stdout).Encode(values); err != nil { panic(err) }
}
'''


class Fixture:
    def __init__(self, args):
        self.args = args
        if not re.fullmatch(r"kind-[a-z0-9][a-z0-9-]*", args.context):
            raise RuntimeError("an explicitly named local kind context is required")
        self.cluster = args.context.removeprefix("kind-")
        self.work = Path(args.resume) if args.resume else Path(tempfile.mkdtemp(prefix="hikyo-rollout-kind-"))
        self.work.chmod(0o700)
        self.ns = args.namespace if args.resume else "hikyo-rollout-" + secrets.token_hex(4)
        if not re.fullmatch(r"hikyo-rollout-[a-f0-9]{8}", self.ns):
            raise RuntimeError("fixture namespace is not an owned unique name")
        self.registry = self.ns + "-registry"
        self.origin = "http://127.0.0.1:" + str(args.port)
        self.forward = None
        self.database_forward = None
        self.token = ""
        self.last_step = 0
        self.evidence = {"context": args.context, "namespace": self.ns,
                         "binary_sha256": hashlib.sha256(Path(args.binary).read_bytes()).hexdigest()}
        self.chart_inputs = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                             for path in sorted(Path("chart/hikyo").rglob("*")) if path.is_file()}
        self.evidence["chart_inputs_sha256"] = self.chart_inputs
        if args.resume and (self.work / "evidence.json").exists():
            retained = json.loads((self.work / "evidence.json").read_text())
            if retained.get("binary_sha256") != self.evidence["binary_sha256"]:
                raise RuntimeError("candidate binary differs from retained fixture evidence")
            if args.expanded and "chart_inputs_sha256" not in retained:
                raise RuntimeError("expanded resume requires an explicit retained chart provenance record")
            if args.expanded and retained.get("operator_binary_sha256") != hashlib.sha256(Path(args.operator_binary).read_bytes()).hexdigest():
                raise RuntimeError("native operator executable differs from retained expanded evidence")
            if retained.get("chart_inputs_sha256", self.chart_inputs) != self.chart_inputs:
                raise RuntimeError("chart inputs differ from retained fixture evidence")
            self.evidence.update(retained)
        self.log = (self.work / "commands.log").open("ab")
        self.validate_cluster()
        if args.source_manifest:
            self.validate_source_manifest()
        print(f"rollout-kind: private custody {self.work}", flush=True)

    def run(self, *args, data=None, timeout=180, env=None):
        result = subprocess.run(args, input=data, stdout=subprocess.PIPE,
                                stderr=self.log, timeout=timeout, check=False, env=env)
        if result.returncode:
            raise RuntimeError(f"{args[0]} failed; inspect private commands.log")
        return result.stdout

    def kube(self, *args, **kwargs):
        return self.run("kubectl", "--context", self.args.context,
                        "--namespace", self.ns, *args, **kwargs)

    def apply(self, obj):
        self.kube("apply", "-f", "-", data=json.dumps(obj).encode())

    def get(self, kind, name):
        return json.loads(self.kube("get", kind, name, "-o", "json"))

    def private(self, name, value):
        path = self.work / name
        path.write_bytes(value)
        path.chmod(0o600)
        return path

    def secret(self, name, values, immutable=True):
        self.apply({"apiVersion": "v1", "kind": "Secret",
                    "metadata": {"name": name, "namespace": self.ns},
                    "immutable": immutable,
                    "data": {k: base64.b64encode(v).decode() for k, v in values.items()}})

    def validate_cluster(self):
        clusters = self.run("kind", "get", "clusters").decode().splitlines()
        if self.cluster not in clusters:
            raise RuntimeError("the explicitly named kind cluster is absent")
        expected_path = self.private("expected-kubeconfig", self.run("kind", "get", "kubeconfig", "--name", self.cluster))
        expected = json.loads(self.run("kubectl", "--kubeconfig", str(expected_path), "config", "view", "--raw", "--minify", "-o", "json"))
        actual = json.loads(self.run("kubectl", "--context", self.args.context, "config", "view", "--raw", "--minify", "-o", "json"))
        wanted = expected["clusters"][0]["cluster"]
        selected = actual["clusters"][0]["cluster"]
        endpoint = urllib.parse.urlparse(wanted["server"])
        if endpoint.scheme != "https" or endpoint.hostname not in ("127.0.0.1", "localhost", "::1"):
            raise RuntimeError("kind fixture requires a loopback Kubernetes endpoint")
        if any(selected.get(key) != wanted.get(key) for key in ("server", "certificate-authority-data")):
            raise RuntimeError("selected context does not match the local kind cluster")
        nodes = self.run("kind", "get", "nodes", "--name", self.cluster).decode().splitlines()
        if len(nodes) != 1:
            raise RuntimeError("rollout fixture requires a dedicated single-node kind cluster")
        self.node = nodes[0]
        container = json.loads(self.run("docker", "inspect", self.node))[0]
        if container["Config"]["Labels"].get("io.x-k8s.kind.cluster") != self.cluster:
            raise RuntimeError("kind node Docker ownership differs from selected cluster")
        node = self.get("node", self.node)
        self.arch = node["status"]["nodeInfo"]["architecture"]
        self.evidence["kubernetes_version"] = node["status"]["nodeInfo"]["kubeletVersion"]
        if self.arch not in ("amd64", "arm64"):
            raise RuntimeError("fixture supports amd64 and arm64 kind nodes")
        binary = self.run("go", "version", "-m", self.args.binary).decode()
        if "GOOS=linux" not in binary or f"GOARCH={self.arch}" not in binary:
            raise RuntimeError("candidate binary does not match the Linux kind node architecture")
        if self.args.operator_binary:
            self.validate_linked_trust()
            self.evidence["operator_binary_sha256"] = hashlib.sha256(Path(self.args.operator_binary).read_bytes()).hexdigest()
        if self.args.resume and (self.evidence.get("context") != self.args.context or self.evidence.get("namespace") != self.ns):
            raise RuntimeError("retained evidence does not match selected fixture ownership")

    def validate_linked_trust(self):
        flags = (Path(self.args.public_dir).parent / "ldflags").read_bytes()
        words = shlex.split(flags.decode())
        if len(words) % 2 or any(words[i] != "-X" for i in range(0, len(words), 2)):
            raise RuntimeError("fixture linker input must contain only explicit Go string assignments")
        expected = dict(value.split("=", 1) for value in words[1::2])
        required = {"main.version", "main.commit", "main.updateChannel"} | {
            "github.com/Hikyo-Org/hikyo/internal/buildcompat." + name for name in
            ("encodedTrustRoot", "encodedRecoveryPublicKey", "encodedDeclaration", "declarationSHA256")}
        if set(expected) != required:
            raise RuntimeError("fixture linker input lacks the complete signed build binding")
        reader = self.private("linked-strings-reader.go", LINKED_STRINGS_READER.encode())
        for executable in (self.args.binary, self.args.operator_binary):
            actual = json.loads(self.run("go", "run", "-p", "2", str(reader), executable, *sorted(expected),
                                         env={**os.environ, "GOMAXPROCS": "2"}))
            if actual != expected:
                raise RuntimeError("actual candidate linked strings differ from the exact signed fixture input")
        self.evidence["signed_linker_input_sha256"] = hashlib.sha256(flags).hexdigest()
        self.evidence["linked_string_symbols_verified"] = sorted(expected)

    def validate_source_manifest(self):
        raw = Path(self.args.source_manifest).read_bytes()
        manifest = json.loads(raw)
        if not isinstance(manifest, dict) or not manifest:
            raise RuntimeError("source manifest must map compiled source/embed paths to SHA-256")
        for name, digest in manifest.items():
            path = Path(name)
            if path.is_absolute() or ".." in path.parts or not isinstance(digest, str) or not re.fullmatch("[0-9a-f]{64}", digest):
                raise RuntimeError("invalid source manifest entry")
            if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                raise RuntimeError("compiled source manifest differs at " + name)
        self.evidence["compiled_source_manifest_sha256"] = hashlib.sha256(raw).hexdigest()
        self.evidence["compiled_source_files_checked"] = len(manifest)

    def finish_evidence(self):
        if self.args.source_manifest:
            self.validate_source_manifest()
        if self.args.operator_binary and hashlib.sha256(Path(self.args.operator_binary).read_bytes()).hexdigest() != self.evidence["operator_binary_sha256"]:
            raise RuntimeError("native operator executable changed during its actual proof run")
        current_chart = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                         for path in sorted(Path("chart/hikyo").rglob("*")) if path.is_file()}
        if current_chart != self.chart_inputs:
            raise RuntimeError("chart inputs changed during the actual proof run")
        process = self.process()
        actual = self.run("docker", "exec", self.node, "sha256sum", f"/proc/{process['host_pid']}/exe").decode().split()[0]
        if actual != self.evidence["binary_sha256"]:
            raise RuntimeError("actual running server ELF differs from candidate binary")
        self.evidence["actual_running_elf_sha256"] = actual
        self.evidence["observed_at"] = datetime.now(timezone.utc).isoformat()
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: PASS; actual running ELF and supplied compiled-source manifest verified", flush=True)

    def setup(self):
        if shutil.disk_usage(self.work).free < 4 * 1024**3:
            raise RuntimeError("fixture requires at least 4 GiB free disk")
        self.kube("create", "namespace", self.ns)
        self.kube("label", "namespace", self.ns, "hikyo.dev/test-fixture=config-rollout-kind")
        self.run("docker", "run", "-d", "--name", self.registry,
                 "--label", "hikyo.dev/test-fixture=config-rollout-kind", "-p",
                 f"127.0.0.1:{self.args.registry_port}:5000",
                 "registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373")
        self.run("docker", "network", "connect", "kind", self.registry)
        registry_dir = f"/etc/containerd/certs.d/localhost:{self.args.registry_port}"
        self.run("docker", "exec", self.node, "mkdir", "-p", registry_dir)
        self.run("docker", "exec", "-i", self.node, "sh", "-c",
                 f"cat > {registry_dir}/hosts.toml",
                 data=f'[host."http://{self.registry}:5000"]\n  capabilities = ["pull", "resolve"]\n'.encode())
        build = self.work / "build"
        target = build / "image-root" / self.arch
        target.mkdir(parents=True)
        shutil.copy2(self.args.binary, target / "hikyo")
        shutil.copy2("Dockerfile.release", build / "Dockerfile.release")
        repository = f"localhost:{self.args.registry_port}/{self.ns}"
        self.run("docker", "buildx", "build", "--load", "--platform", "linux/" + self.arch,
                 "--build-arg", "TARGETARCH=" + self.arch, "-t", repository + ":proof",
                 "-f", str(build / "Dockerfile.release"), str(build), timeout=300)
        self.run("docker", "push", repository + ":proof", timeout=180)
        refs = json.loads(self.run("docker", "image", "inspect", repository + ":proof"))[0]["RepoDigests"]
        digest = next(ref.split("@", 1)[1] for ref in refs if ref.startswith(repository + "@"))
        self.evidence["image"] = repository + "@" + digest
        self.evidence["binary_sha256"] = hashlib.sha256(Path(self.args.binary).read_bytes()).hexdigest()
        node_base = "/var/lib/" + self.ns
        self.run("docker", "exec", self.node, "mkdir", "-p", node_base + "/public", node_base + "/state/operator-custody")
        self.run("docker", "cp", str(Path(self.args.public_dir)) + "/.", self.node + ":" + node_base + "/public/")
        if self.args.expanded:
            operator_env = {**os.environ, "COSIGN_PASSWORD": ""}
            self.run(self.args.cosign, "generate-key-pair", "--output-key-prefix", str(self.work / "operator"), env=operator_env)
            (self.work / "operator.key").chmod(0o600)
            self.run("docker", "cp", str(self.work / "operator.pub"), self.node + ":" + node_base + "/public/operator.pub")
            self.run("docker", "exec", self.node, "chmod", "0644", node_base + "/public/operator.pub")
            self.run("docker", "exec", self.node, "mkdir", "-p", node_base + "/public/next")
            self.run("docker", "exec", self.node, "cp", "-R", node_base + "/public/bundle", node_base + "/public/next/bundle")
            self.run("docker", "exec", self.node, "cp", node_base + "/public/operator.pub", node_base + "/public/next/operator.pub")
        self.run("docker", "exec", self.node, "chown", "-R", "65532:65532", node_base + "/state")
        self.run("docker", "exec", self.node, "chmod", "2770", node_base + "/state")
        self.run("docker", "exec", self.node, "chmod", "0700", node_base + "/state/operator-custody")
        self.node_base = node_base
        for name in ("public", "state", "postgres"):
            self.apply({"apiVersion": "v1", "kind": "PersistentVolume",
                        "metadata": {"name": self.ns + "-" + name},
                        "spec": {"capacity": {"storage": "1Gi"}, "accessModes": ["ReadWriteOnce"],
                                 "persistentVolumeReclaimPolicy": "Retain", "storageClassName": self.ns,
                                 "hostPath": {"path": node_base + "/" + name, "type": "DirectoryOrCreate"}}})
            self.apply({"apiVersion": "v1", "kind": "PersistentVolumeClaim",
                        "metadata": {"name": name, "namespace": self.ns},
                        "spec": {"accessModes": ["ReadWriteOnce"], "storageClassName": self.ns,
                                 "volumeName": self.ns + "-" + name, "resources": {"requests": {"storage": "1Gi"}}}})
        self.setup_sources()
        self.values = {"operator": {"enabled": False},
                       "image": {"repository": repository, "digest": digest},
                       "database": {"existingSecret": "database-primary", "tls": {"existingSecret": "database-ca"}},
                       "rootKey": {"existingSecret": "root-primary"},
                       "upgrade": {"existingClaim": "public", "stateExistingClaim": "state"},
                       "externalOrigin": self.origin,
                       "network": {"allowPlaintextOrigin": True, "trustedProxyCIDRs": ["10.0.0.0/8"]},
                       "updates": {"channel": "stable"}, "rollout": {"enabled": True}}
        if self.args.expanded:
            index = json.loads((Path(self.args.public_dir) / "bundle/index.json").read_text())
            if len(index["releases"]) != 1:
                raise RuntimeError("expanded fixture requires a single authenticated candidate manifest")
            initial = {"bundle_directory": "/run/hikyo-upgrade/bundle",
                       "state_directory": "/var/lib/hikyo-upgrade/operator-custody",
                       "evidence_directory": "", "ciphertext_path": "",
                       "operator_public_key_file": "/run/hikyo-upgrade/operator.pub",
                       "target_manifest_sha256": "", "legacy_writers_stopped": False}
            selected = {"bundle_directory": "/run/hikyo-upgrade/next/bundle",
                        "state_directory": "/var/lib/hikyo-upgrade/aliases/next",
                        "evidence_directory": "/run/hikyo-upgrade/next/evidence",
                        "ciphertext_path": "/run/hikyo-upgrade/next/backup.age",
                        "operator_public_key_file": "/run/hikyo-upgrade/next/operator.pub",
                        "target_manifest_sha256": index["releases"][0]["manifest_sha256"],
                        "legacy_writers_stopped": True}
            self.values["rollout"].update({"topologyNodeIDs": ["hikyo-hikyo-server", "replacement"],
                "upgradeSources": {"initial": initial, "next": selected},
                "initialUpgradeSource": "initial", "upgradeStateAliases": ["next"]})
        self.helm()
        self.kube("rollout", "status", "deployment/hikyo-hikyo", "--timeout=180s")
        self.enroll()

    def enroll(self):
        if self.values["rollout"].get("enrolled"):
            # Reusing a prepared fixture must not reapply Helm's initial source
            # selectors over a deployment that the executor already changed.
            self.start_forward()
            return
        identity = self.sql("SELECT instance_id || '|' || incarnation FROM upgrade_control WHERE singleton=1;").split("|")
        rollout = self.values["rollout"]
        rollout.update({"enrolled": True, "enrollmentID": self.ns,
                        "ownerInstanceID": identity[0], "incarnation": identity[1],
                        "deploymentUID": self.get("deployment", "hikyo-hikyo")["metadata"]["uid"],
                        "authorityPublicKey": (self.work / "authority.pub").read_text(),
                        "authorityExistingSecret": "rollout-authority",
                        "initialDatabaseSource": "primary", "initialRootSource": "primary",
                        "databaseSources": {alias: {"name": "database-" + alias, "key": "HIKYO_DB"} for alias in ("primary", "next")},
                        "rootSources": {alias: {"name": "root-" + alias, "key": "root-key"} for alias in ("primary", "next")}})
        for suffix in ("command", "response", "journal"):
            rollout[suffix + "SecretUID"] = self.get("secret", "hikyo-hikyo-rollout-" + suffix)["metadata"]["uid"]
        rollout["leaseUID"] = self.get("lease", "hikyo-hikyo-rollout")["metadata"]["uid"]
        self.helm()
        self.kube("rollout", "status", "deployment/hikyo-hikyo", "--timeout=180s")
        self.kube("wait", "--for=condition=Ready", "pod/hikyo-hikyo-rollout-0", "--timeout=180s")
        self.evidence["owner_instance_id"], self.evidence["incarnation"] = identity
        self.start_forward()
        print("rollout-kind: signed candidate and enrolled fixed-target executor are ready", flush=True)

    def setup_sources(self):
        def openssl(*args):
            self.run("openssl", *args)
        ca = str(self.work / "ca")
        tls = str(self.work / "tls")
        openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-keyout", ca + ".key", "-out", ca + ".crt", "-subj", "/CN=Hikyo rollout fixture")
        openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", tls + ".key", "-out", tls + ".csr", "-subj", f"/CN=postgres.{self.ns}.svc")
        ext = self.private("tls.ext", f"subjectAltName=DNS:postgres.{self.ns}.svc,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n".encode())
        openssl("x509", "-req", "-days", "1", "-sha256", "-in", tls + ".csr", "-CA", ca + ".crt", "-CAkey", ca + ".key", "-CAcreateserial", "-extfile", str(ext), "-out", tls + ".crt")
        password = secrets.token_hex(24)
        self.secret("postgres-auth", {"password": password.encode()})
        self.secret("postgres-tls", {"tls.crt": Path(tls + ".crt").read_bytes(), "tls.key": Path(tls + ".key").read_bytes()})
        self.secret("database-ca", {"ca.crt": Path(ca + ".crt").read_bytes()})
        dsn = f"postgres://hikyo:{password}@postgres.{self.ns}.svc:5432/hikyo?sslmode=verify-full&sslrootcert=/run/hikyo-database-ca/ca.crt"
        for alias in ("primary", "next"):
            self.secret("database-" + alias, {"HIKYO_DB": (dsn + "&application_name=" + alias).encode()})
            self.secret("root-" + alias, {"root-key": secrets.token_hex(32).encode()})
        openssl("genpkey", "-algorithm", "ED25519", "-out", str(self.work / "authority.key"))
        openssl("pkey", "-in", str(self.work / "authority.key"), "-pubout", "-out", str(self.work / "authority.pub"))
        self.secret("rollout-authority", {"authority.key": (self.work / "authority.key").read_bytes()})
        container = {"name": "postgres", "image": "postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941",
                     "args": ["-c", "ssl=on", "-c", "ssl_cert_file=/tls/tls.crt", "-c", "ssl_key_file=/tls/tls.key"],
                     "env": [{"name": "POSTGRES_USER", "value": "hikyo"}, {"name": "POSTGRES_DB", "value": "hikyo"},
                             {"name": "POSTGRES_PASSWORD", "valueFrom": {"secretKeyRef": {"name": "postgres-auth", "key": "password"}}}],
                     "ports": [{"name": "postgres", "containerPort": 5432}],
                     "readinessProbe": {"exec": {"command": ["pg_isready", "-U", "hikyo", "-d", "hikyo"]}, "periodSeconds": 2},
                     "volumeMounts": [{"name": "data", "mountPath": "/var/lib/postgresql"}, {"name": "tls", "mountPath": "/tls", "readOnly": True}]}
        self.apply({"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": "postgres", "namespace": self.ns},
                    "spec": {"replicas": 1, "selector": {"matchLabels": {"app": "postgres"}},
                             "template": {"metadata": {"labels": {"app": "postgres"}}, "spec": {
                                 "securityContext": {"fsGroup": 999}, "containers": [container],
                                 "volumes": [{"name": "data", "persistentVolumeClaim": {"claimName": "postgres"}},
                                             {"name": "tls", "secret": {"secretName": "postgres-tls", "defaultMode": 0o440}}]}}}})
        self.apply({"apiVersion": "v1", "kind": "Service", "metadata": {"name": "postgres", "namespace": self.ns},
                    "spec": {"selector": {"app": "postgres"}, "ports": [{"port": 5432, "targetPort": 5432}]}})
        self.kube("rollout", "status", "deployment/postgres", "--timeout=180s")

    def helm(self):
        path = self.private("values.json", json.dumps(self.values).encode())
        self.run("helm", "upgrade", "--install", "hikyo", "chart/hikyo", "--kube-context", self.args.context,
                 "--namespace", self.ns, "-f", str(path), timeout=180)

    def sql(self, query):
        return self.kube("exec", "deployment/postgres", "--", "psql", "-U", "hikyo", "-d", "hikyo", "-At", "-c", query).decode().strip()

    def start_forward(self):
        if self.forward is not None:
            self.forward.terminate()
            self.forward.wait(timeout=10)
        self.forward = subprocess.Popen(["kubectl", "--context", self.args.context, "-n", self.ns,
                                         "port-forward", "service/hikyo-hikyo", f"{self.args.port}:8080"], stdout=self.log, stderr=self.log)
        time.sleep(2)

    def request(self, method, path, body=None):
        self.last_http_status = None
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.origin + "/api/v1" + path, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=15) as response:
                raw = response.read()
                result = json.loads(raw) if raw else None
                if isinstance(result, dict) and result.get("session_token"):
                    self.token = result["session_token"]
                return result
        except urllib.error.HTTPError as error:
            self.last_http_status = error.code
            self.private("last-http-error.json", error.read())
            raise RuntimeError(f"HTTP {method} {path} returned {error.code}; inspect private last-http-error.json") from None

    def code(self):
        while int(time.time()) // 30 <= self.last_step:
            time.sleep(1)
        self.last_step = int(time.time()) // 30
        digest = hmac.new(self.totp_secret, struct.pack(">Q", self.last_step), hashlib.sha1).digest()
        offset = digest[-1] & 15
        return str((struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7fffffff) % 1000000).zfill(6)

    def login(self):
        self.token = ""
        result = self.request("POST", "/auth/local/login", {"username": "rollout-doctor", "password": self.password})
        self.token = result["session_token"]
        result = self.request("POST", "/auth/totp/step-up", {"code": self.code()})
        self.token = result["session_token"]
        return result

    def admin(self, name, args, expect_success=True):
        template = copy.deepcopy(self.get("deployment", "hikyo-hikyo")["spec"]["template"])
        template["metadata"] = {"labels": {"app": "rollout-doctor"}}
        spec = template["spec"]
        spec["restartPolicy"] = "Never"
        spec.pop("initContainers", None)
        container = next(c for c in spec["containers"] if c["name"] == "server")
        spec["containers"] = [container]
        container["args"] = args
        root_name = next(v for v in spec["volumes"] if v["name"] == "root-key-source")["secret"]["secretName"]
        container["env"].append({"name": "HIKYO_ROOT_KEY", "valueFrom": {"secretKeyRef": {"name": root_name, "key": "root-key"}}})
        for key in ("ports", "livenessProbe", "readinessProbe", "startupProbe"):
            container.pop(key, None)
        self.apply({"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": name, "namespace": self.ns},
                    "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 90, "template": template}})
        deadline = time.monotonic() + 100
        while time.monotonic() < deadline:
            status = self.get("job", name).get("status", {})
            if status.get("succeeded") or status.get("failed"):
                output = self.kube("logs", "job/" + name)
                self.private(name + ".log", output)
                if bool(status.get("succeeded")) != expect_success:
                    raise RuntimeError(f"admin job {name} had unexpected outcome; inspect private log")
                return output.decode()
            time.sleep(1)
        raise RuntimeError("admin job timed out")

    def authenticate(self):
        if (self.work / "totp-uri").exists():
            self.password = (self.work / "password").read_text()
            uri = (self.work / "totp-uri").read_text()
            encoded = urllib.parse.parse_qs(urllib.parse.urlparse(uri).query)["secret"][0]
            self.totp_secret = base64.b32decode(encoded + "=" * (-len(encoded) % 8))
            self.last_step = int(time.time()) // 30
            self.login()
            return
        output = self.admin("rollout-admin-create", ["admin", "create", "--username", "rollout-doctor", "--display-name", "Rollout Doctor",
                                                     "--output-file", "/var/lib/hikyo-upgrade/operator-custody/rollout-authority"])
        match = re.search(r"principal ([^)]+)\)", output)
        if not match:
            raise RuntimeError("admin did not report its principal identity")
        self.admin("rollout-admin-grant", ["admin", "grant", "--principal", match.group(1), "--capability", "instance-config"])
        authority = self.run("docker", "exec", self.node, "cat", self.node_base + "/state/operator-custody/rollout-authority").decode().strip()
        self.password = secrets.token_urlsafe(32)
        self.private("password", self.password.encode())
        self.request("POST", "/auth/credential/establish", {"authority": authority, "password": self.password})
        result = self.request("POST", "/auth/local/login", {"username": "rollout-doctor", "password": self.password})
        self.token = result["session_token"]
        result = self.request("POST", "/auth/totp/enrol/start", {"password": self.password})
        uri = result["otpauth_uri"]
        self.private("totp-uri", uri.encode())
        encoded = urllib.parse.parse_qs(urllib.parse.urlparse(uri).query)["secret"][0]
        self.totp_secret = base64.b32decode(encoded + "=" * (-len(encoded) % 8))
        result = self.request("POST", "/auth/totp/enrol/confirm", {"code": self.code()})
        self.token = result["session_token"]
        result = self.request("POST", "/auth/totp/step-up", {"code": self.code()})
        self.token = result["session_token"]
        status = self.request("GET", "/instance/config")
        if not status["managed"]:
            raise RuntimeError("supported setup failed to create its managed profile")
        self.evidence["initial_status"] = status
        print("rollout-kind: real password/TOTP administrator authenticated protected setup profile", flush=True)

    def process(self):
        pods = json.loads(self.kube("get", "pods", "-l", "app.kubernetes.io/instance=hikyo", "-o", "json"))["items"]
        pod = next(p for p in pods if p["metadata"].get("ownerReferences", [{}])[0].get("kind") == "ReplicaSet" and not p["metadata"].get("deletionTimestamp"))
        state = next(c for c in pod["status"]["containerStatuses"] if c["name"] == "server")
        container = state["containerID"].split("://", 1)[1]
        inspected = json.loads(self.run("docker", "exec", self.node, "crictl", "inspect", container))
        pid = inspected["info"]["pid"]
        stat = self.run("docker", "exec", self.node, "cat", f"/proc/{pid}/stat").decode()
        return {"pod_uid": pod["metadata"]["uid"], "container_id": container, "host_pid": pid,
                "process_start_ticks": stat.rsplit(") ", 1)[1].split()[19], "started_at": state["state"]["running"]["startedAt"]}

    def publish(self, key, value):
        return self.publish_values({key: value})

    def publish_values(self, values):
        status = self.request("GET", "/instance/config")
        binding = status["binding"]
        scope = "/orgs/{org_id}/projects/{project_id}/environments/{environment_id}".format(**binding)
        versions = [self.request("PUT", scope + "/values/" + key, {"value": value})["version_id"]
                    for key, value in values.items()]
        self.request("POST", scope + "/publish", {"version_ids": versions})
        return self.request("GET", "/instance/config")

    def managed_value(self, key):
        status = self.request("GET", "/instance/config")
        scope = "/orgs/{org_id}/projects/{project_id}/environments/{environment_id}".format(**status["binding"])
        cell = self.request("GET", scope + "/values/" + key)
        if cell["set"] and not cell["revealed"] and cell["classification"] == "secret":
            window = self.request("GET", scope + "/reveal-window")
            if not window["can_reveal"]:
                raise RuntimeError("fixture administrator lacks authorized managed-secret disclosure")
            if not window["live"] or window["single_decision"]:
                if not window["totp_offered"]:
                    raise RuntimeError("managed-secret disclosure requires a passkey in this environment")
                self.request("POST", "/auth/reauth/totp", {"environment_id": status["binding"]["environment_id"], "code": self.code()})
            cell = self.request("POST", scope + "/values/" + key + "/reveal")
        if not cell["set"] or not cell["revealed"] or "value" not in cell:
            raise RuntimeError("managed value was not present and authorized for disclosure")
        return cell["value"]

    def apply_config(self, status, bootstrap=False, hold_observation=False):
        body = {"revision": status["latest_revision"], "schema_version": status["binding"]["schema_version"],
                "expected_generation": status["generation"], "idempotency_key": secrets.token_hex(16),
                "confirm_restored_credentials": False}
        digest = None
        if bootstrap:
            self.request("POST", "/instance/config/apply", {**body, "prepare_only": True})
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                prepared = self.request("GET", "/instance/config")
                if prepared.get("job", {}).get("prepared"):
                    digest = prepared["job"]["plan_digest"]
                    break
                time.sleep(1)
            if not digest:
                raise RuntimeError("executor did not return a prepared exact plan")
            body["plan_digest"] = digest
            if hold_observation:
                self.hold_observation()
        intent = {"action": "apply", "owner_instance_id": status["owner_instance_id"],
                  "revision": body["revision"], "schema_version": body["schema_version"],
                  "expected_generation": body["expected_generation"], "preview_token": "", "to": "",
                  "confirm_restored_credentials": False}
        if digest:
            intent["plan_digest"] = digest
        ceremony = self.request("POST", "/auth/reauth/totp", {"purpose": "self-config", "self_config": intent, "code": self.code()})
        self.last_apply_session_id = ceremony["session_id"]
        result = self.request("POST", "/instance/config/apply", body)
        self.private("last-apply.json", json.dumps(result).encode())
        return body

    def wait_applied(self, body):
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            try:
                status = self.request("GET", "/instance/config")
                self.private("last-status.json", json.dumps(status).encode())
                job = status.get("job", {})
                if job.get("state") == "completed" and job.get("revision") == body["revision"]:
                    return status
            except (OSError, RuntimeError):
                if self.forward.poll() is not None:
                    self.start_forward()
            time.sleep(1)
        raise RuntimeError("actual application did not acknowledge the rollout; inspect private last-status.json")

    def exercise(self):
        before = self.process()
        status = self.request("GET", "/instance/config")
        scope = "/orgs/{org_id}/projects/{project_id}/environments/{environment_id}".format(**status["binding"])
        channel_before = self.request("GET", scope + "/values/HIKYO_UPDATE_CHANNEL")["value"]
        if channel_before == "off":
            raise RuntimeError("ordinary acceptance requires a fresh fixture with the stable seed channel")
        channel = "off"
        body = self.apply_config(self.publish("HIKYO_UPDATE_CHANNEL", channel))
        ordinary = self.wait_applied(body)
        after = self.process()
        if before != after:
            raise RuntimeError("ordinary configuration replaced the process")
        channel_after = self.request("GET", "/instance/update-status")["channel"]
        if channel_after != channel:
            raise RuntimeError("live update service did not capture the published channel")
        self.evidence["ordinary"] = {"process_before": before, "process_after": after, "status": ordinary,
                                     "published_channel_before": channel_before, "runtime_channel_after": channel_after}
        self.evidence["observed_at"] = datetime.now(timezone.utc).isoformat()
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: ordinary configuration applied with identical PID, start ticks and container", flush=True)
        initial_annotations = self.get("deployment", "hikyo-hikyo")["spec"]["template"]["metadata"]["annotations"]
        database = "next" if initial_annotations["hikyo.dev/configuration-database-source"] == "primary" else "primary"
        status = self.publish("HIKYO_BOOTSTRAP_SOURCES", json.dumps({**json.loads(self.managed_value("HIKYO_BOOTSTRAP_SOURCES")), "version": 1, "database_source": database, "root_source": "next"}))
        body = self.apply_config(status, bootstrap=True)
        applied = self.wait_applied(body)
        replacement = self.process()
        if replacement["pod_uid"] == before["pod_uid"] or replacement["container_id"] == before["container_id"]:
            raise RuntimeError("bootstrap configuration did not replace the Pod")
        deployment = self.get("deployment", "hikyo-hikyo")
        annotations = deployment["spec"]["template"]["metadata"]["annotations"]
        for name, wanted in (("database-source", database), ("root-source", "next")):
            if annotations.get("hikyo.dev/configuration-" + name) != wanted:
                raise RuntimeError("replacement did not select the authorized source aliases")
        receipt_secret = self.get("secret", "hikyo-hikyo-rollout-receipt")
        receipt = {k: base64.b64decode(v).decode() for k, v in receipt_secret["data"].items()}
        self.private("receipt.json", json.dumps(receipt).encode())
        acknowledged = json.loads(receipt["receipt.json"])
        if acknowledged.get("phase") != "applied" or not acknowledged.get("application_acknowledged") or acknowledged.get("plan_digest") != body["plan_digest"]:
            raise RuntimeError("executor receipt lacks the exact application acknowledgment")
        if applied["job"].get("plan_digest") != body["plan_digest"]:
            raise RuntimeError("application acknowledged a different plan")
        self.login()
        final = self.request("GET", "/instance/config")
        self.evidence["bootstrap"] = {"process": replacement, "status": final,
                                      "deployment_uid": deployment["metadata"]["uid"], "annotations": annotations,
                                      "plan_digest": body["plan_digest"], "receipt": acknowledged}
        self.evidence["observed_at"] = datetime.now(timezone.utc).isoformat()
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: replacement booted with both source aliases; actual application acknowledged exact plan; TOTP login survived", flush=True)
        print(f"rollout-kind: metadata evidence {self.work / 'evidence.json'}", flush=True)

    def hold_observation(self):
        name = self.ns + "-hold-observation"
        self.apply({"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicy",
                    "metadata": {"name": name}, "spec": {"failurePolicy": "Fail",
                        "matchConstraints": {"resourceRules": [{"apiGroups": [""], "apiVersions": ["v1"], "operations": ["UPDATE"], "resources": ["secrets"]}]},
                        "matchConditions": [{"name": "fixture-executor-response", "expression":
                            f"request.namespace == '{self.ns}' && object.metadata.name == 'hikyo-hikyo-rollout-response' && request.userInfo.username == 'system:serviceaccount:{self.ns}:hikyo-hikyo-rollout'"}],
                        "validations": [{"expression": "object.data == oldObject.data",
                                         "message": "owned fixture holds executor response after preparation"}]}})
        self.apply({"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicyBinding",
                    "metadata": {"name": name}, "spec": {"policyName": name, "validationActions": ["Deny"]}})
        # The next real TOTP time step also allows the admission cache to see
        # this fixture policy before Submit is delivered. Target mutation and
        # app capture can proceed; the coordinator still lacks a matching reply.
        self.last_step = int(time.time()) // 30

    def root_guard_probe(self):
        output = (self.work / "rollout-admin-create.log").read_text()
        principal = re.search(r"principal ([^)]+)\)", output).group(1)
        jobs = json.loads(self.kube("get", "jobs", "-o", "json"))["items"]
        if not any(j["metadata"]["name"] == "rollout-root-grant" and j.get("status", {}).get("succeeded") for j in jobs):
            self.admin("rollout-root-grant", ["admin", "grant", "--principal", principal, "--capability", "rotate-root-key"])
        self.login()
        name = self.ns + "-hold-observation"
        try:
            annotations = self.get("deployment", "hikyo-hikyo")["spec"]["template"]["metadata"]["annotations"]
            database = "next" if annotations["hikyo.dev/configuration-database-source"] == "primary" else "primary"
            status = self.publish("HIKYO_BOOTSTRAP_SOURCES", json.dumps({**json.loads(self.managed_value("HIKYO_BOOTSTRAP_SOURCES")), "version": 1, "database_source": database, "root_source": "next"}))
            body = self.apply_config(status, bootstrap=True, hold_observation=True)
            deadline = time.monotonic() + 120
            while time.monotonic() < deadline:
                try:
                    current = self.request("GET", "/instance/config")
                    if current["nodes"] and all(n["active_generation"] == body["expected_generation"] + 1 for n in current["nodes"]):
                        if current["job"]["state"] == "completed":
                            raise RuntimeError("observation hold did not take effect")
                        break
                except OSError:
                    if self.forward.poll() is not None:
                        self.start_forward()
                time.sleep(1)
            else:
                raise RuntimeError("replacement did not capture while observation was held")
            epochs_query = "SELECT string_agg(root_key_epoch::text, ',' ORDER BY root_key_epoch) FROM master_keys WHERE state='active';"
            epochs_before = self.sql(epochs_query)
            verified = self.request("POST", "/instance/rotate-root-key", {"phase": "verify"})
            try:
                self.request("POST", "/instance/rotate-root-key", {"phase": "finalize"})
            except RuntimeError:
                refusal = json.loads((self.work / "last-http-error.json").read_text())
                if refusal["error"]["code"] != "conflict":
                    raise RuntimeError("root finalization refusal did not reach the exact pending-rollout guard") from None
            else:
                raise RuntimeError("root finalization unexpectedly succeeded during unresolved rollout")
            epochs_after = self.sql(epochs_query)
            if epochs_before != epochs_after or len(epochs_before.split(",")) != 2:
                raise RuntimeError("pending rollout lost its root rollback wrapper")
            self.evidence["root_finalization_guard"] = {"job": current["job"], "nodes": current["nodes"],
                                                        "verified": verified, "refusal": refusal,
                                                        "root_epochs_before": epochs_before, "root_epochs_after": epochs_after}
            print("rollout-kind: root finalization refused after candidate capture while executor acknowledgement remained unresolved", flush=True)
            self.prove_command_renewal(body, current)
        finally:
            self.kube("delete", "validatingadmissionpolicybinding", name, "--ignore-not-found=true")
            self.kube("delete", "validatingadmissionpolicy", name, "--ignore-not-found=true")
        completed = self.wait_applied(body)
        self.evidence["root_finalization_guard"]["completed_after_release"] = completed
        self.evidence["observed_at"] = datetime.now(timezone.utc).isoformat()
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())

    def prove_command_renewal(self, body, current):
        if not re.fullmatch(r"[0-9a-f]{64}", body["plan_digest"]):
            raise RuntimeError("fixture received an invalid exact plan digest")
        query = "SELECT command_json FROM self_config_rollouts WHERE plan_digest='" + body["plan_digest"] + "';"
        before = json.loads(self.sql(query))["command"]
        if before["action"] not in ("submit", "observe", "restore"):
            raise RuntimeError("transport expiry proof requires an already committed action")
        reauth_query = "SELECT count(*) FROM audit_instance_events WHERE type='auth.reauthenticated';"
        reauth_before = int(self.sql(reauth_query))
        expires = datetime.fromisoformat(before["expires_at"].replace("Z", "+00:00"))
        deadline = expires.timestamp() + 60
        next_progress = 0
        renewed = None
        while time.time() < deadline:
            candidate = json.loads(self.sql(query))["command"]
            if candidate["sequence"] > before["sequence"]:
                renewed = candidate
                break
            if time.monotonic() >= next_progress:
                remaining = max(0, int(expires.timestamp() - time.time()))
                print(f"rollout-kind: holding actual executor reply; {remaining}s until signed command expiry", flush=True)
                next_progress = time.monotonic() + 30
            time.sleep(2)
        if renewed is None:
            raise RuntimeError("expired committed command did not renew during real transport refusal")
        normalized_before, normalized_after = copy.deepcopy(before), copy.deepcopy(renewed)
        for name in ("sequence", "issued_at", "expires_at"):
            normalized_before.pop(name)
            normalized_after.pop(name)
        if normalized_before != normalized_after or time.time() < expires.timestamp():
            raise RuntimeError("renewal changed the authorized command or preceded real expiry")
        after = self.request("GET", "/instance/config")
        if any(after[key] != current[key] for key in ("generation", "desired_revision", "owner_instance_id")):
            raise RuntimeError("transport renewal changed the committed runtime target")
        reauth_after = int(self.sql(reauth_query))
        if reauth_before != reauth_after:
            raise RuntimeError("transport renewal unexpectedly required another MFA ceremony")
        fields = ("sequence", "action", "issued_at", "expires_at", "intent", "plan_digest")
        self.evidence["transport_expiry"] = {
            "before": {key: before[key] for key in fields},
            "renewed": {key: renewed[key] for key in fields},
            "unchanged_command_sha256": hashlib.sha256(json.dumps(normalized_before, sort_keys=True).encode()).hexdigest(),
            "reauth_event_count_before": reauth_before, "reauth_event_count_after": reauth_after,
            "status_after_renewal": after,
        }
        print("rollout-kind: actual expired command renewed with unchanged intent, target and MFA count", flush=True)

    def wait_status(self, predicate, description):
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            try:
                current = self.request("GET", "/instance/config")
                self.private("last-status.json", json.dumps(current).encode())
                if predicate(current):
                    return current
            except (OSError, RuntimeError):
                if self.forward.poll() is not None:
                    self.start_forward()
            time.sleep(1)
        raise RuntimeError(description + "; inspect private last-status.json")

    def topology_probe(self):
        initial_nodes = json.loads(self.managed_value("HIKYO_NODE_OVERRIDES"))
        if list(initial_nodes["nodes"]) != ["hikyo-hikyo-server"]:
            raise RuntimeError("topology proof requires the original singleton node")
        node_values = initial_nodes["nodes"]["hikyo-hikyo-server"]
        results = []
        for ha, node in ((True, "replacement"), (False, "hikyo-hikyo-server")):
            before = self.process()
            sources = {**json.loads(self.managed_value("HIKYO_BOOTSTRAP_SOURCES")),
                       "topology": {"ha": ha, "node_id": node}}
            nodes = {"version": 1, "nodes": {node: node_values}}
            body = self.apply_config(self.publish_values({"HIKYO_BOOTSTRAP_SOURCES": json.dumps(sources),
                "HIKYO_NODE_OVERRIDES": json.dumps(nodes)}), bootstrap=True)
            status = self.wait_applied(body)
            after = self.process()
            deployment = self.get("deployment", "hikyo-hikyo")
            server = next(c for c in deployment["spec"]["template"]["spec"]["containers"] if c["name"] == "server")
            effective = {item["name"]: item.get("value") for item in server["env"]}
            if effective.get("HIKYO_HA") != str(ha).lower() or effective.get("HIKYO_NODE_ID") != node:
                raise RuntimeError("replacement did not select the authorized singleton topology")
            if before["pod_uid"] == after["pod_uid"] or status["job"].get("plan_digest") != body["plan_digest"]:
                raise RuntimeError("topology did not replace and acknowledge the exact plan")
            if not any(n["node_id"] == node and n["active_generation"] == status["generation"] for n in status["nodes"]):
                raise RuntimeError("replacement node did not acknowledge the topology generation")
            leases = []
            if ha:
                deadline = time.monotonic() + 60
                while time.monotonic() < deadline:
                    leases = self.sql("SELECT name || '|' || owner FROM singleton_leases WHERE expires_at > now();").splitlines()
                    if "scheduler|replacement" in leases:
                        break
                    time.sleep(1)
                else:
                    raise RuntimeError("HA replacement did not acquire its real scheduler lease")
                self.restore_probe("ha_source_restore_repair")
                if json.loads(self.managed_value("HIKYO_BOOTSTRAP_SOURCES"))["topology"] != sources["topology"]:
                    raise RuntimeError("source-only Restore and repair changed HA topology")
            results.append({"topology": sources["topology"], "before": before, "after": after,
                            "status": status, "scheduler_leases": leases})
            self.evidence["singleton_topology"] = results
            self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
            print(f"rollout-kind: singleton topology ha={ha} node={node} replaced and acknowledged exact plan", flush=True)

    def prepare_disclosure_window(self):
        if "disclosure_window" in self.evidence:
            raise RuntimeError("disclosure fixture was already changed; inspect retained progress before resuming")
        original = self.managed_value("HIKYO_REAUTH_WINDOW_SECONDS")
        status = self.request("GET", "/instance/config")
        scope = "/orgs/{org_id}/projects/{project_id}/environments/{environment_id}".format(**status["binding"])
        window = self.request("GET", scope + "/reveal-window")
        if original != "0" or window["effective_window_seconds"] != 0 or window["totp_offered"]:
            raise RuntimeError("expanded fixture must begin with production's zero disclosure window")
        before = self.process()
        applied = self.wait_applied(self.apply_config(self.publish("HIKYO_REAUTH_WINDOW_SECONDS", "60")))
        changed = self.request("GET", scope + "/reveal-window")
        if self.process() != before or changed["effective_window_seconds"] != 60 or not changed["totp_offered"]:
            raise RuntimeError("authorized disclosure setting did not reload live")
        self.evidence["disclosure_window"] = {"original": original, "before_window": window,
            "configured_window": changed, "configured_status": applied, "configured_process": before}
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: production zero-window refused TOTP disclosure; exact MFA enabled a 60-second fixture window without replacement", flush=True)

    def restore_disclosure_window(self):
        record = self.evidence["disclosure_window"]
        before = self.process()
        status = self.request("GET", "/instance/config")
        scope = "/orgs/{org_id}/projects/{project_id}/environments/{environment_id}".format(**status["binding"])
        disclosure_session = self.token
        # Establish the applying session before opening the 60-second window.
        # Its exact Apply ceremony must not replace the disclosure session's window.
        applying_login = self.login()
        applying_session = self.token
        if applying_session == disclosure_session:
            raise RuntimeError("disclosure policy proof requires two independent sessions")
        self.token = disclosure_session
        disclosure_ceremony = self.request("POST", "/auth/reauth/totp", {"environment_id": status["binding"]["environment_id"], "code": self.code()})
        # Reauthentication rotates the bearer for this same session/window.
        disclosure_session = self.token
        disclosure_id = disclosure_ceremony["session_id"]
        applying_id = applying_login["session"]["id"]
        if disclosure_id == applying_id:
            raise RuntimeError("disclosure and applying ceremonies share a session identity")
        positive = self.request("POST", scope + "/values/HIKYO_NODE_OVERRIDES/reveal")
        if not positive["revealed"] or "value" not in positive:
            raise RuntimeError("positive disclosure did not succeed before returning the policy to zero")
        del positive
        positive_window = self.request("GET", scope + "/reveal-window")
        positive_expiry = datetime.fromisoformat(positive_window["expires_at"].replace("Z", "+00:00"))
        self.token = applying_session
        try:
            restored = self.wait_applied(self.apply_config(self.publish("HIKYO_REAUTH_WINDOW_SECONDS", record["original"])))
            if self.last_apply_session_id != applying_id:
                raise RuntimeError("exact Apply ceremony changed its session identity")
        finally:
            self.token = disclosure_session
        scope = "/orgs/{org_id}/projects/{project_id}/environments/{environment_id}".format(**restored["binding"])
        window = self.request("GET", scope + "/reveal-window")
        if not window["live"] or window["single_decision"] or window["expires_at"] != positive_window["expires_at"]:
            raise RuntimeError("independent disclosure session did not retain its original live window")
        cell = self.request("GET", scope + "/values/HIKYO_NODE_OVERRIDES")
        if self.process() != before or window["effective_window_seconds"] != 0 or window["totp_offered"] or not cell["set"] or cell["classification"] != "secret" or cell["revealed"] or "value" in cell:
            raise RuntimeError("restored disclosure policy did not retain the zero-window/redacted-read boundary")
        try:
            self.request("POST", scope + "/values/HIKYO_NODE_OVERRIDES/reveal")
        except RuntimeError:
            refusal = json.loads((self.work / "last-http-error.json").read_text())
            if self.last_http_status != 403 or refusal["error"]["code"] != "forbidden":
                raise RuntimeError("restored disclosure policy refused for an unexpected reason") from None
        else:
            raise RuntimeError("old positive TOTP window still disclosed after policy returned to zero")
        if datetime.now(timezone.utc) >= positive_expiry:
            raise RuntimeError("disclosure refusal was observed only after the prior positive window expired")
        record.update({"restored_window": window, "restored_status": restored,
                       "restored_process": before, "ordinary_secret_read_redacted": True,
                       "positive_disclosure_before_restore": True, "prior_window_expires_at": positive_window["expires_at"],
                       "independent_applying_session": True, "prior_disclosure_window_retained": True,
                       "disclosure_session_id": disclosure_id, "applying_session_id": applying_id,
                       "post_restore_disclosure_http_status": 403, "post_restore_disclosure_refusal": refusal})
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: exact MFA restored zero disclosure window live; ordinary secret read remains redacted", flush=True)

    def next_root_probe(self):
        before = self.process()
        initial = json.loads(self.managed_value("HIKYO_NODE_OVERRIDES"))
        if len(initial["nodes"]) != 1:
            raise RuntimeError("next-root proof requires the singleton fixture")
        node = next(iter(initial["nodes"]))
        if "HIKYO_NEW_ROOT_SOURCE" in initial["nodes"][node]:
            raise RuntimeError("fresh next-root selection proof requires an absent selector baseline")
        inventory = lambda: hashlib.sha256(self.sql("SELECT json_agg(t ORDER BY root_key_epoch)::text FROM master_keys t WHERE state='active';").encode()).hexdigest()
        wrappers = inventory()
        root_volume = next(v for v in self.get("deployment", "hikyo-hikyo")["spec"]["template"]["spec"]["volumes"] if v["name"] == "root-key-source")
        results = []
        self.evidence["next_root_selection"] = {"process": before, "active_wrapper_inventory_sha256": wrappers,
                                                "current_root_volume": root_volume, "selections": results}
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        for alias in ("primary", ""):
            nodes = copy.deepcopy(initial)
            if alias:
                nodes["nodes"][node]["HIKYO_NEW_ROOT_SOURCE"] = alias
            else:
                nodes["nodes"][node].pop("HIKYO_NEW_ROOT_SOURCE", None)
            body = self.apply_config(self.publish("HIKYO_NODE_OVERRIDES", json.dumps(nodes)))
            applied = self.wait_applied(body)
            selected = json.loads(self.managed_value("HIKYO_NODE_OVERRIDES"))["nodes"][node].get("HIKYO_NEW_ROOT_SOURCE", "")
            current_root = next(v for v in self.get("deployment", "hikyo-hikyo")["spec"]["template"]["spec"]["volumes"] if v["name"] == "root-key-source")
            if selected != alias or self.process() != before or inventory() != wrappers or current_root != root_volume:
                raise RuntimeError("next-root selection changed the current root, wrappers or process")
            if not any(n["node_id"] == node and n["active_revision"] == body["revision"] and n["active_generation"] == applied["generation"] for n in applied["nodes"]):
                raise RuntimeError("actual node did not acknowledge the next-root selector revision")
            results.append({"selected_alias": alias, "status": applied})
            self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: next-root selector applied then cleared live; process, current root and wrapper inventory unchanged", flush=True)

    def upgrade_probe(self, fixture_org=None):
        if not self.args.operator_binary:
            raise RuntimeError("upgrade drill requires the matching native --operator-binary")
        self.login()
        org = fixture_org
        if org is None:
            org = self.request("POST", "/orgs", {"name": "Rollout recovery proof"})["id"]
        # Org creation grants membership and deliberately ends the creator's
        # sessions. Authenticate again before using the new membership.
        self.login()
        project = self.request("POST", f"/orgs/{org}/projects", {"name": "Recovery proof"})["id"]
        environment = self.request("POST", f"/orgs/{org}/projects/{project}/environments", {"name": "Recovery proof"})["id"]
        self.request("POST", f"/orgs/{org}/projects/{project}/keys", {
            "name": "RECOVERY_PROOF", "classification": "secret", "folder_path": "",
            "declaration": {"rule": {"type": "string"}}})
        scope = f"/orgs/{org}/projects/{project}/environments/{environment}"
        version = self.request("PUT", scope + "/values/RECOVERY_PROOF", {"value": secrets.token_hex(24)})["version_id"]
        self.request("POST", scope + "/publish", {"version_ids": [version]})
        principal = re.search(r"principal ([^)]+)\)", (self.work / "rollout-admin-create.log").read_text()).group(1)
        self.run("age-keygen", "-o", str(self.work / "age.key"))
        recipient = self.run("age-keygen", "-y", str(self.work / "age.key")).decode().strip()
        self.prepare_age_identity()
        print("rollout-kind: exporting actual healthy source and restoring independently empty scratch database", flush=True)
        output = self.admin("rollout-upgrade-export", ["backup", "upgrade-export", "--bundle", "/run/hikyo-upgrade/bundle",
            "--out", "/var/lib/hikyo-upgrade/operator-custody/upgrade-export", "--recipient", recipient])
        paths = dict(line.split(": ", 1) for line in output.splitlines() if line.startswith(("ciphertext: ", "receipt: ")))
        for key in ("ciphertext", "receipt"):
            prefix = "/var/lib/hikyo-upgrade/operator-custody/upgrade-export/"
            if not paths.get(key, "").startswith(prefix) or "/" in paths[key].removeprefix(prefix):
                raise RuntimeError("upgrade-export returned a path outside its fixed fixture destination")
        self.run("docker", "cp", self.node + ":" + self.node_base + "/state/operator-custody/upgrade-export", str(self.work / "export"))
        installed_custody = self.node_base + "/state/operator-custody/operator-custody.json"
        custody_bytes = self.run("docker", "exec", self.node, "cat", installed_custody)
        if json.loads(custody_bytes).get("journal") is not None:
            raise RuntimeError("operator drill requires quiescent installation custody")
        (self.work / "operator-state").mkdir(mode=0o700)
        self.private("operator-state/operator-custody.json", custody_bytes)
        # This copy is operator-side drill custody only. The installed server and
        # selected alternate mount continue to share the original PVC object.
        shutil.copytree(self.args.public_dir, self.work / "operator-public")
        shutil.copy2(self.work / "operator.pub", self.work / "operator-public/operator.pub")
        self.sql("CREATE DATABASE rollout_drill;")
        self.complete_upgrade_probe(org, project, principal, paths, custody_bytes)

    def prepare_age_identity(self):
        # Hikyo's custody file contract is one encoded identity, while
        # age-keygen's file also includes public metadata comment lines.
        identities = [line.strip() for line in (self.work / "age.key").read_text().splitlines()
                      if line.strip() and not line.lstrip().startswith("#")]
        if len(identities) != 1 or not identities[0].startswith("AGE-SECRET-KEY-1"):
            raise RuntimeError("expected exactly one separately held X25519 identity")
        path = self.private("age-identity", (identities[0] + "\n").encode())
        original = self.run("age-keygen", "-y", str(self.work / "age.key"))
        if self.run("age-keygen", "-y", str(path)) != original:
            raise RuntimeError("normalized identity changed its public recipient")

    def complete_upgrade_probe(self, org, project, principal, paths, custody_bytes):
        installed_custody = self.node_base + "/state/operator-custody/operator-custody.json"
        password = base64.b64decode(self.get("secret", "postgres-auth")["data"]["password"]).decode()
        dsn = f"postgres://hikyo:{password}@127.0.0.1:{self.args.database_port}/rollout_drill?sslmode=verify-full&sslrootcert=" + urllib.parse.quote(str(self.work / "ca.crt"), safe="/")
        dsn_path = self.private("scratch-dsn", dsn.encode())
        deployment = self.get("deployment", "hikyo-hikyo")
        root_name = next(v["secret"]["secretName"] for v in deployment["spec"]["template"]["spec"]["volumes"] if v["name"] == "root-key-source")
        root = self.private("root-escrow", base64.b64decode(self.get("secret", root_name)["data"]["root-key"]))
        database_pods = json.loads(self.kube("get", "pods", "-l", "app=postgres", "-o", "json"))["items"]
        if len(database_pods) != 1:
            raise RuntimeError("scratch transport requires exactly one owned PostgreSQL Pod")
        self.database_forward = IsolatedDatabaseForward(self.args.context, self.ns,
            database_pods[0]["metadata"]["name"], self.args.database_port, self.log)
        selected = self.values["rollout"]["upgradeSources"]["next"]
        bundle = self.work / "operator-public/bundle"
        operator_env = {k: v for k, v in os.environ.items() if not k.startswith("HIKYO_")}
        operator_env.update({"COSIGN_PASSWORD": "", "HIKYO_DB": dsn,
            "HIKYO_UPGRADE_BUNDLE": str(bundle), "HIKYO_UPGRADE_STATE_DIR": str(self.work / "operator-state"),
            "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY": str(self.work / "operator.pub"),
            "HIKYO_UPGRADE_OPERATOR_INSTANCE": self.evidence["owner_instance_id"],
            "HIKYO_UPGRADE_TARGET_MANIFEST": selected["target_manifest_sha256"]})
        command = [self.args.operator_binary, "backup", "upgrade-drill", "--bundle", str(bundle),
            "--from", str(self.work / "export" / Path(paths["ciphertext"]).name),
            "--receipt", str(self.work / "export" / Path(paths["receipt"]).name),
            "--identity-file", str(self.work / "age-identity"), "--root-key-file", str(root),
            "--target-postgres-dsn-file", str(dsn_path), "--principal", principal,
            "--project", org + "/" + project, "--out", str(self.work / "drill"),
            "--cosign", self.args.cosign, "--signing-key", str(self.work / "operator.key"),
            "--valid-for", "24h"]
        # The operator command intentionally writes its result to stderr.
        output_path = self.private("upgrade-drill.log", b"")
        with output_path.open("wb") as output_file:
            result = subprocess.run(command, stdout=output_file, stderr=subprocess.STDOUT,
                                    env=operator_env, timeout=180, check=False)
        if result.returncode:
            raise RuntimeError("native upgrade drill failed; inspect private upgrade-drill.log")
        output = output_path.read_text()
        self.database_forward.terminate()
        self.database_forward.wait(timeout=10)
        if self.database_forward.errors or self.database_forward.children:
            raise RuntimeError("scratch relay reported a transport or cleanup failure")
        transport_connections = self.database_forward.accepted
        self.complete_upgrade_rollout(paths, custody_bytes, deployment, selected, output, transport_connections)

    def complete_upgrade_rollout(self, paths, custody_bytes, deployment, selected, output, transport_connections):
        installed_custody = self.node_base + "/state/operator-custody/operator-custody.json"
        if self.run("docker", "exec", self.node, "cat", installed_custody) != custody_bytes or (self.work / "operator-state/operator-custody.json").read_bytes() != custody_bytes:
            raise RuntimeError("installed or copied public operator custody changed during the standalone drill")
        results = dict(line.split(": ", 1) for line in output.splitlines() if ": " in line)
        if results.get("hierarchy") != "verified existing wrappers" or results.get("secret") != "existing-secret-readable" or results.get("credential") != "reconciled-minted-revoked":
            raise RuntimeError("real drill did not prove the populated hierarchy, secrets and credential")
        public = self.work / "next-public"
        (public / "evidence").mkdir(parents=True)
        shutil.copy2(self.work / "export" / Path(paths["receipt"]).name, public / "evidence/receipt.json")
        shutil.copy2(results["attestation"], public / "evidence/attestation.json")
        shutil.copy2(results["signature"], public / "evidence/attestation.sigstore.json")
        shutil.copy2(self.work / "export" / Path(paths["ciphertext"]).name, public / "backup.age")
        public.chmod(0o755)
        for path in public.rglob("*"):
            path.chmod(0o755 if path.is_dir() else 0o644)
        self.run("docker", "cp", str(public) + "/.", self.node + ":" + self.node_base + "/public/next/")
        before = self.process()
        body, applied = self.restore_probe("upgrade_source_restore_repair", "upgrade_source")
        after = self.process()
        custody_paths = [f"/proc/{after['host_pid']}/root" + path for path in
            ("/var/lib/hikyo-upgrade/operator-custody", selected["state_directory"])]
        custody_inodes = self.run("docker", "exec", self.node, "stat", "-c", "%d:%i", *custody_paths).decode().splitlines()
        if len(custody_inodes) != 2 or custody_inodes[0] != custody_inodes[1]:
            raise RuntimeError("actual replacement's old/new custody mounts do not share the same persistent object")
        current = self.get("deployment", "hikyo-hikyo")
        old_spec, new_spec = deployment["spec"]["template"]["spec"], current["spec"]["template"]["spec"]
        old_server = next(c for c in old_spec["containers"] if c["name"] == "server")
        new_server = next(c for c in new_spec["containers"] if c["name"] == "server")
        if old_spec["volumes"] != new_spec["volumes"] or old_server["volumeMounts"] != new_server["volumeMounts"] or old_server["image"] != new_server["image"]:
            raise RuntimeError("upgrade source switch changed image or mount authority")
        fields = {"HIKYO_UPGRADE_BUNDLE": "bundle_directory", "HIKYO_UPGRADE_STATE_DIR": "state_directory",
            "HIKYO_UPGRADE_EVIDENCE": "evidence_directory", "HIKYO_UPGRADE_BACKUP": "ciphertext_path",
            "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY": "operator_public_key_file", "HIKYO_UPGRADE_TARGET_MANIFEST": "target_manifest_sha256",
            "HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED": "legacy_writers_stopped"}
        old_env = {v["name"]: v.get("value") for v in old_server["env"]}
        new_env = {v["name"]: v.get("value") for v in new_server["env"]}
        for key, field in fields.items():
            wanted = str(selected[field]).lower() if isinstance(selected[field], bool) else selected[field]
            if new_env.get(key) != wanted or old_env.get(key) == wanted:
                raise RuntimeError("not every one of the seven upgrade inputs changed to its enrolled value")
        annotations = current["spec"]["template"]["metadata"]["annotations"]
        proof = annotations.get("hikyo.dev/configuration-upgrade-proof", "")
        if not re.fullmatch("[0-9a-f]{64}", proof) or annotations.get("hikyo.dev/configuration-upgrade-source") != "next":
            raise RuntimeError("replacement lacks selected upgrade alias and exact material proof")
        if before["pod_uid"] == after["pod_uid"] or applied["job"].get("plan_digest") != body["plan_digest"]:
            raise RuntimeError("upgrade profile did not replace and acknowledge the exact plan")
        self.evidence["upgrade_custody"] = {"before": before, "after": after, "selected": selected,
            "installed_and_selected_custody_device_inode": custody_inodes,
            "operator_public_custody_snapshot_sha256": hashlib.sha256(custody_bytes).hexdigest(),
            "scratch_transport": {"mode": "isolated opaque TCP streams", "connections": transport_connections,
                                  "tls": "verify-full", "children_after_cleanup": 0},
            "changed_inputs": sorted(fields), "material_proof_sha256": proof, "status": applied,
            "drill": {key: results[key] for key in ("hierarchy", "secret", "credential")},
            "public_artifact_sha256": {str(p.relative_to(public)): hashlib.sha256(p.read_bytes()).hexdigest() for p in public.rglob("*") if p.is_file()}}
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: actual encrypted export and signed scratch drill authorized all seven changed upgrade inputs; exact replacement acknowledged", flush=True)

    def restore_probe(self, evidence_key="restore_repair", source_key="database_source"):
        initial = self.get("deployment", "hikyo-hikyo")["spec"]["template"]["metadata"]["annotations"]
        annotation = "hikyo.dev/configuration-" + source_key.replace("_", "-")
        original_database = initial[annotation]
        default_alias = "initial" if source_key == "upgrade_source" else "primary"
        changed_database = "next" if original_database == default_alias else default_alias
        root = initial["hikyo.dev/configuration-root-source"]
        installed_sources = json.loads(self.managed_value("HIKYO_BOOTSTRAP_SOURCES"))
        sources = lambda database: json.dumps({**installed_sources, "version": 1, source_key: database, "root_source": root})
        hold_name = self.ns + "-hold-observation"
        before = self.process()
        try:
            body = self.apply_config(self.publish("HIKYO_BOOTSTRAP_SOURCES", sources(changed_database)), bootstrap=True, hold_observation=True)
            partial = self.wait_status(lambda value: value.get("job", {}).get("state") == "partial", "held executor reply did not produce partial convergence")
            if partial["generation"] != body["expected_generation"] + 1:
                raise RuntimeError("partial generation does not match the authorized target")
            candidate = self.process()
            if candidate["pod_uid"] == before["pod_uid"]:
                raise RuntimeError("restore fixture did not first replace the actual application Pod")
            restore = {"revision": body["revision"], "schema_version": body["schema_version"],
                       "expected_generation": partial["generation"], "idempotency_key": secrets.token_hex(16),
                       "plan_digest": body["plan_digest"], "restore_deployment": True,
                       "confirm_restored_credentials": False}
            intent = {"action": "rollout-restore", "owner_instance_id": partial["owner_instance_id"],
                      "revision": restore["revision"], "schema_version": restore["schema_version"],
                      "expected_generation": restore["expected_generation"], "plan_digest": restore["plan_digest"],
                      "preview_token": "", "to": "", "confirm_restored_credentials": False}
            self.request("POST", "/auth/reauth/totp", {"purpose": "self-config", "self_config": intent, "code": self.code()})
            requested = self.request("POST", "/instance/config/apply", restore)
            if requested["generation"] != partial["generation"] or requested["desired_revision"] != partial["desired_revision"]:
                raise RuntimeError("Restore changed the desired configuration target")
        finally:
            self.kube("delete", "validatingadmissionpolicybinding", hold_name, "--ignore-not-found=true")
            self.kube("delete", "validatingadmissionpolicy", hold_name, "--ignore-not-found=true")
        restored = self.wait_status(lambda value: value.get("job", {}).get("deployment_restored") is True,
                                    "actual deployment did not restore its previous resources")
        if restored["generation"] != partial["generation"] or restored["desired_revision"] != partial["desired_revision"] or restored["job"]["state"] != "partial":
            raise RuntimeError("restoration incorrectly completed the desired runtime target")
        restored_annotations = self.get("deployment", "hikyo-hikyo")["spec"]["template"]["metadata"]["annotations"]
        if restored_annotations != initial:
            raise RuntimeError("Restore did not recover the exact original source/stamp annotations")
        restored_process = self.process()
        if restored_process["pod_uid"] == candidate["pod_uid"]:
            raise RuntimeError("Restore did not replace the candidate Pod")
        try:
            self.request("GET", "/instance/update-status")
        except RuntimeError:
            refusal = json.loads((self.work / "last-http-error.json").read_text())
            if refusal["error"]["code"] != "service_unavailable":
                raise RuntimeError("restored runtime refusal was not the configuration fence") from None
        else:
            raise RuntimeError("restoration alone released the business runtime fence")
        repair_body = self.apply_config(self.publish("HIKYO_BOOTSTRAP_SOURCES", sources(original_database)))
        repaired = self.wait_applied(repair_body)
        if self.process() != restored_process:
            raise RuntimeError("repair of restored source unexpectedly replaced the process")
        self.request("GET", "/instance/update-status")
        repair_coordination = None
        if installed_sources.get("topology", {}).get("ha"):
            node = installed_sources["topology"]["node_id"]
            if not re.fullmatch(r"[A-Za-z0-9_.-]+", node):
                raise RuntimeError("invalid fixture node identity")
            # A stale lease from a previous process must not satisfy recovery.
            # Require a new heartbeat and a renewed scheduler lease after the
            # ordinary repair, before the next bootstrap replacement exists.
            marker = self.sql("SELECT now();")
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                repair_coordination = self.sql("SELECT json_build_object('node', n.node_id, 'heartbeat_at', n.heartbeat_at, 'lease_owner', l.owner, 'lease_expires_at', l.expires_at)::text FROM ha_nodes n JOIN singleton_leases l ON l.owner=n.node_id WHERE n.node_id='" + node + "' AND l.name='scheduler' AND n.heartbeat_at > '" + marker + "'::timestamptz AND l.expires_at > '" + marker + "'::timestamptz + interval '15 seconds';")
                if repair_coordination:
                    break
                time.sleep(1)
            else:
                raise RuntimeError("ordinary HA repair did not resume real shared coordination before reboot")
            if self.process() != restored_process:
                raise RuntimeError("HA coordination proof replaced the repaired process")
        later_body = self.apply_config(self.publish("HIKYO_BOOTSTRAP_SOURCES", sources(changed_database)), bootstrap=True)
        later = self.wait_applied(later_body)
        if self.process()["pod_uid"] == restored_process["pod_uid"] or later["job"].get("plan_digest") != later_body["plan_digest"]:
            raise RuntimeError("fresh later bootstrap plan did not replace and acknowledge its Pod")
        self.evidence[evidence_key] = {"before": before, "candidate": candidate, "partial": partial,
                                          "restored_process": restored_process, "restored": restored,
                                          "fenced_business_refusal": refusal, "repaired": repaired,
                                          "repair_coordination_before_later_rollout": json.loads(repair_coordination) if repair_coordination else None,
                                          "later_rollout": later, "later_process": self.process()}
        self.evidence["observed_at"] = datetime.now(timezone.utc).isoformat()
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: actual Restore kept desired target fenced; fresh TOTP repair resumed runtime; later exact TOTP rollout completed", flush=True)
        return later_body, later


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--resume", help="private custody of a retained fixture")
    parser.add_argument("--namespace", default="")
    probe = parser.add_mutually_exclusive_group()
    probe.add_argument("--root-guard-probe", action="store_true", help="only probe the root guard and real five-minute delivery expiry")
    probe.add_argument("--restore-probe", action="store_true", help="only probe Restore, fresh repair and a subsequent rollout in a retained fixture")
    probe.add_argument("--topology-probe", action="store_true", help="only probe singleton HA/node changes and HA source Restore in an expanded retained fixture")
    probe.add_argument("--upgrade-probe", action="store_true", help="only perform the real export/drill and seven-input upgrade switch in an expanded retained fixture")
    probe.add_argument("--expanded-probes", action="store_true", help="resume all added probes only at the exact completed baseline checkpoint")
    parser.add_argument("--expanded", action="store_true", help="enroll immutable topology/custody authority and run the complete additional probes")
    parser.add_argument("--operator-binary", help="native candidate built from the same source and signed linker input")
    parser.add_argument("--source-manifest", help="pre-build JSON mapping compiled source/embed files to SHA-256; checked before and after proof")
    parser.add_argument("--cosign", default="cosign", help="maintained local cosign executable for separately held operator authority")
    parser.add_argument("--database-port", type=int, default=15438, help="unused loopback port for the separately restored scratch database")
    parser.add_argument("--context", required=True)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--public-dir", required=True)
    parser.add_argument("--port", type=int, default=18188)
    parser.add_argument("--registry-port", type=int, default=56501)
    args = parser.parse_args()
    if (args.root_guard_probe or args.restore_probe or args.topology_probe or args.upgrade_probe or args.expanded_probes) and not args.resume:
        parser.error("individual probes require a retained --resume fixture")
    if (args.topology_probe or args.upgrade_probe or args.expanded_probes) and not args.expanded:
        parser.error("topology and upgrade probes require --expanded")
    if args.expanded and (not args.operator_binary or not args.source_manifest):
        parser.error("--expanded requires the matching native --operator-binary and pre-build --source-manifest")
    os.umask(0o077)
    fixture = Fixture(args)
    try:
        if args.resume:
            namespace = fixture.get("namespace", fixture.ns)
            if namespace["metadata"].get("labels", {}).get("hikyo.dev/test-fixture") != "config-rollout-kind":
                raise RuntimeError("namespace lacks exact fixture ownership")
            fixture.values = json.loads((fixture.work / "values.json").read_text())
            fixture.evidence["image"] = fixture.values["image"]["repository"] + "@" + fixture.values["image"]["digest"]
            actual_image = next(c["image"] for c in fixture.get("deployment", "hikyo-hikyo")["spec"]["template"]["spec"]["containers"] if c["name"] == "server")
            if actual_image != fixture.evidence["image"]:
                raise RuntimeError("deployed image differs from retained fixture evidence")
            fixture.node_base = "/var/lib/" + fixture.ns
            fixture.enroll()
        else:
            fixture.setup()
        fixture.authenticate()
        if args.root_guard_probe:
            fixture.root_guard_probe()
        elif args.restore_probe:
            fixture.restore_probe()
        elif args.topology_probe:
            fixture.topology_probe()
        elif args.upgrade_probe:
            fixture.upgrade_probe()
        elif args.expanded_probes:
            checkpoint = fixture.evidence.get("restore_repair", {})
            current = fixture.request("GET", "/instance/config")
            completed = checkpoint.get("later_rollout", {})
            if any(key in fixture.evidence for key in ("disclosure_window", "next_root_selection", "singleton_topology", "upgrade_custody")) or not completed or any(current[key] != completed[key] for key in ("generation", "desired_revision", "latest_revision")) or current.get("job", {}).get("state") != "completed" or fixture.process() != checkpoint.get("later_process"):
                raise RuntimeError("expanded resume is not at the exact completed baseline process/generation")
            fixture.evidence["expanded_resume_checkpoint"] = {"generation": current["generation"], "process": fixture.process()}
            fixture.prepare_disclosure_window()
            fixture.next_root_probe()
            fixture.topology_probe()
            fixture.upgrade_probe()
            fixture.restore_disclosure_window()
        else:
            fixture.exercise()
            fixture.root_guard_probe()
            fixture.restore_probe()
            if args.expanded:
                fixture.prepare_disclosure_window()
                fixture.next_root_probe()
                fixture.topology_probe()
                fixture.upgrade_probe()
                fixture.restore_disclosure_window()
        fixture.finish_evidence()
    finally:
        if fixture.forward is not None:
            fixture.forward.terminate()
            fixture.forward.wait(timeout=10)
        if fixture.database_forward is not None:
            fixture.database_forward.terminate()
            fixture.database_forward.wait(timeout=10)
        fixture.log.close()
        print(f"rollout-kind: retained owned namespace {fixture.ns}; cluster preserved", flush=True)


if __name__ == "__main__":
    main()

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
import shutil
import struct
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


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
        self.token = ""
        self.last_step = 0
        self.evidence = {"context": args.context, "namespace": self.ns,
                         "binary_sha256": hashlib.sha256(Path(args.binary).read_bytes()).hexdigest()}
        if args.resume and (self.work / "evidence.json").exists():
            retained = json.loads((self.work / "evidence.json").read_text())
            if retained.get("binary_sha256") != self.evidence["binary_sha256"]:
                raise RuntimeError("candidate binary differs from retained fixture evidence")
            self.evidence.update(retained)
        self.log = (self.work / "commands.log").open("ab")
        self.validate_cluster()
        print(f"rollout-kind: private custody {self.work}", flush=True)

    def run(self, *args, data=None, timeout=180):
        result = subprocess.run(args, input=data, stdout=subprocess.PIPE,
                                stderr=self.log, timeout=timeout, check=False)
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
        if self.args.resume and (self.evidence.get("context") != self.args.context or self.evidence.get("namespace") != self.ns):
            raise RuntimeError("retained evidence does not match selected fixture ownership")

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
        ext = self.private("tls.ext", f"subjectAltName=DNS:postgres.{self.ns}.svc\nextendedKeyUsage=serverAuth\n".encode())
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
        status = self.request("GET", "/instance/config")
        binding = status["binding"]
        scope = "/orgs/{org_id}/projects/{project_id}/environments/{environment_id}".format(**binding)
        change = self.request("PUT", scope + "/values/" + key, {"value": value})
        self.request("POST", scope + "/publish", {"version_ids": [change["version_id"]]})
        return self.request("GET", "/instance/config")

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
        self.request("POST", "/auth/reauth/totp", {"purpose": "self-config", "self_config": intent, "code": self.code()})
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
        status = self.publish("HIKYO_BOOTSTRAP_SOURCES", json.dumps({"version": 1, "database_source": database, "root_source": "next"}))
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
            status = self.publish("HIKYO_BOOTSTRAP_SOURCES", json.dumps({"version": 1, "database_source": database, "root_source": "next"}))
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

    def restore_probe(self):
        initial = self.get("deployment", "hikyo-hikyo")["spec"]["template"]["metadata"]["annotations"]
        original_database = initial["hikyo.dev/configuration-database-source"]
        changed_database = "next" if original_database == "primary" else "primary"
        root = initial["hikyo.dev/configuration-root-source"]
        sources = lambda database: json.dumps({"version": 1, "database_source": database, "root_source": root})
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
        later_body = self.apply_config(self.publish("HIKYO_BOOTSTRAP_SOURCES", sources(changed_database)), bootstrap=True)
        later = self.wait_applied(later_body)
        if self.process()["pod_uid"] == restored_process["pod_uid"] or later["job"].get("plan_digest") != later_body["plan_digest"]:
            raise RuntimeError("fresh later bootstrap plan did not replace and acknowledge its Pod")
        self.evidence["restore_repair"] = {"before": before, "candidate": candidate, "partial": partial,
                                          "restored_process": restored_process, "restored": restored,
                                          "fenced_business_refusal": refusal, "repaired": repaired,
                                          "later_rollout": later, "later_process": self.process()}
        self.evidence["observed_at"] = datetime.now(timezone.utc).isoformat()
        self.private("evidence.json", json.dumps(self.evidence, indent=2).encode())
        print("rollout-kind: actual Restore kept desired target fenced; fresh TOTP repair resumed runtime; later exact TOTP rollout completed", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--resume", help="private custody of a retained fixture")
    parser.add_argument("--namespace", default="")
    probe = parser.add_mutually_exclusive_group()
    probe.add_argument("--root-guard-probe", action="store_true", help="only probe the root guard and real five-minute delivery expiry")
    probe.add_argument("--restore-probe", action="store_true", help="only probe Restore, fresh repair and a subsequent rollout in a retained fixture")
    parser.add_argument("--context", required=True)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--public-dir", required=True)
    parser.add_argument("--port", type=int, default=18188)
    parser.add_argument("--registry-port", type=int, default=56501)
    args = parser.parse_args()
    if (args.root_guard_probe or args.restore_probe) and not args.resume:
        parser.error("individual probes require a retained --resume fixture")
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
        else:
            fixture.exercise()
            fixture.root_guard_probe()
            fixture.restore_probe()
    finally:
        if fixture.forward is not None:
            fixture.forward.terminate()
            fixture.forward.wait(timeout=10)
        fixture.log.close()
        print(f"rollout-kind: retained owned namespace {fixture.ns}; cluster preserved", flush=True)


if __name__ == "__main__":
    main()

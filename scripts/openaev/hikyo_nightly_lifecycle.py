"""Run-owned, bounded disposable Hikyo resources in the isolated hosted lab.

Only names in this module can be created/deleted. Deletion uses API UID
preconditions, never a selector deletion. Durable intent precedes creation so a
coordinator restart can recover its own resources without adopting foreign pods.
"""

from __future__ import annotations

import copy
import hashlib
import io
import json
import os
import re
import stat
import subprocess
import tarfile
import time
import uuid
from collections.abc import Callable
from pathlib import Path

KUBE = (
    "kubectl",
    "--kubeconfig",
    os.environ.get("KUBECONFIG", ""),
    "--context",
    os.environ.get("OPENAEV_KUBE_CONTEXT", "default"),
)
NAMESPACE = "hikyo-lab"
OWNER = "openaev-lab.went.io/nightly-run"
INTENT = "openaev-lab.went.io/manifest-sha256"
LEASE = "openaev-lab.went.io/expires-at"
PREPARER_IMAGE = (
    "busybox@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e"
)
SERVER_IMAGE = re.compile(r"ghcr\.io/hikyo-org/hikyo@sha256:[a-f0-9]{64}\Z")
DIGEST = re.compile(r"[a-f0-9]{64}\Z")
MAX_BUNDLE_BYTES = 128 * 1024 * 1024
Command = Callable[[tuple[str, ...], bytes | None, int], bytes]


class LifecycleError(RuntimeError):
    """Sanitized diagnostic, never subprocess logs or credentials."""


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise LifecycleError("invalid_native_object")
    return dict(value)


def _text(document: dict[str, object], key: str) -> str:
    value = document.get(key)
    if not isinstance(value, str) or not value:
        raise LifecycleError("native_field_missing_" + key)
    return value


def _json(value: bytes) -> dict[str, object]:
    try:
        parsed = json.loads(value)
    except (ValueError, UnicodeError):
        raise LifecycleError("invalid_native_json") from None
    return _object(parsed)


def _canonical(value: object) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def _command(arguments: tuple[str, ...], payload: bytes | None, timeout: int) -> bytes:
    try:
        return subprocess.run(
            KUBE + arguments,
            input=payload,
            capture_output=True,
            check=True,
            timeout=timeout,
        ).stdout
    except (OSError, subprocess.SubprocessError):
        raise LifecycleError("bounded_lab_command_failed") from None


def bundle_archive(directory: Path, expected_tree_sha256: str) -> tuple[bytes, str]:
    """Bind a deterministic regular-file archive to independently verified bundle."""
    if DIGEST.fullmatch(expected_tree_sha256) is None or directory.is_symlink():
        raise LifecycleError("invalid_bundle_input")
    if not directory.is_dir() or not (directory / "index.json").is_file():
        raise LifecycleError("bundle_index_missing")
    members: list[dict[str, object]] = []
    data: list[tuple[str, bytes]] = []
    total = 0
    for path in sorted(
        directory.rglob("*"), key=lambda item: item.relative_to(directory).as_posix()
    ):
        if path.is_symlink():
            raise LifecycleError("bundle_symlink_forbidden")
        if path.is_dir():
            continue
        name = path.relative_to(directory).as_posix()
        if not re.fullmatch(r"[A-Za-z0-9_./-]+", name) or any(
            part in (".", "..") or part.startswith(".") for part in Path(name).parts
        ):
            raise LifecycleError("bundle_path_invalid")
        try:
            descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(descriptor, "rb") as source:
                native = os.fstat(source.fileno())
                if not stat.S_ISREG(native.st_mode):
                    raise LifecycleError("bundle_regular_files_only")
                total += native.st_size
                if total > MAX_BUNDLE_BYTES or len(data) >= 2048:
                    raise LifecycleError("bundle_size_bound_exceeded")
                body = source.read(native.st_size + 1)
                if len(body) != native.st_size:
                    raise LifecycleError("bundle_changed_while_reading")
        except OSError:
            raise LifecycleError("bundle_file_read_failed") from None
        members.append(
            {
                "path": name,
                "size": len(body),
                "sha256": hashlib.sha256(body).hexdigest(),
            }
        )
        data.append((name, body))
    tree = hashlib.sha256(_canonical(members)).hexdigest()
    if tree != expected_tree_sha256:
        raise LifecycleError("verified_bundle_tree_changed")
    archive = io.BytesIO()
    with tarfile.open(fileobj=archive, mode="w", format=tarfile.USTAR_FORMAT) as output:
        for name, body in data:
            member = tarfile.TarInfo(name)
            member.size, member.mode, member.uid, member.gid, member.mtime = (
                len(body),
                0o600,
                65532,
                65532,
                0,
            )
            output.addfile(member, io.BytesIO(body))
    result = archive.getvalue()
    if len(result) > MAX_BUNDLE_BYTES:
        raise LifecycleError("bundle_archive_size_bound_exceeded")
    return result, hashlib.sha256(result).hexdigest()


class EphemeralHikyo:
    """Coordinator holds its exclusive lock across this object's whole lifetime."""

    def __init__(
        self, run_id: str, state: Path, *, command: Command = _command
    ) -> None:
        try:
            if str(uuid.UUID(run_id)) != run_id:
                raise ValueError("noncanonical")
        except (ValueError, AttributeError):
            raise LifecycleError("invalid_run_id") from None
        if state.is_symlink():
            raise LifecycleError("unsafe_lifecycle_state")
        state.mkdir(mode=0o700, parents=True, exist_ok=True)
        if (
            stat.S_IMODE(state.stat().st_mode) != 0o700
            or state.stat().st_uid != os.getuid()
        ):
            raise LifecycleError("unsafe_lifecycle_state")
        self.run_id, self.state, self.command = run_id, state, command
        self.preparer = "hikyo-nightly-prepare"
        self.journal = state / "lifecycle.json"
        self.resources: dict[str, object] = {}
        self.expires_at: int | None = None
        self.bound_volume: dict[str, object] | None = None
        self.children: dict[str, object] = {}
        self.prepared_bundle: dict[str, object] | None = None
        if self.journal.exists():
            if (
                self.journal.is_symlink()
                or stat.S_IMODE(self.journal.stat().st_mode) != 0o600
            ):
                raise LifecycleError("unsafe_lifecycle_journal")
            saved = _json(self.journal.read_bytes())
            if saved.get("run_id") != run_id or saved.get("namespace") != NAMESPACE:
                raise LifecycleError("lifecycle_journal_owner_mismatch")
            self.resources = _object(saved.get("resources"))
            self.children = _object(saved.get("children", {}))
            prepared = saved.get("prepared_bundle")
            if prepared is not None:
                self.prepared_bundle = _object(prepared)
                for key in ("bundle_tree_sha256", "transferred_archive_sha256"):
                    if DIGEST.fullmatch(_text(self.prepared_bundle, key)) is None:
                        raise LifecycleError("invalid_prepared_bundle_journal")
            bound = saved.get("bound_volume")
            if bound is not None:
                self.bound_volume = _object(bound)
                if not re.fullmatch(
                    r"pvc-[a-f0-9-]{36}", _text(self.bound_volume, "name")
                ):
                    raise LifecycleError("invalid_journal_volume_name")
                self._uid(_text(self.bound_volume, "uid"))
            expiry = saved.get("expires_at")
            if not isinstance(expiry, int) or isinstance(expiry, bool):
                raise LifecycleError("invalid_lifecycle_lease")
            self.expires_at = expiry
            for key, item in self.resources.items():
                entry = _object(item)
                kind, name = _text(entry, "kind"), _text(entry, "name")
                self._allowed(kind, name)
                if (
                    key != kind + "/" + name
                    or DIGEST.fullmatch(_text(entry, "manifest_sha256")) is None
                ):
                    raise LifecycleError("invalid_lifecycle_resource_journal")
                if entry.get("uid") is not None:
                    self._uid(_text(entry, "uid"))

    @staticmethod
    def _uid(value: str) -> str:
        try:
            if str(uuid.UUID(value)) != value:
                raise ValueError("noncanonical")
        except ValueError:
            raise LifecycleError("invalid_resource_uid") from None
        return value

    def _allowed(self, kind: str, name: str) -> None:
        if (kind, name) not in (
            ("Deployment", "hikyo"),
            ("PersistentVolumeClaim", "hikyo-lab-data"),
            ("Pod", self.preparer),
        ):
            raise LifecycleError("resource_outside_fixed_lab_allowlist")

    def _save(self) -> None:
        temporary = self.state / ("lifecycle-" + uuid.uuid4().hex + ".pending")
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "wb") as output:
            output.write(
                _canonical(
                    {
                        "run_id": self.run_id,
                        "namespace": NAMESPACE,
                        "expires_at": self.expires_at,
                        "resources": self.resources,
                        "children": self.children,
                        "bound_volume": self.bound_volume,
                        "prepared_bundle": self.prepared_bundle,
                    }
                )
            )
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, self.journal)
        descriptor = os.open(self.state, os.O_RDONLY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def _read(
        self, kind: str, name: str, *, cluster: bool = False
    ) -> dict[str, object] | None:
        arguments: tuple[str, ...] = (
            "get",
            kind,
            name,
            "--ignore-not-found",
            "-o",
            "json",
        )
        if not cluster:
            arguments += ("-n", NAMESPACE)
        raw = self.command(arguments, None, 10)
        return _json(raw) if raw.strip() else None

    def _list(self, kind: str) -> list[dict[str, object]]:
        result = _json(
            self.command(("get", kind, "-n", NAMESPACE, "-o", "json"), None, 10)
        )
        items = result.get("items")
        if not isinstance(items, list):
            raise LifecycleError("invalid_resource_list")
        return [_object(item) for item in items]

    def _owned(
        self, native: dict[str, object], kind: str, name: str, entry: dict[str, object]
    ) -> str:
        meta = _object(native.get("metadata"))
        labels, annotations = (
            _object(meta.get("labels")),
            _object(meta.get("annotations")),
        )
        if (
            native.get("kind") != kind
            or meta.get("namespace") != NAMESPACE
            or meta.get("name") != name
            or labels.get(OWNER) != self.run_id
            or annotations.get(OWNER) != self.run_id
            or annotations.get(INTENT) != entry.get("manifest_sha256")
            or annotations.get(LEASE) != str(self.expires_at)
        ):
            raise LifecycleError("native_resource_ownership_mismatch")
        uid = self._uid(_text(meta, "uid"))
        if entry.get("uid") is not None and entry.get("uid") != uid:
            raise LifecycleError("native_resource_uid_changed")
        return uid

    def _create(self, manifest: dict[str, object]) -> None:
        kind = _text(manifest, "kind")
        meta = _object(manifest.get("metadata"))
        name = _text(meta, "name")
        self._allowed(kind, name)
        key = kind + "/" + name
        if key in self.resources or self._read(kind, name) is not None:
            raise LifecycleError("foreign_or_existing_lab_resource")
        meta["namespace"] = NAMESPACE
        meta["labels"] = {**_object(meta.get("labels", {})), OWNER: self.run_id}
        meta["annotations"] = {OWNER: self.run_id, LEASE: str(self.expires_at)}
        manifest["metadata"] = meta
        digest = hashlib.sha256(_canonical(manifest)).hexdigest()
        annotations = _object(meta["annotations"])
        annotations[INTENT] = digest
        meta["annotations"] = annotations
        entry: dict[str, object] = {
            "kind": kind,
            "name": name,
            "uid": None,
            "manifest_sha256": digest,
        }
        self.resources[key] = entry
        self._save()  # Write intent before native create, including interrupted creation.
        native = _json(
            self.command(
                ("create", "-n", NAMESPACE, "-f", "-", "-o", "json"),
                _canonical(manifest),
                20,
            )
        )
        entry["uid"] = self._owned(native, kind, name, entry)
        self._save()

    def _wait(self, check: Callable[[], bool], seconds: int = 180) -> None:
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if check():
                return
            time.sleep(1)
        raise LifecycleError("bounded_resource_wait_failed")

    def _remove(self, kind: str, name: str) -> None:
        self._allowed(kind, name)
        entry = _object(self.resources[kind + "/" + name])
        current = self._read(kind, name)
        if current is None:
            return
        uid = self._owned(current, kind, name, entry)
        if entry.get("uid") is None:
            entry["uid"] = uid
            self.resources[kind + "/" + name] = entry
            self._save()  # Recover only this trusted intent, not labels found globally.
        base = "/apis/apps/v1" if kind == "Deployment" else "/api/v1"
        plural = {
            "Deployment": "deployments",
            "Pod": "pods",
            "PersistentVolumeClaim": "persistentvolumeclaims",
        }[kind]
        route = base + "/namespaces/" + NAMESPACE + "/" + plural + "/" + name
        self.command(
            ("delete", "--raw", route, "-f", "-"),
            _canonical(
                {
                    "apiVersion": "v1",
                    "kind": "DeleteOptions",
                    "preconditions": {
                        "uid": uid,
                        "resourceVersion": _text(
                            _object(current.get("metadata")), "resourceVersion"
                        ),
                    },
                    "propagationPolicy": "Foreground",
                    "gracePeriodSeconds": 10,
                }
            ),
            20,
        )

        def absent() -> bool:
            remaining = self._read(kind, name)
            if remaining is None:
                return True
            self._owned(remaining, kind, name, entry)
            return False

        self._wait(absent)

    def prepare(
        self,
        bundle_directory: Path,
        bundle_tree_sha256: str,
        *,
        lease_seconds: int = 1800,
    ) -> dict[str, object]:
        if self.resources or not 60 <= lease_seconds <= 7200:
            raise LifecycleError("invalid_or_existing_lifecycle_run")
        archive, archive_sha256 = bundle_archive(bundle_directory, bundle_tree_sha256)
        # No existing namespace workload or PVC can be adopted as a nightly target.
        if (
            self._list("deployments")
            or self._list("replicasets")
            or self._list("pods")
            or self._list("persistentvolumeclaims")
        ):
            raise LifecycleError("lab_target_not_idle")
        self.expires_at = int(time.time()) + lease_seconds
        pvc: dict[str, object] = {
            "apiVersion": "v1",
            "kind": "PersistentVolumeClaim",
            "metadata": {"name": "hikyo-lab-data"},
            "spec": {
                "accessModes": ["ReadWriteOnce"],
                "storageClassName": "local-path",
                "resources": {"requests": {"storage": "1Gi"}},
            },
        }
        self._create(pvc)
        prep: dict[str, object] = {
            "apiVersion": "v1",
            "kind": "Pod",
            "metadata": {
                "name": self.preparer,
                "labels": {"app.kubernetes.io/part-of": "openaev-lab"},
            },
            "spec": {
                "automountServiceAccountToken": False,
                "activeDeadlineSeconds": 600,
                "restartPolicy": "Never",
                "priorityClassName": "openaev-lab",
                "nodeSelector": {
                    "kubernetes.io/arch": "arm64",
                },
                "securityContext": {
                    "runAsNonRoot": True,
                    "runAsUser": 65532,
                    "runAsGroup": 65532,
                    "fsGroup": 65532,
                    "seccompProfile": {"type": "RuntimeDefault"},
                },
                "containers": [
                    {
                        "name": "prepare",
                        "image": PREPARER_IMAGE,
                        "command": [
                            "/bin/sh",
                            "-ec",
                            "umask 077; mkdir -m 0700 /var/lib/hikyo/data; mkdir -m 0700 /var/lib/hikyo/data/custody /var/lib/hikyo/data/bundle; sleep 600",
                        ],
                        "resources": {
                            # WaitForFirstConsumer binds this PVC to the chosen
                            # node. Reserve the future server's scheduling budget
                            # so a tiny helper cannot strand it on a full node.
                            # The helper is removed before server creation.
                            "requests": {"cpu": "100m", "memory": "384Mi"},
                            "limits": {"cpu": "500m", "memory": "384Mi"},
                        },
                        "securityContext": {
                            "allowPrivilegeEscalation": False,
                            "readOnlyRootFilesystem": True,
                            "capabilities": {"drop": ["ALL"]},
                        },
                        "volumeMounts": [
                            {"name": "data", "mountPath": "/var/lib/hikyo"}
                        ],
                    }
                ],
                "volumes": [
                    {
                        "name": "data",
                        "persistentVolumeClaim": {"claimName": "hikyo-lab-data"},
                    }
                ],
            },
        }
        self._create(prep)

        def running() -> bool:
            self._require_lease()
            native = self._read("Pod", self.preparer)
            if native is None:
                raise LifecycleError("preparer_disappeared")
            self._owned(
                native,
                "Pod",
                self.preparer,
                _object(self.resources["Pod/" + self.preparer]),
            )
            status = _object(native.get("status", {}))
            if status.get("phase") in ("Failed", "Succeeded"):
                raise LifecycleError("preparer_terminated")
            return status.get("phase") == "Running"

        self._wait(running)
        self._require_lease()
        prefix = ("exec", "-n", NAMESPACE, "-i", self.preparer, "-c", "prepare", "--")
        self.command(
            prefix
            + (
                "/bin/sh",
                "-ec",
                "umask 077; test ! -e /var/lib/hikyo/bundle.tar; cat > /var/lib/hikyo/bundle.tar; sha256sum /var/lib/hikyo/bundle.tar",
            ),
            archive,
            120,
        )
        actual = (
            self.command(prefix + ("sha256sum", "/var/lib/hikyo/bundle.tar"), None, 10)
            .decode("ascii")
            .split()
        )
        if not actual or actual[0] != archive_sha256:
            raise LifecycleError("native_bundle_transfer_digest_mismatch")
        self._require_lease()
        self.command(
            prefix
            + (
                "/bin/sh",
                "-ec",
                "tar -xf /var/lib/hikyo/bundle.tar -C /var/lib/hikyo/data/bundle; test -f /var/lib/hikyo/data/bundle/index.json; rm /var/lib/hikyo/bundle.tar",
            ),
            None,
            30,
        )
        self._remove("Pod", self.preparer)
        self.prepared_bundle = {
            "run_id": self.run_id,
            "bundle_tree_sha256": bundle_tree_sha256,
            "transferred_archive_sha256": archive_sha256,
            "preparer_removed": True,
        }
        self._save()
        return dict(self.prepared_bundle)

    def start(
        self,
        template: dict[str, object],
        image_ref: str,
        *,
        expected_version: str,
        source_commit: str,
    ) -> dict[str, object]:
        if (
            SERVER_IMAGE.fullmatch(image_ref) is None
            or not re.fullmatch(r"[a-f0-9]{40}", source_commit)
            or not re.fullmatch(
                r"0\.0\.1-nightly\.[0-9]{8}\.[0-9]+\.g[a-f0-9]+", expected_version
            )
        ):
            raise LifecycleError("invalid_verified_candidate_identity")
        if (
            self.expires_at is None
            or self.expires_at <= time.time()
            or self.prepared_bundle is None
            or "PersistentVolumeClaim/hikyo-lab-data" not in self.resources
        ):
            raise LifecycleError("prepared_run_required")
        if self._read("Pod", self.preparer) is not None:
            raise LifecycleError("preparer_must_be_removed_before_server")
        manifest = copy.deepcopy(template)
        if (
            manifest.get("kind") != "Deployment"
            or _object(manifest.get("metadata")).get("name") != "hikyo"
        ):
            raise LifecycleError("invalid_lab_deployment_template")
        spec = _object(manifest.get("spec"))
        if (
            spec.get("replicas") != 1
            or _object(spec.get("strategy")).get("type") != "Recreate"
        ):
            raise LifecycleError("bounded_single_server_required")
        selector = _object(_object(spec.get("selector")).get("matchLabels"))
        if selector != {"app": "hikyo-lab"}:
            raise LifecycleError("fixed_lab_selector_required")
        pod = _object(spec.get("template"))
        meta = _object(pod.get("metadata"))
        if _object(meta.get("labels")).get("app") != "hikyo-lab":
            raise LifecycleError("fixed_lab_pod_label_required")
        meta["labels"] = {**_object(meta.get("labels")), OWNER: self.run_id}
        meta["annotations"] = {OWNER: self.run_id, LEASE: str(self.expires_at)}
        pod["metadata"] = meta
        podspec = _object(pod.get("spec"))
        self._validate_pod_boundary(podspec)
        init = podspec.get("initContainers")
        containers = podspec.get("containers")
        if (
            not isinstance(init, list)
            or not isinstance(containers, list)
            or len(containers) != 1
        ):
            raise LifecycleError("invalid_lab_container_template")
        initialization = [_object(item) for item in init]
        for container in initialization:
            name = container.get("name")
            if name == "custody-and-evidence-init":
                container["image"] = PREPARER_IMAGE
                container["args"] = [
                    "umask 077; test -f /var/lib/hikyo/data/bundle/index.json; test ! -L /var/lib/hikyo/data; test ! -L /var/lib/hikyo/data/bundle; test \"$(stat -c '%u:%g:%a' /var/lib/hikyo/data)\" = 65532:65532:700; cp /run/lab-config-source/operator.pub /run/lab-config/operator.pub; chmod 0444 /run/lab-config/operator.pub"
                ]
            elif name in ("root-key-stage", "tls-stage"):
                container["image"] = image_ref
            else:
                raise LifecycleError("foreign_lab_init_container")
        if {item.get("name") for item in initialization} != {
            "custody-and-evidence-init",
            "root-key-stage",
            "tls-stage",
        }:
            raise LifecycleError("lab_initializers_missing")
        server = _object(containers[0])
        if server.get("name") != "server":
            raise LifecycleError("foreign_lab_server_container")
        server["image"] = image_ref
        podspec["initContainers"], podspec["containers"] = initialization, [server]
        pod["spec"], spec["template"], manifest["spec"] = podspec, pod, spec
        self._create(manifest)

        def available() -> bool:
            self._require_lease()
            native = self._read("Deployment", "hikyo")
            if native is None:
                raise LifecycleError("deployment_disappeared")
            self._owned(
                native,
                "Deployment",
                "hikyo",
                _object(self.resources["Deployment/hikyo"]),
            )
            status = _object(native.get("status", {}))
            return status.get("availableReplicas") == 1 and status.get(
                "observedGeneration"
            ) == _object(native.get("metadata")).get("generation")

        self._wait(available, 240)
        return {
            "run_id": self.run_id,
            "image_ref": image_ref,
            "expected_version": expected_version,
            "source_commit": source_commit,
            "deployment_uid": _object(self.resources["Deployment/hikyo"]).get("uid"),
        }

    def _require_lease(self) -> None:
        if self.expires_at is None or self.expires_at <= time.time():
            raise LifecycleError("lifecycle_run_lease_expired")

    @staticmethod
    def _validate_pod_boundary(spec: dict[str, object]) -> None:
        if spec.get("automountServiceAccountToken") is not False or any(
            spec.get(key) not in (None, False)
            for key in ("hostNetwork", "hostPID", "hostIPC")
        ):
            raise LifecycleError("lab_pod_host_or_identity_boundary_invalid")
        if spec.get("priorityClassName") != "openaev-lab":
            raise LifecycleError("bounded_lab_priority_required")
        security = _object(spec.get("securityContext"))
        if (
            security.get("runAsNonRoot") is not True
            or security.get("runAsUser") != 65532
            or security.get("runAsGroup") != 65532
            or security.get("fsGroup") != 65532
            or _object(security.get("seccompProfile")).get("type") != "RuntimeDefault"
        ):
            raise LifecycleError("lab_pod_security_boundary_invalid")
        selector = _object(spec.get("nodeSelector"))
        if (
            selector.get("kubernetes.io/arch") != "arm64"
            or spec.get("nodeName") is not None
        ):
            raise LifecycleError("unpinned_arm64_lab_required")
        volumes = spec.get("volumes")
        if not isinstance(volumes, list):
            raise LifecycleError("lab_volumes_missing")
        expected = {
            "data": "persistentVolumeClaim",
            "root-source": "secret",
            "root": "emptyDir",
            "tls-source": "secret",
            "tls": "emptyDir",
            "config": "emptyDir",
            "tmp": "emptyDir",
            "config-source": "configMap",
        }
        observed: set[str] = set()
        for raw in volumes:
            volume = _object(raw)
            name = _text(volume, "name")
            if (
                name in observed
                or name not in expected
                or set(volume) != {"name", expected[name]}
            ):
                raise LifecycleError("foreign_lab_volume")
            observed.add(name)
            source = _object(volume[expected[name]])
            if (
                name == "data"
                and source.get("claimName") != "hikyo-lab-data"
                or name in ("root-source", "tls-source")
                and source.get("secretName") != "hikyo-lab-custody"
                or name == "config-source"
                and source.get("name") != "hikyo-lab-config"
            ):
                raise LifecycleError("foreign_lab_volume_source")
        if observed != set(expected):
            raise LifecycleError("lab_volumes_missing")
        for key in ("initContainers", "containers"):
            containers = spec.get(key)
            if not isinstance(containers, list):
                raise LifecycleError("lab_container_security_missing")
            for raw in containers:
                container = _object(raw)
                context = _object(container.get("securityContext"))
                if (
                    context.get("allowPrivilegeEscalation") is not False
                    or context.get("readOnlyRootFilesystem") is not True
                    or context.get("privileged") not in (None, False)
                    or _object(context.get("capabilities")).get("drop") != ["ALL"]
                    or _object(context.get("capabilities")).get("add") not in (None, [])
                ):
                    raise LifecycleError("lab_container_security_boundary_invalid")

    def _capture_children(self) -> None:
        deployment = self.resources.get("Deployment/hikyo")
        deployment_uid = None if deployment is None else _object(deployment).get("uid")
        parents: dict[str, str] = {}
        for item in self._list("replicasets"):
            meta = _object(item.get("metadata"))
            uid, name = self._uid(_text(meta, "uid")), _text(meta, "name")
            if not re.fullmatch(r"hikyo-[a-z0-9]{1,10}", name):
                raise LifecycleError("foreign_lab_replicaset")
            self._child_owner(meta, "Deployment", "hikyo", deployment_uid)
            parents[uid] = name
            self._remember_child("ReplicaSet", name, uid)
        for item in self._list("pods"):
            meta = _object(item.get("metadata"))
            uid, name = self._uid(_text(meta, "uid")), _text(meta, "name")
            if name == self.preparer and "Pod/" + name in self.resources:
                self._owned(item, "Pod", name, _object(self.resources["Pod/" + name]))
                continue
            owners = meta.get("ownerReferences")
            if not isinstance(owners, list) or len(owners) != 1:
                raise LifecycleError("foreign_lab_pod")
            parent_uid = _text(_object(owners[0]), "uid")
            parent_name = parents.get(parent_uid)
            if parent_name is None or not name.startswith(parent_name + "-"):
                raise LifecycleError("foreign_lab_pod")
            self._child_owner(meta, "ReplicaSet", parent_name, parent_uid)
            self._remember_child("Pod", name, uid)
        if self.resources:
            self._save()

    def _remember_child(self, kind: str, name: str, uid: str) -> None:
        key = kind + "/" + name
        if key in self.children and self.children[key] != uid:
            raise LifecycleError("lab_child_uid_changed")
        self.children[key] = uid

    def _child_owner(
        self, meta: dict[str, object], kind: str, name: str, uid: object
    ) -> None:
        owners = meta.get("ownerReferences")
        if not isinstance(owners, list) or len(owners) != 1 or uid is None:
            raise LifecycleError("foreign_lab_child_owner")
        owner = _object(owners[0])
        if (
            owner.get("kind") != kind
            or owner.get("name") != name
            or owner.get("uid") != uid
            or owner.get("controller") is not True
            or meta.get("namespace") != NAMESPACE
            or _object(meta.get("labels")).get(OWNER) != self.run_id
            or _object(meta.get("annotations")).get(OWNER) != self.run_id
            or _object(meta.get("annotations")).get(LEASE) != str(self.expires_at)
        ):
            raise LifecycleError("foreign_lab_child_owner")

    def cleanup(self) -> dict[str, object]:
        """Release only journal-owned resources; preserve all foreign identities."""
        proof: dict[str, object] = {"run_id": self.run_id, "complete": False}
        try:
            # Validate all direct ownership before any destructive action.
            for item in self.resources.values():
                entry = _object(item)
                kind, name = _text(entry, "kind"), _text(entry, "name")
                current = self._read(kind, name)
                if current is not None:
                    uid = self._owned(current, kind, name, entry)
                    if entry.get("uid") is None:
                        entry["uid"] = uid
                        self.resources[kind + "/" + name] = entry
                        self._save()  # Recover response-lost parent before child validation.
            self._capture_children()
            pv_name: str | None = (
                None if self.bound_volume is None else _text(self.bound_volume, "name")
            )
            pv_uid: str | None = (
                None if self.bound_volume is None else _text(self.bound_volume, "uid")
            )
            pvc = (
                self._read("PersistentVolumeClaim", "hikyo-lab-data")
                if "PersistentVolumeClaim/hikyo-lab-data" in self.resources
                else None
            )
            if pvc is not None:
                volume = _object(pvc.get("spec")).get("volumeName")
                if isinstance(volume, str) and volume:
                    if not re.fullmatch(r"pvc-[a-f0-9-]{36}", volume):
                        raise LifecycleError("unexpected_local_path_volume_name")
                    pv = self._read("PersistentVolume", volume, cluster=True)
                    if pv is None:
                        raise LifecycleError("bound_volume_missing")
                    pv_spec = _object(pv.get("spec"))
                    claim = _object(pv_spec.get("claimRef"))
                    if (
                        claim.get("namespace") != NAMESPACE
                        or claim.get("name") != "hikyo-lab-data"
                        or claim.get("uid") != _object(pvc.get("metadata")).get("uid")
                        or pv_spec.get("storageClassName") != "local-path"
                        or pv_spec.get("persistentVolumeReclaimPolicy") != "Delete"
                    ):
                        raise LifecycleError("bound_volume_ownership_mismatch")
                    pv_name, pv_uid = (
                        volume,
                        self._uid(_text(_object(pv.get("metadata")), "uid")),
                    )
                    current_bound: dict[str, object] = {"name": pv_name, "uid": pv_uid}
                    if (
                        self.bound_volume is not None
                        and self.bound_volume != current_bound
                    ):
                        raise LifecycleError("bound_volume_changed_since_journal")
                    self.bound_volume = current_bound
                    self._save()  # Recover reclaim verification after PVC deletion.
            for kind, name in (("Deployment", "hikyo"), ("Pod", self.preparer)):
                if kind + "/" + name in self.resources:
                    self._remove(kind, name)
            # Foreground cascade must finish before deleting the backing claim.
            if self._list("replicasets") or self._list("pods"):
                raise LifecycleError("lab_children_or_foreign_workloads_remain")
            if "PersistentVolumeClaim/hikyo-lab-data" in self.resources:
                self._remove("PersistentVolumeClaim", "hikyo-lab-data")
            if pv_name is not None:

                def pv_absent() -> bool:
                    remaining = self._read("PersistentVolume", pv_name, cluster=True)
                    if remaining is None:
                        return True
                    if _object(remaining.get("metadata")).get("uid") != pv_uid:
                        raise LifecycleError("bound_volume_uid_changed")
                    return False

                self._wait(pv_absent)
            if (
                self._list("deployments")
                or self._list("pods")
                or self._list("replicasets")
                or self._list("persistentvolumeclaims")
            ):
                raise LifecycleError("lab_resources_not_released")
            proof.update(
                {
                    "complete": True,
                    "verified": True,
                    "deployment_pods_replicasets_pvc_gone": True,
                    "bound_local_path_pv_gone": True,
                    "bound_volume_uid": pv_uid,
                    "owned_child_uids": self.children,
                }
            )
            return proof
        except LifecycleError as error:
            proof["error"] = str(error)
            raise
        finally:
            destination = self.state / "cleanup.json"
            if destination.is_symlink():
                raise LifecycleError("unsafe_cleanup_proof_path")
            descriptor = os.open(
                destination,
                os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW,
                0o600,
            )
            with os.fdopen(descriptor, "wb") as output:
                output.write(_canonical(proof))

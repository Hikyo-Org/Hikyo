"""Ephemeral runner-local Docker monitoring on an internal-only network.

An independently running host audit collector sends sanitized observations through
Docker stdin to an internal-only relay. Prometheus scrapes the relay and evaluates
the rule; Alertmanager alone sends its webhook. Native control APIs are read using
Docker exec, with no host ports, host networking or mounted Docker socket.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import stat
import subprocess
import tempfile
import time
import uuid
from pathlib import Path
from types import TracebackType
from typing import Self
from urllib.parse import parse_qsl, urlsplit

from hikyo_detection_relay import (
    ALERT_NAME,
    PORT,
    canonical_uuid,
    finite_timestamp,
    object_value,
    validate_observation,
)

PROMETHEUS_IMAGE = "prom/prometheus:v3.15.0@sha256:efd719c99d83b060d9daefdcf00360461adf279f45ef5391f8d111892118753e"
ALERTMANAGER_IMAGE = "prom/alertmanager:v0.34.1@sha256:e9733bafb1bdef9b00e25a21f8f99dc26a22224bf16641ad754d1649f4c3357a"
RELAY_IMAGE = (
    "python@sha256:bb1f2fdb1065c85468775c9d680dcd344f6442a2d1181ef7916b60a623f11d40"
)
LABEL = "io.hikyo.openaev.monitoring-owner"
MAX_OUTPUT = 1_048_576
_CONTAINER = re.compile(r"hikyo-detection-[0-9a-f]{12}-relay")
_ARTIFACT_NAMES = {
    "prometheus.json",
    "rules.json",
    "alertmanager.json",
    "runtime-evidence.json",
    "runtime-owner-journal.json",
}


class MonitoringRuntimeError(RuntimeError):
    """Sanitized Docker/runtime failure."""


def prepare_images() -> None:
    """Fetch pinned ARM64 tools before opening the timed observer/actor window."""
    _local_daemon()
    for image in (PROMETHEUS_IMAGE, ALERTMANAGER_IMAGE, RELAY_IMAGE):
        try:
            subprocess.run(["docker", "pull", "--platform", "linux/arm64", image],
                           stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL, check=True, timeout=180)
        except (OSError, subprocess.SubprocessError):
            raise MonitoringRuntimeError("monitor_image_preparation_failed") from None


def _docker(arguments: list[str], input_text: str | None = None) -> str:
    try:
        result = subprocess.run(
            ["docker", *arguments],
            input=input_text,
            capture_output=True,
            text=True,
            timeout=20,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise MonitoringRuntimeError("docker_command_unavailable") from None
    if result.returncode:
        raise MonitoringRuntimeError("docker_command_failed")
    if len(result.stdout) > MAX_OUTPUT:
        raise MonitoringRuntimeError("docker_response_too_large")
    return result.stdout.strip()


def _json(value: str) -> object:
    try:
        return json.loads(value)
    except ValueError:
        raise MonitoringRuntimeError("invalid_native_json") from None


def _runtime_object(value: object) -> dict[str, object]:
    try:
        return object_value(value)
    except ValueError:
        raise MonitoringRuntimeError("runtime_object_invalid") from None


def _runtime_uuid(value: object) -> str:
    try:
        return canonical_uuid(value)
    except ValueError:
        raise MonitoringRuntimeError("runtime_uuid_invalid") from None


_JOURNAL = "runtime-owner-journal.json"
_NATIVE_ID = re.compile(r"[a-f0-9]{64}")


def _private_directory(directory: Path) -> Path:
    if directory.is_symlink():
        raise MonitoringRuntimeError("runtime_journal_directory_symlink")
    native = directory.lstat()
    if (
        not stat.S_ISDIR(native.st_mode)
        or native.st_uid != os.getuid()
        or stat.S_IMODE(native.st_mode) != 0o700
    ):
        raise MonitoringRuntimeError("runtime_journal_directory_permissions")
    return directory.resolve()


def _local_daemon() -> dict[str, object]:
    if os.environ.get("DOCKER_HOST") or os.environ.get("DOCKER_CONTEXT"):
        raise MonitoringRuntimeError("docker_environment_override_refused")
    endpoint = _json(
        _docker(["context", "inspect", "--format", "{{json .Endpoints.docker.Host}}"])
    )
    if not isinstance(endpoint, str) or not endpoint.startswith("unix:///"):
        raise MonitoringRuntimeError("docker_native_unix_socket_required")
    socket = Path(endpoint.removeprefix("unix://"))
    try:
        native = socket.lstat()
        runner_uid = os.getuid()
        mode = stat.S_IMODE(native.st_mode)
        # Hosted Linux uses a root-owned local daemon socket shared only with
        # the runner's Docker group. Mac rootless sockets are runner-owned.
        if (not stat.S_ISSOCK(native.st_mode) or native.st_uid not in (0, runner_uid)
                or mode & 0o007):
            raise OSError()
        if native.st_uid != runner_uid and (
                native.st_gid not in {os.getgid(), *os.getgroups()} or not mode & 0o020):
            raise OSError()
    except OSError:
        raise MonitoringRuntimeError("docker_socket_owner_invalid") from None
    daemon_id = _docker(["info", "--format", "{{.ID}}"])
    if (
        not daemon_id
        or len(daemon_id) > 128
        or any(char.isspace() for char in daemon_id)
    ):
        raise MonitoringRuntimeError("docker_daemon_identity_invalid")
    return {"endpoint": endpoint, "daemon_id": daemon_id}


def _save_journal(directory: Path, document: dict[str, object]) -> None:
    directory = _private_directory(directory)
    target = directory / _JOURNAL
    if target.exists() or target.is_symlink():
        native = target.lstat()
        if (
            not stat.S_ISREG(native.st_mode)
            or native.st_uid != os.getuid()
            or stat.S_IMODE(native.st_mode) != 0o600
        ):
            raise MonitoringRuntimeError("runtime_journal_permissions_or_size")
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            prefix="runtime-owner-journal-",
            suffix=".tmp",
            dir=directory,
            delete=False,
        ) as handle:
            temporary = Path(handle.name)
            os.fchmod(handle.fileno(), 0o600)
            json.dump(document, handle, sort_keys=True, indent=2)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, target)
        descriptor = os.open(directory, os.O_RDONLY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def _read_journal(directory: Path) -> dict[str, object]:
    directory = _private_directory(directory)
    target = directory / _JOURNAL
    try:
        descriptor = os.open(target, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "r") as handle:
            native = os.fstat(handle.fileno())
            if (
                not stat.S_ISREG(native.st_mode)
                or native.st_uid != os.getuid()
                or stat.S_IMODE(native.st_mode) != 0o600
                or native.st_size > 32_768
            ):
                raise MonitoringRuntimeError("runtime_journal_permissions_or_size")
            document = _runtime_object(_json(handle.read(32_769)))
    except OSError:
        raise MonitoringRuntimeError("runtime_journal_unavailable") from None
    owner = _runtime_uuid(document.get("owner"))
    _runtime_uuid(document.get("run_id"))
    if (
        type(document.get("schema_version")) is not int
        or document.get("schema_version") != 1
    ):
        raise MonitoringRuntimeError("runtime_journal_schema_invalid")
    prefix = "hikyo-detection-" + owner.replace("-", "")[:12]
    resources = document.get("resources")
    if not isinstance(resources, list) or len(resources) > 6:
        raise MonitoringRuntimeError("runtime_journal_resources_invalid")
    seen: set[str] = set()
    for row in resources:
        item = _runtime_object(row)
        kind, name, native_id = (
            item.get("kind"),
            item.get("name"),
            item.get("native_id"),
        )
        if (
            kind not in {"container", "network"}
            or not isinstance(name, str)
            or name in seen
            or name
            not in {
                prefix + "-" + suffix
                for suffix in (
                    "relay",
                    "prometheus",
                    "alertmanager",
                    "promtool",
                    "amtool",
                    "network",
                )
            }
            or (kind == "network") != name.endswith("-network")
            or (
                native_id is not None
                and (
                    not isinstance(native_id, str)
                    or _NATIVE_ID.fullmatch(native_id) is None
                )
            )
        ):
            raise MonitoringRuntimeError("runtime_journal_resource_binding_invalid")
        seen.add(name)
    return document


def _inspect_owned(kind: str, name: str, owner: str) -> str | None:
    try:
        response = _json(_docker([kind, "inspect", name]))
    except MonitoringRuntimeError:
        names = _docker(
            [kind, "ls", "--format", "{{.Name}}"]
            if kind == "network"
            else [kind, "ls", "--all", "--format", "{{.Names}}"]
        )
        if name not in names.splitlines():
            return None
        raise
    if not isinstance(response, list) or len(response) != 1:
        raise MonitoringRuntimeError("runtime_native_inspect_invalid")
    native = _runtime_object(response[0])
    labels = (
        _runtime_object(_runtime_object(native.get("Config")).get("Labels"))
        if kind == "container"
        else _runtime_object(native.get("Labels"))
    )
    native_name = native.get("Name")
    native_id = native.get("Id")
    if (
        native_name != ("/" + name if kind == "container" else name)
        or labels.get(LABEL) != owner
        or not isinstance(native_id, str)
        or _NATIVE_ID.fullmatch(native_id) is None
    ):
        raise MonitoringRuntimeError("runtime_native_identity_mismatch")
    return native_id


def recover_runtime(artifact_dir: Path) -> dict[str, object]:
    """Recover only private pre-create intents, then remove immutable native IDs."""
    directory = _private_directory(artifact_dir.absolute())
    document = _read_journal(directory)
    previous = document.get("cleanup")
    if (
        isinstance(previous, dict)
        and previous.get("complete") is True
        and previous.get("verified") is True
        and previous.get("run_id") == document.get("run_id")
    ):
        return {
            "complete": True,
            "verified": True,
            "run_id": document["run_id"],
            "removed": [],
            "failed_resources": [],
            "already_complete": True,
        }
    if document.get("daemon") != _local_daemon():
        raise MonitoringRuntimeError("runtime_recovery_daemon_changed")
    owner = _runtime_uuid(document.get("owner"))
    resources = document.get("resources")
    if not isinstance(resources, list):
        raise MonitoringRuntimeError("runtime_journal_resources_invalid")
    ordered = [_runtime_object(row) for row in reversed(resources)]
    ordered.sort(key=lambda item: item["kind"] == "network")
    # Inspect every target before any deletion. A foreign replacement aborts all.
    for item in ordered:
        kind, name = str(item["kind"]), str(item["name"])
        native_id = _inspect_owned(kind, name, owner)
        recorded = item.get("native_id")
        if native_id is not None and recorded is not None and native_id != recorded:
            raise MonitoringRuntimeError("runtime_native_id_changed")
        if native_id is not None and recorded is None:
            item["native_id"] = native_id
    document["resources"] = list(reversed(ordered))
    _save_journal(directory, document)
    removed: list[str] = []
    for item in ordered:
        kind, name = str(item["kind"]), str(item["name"])
        native_id = _inspect_owned(kind, name, owner)
        if native_id is None:
            continue
        if native_id != item.get("native_id"):
            raise MonitoringRuntimeError("runtime_native_id_changed")
        _docker(
            ["rm", "-f", native_id]
            if kind == "container"
            else ["network", "rm", native_id]
        )
        removed.append(name)
    for item in ordered:
        if _inspect_owned(str(item["kind"]), str(item["name"]), owner) is not None:
            raise MonitoringRuntimeError("runtime_recovery_resource_remaining")
    proof: dict[str, object] = {
        "complete": True,
        "verified": True,
        "run_id": document["run_id"],
        "removed": removed,
        "failed_resources": [],
    }
    document["cleanup"] = proof
    _save_journal(directory, document)
    return proof


def update_relay_observation(
    relay_container: str, observation: dict[str, object]
) -> None:
    """Collector-only transport. It never calls Alertmanager or supplies alerts."""
    if not _CONTAINER.fullmatch(relay_container):
        raise MonitoringRuntimeError("invalid_relay_container")
    run_id = canonical_uuid(observation.get("run_id"))
    validate_observation(observation, run_id)
    script = (
        "import json,sys,urllib.request; "
        "data=sys.stdin.buffer.read(65537); "
        "assert len(data)<=65536; "
        "request=urllib.request.Request('http://127.0.0.1:18790/metrics-state',"
        "data=data,method='PUT',headers={'Content-Type':'application/json'}); "
        "opener=urllib.request.build_opener(urllib.request.ProxyHandler({})); "
        "response=opener.open(request,timeout=5); "
        "assert json.load(response)=={'accepted':True}"
    )
    _docker(
        ["exec", "-i", relay_container, "python", "-c", script],
        json.dumps(observation, allow_nan=False),
    )


def monitoring_configs(run_id: str) -> dict[str, dict[str, object]]:
    canonical_uuid(run_id)
    selector = '{run_id="' + run_id + '"}'
    success_age = "(time() - hikyo_lab_audit_last_success_unixtime" + selector + ")"
    expression = (
        "(hikyo_lab_native_denial" + selector + " == 1)"
        " and on(run_id) (hikyo_lab_audit_poll_success" + selector + " == 1)"
        " and on(run_id) (" + success_age + " <= 10)"
        " and on(run_id) (" + success_age + " >= 0)"
        " and on(run_id) (hikyo_lab_run_deadline_unixtime" + selector + " > time())"
        ' and on() (up{job="hikyo-lab-native-audit"} == 1)'
        " and on(run_id,event_id) ((time() - timestamp(hikyo_lab_native_denial"
        + selector
        + ")) <= 5)"
    )
    return {
        "prometheus.json": {
            "global": {"scrape_interval": "2s", "evaluation_interval": "2s"},
            "rule_files": ["/etc/hikyo-lab/rules.json"],
            "scrape_configs": [
                {
                    "job_name": "hikyo-lab-native-audit",
                    "scrape_timeout": "1s",
                    "sample_limit": 8,
                    "static_configs": [{"targets": ["relay:18790"]}],
                }
            ],
            "alerting": {
                "alertmanagers": [
                    {"static_configs": [{"targets": ["alertmanager:9093"]}]}
                ]
            },
        },
        "rules.json": {
            "groups": [
                {
                    "name": "hikyo-isolated-native-audit",
                    "rules": [{"alert": ALERT_NAME, "expr": expression, "for": "0s"}],
                }
            ]
        },
        "alertmanager.json": {
            "route": {
                "receiver": "hikyo-lab-inbox",
                "group_by": ["run_id", "event_id"],
                "group_wait": "1s",
                "group_interval": "2s",
                "repeat_interval": "10m",
            },
            "receivers": [
                {
                    "name": "hikyo-lab-inbox",
                    "webhook_configs": [
                        {
                            "url": "http://relay:18790/alerts",
                            "send_resolved": False,
                            "max_alerts": 1,
                        }
                    ],
                }
            ],
        },
    }


class LocalMonitoringRuntime:
    def __init__(
        self,
        run_id: str,
        artifact_dir: Path,
        metrics_port: int = PORT,
        webhook_port: int = PORT,
        lease_seconds: int = 600,
    ):
        self.run_id = canonical_uuid(run_id)
        if (
            metrics_port != PORT
            or webhook_port != PORT
            or not 10 <= lease_seconds <= 600
        ):
            raise MonitoringRuntimeError("invalid_runtime_endpoints_or_lease")
        self.artifact_dir = artifact_dir.absolute()
        self.lease_seconds = lease_seconds
        self.owner = str(uuid.uuid4())
        self.prefix = "hikyo-detection-" + self.owner.replace("-", "")[:12]
        self.network = self.prefix + "-network"
        self.relay_container = self.prefix + "-relay"
        self.prometheus_container = self.prefix + "-prometheus"
        self.alertmanager_container = self.prefix + "-alertmanager"
        self._containers: list[str] = []
        self._checks: list[str] = []
        self._network_attempted = False
        self._started = False
        self._journal: dict[str, object] | None = None
        self.lease_deadline_unixtime = 0.0
        self.evidence: dict[str, object] = {
            "run_id": run_id,
            "network_internal": True,
            "published_ports": [],
            "artifacts": {},
        }

    def __enter__(self) -> Self:
        return self.start()

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        self.stop()

    def _write_evidence(self) -> None:
        if not self.artifact_dir.is_dir():
            return
        target = self.artifact_dir / "runtime-evidence.json"
        target.write_text(
            json.dumps(self.evidence, sort_keys=True, indent=2), encoding="utf-8"
        )
        target.chmod(0o600)

    def _intent(self, kind: str, name: str) -> None:
        if self._journal is None:
            raise MonitoringRuntimeError("runtime_journal_required_before_create")
        resources = self._journal.get("resources")
        if not isinstance(resources, list):
            raise MonitoringRuntimeError("runtime_journal_resources_invalid")
        resources.append({"kind": kind, "name": name, "native_id": None})
        _save_journal(self.artifact_dir, self._journal)

    def _bind(self, kind: str, name: str, native_id: str) -> None:
        if _NATIVE_ID.fullmatch(native_id) is None or self._journal is None:
            raise MonitoringRuntimeError("runtime_create_native_id_invalid")
        resources = self._journal.get("resources")
        if not isinstance(resources, list):
            raise MonitoringRuntimeError("runtime_journal_resources_invalid")
        for row in resources:
            item = object_value(row)
            if item.get("name") == name and item.get("kind") == kind:
                row["native_id"] = native_id
                _save_journal(self.artifact_dir, self._journal)
                return
        raise MonitoringRuntimeError("runtime_create_intent_missing")

    def _create(
        self,
        name: str,
        alias: str,
        image: str,
        memory: str,
        reservation: str,
        cpus: str,
        tmpfs: str,
        arguments: list[str],
        command: list[str],
    ) -> None:
        self._containers.append(name)
        self._intent("container", name)
        native_id = _docker(
            [
                "create",
                "--name",
                name,
                "--label",
                LABEL + "=" + self.owner,
                "--network",
                self.network,
                "--network-alias",
                alias,
                "--read-only",
                "--user",
                "65534:65534",
                "--cap-drop",
                "ALL",
                "--security-opt",
                "no-new-privileges",
                "--pids-limit",
                "64",
                "--memory",
                memory,
                "--memory-reservation",
                reservation,
                "--memory-swap",
                memory,
                "--cpus",
                cpus,
                "--tmpfs",
                tmpfs,
                *arguments,
                image,
                *command,
            ]
        )
        self._bind("container", name, native_id)

    def start(self) -> Self:
        if self._started or self._network_attempted:
            raise MonitoringRuntimeError("runtime_already_started")
        if self.artifact_dir.is_symlink():
            raise MonitoringRuntimeError("artifact_directory_symlink")
        self.artifact_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        info = self.artifact_dir.lstat()
        if (
            not stat.S_ISDIR(info.st_mode)
            or info.st_uid != os.getuid()
            or stat.S_IMODE(info.st_mode) != 0o700
        ):
            raise MonitoringRuntimeError(
                "artifact_directory_requires_owner_only_permissions"
            )
        if any(
            path.name not in _ARTIFACT_NAMES or path.is_symlink() or not path.is_file()
            for path in self.artifact_dir.iterdir()
        ):
            raise MonitoringRuntimeError("artifact_directory_has_foreign_files")
        self.artifact_dir = self.artifact_dir.resolve()
        if (self.artifact_dir / _JOURNAL).exists():
            raise MonitoringRuntimeError("runtime_journal_already_exists")
        self._journal = {
            "schema_version": 1,
            "owner": self.owner,
            "run_id": self.run_id,
            "daemon": _local_daemon(),
            "resources": [],
        }
        _save_journal(self.artifact_dir, self._journal)
        configs = monitoring_configs(self.run_id)
        artifacts: dict[str, object] = {}
        for filename, content in configs.items():
            encoded = json.dumps(content, sort_keys=True, indent=2).encode()
            path = self.artifact_dir / filename
            path.write_bytes(encoded)
            path.chmod(0o644)
            artifacts[filename] = {"sha256": hashlib.sha256(encoded).hexdigest()}
        relay_source = Path(__file__).with_name("hikyo_detection_relay.py").resolve()
        artifacts[relay_source.name] = {
            "sha256": hashlib.sha256(relay_source.read_bytes()).hexdigest()
        }
        self.evidence["artifacts"] = artifacts
        self._write_evidence()
        # The private host directory stays 0700. Bind its public files one by
        # one: uid65534 cannot traverse a uid1001-owned 0700 directory on Linux,
        # and the private owner journal/evidence must never enter containers.
        config_mounts = []
        for filename in configs:
            config_mounts.extend(["--mount", "type=bind,src=" + str(self.artifact_dir / filename)
                                  + ",dst=/etc/hikyo-lab/" + filename + ",readonly"])

        try:
            for suffix, memory, entrypoint, image, command in (
                (
                    "promtool",
                    "128m",
                    "/bin/promtool",
                    PROMETHEUS_IMAGE,
                    ["check", "config", "/etc/hikyo-lab/prometheus.json"],
                ),
                (
                    "amtool",
                    "64m",
                    "/bin/amtool",
                    ALERTMANAGER_IMAGE,
                    ["check-config", "/etc/hikyo-lab/alertmanager.json"],
                ),
            ):
                check_name = self.prefix + "-" + suffix
                self._checks.append(check_name)
                self._intent("container", check_name)
                _docker(
                    [
                        "run",
                        "--rm",
                        "--name",
                        check_name,
                        "--label",
                        LABEL + "=" + self.owner,
                        "--network",
                        "none",
                        "--read-only",
                        "--cap-drop",
                        "ALL",
                        "--security-opt",
                        "no-new-privileges",
                        "--memory",
                        memory,
                        "--cpus",
                        "0.25",
                        "--pids-limit",
                        "64",
                        *config_mounts,
                        "--entrypoint",
                        entrypoint,
                        image,
                        *command,
                    ]
                )
            self.evidence["native_config_checks"] = {"promtool": True, "amtool": True}
            self._network_attempted = True
            self._intent("network", self.network)
            network_id = _docker(
                [
                    "network",
                    "create",
                    "--internal",
                    "--label",
                    LABEL + "=" + self.owner,
                    self.network,
                ]
            )
            self._bind("network", self.network, network_id)
            self._create(
                self.relay_container,
                "relay",
                RELAY_IMAGE,
                "128m",
                "64m",
                "0.25",
                "/state:rw,nodev,nosuid,noexec,size=8m,uid=65534,gid=65534,mode=0700",
                [
                    "--env",
                    "PYTHONDONTWRITEBYTECODE=1",
                    "--mount",
                    "type=bind,src="
                    + str(relay_source)
                    + ",dst=/app/relay.py,readonly",
                ],
                [
                    "python",
                    "/app/relay.py",
                    "--run-id",
                    self.run_id,
                    "--state",
                    "/state",
                    "--lease-seconds",
                    str(self.lease_seconds),
                ],
            )
            self._create(
                self.alertmanager_container,
                "alertmanager",
                ALERTMANAGER_IMAGE,
                "256m",
                "128m",
                "0.25",
                "/alertmanager:rw,nodev,nosuid,noexec,size=32m,uid=65534,gid=65534,mode=0700",
                config_mounts,
                [
                    "--config.file=/etc/hikyo-lab/alertmanager.json",
                    "--storage.path=/alertmanager",
                    "--cluster.listen-address=",
                ],
            )
            self._create(
                self.prometheus_container,
                "prometheus",
                PROMETHEUS_IMAGE,
                "512m",
                "256m",
                "0.5",
                "/prometheus:rw,nodev,nosuid,noexec,size=128m,uid=65534,gid=65534,mode=0700",
                config_mounts,
                [
                    "--config.file=/etc/hikyo-lab/prometheus.json",
                    "--storage.tsdb.path=/prometheus",
                    "--storage.tsdb.retention.time=1h",
                    "--storage.tsdb.retention.size=96MB",
                ],
            )
            for name in self._containers:
                _docker(["start", name])
            self._wait_ready()
            health = object_value(self.api("/health"))
            self.lease_deadline_unixtime = finite_timestamp(
                health.get("lease_deadline_unixtime")
            )
            self.evidence["lease_deadline_unixtime"] = self.lease_deadline_unixtime
            self._capture_native_evidence()
            self._started = True
            self._write_evidence()
            return self
        except (MonitoringRuntimeError, OSError, ValueError):
            self._cleanup()
            raise

    def _wait_ready(self) -> None:
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            try:
                self.api("/health")
                self.prometheus_get("/api/v1/status/buildinfo")
                self.alertmanager_get("/api/v2/status")
                return
            except MonitoringRuntimeError:
                time.sleep(1)
        raise MonitoringRuntimeError("native_monitoring_readiness_timeout")

    def _capture_native_evidence(self) -> None:
        inspected_network = _json(_docker(["network", "inspect", self.network]))
        if not isinstance(inspected_network, list) or len(inspected_network) != 1:
            raise MonitoringRuntimeError("invalid_network_inspection")
        native_network = object_value(inspected_network[0])
        if (
            native_network.get("Internal") is not True
            or object_value(native_network.get("Labels")).get(LABEL) != self.owner
        ):
            raise MonitoringRuntimeError("native_internal_network_boundary_failed")
        self.evidence["native_network"] = {
            "id": native_network.get("Id"),
            "name": native_network.get("Name"),
            "internal": native_network.get("Internal"),
            "driver": native_network.get("Driver"),
        }
        containers = []
        for name in self._containers:
            inspected = _json(_docker(["inspect", name]))
            if (
                not isinstance(inspected, list)
                or len(inspected) != 1
                or not isinstance(inspected[0], dict)
            ):
                raise MonitoringRuntimeError("invalid_container_inspection")
            item = inspected[0]
            host = object_value(item.get("HostConfig"))
            config = object_value(item.get("Config"))
            network = object_value(item.get("NetworkSettings"))
            attached = object_value(network.get("Networks"))
            ports = object_value(network.get("Ports"))
            security_options = host.get("SecurityOpt")
            if (
                set(attached) != {self.network}
                or host.get("PortBindings") not in ({}, None)
                or any(value for value in ports.values())
                or host.get("ReadonlyRootfs") is not True
                or host.get("Privileged") is not False
                or host.get("CapDrop") != ["ALL"]
                or host.get("CapAdd") not in (None, [])
                or not isinstance(security_options, list)
                or "no-new-privileges" not in security_options
                or config.get("User") != "65534:65534"
            ):
                raise MonitoringRuntimeError("native_container_boundary_failed")
            containers.append(
                {
                    "name": name,
                    "id": item.get("Id"),
                    "image_id": item.get("Image"),
                    "host_config": {
                        key: host.get(key)
                        for key in (
                            "Memory",
                            "MemoryReservation",
                            "MemorySwap",
                            "NanoCpus",
                            "PidsLimit",
                            "ReadonlyRootfs",
                            "CapDrop",
                            "SecurityOpt",
                            "Tmpfs",
                            "PortBindings",
                            "Privileged",
                            "NetworkMode",
                        )
                    },
                    "user": config.get("User"),
                    "networks": list(attached),
                }
            )
        self.evidence["containers"] = containers
        self.evidence["docker_version"] = _docker(
            ["version", "--format", "{{.Server.Version}}"]
        )
        self.evidence["prometheus_buildinfo"] = self.prometheus_get(
            "/api/v1/status/buildinfo"
        )
        am = self.alertmanager_get("/api/v2/status")
        self.evidence["alertmanager_version"] = (
            am.get("versionInfo") if isinstance(am, dict) else None
        )
        self.evidence["python_version"] = _docker(
            ["exec", self.relay_container, "python", "--version"]
        )

    def update_observation(self, data: dict[str, object]) -> None:
        update_relay_observation(self.relay_container, data)

    def api(self, path: str) -> object:
        if path not in {"/health", "/inbox"}:
            raise MonitoringRuntimeError("relay_read_route_rejected")
        script = (
            "import json,urllib.request; o=urllib.request.build_opener(urllib.request.ProxyHandler({})); r=o.open("
            + repr("http://127.0.0.1:18790" + path)
            + ",timeout=5); print(r.read(1048577).decode())"
        )
        return _json(_docker(["exec", self.relay_container, "python", "-c", script]))

    def relay_get(self, path: str) -> object:
        return self.api(path)

    def _native_get(
        self, container: str, port: int, path: str, allowed: set[str]
    ) -> object:
        parsed = urlsplit(path)
        if (
            parsed.scheme
            or parsed.netloc
            or parsed.fragment
            or parsed.path not in allowed
        ):
            raise MonitoringRuntimeError("native_read_route_rejected")
        if parsed.query and (
            parsed.path != "/api/v1/query"
            or any(key not in {"query", "time"} for key, _ in parse_qsl(parsed.query))
        ):
            raise MonitoringRuntimeError("native_read_query_rejected")
        return _json(
            _docker(
                [
                    "exec",
                    container,
                    "/bin/wget",
                    "-q",
                    "-O",
                    "-",
                    "-T",
                    "5",
                    "http://127.0.0.1:" + str(port) + path,
                ]
            )
        )

    def prometheus_get(self, path: str) -> object:
        return self._native_get(
            self.prometheus_container,
            9090,
            path,
            {
                "/api/v1/alerts",
                "/api/v1/targets",
                "/api/v1/rules",
                "/api/v1/query",
                "/api/v1/status/buildinfo",
            },
        )

    def alertmanager_get(self, path: str) -> object:
        return self._native_get(
            self.alertmanager_container,
            9093,
            path,
            {"/api/v2/alerts", "/api/v2/status"},
        )

    def _cleanup(self) -> list[str]:
        if self._journal is not None:
            try:
                proof = recover_runtime(self.artifact_dir)
                self.evidence["cleanup"] = proof
                self._started = False
                self._write_evidence()
                return []
            except MonitoringRuntimeError:
                failures = (
                    self._checks
                    + self._containers
                    + ([self.network] if self._network_attempted else [])
                )
                self.evidence["cleanup"] = {
                    "complete": False,
                    "failed_resources": failures,
                }
                self._write_evidence()
                return failures
        failures = (
            self._checks
            + self._containers
            + ([self.network] if self._network_attempted else [])
        )
        self.evidence["cleanup"] = {
            "complete": not failures,
            "failed_resources": failures,
        }
        self._started = False
        self._write_evidence()
        return failures

    def stop(self) -> None:
        if self._cleanup():
            raise MonitoringRuntimeError("owned_resource_cleanup_incomplete")

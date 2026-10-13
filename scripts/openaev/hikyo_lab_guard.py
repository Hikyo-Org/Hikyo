"""Read-only native resource guards for an isolated GitHub-hosted ARM64 lab."""

from __future__ import annotations

import json
import os
import re
import subprocess
import threading
import time
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC, datetime

KUBE = (
    "kubectl",
    "--kubeconfig",
    os.environ.get("KUBECONFIG", ""),
    "--context",
    os.environ.get("OPENAEV_KUBE_CONTEXT", "default"),
)
LAB_NAMESPACES = ("openaev-lab", "hikyo-lab")
MEMORY_PROVIDER = "kubelet-summary"
_DNS_LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")


class GuardError(RuntimeError):
    """Sanitized diagnostic, without cluster response contents."""


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise GuardError("invalid_cluster_object")
    return dict(value)


def _items(value: object) -> list[dict[str, object]]:
    items = _object(value).get("items")
    if not isinstance(items, list):
        raise GuardError("invalid_cluster_list")
    return [_object(item) for item in items]


def quantity(value: object) -> int:
    if not isinstance(value, str):
        raise GuardError("invalid_memory_quantity")
    match = re.fullmatch(r"([0-9]+)(Ki|Mi|Gi|K|M|G)?", value)
    if match is None:
        raise GuardError("unsupported_memory_quantity")
    multiplier = {
        None: 1,
        "Ki": 1024,
        "Mi": 1024**2,
        "Gi": 1024**3,
        "K": 1000,
        "M": 1000**2,
        "G": 1000**3,
    }[match.group(2)]
    return int(match.group(1)) * multiplier


def kube(args: tuple[str, ...]) -> object:
    try:
        completed = subprocess.run(
            KUBE + args, capture_output=True, text=True, check=True, timeout=5
        )
        return json.loads(completed.stdout)
    except (OSError, ValueError, subprocess.SubprocessError):
        raise GuardError("cluster_read_failed") from None


def _node(value: object) -> str:
    if (
        not isinstance(value, str)
        or not 0 < len(value) <= 253
        or any(_DNS_LABEL.fullmatch(label) is None for label in value.split("."))
    ):
        raise GuardError("invalid_node_name")
    return value


def _memory_age(timestamp: object) -> tuple[str, float]:
    if not isinstance(timestamp, str):
        raise GuardError("metrics_timestamp_missing")
    try:
        collected = datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
        if collected.tzinfo is None:
            raise ValueError("missing timezone")
        age = (datetime.now(UTC) - collected).total_seconds()
    except (ValueError, TypeError):
        raise GuardError("metrics_timestamp_invalid") from None
    if not -5 <= age <= 30:
        raise GuardError("metrics_stale")
    return timestamp, age


def _pods(document: object, namespace: str) -> dict[str, object]:
    result: dict[str, object] = {}
    for pod in _items(document):
        meta, spec, status = (
            _object(pod.get(k)) for k in ("metadata", "spec", "status")
        )
        if meta.get("deletionTimestamp") is not None:
            raise GuardError("pod_terminating")
        conditions = status.get("conditions")
        if not isinstance(conditions, list):
            raise GuardError("pod_not_ready")
        ready_conditions = [
            _object(row) for row in conditions if _object(row).get("type") == "Ready"
        ]
        if len(ready_conditions) != 1 or ready_conditions[0].get("status") != "True":
            raise GuardError("pod_not_ready")
        containers = spec.get("containers")
        statuses = status.get("containerStatuses")
        if not isinstance(containers, list) or not isinstance(statuses, list):
            raise GuardError("pod_not_ready")
        states = [_object(row) for row in statuses]
        if (
            len(states) != len(containers)
            or not states
            or any(row.get("ready") is not True for row in states)
        ):
            raise GuardError("pod_not_ready")
        name, uid = meta.get("name"), meta.get("uid")
        if (
            not isinstance(name, str)
            or not name
            or not isinstance(uid, str)
            or not uid
            or meta.get("namespace") != namespace
        ):
            raise GuardError("pod_identity_missing")
        key = namespace + "/" + name
        if key in result:
            raise GuardError("duplicate_pod_identity")
        node = _node(spec.get("nodeName"))
        limits: dict[str, int] = {}
        container_names: set[str] = set()
        for row in containers:
            container_name = _object(row).get("name")
            if (
                not isinstance(container_name, str)
                or not container_name
                or container_name in container_names
            ):
                raise GuardError("container_identity_missing")
            container_names.add(container_name)
        restart_counts: dict[str, int] = {}
        for row in states:
            container_name, restarts = row.get("name"), row.get("restartCount")
            if (
                not isinstance(container_name, str)
                or container_name not in container_names
                or container_name in restart_counts
                or type(restarts) is not int
                or restarts < 0
            ):
                raise GuardError("container_status_identity_missing")
            restart_counts[container_name] = restarts
        if namespace in LAB_NAMESPACES:
            for row in containers:
                container = _object(row)
                container_name = container.get("name")
                if not isinstance(container_name, str):
                    raise GuardError("container_identity_missing")
                resources = _object(container.get("resources"))
                limits[container_name] = quantity(
                    _object(resources.get("limits")).get("memory")
                )
        result[key] = {
            "uid": uid,
            "node": node,
            "memory_limits": limits,
            "restarts": restart_counts,
        }
    if not result:
        raise GuardError("namespace_has_no_ready_pods")
    return result


def collect(
    read: Callable[[tuple[str, ...]], object] = kube, *, expected_lab_pods: int = 7
) -> dict[str, object]:
    if expected_lab_pods not in (6, 7):
        raise GuardError("invalid_expected_lab_pod_count")
    queries = [
        ("get", "namespace", "kube-system", "-o", "json"),
        ("get", "nodes", "-o", "json"),
    ]
    for namespace in LAB_NAMESPACES:
        queries.append(("get", "pods", "-n", namespace, "-o", "json"))
    with ThreadPoolExecutor(max_workers=4) as executor:
        documents = list(executor.map(read, queries))
    meta = _object(_object(documents[0]).get("metadata"))
    cluster_uid = meta.get("uid")
    if not isinstance(cluster_uid, str) or not cluster_uid:
        raise GuardError("runner_cluster_identity_missing")
    identities = []
    for row in _items(documents[1]):
        native = _object(row.get("metadata"))
        uid = native.get("uid")
        if not isinstance(uid, str) or not uid:
            raise GuardError("runner_node_identity_missing")
        info = _object(_object(row.get("status")).get("nodeInfo"))
        labels = _object(native.get("labels"))
        if labels.get("kubernetes.io/arch") != "arm64" or info.get("architecture") != "arm64":
            raise GuardError("native_arm64_runner_required")
        identities.append({"name": _node(native.get("name")), "uid": uid,
                           "architecture": info.get("architecture")})
    if len(identities) != 1:
        raise GuardError("isolated_single_node_runner_required")
    environment = {"scope": "isolated GitHub-hosted runner lab", "cluster_uid": cluster_uid,
                   "nodes": identities}
    pods: dict[str, object] = {}
    for index, namespace in enumerate(LAB_NAMESPACES):
        if expected_lab_pods == 6 and namespace == "hikyo-lab":
            if _items(documents[2 + index]):
                raise GuardError("hikyo_not_removed_at_idle_boundary")
            continue
        pods.update(_pods(documents[2 + index], namespace))
    if len(pods) != expected_lab_pods:
        raise GuardError("unexpected_lab_pod_count")
    nodes = sorted({_node(_object(row)["node"]) for row in pods.values()})
    summary_queries = [
        (
            "get",
            "--raw",
            f"/api/v1/nodes/{node}/proxy/stats/summary?only_cpu_and_memory=true",
        )
        for node in nodes
    ]
    with ThreadPoolExecutor(max_workers=len(nodes)) as executor:
        summaries = list(executor.map(read, summary_queries))
    memory: dict[str, object] = {}
    seen: set[str] = set()
    for node, document in zip(nodes, summaries, strict=True):
        summary = _object(document)
        if _object(summary.get("node")).get("nodeName") != node:
            raise GuardError("summary_node_identity_mismatch")
        rows = summary.get("pods")
        if not isinstance(rows, list):
            raise GuardError("invalid_cluster_list")
        for raw in rows:
            row = _object(raw)
            reference = _object(row.get("podRef"))
            metric_namespace, metric_pod_name = (
                reference.get("namespace"),
                reference.get("name"),
            )
            if not isinstance(metric_namespace, str) or not isinstance(
                metric_pod_name, str
            ):
                raise GuardError("metrics_pod_identity_missing")
            key = metric_namespace + "/" + metric_pod_name
            if key not in pods:
                continue
            identity = _object(pods[key])
            if identity["node"] != node or reference.get("uid") != identity["uid"]:
                raise GuardError("metrics_pod_identity_mismatch")
            if key in seen:
                raise GuardError("duplicate_pod_metrics")
            seen.add(key)
            limits = _object(identity["memory_limits"])
            metric_containers = row.get("containers")
            if not isinstance(metric_containers, list):
                raise GuardError("container_metrics_missing")
            observed: set[str] = set()
            for container_row in metric_containers:
                container = _object(container_row)
                name = container.get("name")
                if not isinstance(name, str) or name not in limits:
                    raise GuardError("container_metrics_identity_mismatch")
                if name in observed:
                    raise GuardError("duplicate_container_metrics")
                observed.add(name)
                stats = _object(container.get("memory"))
                timestamp, age = _memory_age(stats.get("time"))
                used, limit = stats.get("workingSetBytes"), limits[name]
                if type(used) is not int or used < 0:
                    raise GuardError("invalid_memory_sample")
                if type(limit) is not int or limit <= 0 or used * 100 >= limit * 85:
                    raise GuardError("lab_memory_pressure")
                memory[key + "/" + name] = {
                    "used_bytes": used,
                    "limit_bytes": limit,
                    "metrics_age_s": round(age, 3),
                    "metrics_timestamp": timestamp,
                    "provider": MEMORY_PROVIDER,
                }
            if observed != set(limits):
                raise GuardError("container_metrics_incomplete")
    if seen != set(pods):
        raise GuardError("pod_metrics_incomplete")
    return {
        "environment": environment,
        "lab_pods": pods,
        "memory": memory,
        "memory_provider": MEMORY_PROVIDER,
        "memory_scope": "per-container workingSetBytes with native memory.time; sampled data, not instantaneous peak",
        "checked_at": datetime.now(UTC).isoformat(),
    }


class LabGuard:
    def __init__(self, baseline: dict[str, object]):
        self.expected_lab_pods = len(_object(baseline.get("lab_pods")))
        if self.expected_lab_pods not in (6, 7):
            raise GuardError("invalid_expected_lab_pod_count")
        self.baseline = baseline
        self.checks = 0
        self.peak_memory: dict[str, int] = {}
        self.last: dict[str, object] | None = None

    def verify(self, sample: dict[str, object]) -> None:
        if sample.get("environment") != self.baseline.get("environment"):
            raise GuardError("runner_cluster_identity_changed")
        if sample.get("lab_pods") != self.baseline.get("lab_pods"):
            raise GuardError("lab_pod_identity_or_restarts_changed")
        for key, row in _object(sample.get("memory")).items():
            used = _object(row).get("used_bytes")
            if type(used) is not int or used < 0:
                raise GuardError("invalid_memory_sample")
            self.peak_memory[key] = max(used, self.peak_memory.get(key, 0))
        self.last = sample
        self.checks += 1

    def collect_sample(self) -> dict[str, object]:
        return collect(expected_lab_pods=self.expected_lab_pods)

    def __call__(self) -> None:
        self.verify(self.collect_sample())


class BackgroundGuard:
    """Serialized native polling with fast, fail-closed cached load callbacks.

    The collector retains its native command timeouts. Request-phase deadlines
    apply to the callback, which performs no network requests. A sample's age
    starts when native collection starts, not after slow collection completes.
    Native memory timestamps are revalidated every time the cache is consumed.
    """

    def __init__(
        self,
        guard: LabGuard,
        *,
        collect_sample: Callable[[], dict[str, object]] | None = None,
        cadence_s: float = 5.0,
    ) -> None:
        if not 0 < cadence_s <= 5.0:
            raise GuardError("invalid_background_guard_cadence")
        self.guard, self.collect_sample, self.cadence_s = (
            guard,
            guard.collect_sample if collect_sample is None else collect_sample,
            cadence_s,
        )
        self._lock = threading.Lock()
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self._failure: GuardError | None = None
        self._sample: dict[str, object] | None = None
        self._collection_started: float | None = None
        self._finished = False
        self.native_checks = 0
        self.cached_checks = 0
        self.max_collection_duration_s = 0.0
        self.started_at: str | None = None
        self.finished_at: str | None = None

    @staticmethod
    def _validate_current(sample: dict[str, object]) -> None:
        memory = _object(sample.get("memory"))
        if not memory or sample.get("memory_provider") != MEMORY_PROVIDER:
            raise GuardError("background_memory_samples_missing")
        for raw in memory.values():
            row = _object(raw)
            if row.get("provider") != MEMORY_PROVIDER:
                raise GuardError("background_memory_provider_mismatch")
            _memory_age(row.get("metrics_timestamp"))
            used, limit = row.get("used_bytes"), row.get("limit_bytes")
            if (
                type(used) is not int
                or used < 0
                or type(limit) is not int
                or limit <= 0
            ):
                raise GuardError("invalid_memory_sample")
            if used * 100 >= limit * 85:
                raise GuardError("lab_memory_pressure")

    def _refresh(self) -> None:
        began = time.monotonic()
        try:
            sample = self.collect_sample()
            self._validate_current(sample)
            with self._lock:
                self.guard.verify(sample)
                self._sample, self._collection_started = sample, began
                self.native_checks += 1
                self.max_collection_duration_s = max(
                    self.max_collection_duration_s, time.monotonic() - began
                )
        except Exception as error:
            # Collector callbacks are a failure boundary: every ordinary crash
            # must remain fatal even if a later native collection succeeds.
            failure = (
                error
                if isinstance(error, GuardError)
                else GuardError("background_collection_failed")
            )
            with self._lock:
                if self._failure is None:
                    self._failure = failure
            raise failure from None

    def _worker(self) -> None:
        with self._lock:
            began = self._collection_started
        if began is None:
            return
        next_due = began + self.cadence_s
        while not self._stop.wait(max(0.0, next_due - time.monotonic())):
            began = time.monotonic()
            try:
                self._refresh()
            except GuardError:
                self._stop.set()
                return
            next_due = began + self.cadence_s  # Slow polls never overlap or queue.

    def start(self) -> None:
        if self._thread is not None or self._finished:
            raise GuardError("background_guard_already_started")
        self.started_at = datetime.now(UTC).isoformat()
        self._refresh()  # Mandatory fresh native collection before dispatch.
        self()
        self._thread = threading.Thread(
            target=self._worker, name="hikyo-lab-health", daemon=True
        )
        self._thread.start()

    def __call__(self) -> None:
        with self._lock:
            if self._failure is not None:
                raise self._failure
            sample, began = self._sample, self._collection_started
            try:
                if sample is None or began is None:
                    raise GuardError("background_guard_has_no_sample")
                age = time.monotonic() - began
                if not 0 <= age <= 15.0:
                    raise GuardError("background_collection_stale")
                self._validate_current(sample)
            except GuardError as error:
                self._failure = error
                raise
            self.cached_checks += 1

    def finish(self) -> None:
        if self._finished:
            raise GuardError("background_guard_already_finished")
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=15.0)
            if self._thread.is_alive():
                with self._lock:
                    self._failure = GuardError("background_worker_did_not_stop")
                raise self._failure
        self._finished = True
        try:
            self._refresh()  # Mandatory final native collection, outside load deadline.
            self()  # A previous failed poll remains fatal even if this read succeeds.
        finally:
            self.finished_at = datetime.now(UTC).isoformat()

    def evidence(self) -> dict[str, object]:
        with self._lock:
            return {
                "native_checks": self.native_checks,
                "cached_checks": self.cached_checks,
                "cadence_s": self.cadence_s,
                "max_collection_age_s": 15.0,
                "max_native_memory_age_s": 30.0,
                "max_collection_duration_s": round(self.max_collection_duration_s, 3),
                "started_at": self.started_at,
                "finished_at": self.finished_at,
                "worker_stopped": self._thread is None or not self._thread.is_alive(),
                "failure": None if self._failure is None else str(self._failure),
            }

"""Guard regressions: pressure, stale telemetry and changed workloads fail closed."""

from __future__ import annotations

import copy
import threading
import time
import unittest
from datetime import UTC, datetime, timedelta
from unittest.mock import patch

import hikyo_lab_guard as guard


def pod(name: str, namespace: str) -> dict[str, object]:
    return {
        "metadata": {"name": name, "namespace": namespace, "uid": "uid-" + name},
        "spec": {
            "nodeName": "node",
            "containers": [
                {"name": "server", "resources": {"limits": {"memory": "384Mi"}}}
            ],
        },
        "status": {
            "conditions": [{"type": "Ready", "status": "True"}],
            "containerStatuses": [{"name": "server", "ready": True, "restartCount": 0}],
        },
    }


class GuardTests(unittest.TestCase):
    def setUp(self) -> None:
        self.documents: dict[tuple[str, ...], object] = {}
        self.calls: list[tuple[str, ...]] = []
        self.documents[("get", "namespace", "kube-system", "-o", "json")] = {"metadata": {"uid": "runner-cluster"}}
        self.documents[("get", "nodes", "-o", "json")] = {"items": [{"metadata": {"name": "node", "uid": "node-uid", "labels": {"kubernetes.io/arch": "arm64"}}, "status": {"nodeInfo": {"architecture": "arm64"}}}]}
        samples: list[dict[str, object]] = []
        for namespace, count in (
            ("hikyo-dbugit", 1),
            ("openaev-lab", 6),
            ("hikyo-lab", 1),
        ):
            names = [namespace + "-" + str(index) for index in range(count)]
            self.documents[("get", "pods", "-n", namespace, "-o", "json")] = {
                "items": [pod(name, namespace) for name in names]
            }
            if namespace in guard.LAB_NAMESPACES:
                samples.extend(
                    {
                        "podRef": {
                            "name": name,
                            "namespace": namespace,
                            "uid": "uid-" + name,
                        },
                        "containers": [
                            {
                                "name": "server",
                                "memory": {
                                    "time": datetime.now(UTC).isoformat(),
                                    "workingSetBytes": 100 * 1024**2,
                                },
                            }
                        ],
                    }
                    for name in names
                )
        self.summary_query = (
            "get",
            "--raw",
            "/api/v1/nodes/node/proxy/stats/summary?only_cpu_and_memory=true",
        )
        self.documents[self.summary_query] = {
            "node": {"nodeName": "node"},
            "pods": samples,
        }

    def read(self, args: tuple[str, ...]) -> object:
        self.calls.append(args)
        return self.documents[args]

    def collect(self) -> dict[str, object]:
        return guard.collect(self.read)

    def metrics(self) -> list[dict[str, object]]:
        value = guard._object(self.documents[self.summary_query])["pods"]
        if not isinstance(value, list):
            raise TypeError("invalid test summary")
        return [guard._object(row) for row in value]

    def change_metrics(self, rows: list[dict[str, object]]) -> None:
        self.documents[self.summary_query] = {
            "node": {"nodeName": "node"},
            "pods": rows,
        }

    def change_memory(self, **changes: object) -> None:
        rows = self.metrics()
        containers = rows[-1]["containers"]
        if not isinstance(containers, list):
            raise TypeError("invalid test containers")
        container = guard._object(containers[0])
        memory = guard._object(container["memory"])
        memory.update(changes)
        container["memory"] = memory
        rows[-1]["containers"] = [container]
        self.change_metrics(rows)

    def test_stable_baseline_accepts_and_tracks_observed_peak(self) -> None:
        baseline = self.collect()
        verifier = guard.LabGuard(baseline)
        verifier.verify(self.collect())
        self.assertEqual(verifier.checks, 1)
        self.assertEqual(len(verifier.peak_memory), 7)
        self.assertEqual(baseline["memory_provider"], "kubelet-summary")
        for raw in guard._object(baseline["memory"]).values():
            sample = guard._object(raw)
            self.assertEqual(sample["provider"], "kubelet-summary")
            self.assertIsInstance(sample["metrics_timestamp"], str)
        self.assertEqual(self.calls.count(self.summary_query), 2)
        self.assertFalse(
            any("metrics.k8s.io" in arg for args in self.calls for arg in args)
        )

    def test_x86_or_missing_node_architecture_cannot_claim_native_arm64(self) -> None:
        query = ("get", "nodes", "-o", "json")
        for architecture in ("amd64", None):
            self.documents[query] = {"items": [{"metadata": {"name": "node", "uid": "node-uid", "labels": {"kubernetes.io/arch": "arm64"}}, "status": {"nodeInfo": {"architecture": architecture}}}]}
            with self.assertRaisesRegex(guard.GuardError, "native_arm64_runner_required"):
                self.collect()

    def test_missing_or_invalid_native_node_uid_is_rejected(self) -> None:
        query = ("get", "nodes", "-o", "json")
        original = copy.deepcopy(self.documents[query])
        for uid in (None, "", 42):
            changed = copy.deepcopy(original)
            changed["items"][0]["metadata"]["uid"] = uid
            self.documents[query] = changed
            with self.subTest(uid=uid), self.assertRaisesRegex(
                guard.GuardError, "runner_node_identity_missing"
            ):
                self.collect()

    def test_idle_guard_collects_the_six_pod_boundary(self) -> None:
        self.documents[("get", "pods", "-n", "hikyo-lab", "-o", "json")] = {"items": []}
        self.change_metrics(self.metrics()[:-1])
        baseline = guard.collect(self.read, expected_lab_pods=6)
        verifier = guard.LabGuard(baseline)
        with patch.object(guard, "collect", return_value=baseline) as collect:
            verifier()
            collect.assert_called_once_with(expected_lab_pods=6)
        background = guard.BackgroundGuard(verifier)
        with patch.object(guard, "collect", return_value=baseline) as collect:
            background.collect_sample()
            collect.assert_called_once_with(expected_lab_pods=6)

    def test_85_percent_pressure_is_rejected(self) -> None:
        threshold = (384 * 1024**2 * 85 + 99) // 100
        self.change_memory(workingSetBytes=threshold - 1)
        self.collect()
        self.change_memory(workingSetBytes=threshold)
        with self.assertRaisesRegex(guard.GuardError, "lab_memory_pressure"):
            self.collect()

    def test_stale_memory_cannot_cover_whole_phase(self) -> None:
        self.change_memory(time=(datetime.now(UTC) - timedelta(seconds=31)).isoformat())
        document = guard._object(self.documents[self.summary_query])
        document["node"] = {
            "nodeName": "node",
            "memory": {"time": datetime.now(UTC).isoformat()},
        }
        self.documents[self.summary_query] = document
        with self.assertRaisesRegex(guard.GuardError, "metrics_stale"):
            self.collect()

    def test_missing_container_metric_is_rejected(self) -> None:
        rows = self.metrics()
        rows[0]["containers"] = []
        self.change_metrics(rows)
        with self.assertRaisesRegex(guard.GuardError, "container_metrics_incomplete"):
            self.collect()

    def test_native_timestamp_future_naive_invalid_or_missing_is_rejected(self) -> None:
        for timestamp in (
            (datetime.now(UTC) + timedelta(seconds=6)).isoformat(),
            "2026-10-07T06:00:00",
            "invalid",
            None,
        ):
            with self.subTest(timestamp=timestamp):
                self.change_memory(time=timestamp)
                with self.assertRaises(guard.GuardError):
                    self.collect()

    def test_working_set_requires_nonnegative_integer_not_bool(self) -> None:
        for used in (True, False, -1, 1.0, "100", None):
            with self.subTest(used=used):
                self.change_memory(workingSetBytes=used)
                with self.assertRaisesRegex(guard.GuardError, "invalid_memory_sample"):
                    self.collect()

    def test_duplicate_or_unexpected_container_metrics_are_rejected(self) -> None:
        baseline = self.metrics()
        containers = baseline[-1]["containers"]
        if not isinstance(containers, list):
            raise TypeError("invalid test containers")
        for changed, code in (
            (containers + containers, "duplicate_container_metrics"),
            (containers + [{"name": "foreign"}], "container_metrics_identity_mismatch"),
        ):
            with self.subTest(code=code):
                rows = copy.deepcopy(baseline)
                rows[-1]["containers"] = changed
                self.change_metrics(rows)
                with self.assertRaisesRegex(guard.GuardError, code):
                    self.collect()

    def test_pod_uid_mismatch_duplicate_or_missing_metrics_are_rejected(self) -> None:
        baseline = self.metrics()
        changed = copy.deepcopy(baseline)
        reference = guard._object(changed[-1]["podRef"])
        reference["uid"] = "stale-replaced-pod"
        changed[-1]["podRef"] = reference
        for rows, code in (
            (changed, "metrics_pod_identity_mismatch"),
            (baseline + [copy.deepcopy(baseline[-1])], "duplicate_pod_metrics"),
            (baseline[:-1], "pod_metrics_incomplete"),
        ):
            with self.subTest(code=code):
                self.change_metrics(rows)
                with self.assertRaisesRegex(guard.GuardError, code):
                    self.collect()

    def test_foreign_namespace_sample_is_filtered_not_used(self) -> None:
        foreign: dict[str, object] = {
            "podRef": {
                "name": "hikyo-lab-0",
                "namespace": "environment",
                "uid": "foreign",
            },
            "containers": [],
        }
        self.change_metrics(self.metrics() + [foreign])
        self.assertEqual(len(guard._object(self.collect()["memory"])), 7)

    def test_summary_node_must_match_requested_live_node(self) -> None:
        document = guard._object(self.documents[self.summary_query])
        document["node"] = {"nodeName": "foreign-node"}
        self.documents[self.summary_query] = document
        with self.assertRaisesRegex(guard.GuardError, "summary_node_identity_mismatch"):
            self.collect()

    def test_invalid_live_node_name_cannot_modify_proxy_route(self) -> None:
        query = ("get", "pods", "-n", "hikyo-lab", "-o", "json")
        baseline = guard._items(self.documents[query])
        for node in ("node/../../foreign", "Node", "-node", "node..test", "n" * 64):
            with self.subTest(node=node):
                rows = copy.deepcopy(baseline)
                spec = guard._object(rows[0]["spec"])
                spec["nodeName"] = node
                rows[0]["spec"] = spec
                self.documents[query] = {"items": rows}
                with self.assertRaisesRegex(guard.GuardError, "invalid_node_name"):
                    self.collect()
                self.assertNotIn(self.summary_query, self.calls)

    def test_unique_nodes_read_once_and_each_pod_checked_on_its_node(self) -> None:
        rows = self.metrics()
        last = rows[-1]
        self.change_metrics(rows[:-1])
        query = ("get", "pods", "-n", "hikyo-lab", "-o", "json")
        live = guard._items(self.documents[query])
        spec = guard._object(live[0]["spec"])
        spec["nodeName"] = "node-two"
        live[0]["spec"] = spec
        self.documents[query] = {"items": live}
        second = (
            "get",
            "--raw",
            "/api/v1/nodes/node-two/proxy/stats/summary?only_cpu_and_memory=true",
        )
        self.documents[second] = {"node": {"nodeName": "node-two"}, "pods": [last]}
        self.assertEqual(len(guard._object(self.collect()["memory"])), 7)
        self.assertEqual(self.calls.count(self.summary_query), 1)
        self.assertEqual(self.calls.count(second), 1)
        self.change_metrics(rows)
        with self.assertRaisesRegex(guard.GuardError, "metrics_pod_identity_mismatch"):
            self.collect()

    def test_changed_environment_spec_or_identity_is_rejected(self) -> None:
        baseline = self.collect()
        verifier = guard.LabGuard(baseline)
        for field in ("cluster_uid", "nodes", "scope"):
            sample = copy.deepcopy(baseline)
            environment = guard._object(sample["environment"])
            environment[field] = "changed"
            sample["environment"] = environment
            with self.assertRaisesRegex(
                guard.GuardError, "runner_cluster_identity_changed"
            ):
                verifier.verify(sample)

    def test_lab_restart_or_pod_replacement_is_rejected(self) -> None:
        baseline = self.collect()
        verifier = guard.LabGuard(baseline)
        sample = copy.deepcopy(baseline)
        pods = guard._object(sample["lab_pods"])
        name = next(iter(pods))
        changed = guard._object(pods[name])
        changed["restarts"] = {"server": 1}
        pods[name] = changed
        sample["lab_pods"] = pods
        with self.assertRaisesRegex(
            guard.GuardError, "lab_pod_identity_or_restarts_changed"
        ):
            verifier.verify(sample)

    def test_unbounded_or_invalid_quantity_is_rejected(self) -> None:
        for value in ("1Ti", "-1Mi", "NaN", None):
            with self.assertRaises(guard.GuardError):
                guard.quantity(value)

    def test_pod_ready_condition_required_even_when_containers_ready(self) -> None:
        query = ("get", "pods", "-n", "hikyo-lab", "-o", "json")
        baseline = guard._items(self.documents[query])
        for conditions in (
            [],
            [{"type": "Ready", "status": "False"}],
            [{"type": "Ready", "status": "Unknown"}],
            [{"type": "Ready", "status": "True"}, {"type": "Ready", "status": "True"}],
        ):
            with self.subTest(conditions=conditions):
                rows = copy.deepcopy(baseline)
                status = guard._object(rows[0]["status"])
                status["conditions"] = conditions
                rows[0]["status"] = status
                self.documents[query] = {"items": rows}
                with self.assertRaisesRegex(guard.GuardError, "pod_not_ready"):
                    self.collect()
                self.assertNotIn(self.summary_query, self.calls)

    def test_background_cache_callback_performs_no_native_reads(self) -> None:
        baseline = self.collect()
        verifier = guard.LabGuard(baseline)
        background = guard.BackgroundGuard(verifier, collect_sample=self.collect)
        background.start()
        before = len(self.calls)
        began = time.monotonic()
        for _ in range(20):
            background()
        self.assertLess(time.monotonic() - began, 0.1)
        self.assertEqual(len(self.calls), before)
        background.finish()
        self.assertEqual(background.native_checks, 2)
        self.assertEqual(verifier.checks, 2)
        self.assertTrue(background.evidence()["worker_stopped"])

    def test_background_cache_revalidates_native_timestamp_age(self) -> None:
        baseline = self.collect()
        background = guard.BackgroundGuard(
            guard.LabGuard(baseline), collect_sample=self.collect
        )
        background.start()
        sample = background._sample
        self.assertIsNotNone(sample)
        if sample is None:
            raise AssertionError("initial sample absent")
        stale = copy.deepcopy(sample)
        memory = guard._object(stale["memory"])
        first_key = next(iter(memory))
        row = guard._object(memory[first_key])
        row["metrics_timestamp"] = (
            datetime.now(UTC) - timedelta(seconds=31)
        ).isoformat()
        memory[first_key], stale["memory"] = row, memory
        background._sample = stale
        with self.assertRaisesRegex(guard.GuardError, "metrics_stale"):
            background()
        with self.assertRaisesRegex(guard.GuardError, "metrics_stale"):
            background.finish()

    def test_background_collection_age_starts_before_slow_collection(self) -> None:
        baseline = self.collect()
        background = guard.BackgroundGuard(
            guard.LabGuard(baseline), collect_sample=self.collect
        )
        background.start()
        began = background._collection_started
        if began is None:
            raise AssertionError("collection start absent")
        with (
            patch.object(guard.time, "monotonic", return_value=began + 15.001),
            self.assertRaisesRegex(guard.GuardError, "background_collection_stale"),
        ):
            background()
        with self.assertRaisesRegex(guard.GuardError, "background_collection_stale"):
            background.finish()

    def test_background_native_failure_is_sticky_and_cached_checks_fail_immediately(
        self,
    ) -> None:
        baseline = self.collect()
        failed = threading.Event()
        calls = 0

        def collector() -> dict[str, object]:
            nonlocal calls
            calls += 1
            if calls == 2:
                failed.set()
                raise guard.GuardError("cluster_read_failed")
            return self.collect()

        background = guard.BackgroundGuard(
            guard.LabGuard(baseline), collect_sample=collector, cadence_s=0.01
        )
        background.start()
        self.assertTrue(failed.wait(1.0))
        # Join guarantees failure publication has completed before the assertion.
        if background._thread is not None:
            background._thread.join(1.0)
        with self.assertRaisesRegex(guard.GuardError, "cluster_read_failed"):
            background()
        with self.assertRaisesRegex(guard.GuardError, "cluster_read_failed"):
            background.finish()
        self.assertEqual(background.native_checks, 2)  # Initial plus final read.
        self.assertTrue(background.evidence()["worker_stopped"])

    def test_background_polls_are_serialized_and_stop_before_final_read(self) -> None:
        baseline = self.collect()
        entered = threading.Event()
        release = threading.Event()
        active = 0
        max_active = 0
        calls = 0
        lock = threading.Lock()

        def collector() -> dict[str, object]:
            nonlocal active, max_active, calls
            with lock:
                active += 1
                max_active = max(active, max_active)
                calls += 1
                number = calls
            try:
                if number == 2:
                    entered.set()
                    if not release.wait(1.0):
                        raise guard.GuardError("test_worker_not_released")
                return self.collect()
            finally:
                with lock:
                    active -= 1

        background = guard.BackgroundGuard(
            guard.LabGuard(baseline), collect_sample=collector, cadence_s=0.01
        )
        background.start()
        self.assertTrue(entered.wait(1.0))
        began = time.monotonic()
        background()
        self.assertLess(time.monotonic() - began, 0.1)
        release.set()
        background.finish()
        self.assertEqual(max_active, 1)
        self.assertGreaterEqual(background.native_checks, 3)
        self.assertTrue(background.evidence()["worker_stopped"])

    def test_background_final_read_rechecks_environment_identity(self) -> None:
        baseline = self.collect()
        background = guard.BackgroundGuard(
            guard.LabGuard(baseline), collect_sample=self.collect
        )
        background.start()
        query = ("get", "namespace", "kube-system", "-o", "json")
        changed = guard._object(self.documents[query])
        metadata = guard._object(changed["metadata"])
        metadata["uid"] = "replacement"
        changed["metadata"] = metadata
        self.documents[query] = changed
        with self.assertRaisesRegex(guard.GuardError, "runner_cluster_identity_changed"):
            background.finish()
        self.assertTrue(background.evidence()["worker_stopped"])

    def test_idle_six_allows_only_empty_hikyo_and_keeps_native_openaev_checks(
        self,
    ) -> None:
        self.documents[("get", "pods", "-n", "hikyo-lab", "-o", "json")] = {"items": []}
        self.change_metrics(self.metrics()[:-1])
        sample = guard.collect(self.read, expected_lab_pods=6)
        self.assertEqual(len(guard._object(sample["lab_pods"])), 6)
        self.assertEqual(len(guard._object(sample["memory"])), 6)
        self.assertEqual(sample["memory_provider"], guard.MEMORY_PROVIDER)
        with self.assertRaisesRegex(guard.GuardError, "namespace_has_no_ready_pods"):
            self.collect()

    def test_idle_six_refuses_hikyo_instead_of_hiding_extra_workload(self) -> None:
        with self.assertRaisesRegex(
            guard.GuardError, "hikyo_not_removed_at_idle_boundary"
        ):
            guard.collect(self.read, expected_lab_pods=6)


if __name__ == "__main__":
    unittest.main()

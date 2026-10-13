"""Coordinator regressions for truthful counts and interrupted-run recovery."""

from __future__ import annotations

import copy
import json
import os
import tempfile
import unittest
import uuid
from pathlib import Path
from unittest.mock import Mock, patch

import hikyo_nightly_release as release
import hikyo_nightly_runner as runner


def load_report() -> dict[str, object]:
    phases = []
    for name, count, cycle in runner.PHASES:
        rows = []
        for index in range(count):
            case, statuses = cycle[index % len(cycle)]
            status = (
                429
                if name == "native-principal-throttling" and index == 99
                else statuses[0]
            )
            rows.append(
                {
                    "index": index,
                    "case": case,
                    "observation": {
                        "status": status,
                        "elapsed_ms": 10.0,
                        "response_bytes": 20,
                        "body_verified": True,
                        "secret_absent": True,
                        "native_retry_after": 1 if status == 429 else None,
                    },
                }
            )
        phases.append(
            {
                "name": name,
                "requested": count,
                "completed": count,
                "observations": rows,
                "failure": None,
                "undrained_workers": 0,
                "health_checks": 2,
            }
        )
    return {
        "load_phases": phases,
        "throttling_recovery": {
            "observed_429": 1,
            "same_principal_status": 200,
            "other_principal_control": 200,
        },
    }


class RunnerTests(unittest.TestCase):
    def test_public_diagnostics_allow_only_closed_codes(self) -> None:
        self.assertEqual(runner.public_error_code(RuntimeError("private token=value")), "unexpected_failure")
        self.assertEqual(runner.public_error_code(runner.monitoring_runtime.MonitoringRuntimeError("docker_socket_owner_invalid")), "docker_socket_owner_invalid")

    def test_stale_or_symlink_pass_is_rejected_before_any_execution(self) -> None:
        for symlink in (False, True):
            with self.subTest(symlink=symlink), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                attestation = root / "validated-pass.json"
                if symlink:
                    attestation.symlink_to(root / "absent")
                else:
                    attestation.write_text('{"status":"PASS"}')
                with (patch.object(runner.lifecycle, "EphemeralHikyo") as target,
                      patch.object(runner.preparation, "prepare_candidate") as prepare,
                      self.assertRaisesRegex(runner.RunnerError, "fresh_pass_attestation_required")):
                    runner.execute(root / "exercise", release_json=root / "release.json",
                                   tag="v1.0.0", source_sha="a" * 40,
                                   image_ref="ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64)
                target.assert_not_called()
                prepare.assert_not_called()
                self.assertFalse((root / "exercise").exists())

    def test_infrastructure_failure_creates_no_public_pass_and_cleans_owned_target(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "release.json"
            source.write_text(json.dumps({"id": 1, "tag_name": "v1.0.0-rc.1", "draft": False,
                                          "published_at": "2026-10-13T10:00:00Z"}))
            source.chmod(0o600)
            ca = root / "ca.crt"
            ca.write_text("unit test never performs TLS")
            target = Mock()
            target.cleanup.return_value = {"verified": True}
            with (patch.dict(os.environ, {"KUBECONFIG": "private-runner-config",
                                           "OPENAEV_PLATFORM_TOKEN_FILE": "private-platform-token"}),
                  patch.object(runner, "CA", ca),
                  patch.object(runner.monitoring_runtime, "prepare_images"),
                  patch.object(runner.lifecycle, "EphemeralHikyo", return_value=target),
                  patch.object(runner.health, "collect", side_effect=runner.health.GuardError("lab_memory_pressure")),
                  patch.object(runner.preparation, "prepare_candidate") as prepare,
                  self.assertRaisesRegex(runner.RunnerError, "not_complete_pass")):
                runner.execute(root / "exercise", release_json=source, tag="v1.0.0-rc.1",
                               source_sha="a" * 40, image_ref="ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64)
            target.cleanup.assert_called_once()
            prepare.assert_not_called()
            self.assertFalse((root / "validated-pass.json").exists())
            summary = runner.read_json(root / "exercise/summary.json")
            self.assertEqual(summary["status"], "INFRA ERROR")

    def test_assertion_inventory_remains_313(self) -> None:
        suites = (runner.atomic_results("Authorization", runner.SECURITY, None),
                  runner.atomic_results("Adversarial", runner.ADVERSARIAL, None),
                  runner.load_results(None), runner.load_control_results(None),
                  runner.detection_result(None))
        self.assertEqual([len(suite.tests) for suite in suites], [24, 22, 260, 6, 1])
        self.assertEqual(sum(len(suite.tests) for suite in suites), 313)



    def test_completed_requests_with_failed_guard_are_not_green(self) -> None:
        report = load_report()
        report["load_health"] = [
            {
                "phase": name,
                "worker_stopped": True,
                "failure": None,
                "native_checks": 3,
                "max_collection_age_s": 15.0,
                "max_native_memory_age_s": 30.0,
                "started_at": "2026-10-09T10:00:00Z",
                "finished_at": "2026-10-09T10:00:05Z",
            }
            for name, _, _ in runner.PHASES
        ]
        suites = (runner.load_results(report), runner.load_control_results(report))
        self.assertEqual("PASS", release.outcome(suites, ()))
        for mutation in (
            "phase-failure",
            "worker-running",
            "missing-native",
            "native-failure",
        ):
            changed = copy.deepcopy(report)
            phases, native = changed["load_phases"], changed["load_health"]
            if not isinstance(phases, list) or not isinstance(native, list):
                raise TypeError()
            if mutation == "phase-failure":
                phases[0]["failure"] = "health_timeout"
                phases[0]["undrained_workers"] = 1
            elif mutation == "worker-running":
                native[0]["worker_stopped"] = False
            elif mutation == "missing-native":
                changed["load_health"] = []
            else:
                native[0]["failure"] = "lab_memory_pressure"
            with self.subTest(mutation=mutation):
                observations = runner.load_results(changed)
                self.assertEqual(
                    260, sum(row.status == "passed" for row in observations.tests)
                )
                self.assertEqual(
                    "INFRA ERROR",
                    release.outcome(
                        (observations, runner.load_control_results(changed)), ()
                    ),
                )

    def test_real_load_observations_are_counted(self) -> None:
        result = runner.load_results(load_report())
        self.assertEqual(260, len(result.tests))
        self.assertTrue(
            all(
                row.status == "passed" and row.evidence_verified for row in result.tests
            )
        )

    def test_missing_load_cannot_pass(self) -> None:
        result = runner.load_results(None)
        self.assertEqual(260, sum(row.status == "skipped" for row in result.tests))
        self.assertEqual("INCOMPLETE", release.outcome((result,), ()))

    def test_load_tampering_and_missing_recovery_rejected(self) -> None:
        for mutation in ("duplicate", "body", "count", "status", "recovery"):
            report = load_report()
            phases = report["load_phases"]
            self.assertIsInstance(phases, list)
            if not isinstance(phases, list):
                raise TypeError()
            row = phases[0]["observations"][0]
            if mutation == "duplicate":
                phases[0]["observations"][1]["index"] = 0
            elif mutation == "body":
                row["observation"]["body_verified"] = False
            elif mutation == "count":
                phases[0]["completed"] = 39
            elif mutation == "status":
                row["observation"]["status"] = 401
            else:
                report.pop("throttling_recovery")
            with self.subTest(mutation=mutation), self.assertRaises(runner.RunnerError):
                runner.load_results(report)

    def test_partial_failed_phase_retains_only_measured_counts(self) -> None:
        report = load_report()
        phases = report["load_phases"]
        if not isinstance(phases, list):
            raise TypeError()
        phases[0]["observations"] = phases[0]["observations"][:2]
        phases[0]["completed"] = 2
        phases[0]["failure"] = "request_timeout"
        report["load_phases"] = phases[:1]
        result = runner.load_results(report)
        self.assertEqual(2, sum(row.status == "passed" for row in result.tests))
        self.assertEqual(258, sum(row.status == "skipped" for row in result.tests))


    def test_baseline_changes_fail_closed(self) -> None:
        before: dict[str, object] = {
            "environment": {"uid": "prod"},
            "lab_pods": {str(i): "fixed" for i in range(6)},
        }
        active = copy.deepcopy(before)
        pods = active["lab_pods"]
        if not isinstance(pods, dict):
            raise TypeError()
        pods["hikyo"] = "fresh"
        runner.active_baseline(before, active)
        runner.unchanged_idle(before, copy.deepcopy(before))
        active["environment"] = {"uid": "other"}
        with self.assertRaises(runner.RunnerError):
            runner.active_baseline(before, active)

    def test_only_completed_atomic_evidence_is_failed(self) -> None:
        row: dict[str, object] = {
            "case": "wrong-tenant",
            "passed": False,
            "expected_status": 404,
            "direct_status": 200,
            "execution_recorded": True,
            "inject_id": str(uuid.uuid4()),
            "domain_assertion_passed": True,
        }
        result = runner.atomic_results(
            "Authorization", ("wrong-tenant",), {"cases": [row]}
        )
        self.assertEqual("FAIL", release.outcome((result,), ()))
        row.pop("direct_status")
        result = runner.atomic_results(
            "Authorization", ("wrong-tenant",), {"cases": [row]}
        )
        self.assertEqual("INFRA ERROR", release.outcome((result,), ()))

    def test_isolation_requires_all_eleven_live_controls(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "isolation.json"
            names = (
                "lab-rabbitmq",
                "lab-hikyo",
                "forbidden-canary-service",
                "forbidden-canary-pod",
                "forbidden-canary-ingress",
                "kubernetes-api",
                "internet",
                "node-kubernetes-api",
                "runner-metadata-canary",
                "lab-cluster-dns",
                "forbidden-external-dns",
            )
            report = {
                "pass": True,
                "forbidden_destination_control": "all_connected",
                "dns_positive_control": "cluster_resolved",
                "checks": [{"target": name, "pass": True} for name in names],
            }
            runner.save(path, report)
            runner.verify_isolation(path)
            without_dns_control = dict(report)
            without_dns_control.pop("dns_positive_control")
            runner.save(path, without_dns_control)
            with self.assertRaises(runner.RunnerError):
                runner.verify_isolation(path)
            checks = report["checks"]
            if not isinstance(checks, list):
                raise TypeError()
            checks.pop()
            runner.save(path, report)
            with self.assertRaises(runner.RunnerError):
                runner.verify_isolation(path)

    def test_result_version_binding(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary)
            for name in ("security", "adversarial", "detection/fixture"):
                runner.private_directory(state / name)
            runner.save(
                state / "detection/fixture/detection-results.json",
                {"instance_identity": "instance", "release": "nightly-new"},
            )
            runner.reports(state, "instance", "nightly-new")
            with self.assertRaises(runner.RunnerError):
                runner.reports(state, "instance", "nightly-old")

    def test_private_evidence_permissions(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "evidence.json"
            path.write_text(json.dumps({"safe": True}))
            path.chmod(0o644)
            with self.assertRaises(runner.RunnerError):
                runner.read_json(path)
            self.assertEqual({"safe": True}, runner.read_json(path, private=False))


if __name__ == "__main__":
    unittest.main()

"""One-shot orchestration must fail closed without an independent alert chain."""

from __future__ import annotations

import argparse
import importlib.util
import io
import json
import sys
import tempfile
import threading
import time
import unittest
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from pathlib import Path
from types import ModuleType
from unittest.mock import Mock, patch

import hikyo_adversarial_transport as transport
import hikyo_detection_monitor as monitor
import openaev_reporting as reporting


def load_runner() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "tested_detection_exercise", Path(__file__).with_name("detection-exercise.py")
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("test_runner_unavailable")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


exercise = load_runner()
RUN_ID = "5f6882ba-2112-4c13-9981-fdf26a8dbd76"
INJECT_ID = "88e07408-9d6e-429e-9260-04a3a89d0baf"
EVENT_ID = "evt_b1234cbb-122a-4b98-98c9-e20b54818330"


def positive() -> dict[str, object]:
    return {
        "current": False,
        "keys": [
            {
                "name": "LAB_CONFIG",
                "classification": "config",
                "presence": "set",
                "value": "CONFIG_CANARY",
            },
            {"name": "LAB_SECRET", "classification": "secret", "presence": "set"},
        ],
    }


class DetectionExerciseTests(unittest.TestCase):
    def setUp(self) -> None:
        self.runner = exercise.Runner.__new__(exercise.Runner)
        self.runner.fixture = {
            "config_value": "CONFIG_CANARY",
            "secret_value": "SECRET_CANARY",
        }
        self.runner.run_id = RUN_ID
        self.runner.token = "hik_1_wl_synthetic"
        self.runner.actor_id = "prn_reader"
        self.runner.target = (
            "/api/v1/orgs/org_outside/projects/prj_lab/environments/env_lab/delivery"
        )
        self.runner.authorized = (
            "/api/v1/orgs/org_own/projects/prj_lab/environments/env_lab/delivery"
        )
        self.runner.report = {"passed": False, "negative_replays_after_baseline": 0}
        self.runner.guard = Mock()
        self.runner.health_lock = threading.Lock()
        self.runner.watchdog = None
        self.runner.credential_deadline = time.time() + 3600
        self.runner.persist = Mock()
        self.events: list[str] = []
        self.runner.client = Mock()
        self.requests = 0

        def request(_method: str, path: str, _token: str) -> transport.Response:
            self.requests += 1
            if path == self.runner.target:
                self.events.append("direct-negative")
                return transport.Response(
                    404, {"error": {"code": "not_found"}}, 1.0, 30, None
                )
            self.events.append("positive")
            return transport.Response(200, positive(), 1.0, 100, None)

        self.runner.client.request.side_effect = request
        self.runner.pipeline = Mock()
        self.armed_at = datetime.now(UTC)

        def arm(**kwargs: object) -> object:
            self.events.append("arm")
            self.armed_at = datetime.now(UTC)
            return exercise.ArmedMonitor(
                str(kwargs["run_id"]),
                self.armed_at.isoformat(),
                "prn_independent_collector",
                True,
            )

        self.runner.pipeline.arm.side_effect = arm
        self.runner.pipeline.verify_quiet.return_value = {
            "query_succeeded": True,
            "rule_fired": False,
            "inbox_receipts": 0,
            "completed_scrapes": 2,
        }
        self.runner.pipeline.close.side_effect = lambda: self.events.append("close")
        self.runner.recorder = Mock()

        def record(*args: object) -> dict[str, object]:
            self.events.append("record")
            assertion = args[-1]
            if not callable(assertion):
                raise TypeError("missing_recorded_assertion")
            assertion({"error": {"code": "not_found"}})
            return {
                "inject_id": INJECT_ID,
                "domain_assertion_passed": True,
                "execution_recorded": True,
                "http_status_recorded": False,
            }

        self.runner.recorder.record_case.side_effect = record
        self.runner.reporter = Mock()
        self.runner.reporter.execution_window.side_effect = lambda _id: (
            (self.armed_at + timedelta(seconds=1)).isoformat(),
            (self.armed_at + timedelta(seconds=3)).isoformat(),
        )

        def verify(**kwargs: object) -> reporting.MonitoringEvidence:
            self.events.append("verify")
            guard = kwargs["guard"]
            if not callable(guard):
                raise TypeError("missing_wait_guard")
            guard()
            guard()
            return self.proof()

        self.runner.pipeline.verify_monitoring.side_effect = verify

        def publish(*_args: object, **_kwargs: object) -> dict[str, object]:
            self.events.append("publish")
            return {"verified": True}

        self.runner.reporter.publish_verdict.side_effect = publish

    def proof(self) -> reporting.MonitoringEvidence:
        def at(seconds: int) -> str:
            return (self.armed_at + timedelta(seconds=seconds)).isoformat()

        return reporting.MonitoringEvidence(
            reporting.AuditEvidence(
                INJECT_ID, "wrong-tenant", at(1), at(3), True, True, EVENT_ID
            ),
            RUN_ID,
            "prn_independent_collector",
            True,
            at(0),
            at(2),
            at(2),
            at(4),
            at(5),
            at(7),
            at(6),
            "abcdef0123456789",
            "a" * 64,
            True,
            True,
            True,
        )

    def test_exact_one_recorded_attack_and_no_post_baseline_negative_replay(
        self,
    ) -> None:
        self.runner.run()
        self.assertEqual(
            self.events,
            [
                "direct-negative",
                "positive",
                "arm",
                "positive",
                "record",
                "verify",
                "close",
                "publish",
            ],
        )
        self.runner.recorder.record_case.assert_called_once()
        self.assertEqual(self.requests, 3)
        self.assertEqual(self.runner.report["openaev_negative_requests"], 1)
        self.assertEqual(
            self.runner.report["direct_negative_controls_before_baseline"], 1
        )
        self.assertEqual(self.runner.report["negative_replays_after_baseline"], 0)
        self.assertIs(self.runner.report["passed"], True)
        self.assertEqual(self.runner.pipeline.verify_quiet.call_count, 2)
        verification = self.runner.pipeline.verify_monitoring.call_args.kwargs
        self.assertEqual(verification["timeout_s"], 60.0)
        self.assertNotIn("event_id", verification)
        self.assertGreaterEqual(self.runner.guard.call_count, 9)

    def test_final_guard_failure_after_runtime_close_blocks_publication(self) -> None:
        def close() -> None:
            self.events.append("close")
            self.runner.guard.side_effect = RuntimeError("runner_cluster_identity_changed")

        self.runner.pipeline.close.side_effect = close
        with self.assertRaisesRegex(RuntimeError, "runner_cluster_identity_changed"):
            self.runner.run()
        self.runner.pipeline.verify_monitoring.assert_called_once()
        self.runner.pipeline.close.assert_called_once()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.assertIs(self.runner.report["passed"], False)

    def test_runtime_cleanup_failure_blocks_publication(self) -> None:
        self.runner.pipeline.close.side_effect = RuntimeError("runtime_cleanup_failed")
        with self.assertRaisesRegex(RuntimeError, "runtime_cleanup_failed"):
            self.runner.run()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.assertIs(self.runner.report["passed"], False)

    def test_partial_publication_failure_retains_failed_report(self) -> None:
        self.runner.reporter.publish_verdict.side_effect = RuntimeError(
            "native_reporting_put_failed"
        )
        with self.assertRaisesRegex(RuntimeError, "native_reporting_put_failed"):
            self.runner.run()
        self.runner.pipeline.close.assert_called_once()
        self.assertIs(self.runner.report["monitor_runtime_closed"], True)
        self.assertIs(self.runner.report["publication_attempted"], True)
        self.assertIs(self.runner.report["passed"], False)

    def test_missing_notification_cannot_publish_detection(self) -> None:
        self.runner.pipeline.verify_monitoring.side_effect = lambda **_kwargs: replace(
            self.proof(), notification_delivered=False
        )
        with self.assertRaises(reporting.ReportingError):
            self.runner.run()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.runner.pipeline.close.assert_called_once()
        self.assertIs(self.runner.report["passed"], False)

    def test_prebaseline_native_event_cannot_substitute_for_execution(self) -> None:
        self.runner.pipeline.verify_monitoring.side_effect = lambda **_kwargs: replace(
            self.proof(),
            native_occurred_at=(self.armed_at - timedelta(seconds=1)).isoformat(),
        )
        with self.assertRaises(reporting.ReportingError):
            self.runner.run()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.runner.pipeline.close.assert_called_once()

    def test_monitor_run_or_collector_binding_mismatch_never_publishes(self) -> None:
        self.runner.pipeline.verify_monitoring.side_effect = lambda **_kwargs: replace(
            self.proof(), collector_principal_id="prn_other_collector"
        )
        with self.assertRaisesRegex(
            exercise.ExerciseError, "independent_monitor_binding_mismatch"
        ):
            self.runner.run()
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_false_positive_quiet_control_blocks_attack(self) -> None:
        self.runner.pipeline.verify_quiet.return_value = {
            "query_succeeded": True,
            "rule_fired": True,
            "inbox_receipts": 1,
            "completed_scrapes": 2,
        }
        with self.assertRaisesRegex(
            exercise.ExerciseError, "false_positive_monitor_control_failed"
        ):
            self.runner.run()
        self.runner.recorder.record_case.assert_not_called()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.runner.pipeline.close.assert_called_once()

    def test_positive_request_after_arm_must_still_be_authorized(self) -> None:
        self.runner.client.request.side_effect = [
            transport.Response(404, {"error": {"code": "not_found"}}, 1, 10, None),
            transport.Response(200, positive(), 1, 100, None),
            transport.Response(404, {"error": {"code": "not_found"}}, 1, 10, None),
        ]
        with self.assertRaisesRegex(
            exercise.ExerciseError, "armed_positive_control_status_mismatch"
        ):
            self.runner.run()
        self.runner.recorder.record_case.assert_not_called()
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_guard_failure_during_monitor_wait_cancels_reporting_and_closes(
        self,
    ) -> None:
        def cancelled(**kwargs: object) -> reporting.MonitoringEvidence:
            self.runner.guard.side_effect = RuntimeError("runner_cluster_identity_changed")
            callback = kwargs["guard"]
            if callable(callback):
                callback()
            raise AssertionError("guard_failure_swallowed")

        self.runner.pipeline.verify_monitoring.side_effect = cancelled
        with self.assertRaisesRegex(RuntimeError, "runner_cluster_identity_changed"):
            self.runner.run()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.runner.pipeline.close.assert_called_once()

    def test_keyboard_cancellation_always_closes_owned_runtime(self) -> None:
        self.runner.pipeline.verify_monitoring.side_effect = KeyboardInterrupt()
        with self.assertRaises(KeyboardInterrupt):
            self.runner.run()
        self.runner.pipeline.close.assert_called_once()
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_over_budget_monitor_cannot_publish_a_late_success(self) -> None:
        clock = 100.0

        def late(**_kwargs: object) -> reporting.MonitoringEvidence:
            nonlocal clock
            clock += 61
            return self.proof()

        self.runner.pipeline.verify_monitoring.side_effect = late
        with (
            patch.object(exercise.time, "monotonic", side_effect=lambda: clock),
            self.assertRaisesRegex(
                exercise.ExerciseError, "independent_monitor_timeout_exceeded"
            ),
        ):
            self.runner.run()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.runner.pipeline.close.assert_called_once()

    def test_reflected_secret_in_recorded_body_blocks_verdict(self) -> None:
        def record(*args: object) -> dict[str, object]:
            callback = args[-1]
            if callable(callback):
                callback({"error": {"code": "not_found"}, "note": "SECRET_CANARY"})
            raise AssertionError("disclosure_assertion_swallowed")

        self.runner.recorder.record_case.side_effect = record
        with self.assertRaises(transport.TransportError):
            self.runner.run()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.runner.pipeline.close.assert_called_once()

    def test_guard_remains_active_during_blocking_record_and_prevents_scoring(
        self,
    ) -> None:
        active = False
        observed = threading.Event()

        def guard() -> None:
            if active:
                observed.set()
                raise RuntimeError("runner_cluster_identity_changed")

        def record(*_args: object) -> dict[str, object]:
            nonlocal active
            active = True
            self.assertTrue(observed.wait(timeout=1.0))
            return {
                "inject_id": INJECT_ID,
                "execution_recorded": True,
                "domain_assertion_passed": True,
            }

        original_watch = exercise.HealthWatch
        self.runner.guard.side_effect = guard
        self.runner.recorder.record_case.side_effect = record
        with (
            patch.object(
                exercise,
                "HealthWatch",
                side_effect=lambda callback: original_watch(callback, interval_s=0.01),
            ),
            self.assertRaisesRegex(
                exercise.ExerciseError, "background_health_guard_failed"
            ),
        ):
            self.runner.run()
        self.runner.pipeline.verify_monitoring.assert_not_called()
        self.runner.reporter.publish_verdict.assert_not_called()
        self.runner.pipeline.close.assert_called_once()
        self.assertFalse(self.runner.watchdog.thread.is_alive())

    def test_constructor_failure_cannot_start_or_orphan_monitor_runtime(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            args = argparse.Namespace(
                state=state,
                collector_config_file=state / "collector.json",
                platform_url="https://127.0.0.1:18080",
                platform_token_file=state / "platform-token",
            )
            prepared: dict[str, object] = {
                "format": "hikyo-openaev-security/v1",
                "ca_file": str(state / "ca.crt"),
                "admin_file": str(state / "admin.json"),
                "instance_identity": "synthetic_instance",
                "env": self.runner.authorized.removesuffix("/delivery"),
                "outside_envs": [
                    "unused-one",
                    "unused-two",
                    self.runner.target.removesuffix("/delivery"),
                ],
                "accounts": {
                    "reader": {
                        "principal": "prn_reader",
                        "token": "hik_1_wl_synthetic",
                        "credential_path": "/api/v1/orgs/org_own/service-accounts/sa_reader/credentials",
                        "revoke_path": "/api/v1/orgs/org_own/service-accounts/sa_reader/credentials/cred_reader",
                    }
                },
            }
            config: dict[str, object] = {
                "run_id": RUN_ID,
                "collector_principal_id": "prn_independent",
                "test_principal_id": "prn_admin",
            }
            admin = Mock()
            admin.call.return_value = {
                "items": [
                    {
                        "id": "cred_reader",
                        "revoked_at": None,
                        "expires_at": (
                            datetime.now(UTC) + timedelta(hours=1)
                        ).isoformat(),
                    }
                ]
            }
            pipeline_factory = Mock()
            with (
                patch.object(
                    exercise.suite,
                    "private",
                    side_effect=lambda path: (
                        prepared if path.name == "fixture.json" else config
                    ),
                ),
                patch.object(exercise.suite, "identity"),
                patch.object(exercise.suite, "save"),
                patch.object(exercise.suite, "Admin", return_value=admin),
                patch.object(exercise.health, "collect"),
                patch.object(
                    exercise.health, "LabGuard", return_value=Mock(baseline={})
                ),
                patch.object(exercise.transport, "WorkloadTransport"),
                patch.object(
                    exercise.reporting,
                    "OpenAEVReporter",
                    side_effect=RuntimeError("platform_unavailable"),
                ),
                self.assertRaisesRegex(RuntimeError, "platform_unavailable"),
            ):
                exercise.Runner(args, pipeline_factory)
            pipeline_factory.assert_not_called()

    def test_workload_expiry_margin_blocks_even_direct_controls(self) -> None:
        self.runner.credential_deadline = time.time() - 1
        with self.assertRaisesRegex(
            exercise.ExerciseError, "workload_credential_safety_deadline_reached"
        ):
            self.runner.run()
        self.runner.client.request.assert_not_called()
        self.runner.recorder.record_case.assert_not_called()
        self.runner.pipeline.close.assert_called_once()


class FailureReportingTests(unittest.TestCase):
    def invoke(self, runner: Mock, error: Exception | None) -> str:
        runner.run.side_effect = error
        stderr = io.StringIO()
        with (
            patch.object(exercise, "Runner", return_value=runner),
            patch.object(
                sys,
                "argv",
                [
                    "detection-exercise.py",
                    "--state",
                    "/tmp/synthetic-exercise-state",
                    "--collector-config-file",
                    "/tmp/synthetic-collector-config",
                ],
            ),
            patch.object(sys, "stderr", stderr),
        ):
            if error is None:
                exercise.main()
            else:
                with self.assertRaises(SystemExit) as exited:
                    exercise.main()
                self.assertEqual(exited.exception.code, 1)
        return stderr.getvalue()

    def test_native_monitor_failure_retains_literal_reason_and_failed_verdict(
        self,
    ) -> None:
        runner = Mock(report={"passed": True})
        output = self.invoke(
            runner, monitor.MonitorError("native_event_outside_execution_window")
        )
        self.assertFalse(runner.report["passed"])
        self.assertEqual(runner.report["failure"], "MonitorError")
        self.assertEqual(
            runner.report["failure_code"], "native_event_outside_execution_window"
        )
        runner.persist.assert_called_once()
        self.assertIn("FAIL MonitorError", output)

    def test_existing_exercise_reason_stays_and_untrusted_error_text_is_excluded(
        self,
    ) -> None:
        for error, expected in (
            (
                exercise.ExerciseError("background_health_guard_failed"),
                "background_health_guard_failed",
            ),
            (
                RuntimeError(
                    "hik_1_cli_SYNTHETIC_SECRET raw audit payload CONFIG_CANARY"
                ),
                None,
            ),
            (
                monitor.MonitorError("hik_1_cli_SYNTHETIC_SECRET\nraw audit payload"),
                None,
            ),
        ):
            runner = Mock(report={"passed": True})
            output = self.invoke(runner, error)
            self.assertEqual(runner.report.get("failure_code"), expected)
            self.assertFalse(runner.report["passed"])
            runner.persist.assert_called_once()
            self.assertNotIn("SYNTHETIC_SECRET", json.dumps(runner.report) + output)
            self.assertNotIn("CONFIG_CANARY", json.dumps(runner.report) + output)
            self.assertNotIn("raw audit payload", json.dumps(runner.report) + output)

    def test_success_does_not_add_failure_metadata(self) -> None:
        runner = Mock(report={"passed": True})
        self.assertEqual(self.invoke(runner, None), "")
        runner.run.assert_called_once()
        runner.persist.assert_not_called()
        self.assertEqual(runner.report, {"passed": True})


if __name__ == "__main__":
    unittest.main()

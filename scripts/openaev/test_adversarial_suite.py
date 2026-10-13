"""Runner regressions with mocked boundaries; no fixture or network mutations."""

from __future__ import annotations

import argparse
import importlib.util
import sys
import tempfile
import time
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from types import ModuleType, SimpleNamespace
from unittest.mock import Mock, patch
from unittest.mock import call as mock_call

import hikyo_adversarial_cases as cases
import hikyo_adversarial_transport as transport
import hikyo_bounded_load as load
import hikyo_reporting_audit as audit
import openaev_reporting as reporting


def load_runner() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "tested_adversarial_suite", Path(__file__).with_name("adversarial-suite.py")
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("test_runner_unavailable")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


runner_module = load_runner()


def fixture() -> dict[str, object]:
    base = "/api/v1/orgs/org_test/projects/prj_test"
    return {
        "org_path": "/api/v1/orgs/org_test",
        "base": base,
        "env": base + "/environments/env_test",
        "outside_envs": [
            base + "/environments/env_outside",
            "/api/v1/orgs/org_test/projects/prj_other/environments/env_other",
            "/api/v1/orgs/org_other/projects/prj_other/environments/env_other",
        ],
        "accounts": {
            "reader": {"principal": "sa_reader"},
            "revealer": {"principal": "sa_revealer"},
        },
        "publish_version": "ver_pending",
        "config_value": "CONFIG_CANARY_TEST",
        "secret_value": "SECRET_CANARY_TEST",
    }


def delivery() -> dict[str, object]:
    return {
        "current": False,
        "cursor": "valid_cursor",
        "keys": [
            {
                "name": "LAB_CONFIG",
                "classification": "config",
                "presence": "set",
                "value": "CONFIG_CANARY_TEST",
            },
            {"name": "LAB_SECRET", "classification": "secret", "presence": "set"},
        ],
    }


class RegistryTests(unittest.TestCase):
    def test_every_built_case_matches_reporting_registry(self) -> None:
        built = cases.build_cases(fixture(), "valid_cursor")
        self.assertEqual(len(built), 20)
        for case in built:
            self.assertEqual(reporting.NEGATIVE_CASE_STATUS[case.name], case.status)
            self.assertEqual(
                case.name in reporting.STATE_CASES, case.observer is not None
            )

    def test_detection_registry_exact_for_built_cases(self) -> None:
        built = cases.build_cases(fixture(), "valid_cursor")
        expected = {
            "whack-cursor-outside-scope": "delivery.fetch",
            "whack-hostile-scalar-write": "value.stage",
            "whack-8k-scalar-write": "value.stage",
            "whack-publish-bypass-attempt": "value.publish",
        }
        observed = {
            case.name: case.detection_operation
            for case in built
            if case.detection_operation is not None
        }
        self.assertEqual(observed, expected)
        for case in built:
            self.assertEqual(
                audit.DETECTION_OPERATIONS.get(case.name), case.detection_operation
            )
            self.assertEqual(
                case.name in reporting.DETECTION_CASES, case.name in expected
            )


class RefusalTests(unittest.TestCase):
    def test_each_native_refusal_requires_exact_domain(self) -> None:
        for mode in (
            "unauthenticated",
            "not_found",
            "bad_request",
            "too_many_requests",
        ):
            transport.assert_refusal(fixture(), mode, {"error": {"code": mode}})
            with self.subTest(mode=mode), self.assertRaises(transport.TransportError):
                transport.assert_refusal(fixture(), mode, {"error": {"code": "wrong"}})

    def test_missing_or_malformed_error_is_not_a_security_success(self) -> None:
        responses: tuple[dict[str, object], ...] = (
            {},
            {"error": None},
            {"error": "unauthenticated"},
            {"error": {}},
        )
        for body in responses:
            with self.assertRaises(transport.TransportError):
                transport.assert_refusal(fixture(), "unauthenticated", body)

    def test_refusals_reject_delivery_and_nested_canary_leaks(self) -> None:
        leaks: tuple[dict[str, object], ...] = (
            {"keys": []},
            {"secrets": {}},
            {"nested": {"detail": "SECRET_CANARY_TEST"}},
            {"detail": "CONFIG_CANARY_TEST"},
        )
        for leak in leaks:
            response: dict[str, object] = {"error": {"code": "unauthenticated"}}
            response.update(leak)
            with self.assertRaises(transport.TransportError):
                transport.assert_refusal(fixture(), "unauthenticated", response)


class RunnerBoundaryTests(unittest.TestCase):
    def setUp(self) -> None:
        self.runner = runner_module.Runner.__new__(runner_module.Runner)
        self.runner.fixture = fixture()
        self.runner.env = self.runner.fixture["env"]
        self.runner.accounts = self.runner.fixture["accounts"]
        self.runner.reader = {
            "principal": "sa_reader",
            "revoke_path": "/api/v1/orgs/org_test/service-accounts/sa_reader/credentials/cred_test",
        }
        self.runner.tokens = {
            "reader": "hik_1_wl_test",
            "revealer": "hik_1_wl_revealer",
            "tampered": "hik_1_wl_changed",
            "long-garbage": "hik_1_wl_long",
        }
        self.runner.guard = Mock()
        self.runner.run_scope = "full"
        self.runner.credential_deadline = time.time() + 1800
        self.runner.persist = Mock()
        self.runner.cases = []
        self.runner.report = {}
        self.runner.admin = Mock()
        self.runner.admin.call.return_value = {"cells": [{"pending_by_others": False}]}
        self.runner.client = Mock()
        self.runner.recorder = Mock()
        self.runner.recorder.record_case.return_value = {
            "inject_id": "00000000-0000-4000-8000-000000000001",
            "domain_assertion_passed": True,
            "execution_recorded": True,
            "http_status_recorded": False,
            "passed": True,
        }
        self.runner.reporter = Mock()
        self.runner.reporter.publish_verdict.return_value = {"published": True}
        self.named = {
            case.name: case for case in cases.build_cases(self.runner.fixture, "cursor")
        }
        self.case = self.named["whack-structured-value"]
        self.runner.client.request.return_value = transport.Response(
            400, {"error": {"code": "bad_request"}}, 1.0, 40, None
        )

    def test_ambiguous_other_draft_baseline_aborts_before_dispatch(self) -> None:
        self.runner.admin.call.return_value = {"cells": [{"pending_by_others": True}]}
        with (
            patch.object(
                runner_module.suite, "snapshot", return_value={"stable": True}
            ),
            self.assertRaisesRegex(
                runner_module.AdversarialRunError, "draft_observer_baseline_ambiguous"
            ),
        ):
            self.runner.record(self.case)
        self.runner.recorder.record_case.assert_not_called()
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_expired_credential_budget_aborts_before_dispatch_or_home(self) -> None:
        self.runner.credential_deadline = time.time() - 1
        with self.assertRaises(runner_module.AdversarialRunError):
            self.runner.record(self.case)
        self.runner.recorder.record_case.assert_not_called()
        self.runner.client.request.assert_not_called()
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_new_other_draft_signal_aborts_and_never_publishes_home(self) -> None:
        self.runner.admin.call.side_effect = [
            {"cells": [{"pending_by_others": False}]},
            {"cells": [{"pending_by_others": True}]},
        ]
        with (
            patch.object(
                runner_module.suite, "snapshot", return_value={"stable": True}
            ),
            self.assertRaisesRegex(
                runner_module.AdversarialRunError, "mutation_changed_draft_signals"
            ),
        ):
            self.runner.record(self.case)
        self.assertIs(self.runner.cases[0]["passed"], False)
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_changed_published_observer_aborts_before_home(self) -> None:
        with (
            patch.object(
                runner_module.suite,
                "snapshot",
                side_effect=[{"revision": 1}, {"revision": 2}],
            ),
            self.assertRaisesRegex(
                runner_module.AdversarialRunError, "mutation_changed_observed_state"
            ),
        ):
            self.runner.record(self.case)
        self.assertIs(self.runner.cases[0]["passed"], False)
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_recorded_pass_cannot_override_failed_direct_status(self) -> None:
        self.runner.client.request.return_value = transport.Response(
            404, {"error": {"code": "not_found"}}, 1.0, 40, None
        )
        with (
            patch.object(
                runner_module.suite, "snapshot", return_value={"stable": True}
            ),
            self.assertRaisesRegex(
                runner_module.AdversarialRunError, "unexpected_direct_status"
            ),
        ):
            self.runner.record(self.case)
        self.assertIs(self.runner.cases[0]["passed"], False)
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_secret_leak_in_direct_error_aborts_before_home(self) -> None:
        self.runner.client.request.return_value = transport.Response(
            400,
            {"error": {"code": "bad_request"}, "detail": "SECRET_CANARY_TEST"},
            1.0,
            40,
            None,
        )
        with (
            patch.object(
                runner_module.suite, "snapshot", return_value={"stable": True}
            ),
            self.assertRaises(cases.AdversarialAssertionError),
        ):
            self.runner.record(self.case)
        self.assertIs(self.runner.cases[0]["passed"], False)
        self.runner.reporter.publish_verdict.assert_not_called()

    def test_stable_mutation_observers_permit_verified_reporting(self) -> None:
        with patch.object(
            runner_module.suite, "snapshot", return_value={"stable": True}
        ):
            self.runner.record(self.case)
        self.assertIs(self.runner.cases[0]["passed"], True)
        self.assertIs(self.runner.cases[0]["draft_signals_unchanged"], True)
        self.runner.reporter.publish_verdict.assert_called_once()

    def test_revocation_requires_restoration_and_positive_control(self) -> None:
        events: list[str] = []
        restored = False

        def grant(
            _env: str, _principal: str, _capability: str, remove: bool = False
        ) -> None:
            nonlocal restored
            events.append("remove" if remove else "restore")
            restored = not remove

        def control(role: str = "reader") -> transport.Response:
            events.append("control-" + role)
            return transport.Response(200, delivery(), 1.0, 120, None)

        def phase(config: load.PhaseConfig, _paths: object) -> SimpleNamespace:
            name = config.name
            events.append("phase-" + str(name))
            return SimpleNamespace(
                observations=[
                    SimpleNamespace(observation=SimpleNamespace(status=200)),
                    SimpleNamespace(
                        observation=SimpleNamespace(status=429, native_retry_after=1)
                    ),
                ]
            )

        def admin_call(
            method: str, _path: str, expected: int = 200
        ) -> dict[str, object]:
            if method == "DELETE":
                self.assertTrue(restored)
                events.append("revoke")
            return {}

        self.runner.admin.grant.side_effect = grant
        self.runner.admin.call.side_effect = admin_call
        self.runner.control = Mock(side_effect=control)
        self.runner.phase = Mock(side_effect=phase)
        self.runner.record = Mock()
        with (
            patch.object(runner_module.time, "sleep"),
            patch.object(
                runner_module.suite, "tamper", return_value="hik_1_wl_changed"
            ),
        ):
            self.runner.run()
        restore = events.index("restore")
        revoke = events.index("revoke")
        self.assertEqual(events[restore + 1 : revoke], ["control-reader"])

        # A rejected post-restoration positive control must prevent credential
        # deletion, so grant removal cannot masquerade as credential rejection.
        self.setUp()
        restored_for_revoke = False

        def track_restoration(
            _env: str, _principal: str, _capability: str, remove: bool = False
        ) -> None:
            nonlocal restored_for_revoke
            restored_for_revoke = not remove

        def fail_restoration(role: str = "reader") -> transport.Response:
            if restored_for_revoke and role == "reader":
                raise runner_module.AdversarialRunError(
                    "positive_access_control_failed"
                )
            return transport.Response(200, delivery(), 1.0, 120, None)

        self.runner.control = Mock(side_effect=fail_restoration)
        self.runner.admin.grant.side_effect = track_restoration
        self.runner.phase = Mock(side_effect=phase)
        self.runner.record = Mock()
        with (
            patch.object(runner_module.time, "sleep"),
            patch.object(
                runner_module.suite, "tamper", return_value="hik_1_wl_changed"
            ),
            self.assertRaisesRegex(
                runner_module.AdversarialRunError, "positive_access_control_failed"
            ),
        ):
            self.runner.run()
        self.assertFalse(
            any(
                call.args and call.args[0] == "DELETE"
                for call in self.runner.admin.call.call_args_list
            )
        )

    def test_phase_slow_native_collects_stay_outside_fast_load_health_deadline(
        self,
    ) -> None:
        self.runner.phases = []
        self.runner.client.request.return_value = transport.Response(
            200, delivery(), 1.0, 120, None
        )
        native_reads = 0

        def sample() -> dict[str, object]:
            return {
                "environment": {"scope": "isolated runner test"},
                "lab_pods": {f"lab/pod-{index}": {} for index in range(7)},
                "memory_provider": "kubelet-summary",
                "memory": {
                    "lab/server": {
                        "used_bytes": 1,
                        "limit_bytes": 100,
                        "provider": "kubelet-summary",
                        "metrics_timestamp": datetime.now(UTC).isoformat(),
                    }
                },
            }

        def slow_collect() -> dict[str, object]:
            nonlocal native_reads
            native_reads += 1
            time.sleep(1.05)  # Longer than the unchanged one-second callback cap.
            return sample()

        self.runner.guard = runner_module.health.LabGuard(sample())
        background = runner_module.health.BackgroundGuard(
            self.runner.guard, collect_sample=slow_collect
        )
        config = load.PhaseConfig(
            "bounded-background-health",
            (load.RequestCase("authorized", frozenset({200})),),
            2,
            concurrency=1,
            rate_per_second=12.0,
            max_duration_s=2.0,
        )
        with patch.object(
            runner_module.health, "BackgroundGuard", return_value=background
        ):
            report = self.runner.phase(
                config, {"authorized": ("reader", self.runner.env + "/delivery")}
            )
        self.assertTrue(report.succeeded)
        self.assertEqual(report.completed, 2)
        self.assertLess(report.wall_s, 1.0)
        self.assertEqual(native_reads, 2)
        self.assertGreaterEqual(background.cached_checks, 2)
        self.assertTrue(self.runner.report["load_health"][0]["worker_stopped"])

    def test_phase_final_health_failure_retains_completed_request_evidence_and_fails(
        self,
    ) -> None:
        self.runner.phases = []
        self.runner.client.request.return_value = transport.Response(
            200, delivery(), 1.0, 120, None
        )
        background = Mock()
        background.finish.side_effect = runner_module.health.GuardError(
            "runner_cluster_identity_changed"
        )
        background.evidence.return_value = {
            "worker_stopped": True,
            "failure": "runner_cluster_identity_changed",
        }
        config = load.PhaseConfig(
            "bounded-final-health",
            (load.RequestCase("authorized", frozenset({200})),),
            2,
            concurrency=1,
            rate_per_second=12.0,
            max_duration_s=2.0,
        )
        with (
            patch.object(
                runner_module.health, "BackgroundGuard", return_value=background
            ),
            self.assertRaisesRegex(
                runner_module.health.GuardError, "runner_cluster_identity_changed"
            ),
        ):
            self.runner.phase(
                config, {"authorized": ("reader", self.runner.env + "/delivery")}
            )
        self.assertEqual(self.runner.phases[0]["completed"], 2)
        self.assertEqual(
            self.runner.phases[0]["failure"],
            "post_phase_health_runner_cluster_identity_changed",
        )
        background.finish.assert_called_once()
        self.runner.persist.assert_called()

    def test_lifecycle_scope_runs_only_two_fresh_phases_and_atomic_cases(self) -> None:
        self.runner.run_scope = "lifecycle"
        # Invalid full-prefix inputs are intentionally unavailable. Lifecycle
        # must obtain its own native positive control, not a saved prefix.
        self.runner.fixture.pop("outside_envs")
        self.runner.fixture.pop("publish_version")
        self.runner.client.request.return_value = transport.Response(
            200, delivery(), 1.0, 120, None
        )
        self.runner.phase = Mock()
        self.runner.record = Mock()
        with patch.object(
            runner_module.adversarial,
            "build_cases",
            side_effect=AssertionError("full_prefix_replayed"),
        ):
            self.runner.run()
        configurations = [call.args[0] for call in self.runner.phase.call_args_list]
        self.assertEqual(
            [config.name for config in configurations],
            ["post-read-grant-removal", "post-credential-revocation"],
        )
        self.assertEqual(sum(config.request_count for config in configurations), 80)
        self.assertTrue(
            all(
                config.concurrency == 4
                and config.request_timeout_s == 3
                and config.max_duration_s == 45
                for config in configurations
            )
        )
        atomic = [call.args[0] for call in self.runner.record.call_args_list]
        self.assertEqual(
            [(case.name, case.status, case.detection_operation) for case in atomic],
            [
                ("whack-after-read-removal", 404, "delivery.fetch"),
                ("whack-after-credential-revocation", 401, None),
            ],
        )
        for call in self.runner.phase.call_args_list:
            paths = call.args[1]
            self.assertTrue(
                any("cursor=valid_cursor" in path for _role, path in paths.values())
            )
        self.assertNotIn("throttling_recovery", self.runner.report)
        self.assertIs(self.runner.report["passed"], True)

    def test_lifecycle_rejected_initial_control_prevents_mutations(self) -> None:
        self.runner.run_scope = "lifecycle"
        self.runner.phase = Mock()
        self.runner.record = Mock()
        self.runner.client.request.return_value = transport.Response(
            401, {"error": {"code": "unauthenticated"}}, 1.0, 40, None
        )
        with self.assertRaisesRegex(
            runner_module.AdversarialRunError, "positive_access_control_failed"
        ):
            self.runner.run()
        self.runner.admin.grant.assert_not_called()
        self.runner.admin.call.assert_not_called()
        self.runner.phase.assert_not_called()
        self.runner.record.assert_not_called()
        self.runner.reporter.home_metrics.assert_not_called()

    def test_lifecycle_requires_real_cursor_before_mutating(self) -> None:
        self.runner.run_scope = "lifecycle"
        response = delivery()
        response.pop("cursor")
        self.runner.client.request.return_value = transport.Response(
            200, response, 1.0, 120, None
        )
        with self.assertRaisesRegex(
            runner_module.AdversarialRunError, "invalid_state_field"
        ):
            self.runner.run()
        self.runner.admin.grant.assert_not_called()
        self.runner.admin.call.assert_not_called()

    def test_unknown_scope_fails_without_prefix_or_lifecycle(self) -> None:
        self.runner.run_scope = "resume"
        self.runner.run_full_prefix = Mock()
        self.runner.run_lifecycle = Mock()
        with self.assertRaisesRegex(
            runner_module.AdversarialRunError, "invalid_run_scope"
        ):
            self.runner.run()
        self.runner.run_full_prefix.assert_not_called()
        self.runner.run_lifecycle.assert_not_called()

    def test_lifecycle_never_opens_existing_full_or_security_reports(self) -> None:
        for filename in ("adversarial-results.json", "results.json"):
            with tempfile.TemporaryDirectory() as temporary:
                state = Path(temporary)
                (state / filename).write_text('{"passed":true,"load_requests":180}')
                args = argparse.Namespace(state=state, scope="lifecycle")
                with (
                    patch.object(runner_module.suite, "private") as private,
                    self.assertRaisesRegex(
                        runner_module.AdversarialRunError, "fresh_run_required"
                    ),
                ):
                    runner_module.Runner(args)
                private.assert_not_called()

    def test_lifecycle_constructor_keeps_native_preflight_and_new_evidence(
        self,
    ) -> None:
        prepared = fixture()
        prepared.update(
            {
                "ca_file": "/synthetic/ca.crt",
                "admin_file": "/synthetic/admin.json",
                "instance_identity": "synthetic_instance",
            }
        )
        accounts = runner_module.object_value(prepared["accounts"])
        for role in ("reader", "revealer"):
            account = runner_module.object_value(accounts[role])
            account.update(
                {
                    "token": "hik_1_wl_synthetic_" + role,
                    "credential_path": "/api/v1/orgs/org_test/service-accounts/sa_"
                    + role
                    + "/credentials",
                    "revoke_path": "/api/v1/orgs/org_test/service-accounts/sa_"
                    + role
                    + "/credentials/cred_"
                    + role,
                }
            )
            accounts[role] = account
        prepared["accounts"] = accounts
        admin = Mock()

        def native(method: str, path: str) -> dict[str, object]:
            self.assertEqual(method, "GET")
            if path.endswith("/credentials"):
                role = "reader" if "sa_reader" in path else "revealer"
                return {
                    "items": [
                        {
                            "id": "cred_" + role,
                            "revoked_at": None,
                            "expires_at": (
                                datetime.now(UTC) + timedelta(seconds=2000)
                            ).isoformat(),
                        }
                    ]
                }
            if path == "/api/v1/auth/whoami":
                return {"principal": {"id": "admin_test"}}
            if path.endswith("/audit?limit=1"):
                return {"items": []}
            raise AssertionError("unexpected_native_preflight_request")

        admin.call.side_effect = native
        guard = Mock(baseline={}, checks=1, peak_memory=0, last={})
        with (
            tempfile.TemporaryDirectory() as temporary,
            patch.object(runner_module.suite, "private", return_value=prepared),
            patch.object(runner_module.suite, "identity") as identity,
            patch.object(runner_module.suite, "save"),
            patch.object(
                runner_module.suite, "tamper", return_value="hik_1_wl_changed"
            ),
            patch.object(runner_module.suite, "Admin", return_value=admin),
            patch.object(runner_module.transport, "WorkloadTransport"),
            patch.object(runner_module.health, "collect"),
            patch.object(runner_module.health, "LabGuard", return_value=guard),
            patch.object(runner_module.reporting, "OpenAEVReporter"),
            patch.object(runner_module.recording, "OpenAEVRecorder"),
        ):
            args = argparse.Namespace(
                state=Path(temporary),
                scope="lifecycle",
                platform_url="https://127.0.0.1:18080",
                platform_token_file=Path("/synthetic/platform-token"),
            )
            created = runner_module.Runner(args)
        identity.assert_called_once()
        guard.assert_called_once()
        self.assertEqual(
            len(
                [
                    call
                    for call in admin.call.call_args_list
                    if call.args[1].endswith("/credentials")
                ]
            ),
            2,
        )
        admin.grant.assert_called_once_with(
            "/api/v1/orgs/org_test", "admin_test", "audit-read"
        )
        admin.login.assert_called_once()
        self.assertIn(
            mock_call("GET", "/api/v1/orgs/org_test/audit?limit=1"),
            admin.call.call_args_list,
        )
        self.assertGreater(created.credential_deadline, time.time() + 1800)
        self.assertEqual(created.report["run_scope"], "lifecycle")
        self.assertEqual(created.report["planned_load_requests"], 80)
        self.assertEqual(created.report["planned_atomic_cases"], 2)
        self.assertEqual(created.cases, [])
        self.assertEqual(created.phases, [])


if __name__ == "__main__":
    unittest.main()

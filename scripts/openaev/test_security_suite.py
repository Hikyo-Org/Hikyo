"""Regression tests for security assertions, using synthetic in-memory bodies."""

import argparse
import copy
import importlib.util
import json
import sys
import tempfile
import unittest
import uuid
from datetime import UTC, datetime
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location(
    "security_suite_tests", Path(__file__).with_name("security-suite.py")
)
if spec is None or spec.loader is None:
    raise RuntimeError("suite_module_unavailable")
suite = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = suite
spec.loader.exec_module(suite)


class SecurityAssertions(unittest.TestCase):
    def setUp(self):
        self.fixture = {
            "config_value": "synthetic-config",
            "secret_value": "synthetic-secret",
        }
        self.config = {
            "name": "LAB_CONFIG",
            "classification": "config",
            "presence": "set",
            "value": "synthetic-config",
        }
        self.secret = {
            "name": "LAB_SECRET",
            "classification": "secret",
            "presence": "set",
        }

    def response(self, secret_value=False):
        secret = dict(self.secret)
        if secret_value:
            secret["value"] = "synthetic-secret"
        return {"current": False, "keys": [dict(self.config), secret]}

    def rejected(self, mode, response):
        with self.assertRaises(suite.SuiteError):
            suite.assert_body(self.fixture, mode, response)

    def test_presence_requires_exact_config_and_withholds_secret(self):
        suite.assert_body(self.fixture, "presence", self.response())
        self.rejected("presence", self.response(True))
        response = self.response()
        response["keys"][0]["value"] = "wrong-config"
        self.rejected("presence", response)

    def test_reveal_requires_secret_plaintext(self):
        suite.assert_body(self.fixture, "reveal", self.response(True))
        self.rejected("reveal", self.response())

    def test_projection_excludes_secret_names_and_values(self):
        suite.assert_body(
            self.fixture, "config-only", {"current": False, "keys": [self.config]}
        )
        self.rejected("config-only", self.response())
        self.rejected(
            "config-only",
            {"current": False, "keys": [self.config], "extra": "synthetic-secret"},
        )

    def test_denials_require_exact_domain_and_no_material(self):
        for mode in ("not_found", "unauthenticated"):
            suite.assert_body(self.fixture, mode, {"error": {"code": mode}})
            self.rejected(mode, {"error": {"code": "bad_request"}})
            self.rejected(mode, {"error": {"code": mode}, "keys": []})
            self.rejected(mode, {"error": {"code": mode}, "detail": "synthetic-secret"})

    def test_current_requires_true_and_empty_keys(self):
        suite.assert_body(self.fixture, "current", {"current": True, "keys": []})
        self.rejected("current", {"current": False, "keys": []})
        self.rejected("current", {"current": True, "keys": [self.config]})

    def test_duplicate_key_and_wrong_classification_fail(self):
        response = self.response()
        response["keys"].append(dict(self.config))
        self.rejected("presence", response)

    def test_tampered_token_changes_body_and_keeps_valid_checksum(self):
        original = "hik_1_wl_" + "A" * 43 + "000000"
        changed = suite.tamper(original)
        self.assertNotEqual(changed, original)
        self.assertEqual(len(changed), len(original))
        self.assertTrue(changed.startswith("hik_1_wl_"))
        self.assertNotEqual(changed[9:-6], original[9:-6])
        checksum = 0
        for char in changed[-6:]:
            checksum = (
                checksum * 62
                + "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz".index(
                    char
                )
            )
        self.assertEqual(checksum, suite.binascii.crc32(changed[9:-6].encode()))

    def run_failed_case(self, actual_status, observer_changed=False, report_home=False):
        class FakeRecordingError(Exception):
            inject_id = None

        class FakeRecorder:
            def __init__(self, *args, **kwargs):
                pass

            def record_case(self, *args):
                return {
                    "inject_id": "synthetic-inject",
                    "passed": True,
                    "domain_assertion_passed": True,
                }

        response = self.response()
        response["cursor"] = "synthetic-cursor"

        class FakeAPI:
            def __init__(self, *args):
                pass

            def call(self, *args, **kwargs):
                return actual_status, response

        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary)
            state.chmod(0o700)
            fixture = {
                **self.fixture,
                "format": "hikyo-openaev-security/v1",
                "ca_file": "synthetic-ca",
                "instance_identity": "synthetic-instance",
                "admin_file": "synthetic-admin",
                "base": "/api/v1/orgs/o/projects/p",
                "env": "/api/v1/orgs/o/projects/p/environments/e",
                "accounts": {
                    "reader": {"token": "hik_1_wl_" + "A" * 49},
                    "revealer": {"token": "hik_1_wl_" + "B" * 49},
                },
                "expired_token": "hik_1_wl_" + "C" * 49,
                "opt_in_grant_refusal_status": 409,
                "opt_in_grants_unchanged": True,
            }
            suite.save(state / "fixture.json", fixture)
            args = argparse.Namespace(
                state=state,
                platform_url="synthetic-platform",
                platform_token_file=Path("synthetic-token"),
                report_home=report_home,
            )
            case = suite.Case(
                "write-without-edit" if report_home else "test-case",
                "GET",
                fixture["env"] + "/delivery",
                "reader",
                200,
                "presence",
                observer="published" if observer_changed else None,
            )
            recorder = SimpleNamespace(
                OpenAEVRecorder=FakeRecorder, OpenAEVRecordingError=FakeRecordingError
            )
            admin = Mock()
            admin.call.return_value = {"principal": {"id": "synthetic-admin"}}
            reporter = Mock()
            reporter.ensure_asset.return_value = "bfa84df0-8450-4b1d-b507-727702d1ab56"
            with (
                patch.object(suite, "identity"),
                patch.object(suite, "Admin", return_value=admin),
                patch.object(suite.reporting, "OpenAEVReporter", return_value=reporter),
                patch.object(suite, "module", return_value=recorder),
                patch.object(suite.pilot, "API", FakeAPI),
                patch.object(suite, "cases", return_value=[case]),
                patch.object(suite, "snapshot", side_effect=["before", "after"]),
                self.assertRaises(suite.SuiteError),
            ):
                suite.run(args)
            result = json.loads((state / "results.json").read_text())
            self.assertIs(result["passed"], False)
            self.assertIs(result["cases"][0]["passed"], False)
            self.assertEqual(result["cases"][0]["inject_id"], "synthetic-inject")
            reporter.publish_verdict.assert_not_called()

    def test_recorded_success_cannot_hide_wrong_direct_status(self):
        self.run_failed_case(404)

    def test_recorded_success_cannot_hide_observer_change(self):
        self.run_failed_case(200, observer_changed=True)
        response = self.response()
        response["keys"][1]["classification"] = "config"
        self.rejected("presence", response)

    def test_home_publication_cannot_precede_direct_status_or_state_assertions(self):
        self.run_failed_case(404, report_home=True)
        self.run_failed_case(200, observer_changed=True, report_home=True)

    def test_published_snapshot_ignores_per_fetch_receipts(self):
        first, second = self.response(), self.response()
        first["keys"][0]["snapshot_receipt"] = {"issued_at": "first"}
        second["keys"][0]["snapshot_receipt"] = {"issued_at": "second"}
        for response in (first, second):
            response.update({"revision": 1, "schema_revision": 1})
        fixture = {
            "base": "/api/v1/orgs/o/projects/p",
            "env": "/api/v1/orgs/o/projects/p/environments/e",
            "ca_file": "synthetic-ca",
            "accounts": {"reader": {"token": "synthetic-token"}},
        }
        api = SimpleNamespace(call=Mock(side_effect=[(200, first), (200, second)]))
        with patch.object(suite.pilot, "API", return_value=api):
            before = suite.snapshot(None, fixture, "published")
            after = suite.snapshot(None, fixture, "published")
        self.assertEqual(before, after)

    def test_admin_http_failure_sanitizes_arbitrary_domain_text(self):
        admin = suite.Admin.__new__(suite.Admin)
        admin.api = SimpleNamespace(
            call=Mock(return_value=(403, {"error": {"code": "synthetic-secret"}}))
        )
        with self.assertRaises(suite.SuiteError) as failure:
            admin.call("GET", "/synthetic")
        self.assertEqual(
            str(failure.exception), "lab_GET_id_http_403_expected_200_domain_unknown"
        )

    def test_grant_observer_reads_project_membership_route(self):
        admin = SimpleNamespace(call=Mock(return_value={"items": [], "count": 0}))
        fixture = {
            "base": "/api/v1/orgs/o/projects/p",
            "env": "/api/v1/orgs/o/projects/p/environments/e",
        }
        suite.snapshot(admin, fixture, "grants")
        admin.call.assert_called_once_with("GET", fixture["base"] + "/grants")

    def resume_fixture_report(self):
        base = "/api/v1/orgs/o/projects/p"
        env = base + "/environments/e"
        fixture = {
            **self.fixture,
            "format": "hikyo-openaev-security/v1",
            "instance_identity": "synthetic-instance",
            "ca_file": "synthetic-ca",
            "admin_file": "synthetic-admin",
            "base": base,
            "env": env,
            "org_path": "/api/v1/orgs/o",
            "publish_version": "synthetic-version",
            "outside_envs": [
                base + "/environments/outside",
                "/api/v1/orgs/o/projects/outside/environments/e",
                "/api/v1/orgs/outside/projects/p/environments/e",
            ],
            "accounts": {
                "reader": {
                    "principal": "reader",
                    "token": "hik_1_wl_" + "A" * 49,
                    "credential_path": base + "/service-accounts/sa/credentials",
                },
                "revealer": {"principal": "revealer", "token": "hik_1_wl_" + "B" * 49},
            },
            "expired_token": "hik_1_wl_" + "C" * 49,
            "opt_in_grant_refusal_status": 409,
            "opt_in_grants_unchanged": True,
            "synthetic_staging_reauth_window_seconds": 60,
        }
        results = [
            {
                "case": case.name,
                "name": case.name,
                "passed": True,
                "expected_status": case.status,
                "direct_status": case.status,
                "inject_id": str(uuid.uuid4()),
                "execution_recorded": True,
                "domain_assertion_passed": True,
                "http_status_recorded": False,
                "observer_unchanged": None,
            }
            for case in suite.cases(fixture, "synthetic-cursor")[:11]
        ]
        results.append(
            {
                "case": suite.RESUME_FAILED_CASE,
                "passed": False,
                "expected_status": 404,
                "inject_id": str(uuid.uuid4()),
            }
        )
        report = {
            "format": "hikyo-openaev-security-results/v1",
            "instance_identity": fixture["instance_identity"],
            "release": suite.VERSION,
            "started_at": datetime.now(UTC).isoformat(),
            "cases": results,
            "passed": False,
            "opt_in_grant_refusal_status": 409,
            "opt_in_grants_unchanged": True,
            "failure": "OpenAEVRecordingError",
        }
        return fixture, report

    def test_resume_accepts_exact_prefix_without_changing_report(self):
        fixture, report = self.resume_fixture_report()
        before = copy.deepcopy(report)
        completed = suite.validate_resume_report(fixture, report)
        self.assertEqual(len(completed), 11)
        self.assertEqual(report, before)

    def test_resume_rejects_missing_or_false_evidence(self):
        fixture, original = self.resume_fixture_report()
        mutations = [
            ("domain_assertion_passed", False),
            ("execution_recorded", False),
            ("passed", False),
            ("direct_status", 404),
            ("name", "wrong-case"),
            ("http_status_recorded", True),
            ("inject_id", "invalid-uuid"),
        ]
        for field, value in mutations:
            with self.subTest(field=field):
                report = copy.deepcopy(original)
                report["cases"][0][field] = value
                with self.assertRaises(suite.SuiteError):
                    suite.validate_resume_report(fixture, report)
        report = copy.deepcopy(original)
        report["cases"][-1]["direct_status"] = None
        with self.assertRaises(suite.SuiteError):
            suite.validate_resume_report(fixture, report)

    def test_resume_rejects_changed_binding_prefix_or_second_resume(self):
        fixture, original = self.resume_fixture_report()
        for field, value in (
            ("release", "other-release"),
            ("instance_identity", "other-instance"),
            ("resumed_at", datetime.now(UTC).isoformat()),
            ("passed", True),
        ):
            with self.subTest(field=field):
                report = copy.deepcopy(original)
                report[field] = value
                with self.assertRaises(suite.SuiteError):
                    suite.validate_resume_report(fixture, report)
        report = copy.deepcopy(original)
        report["cases"] = report["cases"][:-1]
        with self.assertRaises(suite.SuiteError):
            suite.validate_resume_report(fixture, report)

    def test_resume_native_state_requires_opt_in_grants_and_live_delivery(self):
        fixture, _ = self.resume_fixture_report()
        grants = [
            {"principal_id": who, "capability": cap, "scope": {"environment_id": "e"}}
            for who, cap in (
                ("reader", "read"),
                ("revealer", "read"),
                ("revealer", "reveal"),
            )
        ]
        admin = SimpleNamespace(
            call=Mock(side_effect=[{"enabled": True}, {"items": grants, "count": 3}])
        )
        full = self.response(True)
        full["cursor"] = "fresh-revealer-cursor"
        probe = SimpleNamespace(
            call=Mock(side_effect=[(200, full), (200, self.response())])
        )
        self.assertEqual(
            suite.verify_resume_native_state(admin, probe, fixture),
            "fresh-revealer-cursor",
        )
        self.assertTrue(
            all(call.args[0] == "GET" for call in admin.call.call_args_list)
        )
        for changed in (False, True):
            admin = SimpleNamespace(
                call=Mock(side_effect=[{"enabled": changed}, {"items": [], "count": 0}])
            )
            with self.assertRaises(suite.SuiteError):
                suite.verify_resume_native_state(admin, probe, fixture)

    def test_resume_preserves_prefix_backup_and_starts_at_case_twelve(self):
        fixture, old_report = self.resume_fixture_report()
        calls = []

        class FakeRecordingError(Exception):
            inject_id = None

        class FakeRecorder:
            def __init__(self, *args):
                pass

            def verify_missing_body_failure(self, inject_id, name):
                return {
                    "inject_id": inject_id,
                    "failure_verified": True,
                    "failure_code": "blank_mandatory_body_before_dispatch",
                }

            def record_case(self, name, *args):
                calls.append(name)
                raise FakeRecordingError("synthetic-stop")

        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary)
            state.chmod(0o700)
            suite.save(state / "fixture.json", fixture)
            suite.save(state / "results.json", old_report)
            args = argparse.Namespace(
                state=state,
                platform_url="synthetic-platform",
                platform_token_file=Path("synthetic-token"),
                resume_pre_dispatch_body_error=True,
            )
            recorder = SimpleNamespace(
                OpenAEVRecorder=FakeRecorder, OpenAEVRecordingError=FakeRecordingError
            )
            with (
                patch.object(suite, "identity"),
                patch.object(suite, "Admin"),
                patch.object(suite, "module", return_value=recorder),
                patch.object(suite.pilot, "API"),
                patch.object(
                    suite, "verify_resume_native_state", return_value="fresh-cursor"
                ),
                self.assertRaises(suite.SuiteError),
            ):
                suite.run(args)
            result = json.loads((state / "results.json").read_text())
            backup = json.loads(
                (state / "results-before-missing-body-resume.json").read_text()
            )
            self.assertEqual(backup, old_report)
            self.assertEqual(result["cases"][:11], old_report["cases"][:11])
            self.assertEqual(result["started_at"], old_report["started_at"])
            self.assertIn("resumed_at", result)
            self.assertEqual(calls, [suite.RESUME_FAILED_CASE])
            self.assertEqual(
                result["prior_execution_failures"][0]["inject_id"],
                old_report["cases"][11]["inject_id"],
            )
            self.assertIs(result["prior_execution_failures"][0]["hikyo_finding"], False)


if __name__ == "__main__":
    unittest.main()

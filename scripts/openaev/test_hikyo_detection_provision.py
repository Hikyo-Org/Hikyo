"""Native collector provisioning must stay separate and scoped to one lab env."""

from __future__ import annotations

import argparse
import json
import stat
import tempfile
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from unittest.mock import Mock, patch

import hikyo_detection_provision as provision

TARGET = "/api/v1/orgs/org_target/projects/prj_target/environments/env_target/delivery"
SCOPE = {
    "org_id": "org_target",
    "project_id": "prj_target",
    "environment_id": "env_target",
}


def listing(rows: list[dict[str, object]]) -> dict[str, object]:
    return {"items": rows, "count": len(rows)}


def grant() -> dict[str, object]:
    return {
        "principal_id": "prn_collector",
        "capability": "audit-read",
        "scope": dict(SCOPE),
    }


def whoami() -> dict[str, object]:
    return {
        "principal": {"id": "prn_collector", "kind": "human"},
        "capabilities": {"instance_operator": False},
        "session": {
            "artifact": "cli",
            "assurance": {"factors": ["password", "totp"]},
            "idle_expires_at": (datetime.now(UTC) + timedelta(hours=1)).isoformat(),
            "absolute_expires_at": (datetime.now(UTC) + timedelta(hours=2)).isoformat(),
        },
    }


class ScopeTests(unittest.TestCase):
    def test_exact_environment_grant_and_no_instance_authority_pass(self) -> None:
        proof = provision.verify_scope(
            listing([grant()]), listing([]), "prn_collector", TARGET, 0
        )
        self.assertEqual(proof["matching_tenant_grants"], 1)
        self.assertEqual(proof["matching_instance_grants"], 0)

    def test_org_project_other_environment_or_extra_capability_are_rejected(
        self,
    ) -> None:
        for scope in (
            {"org_id": "org_target"},
            {"org_id": "org_target", "project_id": "prj_target"},
            SCOPE | {"environment_id": "env_other"},
        ):
            changed = grant() | {"scope": scope}
            with self.assertRaises(provision.ProvisionError):
                provision.verify_scope(
                    listing([changed]), listing([]), "prn_collector", TARGET, 0
                )
        with self.assertRaises(provision.ProvisionError):
            provision.verify_scope(
                listing([grant(), grant() | {"capability": "read"}]),
                listing([]),
                "prn_collector",
                TARGET,
                0,
            )

    def test_instance_grant_or_nonzero_invitation_grants_rejected(self) -> None:
        for instance, initial in (
            (listing([grant() | {"scope": {}}]), 0),
            (listing([]), 1),
            (listing([]), False),
        ):
            with self.assertRaises(provision.ProvisionError):
                provision.verify_scope(
                    listing([grant()]), instance, "prn_collector", TARGET, initial
                )

    def test_invalid_grant_count_boolean_or_truncation_rejected(self) -> None:
        for count in (True, 2, "1", None):
            with self.assertRaises(provision.ProvisionError):
                provision.grants({"items": [grant()], "count": count}, "prn_collector")

    def test_session_must_be_different_human_cli_mfa_without_operator(self) -> None:
        provision.session_identity(whoami(), "prn_collector", "prn_admin")
        for field, value in (
            ("principal", {"id": "prn_collector", "kind": "machine"}),
            ("capabilities", {"instance_operator": True}),
            ("enrolment_required", True),
        ):
            with self.assertRaises(provision.ProvisionError):
                provision.session_identity(
                    whoami() | {field: value}, "prn_collector", "prn_admin"
                )
        with self.assertRaises(provision.ProvisionError):
            provision.session_identity(whoami(), "prn_collector", "prn_collector")

    def test_session_expiry_and_missing_totp_or_browser_artifact_rejected(self) -> None:
        for change in (
            {"artifact": "browser"},
            {"assurance": {"factors": ["password"]}},
            {
                "idle_expires_at": (
                    datetime.now(UTC) + timedelta(seconds=620)
                ).isoformat()
            },
            {"absolute_expires_at": "2026-10-09T12:00:00"},
        ):
            value = whoami()
            value["session"] = provision.obj(value["session"]) | change
            with self.assertRaises(provision.ProvisionError):
                provision.session_identity(value, "prn_collector", "prn_admin")


class ProvisionFlowTests(unittest.TestCase):
    def test_native_invitation_enrolment_grant_and_rotation_keep_secrets_separate(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary) / "collector"
            fixture: dict[str, object] = {
                "format": "hikyo-openaev-security/v1",
                "ca_file": "/synthetic/ca.crt",
                "admin_file": "/synthetic/admin.json",
                "instance_identity": "synthetic_instance",
                "outside_envs": [
                    "other_env",
                    "other_project",
                    TARGET.removesuffix("/delivery"),
                ],
                "accounts": {"reader": {"principal": "prn_reader"}},
            }
            args = argparse.Namespace(
                state=state,
                fixture_state=Path(temporary) / "fixture",
                pilot_config=Path("/synthetic/pilot.json"),
                admin_file=Path("/synthetic/admin.json"),
            )
            admin = Mock()
            granted = False

            def admin_call(
                method: str,
                path: str,
                body: dict[str, object] | None = None,
                expected: int = 200,
            ) -> dict[str, object]:
                if path == "/api/v1/auth/whoami":
                    return {"principal": {"id": "prn_test_admin"}}
                if path.endswith("/invitations"):
                    self.assertEqual(method, "POST")
                    self.assertEqual(expected, 201)
                    self.assertIsNotNone(body)
                    assert body is not None
                    self.assertNotIn("template", body)
                    return {
                        "principal_id": "prn_collector",
                        "authority": "native_invitation_only",
                    }
                if path == "/api/v1/instance/grants":
                    return listing([])
                if path.endswith("/grants"):
                    return listing([grant()] if granted else [])
                raise AssertionError("unexpected_admin_operation")

            def create_grant(env: str, principal: str, capability: str) -> None:
                nonlocal granted
                self.assertEqual(
                    (env, principal, capability),
                    (TARGET.removesuffix("/delivery"), "prn_collector", "audit-read"),
                )
                granted = True

            admin.call.side_effect = admin_call
            admin.grant.side_effect = create_grant
            api = Mock()
            api.human_token = "hik_1_cli_native_collector_only"
            paths: list[str] = []

            def call(
                _method: str, path: str, _body: dict[str, object] | None
            ) -> tuple[int, dict[str, object]]:
                paths.append(path)
                if path.endswith("/credential/establish"):
                    return 204, {}
                if path.endswith("/totp/enrol/start"):
                    return 200, {"otpauth_uri": "native_totp_custody_only"}
                if path.endswith("/whoami"):
                    return 200, whoami()
                return 200, {}

            api.call.side_effect = call
            with (
                patch.object(provision.suite, "private", return_value=fixture),
                patch.object(provision.suite, "identity"),
                patch.object(provision.suite, "Admin", return_value=admin),
                patch.object(provision.suite.pilot, "API", return_value=api),
                patch.object(provision.suite.pilot, "wait_new_totp") as wait,
                patch.object(
                    provision.suite.pilot, "totp", return_value="123456"
                ) as totp,
            ):
                config = provision.prepare(args)
            self.assertEqual(wait.call_count, 2)
            self.assertEqual(totp.call_count, 2)
            self.assertEqual(paths.count("/api/v1/auth/local/login"), 2)
            self.assertEqual(
                paths[-1], TARGET.removesuffix("/delivery") + "/audit?limit=1"
            )
            self.assertEqual(config["session_token"], api.human_token)
            for name in (
                "collector.json",
                "collector-custody.json",
                "provision-result.json",
            ):
                self.assertEqual(stat.S_IMODE((state / name).stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(state.stat().st_mode), 0o700)
            serialized = json.dumps(config)
            for field in ("password", "totp_uri", "authority", "admin_file"):
                self.assertNotIn(field, serialized)
            custody = json.loads((state / "collector-custody.json").read_text())
            self.assertEqual(custody["totp_uri"], "native_totp_custody_only")
            self.assertNotIn("session_token", custody)
            self.assertNotIn("authority", custody)

    def test_existing_run_report_blocks_native_account_creation(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary) / "fixture"
            fixture.mkdir()
            (fixture / "detection-results.json").write_text("{}")
            args = argparse.Namespace(
                state=Path(temporary) / "collector", fixture_state=fixture
            )
            with (
                patch.object(provision.suite, "Admin") as admin,
                self.assertRaisesRegex(
                    provision.ProvisionError, "fresh_prepared_fixture_required"
                ),
            ):
                provision.prepare(args)
            admin.assert_not_called()


class CleanupTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fixture: dict[str, object] = {
            "format": "hikyo-openaev-security/v1",
            "ca_file": "/synthetic/ca.crt",
            "admin_file": "/synthetic/admin.json",
            "instance_identity": "synthetic_instance",
            "outside_envs": [
                "unused-one",
                "unused-two",
                TARGET.removesuffix("/delivery"),
            ],
            "accounts": {"reader": {"principal": "prn_reader"}},
        }
        self.config: dict[str, object] = {
            "format": "hikyo-lab-monitor/v1",
            "run_id": "5f6882ba-2112-4c13-9981-fdf26a8dbd76",
            "instance_identity": "synthetic_instance",
            "target_path": TARGET,
            "actor_id": "prn_reader",
            "collector_principal_id": "prn_collector",
            "test_principal_id": "prn_admin",
            "session_token": "hik_1_cli_native_collector",
        }
        self.rows = [grant()]
        self.instance_rows: list[dict[str, object]] = []
        self.events: list[str] = []
        self.admin = Mock()

        def call(_method: str, path: str) -> dict[str, object]:
            if path.endswith("/whoami"):
                return {"principal": {"id": "prn_admin"}}
            return listing(
                self.instance_rows if path == "/api/v1/instance/grants" else self.rows
            )

        def remove(
            env: str, principal: str, capability: str, remove: bool = False
        ) -> None:
            self.events.append("grant-remove")
            self.assertEqual(
                (env, principal, capability, remove),
                (TARGET.removesuffix("/delivery"), "prn_collector", "audit-read", True),
            )
            self.rows = []

        self.admin.call.side_effect = call
        self.admin.grant.side_effect = remove
        self.api = Mock()
        self.logout_status = 204

        def collector(method: str, path: str) -> tuple[int, dict[str, object]]:
            if method == "GET":
                return 200, whoami()
            self.assertEqual(path, "/api/v1/auth/logout")
            self.events.append("logout")
            return self.logout_status, {} if self.logout_status == 204 else {
                "error": {"code": "unauthenticated"}
            }

        self.api.call.side_effect = collector

    def cleanup(self) -> dict[str, object]:
        with tempfile.TemporaryDirectory() as temporary:
            args = argparse.Namespace(
                state=Path(temporary), fixture_state=Path(temporary) / "fixture"
            )
            with (
                patch.object(
                    provision.suite,
                    "private",
                    side_effect=lambda path: (
                        self.fixture if path.name == "fixture.json" else self.config
                    ),
                ),
                patch.object(provision.suite, "identity"),
                patch.object(provision.suite, "Admin", return_value=self.admin),
                patch.object(provision.suite.pilot, "API", return_value=self.api),
            ):
                result = provision.cleanup(args)
            stored = json.loads((args.state / "cleanup-result.json").read_text())
            self.assertEqual(stored, result)
            self.assertEqual(
                stat.S_IMODE((args.state / "cleanup-result.json").stat().st_mode), 0o600
            )
            self.assertNotIn("session_token", result)
            return result

    def test_logout_precedes_exact_grant_removal_and_zero_readback(self) -> None:
        result = self.cleanup()
        self.assertEqual(self.events, ["logout", "grant-remove"])
        self.assertEqual(result["audit_grants_remaining"], 0)
        self.assertEqual(result["instance_grants_remaining"], 0)
        self.assertIs(result["verified"], True)

    def test_already_revoked_session_and_zero_grants_are_idempotent(self) -> None:
        self.rows = []
        self.logout_status = 401
        self.api.call.side_effect = lambda _method, _path: (
            401,
            {"error": {"code": "unauthenticated"}},
        )
        result = self.cleanup()
        self.admin.grant.assert_not_called()
        self.assertEqual(result["logout_status"], 401)

    def test_foreign_target_or_instance_grant_prevents_all_cleanup_mutations(
        self,
    ) -> None:
        self.rows = [grant() | {"scope": SCOPE | {"environment_id": "env_foreign"}}]
        with self.assertRaises(provision.ProvisionError):
            self.cleanup()
        self.api.call.assert_not_called()
        self.admin.grant.assert_not_called()
        self.rows = [grant()]
        self.instance_rows = [grant() | {"scope": {}}]
        with self.assertRaises(provision.ProvisionError):
            self.cleanup()
        self.api.call.assert_not_called()

    def test_reused_admin_session_cannot_be_logged_out_as_collector(self) -> None:
        value = whoami()
        value["principal"] = {"id": "prn_admin", "kind": "human"}
        self.api.call.return_value = 200, value
        self.api.call.side_effect = None
        with self.assertRaisesRegex(
            provision.ProvisionError, "cleanup_collector_session_binding_mismatch"
        ):
            self.cleanup()
        self.assertEqual(self.api.call.call_count, 1)
        self.admin.grant.assert_not_called()


if __name__ == "__main__":
    unittest.main()

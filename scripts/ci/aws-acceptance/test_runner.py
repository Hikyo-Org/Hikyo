"""Offline boundary tests. These never obtain a token or contact AWS/GitHub."""

import importlib.util
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import time
import unittest
from unittest import mock

import historical_cleanup


SPEC = importlib.util.spec_from_file_location("aws_acceptance_runner", Path(__file__).with_name("runner.py"))
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)
OFFLINE_PREFIXES = ("hikyo-e2e-offline01/", "hikyo-e2e-offline02/")


def approved_environment(now):
    expiry = runner.dt.datetime.fromtimestamp(now + 90 * 60, runner.dt.timezone.utc).isoformat()
    cleanup = runner.dt.datetime.fromtimestamp(now + 120 * 60, runner.dt.timezone.utc).isoformat()
    return {
        "AWS_ACCEPTANCE_CANDIDATE_SHA": "a" * 40, "AWS_ACCEPTANCE_APPROVED_SHA": "a" * 40,
        "GITHUB_SHA": "b" * 40, "AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA": "b" * 40,
        "GITHUB_RUN_ID": "1234", "AWS_ACCEPTANCE_APPROVED_RUN_ID": "1234", "GITHUB_RUN_ATTEMPT": "1",
        "GITHUB_REPOSITORY": runner.REPOSITORY, "GITHUB_REF": "refs/heads/main",
        "GITHUB_EVENT_NAME": "workflow_dispatch", "RUNNER_ENVIRONMENT": "github-hosted",
        "GITHUB_WORKFLOW_REF": runner.REPOSITORY + "/.github/workflows/aws-acceptance.yml@refs/heads/main",
        "AWS_ACCEPTANCE_AUTHORIZATION_ID": "approved-offline-fixture",
        "AWS_ACCEPTANCE_RUN_PREFIX": OFFLINE_PREFIXES[0],
        "AWS_ACCEPTANCE_AUTH_EXPIRES_AT": expiry, "AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT": cleanup,
        "AWS_ACCEPTANCE_KMS_KEY_ARN": f"arn:aws:kms:{runner.REGION}:{runner.ACCOUNT}:key/11111111-2222-3333-4444-555555555555",
        "AWS_ACCEPTANCE_EXTERNAL_ID": "offline-fixture",
    }


class ApprovalTests(unittest.TestCase):
    def test_clean_go_environment_compiles_an_offline_module_with_installed_go(self):
        go = shutil.which("go")
        self.assertIsNotNone(go, "The installed Go compiler is required for this capability check")
        with tempfile.TemporaryDirectory() as directory:
            custody = Path(directory)
            module = custody / "module"
            module.mkdir(mode=0o700)
            (module / "go.mod").write_text("module example.invalid/acceptance-capability\n\ngo 1.20\n")
            (module / "capability_test.go").write_text(
                'package capability\nimport "testing"\n'
                'func TestCompile(t *testing.T) { if 2+2 != 4 { t.Fatal("arithmetic") } }\n'
            )
            with mock.patch.dict(os.environ, {
                "HOME": "untrusted-home", "GOPATH": "untrusted-cache",
                "GOCACHE": "untrusted-cache", "GOMODCACHE": "untrusted-cache",
                "GOENV": "untrusted-config", "GOFLAGS": "untrusted-flags",
                "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "synthetic-private-bearer",
                "AWS_SESSION_TOKEN": "synthetic-private-session",
            }):
                env = runner.go_environment(custody)
            self.assertNotIn("ACTIONS_ID_TOKEN_REQUEST_TOKEN", env)
            self.assertNotIn("AWS_SESSION_TOKEN", env)
            self.assertEqual(env["GOFLAGS"], "-modcacherw")
            for key in ("HOME", "GOPATH", "GOCACHE", "GOMODCACHE", "TMPDIR"):
                self.assertTrue(Path(env[key]).is_relative_to(custody))
                self.assertEqual(Path(env[key]).stat().st_mode & 0o777, 0o700)
            env.update({"GOPROXY": "off", "GOSUMDB": "off", "CGO_ENABLED": "0", "GOMAXPROCS": "2"})
            binary = custody / "capability.test"
            result = runner.subprocess.run([go, "test", "-p", "1", "-c", "-o", str(binary), "."],
                                           cwd=module, env=env, stdout=runner.subprocess.PIPE,
                                           stderr=runner.subprocess.PIPE, timeout=120, check=False)
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertTrue(binary.is_file())

    def test_exact_approval_and_expiry_are_required(self):
        now = time.time()
        env = approved_environment(now)
        runner.Approval(env, now)
        for key, value in (
            ("AWS_ACCEPTANCE_APPROVED_SHA", "c" * 40),
            ("AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA", "c" * 40),
            ("AWS_ACCEPTANCE_APPROVED_RUN_ID", "1235"),
            ("GITHUB_RUN_ATTEMPT", "2"),
            ("GITHUB_REF", "refs/pull/1/head"),
            ("GITHUB_REPOSITORY", "other/repository"),
            ("GITHUB_EVENT_NAME", "pull_request_target"),
            ("RUNNER_ENVIRONMENT", "self-hosted"),
            ("AWS_ACCEPTANCE_AUTH_EXPIRES_AT", "2000-01-01T00:00:00Z"),
            ("AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT", "2100-01-01T00:00:00Z"),
            ("AWS_ACCEPTANCE_KMS_KEY_ARN", "arn:aws:kms:us-east-1:111122223333:key/foreign"),
        ):
            with self.subTest(key=key), self.assertRaises((runner.Refused, ValueError)):
                runner.Approval({**env, key: value}, now)

    def test_child_environment_never_inherits_identity_or_proxy_sources(self):
        with mock.patch.dict(os.environ, {
            "PATH": "/usr/bin", "AWS_ACCESS_KEY_ID": "synthetic-private",
            "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "synthetic-private", "GITHUB_TOKEN": "synthetic-private",
            "HTTPS_PROXY": "https://untrusted.invalid", "HIKYO_TEST_POSTGRES_DSN": "synthetic-private",
        }, clear=True):
            self.assertEqual(runner.clean_environment(), {"PATH": "/usr/bin"})

    def test_run_prefix_is_required_and_has_no_fallback_or_path_reuse_format(self):
        now = time.time()
        env = approved_environment(now)
        del env["AWS_ACCEPTANCE_RUN_PREFIX"]
        with self.assertRaises(runner.Refused):
            runner.Approval(env, now)
        for prefix in ("", "hikyo-e2e-short/", "hikyo-e2e-" + "a" * 33 + "/",
                       "hikyo-e2e-UPPERCASE/", "other-e2e-offline01/",
                       "hikyo-e2e-offline01", "hikyo-e2e-offline01//",
                       "hikyo-e2e-offline01/again/", "hikyo-e2e-offline01/../",
                       "hikyo-e2e-offline01/\n", " hikyo-e2e-offline01/"):
            with self.subTest(prefix=prefix), self.assertRaises(runner.Refused):
                runner.Approval({**env, "AWS_ACCEPTANCE_RUN_PREFIX": prefix}, now)
        for prefix in (*OFFLINE_PREFIXES, "hikyo-e2e-" + "a" * 8 + "/",
                       "hikyo-e2e-" + "z" * 32 + "/"):
            with self.subTest(prefix=prefix):
                self.assertEqual(runner.Approval({**env, "AWS_ACCEPTANCE_RUN_PREFIX": prefix}, now).prefix, prefix)
        with self.assertRaises(runner.Refused):
            runner.Approval({**env, "AWS_ACCEPTANCE_RUN_PREFIX": OFFLINE_PREFIXES[1],
                             "GITHUB_RUN_ATTEMPT": "2"}, now)

    def test_optional_historical_cleanup_is_empty_or_the_one_exact_full_arn(self):
        now = time.time()
        env = approved_environment(now)
        self.assertEqual(runner.Approval(env, now).historical_cleanup_arn, "")
        self.assertEqual(runner.Approval({**env, "AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN": historical_cleanup.ARN}, now).historical_cleanup_arn,
                         historical_cleanup.ARN)
        for arn in (historical_cleanup.ARN + "other", historical_cleanup.NAME, "*",
                    historical_cleanup.ARN.replace("099256073061", "111122223333"),
                    historical_cleanup.ARN + "\n"):
            with self.subTest(arn=arn), self.assertRaises(runner.Refused):
                runner.Approval({**env, "AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN": arn}, now)

    def test_private_file_replace_refuses_symlinks_and_preserves_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "descriptor"
            runner.private_write(path, b"synthetic-first")
            runner.private_write(path, b"synthetic-second")
            self.assertEqual(path.read_bytes(), b"synthetic-second")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            path.unlink()
            path.symlink_to(Path(directory) / "elsewhere")
            with self.assertRaises(runner.Refused):
                runner.private_write(path, b"synthetic-private")


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "evidence.json"
        self.approval = runner.Approval(approved_environment(time.time()), time.time())
        self.evidence = runner.Evidence(self.path, self.approval)
        self.name = self.approval.prefix + "static/sqlite/document"
        self.arn = f"arn:aws:secretsmanager:{runner.REGION}:{runner.ACCOUNT}:secret:{self.name}-Ab0123"

    def output(self, output):
        self.evidence.event({"Action": "output", "Test": runner.SUITE, "Output": output})

    def emit_metadata(self, label, value):
        self.output("    aws_real_fixture_test.go:42: " + label + value + "\n")

    def test_approved_namespace_and_expected_suite_cases_remain_bounded(self):
        self.assertEqual(self.evidence.data["prefix"], self.approval.prefix)
        self.assertEqual(len(runner.TEST_NAMES), 46)
        self.assertEqual(self.evidence.data["maximum_create_reservations"], 32)
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn.replace(self.approval.prefix, OFFLINE_PREFIXES[1]))
        self.assertEqual(self.evidence.data["recorded_arns"], [])

    def test_distinct_run_prefixes_isolate_all_creation_and_cleanup_metadata(self):
        for index, prefix in enumerate(OFFLINE_PREFIXES):
            with self.subTest(prefix=prefix):
                now = time.time()
                approval = runner.Approval({**approved_environment(now), "AWS_ACCEPTANCE_RUN_PREFIX": prefix}, now)
                evidence = runner.Evidence(Path(self.directory.name) / f"isolated-{index}.json", approval)
                name = prefix + "static/sqlite/document"
                arn = f"arn:aws:secretsmanager:{runner.REGION}:{runner.ACCOUNT}:secret:{name}-Ab0123"
                foreign_name = OFFLINE_PREFIXES[1-index] + "static/sqlite/document"
                foreign_arn = arn.replace(name, foreign_name)
                def emit(text):
                    evidence.event({"Action": "output", "Test": runner.SUITE,
                                    "Output": "    aws_real_fixture_test.go:42: " + text + "\n"})
                emit("reserved synthetic AWS create: 1 " + foreign_name)
                emit("recorded synthetic AWS resource: " + foreign_arn)
                emit("verified synthetic AWS resource absent: " + foreign_arn)
                emit("unresolved synthetic AWS create; reconcile name " + foreign_name + " before rerunning")
                self.assertTrue(all(not evidence.data[key] for key in (
                    "creation_reservations", "recorded_arns", "verified_absent_arns", "unresolved_names")))
                emit("reserved synthetic AWS create: 1 " + name)
                emit("recorded synthetic AWS resource: " + arn)
                for test in runner.TEST_NAMES:
                    evidence.event({"Action": "pass", "Test": test})
                emit("verified synthetic AWS resource absent: " + foreign_arn)
                self.assertFalse(evidence.finish(0, True))
                emit("verified synthetic AWS resource absent: " + arn)
                self.assertTrue(evidence.finish(0, True))
                self.assertEqual(evidence.data["prefix"], prefix)
                self.assertEqual(evidence.data["creation_reservations"], [name])
                emit("unresolved synthetic AWS create; reconcile name " + name + " before rerunning")
                self.assertEqual(evidence.data["unresolved_names"], [name])
                self.assertFalse(evidence.finish(0, True))

    def test_diagnostic_token_error_keeps_only_structured_enums(self):
        self.output("    aws_real_diagnostics_test.go:42: synthetic AWS diagnostic: CreateSecret InvalidRequestException\n")
        raw = json.loads(self.path.read_text())
        self.assertEqual(raw["diagnostics"], [{
            "test": runner.SUITE, "operation": "CreateSecret", "code": "InvalidRequestException",
        }])
        self.assertFalse(raw["diagnostics_truncated"])
        self.assertNotIn("synthetic AWS diagnostic", self.path.read_text())

    def test_diagnostic_contract_accepts_each_closed_operation_and_code(self):
        for operation in runner.DIAGNOSTIC_OPERATIONS:
            self.output(f"    aws_real_diagnostics_test.go:1: synthetic AWS diagnostic: {operation} other\n")
        for code in runner.DIAGNOSTIC_CODES:
            self.output(f"    aws_real_diagnostics_test.go:1: synthetic AWS diagnostic: CreateSecret {code}\n")
        self.assertEqual({entry["operation"] for entry in self.evidence.data["diagnostics"]}, set(runner.DIAGNOSTIC_OPERATIONS))
        self.assertEqual({entry["code"] for entry in self.evidence.data["diagnostics"]}, set(runner.DIAGNOSTIC_CODES))

    def test_diagnostics_reject_untrusted_event_source_and_payload(self):
        marker = "    aws_real_diagnostics_test.go:42: synthetic AWS diagnostic: CreateSecret InvalidRequestException\n"
        for event in (
            {"Action": "pass"}, {"Action": "fail"}, {"Action": "skip"},
            {"Test": runner.SUITE + "/synthetic-private"}, {"Test": "arbitrary-test"},
            {"Test": 42}, {"Test": [runner.SUITE]}, {"Test": ""},
        ):
            with self.subTest(event=event):
                self.evidence.event({"Action": "output", "Test": runner.SUITE, "Output": marker, **event})
        for output in (
            marker.replace("aws_real_diagnostics_test.go", "aws_real_fixture_test.go"),
            marker.replace("aws_real_diagnostics_test.go", "prefix/aws_real_diagnostics_test.go"),
            marker.replace("aws_real_diagnostics_test.go", "aws_real_diagnostics_testXgo"),
            marker.replace(":42:", ":0:"), marker.replace(":42:", ":-1:"),
            marker.replace("CreateSecret", "CreatePolicy"),
            marker.replace("InvalidRequestException", "synthetic-private"),
            marker.replace("InvalidRequestException", "InvalidRequestException synthetic-private"),
            marker.replace("InvalidRequestException", "InvalidRequestException\x00synthetic-private"),
            "synthetic-private\n" + marker, "\n" + marker,
            marker + "synthetic-private\n", marker + marker,
            "    aws_real_diagnostics_test.go:42: synthetic AWS diagnostic: CreateSecret " + "x" * 60000,
        ):
            with self.subTest(output=output[:100]):
                self.output(output)
        self.assertEqual(self.evidence.data["diagnostics"], [])
        self.assertFalse(self.evidence.data["diagnostics_truncated"])
        self.assertNotIn("synthetic-private", self.path.read_text())

    def test_diagnostics_cap_marks_only_additional_valid_records_truncated(self):
        self.assertEqual(runner.MAX_DIAGNOSTICS, 128)
        marker = "    aws_real_diagnostics_test.go:42: synthetic AWS diagnostic: CreateSecret InvalidRequestException\n"
        for _ in range(128):
            self.output(marker)
        self.assertFalse(self.evidence.data["diagnostics_truncated"])
        self.output(marker.replace("InvalidRequestException", "synthetic-private"))
        self.assertFalse(self.evidence.data["diagnostics_truncated"])
        for _ in range(10):
            self.output(marker)
        raw = json.loads(self.path.read_text())
        self.assertEqual(len(raw["diagnostics"]), 128)
        self.assertTrue(raw["diagnostics_truncated"])
        self.assertNotIn("synthetic-private", self.path.read_text())

    def test_metadata_allowlist_drops_raw_secrets_foreign_arns_and_unknown_test_names(self):
        self.output('synthetic-private {"secret_access_key":"synthetic-private"}\n')
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn.replace(runner.ACCOUNT, "111122223333"))
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn.replace(self.approval.prefix, "unapproved/"))
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn.replace(runner.REGION, "us-east-1"))
        self.evidence.event({"Action": "pass", "Test": runner.SUITE + "/synthetic-private"})
        raw = self.path.read_text()
        self.assertNotIn("synthetic-private", raw)
        self.assertEqual(self.evidence.data["recorded_arns"], [])
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn)
        self.assertEqual(self.evidence.data["recorded_arns"], [self.arn])

    def test_create_reservations_preserve_retries_without_refunds(self):
        for count in range(1, 33):
            self.emit_metadata("reserved synthetic AWS create: ", f"{count} {self.name}")
        self.assertEqual(len(self.evidence.data["creation_reservations"]), 32)
        self.emit_metadata("reserved synthetic AWS create: ", f"1 {OFFLINE_PREFIXES[1]}static/sqlite/document")
        self.assertEqual(len(self.evidence.data["creation_reservations"]), 32)
        with self.assertRaises(runner.Refused):
            self.emit_metadata("reserved synthetic AWS create: ", f"33 {self.name}")
        with self.assertRaises(runner.Refused):
            self.emit_metadata("reserved synthetic AWS create: ", f"1 {self.name}")

    def test_no_pass_without_every_named_case_ledger_and_exact_cleanup(self):
        for name in runner.TEST_NAMES:
            self.evidence.event({"Action": "pass", "Test": name})
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn)
        self.assertFalse(self.evidence.finish(0, True))
        self.emit_metadata("verified synthetic AWS resource absent: ", self.arn)
        self.assertFalse(self.evidence.finish(0, True))
        self.emit_metadata("reserved synthetic AWS create: ", f"1 {self.name}")
        self.assertTrue(self.evidence.finish(0, True))
        self.assertFalse(self.evidence.finish(0, False))
        self.evidence.event({"Action": "skip", "Test": runner.SUITE + "/lifecycle/postgres"})
        self.assertFalse(self.evidence.finish(0, True))

    def test_requested_historical_cleanup_cannot_be_hidden_by_new_suite_success(self):
        for name in runner.TEST_NAMES:
            self.evidence.event({"Action": "pass", "Test": name})
        self.emit_metadata("reserved synthetic AWS create: ", f"1 {self.name}")
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn)
        self.emit_metadata("verified synthetic AWS resource absent: ", self.arn)
        self.evidence.data["historical_cleanup"] = historical_cleanup.state_for(historical_cleanup.ARN)
        self.assertFalse(self.evidence.finish(0, True))
        self.evidence.data["historical_cleanup"].update(status="already-absent", verified_absent=True)
        self.assertTrue(self.evidence.finish(0, True))
        self.assertEqual(self.evidence.data["recorded_arns"], [self.arn])
        self.assertEqual(self.evidence.data["creation_reservations"], [self.name])

    def test_resource_registry_is_persisted_before_later_failure(self):
        self.emit_metadata("reserved synthetic AWS create: ", f"1 {self.name}")
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn)
        self.evidence.event({"Action": "fail", "Test": runner.SUITE})
        raw = json.loads(self.path.read_text())
        self.assertEqual(raw["recorded_arns"], [self.arn])
        self.assertEqual(raw["creation_reservations"], [self.name])
        self.assertNotEqual(raw["status"], "passed")

    def test_oversized_events_refuse(self):
        with self.assertRaises(runner.Refused):
            self.output("x" * (runner.MAX_LINE + 1))


class SessionTests(unittest.TestCase):
    def test_historical_delete_observes_durable_separate_runner_evidence_before_call(self):
        now = time.time()
        env = {**approved_environment(now), "AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN": historical_cleanup.ARN}
        with tempfile.TemporaryDirectory() as directory:
            approval = runner.Approval(env, now)
            projection = runner.Projection(approval, Path(directory))
            projection.cleanup_credentials = {"AccessKeyId": "synthetic-access", "SecretAccessKey": "synthetic-private-key",
                                              "SessionToken": "synthetic-private-session"}
            projection.cleanup_session_expiry = now + 14400
            evidence = runner.Evidence(Path(directory) / "evidence.json", approval)
            operations = []
            def command(arguments, **options):
                operation = arguments[arguments.index("secretsmanager") + 1]
                operations.append(operation)
                if operation == "delete-secret":
                    persisted = json.loads(evidence.path.read_text())
                    self.assertEqual(persisted["historical_cleanup"]["status"], "delete-intent-recorded")
                    self.assertEqual(persisted["historical_cleanup"]["delete_attempts"], 1)
                    intent = json.loads((evidence.path.parent / "historical-cleanup-intent.json").read_text())
                    self.assertEqual(intent["run_id"], approval.run_id)
                    return runner.subprocess.CompletedProcess([], 0, json.dumps({"ARN": historical_cleanup.ARN}).encode(), b"")
                if len(operations) == 1:
                    value = {"ARN": historical_cleanup.ARN, "Name": historical_cleanup.NAME,
                             "KmsKeyId": historical_cleanup.KEY_ID, "DeletedDate": now + 86400}
                    return runner.subprocess.CompletedProcess([], 0, json.dumps(value).encode(), b"")
                return runner.subprocess.CompletedProcess([], 254, b"", b"An error occurred (ResourceNotFoundException) when calling the DescribeSecret operation: absent\n")
            with mock.patch.object(runner.subprocess, "run", side_effect=command), \
                 mock.patch.object(runner.os, "fsync", wraps=runner.os.fsync) as fsync:
                projection.cleanup_historical(evidence)
                self.assertGreaterEqual(fsync.call_count, 8)
            self.assertEqual(operations, ["describe-secret", "delete-secret", "describe-secret"])
            self.assertTrue(evidence.data["historical_cleanup"]["verified_absent"])
            self.assertEqual(evidence.data["creation_reservations"], [])
            self.assertEqual(evidence.data["recorded_arns"], [])
            self.assertNotIn("synthetic-private", evidence.path.read_text())

    def test_historical_cleanup_requires_a_verified_active_cleanup_role_session(self):
        now = time.time()
        env = {**approved_environment(now), "AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN": historical_cleanup.ARN}
        with tempfile.TemporaryDirectory() as directory:
            approval = runner.Approval(env, now)
            projection = runner.Projection(approval, Path(directory))
            evidence = runner.Evidence(Path(directory) / "evidence.json", approval)
            with mock.patch.object(runner.historical_cleanup, "Cleanup") as helper:
                with self.assertRaises(runner.Refused):
                    projection.cleanup_historical(evidence)
                helper.assert_not_called()
            expiration = runner.dt.datetime.fromtimestamp(now + 14400, runner.dt.timezone.utc).isoformat()
            credentials = {"AccessKeyId": "synthetic-access", "SecretAccessKey": "synthetic-private-key",
                           "SessionToken": "synthetic-private-session", "Expiration": expiration}
            foreign = {"Account": runner.ACCOUNT, "Arn": f"arn:aws:sts::{runner.ACCOUNT}:assumed-role/hikyo-158-static/session"}
            with mock.patch.object(projection, "aws", side_effect=[{"Credentials": credentials}, foreign]), self.assertRaises(runner.Refused):
                projection.session("cleanup", Path(directory) / "unused.jwt", 14400)
            self.assertIsNone(projection.cleanup_credentials)
            exact = {"Account": runner.ACCOUNT, "Arn": f"arn:aws:sts::{runner.ACCOUNT}:assumed-role/hikyo-158-cleanup/session"}
            with mock.patch.object(projection, "aws", side_effect=[{"Credentials": credentials}, exact]):
                projection.session("cleanup", Path(directory) / "unused.jwt", 14400)
            with mock.patch.object(runner.historical_cleanup, "Cleanup") as helper:
                projection.cleanup_historical(evidence)
                helper.return_value.execute.assert_called_once_with(historical_cleanup.ARN)
                self.assertEqual(helper.call_args.args[0]["AWS_ACCESS_KEY_ID"], "synthetic-access")
                self.assertEqual(helper.call_args.args[3], approval.run_id)
            projection.cleanup_session_expiry = now - 1
            with self.assertRaises(runner.Refused):
                projection.cleanup_historical(evidence)

    def test_private_manifests_use_each_exact_approved_prefix_with_unchanged_scope(self):
        for prefix in OFFLINE_PREFIXES:
            with self.subTest(prefix=prefix), tempfile.TemporaryDirectory() as directory:
                clock = [time.time()]
                approval = runner.Approval({**approved_environment(clock[0]), "AWS_ACCEPTANCE_RUN_PREFIX": prefix}, clock[0])
                projection = runner.Projection(approval, Path(directory))
                def wait(seconds):
                    clock[0] += seconds
                    return False
                def session(actor, token, duration):
                    return {"mode": "static", "region": runner.REGION}, clock[0] + duration
                with mock.patch.object(runner.time, "time", side_effect=lambda: clock[0]), \
                     mock.patch.object(projection.stop, "wait", side_effect=wait), \
                     mock.patch.object(projection, "token", side_effect=lambda name: (Path(directory) / name, clock[0] + 600)), \
                     mock.patch.object(projection, "session", side_effect=session):
                    projection.prepare()
                manifest = json.loads((Path(directory) / "manifest.json").read_text())
                self.assertEqual(manifest["prefix"], prefix)
                self.assertEqual(manifest["account"], runner.ACCOUNT)
                self.assertEqual(manifest["region"], runner.REGION)
                self.assertEqual(manifest["budget_usd"], 1)
                self.assertEqual(manifest["authorization_id"], approval.authorization)
                self.assertEqual(manifest["kms_key_arn"], approval.kms)
                self.assertEqual(set(manifest["writers"]), set(runner.MODES))

    def test_projection_refuses_any_nonapproved_subject_without_sts_or_secret_output(self):
        now = time.time()
        approval = runner.Approval(approved_environment(now), now)
        claims = {"iss": "https://token.actions.githubusercontent.com", "aud": runner.AUDIENCE,
                  "sub": runner.SUBJECT, "repository": runner.REPOSITORY,
                  "repository_id": "1316165429", "repository_owner_id": "316726515",
                  "sha": approval.workflow, "run_id": approval.run_id, "run_attempt": "1",
                  "event_name": "workflow_dispatch", "exp": int(now + 300)}
        with tempfile.TemporaryDirectory() as directory:
            projection = runner.Projection(approval, Path(directory))
            for key, value in (("sub", "repo:Hikyo-Org/Hikyo:environment:hikyo-158-aws-sandbox"),
                               ("aud", "unapproved"), ("iss", "https://unapproved.invalid"),
                               ("sha", "c" * 40), ("run_id", "unapproved"),
                               ("exp", int(now - 1))):
                payload = runner.base64.urlsafe_b64encode(json.dumps({**claims, key: value}).encode()).decode().rstrip("=")
                response = mock.MagicMock()
                response.__enter__.return_value.read.return_value = json.dumps({"value": "header." + payload + ".signature"}).encode()
                opener = mock.Mock()
                opener.open.return_value = response
                with self.subTest(key=key), mock.patch.dict(os.environ, {
                    "ACTIONS_ID_TOKEN_REQUEST_URL": "https://vstoken.actions.githubusercontent.com/fixture?key=nonsecret",
                    "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "synthetic-private-bearer",
                }), mock.patch.object(runner.urllib.request, "build_opener", return_value=opener), self.assertRaises(runner.Refused):
                    projection.token("projected.jwt")
                self.assertFalse((Path(directory) / "projected.jwt").exists())

    def test_cli_only_receives_token_path_and_binds_real_session_identity(self):
        now = time.time()
        approval = runner.Approval(approved_environment(now), now)
        with tempfile.TemporaryDirectory() as directory:
            custody = Path(directory)
            projection = runner.Projection(approval, custody)
            token = custody / "token.jwt"
            runner.private_write(token, b"synthetic-private-token")
            expiration = runner.dt.datetime.fromtimestamp(now + 900, runner.dt.timezone.utc).isoformat()
            credentials = {"AccessKeyId": "synthetic-access", "SecretAccessKey": "synthetic-private-secret",
                           "SessionToken": "synthetic-private-session", "Expiration": expiration}
            responses = [
                subprocess_result({"Credentials": credentials}),
                subprocess_result({"Account": runner.ACCOUNT,
                                   "Arn": f"arn:aws:sts::{runner.ACCOUNT}:assumed-role/hikyo-158-static/session"}),
            ]
            with mock.patch.object(runner.subprocess, "run", side_effect=responses) as command:
                descriptor, _ = projection.session("static", token, 900)
            for call in command.call_args_list:
                argv = " ".join(call.args[0])
                self.assertNotIn("synthetic-private", argv)
            self.assertIn("file://" + str(token), command.call_args_list[0].args[0])
            self.assertNotIn("AWS_ACCESS_KEY_ID", command.call_args_list[0].kwargs["env"])
            self.assertEqual(command.call_args_list[1].kwargs["env"]["AWS_ACCESS_KEY_ID"], "synthetic-access")
            self.assertEqual(descriptor["session_token"], "synthetic-private-session")
            with mock.patch.object(projection, "aws", side_effect=[
                {"Credentials": credentials}, {"Account": runner.ACCOUNT, "Arn": f"arn:aws:iam::{runner.ACCOUNT}:root"},
            ]), self.assertRaises(runner.Refused):
                projection.session("static", token, 900)


class ChildOwnershipTests(unittest.TestCase):
    def test_historical_refusal_holds_main_before_refresh_or_suite_process_creation(self):
        now = time.time()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            runner_temp = root / "runner-temp"
            runner_temp.mkdir()
            workspace = root / "workspace"
            (workspace / "acceptance-candidate").mkdir(parents=True)
            env = {**approved_environment(now), "AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN": historical_cleanup.ARN,
                   "RUNNER_TEMP": str(runner_temp), "GITHUB_WORKSPACE": str(workspace)}
            def prepare(projection):
                projection.cleanup_credentials = {"AccessKeyId": "synthetic-access", "SecretAccessKey": "synthetic-private-key",
                                                  "SessionToken": "synthetic-private-session"}
                projection.cleanup_session_expiry = now + 14400
            responses = [runner.subprocess.CompletedProcess([], 0, b"a" * 40 + b"\n"),
                         runner.subprocess.CompletedProcess([], 0),
                         runner.subprocess.CompletedProcess([], 254, b"", b"AccessDenied synthetic-private-canary")]
            with mock.patch.dict(os.environ, env, clear=True), \
                 mock.patch.object(sys, "argv", ["runner.py", "run"]), \
                 mock.patch.object(runner.os, "geteuid", return_value=1000), \
                 mock.patch.object(runner.signal, "signal"), \
                 mock.patch.object(runner.subprocess, "run", side_effect=responses) as commands, \
                 mock.patch.object(runner.subprocess, "Popen") as spawn, \
                 mock.patch.object(runner.Projection, "prepare", prepare), \
                 mock.patch.object(runner.Projection, "start_refresh") as refresh, \
                 self.assertRaises(runner.Refused):
                runner.main()
            spawn.assert_not_called()
            refresh.assert_not_called()
            self.assertEqual(len(commands.call_args_list), 3)
            raw = (runner_temp / "aws-acceptance-evidence/evidence.json").read_text()
            evidence = json.loads(raw)
            self.assertEqual(evidence["historical_cleanup"]["status"], "held-reconcile-before-new-suite")
            self.assertEqual(evidence["creation_reservations"], [])
            self.assertEqual(evidence["tests"], {})
            self.assertNotIn("synthetic-private", raw)

    def test_post_spawn_faults_terminate_wait_and_close_the_real_child(self):
        # This is a harmless local sleeping process, never Go acceptance or a
        # provider client. Native process/pipe state verifies actual cleanup.
        original_popen = runner.subprocess.Popen
        original_killpg = runner.os.killpg
        original_selector = runner.selectors.DefaultSelector
        for fault in ("evidence-save", "selector-register", "selector-create",
                      "selector-close", "cancellation-during-spawn"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                custody = Path(directory)
                projection = runner.Projection(runner.Approval(approved_environment(time.time()), time.time()), custody)
                evidence = mock.Mock()
                evidence.data = {}
                if fault in ("evidence-save", "selector-close"):
                    evidence.save.side_effect = OSError("synthetic evidence failure")
                selector = original_selector()
                wrapped_selector = mock.Mock(wraps=selector)
                if fault == "selector-register":
                    wrapped_selector.register.side_effect = OSError("synthetic register failure")
                if fault == "selector-close":
                    wrapped_selector.close.side_effect = OSError("synthetic close failure")
                created = []

                def spawn_benign_child(*_args, **_kwargs):
                    child = original_popen([sys.executable, "-c", "import time; time.sleep(120)"],
                                           env=runner.clean_environment(), stdout=runner.subprocess.PIPE,
                                           stderr=runner.subprocess.DEVNULL, start_new_session=True)
                    child.wait = mock.Mock(wraps=child.wait)
                    created.append(child)
                    if fault == "cancellation-during-spawn":
                        # The installed signal handler sets this flag rather
                        # than raising across Popen's handle assignment.
                        projection.stop.set()
                    return child

                selector_options = {"return_value": wrapped_selector}
                if fault == "selector-create":
                    selector_options = {"side_effect": OSError("synthetic selector failure")}
                try:
                    with mock.patch.object(runner.subprocess, "Popen", side_effect=spawn_benign_child), \
                         mock.patch.object(runner.selectors, "DefaultSelector", **selector_options), \
                         mock.patch.object(runner.os, "killpg", wraps=original_killpg) as signals, \
                         self.assertRaises((OSError, runner.Refused)):
                        runner.run_suite(custody / "unused.test", custody, projection, evidence)
                    self.assertEqual(len(created), 1)
                    child = created[0]
                    self.assertIsNotNone(child.returncode)
                    self.assertTrue(child.stdout.closed)
                    self.assertTrue(projection.child_stopped)
                    child.wait.assert_any_call(timeout=15)
                    child.wait.assert_any_call(timeout=5)
                    signals.assert_any_call(child.pid, runner.signal.SIGTERM)
                    signals.assert_any_call(child.pid, runner.signal.SIGKILL)
                finally:
                    selector.close()
                    # Keep the test itself safe even if the regression returns.
                    for child in created:
                        if child.poll() is None:
                            original_killpg(child.pid, runner.signal.SIGKILL)
                            child.wait(timeout=5)
                        if child.stdout is not None:
                            child.stdout.close()


def subprocess_result(value):
    return runner.subprocess.CompletedProcess([], 0, json.dumps(value).encode())


if __name__ == "__main__":
    unittest.main()

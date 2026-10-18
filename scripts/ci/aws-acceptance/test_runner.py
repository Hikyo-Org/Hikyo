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


SPEC = importlib.util.spec_from_file_location("aws_acceptance_runner", Path(__file__).with_name("runner.py"))
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


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
        self.evidence = runner.Evidence(self.path, runner.Approval(approved_environment(time.time()), time.time()))
        self.name = runner.PREFIX + "static/sqlite/document"
        self.arn = f"arn:aws:secretsmanager:{runner.REGION}:{runner.ACCOUNT}:secret:{self.name}-Ab0123"

    def output(self, output):
        self.evidence.event({"Action": "output", "Test": runner.SUITE, "Output": output})

    def emit_metadata(self, label, value):
        self.output("    aws_real_fixture_test.go:42: " + label + value + "\n")

    def test_metadata_allowlist_drops_raw_secrets_foreign_arns_and_unknown_test_names(self):
        self.output('synthetic-private {"secret_access_key":"synthetic-private"}\n')
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn.replace(runner.ACCOUNT, "111122223333"))
        self.emit_metadata("recorded synthetic AWS resource: ", self.arn.replace(runner.PREFIX, "unapproved/"))
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

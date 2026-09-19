"""Exercise evidence/trust rejection before any Docker resource can be created."""

import contextlib
import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "rehearsal", Path(__file__).with_name("rehearse-upgrade.py")
)
rehearsal = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rehearsal)
cleanup_spec = importlib.util.spec_from_file_location(
    "cleanup", Path(__file__).with_name("cleanup-rehearsal.py")
)
cleanup = importlib.util.module_from_spec(cleanup_spec)
cleanup_spec.loader.exec_module(cleanup)
SOURCE = "ghcr.io/hikyo-org/hikyo@sha256:" + "a" * 64
CANDIDATE = "ghcr.io/hikyo-org/hikyo@sha256:" + "b" * 64


class GateTests(unittest.TestCase):
    def test_exited_server_fails_before_http_or_sleep(self):
        with tempfile.TemporaryDirectory() as directory:
            run = rehearsal.Rehearsal(
                rehearsal.arguments(
                    [
                        "--source",
                        SOURCE,
                        "--bootstrap-only",
                        "--output",
                        directory + "/new",
                    ]
                )
            )
            run.server = run.run_id + "-source"
            with (
                patch.object(
                    run,
                    "call",
                    return_value=json.dumps(
                        {
                            "Running": False,
                            "Status": "exited",
                            "ExitCode": 1,
                            "OOMKilled": False,
                        }
                    ),
                ),
                patch.object(rehearsal.urllib.request, "build_opener") as opener,
                patch.object(rehearsal.time, "sleep") as sleep,
                self.assertRaisesRegex(RuntimeError, "stopped before readiness"),
            ):
                run.wait_ready("http://127.0.0.1:1234/readyz")
            opener.return_value.open.assert_not_called()
            sleep.assert_not_called()
            saved = json.loads((run.output / "report.json").read_text())
            self.assertEqual(saved["server_failure"]["exit_code"], 1)

    def test_failed_log_capture_does_not_mask_report(self):
        with tempfile.TemporaryDirectory() as directory:
            run = rehearsal.Rehearsal(
                rehearsal.arguments(
                    [
                        "--source",
                        SOURCE,
                        "--bootstrap-only",
                        "--output",
                        directory + "/new",
                    ]
                )
            )
            run.resources.append({"kind": "container", "name": run.run_id + "-source"})
            with (
                patch.object(
                    subprocess,
                    "run",
                    side_effect=subprocess.TimeoutExpired("docker", 15),
                ),
                contextlib.redirect_stdout(io.StringIO()),
            ):
                run.finish()
            self.assertEqual(run.report["status"], "incomplete")

    def test_bootstrap_restart_cannot_become_upgrade_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            run = rehearsal.Rehearsal(
                rehearsal.arguments(
                    [
                        "--source",
                        SOURCE,
                        "--bootstrap-only",
                        "--output",
                        directory + "/new",
                    ]
                )
            )

            def command(args, **kwargs):
                if "inspect" in args:
                    return '{"8080/tcp":[{"HostPort":"40000"}],"8081/tcp":[{"HostPort":"40001"}]}'
                return ""

            def boot(reference, name):
                run.server = name

            with (
                patch.dict(os.environ, {"DOCKER_HOST": "unix:///tmp/local-test.sock"}),
                patch.object(rehearsal.shutil, "which", return_value="tool"),
                patch.object(run, "call", side_effect=command),
                patch.object(run, "verify"),
                patch.object(run, "prepare_tls"),
                patch.object(run, "own"),
                patch.object(run, "boot", side_effect=boot) as boots,
                patch.object(run, "fixture") as fixture,
            ):
                run.run()
            self.assertEqual(run.report["status"], "bootstrap-only-no-upgrade-proof")
            self.assertEqual(
                [call.args for call in boots.call_args_list],
                [(SOURCE, "source"), (SOURCE, "source-restart")],
            )
            self.assertEqual(
                [call.args for call in fixture.call_args_list], [("seed",), ("verify",)]
            )

    def test_rejects_missing_fixture_same_image_and_mutable_reference(self):
        with tempfile.TemporaryDirectory() as directory:
            base = ["--output", directory + "/new"]
            for inputs in (
                ["--source", SOURCE, "--candidate", CANDIDATE],
                ["--source", SOURCE, "--candidate", SOURCE],
                ["--source", "ghcr.io/hikyo-org/hikyo:nightly", "--bootstrap-only"],
                ["--source", SOURCE, "--bootstrap-only", "--candidate", CANDIDATE],
            ):
                with (
                    self.subTest(inputs=inputs),
                    contextlib.redirect_stderr(io.StringIO()),
                    self.assertRaises(SystemExit),
                ):
                    rehearsal.arguments(base + inputs)

    def test_prior_evidence_directory_cannot_be_reused(self):
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stderr(io.StringIO()),
            self.assertRaises(SystemExit),
        ):
            rehearsal.arguments(
                ["--source", SOURCE, "--bootstrap-only", "--output", directory]
            )

    def test_signature_failure_prevents_manifest_fetch_or_pull(self):
        with tempfile.TemporaryDirectory() as directory:
            run = rehearsal.Rehearsal(
                rehearsal.arguments(
                    [
                        "--source",
                        SOURCE,
                        "--bootstrap-only",
                        "--output",
                        directory + "/new",
                    ]
                )
            )
            with patch.object(
                run, "call", side_effect=RuntimeError("signature failure")
            ) as command:
                with self.assertRaises(RuntimeError):
                    run.verify(SOURCE, "source")
                self.assertEqual(command.call_count, 1)
                self.assertIn(rehearsal.IDENTITY, command.call_args.args[0])
                self.assertEqual(run.report["stages"], [])


class CleanupTests(unittest.TestCase):
    def invoke(self, directory, endpoint, labels):
        run_id = "hikyo-rehearsal-" + "a" * 16
        report = Path(directory) / "report.json"
        report.write_text(
            json.dumps(
                {
                    "run_id": run_id,
                    "docker_endpoint": endpoint,
                    "resources": [{"kind": "container", "name": run_id + "-source"}],
                }
            )
        )
        inspect = subprocess.CompletedProcess(
            [], 0, json.dumps([{"Config": {"Labels": labels}}]).encode(), b""
        )
        return report, inspect, run_id

    def test_remote_endpoint_refuses_before_docker(self):
        with tempfile.TemporaryDirectory() as directory:
            report, _, _ = self.invoke(directory, "ssh://production", {})
            with (
                patch.object(sys, "argv", ["cleanup", "--report", str(report)]),
                patch.object(subprocess, "run") as command,
                contextlib.redirect_stderr(io.StringIO()),
                self.assertRaises(SystemExit),
            ):
                cleanup.main()
            command.assert_not_called()

    def test_wrong_label_never_removes(self):
        with tempfile.TemporaryDirectory() as directory:
            report, inspect, _ = self.invoke(
                directory, "unix:///tmp/local.sock", {rehearsal.LABEL: "another-run"}
            )
            with (
                patch.object(sys, "argv", ["cleanup", "--report", str(report)]),
                patch.object(subprocess, "run", return_value=inspect) as command,
                contextlib.redirect_stderr(io.StringIO()),
                self.assertRaises(SystemExit),
            ):
                cleanup.main()
            self.assertEqual(command.call_count, 1)
            self.assertIn("inspect", command.call_args.args[0])

    def test_only_explicit_owned_name_is_removed(self):
        with tempfile.TemporaryDirectory() as directory:
            report, inspect, run_id = self.invoke(
                directory,
                "unix:///tmp/local.sock",
                {rehearsal.LABEL: "hikyo-rehearsal-" + "a" * 16},
            )
            with (
                patch.object(sys, "argv", ["cleanup", "--report", str(report)]),
                patch.object(subprocess, "run", return_value=inspect) as command,
                contextlib.redirect_stdout(io.StringIO()),
            ):
                cleanup.main()
            self.assertEqual(command.call_count, 2)
            self.assertEqual(
                command.call_args.args[0],
                [
                    "docker",
                    "--host",
                    "unix:///tmp/local.sock",
                    "container",
                    "rm",
                    "-f",
                    run_id + "-source",
                ],
            )

    def test_amd64_child_manifest_refuses_before_pull(self):
        with tempfile.TemporaryDirectory() as directory:
            run = rehearsal.Rehearsal(
                rehearsal.arguments(
                    [
                        "--source",
                        SOURCE,
                        "--bootstrap-only",
                        "--output",
                        directory + "/new",
                    ]
                )
            )
            with patch.object(
                run,
                "call",
                side_effect=[
                    "[]",
                    '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}',
                ],
            ) as command:
                with self.assertRaisesRegex(RuntimeError, "arm64"):
                    run.verify(SOURCE, "source")
                self.assertEqual(command.call_count, 2)
                self.assertEqual(run.report["stages"], [])


if __name__ == "__main__":
    unittest.main()

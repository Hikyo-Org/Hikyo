"""Docker ownership, endpoint isolation and observer-only transport regressions."""

from __future__ import annotations

import hashlib
import json
import os
import stat
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

import hikyo_detection_runtime as runtime

RUN_ID = "f5c301bd-b75e-4343-a44c-1b7e827fbd1a"


class FakeDocker:
    def __init__(self) -> None:
        self.resources: dict[str, str] = {"unrelated-production": "different-owner"}
        self.commands: list[list[str]] = []
        self.native_ids: dict[str, str] = {}

    def __call__(self, arguments: list[str], input_text: str | None = None) -> str:
        self.commands.append(arguments)
        if arguments[:2] == ["network", "create"]:
            self.resources[arguments[-1]] = arguments[
                arguments.index("--label") + 1
            ].split("=", 1)[1]
            return self.native_ids.get(
                arguments[-1], hashlib.sha256(arguments[-1].encode()).hexdigest()
            )
        elif arguments[0] == "create":
            name = arguments[arguments.index("--name") + 1]
            if name.endswith("-alertmanager"):
                raise runtime.MonitoringRuntimeError("test_partial_failure")
            self.resources[name] = arguments[arguments.index("--label") + 1].split(
                "=", 1
            )[1]
            return self.native_ids.get(name, hashlib.sha256(name.encode()).hexdigest())
        elif len(arguments) > 1 and arguments[1] == "inspect":
            if arguments[-1] not in self.resources:
                raise runtime.MonitoringRuntimeError("not_found")
            name = arguments[-1]
            labels = {runtime.LABEL: self.resources[name]}
            native_id = self.native_ids.get(
                name, hashlib.sha256(name.encode()).hexdigest()
            )
            if "--format" in arguments:
                return json.dumps(labels)
            return json.dumps(
                [
                    {
                        "Name": name if arguments[0] == "network" else "/" + name,
                        "Id": native_id,
                        "Labels": labels,
                        "Config": {"Labels": labels},
                    }
                ]
            )
        elif arguments[:2] == ["container", "ls"] or arguments[:2] == ["network", "ls"]:
            return "\n".join(self.resources)
        elif arguments[0] == "rm" or arguments[:2] == ["network", "rm"]:
            selected = arguments[-1]
            if selected not in self.resources:
                selected = next(
                    name
                    for name in self.resources
                    if self.native_ids.get(
                        name, hashlib.sha256(name.encode()).hexdigest()
                    )
                    == selected
                )
            self.resources.pop(selected)
        return ""


class RuntimeTests(unittest.TestCase):
    def test_cold_image_preparation_is_separate_from_native_control_timeouts(self) -> None:
        with (patch.object(runtime, "_local_daemon") as daemon,
              patch.object(runtime.subprocess, "run") as invoke):
            runtime.prepare_images()
        daemon.assert_called_once()
        self.assertEqual([call.args[0] for call in invoke.call_args_list], [
            ["docker", "pull", "--platform", "linux/arm64", image]
            for image in (runtime.PROMETHEUS_IMAGE, runtime.ALERTMANAGER_IMAGE, runtime.RELAY_IMAGE)])
        self.assertTrue(all(call.kwargs["timeout"] == 180 for call in invoke.call_args_list))
        with patch.object(runtime.subprocess, "run") as native:
            native.return_value.returncode = 0
            native.return_value.stdout = "{}"
            runtime._docker(["version"])
            self.assertEqual(native.call_args.kwargs["timeout"], 20)

    def test_failed_monitor_image_download_cannot_open_actor_window(self) -> None:
        with (patch.object(runtime, "_local_daemon"),
              patch.object(runtime.subprocess, "run", side_effect=runtime.subprocess.TimeoutExpired("docker", 180)) as invoke,
              self.assertRaisesRegex(runtime.MonitoringRuntimeError, "monitor_image_preparation_failed")):
            runtime.prepare_images()
        self.assertEqual(invoke.call_count, 1)

    def test_native_network_readback_rejects_external_bridge(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            stack = runtime.LocalMonitoringRuntime(RUN_ID, Path(directory))
            inspected = [{"Internal": False, "Labels": {runtime.LABEL: stack.owner}}]
            with (
                patch.object(runtime, "_docker", return_value=json.dumps(inspected)),
                self.assertRaises(runtime.MonitoringRuntimeError) as caught,
            ):
                stack._capture_native_evidence()
            self.assertEqual(
                str(caught.exception), "native_internal_network_boundary_failed"
            )

    def test_symlink_or_foreign_artifact_directory_cannot_mount_credentials(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory)
            fixture = parent / "fixture"
            fixture.mkdir(mode=0o700)
            (fixture / "human.token").write_text("synthetic-private-token")
            linked = parent / "stack"
            linked.symlink_to(fixture, target_is_directory=True)
            with patch.object(runtime, "_docker") as docker:
                for path in (linked, fixture):
                    with self.assertRaises(runtime.MonitoringRuntimeError):
                        runtime.LocalMonitoringRuntime(RUN_ID, path).start()
                docker.assert_not_called()
            self.assertEqual(
                (fixture / "human.token").read_text(), "synthetic-private-token"
            )

    def test_partial_start_cleans_only_owned_resources_and_preserves_evidence(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as directory:
            stack = runtime.LocalMonitoringRuntime(RUN_ID, Path(directory))
            docker = FakeDocker()
            with (
                patch.object(runtime, "_docker", side_effect=docker),
                patch.object(
                    runtime,
                    "_local_daemon",
                    return_value={
                        "endpoint": "unix:///test.sock",
                        "daemon_id": "test-daemon",
                    },
                ),
            ):
                with self.assertRaises(runtime.MonitoringRuntimeError):
                    stack.start()
                stack.stop()
            self.assertEqual(
                docker.resources, {"unrelated-production": "different-owner"}
            )
            evidence = json.loads(
                (Path(directory) / "runtime-evidence.json").read_text()
            )
            self.assertTrue(evidence["cleanup"]["complete"])
            network = next(
                args for args in docker.commands if args[:2] == ["network", "create"]
            )
            self.assertIn("--internal", network)
            for command in docker.commands:
                self.assertNotIn("--publish", command)
                self.assertNotIn("--privileged", command)
                self.assertNotIn("/var/run/docker.sock", " ".join(command))

    def test_monitor_mounts_only_public_files_under_private_host_directory(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory)
            stack = runtime.LocalMonitoringRuntime(RUN_ID, parent)
            docker = FakeDocker()
            with (patch.object(runtime, "_docker", side_effect=docker),
                  patch.object(runtime, "_local_daemon", return_value={
                      "endpoint": "unix:///test.sock", "daemon_id": "test-daemon"}),
                  self.assertRaises(runtime.MonitoringRuntimeError)):
                stack.start()
            self.assertEqual(parent.stat().st_mode & 0o777, 0o700)
            allowed = {str(parent.resolve() / name) for name in ("prometheus.json", "rules.json", "alertmanager.json")}
            config_checks = [args for args in docker.commands if args[0] == "run"]
            for args in config_checks:
                mounts = [args[index + 1] for index, value in enumerate(args) if value == "--mount"]
                sources = {item.split("src=", 1)[1].split(",", 1)[0] for item in mounts}
                self.assertEqual(sources, allowed)
                self.assertTrue(all(item.endswith(",readonly") for item in mounts))
                self.assertNotIn(str(parent), sources)
            alertmanager = next(args for args in docker.commands
                                if args[0] == "create" and args[args.index("--name") + 1].endswith("-alertmanager"))
            mounts = [alertmanager[index + 1] for index, value in enumerate(alertmanager) if value == "--mount"]
            self.assertEqual({item.split("src=", 1)[1].split(",", 1)[0] for item in mounts}, allowed)

    def test_cleanup_owner_mismatch_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            stack = runtime.LocalMonitoringRuntime(RUN_ID, Path(directory))
            stack._containers = [stack.relay_container]
            docker = FakeDocker()
            docker.resources[stack.relay_container] = "another-owner"
            with (
                patch.object(runtime, "_docker", side_effect=docker),
                self.assertRaises(runtime.MonitoringRuntimeError),
            ):
                stack.stop()
            self.assertIn(stack.relay_container, docker.resources)
            self.assertFalse(any(args[0] == "rm" for args in docker.commands))

    def test_collector_stdin_update_has_no_alertmanager_or_alert_post(self) -> None:
        observation: dict[str, object] = {
            "run_id": RUN_ID,
            "native_event_id": None,
            "native_denial": False,
            "audit_poll_success": True,
            "audit_last_success_unixtime": time.time(),
            "run_deadline_unixtime": time.time() + 60,
        }
        with patch.object(runtime, "_docker", return_value="") as docker:
            runtime.update_relay_observation(
                "hikyo-detection-0123456789ab-relay", observation
            )
        arguments, stdin = docker.call_args.args
        self.assertEqual(
            arguments[:3], ["exec", "-i", "hikyo-detection-0123456789ab-relay"]
        )
        self.assertIn("/metrics-state", arguments[-1])
        self.assertNotIn("/alerts", arguments[-1])
        self.assertNotIn("9093", arguments[-1])
        self.assertEqual(json.loads(stdin), observation)

    def test_native_get_routes_reject_mutations_redirect_targets_and_foreign_origins(
        self,
    ) -> None:
        stack = runtime.LocalMonitoringRuntime(RUN_ID, Path("/tmp/unused-runtime-test"))
        with patch.object(runtime, "_docker") as docker:
            for path in (
                "https://production.example/api/v1/alerts",
                "//evil/api/v1/alerts",
                "/-/reload",
                "/api/v1/alerts#fragment",
                "/api/v1/alerts?redirect=evil",
            ):
                with self.assertRaises(runtime.MonitoringRuntimeError):
                    stack.prometheus_get(path)
            with self.assertRaises(runtime.MonitoringRuntimeError):
                stack.api("/alerts")
            docker.assert_not_called()

    def test_rule_requires_native_event_success_freshness_scrape_and_fixed_run(
        self,
    ) -> None:
        configs = runtime.monitoring_configs(RUN_ID)
        expression = json.dumps(configs["rules.json"])
        for required in (
            RUN_ID,
            "hikyo_lab_native_denial",
            "hikyo_lab_audit_poll_success",
            "hikyo_lab_audit_last_success_unixtime",
            "hikyo_lab_run_deadline_unixtime",
            "timestamp(",
            "up{",
        ):
            self.assertIn(required, expression)
        self.assertNotIn("host.docker.internal", json.dumps(configs))


class RecoveryTests(unittest.TestCase):
    def journal(
        self, directory: Path, docker: FakeDocker, *, native_id: bool = True
    ) -> tuple[runtime.LocalMonitoringRuntime, dict[str, object]]:
        stack = runtime.LocalMonitoringRuntime(RUN_ID, directory)
        name = stack.relay_container
        docker.resources[name] = stack.owner
        document: dict[str, object] = {
            "schema_version": 1,
            "run_id": RUN_ID,
            "owner": stack.owner,
            "daemon": {"endpoint": "unix:///test.sock", "daemon_id": "test-daemon"},
            "resources": [
                {
                    "kind": "container",
                    "name": name,
                    "native_id": hashlib.sha256(name.encode()).hexdigest()
                    if native_id
                    else None,
                }
            ],
        }
        runtime._save_journal(directory, document)
        return stack, document

    def test_crash_after_create_before_id_bind_recovers_only_precreated_intent(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            docker = FakeDocker()
            stack, _ = self.journal(directory, docker, native_id=False)
            with (
                patch.object(runtime, "_docker", side_effect=docker),
                patch.object(
                    runtime,
                    "_local_daemon",
                    return_value={
                        "endpoint": "unix:///test.sock",
                        "daemon_id": "test-daemon",
                    },
                ),
            ):
                proof = runtime.recover_runtime(directory)
                again = runtime.recover_runtime(directory)
            self.assertTrue(proof["complete"])
            self.assertTrue(proof["verified"])
            self.assertEqual(again["removed"], [])
            with patch.object(
                runtime,
                "_local_daemon",
                side_effect=runtime.MonitoringRuntimeError("new-daemon"),
            ):
                self.assertTrue(runtime.recover_runtime(directory)["already_complete"])
            self.assertEqual(
                docker.resources, {"unrelated-production": "different-owner"}
            )
            removals = [args for args in docker.commands if args[0] == "rm"]
            self.assertEqual(
                removals,
                [
                    [
                        "rm",
                        "-f",
                        hashlib.sha256(stack.relay_container.encode()).hexdigest(),
                    ]
                ],
            )
            self.assertEqual(
                (directory / runtime._JOURNAL).stat().st_mode & 0o777, 0o600
            )

    def test_bound_native_id_or_owner_change_refuses_every_delete(self) -> None:
        for mismatch in ("id", "owner"):
            with tempfile.TemporaryDirectory() as temporary:
                directory, docker = Path(temporary), FakeDocker()
                stack, _ = self.journal(directory, docker)
                if mismatch == "id":
                    docker.native_ids[stack.relay_container] = "b" * 64
                else:
                    docker.resources[stack.relay_container] = "foreign-owner"
                with (
                    patch.object(runtime, "_docker", side_effect=docker),
                    patch.object(
                        runtime,
                        "_local_daemon",
                        return_value={
                            "endpoint": "unix:///test.sock",
                            "daemon_id": "test-daemon",
                        },
                    ),
                    self.assertRaises(runtime.MonitoringRuntimeError),
                ):
                    runtime.recover_runtime(directory)
                self.assertFalse(any(args[0] == "rm" for args in docker.commands))

    def test_journal_permissions_symlink_foreign_name_or_daemon_change_refused(
        self,
    ) -> None:
        for mismatch in ("mode", "symlink", "name", "daemon"):
            with tempfile.TemporaryDirectory() as temporary:
                directory, docker = Path(temporary), FakeDocker()
                _, document = self.journal(directory, docker)
                journal = directory / runtime._JOURNAL
                if mismatch == "mode":
                    journal.chmod(0o644)
                elif mismatch == "symlink":
                    copy = directory / "outside.json"
                    journal.rename(copy)
                    journal.symlink_to(copy)
                elif mismatch == "name":
                    document["resources"] = [
                        {
                            "kind": "container",
                            "name": "unrelated-production",
                            "native_id": None,
                        }
                    ]
                    runtime._save_journal(directory, document)
                daemon = {
                    "endpoint": "unix:///other.sock"
                    if mismatch == "daemon"
                    else "unix:///test.sock",
                    "daemon_id": "test-daemon",
                }
                with (
                    patch.object(runtime, "_docker", side_effect=docker),
                    patch.object(runtime, "_local_daemon", return_value=daemon),
                    self.assertRaises(runtime.MonitoringRuntimeError),
                ):
                    runtime.recover_runtime(directory)
                self.assertFalse(any(args[0] == "rm" for args in docker.commands))

    def test_creation_intent_is_durable_before_first_docker_resource_action(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            stack = runtime.LocalMonitoringRuntime(RUN_ID, directory)

            def kill(arguments: list[str], input_text: str | None = None) -> str:
                self.assertEqual(arguments[0], "run")
                document = runtime._read_journal(directory)
                self.assertEqual(document["owner"], stack.owner)
                rows = document["resources"]
                self.assertIsInstance(rows, list)
                self.assertIn("-promtool", json.dumps(rows))
                raise SystemExit("synthetic-sigkill")

            with (
                patch.object(runtime, "_docker", side_effect=kill),
                patch.object(
                    runtime,
                    "_local_daemon",
                    return_value={
                        "endpoint": "unix:///test.sock",
                        "daemon_id": "test-daemon",
                    },
                ),
                self.assertRaises(SystemExit),
            ):
                stack.start()
            self.assertTrue((directory / runtime._JOURNAL).exists())

    def test_hosted_root_socket_requires_local_type_and_runner_group_access(self) -> None:
        cases = [(0, 123, 0o660, True), (1001, 123, 0o600, True),
                 (999, 123, 0o660, False), (0, 456, 0o660, False),
                 (0, 123, 0o666, False), (0, 123, 0o600, False)]
        for owner, group, mode, accepted in cases:
            with self.subTest(owner=owner, group=group, mode=oct(mode)):
                native = os.stat_result((stat.S_IFSOCK | mode, 1, 1, 1, owner, group, 0, 0, 0, 0))
                with (patch.dict(os.environ, {}, clear=True),
                      patch.object(runtime, "_docker", side_effect=[json.dumps("unix:///var/run/docker.sock"), "native-daemon"]),
                      patch.object(runtime.Path, "lstat", return_value=native),
                      patch.object(runtime.os, "getuid", return_value=1001),
                      patch.object(runtime.os, "getgid", return_value=1001),
                      patch.object(runtime.os, "getgroups", return_value=[1001, 123])):
                    if accepted:
                        self.assertEqual(runtime._local_daemon()["daemon_id"], "native-daemon")
                    else:
                        with self.assertRaisesRegex(runtime.MonitoringRuntimeError, "socket_owner_invalid"):
                            runtime._local_daemon()

    def test_docker_environment_override_or_remote_socket_refused(self) -> None:
        with (
            patch.dict(os.environ, {"DOCKER_HOST": "tcp://remote.example:2376"}),
            self.assertRaises(runtime.MonitoringRuntimeError),
        ):
            runtime._local_daemon()
        with (
            patch.dict(os.environ, {}, clear=True),
            patch.object(
                runtime, "_docker", return_value=json.dumps("tcp://remote.example:2376")
            ),
            self.assertRaises(runtime.MonitoringRuntimeError),
        ):
            runtime._local_daemon()


if __name__ == "__main__":
    unittest.main()

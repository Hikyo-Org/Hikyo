"""Independent detection must fail closed on stale, wrong or synthetic evidence."""

from __future__ import annotations

import hashlib
import json
import subprocess
import tempfile
import time
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from unittest.mock import Mock, patch

import hikyo_detection_monitor as monitor

RUN = "dd24ce1a-bdcd-4cde-a43d-ae580d7eeb41"
INJECT = "91bc0646-7770-44b3-99cd-5420779743b2"
EVENT = "evt_88aa5c12-c69b-4749-aad9-67fb82e5390a"
TARGET = "/api/v1/orgs/org_target/projects/prj_target/environments/env_target/delivery"


def configuration() -> dict[str, object]:
    return {
        "format": "hikyo-lab-monitor/v1",
        "run_id": RUN,
        "target_path": TARGET,
        "actor_id": "sa_reader",
        "collector_principal_id": "prn_audit",
        "test_principal_id": "prn_admin",
        "session_token": "hik_1_cli_SYNTHETIC_NOT_A_CREDENTIAL",
        "session_expires_at": (datetime.now(UTC) + timedelta(hours=1)).isoformat(),
        "instance_identity": "synthetic_instance",
        "lease_seconds": 600,
        "scope_proof": {
            "principal_id": "prn_audit",
            "target_path": TARGET,
            "capability": "audit-read",
            "matching_tenant_grants": 1,
            "matching_instance_grants": 0,
            "invitation_grants_created": 0,
        },
    }


def write(path: Path, data: dict[str, object]) -> None:
    path.write_text(json.dumps(data))
    path.chmod(0o600)


def page(
    events: list[dict[str, object]],
    *,
    upper: int = 12,
    cursor: int = 12,
    exhausted: bool = True,
) -> dict[str, object]:
    return {
        "items": events,
        "count": len(events),
        "upper_seq": upper,
        "next_after_seq": cursor,
        "exhausted": exhausted,
    }


def event(at: datetime, **changes: object) -> dict[str, object]:
    result: dict[str, object] = {
        "seq": 11,
        "id": EVENT,
        "type": "grant.denied",
        "outcome": "denied",
        "actor_id": "sa_reader",
        "actor_class": "machine",
        "org_id": "org_target",
        "project_id": "prj_target",
        "env_id": "env_target",
        "occurred_asserted": False,
        "occurred_at": at.isoformat(),
        "recorded_at": at.isoformat(),
        "payload": {"operation": "delivery.fetch", "resolution": "resolvable"},
    }
    result.update(changes)
    return result


class MonitorTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.path = self.directory / "config.json"
        write(self.path, configuration())
        self.config = monitor.Config.load(self.path)

    def poller(self, pages: list[dict[str, object]]) -> tuple[monitor.Poller, Mock]:
        reader = Mock(
            side_effect=[
                {"instance_identity": "synthetic_instance"},
                {
                    "principal": {"id": "prn_audit", "kind": "human"},
                    "session": {"artifact": "cli"},
                    "capabilities": {"instance_operator": False},
                },
                page([], upper=10, cursor=0, exhausted=False),
                *pages,
            ]
        )
        poller = monitor.Poller(
            self.config, reader, time.time() + self.config.lease_seconds
        )
        poller.arm()
        return poller, reader

    def test_config_requires_owner_only_file_and_distinct_native_scope(self) -> None:
        self.path.chmod(0o644)
        with self.assertRaisesRegex(monitor.MonitorError, "owner_only"):
            monitor.Config.load(self.path)
        for field, value in [
            ("collector_principal_id", "prn_admin"),
            ("lease_seconds", 601),
            ("admin_file", "/private/admin"),
            ("session_token", "secret\nheader"),
        ]:
            config = configuration()
            config[field] = value
            write(self.path, config)
            with self.assertRaises(monitor.MonitorError):
                monitor.Config.load(self.path)

    def test_config_rejects_symlink_scope_expansion_and_short_lifetime(self) -> None:
        link = self.directory / "linked"
        link.symlink_to(self.path)
        with self.assertRaises(OSError):
            monitor.Config.load(link)
        for field, value in [
            ("matching_tenant_grants", 2),
            ("matching_instance_grants", 1),
            ("invitation_grants_created", 1),
            ("matching_tenant_grants", True),
        ]:
            config = configuration()
            proof = monitor._object(config["scope_proof"])
            proof[field] = value
            config["scope_proof"] = proof
            write(self.path, config)
            with self.assertRaisesRegex(monitor.MonitorError, "least_privilege"):
                monitor.Config.load(self.path)
        config = configuration()
        config["session_expires_at"] = datetime.now(UTC).isoformat()
        write(self.path, config)
        with self.assertRaisesRegex(monitor.MonitorError, "lifetime"):
            monitor.Config.load(self.path)
        self.assertNotIn(self.config.session_token, repr(self.config))

    def test_arm_verifies_real_human_identity_and_native_instance(self) -> None:
        for identity in [
            {
                "principal": {"id": "prn_admin", "kind": "human"},
                "session": {"artifact": "cli"},
                "capabilities": {"instance_operator": True},
            },
            {
                "principal": {"id": "prn_audit", "kind": "machine"},
                "session": {"artifact": "cli"},
                "capabilities": {"instance_operator": False},
            },
        ]:
            reader = Mock(
                side_effect=[{"instance_identity": "synthetic_instance"}, identity]
            )
            with self.assertRaisesRegex(monitor.MonitorError, "human_session"):
                monitor.Poller(
                    self.config, reader, time.time() + self.config.lease_seconds
                ).arm()
        with self.assertRaisesRegex(monitor.MonitorError, "wrong_native_instance"):
            monitor.Poller(
                self.config,
                Mock(return_value={"instance_identity": "other"}),
                time.time() + self.config.lease_seconds,
            ).arm()

    def test_old_control_excluded_and_fixed_watermark_recovers_late_commit(
        self,
    ) -> None:
        at = datetime.now(UTC) + timedelta(seconds=1)
        poller, reader = self.poller([page([]), page([event(at)]), page([event(at)])])
        self.assertFalse(poller.poll()["native_denial"])
        with patch.object(monitor, "datetime") as clock:
            clock.now.return_value = at + timedelta(seconds=1)
            clock.fromisoformat = datetime.fromisoformat
            first = poller.poll()
            stamp = monitor._object(poller.first)["first_polled_at"]
            clock.now.return_value = at + timedelta(seconds=2)
            self.assertTrue(poller.poll()["native_denial"])
        self.assertEqual(first["native_event_id"], EVENT)
        self.assertEqual(monitor._object(poller.first)["first_polled_at"], stamp)
        self.assertTrue(
            all("after_seq=10" in c.args[0] for c in reader.call_args_list[3:])
        )
        self.assertEqual(
            set(first),
            {
                "run_id",
                "native_event_id",
                "native_denial",
                "audit_poll_success",
                "audit_last_success_unixtime",
                "run_deadline_unixtime",
            },
        )
        self.assertNotIn("sa_reader", json.dumps(first))

    def test_wrong_actor_scope_operation_or_prebaseline_event_cannot_alert(
        self,
    ) -> None:
        old = datetime.now(UTC) - timedelta(minutes=1)
        variants: list[dict[str, object]] = [
            {"actor_id": "someone_else"},
            {"org_id": "other_org"},
            {"payload": {"operation": "value.stage", "resolution": "resolvable"}},
            {"occurred_asserted": True},
            {},
        ]
        for changes in variants:
            at = old if not changes else datetime.now(UTC) + timedelta(seconds=1)
            poller, _reader = self.poller([page([event(at, **changes)])])
            with patch.object(monitor, "datetime") as clock:
                clock.now.return_value = at + timedelta(seconds=1)
                clock.fromisoformat = datetime.fromisoformat
                self.assertFalse(poller.poll()["native_denial"])

    def test_query_failure_pagination_bound_and_duplicate_never_fabricate(self) -> None:
        poller, reader = self.poller([])
        reader.side_effect = OSError("sensitive response")
        with self.assertRaises(OSError):
            poller.poll()
        self.assertIsNone(poller.first)
        poller, _reader = self.poller(
            [page([], upper=100, cursor=n, exhausted=False) for n in range(11, 16)]
        )
        with self.assertRaisesRegex(monitor.MonitorError, "page_bound"):
            poller.poll()
        poller.deadline = time.time() - 1
        with self.assertRaisesRegex(monitor.MonitorError, "lease_expired"):
            poller.poll()

    def test_shared_deadline_is_exact_and_invalid_leases_fail_closed(self) -> None:
        deadline = time.time() + 90
        self.assertEqual(
            monitor.Poller(self.config, Mock(), deadline).deadline, deadline
        )
        for invalid in [float("nan"), float("inf"), time.time() - 1, time.time() + 601]:
            with self.assertRaises(monitor.MonitorError):
                monitor.Poller(self.config, Mock(), invalid)

    def test_stop_retains_matched_event_but_disables_rule(self) -> None:
        poller = monitor.Poller(self.config, Mock(), time.time() + 90)
        poller.first = {"event_id": EVENT}
        stopped = monitor.stop_observation(poller)
        self.assertEqual(stopped["native_event_id"], EVENT)
        self.assertEqual(stopped["run_deadline_unixtime"], poller.deadline)
        self.assertFalse(stopped["native_denial"])
        self.assertFalse(stopped["audit_poll_success"])

    def pipeline(self) -> tuple[monitor.DetectionPipeline, Mock, datetime]:
        pipeline = monitor.DetectionPipeline(
            self.path, self.directory / "ca", self.directory
        )
        runtime = Mock()
        pipeline.runtime = runtime
        process = Mock()
        process.poll.return_value = None
        pipeline.process = process
        start = datetime.now(UTC) - timedelta(seconds=10)
        pipeline.armed = monitor.ArmedMonitor(RUN, start.isoformat(), "prn_audit", True)
        times = [start + timedelta(seconds=x) for x in [1, 2, 3, 4, 5]]
        first = {
            "event_id": EVENT,
            "occurred_at": times[0].isoformat(),
            "recorded_at": times[1].isoformat(),
            "first_polled_at": times[2].isoformat(),
            "native_audit_row": event(times[0], recorded_at=times[1].isoformat()),
        }
        write(
            self.directory / "collector-state.json",
            {
                "run_id": RUN,
                "poll_success": True,
                "last_polled_at": datetime.now(UTC).isoformat(),
                "deadline": str(time.time() + 100),
                "first_event": first,
            },
        )
        labels = {
            "alertname": "HikyoLabCrossTenantDenied",
            "run_id": RUN,
            "event_id": EVENT,
        }
        prom = {
            "status": "success",
            "data": {
                "alerts": [
                    {
                        "labels": {
                            "alertname": "HikyoLabCrossTenantDenied",
                            "run_id": RUN,
                            "event_id": EVENT,
                        },
                        "state": "firing",
                        "activeAt": times[3].isoformat(),
                    }
                ]
            },
        }
        runtime.prometheus_get.return_value = prom
        runtime.alertmanager_get.return_value = [
            {
                "labels": labels,
                "fingerprint": "0123456789abcdef",
                "startsAt": monitor._alertmanager_wire_time(
                    times[3].isoformat()
                ).isoformat(),
                "status": {"state": "active", "silencedBy": [], "inhibitedBy": []},
            }
        ]
        receipt: dict[str, object] = {
            "run_id": RUN,
            "event_id": EVENT,
            "fingerprint": "0123456789abcdef",
            "starts_at": monitor._alertmanager_wire_time(
                times[3].isoformat()
            ).isoformat(),
            "received_at": times[4].isoformat(),
            "status": "firing",
        }
        receipt["receipt_sha256"] = hashlib.sha256(
            json.dumps(receipt, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        runtime.api.return_value = {"run_id": RUN, "receipts": [receipt], "count": 1}
        return pipeline, runtime, start

    def test_chain_requires_real_prometheus_alertmanager_receipt_and_native_query(
        self,
    ) -> None:
        pipeline, _runtime, start = self.pipeline()
        native = event(
            start + timedelta(seconds=1),
            recorded_at=(start + timedelta(seconds=2)).isoformat(),
        )
        with patch.object(monitor, "NativeClient") as client:
            client.return_value.get.return_value = page([native])
            evidence = pipeline.verify_monitoring(
                INJECT,
                "wrong-tenant",
                start.isoformat(),
                (start + timedelta(seconds=3)).isoformat(),
                TARGET,
                "sa_reader",
                Mock(),
            )
            self.assertTrue(
                client.return_value.get.call_args.args[0].startswith(
                    TARGET.removesuffix("/delivery") + "/audit?"
                )
            )
        self.assertEqual(evidence.native_audit.event_id, EVENT)
        self.assertTrue(evidence.notification_delivered)
        saved = monitor.private(self.directory / "monitoring-native-proof.json")
        self.assertEqual(saved["native_event_id"], EVENT)
        self.assertIn("prometheus_alert", saved)
        self.assertIn("alertmanager_alert", saved)
        self.assertEqual(saved["native_audit_row"], native)
        self.assertEqual(
            (self.directory / "monitoring-native-proof.json").stat().st_mode & 0o777,
            0o600,
        )
        self.assertNotIn(self.config.session_token, json.dumps(saved))

    def test_native_row_retains_allowlist_without_secret_payload_or_arbitrary_details(
        self,
    ) -> None:
        at = datetime.now(UTC) + timedelta(seconds=1)
        native = event(
            at,
            payload={
                "operation": "delivery.fetch",
                "resolution": "resolvable",
                "credential": self.config.session_token,
                "canary": "SYNTHETIC-CANARY",
            },
            request_headers={"Authorization": self.config.session_token},
            private_detail="SYNTHETIC-PRIVATE",
        )
        poller, _reader = self.poller([page([native])])
        with patch.object(monitor, "datetime") as clock:
            clock.now.return_value = at + timedelta(seconds=1)
            clock.fromisoformat = datetime.fromisoformat
            observation = poller.poll()
        first = monitor._object(poller.first)
        row = monitor._object(first["native_audit_row"])
        self.assertEqual(row["id"], EVENT)
        self.assertEqual(row["actor_id"], "sa_reader")
        self.assertEqual(
            row["payload"], {"operation": "delivery.fetch", "resolution": "resolvable"}
        )
        self.assertNotIn(self.config.session_token, json.dumps(first))
        self.assertNotIn("SYNTHETIC-CANARY", json.dumps(first))
        self.assertNotIn("SYNTHETIC-PRIVATE", json.dumps(first))
        self.assertNotIn("actor_id", observation)

    def test_stored_native_row_cannot_change_actor_scope_type_or_selected_clock(
        self,
    ) -> None:
        for field, value in (
            ("actor_id", "foreign"),
            ("org_id", "foreign"),
            ("type", "secret.viewed"),
            ("id", "evt_98aa5c12-c69b-4749-aad9-67fb82e5390a"),
            ("occurred_at", datetime.now(UTC).isoformat()),
            ("occurred_asserted", True),
        ):
            pipeline, _runtime, start = self.pipeline()
            proof = monitor.private(self.directory / "collector-state.json")
            first = monitor._object(proof["first_event"])
            row = monitor._object(first["native_audit_row"])
            row[field] = value
            first["native_audit_row"] = row
            proof["first_event"] = first
            write(self.directory / "collector-state.json", proof)
            with (
                patch.object(monitor, "NativeClient"),
                self.assertRaises(monitor.MonitorError),
            ):
                pipeline.verify_monitoring(
                    INJECT,
                    "wrong-tenant",
                    start.isoformat(),
                    (start + timedelta(seconds=3)).isoformat(),
                    TARGET,
                    "sa_reader",
                    Mock(),
                )

    def test_native_row_is_durable_before_relay_failure_and_teardown(self) -> None:
        at = datetime.now(UTC)
        native = event(at)
        poller = Mock()
        poller.deadline = time.time() + 100
        poller.baseline = at.isoformat()
        poller.first = {
            "event_id": EVENT,
            "occurred_at": at.isoformat(),
            "recorded_at": at.isoformat(),
            "first_polled_at": at.isoformat(),
            "native_audit_row": native,
        }
        poller.poll.return_value = {"run_id": RUN}
        with (
            patch.object(monitor, "Poller", return_value=poller),
            patch.object(monitor, "NativeClient"),
            patch(
                "hikyo_detection_runtime.update_relay_observation",
                side_effect=RuntimeError("transport-unavailable"),
            ),
            self.assertRaisesRegex(monitor.MonitorError, "collector_stopped"),
        ):
            monitor.collector(
                self.path,
                self.directory / "ca",
                self.directory,
                "hikyo-detection-0123456789ab-relay",
                poller.deadline,
            )
        saved = monitor.private(self.directory / "native-audit-readback.json")
        self.assertEqual(saved["native_audit_row"], native)
        self.assertEqual(
            (self.directory / "native-audit-readback.json").stat().st_mode & 0o777,
            0o600,
        )
        stopped = monitor.private(self.directory / "collector-state.json")
        self.assertFalse(stopped["poll_success"])
        self.assertEqual(
            monitor._object(stopped["first_event"])["native_audit_row"], native
        )

    def test_wire_precision_is_exact_floor_and_never_an_arbitrary_window(self) -> None:
        original = "2026-10-09T08:23:19.33963822Z"
        self.assertEqual(
            monitor._alertmanager_wire_time(original),
            monitor._time("2026-10-09T08:23:19.339Z"),
        )
        self.assertNotEqual(
            monitor._alertmanager_wire_time(original),
            monitor._time("2026-10-09T08:23:19.340Z"),
        )
        pipeline, runtime, start = self.pipeline()
        receipt = runtime.api.return_value["receipts"][0]
        receipt["starts_at"] = (
            monitor._time(receipt["starts_at"]) + timedelta(milliseconds=1)
        ).isoformat()
        with (
            patch.object(monitor, "NativeClient"),
            self.assertRaisesRegex(monitor.MonitorError, "fingerprint_mismatch"),
        ):
            pipeline.verify_monitoring(
                INJECT,
                "wrong-tenant",
                start.isoformat(),
                (start + timedelta(seconds=3)).isoformat(),
                TARGET,
                "sa_reader",
                Mock(),
            )

    def test_guard_failure_cancels_wait_and_wrong_receipt_hash_cannot_pass(
        self,
    ) -> None:
        pipeline, runtime, start = self.pipeline()
        with patch.object(monitor, "NativeClient") as client:
            client.return_value.get.return_value = page(
                [event(start + timedelta(seconds=1))]
            )
            with self.assertRaisesRegex(RuntimeError, "guard_stopped"):
                pipeline.verify_monitoring(
                    INJECT,
                    "wrong-tenant",
                    start.isoformat(),
                    (start + timedelta(seconds=3)).isoformat(),
                    TARGET,
                    "sa_reader",
                    Mock(side_effect=RuntimeError("guard_stopped")),
                )
            inbox = runtime.api.return_value
            inbox["receipts"][0]["receipt_sha256"] = "a" * 64
            with self.assertRaisesRegex(monitor.MonitorError, "receipt_hash"):
                pipeline.verify_monitoring(
                    INJECT,
                    "wrong-tenant",
                    start.isoformat(),
                    (start + timedelta(seconds=3)).isoformat(),
                    TARGET,
                    "sa_reader",
                    Mock(),
                )

    def test_later_direct_probe_and_stale_poll_cannot_substitute(self) -> None:
        pipeline, _runtime, start = self.pipeline()
        with (
            patch.object(monitor, "NativeClient"),
            self.assertRaisesRegex(monitor.MonitorError, "outside_execution"),
        ):
            pipeline.verify_monitoring(
                INJECT,
                "wrong-tenant",
                (start - timedelta(seconds=4)).isoformat(),
                start.isoformat(),
                TARGET,
                "sa_reader",
                Mock(),
            )
        proof = monitor.private(self.directory / "collector-state.json")
        proof["last_polled_at"] = (start - timedelta(seconds=60)).isoformat()
        write(self.directory / "collector-state.json", proof)
        with self.assertRaisesRegex(monitor.MonitorError, "not_fresh"):
            pipeline._proof()

    def test_quiet_requires_two_distinct_successful_scrapes_and_empty_inbox(
        self,
    ) -> None:
        pipeline, runtime, _start = self.pipeline()
        proof = monitor.private(self.directory / "collector-state.json")
        proof["first_event"] = None
        write(self.directory / "collector-state.json", proof)
        runtime.api.return_value = {"run_id": RUN, "receipts": [], "count": 0}
        runtime.alertmanager_get.return_value = []
        counter = 0

        def native(path: str) -> dict[str, object]:
            nonlocal counter
            if path.endswith("alerts"):
                return {"status": "success", "data": {"alerts": []}}
            counter += 1
            return {
                "status": "success",
                "data": {
                    "activeTargets": [
                        {
                            "health": "up",
                            "lastScrape": (
                                datetime.now(UTC) + timedelta(microseconds=counter)
                            ).isoformat(),
                        }
                    ]
                },
            }

        runtime.prometheus_get.side_effect = native
        with patch.object(monitor.time, "sleep"):
            result = pipeline.verify_quiet(Mock())
        self.assertEqual(result["completed_scrapes"], 2)
        self.assertFalse(result["rule_fired"])
        runtime.api.return_value = {
            "run_id": RUN,
            "receipts": [{"fake": True}],
            "count": 1,
        }
        with self.assertRaises(monitor.MonitorError):
            pipeline.verify_quiet(Mock())

    def test_quiet_rejects_native_alertmanager_alert(self) -> None:
        pipeline, runtime, _start = self.pipeline()
        proof = monitor.private(self.directory / "collector-state.json")
        proof["first_event"] = None
        write(self.directory / "collector-state.json", proof)
        runtime.prometheus_get.return_value = {
            "status": "success",
            "data": {"alerts": []},
        }
        runtime.api.return_value = {"run_id": RUN, "receipts": [], "count": 0}
        with self.assertRaisesRegex(monitor.MonitorError, "unexpected_alert"):
            pipeline.verify_quiet(Mock())

    def test_native_client_rejects_arbitrary_origin_or_mutation_route_before_io(
        self,
    ) -> None:
        client = object.__new__(monitor.NativeClient)
        client.config = self.config
        for path in [
            "https://other.example/audit",
            "/api/v1/orgs/prod/audit?limit=1",
            "/api/v1/auth/local/login",
        ]:
            with self.assertRaisesRegex(monitor.MonitorError, "route_not_permitted"):
                client.get(path)

    def test_close_stops_owned_collector_and_runtime_even_on_process_failure(
        self,
    ) -> None:
        pipeline, runtime, _start = self.pipeline()
        process = pipeline.process
        if not isinstance(process, Mock):
            raise TypeError("expected mock process")
        process.wait.side_effect = [subprocess.TimeoutExpired("collector", 5), None]
        pipeline.close()
        process.terminate.assert_called_once()
        process.kill.assert_called_once()
        runtime.stop.assert_called_once()


if __name__ == "__main__":
    unittest.main()

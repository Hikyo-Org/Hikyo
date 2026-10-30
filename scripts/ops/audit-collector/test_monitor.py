"""Independent expected-stream checks, synthetic local evidence only."""

from contextlib import closing
import importlib.util
import json
from pathlib import Path
import sqlite3
import tempfile
import time
import unittest
from unittest.mock import patch
import subprocess
from types import SimpleNamespace

from test_receiver import packet, event, receiver, SCRIPT


def module(name):
    spec = importlib.util.spec_from_file_location(name, SCRIPT.with_name(name + ".py"))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


monitor = module("monitor")
health = module("health")
poller = module("poll-once")


class MonitorTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hikyo-1037-monitor-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cfgpath = self.root / "stream.json"
        self.cfgpath.write_text(json.dumps({"source_id": "synthetic", "trail": "tenant",
                                           "scope": {"org_id": "org_fixture"},
                                           "database": "retained.sqlite", "max_bytes": 65536, "max_rows": 10}))
        self.cfg = receiver.config(self.cfgpath)
        receiver.initialize(self.cfg)
        self.pollpath = self.root / "poll.json"
        self.pollpath.write_text(json.dumps({"attempt_ns": time.time_ns(), "outcome": "success"}))
        self.inventory = self.root / "inventory.json"
        self.manifest = {"max_age": 900, "min_free_bytes": 1,
                         "streams": {"tenant": {"config": str(self.cfgpath), "poll_status": str(self.pollpath)}}}
        self.state, self.output = self.root / "state.json", self.root / "health.json"

    def check(self):
        self.inventory.write_text(json.dumps(self.manifest))
        return monitor.check(self.inventory, SCRIPT, self.state, self.output)

    def complete(self, batch="first"):
        receiver.receive(self.cfg, packet([event()], batch))

    def test_never_received_interval_stays_unresolved(self):
        self.assertFalse(self.check()["healthy"])
        self.complete()
        self.assertFalse(self.check()["healthy"], "never-received interval remains unresolved")
        self.assertFalse(self.check()["healthy"])

    def test_missing_stream_and_stopped_detector(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        self.assertTrue(health.healthy(self.output))
        state = json.loads(self.output.read_text())
        state["checked_ns"] -= 121_000_000_000
        self.output.write_text(json.dumps(state))
        self.assertFalse(health.healthy(self.output))
        self.manifest["streams"]["instance"] = {"config": str(self.root / "absent")}
        self.assertFalse(self.check()["healthy"])
        del self.manifest["streams"]["instance"]
        self.assertFalse(self.check()["healthy"])
        self.assertFalse(self.check()["healthy"], "inventory removal must stay HOLD")

    def test_refusal_history_is_sticky_even_between_monitor_checks(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        with self.assertRaises(receiver.Refused):
            receiver.receive(self.cfg, b"partial\n")
        self.complete("new-success")
        self.assertTrue(receiver.status(self.cfg, 900)["healthy"])
        result = self.check()
        self.assertFalse(result["healthy"])
        self.assertIn("unresolved-collection-history", result["streams"]["tenant"]["reasons"])
        self.state.unlink()  # Reset cannot erase independently retained history.
        self.assertFalse(self.check()["healthy"])

    def test_stale_future_capacity_auth_transport_and_stopped_poller(self):
        self.complete()
        for poll in ({"attempt_ns": time.time_ns(), "outcome": "HOLD"},
                     {"attempt_ns": time.time_ns() - 901_000_000_000, "outcome": "success"},
                     {"attempt_ns": time.time_ns() + 60_000_000_000, "outcome": "success"}):
            with self.subTest(poll=poll):
                self.pollpath.write_text(json.dumps(poll))
                self.assertFalse(self.check()["healthy"])
        self.pollpath.write_text(json.dumps({"attempt_ns": time.time_ns(), "outcome": "success"}))
        self.manifest["min_free_bytes"] = 2**63
        self.assertFalse(self.check()["healthy"])
        self.manifest["min_free_bytes"] = 1
        for delta in (-901_000_000_000, 60_000_000_000):
            with closing(sqlite3.connect(self.cfg["database"])) as db:
                db.execute("UPDATE batches SET received_ns=?", (time.time_ns() + delta,))
                db.commit()
            self.assertFalse(self.check()["healthy"])

    def test_failed_and_killed_source_attempt_between_checks_stays_hold(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        for code in (1, 0):
            with patch.object(monitor.subprocess, "run", return_value=subprocess.CompletedProcess([], code)):
                monitor.poll(str(SCRIPT), str(self.cfgpath), self.pollpath, [])
        self.assertFalse(self.check()["healthy"], "success must preserve failed attempt history")
        state = json.loads(self.pollpath.read_bytes())
        state["outcome"] = "pending"
        self.pollpath.write_text(json.dumps(state))
        with patch.object(monitor.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)):
            monitor.poll(str(SCRIPT), str(self.cfgpath), self.pollpath, [])
        self.assertEqual(json.loads(self.pollpath.read_bytes())["failure_count"], 2)
        self.assertFalse(self.check()["healthy"])

    def test_missed_detector_interval_and_recovered_transport_stay_hold(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        state = json.loads(self.state.read_bytes())
        state["tenant"]["last_checked_ns"] -= 121_000_000_000
        self.state.write_text(json.dumps(state))
        self.assertFalse(self.check()["healthy"])
        self.assertFalse(self.check()["healthy"])

    def test_killed_run_gap_below_two_minutes_latches_its_own_hold(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        state = json.loads(self.state.read_bytes())
        state["tenant"]["last_checked_ns"] -= 100_000_000_000
        self.state.write_text(json.dumps(state))
        result = self.check()
        self.assertFalse(result["healthy"])
        self.assertIn("detector-interval-HOLD", result["streams"]["tenant"]["reasons"])
        self.assertEqual(self.check()["streams"]["tenant"]["reasons"], ["unresolved-collection-history"])

    def test_ordinary_minute_cadence_with_accuracy_margin_stays_healthy(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        state = json.loads(self.state.read_bytes())
        state["tenant"]["last_checked_ns"] -= 65_000_000_000
        self.state.write_text(json.dumps(state))
        self.assertTrue(self.check()["healthy"])

    def test_delayed_interval_below_detector_limit_stays_healthy(self):
        """A delayed minute check within the accepted margin stays healthy."""
        self.complete()
        self.assertTrue(self.check()["healthy"])
        state = json.loads(self.state.read_bytes())
        state["tenant"]["last_checked_ns"] -= 85_000_000_000
        self.state.write_text(json.dumps(state))
        self.assertTrue(self.check()["healthy"])

    def test_detector_interval_inclusive_boundary_and_first_rejected_second(self):
        """Accept exactly 90 seconds; latch the first whole second above it."""
        self.complete()
        now = time.time_ns()
        for seconds, expected_healthy in ((90, True), (91, False)):
            with self.subTest(seconds=seconds), \
                    patch.object(self, "state", self.root / f"boundary-{seconds}.json"), \
                    patch.object(self, "output", self.root / f"health-{seconds}.json"), \
                    patch.object(monitor.time, "time_ns", return_value=now):
                self.assertTrue(self.check()["healthy"])
                state = json.loads(self.state.read_bytes())
                state["tenant"]["last_checked_ns"] -= seconds * 1_000_000_000
                self.state.write_text(json.dumps(state))
                result = self.check()
                self.assertEqual(result["healthy"], expected_healthy)
                if not expected_healthy:
                    self.assertIn("detector-interval-HOLD", result["streams"]["tenant"]["reasons"])
                    self.assertEqual(self.check()["streams"]["tenant"]["reasons"], ["unresolved-collection-history"])

    def test_detector_setup_failure_then_fast_recovery_is_latched(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        self.inventory.write_text("invalid inventory")
        result = monitor.check(self.inventory, SCRIPT, self.state, self.output)
        self.assertFalse(result["healthy"])
        self.assertFalse(health.healthy(self.output), "failure must immediately replace healthy snapshot")
        self.assertFalse(self.check()["healthy"], "fast inventory repair cannot erase detector failure")
        self.assertTrue(self.state.with_suffix(".detector-hold").exists())

    def test_expired_preflight_then_success_before_detection_is_retained(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        path = self.root / "poll-pg-tenant.json"
        def callback():
            return poller.arguments("pg-tenant", False)
        with patch.object(poller, "CONFIG", self.root), patch.object(Path, "stat", return_value=SimpleNamespace(st_uid=0, st_mode=0o100644)):
            path.write_text(json.dumps({"url": "https://fixture.invalid/api/v1/instance/audit/export?receipt=v1",
                                        "approved_until": "1970-01-01T00:00:00Z"}))
            with patch.object(monitor.subprocess, "run") as transport:
                self.assertEqual(monitor.poll(str(SCRIPT), str(self.cfgpath), self.pollpath, [], preflight=callback), 1)
                transport.assert_not_called()
            path.write_text(json.dumps({"url": "https://fixture.invalid/api/v1/instance/audit/export?receipt=v1",
                                        "approved_until": "2099-01-01T00:00:00Z"}))
            with patch.object(monitor.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)):
                self.assertEqual(monitor.poll(str(SCRIPT), str(self.cfgpath), self.pollpath, [], preflight=callback), 0)
        self.assertEqual(json.loads(self.pollpath.read_bytes())["failure_count"], 1)
        self.assertFalse(self.check()["healthy"])

    def test_valid_json_wrong_container_shapes_latch_before_fast_recovery(self):
        self.complete()
        for number, invalid in enumerate(([], {"streams": ["tenant"], "max_age": 900, "min_free_bytes": 1},
                                         {"streams": {"tenant": []}, "max_age": 900, "min_free_bytes": 1})):
            with self.subTest(invalid=invalid):
                state = self.root / f"shape-{number}.json"
                output = self.root / f"health-{number}.json"
                self.inventory.write_text(json.dumps(self.manifest))
                self.assertTrue(monitor.check(self.inventory, SCRIPT, state, output)["healthy"])
                self.inventory.write_text(json.dumps(invalid))
                self.assertFalse(monitor.check(self.inventory, SCRIPT, state, output)["healthy"])
                self.assertFalse(health.healthy(output))
                self.inventory.write_text(json.dumps(self.manifest))
                self.assertFalse(monitor.check(self.inventory, SCRIPT, state, output)["healthy"])

    def test_wrong_state_shape_is_unhealthy_and_latched(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        self.state.write_text("[]")
        self.assertFalse(self.check()["healthy"])
        self.state.write_text("{}")
        self.assertFalse(self.check()["healthy"])

    def test_wrong_poll_shape_has_its_own_healthy_baseline_and_latches(self):
        self.complete()
        self.assertTrue(self.check()["healthy"])
        self.pollpath.write_text("[]")
        failed = self.check()
        self.assertFalse(failed["healthy"])
        self.assertIn("check-unavailable-HOLD", failed["streams"]["tenant"]["reasons"])
        self.pollpath.write_text(json.dumps({"attempt_ns": time.time_ns(), "outcome": "success", "failure_count": 0}))
        recovered = self.check()
        self.assertFalse(recovered["healthy"])
        self.assertEqual(recovered["streams"]["tenant"]["reasons"], ["unresolved-collection-history"])


if __name__ == "__main__":
    unittest.main()

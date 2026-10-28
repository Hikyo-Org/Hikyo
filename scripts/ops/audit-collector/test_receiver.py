"""Synthetic destination rehearsal. This does not exercise a live Hikyo API."""

import importlib.util
from argparse import Namespace
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("receiver.py")
SPEC = importlib.util.spec_from_file_location("receiver", SCRIPT)
receiver = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(receiver)


def event(event_id="evt_tenant", seq=20, **fields):
    return {"id": event_id, "seq": seq, "type": "settings.project_created",
            "org_id": "org_fixture", "project_id": "prj_fixture",
            "schema_version": 1, "occurred_at": "2026-10-28T00:00:00Z",
            "occurred_asserted": False, "recorded_at": "2026-10-28T00:00:00Z",
            "actor_class": "human", "scope_class": "project", "outcome": "success",
            "origin": "cli", "payload": {}, **fields}


def packet(rows=(), batch="evt_export", **overrides):
    receipt = {"version": 1, "started_event_id": batch,
               "snapshot_time": "2026-10-28T01:00:00Z",
               "rows_streamed": len(rows), "outcome": "success",
               "outcome_recorded": True, **overrides}
    framing = ("Hikyo-Audit-Receipt: v1\nHikyo-Audit-Export-ID: " + batch
               + "\nHikyo-Audit-Snapshot-Time: 2026-10-28T01:00:00Z\n\n").encode()
    return (framing + b"".join(
        (json.dumps(row) + "\n").encode() for row in rows)
        + (json.dumps({"export_receipt": receipt}) + "\n").encode())


class ReceiverTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hikyo-1037-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.path = self.root / "stream.json"
        self.path.write_text(json.dumps({
            "source_id": "synthetic-instance", "trail": "tenant",
            "scope": {"org_id": "org_fixture", "project_id": "prj_fixture"},
            "database": "evidence.sqlite", "max_bytes": 65536, "max_rows": 10,
        }))
        self.cfg = receiver.config(self.path)
        receiver.initialize(self.cfg)

    def counts(self):
        with closing(sqlite3.connect(self.cfg["database"])) as db:
            return tuple(db.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
                         for table in ("events", "batches"))

    def cli(self, command, body=b"", **kwargs):
        return subprocess.run([sys.executable, "-I", "-B", str(SCRIPT),
                               command, str(self.path)], input=body,
                              capture_output=True, check=False, **kwargs)

    def test_ack_is_durable_deduplicated_and_survives_producer_deletion(self):
        producer = self.root / "producer.jsonl"
        original = packet([event()])
        producer.write_bytes(original)
        first = self.cli("receive", producer.read_bytes())
        self.assertEqual(first.returncode, 0, first.stderr)
        acknowledgment = json.loads(first.stdout)
        self.assertEqual(acknowledgment["source_id"], "synthetic-instance")
        producer.write_bytes(b"rewritten producer")
        producer.unlink()
        with closing(sqlite3.connect(self.cfg["database"])) as independent_reader:
            self.assertEqual(independent_reader.execute(
                "SELECT packet FROM batches").fetchone()[0], original)
        retry = self.cli("receive", original)
        self.assertEqual(retry.returncode, 0, retry.stderr)
        self.assertEqual(json.loads(retry.stdout), acknowledgment)
        self.assertEqual(self.counts(), (1, 1))

    def test_lost_ack_after_commit_restarts_without_double_delivery(self):
        # Real subprocess exits after receive commits, before any ack is sent.
        child = """
import importlib.util, os, sys
spec = importlib.util.spec_from_file_location('receiver', sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
module.receive(module.config(sys.argv[2]), sys.stdin.buffer.read())
os._exit(23)
"""
        body = packet([event()])
        lost = subprocess.run([sys.executable, "-I", "-B", "-c", child,
                               str(SCRIPT), str(self.path)], input=body,
                              capture_output=True, check=False)
        self.assertEqual(lost.returncode, 23, lost.stderr)
        self.assertEqual(lost.stdout, b"")
        self.assertEqual(self.counts(), (1, 1))
        self.assertEqual(self.cli("receive", body).returncode, 0)
        self.assertEqual(self.counts(), (1, 1))

    def test_failure_before_commit_rolls_back_events_and_checkpoint(self):
        receiver.receive(self.cfg, packet([event()], "evt_first"))
        with closing(sqlite3.connect(self.cfg["database"])) as db:
            db.execute("""CREATE TRIGGER destination_failure BEFORE INSERT ON batches
                       BEGIN SELECT RAISE(ABORT, 'synthetic destination failure'); END""")
        body = packet([event("evt_second", 30)], "evt_second_export")
        refused = self.cli("receive", body)
        self.assertNotEqual(refused.returncode, 0)
        self.assertEqual(refused.stdout, b"")
        self.assertEqual(self.counts(), (1, 1))
        self.assertNotEqual(self.cli("status").returncode, 0)
        with closing(sqlite3.connect(self.cfg["database"])) as db:
            db.execute("DROP TRIGGER destination_failure")
        self.assertEqual(self.cli("receive", body).returncode, 0)
        self.assertEqual(self.counts(), (2, 2))

    def test_process_crash_before_commit_recovers_journal_and_retries(self):
        child = """
import importlib.util, os, sqlite3, sys
spec = importlib.util.spec_from_file_location('receiver', sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
original_connect = sqlite3.connect
class Interrupted(sqlite3.Connection):
    def execute(self, sql, *args):
        if sql.startswith('INSERT INTO batches'):
            os._exit(24)
        return super().execute(sql, *args)
module.sqlite3.connect = lambda *args, **kw: original_connect(
    *args, **kw, factory=Interrupted)
module.receive(module.config(sys.argv[2]), sys.stdin.buffer.read())
"""
        body = packet([event()])
        crashed = subprocess.run([sys.executable, "-I", "-B", "-c", child,
                                  str(SCRIPT), str(self.path)], input=body,
                                 capture_output=True, check=False)
        self.assertEqual(crashed.returncode, 24, crashed.stderr)
        self.assertEqual(self.counts(), (0, 0))
        self.assertEqual(self.cli("receive", body).returncode, 0)
        self.assertEqual(self.counts(), (1, 1))

    def test_http200_with_valid_lines_without_receipt_cannot_advance(self):
        receiver.receive(self.cfg, packet([event()], "evt_first"))
        partial = packet([event("evt_later", 99)], "evt_later_export")
        partial = b"\n".join(partial.split(b"\n")[:-2]) + b"\n"
        self.assertRaises(receiver.Refused, receiver.receive, self.cfg, partial)
        self.assertEqual(self.counts(), (1, 1))
        self.assertTrue(receiver.status(self.cfg, 900)["receive_failure"])

    def test_negative_receipts_trailing_records_and_scope_are_rejected(self):
        bodies = [
            packet([event()], outcome="failure"),
            packet([event()], outcome="revoked"),
            packet([event()], outcome="disconnected"),
            packet([event()], outcome_recorded=False),
            packet([event()], rows_streamed=0),
            packet([event()], version=True),
            packet([event()], cause="outcome-write-failed"),
            packet([event()], started_event_id=""),
            packet([event()], started_event_id="evt_different_response"),
            packet([event()], snapshot_time="2026-10-28T02:00:00Z"),
            packet([event()]) + b"{}\n",
            packet([event(), event()]),
            packet([event(org_id="org_foreign")]),
            packet([event()]).replace(b"v1\n", b"v2\n", 1),
            packet([event()])[:-1],
            b"x" * 65537,
            packet([event(str(i), i + 1) for i in range(11)]),
            packet([event()]).replace(b'"seq": 20', b'"seq": 20, "seq": 21'),
        ]
        for body in bodies:
            with self.subTest(body=body[:70]):
                self.assertRaises(ValueError, receiver.receive, self.cfg, body)
                self.assertEqual(self.counts(), (0, 0))

    def test_full_replay_covers_late_lower_sequence_and_deduplicates(self):
        high = event("evt_high", 100)
        low = event("evt_late_commit", 2)
        receiver.receive(self.cfg, packet([high], "evt_first"))
        receiver.receive(self.cfg, packet([high, low], "evt_second"))
        self.assertEqual(self.counts(), (2, 2))
        # Retention removes the producer's first row. Received data is retained.
        receiver.receive(self.cfg, packet([low], "evt_after_prune"))
        self.assertEqual(self.counts(), (2, 3))

    def test_conflicts_do_not_replace_independent_evidence(self):
        receiver.receive(self.cfg, packet([event()], "evt_first"))
        for body in (packet([event(seq=21)], "evt_first"),
                     packet([event(seq=21)], "evt_new_export")):
            self.assertRaises(receiver.Refused, receiver.receive, self.cfg, body)
            self.assertEqual(self.counts(), (1, 1))

    def test_large_integers_and_close_decimals_do_not_collapse_content(self):
        row = event(seq=9007199254740993, payload={"number": "NUMBER"})
        first = packet([row], "evt_first").replace(b'"NUMBER"', b"1.00000000000000001")
        second = packet([row], "evt_second").replace(b'"NUMBER"', b"1.00000000000000002")
        receiver.receive(self.cfg, first)
        self.assertRaises(receiver.Refused, receiver.receive, self.cfg, second)
        self.assertEqual(self.counts(), (1, 1))

    def test_line_limit_and_exact_terminal_lf(self):
        cfg = {**self.cfg, "max_bytes": 2 * 1024 * 1024}
        oversized = packet([event(payload={"value": "x" * (1024 * 1024)})])
        self.assertRaises(receiver.Refused, receiver.receive, cfg, oversized)
        for suffix in (b"\n", b" ", b"{}", b"{}\n"):
            self.assertRaises(ValueError, receiver.receive, cfg, packet([]) + suffix)
        self.assertEqual(self.counts(), (0, 0))

    def test_empty_instance_success_and_wrong_trail_negative(self):
        cfg = {**self.cfg, "trail": "instance", "scope": {},
               "database": str(self.root / "instance.sqlite")}
        receiver.initialize(cfg)
        receiver.receive(cfg, packet([], "evt_empty"))
        self.assertRaises(receiver.Refused, receiver.receive, cfg, packet([event()]))
        row = event("evt_login", 1, type="auth.login", scope_class="instance", outcome="failure")
        del row["org_id"], row["project_id"]
        receiver.receive(cfg, packet([row], "evt_instance_export"))
        self.assertTrue(receiver.status(cfg, 900)["healthy"])

    def test_independent_clock_staleness_replays_and_loss_of_authority(self):
        self.assertFalse(receiver.status(self.cfg, 900)["healthy"])
        body = packet([event(recorded_at="2099-01-01T00:00:00Z")])
        with patch.object(receiver.time, "time_ns", return_value=1_000_000_000):
            acknowledgment = receiver.receive(self.cfg, body)
        with patch.object(receiver.time, "time_ns", return_value=1001_000_000_000):
            self.assertEqual(receiver.receive(self.cfg, body), acknowledgment)
            self.assertFalse(receiver.status(self.cfg, 900)["healthy"])
        # Unavailable/misbound destination never silently starts a new journal.
        missing = {**self.cfg, "database": str(self.root / "missing.sqlite")}
        self.assertRaises(sqlite3.Error, receiver.receive, missing, body)
        wrong = {**self.cfg, "source_id": "other-instance"}
        self.assertRaises(receiver.Refused, receiver.receive, wrong, body)
        self.assertRaises(FileExistsError, receiver.initialize, self.cfg)
        self.assertFalse(Path(missing["database"]).exists())

    def test_cli_caps_stdin_and_emits_no_producer_content_on_failure(self):
        result = self.cli("receive", b"sensitive-fixture" * 10000)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, b"")
        self.assertNotIn(b"sensitive-fixture", result.stderr)
        self.assertEqual(self.counts(), (0, 0))

    def collector(self):
        spool = self.root / "spool"
        spool.mkdir()
        return Namespace(url="https://fixture.invalid/api/v1/orgs/org_fixture/"
                         "projects/prj_fixture/audit/export?receipt=v1",
                         ssh_host="evidence-fixture", ssh_config="private-ssh-config",
                         curl_config="private-curl-config", spool=str(spool))

    def transport(self, body, *, header=None,
                  destination_down=False, wrong_ack=False):
        if header is None:
            header = (b"Content-Type: application/x-ndjson\r\nHikyo-Audit-Receipt: v1\r\n"
                      b"Hikyo-Audit-Export-ID: evt_export\r\n"
                      b"Hikyo-Audit-Snapshot-Time: 2026-10-28T01:00:00Z\r\n")
        def run(argv, **kwargs):
            if argv[0] == "curl":
                self.assertIn("-q", argv)
                self.assertNotIn("--location", argv)
                self.assertNotIn("--insecure", argv)
                self.assertEqual(argv[argv.index("--proto") + 1], "=https")
                Path(argv[argv.index("--dump-header") + 1]).write_bytes(header)
                Path(argv[argv.index("--output") + 1]).write_bytes(body)
                return subprocess.CompletedProcess(argv, 0, b"200", b"")
            self.assertEqual(argv[0], "ssh")
            self.assertIn("-oStrictHostKeyChecking=yes", argv)
            if destination_down:
                raise subprocess.CalledProcessError(255, argv)
            ack = receiver.receive(self.cfg, kwargs["input"])
            if wrong_ack:
                ack["source_id"] = "wrong-instance"
            return subprocess.CompletedProcess(argv, 0, json.dumps(ack).encode(), b"")
        return run

    def test_collect_retries_durable_pending_before_contacting_source(self):
        args = self.collector()
        original = packet([event()])
        body = original.split(b"\n\n", 1)[1]
        with patch.object(receiver.subprocess, "run", side_effect=self.transport(
                body, destination_down=True)):
            self.assertRaises(subprocess.CalledProcessError, receiver.collect,
                              self.cfg, args)
        spool = Path(args.spool)
        self.assertEqual(len(list(spool.glob("*.pending"))), 1)
        self.assertEqual(list(spool.glob("*.checkpoint")), [])
        # Producer copy is now unavailable. Retry only sends retained pending.
        with patch.object(receiver.subprocess, "run", side_effect=self.transport(b"")) as calls:
            ack = receiver.collect(self.cfg, args)
            self.assertEqual([call.args[0][0] for call in calls.call_args_list], ["ssh"])
        self.assertEqual(ack["rows"], 1)
        self.assertEqual(list(spool.glob("*.pending")), [])
        self.assertEqual(len(list(spool.glob("*.checkpoint"))), 1)
        self.assertEqual(self.counts(), (1, 1))

    def test_collect_requires_negotiation_and_terminal_success_before_send(self):
        args = self.collector()
        for header, body in (
            (b"", packet([event()]).split(b"\n\n", 1)[1]),
            (b"Hikyo-Audit-Receipt: v2\r\n", b"{}\n"),
            (b"Hikyo-Audit-Receipt: v1\r\n", json.dumps(event()).encode() + b"\n"),
        ):
            with self.subTest(header=header, body=body[:40]):
                with patch.object(receiver.subprocess, "run", side_effect=self.transport(
                        body, header=header)) as calls:
                    self.assertRaises(ValueError, receiver.collect, self.cfg, args)
                    self.assertEqual(calls.call_count, 1)
                self.assertEqual(list(Path(args.spool).glob("*.pending")), [])
                self.assertEqual(self.counts(), (0, 0))

    def test_collect_mismatched_ack_preserves_pending_and_checkpoint(self):
        args = self.collector()
        body = packet([event()]).split(b"\n\n", 1)[1]
        with patch.object(receiver.subprocess, "run", side_effect=self.transport(
                body, wrong_ack=True)):
            self.assertRaises(receiver.Refused, receiver.collect, self.cfg, args)
        self.assertEqual(list(Path(args.spool).glob("*.checkpoint")), [])
        self.assertEqual(len(list(Path(args.spool).glob("*.pending"))), 1)
        self.assertEqual(self.counts(), (1, 1))  # Destination committed, ack uncertain.
        with patch.object(receiver.subprocess, "run", side_effect=self.transport(b"")):
            receiver.collect(self.cfg, args)
        self.assertEqual(self.counts(), (1, 1))

    def test_source_authority_loss_and_wrong_route_never_advance(self):
        args = self.collector()
        with patch.object(receiver.subprocess, "run", side_effect=
                          subprocess.CalledProcessError(22, ["curl"])):
            self.assertRaises(subprocess.CalledProcessError, receiver.collect, self.cfg, args)
        self.assertEqual(list(Path(args.spool).glob("*.checkpoint")), [])
        for url in (args.url.replace("https:", "http:"), args.url + "&after_seq=100",
                    args.url.replace("org_fixture", "org_foreign")):
            with patch.object(receiver.subprocess, "run") as calls:
                wrong = Namespace(**{**vars(args), "url": url})
                self.assertRaises(receiver.Refused, receiver.collect, self.cfg, wrong)
                calls.assert_not_called()


if __name__ == "__main__":
    unittest.main()

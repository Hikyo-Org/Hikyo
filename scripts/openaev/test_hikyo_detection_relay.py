"""Schema, freshness and receipt regressions for the isolated relay."""

from __future__ import annotations

import hashlib
import json
import tempfile
import time
import unittest
from datetime import UTC, datetime
from pathlib import Path

from hikyo_detection_relay import (
    ALERT_NAME,
    RelayError,
    RelayState,
    validate_observation,
)

RUN_ID = "f5c301bd-b75e-4343-a44c-1b7e827fbd1a"
EVENT_ID = "evt_3c487fa3-721a-4fd9-a3b3-81fc8337c63d"


def observation(deadline: float) -> dict[str, object]:
    return {
        "run_id": RUN_ID,
        "native_event_id": EVENT_ID,
        "native_denial": True,
        "audit_poll_success": True,
        "audit_last_success_unixtime": time.time(),
        "run_deadline_unixtime": deadline,
    }


def webhook() -> dict[str, object]:
    return {
        "status": "firing",
        "alerts": [
            {
                "status": "firing",
                "labels": {
                    "alertname": ALERT_NAME,
                    "run_id": RUN_ID,
                    "event_id": EVENT_ID,
                },
                "fingerprint": "0123456789abcdef",
                "startsAt": datetime.now(UTC).isoformat(),
            }
        ],
    }


class RelayTests(unittest.TestCase):
    def test_observation_rejects_private_data_invalid_ids_and_nonfinite_values(
        self,
    ) -> None:
        data = observation(time.time() + 300)
        for changed in (
            {**data, "actor": "secret-actor"},
            {**data, "run_id": 'untrusted"metric'},
            {**data, "native_event_id": "evt_invalid"},
            {**data, "audit_last_success_unixtime": float("nan")},
            {**data, "native_denial": 1},
            {**data, "native_event_id": None},
        ):
            with self.assertRaises(RelayError):
                validate_observation(changed, RUN_ID)

    def test_metrics_emit_only_verified_event_and_lease_cannot_extend(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            state = RelayState(RUN_ID, Path(directory), 60)
            self.assertNotIn(b"hikyo_lab_native_denial", state.metrics())
            state.update(observation(state.lease_deadline))
            self.assertIn(EVENT_ID.encode(), state.metrics())
            with self.assertRaises(RelayError):
                state.update(observation(state.lease_deadline + 10))
            with self.assertRaises(RelayError):
                state.update(
                    {
                        **observation(state.lease_deadline),
                        "native_event_id": "evt_29b5e457-f47a-49bb-bb9e-6d9e459dc2bc",
                    }
                )

    def test_receipt_is_sanitized_hashed_persisted_and_deduplicated(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            state = RelayState(RUN_ID, Path(directory), 60)
            state.update(observation(state.lease_deadline))
            body = webhook()
            state.receive(body)
            state.receive(body)
            self.assertEqual(state.inbox()["count"], 1)
            receipt = state.receipts[0]
            unhashed = {
                key: value for key, value in receipt.items() if key != "receipt_sha256"
            }
            digest = hashlib.sha256(
                json.dumps(unhashed, sort_keys=True, separators=(",", ":")).encode()
            ).hexdigest()
            self.assertEqual(receipt["receipt_sha256"], digest)
            self.assertEqual(
                set(receipt),
                {
                    "run_id",
                    "event_id",
                    "fingerprint",
                    "starts_at",
                    "received_at",
                    "status",
                    "receipt_sha256",
                },
            )
            self.assertEqual(
                json.loads((Path(directory) / "inbox.json").read_text()), state.inbox()
            )
            self.assertEqual(
                (Path(directory) / "inbox.json").stat().st_mode & 0o777, 0o600
            )

    def test_webhook_rejects_wrong_identity_stale_poll_and_failed_poll(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            state = RelayState(RUN_ID, Path(directory), 60)
            with self.assertRaises(RelayError):
                state.receive(webhook())
            state.update(
                {
                    **observation(state.lease_deadline),
                    "audit_last_success_unixtime": time.time() - 11,
                }
            )
            with self.assertRaises(RelayError):
                state.receive(webhook())
            state.update(
                {**observation(state.lease_deadline), "audit_poll_success": False}
            )
            with self.assertRaises(RelayError):
                state.receive(webhook())
            state.update(observation(state.lease_deadline))
            wrong = webhook()
            alerts = wrong["alerts"]
            if not isinstance(alerts, list):
                self.fail("invalid fixture")
            alerts[0]["labels"]["run_id"] = "00000000-0000-0000-0000-000000000000"
            with self.assertRaises(RelayError):
                state.receive(wrong)
            self.assertEqual(state.inbox()["count"], 0)


if __name__ == "__main__":
    unittest.main()

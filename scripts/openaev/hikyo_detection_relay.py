"""Bounded transport for a native-audit collector and isolated Docker monitoring.

The internal-only Docker network has no published ports. This relay handles no
Hikyo credentials, scopes, actors or request bodies. Its local Docker operator is
trusted. Native Alertmanager readback must corroborate receipt provenance.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import re
import socket
import threading
import time
import uuid
from datetime import UTC, datetime
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

PORT = 18790
ALERT_NAME = "HikyoLabCrossTenantDenied"
MAX_BODY = 65_536
MAX_RECEIPTS = 32
OBSERVATION_KEYS = {
    "run_id",
    "native_event_id",
    "native_denial",
    "audit_poll_success",
    "audit_last_success_unixtime",
    "run_deadline_unixtime",
}


class RelayError(ValueError):
    """Closed error code safe to log."""


def canonical_uuid(value: object) -> str:
    if not isinstance(value, str):
        raise RelayError("invalid_uuid")
    try:
        if str(uuid.UUID(value)) != value:
            raise ValueError()
    except ValueError:
        raise RelayError("invalid_uuid") from None
    return value


def event_identifier(value: object) -> str:
    if not isinstance(value, str) or not value.startswith("evt_"):
        raise RelayError("invalid_event_id")
    canonical_uuid(value[4:])
    return value


def object_value(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or not all(isinstance(k, str) for k in value):
        raise RelayError("invalid_json_object")
    return {k: v for k, v in value.items()}


def finite_timestamp(value: object) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise RelayError("invalid_timestamp")
    result = float(value)
    if not math.isfinite(result) or result < 0:
        raise RelayError("invalid_timestamp")
    return result


def validate_observation(value: object, run_id: str) -> dict[str, object]:
    data = object_value(value)
    if set(data) != OBSERVATION_KEYS or canonical_uuid(data["run_id"]) != run_id:
        raise RelayError("invalid_observation_schema")
    event = data["native_event_id"]
    if event is not None:
        event_identifier(event)
    for key in ("native_denial", "audit_poll_success"):
        if not isinstance(data[key], bool):
            raise RelayError("invalid_observation_boolean")
    if data["native_denial"] and event is None:
        raise RelayError("denial_requires_native_event")
    finite_timestamp(data["audit_last_success_unixtime"])
    finite_timestamp(data["run_deadline_unixtime"])
    return data


def utc_timestamp(value: object) -> str:
    if not isinstance(value, str) or len(value) > 40:
        raise RelayError("invalid_alert_timestamp")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        if parsed.tzinfo is None or parsed.utcoffset() != UTC.utcoffset(parsed):
            raise ValueError()
    except ValueError:
        raise RelayError("invalid_alert_timestamp") from None
    return value


class RelayState:
    def __init__(self, run_id: str, directory: Path, lease_seconds: int = 600):
        self.run_id = canonical_uuid(run_id)
        if not 10 <= lease_seconds <= 600:
            raise RelayError("invalid_lease")
        self.lease_deadline = time.time() + lease_seconds
        self.directory = directory
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.lock = threading.Lock()
        self.observation: dict[str, object] = {
            "run_id": run_id,
            "native_event_id": None,
            "native_denial": False,
            "audit_poll_success": False,
            "audit_last_success_unixtime": 0,
            "run_deadline_unixtime": self.lease_deadline,
        }
        self.receipts: list[dict[str, object]] = []

    def _save(self, name: str, value: object) -> None:
        target = self.directory / name
        temporary = self.directory / (name + ".tmp")
        temporary.write_text(json.dumps(value, sort_keys=True), encoding="utf-8")
        temporary.chmod(0o600)
        temporary.replace(target)

    def update(self, value: object) -> None:
        data = validate_observation(value, self.run_id)
        now = time.time()
        deadline = finite_timestamp(data["run_deadline_unixtime"])
        if deadline > self.lease_deadline + 1 or deadline <= now:
            raise RelayError("observation_outside_lease")
        if finite_timestamp(data["audit_last_success_unixtime"]) > now + 1:
            raise RelayError("future_audit_success")
        with self.lock:
            previous = self.observation["native_event_id"]
            if previous is not None and data["native_event_id"] != previous:
                raise RelayError("native_event_changed")
            self.observation = data
            self._save("observation.json", data)

    def metrics(self) -> bytes:
        with self.lock:
            data = dict(self.observation)
        labels = '{run_id="' + self.run_id + '"}'
        lines = [
            "hikyo_lab_audit_poll_success"
            + labels
            + " "
            + str(int(bool(data["audit_poll_success"]))),
            "hikyo_lab_audit_last_success_unixtime"
            + labels
            + " "
            + str(data["audit_last_success_unixtime"]),
            "hikyo_lab_run_deadline_unixtime"
            + labels
            + " "
            + str(data["run_deadline_unixtime"]),
        ]
        if data["native_event_id"] is not None:
            event_labels = (
                '{run_id="'
                + self.run_id
                + '",event_id="'
                + str(data["native_event_id"])
                + '"}'
            )
            lines.append(
                "hikyo_lab_native_denial"
                + event_labels
                + " "
                + str(int(bool(data["native_denial"])))
            )
        return ("\n".join(lines) + "\n").encode("ascii")

    def inbox(self) -> dict[str, object]:
        with self.lock:
            receipts = [dict(item) for item in self.receipts]
        return {"run_id": self.run_id, "count": len(receipts), "receipts": receipts}

    def receive(self, value: object) -> None:
        data = object_value(value)
        alerts = data.get("alerts")
        if (
            data.get("status") != "firing"
            or not isinstance(alerts, list)
            or len(alerts) != 1
        ):
            raise RelayError("invalid_alert_group")
        alert = object_value(alerts[0])
        labels = object_value(alert.get("labels"))
        if labels.get("alertname") != ALERT_NAME or labels.get("run_id") != self.run_id:
            raise RelayError("wrong_alert_identity")
        event_id = event_identifier(labels.get("event_id"))
        fingerprint = alert.get("fingerprint")
        if not isinstance(fingerprint, str) or not re.fullmatch(
            r"[0-9a-f]{16}", fingerprint
        ):
            raise RelayError("invalid_alert_fingerprint")
        starts_at = utc_timestamp(alert.get("startsAt"))
        if alert.get("status") != "firing":
            raise RelayError("nonfiring_alert")
        now = time.time()
        with self.lock:
            observed = self.observation
            last_success = finite_timestamp(observed["audit_last_success_unixtime"])
            if (
                observed["native_event_id"] != event_id
                or observed["native_denial"] is not True
                or observed["audit_poll_success"] is not True
                or not 0 <= now - last_success <= 10
                or now >= finite_timestamp(observed["run_deadline_unixtime"])
                or now >= self.lease_deadline
            ):
                raise RelayError("alert_without_fresh_native_observation")
            if any(item["fingerprint"] == fingerprint for item in self.receipts):
                return
            if len(self.receipts) >= MAX_RECEIPTS:
                raise RelayError("receipt_limit")
            receipt: dict[str, object] = {
                "run_id": self.run_id,
                "event_id": event_id,
                "fingerprint": fingerprint,
                "starts_at": starts_at,
                "received_at": datetime.fromtimestamp(now, UTC).isoformat(),
                "status": "firing",
            }
            canonical = json.dumps(
                receipt, sort_keys=True, separators=(",", ":")
            ).encode()
            receipt["receipt_sha256"] = hashlib.sha256(canonical).hexdigest()
            self.receipts.append(receipt)
            self._save("inbox.json", self.inbox_unlocked())

    def inbox_unlocked(self) -> dict[str, object]:
        return {
            "run_id": self.run_id,
            "count": len(self.receipts),
            "receipts": self.receipts,
        }


class RelayServer(HTTPServer):
    def __init__(self, address: tuple[str, int], state: RelayState):
        self.state = state
        super().__init__(address, RelayHandler)

    def get_request(self) -> tuple[socket.socket, tuple[str, int]]:
        connection, address = super().get_request()
        connection.settimeout(5)
        return connection, address


class RelayHandler(BaseHTTPRequestHandler):
    def log_message(self, format: str, *args: object) -> None:
        pass

    def _state(self) -> RelayState:
        if not isinstance(self.server, RelayServer):
            raise RelayError("invalid_server")
        return self.server.state

    def _reply(self, code: int, value: object, metrics: bool = False) -> None:
        body = value if isinstance(value, bytes) else json.dumps(value).encode()
        self.send_response(code)
        self.send_header(
            "Content-Type",
            "text/plain; version=0.0.4" if metrics else "application/json",
        )
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _body(self) -> dict[str, object]:
        if (
            self.headers.get("Transfer-Encoding")
            or len(self.headers.get_all("Content-Length", [])) != 1
        ):
            raise RelayError("invalid_body_framing")
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            raise RelayError("invalid_body_length") from None
        if not 1 <= length <= MAX_BODY:
            raise RelayError("invalid_body_length")
        raw = self.rfile.read(length)
        if len(raw) != length:
            raise RelayError("truncated_body")
        return object_value(json.loads(raw))

    def do_GET(self) -> None:
        if self.path == "/health":
            self._reply(
                200,
                {
                    "run_id": self._state().run_id,
                    "ready": True,
                    "lease_deadline_unixtime": self._state().lease_deadline,
                },
            )
        elif self.path == "/metrics":
            self._reply(200, self._state().metrics(), metrics=True)
        elif self.path == "/inbox":
            self._reply(200, self._state().inbox())
        else:
            self._reply(404, {"error": "route_not_found"})

    def do_PUT(self) -> None:
        if self.path != "/metrics-state":
            self._reply(404, {"error": "route_not_found"})
            return
        try:
            self._state().update(self._body())
        except (RelayError, ValueError, OSError):
            self._reply(400, {"error": "observation_rejected"})
            return
        self._reply(200, {"accepted": True})

    def do_POST(self) -> None:
        if self.path != "/alerts":
            self._reply(404, {"error": "route_not_found"})
            return
        try:
            self._state().receive(self._body())
        except (RelayError, ValueError, OSError):
            self._reply(400, {"error": "alert_rejected"})
            return
        self._reply(200, {"accepted": True})


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--lease-seconds", type=int, default=600)
    args = parser.parse_args()
    state = RelayState(args.run_id, args.state, args.lease_seconds)
    with RelayServer(("0.0.0.0", PORT), state) as server:
        server.serve_forever()


if __name__ == "__main__":
    main()

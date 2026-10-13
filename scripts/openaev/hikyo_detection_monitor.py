"""Independent native-audit polling and read-back of the isolated alert chain."""

from __future__ import annotations

import argparse
import hashlib
import http.client
import json
import math
import os
import re
import socket
import ssl
import stat
import subprocess
import sys
import threading
import time
import uuid
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING
from urllib.parse import urlencode

import hikyo_reporting_audit as audit

if TYPE_CHECKING:
    from hikyo_detection_runtime import LocalMonitoringRuntime
    from openaev_reporting import MonitoringEvidence


class MonitorError(RuntimeError):
    """Literal diagnostics only; external responses and credentials stay private."""


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise MonitorError("invalid_monitor_object")
    return dict(value)


def _string(value: dict[str, object], key: str) -> str:
    result = value.get(key)
    if not isinstance(result, str) or not result:
        raise MonitorError("invalid_monitor_field")
    return result


def _time(value: object) -> datetime:
    if not isinstance(value, str):
        raise MonitorError("invalid_monitor_timestamp")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        raise MonitorError("invalid_monitor_timestamp") from None
    if parsed.tzinfo is None:
        raise MonitorError("invalid_monitor_timestamp")
    return parsed.astimezone(UTC)


def _uuid(value: str) -> str:
    try:
        if str(uuid.UUID(value)) != value:
            raise ValueError()
    except ValueError:
        raise MonitorError("invalid_monitor_identifier") from None
    return value


def _event_id(value: object) -> str:
    if not isinstance(value, str) or not value.startswith("evt_"):
        raise MonitorError("invalid_native_event_id")
    _uuid(value[4:])
    return value


def native_audit_row(
    value: object,
    *,
    actor_id: str,
    target_path: str,
    event_id: str | None = None,
    occurred_at: str | None = None,
    recorded_at: str | None = None,
) -> dict[str, object]:
    """Retain actual matching native fields, excluding arbitrary audit payloads."""
    row = _object(value)
    payload = _object(row.get("payload"))
    parts = target_path.split("/")
    sequence = row.get("seq")
    if (
        len(parts) != 10
        or row.get("type") != "grant.denied"
        or row.get("outcome") != "denied"
        or row.get("actor_id") != actor_id
        or row.get("actor_class") != "machine"
        or (row.get("org_id"), row.get("project_id"), row.get("env_id"))
        != (parts[4], parts[6], parts[8])
        or row.get("occurred_asserted") is not False
        or type(sequence) is not int
        or sequence <= 0
        or payload.get("operation") != "delivery.fetch"
        or payload.get("resolution") != "resolvable"
    ):
        raise MonitorError("native_audit_row_binding_mismatch")
    identifier = _event_id(row.get("id"))
    occurred, recorded = _time(row.get("occurred_at")), _time(row.get("recorded_at"))
    if (
        occurred > recorded
        or (event_id is not None and identifier != event_id)
        or (occurred_at is not None and occurred != _time(occurred_at))
        or (recorded_at is not None and recorded != _time(recorded_at))
    ):
        raise MonitorError("native_audit_row_identity_or_time_mismatch")
    retained = {
        key: row[key]
        for key in (
            "id",
            "seq",
            "type",
            "outcome",
            "actor_class",
            "actor_id",
            "org_id",
            "project_id",
            "env_id",
            "occurred_asserted",
            "occurred_at",
            "recorded_at",
        )
    }
    retained["payload"] = {
        "operation": payload["operation"],
        "resolution": payload["resolution"],
    }
    return retained


def _alertmanager_wire_time(value: object) -> datetime:
    """Pinned Alertmanager DateTime serialization floors to milliseconds.

    v0.34.1 api/v2/models/gettable_alert.go uses strfmt.DateTime;
    go-openapi/strfmt v0.27.0 time.go MarshalFormat is RFC3339Millis.
    Prometheus v3.15.0 rules/manager.go sends FiredAt as StartsAt,
    equal to ActiveAt for this fixed zero-hold rule.
    """
    stamp = _time(value)
    return stamp.replace(microsecond=(stamp.microsecond // 1000) * 1000)


def private(path: Path) -> dict[str, object]:
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(descriptor)
        if (
            not stat.S_ISREG(info.st_mode)
            or info.st_uid != os.getuid()
            or info.st_mode & 0o077
            or info.st_size > 1_048_576
        ):
            raise MonitorError("monitor_file_not_owner_only")
        with os.fdopen(descriptor) as stream:
            descriptor = -1
            decoded: object = json.load(stream)
            return _object(decoded)
    finally:
        if descriptor >= 0:
            os.close(descriptor)


def _save(path: Path, value: dict[str, object]) -> None:
    temporary = path.with_name(path.name + ".new")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        parent_descriptor = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(parent_descriptor)
        finally:
            os.close(parent_descriptor)
    finally:
        temporary.unlink(missing_ok=True)


@dataclass(frozen=True)
class Config:
    run_id: str
    target_path: str
    actor_id: str
    collector_principal_id: str
    test_principal_id: str
    session_token: str = field(repr=False)
    session_expires_at: datetime
    instance_identity: str
    lease_seconds: int

    @classmethod
    def load(cls, path: Path) -> Config:
        value = private(path)
        if value.get("format") != "hikyo-lab-monitor/v1":
            raise MonitorError("invalid_monitor_config")
        if "admin_file" in value or "password" in value or "totp_uri" in value:
            raise MonitorError("administrator_material_not_permitted")
        run_id = _uuid(_string(value, "run_id"))
        target = _string(value, "target_path")
        if (
            re.fullmatch(
                r"/api/v1/orgs/([A-Za-z0-9_-]+)/projects/([A-Za-z0-9_-]+)/environments/([A-Za-z0-9_-]+)/delivery",
                target,
            )
            is None
        ):
            raise MonitorError("invalid_monitor_target")
        principal = _string(value, "collector_principal_id")
        test_principal = _string(value, "test_principal_id")
        actor = _string(value, "actor_id")
        if principal in {test_principal, actor}:
            raise MonitorError("independent_collector_required")
        proof = _object(value.get("scope_proof"))
        if (
            proof.get("principal_id") != principal
            or proof.get("target_path") != target
            or proof.get("capability") != "audit-read"
            or type(proof.get("matching_tenant_grants")) is not int
            or proof.get("matching_tenant_grants") != 1
            or type(proof.get("matching_instance_grants")) is not int
            or proof.get("matching_instance_grants") != 0
            or type(proof.get("invitation_grants_created")) is not int
            or proof.get("invitation_grants_created") != 0
        ):
            raise MonitorError("native_least_privilege_proof_required")
        token = _string(value, "session_token")
        if (
            not 16 <= len(token) <= 4096
            or not token.isascii()
            or any(character.isspace() or ord(character) < 33 for character in token)
        ):
            raise MonitorError("invalid_collector_session")
        lease = value.get("lease_seconds")
        if type(lease) is not int or not 30 <= lease <= 600:
            raise MonitorError("invalid_monitor_lease")
        expires = _time(value.get("session_expires_at"))
        if expires.timestamp() < time.time() + lease + 30:
            raise MonitorError("collector_session_lifetime_insufficient")
        return cls(
            run_id,
            target,
            actor,
            principal,
            test_principal,
            token,
            expires,
            _string(value, "instance_identity"),
            lease,
        )

    @property
    def audit_path(self) -> str:
        return self.target_path.removesuffix("/delivery") + "/audit"


class NativeClient:
    """Fixed TLS origin, GET-only, absolute three-second socket deadline."""

    def __init__(self, config: Config, ca_file: Path, deadline: float | None = None):
        self.config = config
        self.deadline = deadline
        self.context = ssl.create_default_context(cafile=str(ca_file))

    def get(self, path: str) -> dict[str, object]:
        if path not in {"/api/v1/meta", "/api/v1/auth/whoami"} and not path.startswith(
            self.config.audit_path + "?"
        ):
            raise MonitorError("collector_route_not_permitted")
        remaining = (
            3.0 if self.deadline is None else min(3.0, self.deadline - time.time())
        )
        if remaining <= 0:
            raise MonitorError("monitor_lease_expired")
        end = time.monotonic() + remaining
        connection = http.client.HTTPSConnection(
            "127.0.0.1", 18081, context=self.context
        )
        secured: ssl.SSLSocket | None = None
        timer: threading.Timer | None = None
        try:
            raw = socket.create_connection(("127.0.0.1", 18081), timeout=remaining)
            try:
                secured = self.context.wrap_socket(
                    raw, server_hostname="127.0.0.1", do_handshake_on_connect=False
                )
            except BaseException:
                raw.close()
                raise
            connection.sock = secured

            def expire() -> None:
                if secured is not None:
                    try:
                        secured.shutdown(socket.SHUT_RDWR)
                    except OSError:
                        pass

            timer = threading.Timer(max(0, end - time.monotonic()), expire)
            timer.daemon = True
            timer.start()
            secured.settimeout(max(0.001, end - time.monotonic()))
            secured.do_handshake()
            connection.request(
                "GET",
                path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Bearer " + self.config.session_token,
                },
            )
            response = connection.getresponse()
            if response.status != 200:
                raise MonitorError("native_audit_read_failed")
            data = response.read(1_048_577)
            if len(data) > 1_048_576 or time.monotonic() > end:
                raise MonitorError("native_audit_response_bound_exceeded")
            decoded: object = json.loads(data)
            return _object(decoded)
        except MonitorError:
            raise
        except (OSError, ValueError, http.client.HTTPException):
            raise MonitorError("native_audit_read_failed") from None
        finally:
            if timer is not None:
                timer.cancel()
            connection.close()
            if secured is not None:
                secured.close()


class Poller:
    """The collector accepts identity/scope, never a test response or event ID."""

    def __init__(
        self, config: Config, read: Callable[[str], dict[str, object]], deadline: float
    ):
        self.config, self.read = config, read
        if (
            not math.isfinite(deadline)
            or not time.time() < deadline <= time.time() + config.lease_seconds
            or config.session_expires_at.timestamp() < deadline + 30
        ):
            raise MonitorError("invalid_shared_monitor_lease")
        self.deadline = deadline
        self.baseline = ""
        self.watermark = 0
        self.first: dict[str, object] | None = None

    def arm(self) -> None:
        meta = self.read("/api/v1/meta")
        if meta.get("instance_identity") != self.config.instance_identity:
            raise MonitorError("wrong_native_instance")
        identity = self.read("/api/v1/auth/whoami")
        principal, session = (
            _object(identity.get("principal")),
            _object(identity.get("session")),
        )
        if (
            principal.get("id") != self.config.collector_principal_id
            or principal.get("kind") != "human"
            or session.get("artifact") != "cli"
            or _object(identity.get("capabilities")).get("instance_operator")
            is not False
        ):
            raise MonitorError("independent_human_session_required")
        page = self.read(self.config.audit_path + "?limit=1")
        upper = page.get("upper_seq")
        if type(upper) is not int or upper < 0:
            raise MonitorError("invalid_native_watermark")
        self.watermark = upper
        self.baseline = datetime.now(UTC).isoformat()

    def poll(self) -> dict[str, object]:
        if not self.baseline or time.time() >= self.deadline:
            raise MonitorError("monitor_lease_expired")
        after, upper = self.watermark, None
        matches: dict[str, dict[str, object]] = {}
        parts = self.config.target_path.split("/")
        org, project, env = parts[4], parts[6], parts[8]
        for _ in range(5):
            query = {"limit": "1000", "after_seq": str(after)}
            if upper is not None:
                query["to_seq"] = str(upper)
            page = self.read(self.config.audit_path + "?" + urlencode(query))
            rows, ceiling, cursor = (
                page.get("items"),
                page.get("upper_seq"),
                page.get("next_after_seq"),
            )
            exhausted = page.get("exhausted")
            if (
                not isinstance(rows, list)
                or type(ceiling) is not int
                or type(cursor) is not int
                or type(exhausted) is not bool
                or type(page.get("count")) is not int
                or page.get("count") != len(rows)
                or len(rows) > 1000
                or (upper is not None and upper != ceiling)
                or not after <= cursor <= ceiling
                or (not exhausted and cursor <= after)
            ):
                raise MonitorError("invalid_native_poll_pagination")
            previous = after
            for item in rows:
                event = _object(item)
                seq = event.get("seq")
                if type(seq) is not int or not previous < seq <= cursor:
                    raise MonitorError("invalid_native_poll_sequence")
                previous = seq
                if (
                    event.get("type") != "grant.denied"
                    or event.get("outcome") != "denied"
                    or event.get("actor_id") != self.config.actor_id
                    or event.get("actor_class") != "machine"
                    or event.get("org_id") != org
                    or event.get("project_id") != project
                    or event.get("env_id") != env
                    or event.get("occurred_asserted") is not False
                ):
                    continue
                payload = _object(event.get("payload"))
                if (
                    payload.get("operation") != "delivery.fetch"
                    or payload.get("resolution") != "resolvable"
                ):
                    continue
                occurred, recorded = (
                    _time(event.get("occurred_at")),
                    _time(event.get("recorded_at")),
                )
                if (
                    not _time(self.baseline)
                    <= occurred
                    <= recorded
                    <= datetime.now(UTC)
                ):
                    continue
                identifier = _event_id(event.get("id"))
                if identifier in matches:
                    raise MonitorError("duplicate_native_poll_event")
                matches[identifier] = {
                    "event_id": identifier,
                    "occurred_at": occurred.isoformat(),
                    "recorded_at": recorded.isoformat(),
                    "native_audit_row": native_audit_row(
                        event,
                        actor_id=self.config.actor_id,
                        target_path=self.config.target_path,
                    ),
                }
            upper, after = ceiling, cursor
            if exhausted:
                break
        else:
            raise MonitorError("native_poll_page_bound_exceeded")
        if len(matches) > 1:
            raise MonitorError("ambiguous_native_denial")
        if matches:
            candidate = next(iter(matches.values()))
            if self.first is None:
                self.first = {
                    **candidate,
                    "first_polled_at": datetime.now(UTC).isoformat(),
                }
            elif self.first.get("event_id") != candidate["event_id"]:
                raise MonitorError("native_denial_changed")
        elif self.first is not None:
            raise MonitorError("native_denial_disappeared")
        return {
            "run_id": self.config.run_id,
            "native_event_id": self.first.get("event_id") if self.first else None,
            "native_denial": self.first is not None,
            "audit_poll_success": True,
            "audit_last_success_unixtime": time.time(),
            "run_deadline_unixtime": self.deadline,
        }


@dataclass(frozen=True)
class ArmedMonitor:
    run_id: str
    baseline_at: str
    collector_principal_id: str
    scope_only_verified: bool


class DetectionPipeline:
    def __init__(self, config_file: Path, ca_file: Path, state: Path):
        self.config_file, self.ca_file, self.state = config_file, ca_file, state
        self.config = Config.load(config_file)
        self.runtime: LocalMonitoringRuntime | None = None
        self.process: subprocess.Popen[bytes] | None = None
        self.armed: ArmedMonitor | None = None

    def arm(
        self, run_id: str, target_path: str, actor_id: str, guard: Callable[[], None]
    ) -> ArmedMonitor:
        from hikyo_detection_runtime import LocalMonitoringRuntime

        if self.armed is not None or (run_id, target_path, actor_id) != (
            self.config.run_id,
            self.config.target_path,
            self.config.actor_id,
        ):
            raise MonitorError("monitor_arm_binding_mismatch")
        self.state.mkdir(mode=0o700, parents=True, exist_ok=True)
        if (
            self.state.stat().st_mode & 0o077
            or (self.state / "collector-state.json").exists()
        ):
            raise MonitorError("fresh_private_monitor_state_required")
        guard()
        self.runtime = LocalMonitoringRuntime(
            run_id, self.state / "stack", 18790, 18790
        )
        try:
            self.runtime.start()
            self.process = subprocess.Popen(
                [
                    sys.executable,
                    str(Path(__file__).resolve()),
                    "collector",
                    "--config",
                    str(self.config_file),
                    "--ca",
                    str(self.ca_file),
                    "--state",
                    str(self.state),
                    "--relay-container",
                    self.runtime.relay_container,
                    "--lease-deadline-unixtime",
                    str(self.runtime.lease_deadline_unixtime),
                ],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            until = time.monotonic() + 30
            while time.monotonic() < until:
                guard()
                if self.process.poll() is not None:
                    raise MonitorError("collector_start_failed")
                path = self.state / "collector-state.json"
                if path.exists():
                    proof = private(path)
                    if (
                        proof.get("run_id") == self.config.run_id
                        and proof.get("ready") is True
                        and proof.get("poll_success") is True
                    ):
                        self.armed = ArmedMonitor(
                            self.config.run_id,
                            _string(proof, "baseline_at"),
                            self.config.collector_principal_id,
                            True,
                        )
                        return self.armed
                time.sleep(0.2)
            raise MonitorError("collector_start_deadline")
        except BaseException:
            self.close()
            raise

    def _proof(self) -> dict[str, object]:
        if (
            self.armed is None
            or self.process is None
            or self.process.poll() is not None
        ):
            raise MonitorError("collector_not_running")
        proof = private(self.state / "collector-state.json")
        deadline = float(_string(proof, "deadline"))
        if (
            proof.get("run_id") != self.config.run_id
            or proof.get("poll_success") is not True
            or not -5
            <= time.time() - _time(proof.get("last_polled_at")).timestamp()
            <= 10
            or not math.isfinite(deadline)
            or time.time() >= deadline
        ):
            raise MonitorError("collector_not_fresh")
        return proof

    def verify_quiet(
        self, guard: Callable[[], None], timeout_s: float = 30
    ) -> dict[str, object]:
        if self.runtime is None or not 0 < timeout_s <= 30:
            raise MonitorError("monitor_not_armed")
        scrapes: set[str] = set()
        begin = datetime.now(UTC)
        until = time.monotonic() + timeout_s
        while time.monotonic() < until:
            guard()
            if self._proof().get("first_event") is not None:
                raise MonitorError("unexpected_native_denial_before_exercise")
            alerts = _prom_alerts(
                self.runtime.prometheus_get("/api/v1/alerts"), self.config.run_id
            )
            inbox = _object(self.runtime.api("/inbox"))
            managed = self.runtime.alertmanager_get("/api/v2/alerts")
            if not isinstance(managed, list):
                raise MonitorError("invalid_alertmanager_readback")
            if any(
                _matching_labels(_object(row), self.config.run_id) for row in managed
            ):
                raise MonitorError("unexpected_alert_before_exercise")
            if inbox.get("run_id") != self.config.run_id or inbox.get("count") != 0:
                raise MonitorError("invalid_inbox_readback")
            if alerts or inbox.get("receipts") != []:
                raise MonitorError("unexpected_alert_before_exercise")
            target_response = _object(self.runtime.prometheus_get("/api/v1/targets"))
            if target_response.get("status") != "success":
                raise MonitorError("prometheus_read_failed")
            targets = _object(target_response.get("data")).get("activeTargets")
            if isinstance(targets, list) and len(targets) == 1:
                target = _object(targets[0])
                stamp = target.get("lastScrape")
                if (
                    target.get("health") == "up"
                    and isinstance(stamp, str)
                    and _time(stamp) >= begin
                ):
                    scrapes.add(stamp)
            if len(scrapes) >= 2:
                guard()
                self._proof()
                if time.monotonic() >= until:
                    raise MonitorError("quiet_scrape_proof_deadline")
                return {
                    "query_succeeded": True,
                    "rule_fired": False,
                    "inbox_receipts": 0,
                    "completed_scrapes": len(scrapes),
                }
            time.sleep(0.2)
        raise MonitorError("quiet_scrape_proof_deadline")

    def verify_monitoring(
        self,
        inject_id: str,
        case_name: str,
        window_start: str,
        window_end: str,
        target_path: str,
        actor_id: str,
        guard: Callable[[], None],
        timeout_s: float = 60,
    ) -> MonitoringEvidence:
        import openaev_reporting as reporting

        if self.runtime is None or self.armed is None or not 0 < timeout_s <= 60:
            raise MonitorError("monitor_not_armed")
        if (target_path, actor_id) != (self.config.target_path, self.config.actor_id):
            raise MonitorError("monitor_verification_binding_mismatch")
        client = NativeClient(self.config, self.ca_file)
        org = self.config.target_path.split("/")[4]

        def native(method: str, path: str) -> dict[str, object]:
            prefix = f"/api/v1/orgs/{org}/audit?"
            if method != "GET" or not path.startswith(prefix):
                raise MonitorError("native_verification_route_rejected")
            return client.get(self.config.audit_path + "?" + path.removeprefix(prefix))

        until = time.monotonic() + timeout_s
        while time.monotonic() < until:
            guard()
            proof = self._proof()
            first = proof.get("first_event")
            if first is None:
                time.sleep(0.2)
                continue
            event = _object(first)
            event_id = _event_id(event.get("event_id"))
            retained_row = native_audit_row(
                event.get("native_audit_row"),
                actor_id=actor_id,
                target_path=target_path,
                event_id=event_id,
                occurred_at=_string(event, "occurred_at"),
                recorded_at=_string(event, "recorded_at"),
            )
            if not (
                _time(window_start)
                <= _time(event.get("occurred_at"))
                <= _time(window_end)
                and _time(window_start)
                <= _time(event.get("recorded_at"))
                <= _time(window_end)
            ):
                raise MonitorError("native_event_outside_execution_window")
            alerts = _prom_alerts(
                self.runtime.prometheus_get("/api/v1/alerts"), self.config.run_id
            )
            active = [
                a
                for a in alerts
                if a.get("state") == "firing"
                and _object(a.get("labels")).get("event_id") == event_id
            ]
            received = self.runtime.alertmanager_get("/api/v2/alerts")
            observed_at = datetime.now(UTC).isoformat()
            inbox = _object(self.runtime.api("/inbox"))
            receipts = inbox.get("receipts")
            if (
                not isinstance(received, list)
                or not isinstance(receipts, list)
                or inbox.get("run_id") != self.config.run_id
                or type(inbox.get("count")) is not int
                or inbox.get("count") != len(receipts)
            ):
                raise MonitorError("invalid_monitoring_readback")
            managed = [
                r
                for raw in received
                if (r := _object(raw))
                and _matching_labels(r, self.config.run_id, event_id)
                and _object(r.get("status")).get("state") == "active"
                and _object(r.get("status")).get("silencedBy") == []
                and _object(r.get("status")).get("inhibitedBy") == []
            ]
            delivered = [
                r
                for raw in receipts
                if (r := _object(raw))
                and r.get("run_id") == self.config.run_id
                and r.get("event_id") == event_id
                and r.get("status") == "firing"
            ]
            if len(active) > 1 or len(managed) > 1 or len(delivered) > 1:
                raise MonitorError("ambiguous_monitoring_readback")
            if len(active) == len(managed) == len(delivered) == 1:
                receipt = delivered[0]
                fingerprint = _string(managed[0], "fingerprint")
                if (
                    receipt.get("fingerprint") != fingerprint
                    or re.fullmatch(r"[a-f0-9]{16}", fingerprint) is None
                    or _time(receipt.get("starts_at"))
                    != _time(managed[0].get("startsAt"))
                    or _time(managed[0].get("startsAt"))
                    != _alertmanager_wire_time(active[0].get("activeAt"))
                ):
                    raise MonitorError("notification_fingerprint_mismatch")
                assessed = audit.assess_native_audit(
                    native,
                    inject_id=inject_id,
                    case_name="wrong-tenant",
                    target_path=target_path,
                    actor_id=actor_id,
                    window_start=window_start,
                    window_end=window_end,
                    max_pages=2,
                )
                if assessed.event_id != event_id:
                    raise MonitorError("native_rule_event_mismatch")
                canonical = json.dumps(
                    {k: v for k, v in receipt.items() if k != "receipt_sha256"},
                    sort_keys=True,
                    separators=(",", ":"),
                ).encode()
                receipt_hash = hashlib.sha256(canonical).hexdigest()
                if receipt.get("receipt_sha256") != receipt_hash:
                    raise MonitorError("notification_receipt_hash_mismatch")
                evidence = reporting.MonitoringEvidence(
                    native_audit=reporting.AuditEvidence(
                        inject_id,
                        case_name,
                        window_start,
                        window_end,
                        True,
                        True,
                        event_id,
                    ),
                    run_id=self.config.run_id,
                    collector_principal_id=self.config.collector_principal_id,
                    collector_scope_only_verified=True,
                    baseline_at=self.armed.baseline_at,
                    native_occurred_at=_string(event, "occurred_at"),
                    native_recorded_at=_string(event, "recorded_at"),
                    collector_polled_at=_string(event, "first_polled_at"),
                    prometheus_firing_at=_string(active[0], "activeAt"),
                    alertmanager_observed_at=observed_at,
                    inbox_received_at=_string(receipt, "received_at"),
                    alertmanager_fingerprint=fingerprint,
                    inbox_receipt_sha256=receipt_hash,
                    scope_actor_window_verified=True,
                    rule_fired=True,
                    notification_delivered=True,
                )
                evidence.validate(inject_id, case_name)
                guard()
                self._proof()
                if time.monotonic() >= until:
                    raise MonitorError("monitoring_notification_deadline")
                _save(
                    self.state / "monitoring-native-proof.json",
                    {
                        "run_id": self.config.run_id,
                        "native_event_id": event_id,
                        "native_audit_row": retained_row,
                        "prometheus_alert": active[0],
                        "alertmanager_alert": managed[0],
                        "alertmanager_observed_at": observed_at,
                        "inbox": inbox,
                    },
                )
                return evidence
            time.sleep(0.2)
        raise MonitorError("monitoring_notification_deadline")

    def close(self) -> None:
        try:
            if self.process is not None and self.process.poll() is None:
                self.process.terminate()
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=5)
        finally:
            if self.runtime is not None:
                self.runtime.stop()


def _matching_labels(
    value: dict[str, object], run_id: str, event_id: str | None = None
) -> bool:
    labels = _object(value.get("labels"))
    return (
        labels.get("alertname") == "HikyoLabCrossTenantDenied"
        and labels.get("run_id") == run_id
        and (event_id is None or labels.get("event_id") == event_id)
    )


def _prom_alerts(value: object, run_id: str) -> list[dict[str, object]]:
    response = _object(value)
    if response.get("status") != "success":
        raise MonitorError("prometheus_read_failed")
    rows = _object(response.get("data")).get("alerts")
    if not isinstance(rows, list):
        raise MonitorError("invalid_prometheus_alerts")
    return [r for item in rows if (r := _object(item)) and _matching_labels(r, run_id)]


def create_pipeline(config_file: Path, ca_file: Path, state: Path) -> DetectionPipeline:
    return DetectionPipeline(config_file, ca_file, state)


def stop_observation(poller: Poller) -> dict[str, object]:
    return {
        "run_id": poller.config.run_id,
        "native_event_id": poller.first.get("event_id") if poller.first else None,
        "native_denial": False,
        "audit_poll_success": False,
        "audit_last_success_unixtime": 0.0,
        "run_deadline_unixtime": poller.deadline,
    }


def collector(
    config_file: Path, ca_file: Path, state: Path, relay_container: str, deadline: float
) -> None:
    from hikyo_detection_runtime import update_relay_observation

    config = Config.load(config_file)
    poller = Poller(config, NativeClient(config, ca_file, deadline).get, deadline)
    proof: dict[str, object] = {
        "run_id": config.run_id,
        "ready": False,
        "poll_success": False,
    }
    try:
        poller.arm()
        while time.time() < poller.deadline:
            observation = poller.poll()
            proof = {
                "run_id": config.run_id,
                "ready": True,
                "poll_success": True,
                "baseline_at": poller.baseline,
                "last_polled_at": datetime.now(UTC).isoformat(),
                "deadline": str(poller.deadline),
                "first_event": poller.first,
            }
            _save(state / "collector-state.json", proof)
            if poller.first is not None:
                _save(
                    state / "native-audit-readback.json",
                    {
                        "run_id": config.run_id,
                        "native_audit_row": native_audit_row(
                            poller.first.get("native_audit_row"),
                            actor_id=config.actor_id,
                            target_path=config.target_path,
                            event_id=_string(poller.first, "event_id"),
                            occurred_at=_string(poller.first, "occurred_at"),
                            recorded_at=_string(poller.first, "recorded_at"),
                        ),
                    },
                )
            update_relay_observation(relay_container, observation)
            time.sleep(min(2, max(0, poller.deadline - time.time())))
        raise MonitorError("monitor_lease_expired")
    except (MonitorError, RuntimeError, OSError, ValueError):
        _save(
            state / "collector-state.json",
            {
                **proof,
                "ready": False,
                "poll_success": False,
                "failure": "collector_stopped",
            },
        )
        try:
            update_relay_observation(relay_container, stop_observation(poller))
        except (RuntimeError, OSError, ValueError):
            _save(
                state / "collector-state.json",
                {
                    **proof,
                    "ready": False,
                    "poll_success": False,
                    "failure": "collector_stopped",
                    "stop_delivery_failed": True,
                },
            )
        raise MonitorError("collector_stopped") from None


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["collector"])
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--ca", required=True, type=Path)
    parser.add_argument("--state", required=True, type=Path)
    parser.add_argument("--relay-container", required=True)
    parser.add_argument("--lease-deadline-unixtime", required=True, type=float)
    args = parser.parse_args()
    try:
        collector(
            args.config,
            args.ca,
            args.state,
            args.relay_container,
            args.lease_deadline_unixtime,
        )
    except (RuntimeError, OSError, ValueError):
        raise SystemExit("collector_stopped") from None


if __name__ == "__main__":
    main()

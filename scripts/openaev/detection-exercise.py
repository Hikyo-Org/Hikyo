#!/usr/bin/env python3
"""One fresh OpenAEV wrong-tenant exercise with independently delivered detection.

Prepare a new fixture with security-suite.py first. A direct 404 control precedes
the monitor baseline, then exactly one new OpenAEV request is recorded. OpenAEV
records the domain response; the direct control separately proves HTTP status.
No negative request is replayed after monitor arming. Only the independent native
collector, Prometheus, Alertmanager and lab inbox can establish detection.
"""

from __future__ import annotations

import argparse
import importlib.util
import os
import re
import sys
import threading
import time
import uuid
from collections.abc import Callable
from dataclasses import asdict, dataclass
from datetime import UTC, datetime
from pathlib import Path
from types import ModuleType
from typing import Protocol, runtime_checkable
from urllib.parse import urlsplit

import hikyo_adversarial_transport as transport
import hikyo_lab_guard as health
import openaev_reporting as reporting
import openaev_security as recording

CASE_NAME = "wrong-tenant"
MONITOR_TIMEOUT_SECONDS = 60.0


def _suite() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "hikyo_detection_security_suite", Path(__file__).with_name("security-suite.py")
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("security_suite_unavailable")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


suite = _suite()


class ExerciseError(RuntimeError):
    """Literal reason codes only, without credentials or raw responses."""


class HealthWatch:
    """Keep the unchanged guard active while OpenAEV awaits dispatch/callback."""

    def __init__(self, guard: Callable[[], None], interval_s: float = 5.0):
        if type(interval_s) not in (float, int) or not 0.01 <= interval_s <= 5.0:
            raise ExerciseError("invalid_background_guard_interval")
        self._stop = threading.Event()
        self._error: BaseException | None = None

        def check() -> None:
            while not self._stop.wait(interval_s):
                try:
                    guard()
                except (
                    RuntimeError,
                    OSError,
                    ValueError,
                    TypeError,
                    KeyError,
                    AssertionError,
                    SystemExit,
                    KeyboardInterrupt,
                ) as error:
                    self._error = error
                    self._stop.set()

        self.thread = threading.Thread(
            target=check, name="hikyo-detection-health", daemon=True
        )

    def start(self) -> None:
        self.thread.start()

    def verify(self) -> None:
        if self._error is not None:
            raise ExerciseError("background_health_guard_failed") from None

    def close(self) -> None:
        self._stop.set()
        # collect has two sequential waves of independent kubectl reads, each
        # retaining its existing five-second subprocess timeout.
        self.thread.join(timeout=11.0)
        if self.thread.is_alive():
            raise ExerciseError("background_health_guard_undrained")
        self.verify()


def obj(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ExerciseError("invalid_exercise_object")
    return dict(value)


def text(value: dict[str, object], field: str) -> str:
    result = value.get(field)
    if not isinstance(result, str) or not result:
        raise ExerciseError("invalid_exercise_field")
    return result


def timestamp(value: str) -> datetime:
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            raise ValueError()
        return parsed
    except ValueError:
        raise ExerciseError("invalid_exercise_timestamp") from None


@dataclass(frozen=True)
class ArmedMonitor:
    run_id: str
    baseline_at: str
    collector_principal_id: str
    scope_only_verified: bool


@runtime_checkable
class DetectionPipeline(Protocol):
    """Independent component; receives scope/actor but never a runner event ID.

    arm captures a native audit cursor AFTER the direct control. verify_monitoring
    rereads native audit, live rule/Alertmanager APIs and the actual inbox receipt.
    It must call guard during the bounded wait and honor timeout_s. Runner request
    results, static exported booleans and old event IDs cannot establish proof.
    """

    def arm(
        self, *, run_id: str, target_path: str, actor_id: str, guard: Callable[[], None]
    ) -> ArmedMonitor: ...

    def verify_monitoring(
        self,
        *,
        inject_id: str,
        case_name: str,
        window_start: str,
        window_end: str,
        target_path: str,
        actor_id: str,
        guard: Callable[[], None],
        timeout_s: float,
    ) -> reporting.MonitoringEvidence: ...

    def verify_quiet(
        self, *, guard: Callable[[], None], timeout_s: float
    ) -> dict[str, object]: ...

    def close(self) -> None: ...


PipelineFactory = Callable[[Path, Path, Path], DetectionPipeline]


def create_pipeline(config_file: Path, ca_file: Path, state: Path) -> DetectionPipeline:
    """Fixed local adapter, with no arbitrary module/code name in configuration."""
    try:
        hikyo_detection_monitor = importlib.import_module("hikyo_detection_monitor")
    except ImportError:
        raise ExerciseError("independent_monitor_adapter_unavailable") from None
    candidate = hikyo_detection_monitor.create_pipeline(config_file, ca_file, state)
    if not isinstance(candidate, DetectionPipeline):
        raise ExerciseError("invalid_independent_monitor_adapter")
    return candidate


class Runner:
    def __init__(
        self,
        args: argparse.Namespace,
        pipeline_factory: PipelineFactory = create_pipeline,
    ):
        self.state: Path = args.state
        suite.directory(self.state)
        if any(
            (self.state / name).exists()
            for name in (
                "results.json",
                "adversarial-results.json",
                "detection-results.json",
            )
        ):
            raise ExerciseError("fresh_prepared_fixture_required")
        self.fixture = obj(suite.private(self.state / "fixture.json"))
        if self.fixture.get("format") != "hikyo-openaev-security/v1":
            raise ExerciseError("invalid_prepared_fixture")
        self.ca = Path(text(self.fixture, "ca_file"))
        suite.identity(
            self.ca,
            text(self.fixture, "instance_identity"),
            suite.fixture_release(self.fixture),
        )
        self.env = text(self.fixture, "env")
        self.reader = obj(obj(self.fixture.get("accounts")).get("reader"))
        self.token = text(self.reader, "token")
        self.actor_id = text(self.reader, "principal")
        outside = self.fixture.get("outside_envs")
        if (
            not isinstance(outside, list)
            or len(outside) != 3
            or not isinstance(outside[2], str)
        ):
            raise ExerciseError("wrong_tenant_fixture_required")
        self.target = outside[2] + "/delivery"
        self.authorized = self.env + "/delivery"
        # Both transports share this fixed internal lab origin/path parser.
        recording._target(self.target)
        recording._target(self.authorized)
        if (
            urlsplit(self.target).path.split("/")[4]
            == urlsplit(self.authorized).path.split("/")[4]
        ):
            raise ExerciseError("target_must_be_different_synthetic_tenant")
        self.guard = health.LabGuard(health.collect())
        self.health_lock = threading.Lock()
        self.watchdog: HealthWatch | None = None
        self.guard()
        # Check native metadata; no fixture timestamp or local token decoding
        # can prove a workload credential is still live with sufficient margin.
        admin = suite.Admin(Path(text(self.fixture, "admin_file")), self.ca)
        credential_id = text(self.reader, "revoke_path").rsplit("/", 1)[1]
        listing = obj(admin.call("GET", text(self.reader, "credential_path")))
        rows = listing.get("items")
        if not isinstance(rows, list):
            raise ExerciseError("native_credential_metadata_invalid")
        matches = [obj(row) for row in rows if obj(row).get("id") == credential_id]
        if len(matches) != 1 or matches[0].get("revoked_at") is not None:
            raise ExerciseError("fresh_workload_credential_required")
        expiry = timestamp(text(matches[0], "expires_at"))
        if (expiry - datetime.now(UTC)).total_seconds() < 1800:
            raise ExerciseError("workload_credential_requires_30_minutes_remaining")
        self.credential_deadline = expiry.timestamp() - 120
        self.client = transport.WorkloadTransport(self.ca)
        collector_config = obj(suite.private(args.collector_config_file))
        if (
            args.collector_config_file.resolve()
            == Path(text(self.fixture, "admin_file")).resolve()
        ):
            raise ExerciseError("independent_collector_credentials_required")
        if any(
            field in collector_config
            for field in ("password", "totp_uri", "admin_file")
        ):
            raise ExerciseError("test_admin_credentials_rejected")
        if text(collector_config, "collector_principal_id") in {
            text(collector_config, "test_principal_id"),
            self.actor_id,
        }:
            raise ExerciseError("independent_collector_principal_required")
        self.run_id = text(collector_config, "run_id")
        try:
            if str(uuid.UUID(self.run_id)) != self.run_id:
                raise ValueError()
        except ValueError:
            raise ExerciseError("invalid_exercise_run_identifier") from None
        self.path = self.state / "detection-results.json"
        self.report: dict[str, object] = {
            "format": "hikyo-openaev-detection-exercise/v1",
            "run_id": self.run_id,
            "instance_identity": text(self.fixture, "instance_identity"),
            "release": suite.fixture_release(self.fixture),
            "started_at": datetime.now(UTC).isoformat(),
            "passed": False,
            "case_name": CASE_NAME,
            "scope": "one new disposable synthetic wrong-tenant exercise",
            "openaev_negative_requests": 0,
            "direct_negative_controls_before_baseline": 0,
            "negative_replays_after_baseline": 0,
            "openaev_http_status_recorded": False,
            "health_baseline": self.guard.baseline,
            "credential_run_deadline": datetime.fromtimestamp(
                self.credential_deadline, UTC
            ).isoformat(),
            "background_guard_interval_seconds": 5.0,
            "native_cluster_read_timeout_seconds": 5,
        }
        suite.save(self.path, self.report)
        # The adapter gets a separate collector config, never the fixture's
        # admin_file, password or TOTP material. Its least-privilege proof is
        # required at arm and again in MonitoringEvidence before publication.
        self.reporter = reporting.OpenAEVReporter(
            args.platform_url, args.platform_token_file, self.ca
        )
        asset = self.reporter.ensure_asset()
        self.recorder = recording.OpenAEVRecorder(
            args.platform_url,
            args.platform_token_file,
            self.ca,
            reporting_config=lambda name, payload: reporting.configure_payload(
                payload, name, asset, detection_assessed=True, monitoring_assessed=True
            ),
        )
        # Factory is last; later constructors cannot orphan a started runtime.
        # The adapter cleans up a partial start if its own factory raises.
        self.pipeline = pipeline_factory(
            args.collector_config_file, self.ca, self.state
        )

    def persist(self) -> None:
        self.report["health"] = {
            "checks": self.guard.checks,
            "peak_observed_memory_bytes": self.guard.peak_memory,
            "last_sample": self.guard.last,
        }
        suite.save(self.path, self.report, replace=True)

    def check_health(self) -> None:
        if self.watchdog is not None:
            self.watchdog.verify()
        if time.time() >= self.credential_deadline:
            raise ExerciseError("workload_credential_safety_deadline_reached")
        with self.health_lock:
            self.guard()
        if self.watchdog is not None:
            self.watchdog.verify()

    def quiet_control(self) -> dict[str, object]:
        self.check_health()
        result = self.pipeline.verify_quiet(guard=self.check_health, timeout_s=30.0)
        scrapes = result.get("completed_scrapes")
        if (
            result.get("query_succeeded") is not True
            or result.get("rule_fired") is not False
            or type(result.get("inbox_receipts")) is not int
            or result.get("inbox_receipts") != 0
            or type(scrapes) is not int
            or scrapes < 2
        ):
            raise ExerciseError("false_positive_monitor_control_failed")
        self.check_health()
        return result

    def run(self) -> None:
        self.watchdog = HealthWatch(self.check_health)
        self.watchdog.start()
        try:
            self.check_health()
            negative = self.client.request("GET", self.target, self.token)
            if negative.status != 404:
                raise ExerciseError("negative_control_status_mismatch")
            transport.assert_refusal(self.fixture, "not_found", negative.body)
            self.report["direct_negative_controls_before_baseline"] = 1
            self.report["prebaseline_direct_status"] = negative.status
            self.report["prebaseline_direct_body_asserted"] = True
            self.check_health()
            positive = self.client.request("GET", self.authorized, self.token)
            if positive.status != 200:
                raise ExerciseError("positive_control_status_mismatch")
            suite.assert_body(self.fixture, "presence", positive.body)
            self.report["positive_authorized_control_status"] = 200
            controls_completed = datetime.now(UTC)
            self.persist()
            armed = self.pipeline.arm(
                run_id=self.run_id,
                target_path=self.target,
                actor_id=self.actor_id,
                guard=self.check_health,
            )
            if (
                armed.run_id != self.run_id
                or armed.scope_only_verified is not True
                or not armed.collector_principal_id
                or timestamp(armed.baseline_at) < controls_completed
            ):
                raise ExerciseError("independent_collector_baseline_invalid")
            self.report["monitor_baseline"] = asdict(armed)
            self.report["quiet_before_authorized_control"] = self.quiet_control()
            armed_positive = self.client.request("GET", self.authorized, self.token)
            if armed_positive.status != 200:
                raise ExerciseError("armed_positive_control_status_mismatch")
            suite.assert_body(self.fixture, "presence", armed_positive.body)
            self.report["positive_authorized_control_after_baseline_status"] = 200
            self.report["quiet_after_authorized_control"] = self.quiet_control()
            self.persist()
            self.report["attempted_openaev_launches"] = 1
            self.check_health()
            recorded = self.recorder.record_case(
                CASE_NAME,
                "GET",
                self.target,
                self.token,
                None,
                lambda body: transport.assert_refusal(
                    self.fixture, "not_found", obj(body)
                ),
            )
            self.check_health()
            if (
                recorded.get("domain_assertion_passed") is not True
                or recorded.get("execution_recorded") is not True
            ):
                raise ExerciseError("recorded_domain_assertion_required")
            inject_id = text(recorded, "inject_id")
            self.report["inject_id"] = inject_id
            self.report["openaev_negative_requests"] = 1
            self.report["recorded_domain_assertion_passed"] = True
            start, end = self.reporter.execution_window(inject_id)
            self.persist()
            verification_started = time.monotonic()
            proof = self.pipeline.verify_monitoring(
                inject_id=inject_id,
                case_name=CASE_NAME,
                window_start=start,
                window_end=end,
                target_path=self.target,
                actor_id=self.actor_id,
                guard=self.check_health,
                timeout_s=MONITOR_TIMEOUT_SECONDS,
            )
            if time.monotonic() - verification_started > MONITOR_TIMEOUT_SECONDS:
                raise ExerciseError("independent_monitor_timeout_exceeded")
            proof.validate(inject_id, CASE_NAME)
            if (
                proof.run_id != armed.run_id
                or proof.baseline_at != armed.baseline_at
                or proof.collector_principal_id != armed.collector_principal_id
                or proof.native_audit.window_start != start
                or proof.native_audit.window_end != end
            ):
                raise ExerciseError("independent_monitor_binding_mismatch")
            self.check_health()
            self.report["independent_detection"] = asdict(proof)
            self.persist()
        finally:
            try:
                self.watchdog.close()
            finally:
                self.pipeline.close()
        # The native proof was captured while the monitor was running. No
        # success score is written until owned-runtime cleanup and the final
        # cluster baseline check have both succeeded.
        self.check_health()
        self.report["monitor_runtime_closed"] = True
        self.report["final_health_verified_at"] = datetime.now(UTC).isoformat()
        self.report["publication_attempted"] = True
        self.persist()
        self.report["home_reporting"] = self.reporter.publish_verdict(
            inject_id,
            CASE_NAME,
            reporting.VerifiedOutcome(
                inject_id, CASE_NAME, 404, negative.status, True, True
            ),
            monitoring_evidence=proof,
        )
        # Publication uses several native PUTs. If it fails part-way through,
        # retain the failed local report without rewriting genuine history.
        self.report["completed_at"] = datetime.now(UTC).isoformat()
        self.report["passed"] = True
        self.persist()


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--collector-config-file", type=Path, required=True)
    parser.add_argument("--platform-url", default="https://127.0.0.1:18080")
    parser.add_argument(
        "--platform-token-file",
        type=Path,
        default=Path.home() / ".local/state/openaev-lab/openaev-token",
    )
    args = parser.parse_args()
    runner: Runner | None = None
    try:
        runner = Runner(args)
        runner.run()
    except (
        RuntimeError,
        OSError,
        ValueError,
        TypeError,
        KeyError,
        recording.OpenAEVRecordingError,
        reporting.ReportingError,
    ) as error:
        if runner is not None:
            runner.report["passed"] = False
            runner.report["failure"] = type(error).__name__
            # The fixed monitor adapter raises audited literal reason codes.
            # Look up the already loaded class so adapter absence still follows
            # create_pipeline's existing unavailable-adapter error handling.
            monitor_error = getattr(
                sys.modules.get("hikyo_detection_monitor"), "MonitorError", None
            )
            trusted_reason = isinstance(error, ExerciseError) or (
                isinstance(monitor_error, type) and isinstance(error, monitor_error)
            )
            if trusted_reason and re.fullmatch(r"[a-z][a-z0-9_]{0,95}", str(error)):
                runner.report["failure_code"] = str(error)
            if (
                isinstance(error, recording.OpenAEVRecordingError)
                and error.inject_id is not None
            ):
                runner.report["inject_id"] = error.inject_id
            runner.persist()
        print(
            "FAIL " + type(error).__name__ + "; review private evidence",
            file=sys.stderr,
        )
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()

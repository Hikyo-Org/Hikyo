#!/usr/bin/env python3
"""Fresh adversarial records plus separately measured, bounded Hikyo load.

Prepare with security-suite.py first. Only synthetic workload material enters
OpenAEV. Human administration stays in the native owner-only fixture. There is
no resume/replay path: failed partial runs retain their evidence.
Use --scope lifecycle with a newly prepared fixture to run only the two
revocation phases and their atomic records, without replaying the full prefix.
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import os
import sys
import time
from dataclasses import asdict
from datetime import UTC, datetime
from pathlib import Path
from types import ModuleType
from urllib.parse import urlencode

import hikyo_adversarial_cases as adversarial
import hikyo_adversarial_transport as transport
import hikyo_bounded_load as load
import hikyo_lab_guard as health
import hikyo_reporting_audit as audit
import openaev_reporting as reporting
import openaev_security as recording


def _suite() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "hikyo_security_suite", Path(__file__).with_name("security-suite.py")
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("suite_module_unavailable")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


suite = _suite()


class AdversarialRunError(RuntimeError):
    """Sanitized code only; no paths, tokens or raw response bodies."""


def object_value(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise AdversarialRunError("invalid_state_object")
    return dict(value)


def string_value(value: dict[str, object], field: str) -> str:
    result = value.get(field)
    if not isinstance(result, str) or not result:
        raise AdversarialRunError("invalid_state_field")
    return result


class Runner:
    def __init__(self, args: argparse.Namespace):
        self.args = args
        self.run_scope: str = args.scope
        if self.run_scope not in {"full", "lifecycle"}:
            raise AdversarialRunError("invalid_run_scope")
        self.state: Path = args.state
        suite.directory(self.state)
        self.path = self.state / "adversarial-results.json"
        if self.path.exists() or (self.state / "results.json").exists():
            raise AdversarialRunError("fresh_run_required")
        self.fixture = object_value(suite.private(self.state / "fixture.json"))
        self.ca = Path(string_value(self.fixture, "ca_file"))
        suite.identity(
            self.ca,
            string_value(self.fixture, "instance_identity"),
            suite.fixture_release(self.fixture),
        )
        self.client = transport.WorkloadTransport(self.ca)
        self.accounts = object_value(self.fixture["accounts"])
        self.reader = object_value(self.accounts["reader"])
        self.revealer = object_value(self.accounts["revealer"])
        self.tokens = {
            "reader": string_value(self.reader, "token"),
            "revealer": string_value(self.revealer, "token"),
            "tampered": str(suite.tamper(string_value(self.reader, "token"))),
            "long-garbage": "hik_1_wl_" + "A" * 4096,
        }
        self.env = string_value(self.fixture, "env")
        self.guard = health.LabGuard(health.collect())
        self.guard()
        self.report: dict[str, object] = {
            "format": "hikyo-openaev-adversarial/v1",
            "passed": False,
            "started_at": datetime.now(UTC).isoformat(),
            "instance_identity": string_value(self.fixture, "instance_identity"),
            "source_revision": adversarial.SOURCE_REVISION,
            "release": suite.fixture_release(self.fixture),
            "scope": "disposable synthetic lab; no production attack requests",
            "run_scope": self.run_scope,
            "planned_load_requests": 80 if self.run_scope == "lifecycle" else 260,
            "planned_atomic_cases": 2 if self.run_scope == "lifecycle" else 22,
            "health_baseline": self.guard.baseline,
        }
        self.cases: list[dict[str, object]] = []
        self.phases: list[dict[str, object]] = []
        self.report["cases"], self.report["load_phases"] = self.cases, self.phases
        suite.save(self.path, self.report)
        self.admin = suite.Admin(
            Path(string_value(self.fixture, "admin_file")), self.ca
        )
        expiries: list[datetime] = []
        for account in (self.reader, self.revealer):
            listing = object_value(
                self.admin.call("GET", string_value(account, "credential_path"))
            )
            rows = listing.get("items")
            if not isinstance(rows, list):
                raise AdversarialRunError("credential_metadata_invalid")
            credential_id = string_value(account, "revoke_path").rsplit("/", 1)[1]
            matches = [
                object_value(row)
                for row in rows
                if object_value(row).get("id") == credential_id
            ]
            if len(matches) != 1 or matches[0].get("revoked_at") is not None:
                raise AdversarialRunError("fresh_credential_required")
            expiry = datetime.fromisoformat(
                string_value(matches[0], "expires_at").replace("Z", "+00:00")
            )
            if (
                expiry.tzinfo is None
                or (expiry - datetime.now(UTC)).total_seconds() < 1800
            ):
                raise AdversarialRunError("credential_requires_30_minutes_remaining")
            expiries.append(expiry)
        self.credential_deadline = min(expiries).timestamp() - 120
        self.report["credential_validity_preflight"] = {
            "minimum_remaining_seconds": round(
                (min(expiries) - datetime.now(UTC)).total_seconds()
            ),
            "run_deadline": datetime.fromtimestamp(
                self.credential_deadline, UTC
            ).isoformat(),
        }
        principal = string_value(
            object_value(self.admin.call("GET", "/api/v1/auth/whoami")["principal"]),
            "id",
        )
        self.admin.grant(
            string_value(self.fixture, "org_path"), principal, "audit-read"
        )
        self.admin.login()
        self.admin.call(
            "GET", string_value(self.fixture, "org_path") + "/audit?limit=1"
        )
        self.reporter = reporting.OpenAEVReporter(
            args.platform_url, args.platform_token_file, self.ca
        )
        self.asset = self.reporter.ensure_asset()
        self.report["home_before"] = self.reporter.home_metrics()
        self.recorder = recording.OpenAEVRecorder(
            args.platform_url,
            args.platform_token_file,
            self.ca,
            reporting_config=lambda name, payload: reporting.configure_payload(
                payload,
                name,
                self.asset,
                detection_assessed=name in audit.DETECTION_OPERATIONS,
            ),
        )
        self.persist()

    def persist(self) -> None:
        self.report["health"] = {
            "checks": self.guard.checks,
            "peak_observed_memory_bytes": self.guard.peak_memory,
            "last_sample": self.guard.last,
            "scope": "metrics samples, not instantaneous peak; pod UID/readiness/restart and isolated cluster UID and architecture baseline checks",
        }
        suite.save(self.path, self.report, replace=True)

    def check_health(self) -> None:
        if time.time() >= self.credential_deadline:
            raise AdversarialRunError("credential_safety_deadline_reached")
        self.guard()

    def control(self, role: str = "reader") -> transport.Response:
        response = self.client.request("GET", self.env + "/delivery", self.tokens[role])
        if response.status != 200:
            raise AdversarialRunError("positive_access_control_failed")
        suite.assert_body(self.fixture, "presence", response.body)
        return response

    def record(self, case: adversarial.AdversarialCase) -> None:
        self.check_health()
        before = (
            suite.snapshot(self.admin, self.fixture, case.observer)
            if case.observer
            else None
        )
        signals: dict[str, object] | None = None
        if case.observer == "published":
            signals = object_value(self.admin.call("GET", self.env + "/signals"))
            raw_cells = signals.get("cells")
            if not isinstance(raw_cells, list) or any(
                object_value(cell).get("pending_by_others") is not False
                for cell in raw_cells
            ):
                raise AdversarialRunError("draft_observer_baseline_ambiguous")
        result: dict[str, object] = {
            "case": case.name,
            "passed": False,
            "expected_status": case.status,
        }
        self.cases.append(result)
        self.persist()
        print("RUN " + case.name, flush=True)
        try:
            recorded = self.recorder.record_case(
                case.name,
                case.method,
                case.path,
                self.tokens[case.role],
                case.body,
                lambda body: adversarial.assert_response(
                    self.fixture, case, object_value(body), suite.assert_body
                ),
            )
        except recording.OpenAEVRecordingError as error:
            if error.inject_id is not None:
                result["inject_id"] = error.inject_id
            raise
        result.update(recorded)
        result["passed"] = False
        direct = self.client.request(
            case.method, case.path, self.tokens[case.role], case.body
        )
        result["direct_status"] = direct.status
        result["direct_elapsed_ms"] = direct.elapsed_ms
        if direct.status != case.status:
            raise AdversarialRunError("unexpected_direct_status")
        adversarial.assert_response(self.fixture, case, direct.body, suite.assert_body)
        if (
            case.observer
            and suite.snapshot(self.admin, self.fixture, case.observer) != before
        ):
            raise AdversarialRunError("mutation_changed_observed_state")
        if (
            signals is not None
            and object_value(self.admin.call("GET", self.env + "/signals")) != signals
        ):
            raise AdversarialRunError("mutation_changed_draft_signals")
        self.check_health()
        inject_id = string_value(result, "inject_id")
        evidence: reporting.AuditEvidence | None = None
        if case.detection_operation is not None:
            start, end = self.reporter.execution_window(inject_id)
            assessed = audit.assess_native_audit(
                self.admin.call,
                inject_id=inject_id,
                case_name=case.name,
                target_path=case.path,
                actor_id=string_value(
                    object_value(self.accounts[case.role]), "principal"
                ),
                window_start=start,
                window_end=end,
            )
            evidence = reporting.AuditEvidence(
                assessed.inject_id,
                assessed.case_name,
                assessed.window_start,
                assessed.window_end,
                assessed.query_succeeded,
                assessed.actor_scope_verified,
                assessed.event_id,
            )
            result["native_audit"] = {
                "event_id": assessed.event_id,
                "event_type": assessed.event_type,
                "window_start": start,
                "window_end": end,
            }
        result["home_reporting"] = self.reporter.publish_verdict(
            inject_id,
            case.name,
            reporting.VerifiedOutcome(
                inject_id,
                case.name,
                case.status,
                direct.status,
                recorded.get("domain_assertion_passed") is True,
                True,
                state_asserted=case.observer is not None,
            ),
            audit_evidence=evidence,
        )
        result["state_observer"] = case.observer
        result["draft_signals_unchanged"] = signals is not None
        result["passed"] = True
        self.persist()
        print("PASS " + case.name + " HTTP " + str(direct.status), flush=True)

    def phase(
        self, config: load.PhaseConfig, paths: dict[str, tuple[str, str]]
    ) -> load.PhaseReport:
        def request(context: load.RequestContext) -> load.ResponseObservation:
            if context.cancellation.is_set():
                raise AdversarialRunError("request_cancelled")
            role, path = paths[context.case.name]
            response = self.client.request(
                "GET",
                path,
                self.tokens[role],
                timeout_s=context.timeout_s,
                deadline=context.deadline,
            )
            if response.status == 200:
                suite.assert_body(self.fixture, "presence", response.body)
            else:
                mode = {
                    401: "unauthenticated",
                    404: "not_found",
                    429: "too_many_requests",
                }.get(response.status)
                if mode is None:
                    raise AdversarialRunError("unexpected_load_status")
                transport.assert_refusal(self.fixture, mode, response.body)
            return load.ResponseObservation(
                response.status,
                response.elapsed_ms,
                response.response_bytes,
                True,
                True,
                response.retry_after,
            )

        print(
            "LOAD " + config.name + " requests=" + str(config.request_count), flush=True
        )
        background = health.BackgroundGuard(self.guard)

        def cached_health() -> None:
            if time.time() >= self.credential_deadline:
                raise AdversarialRunError("credential_safety_deadline_reached")
            background()

        if time.time() >= self.credential_deadline:
            raise AdversarialRunError("credential_safety_deadline_reached")
        background.start()
        try:
            result = load.run_phase(config, request, cached_health)
            self.phases.append(asdict(result))
        except load.PhaseFailure as error:
            self.phases.append(asdict(error.report))
            raise
        finally:
            try:
                background.finish()
            except health.GuardError as error:
                if self.phases and self.phases[-1].get("name") == config.name:
                    self.phases[-1]["failure"] = "post_phase_health_" + str(error)
                raise
            finally:
                self.report.setdefault("load_health", [])
                load_health = self.report["load_health"]
                if not isinstance(load_health, list):
                    raise AdversarialRunError("invalid_load_health_report")
                load_health.append({"phase": config.name, **background.evidence()})
                self.persist()
        self.persist()
        print(
            "PASS " + config.name + " " + json.dumps(dict(result.status_counts)),
            flush=True,
        )
        return result

    def run_full_prefix(self, cursor: str) -> None:
        delivery = self.env + "/delivery"
        outside = self.fixture.get("outside_envs")
        if (
            not isinstance(outside, list)
            or not outside
            or not isinstance(outside[0], str)
        ):
            raise AdversarialRunError("invalid_outside_path")
        outside_delivery = outside[0] + "/delivery"
        self.phase(
            load.PhaseConfig(
                "mixed-authorization",
                (
                    load.RequestCase("authorized", frozenset({200})),
                    load.RequestCase("tampered", frozenset({401})),
                    load.RequestCase("outside", frozenset({404})),
                    load.RequestCase("opaque-cursor", frozenset({200})),
                ),
                40,
            ),
            {
                "authorized": ("reader", delivery),
                "tampered": ("tampered", delivery),
                "outside": ("reader", outside_delivery),
                "opaque-cursor": ("reader", delivery + "?cursor=sql-opaque-control"),
            },
        )
        self.control("revealer")
        burst = self.phase(
            load.PhaseConfig(
                "native-principal-throttling",
                (load.RequestCase("authorized-burst", frozenset({200, 429})),),
                100,
            ),
            {"authorized-burst": ("revealer", delivery)},
        )
        throttled = [
            row.observation
            for row in burst.observations
            if row.observation.status == 429
        ]
        if not throttled or not any(
            row.observation.status == 200 for row in burst.observations
        ):
            raise AdversarialRunError("native_throttling_not_observed")
        self.tokens["burst-tampered"] = str(suite.tamper(self.tokens["revealer"]))
        self.phase(
            load.PhaseConfig(
                "denials-after-throttling",
                (
                    load.RequestCase("tampered", frozenset({401})),
                    load.RequestCase("outside", frozenset({404})),
                ),
                40,
            ),
            {
                "tampered": ("burst-tampered", delivery),
                "outside": ("revealer", outside_delivery),
            },
        )
        self.control("reader")
        retry_after = max(row.native_retry_after or 0 for row in throttled)
        if not 0 < retry_after <= 30:
            raise AdversarialRunError("unbounded_recovery_wait")
        time.sleep(retry_after)
        self.check_health()
        recovery = self.control("revealer")
        self.report["throttling_recovery"] = {
            "observed_429": len(throttled),
            "wait_s": retry_after,
            "same_principal_status": recovery.status,
            "elapsed_ms": recovery.elapsed_ms,
            "other_principal_control": 200,
        }
        # Measure non-mutating bursts while fresh credentials have the largest
        # validity margin. Revocation phases wait until static probes finish.
        for case in adversarial.build_cases(self.fixture, cursor):
            self.record(case)

    def run_lifecycle(self) -> None:
        # This control and cursor come from the current fresh fixture, whether
        # running the full suite or lifecycle alone. No prefix report is read.
        self.check_health()
        cursor = string_value(self.control("reader").body, "cursor")
        delivery = self.env + "/delivery"
        self.admin.grant(
            self.env, string_value(self.reader, "principal"), "read", remove=True
        )
        self.phase(
            load.PhaseConfig(
                "post-read-grant-removal",
                (
                    load.RequestCase("removed-grant", frozenset({404})),
                    load.RequestCase("removed-grant-cursor", frozenset({404})),
                ),
                40,
            ),
            {
                "removed-grant": ("reader", delivery),
                "removed-grant-cursor": (
                    "reader",
                    delivery + "?" + urlencode({"cursor": cursor}),
                ),
            },
        )
        self.record(
            adversarial.AdversarialCase(
                "whack-after-read-removal",
                "GET",
                delivery,
                "reader",
                404,
                "not_found",
                detection_operation="delivery.fetch",
            )
        )
        # Restore and prove access before revoking the credential. Otherwise a
        # missing grant could mask a credential-revocation bug.
        self.admin.grant(self.env, string_value(self.reader, "principal"), "read")
        self.control("reader")
        self.admin.assure()
        self.admin.call(
            "DELETE", string_value(self.reader, "revoke_path"), expected=204
        )
        self.phase(
            load.PhaseConfig(
                "post-credential-revocation",
                (
                    load.RequestCase("revoked", frozenset({401})),
                    load.RequestCase("revoked-cursor", frozenset({401})),
                ),
                40,
            ),
            {
                "revoked": ("reader", delivery),
                "revoked-cursor": (
                    "reader",
                    delivery + "?" + urlencode({"cursor": cursor}),
                ),
            },
        )
        self.record(
            adversarial.AdversarialCase(
                "whack-after-credential-revocation",
                "GET",
                delivery,
                "reader",
                401,
                "unauthenticated",
            )
        )

    def run(self) -> None:
        self.check_health()
        if self.run_scope == "full":
            self.run_full_prefix(string_value(self.control().body, "cursor"))
        elif self.run_scope != "lifecycle":
            raise AdversarialRunError("invalid_run_scope")
        self.run_lifecycle()
        self.check_health()
        self.report["home_after"] = self.reporter.home_metrics()
        self.report["completed_at"] = datetime.now(UTC).isoformat()
        self.report["passed"] = True
        self.persist()


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument(
        "--scope",
        choices=("full", "lifecycle"),
        default="full",
        help="lifecycle runs 80 fresh revocation load requests and two atomic cases",
    )
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
        SystemExit,
        reporting.ReportingError,
        recording.OpenAEVRecordingError,
        audit.NativeAuditError,
    ) as error:
        if runner is not None:
            runner.report["passed"] = False
            runner.report["failure"] = type(error).__name__
            if isinstance(error, (health.GuardError, AdversarialRunError)):
                # These owned exception classes contain literal diagnostic
                # codes only. External exception text may contain credentials.
                runner.report["failure_code"] = str(error)
            runner.persist()
        print(
            "FAIL " + type(error).__name__ + "; review owner-only partial evidence",
            file=sys.stderr,
        )
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()

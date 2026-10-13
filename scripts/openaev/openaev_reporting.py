"""Publish independently verified, disposable Hikyo authorization results.

Pinned OpenAEV API: 3.261005.0 / 430c0283e63573c2467d058b735aaf49b17166fa.
Native expectations are generated at launch. Historical injects are never
replayed, modified or deleted by this module. Positive controls create no attack
metrics. Detection requires an assessed, bounded native audit observation.
"""

from __future__ import annotations

import copy
import http.client
import json
import re
import uuid
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Protocol
from urllib.request import Request

import openaev_security

HIKYO_ORIGIN = openaev_security.HIKYO_ORIGIN
ASSET_NAME = "Hikyo disposable authorization lab"
ASSET_SEARCH_SIZE = 100
MAX_ASSET_SEARCH_PAGES = 20
ASSET_REFERENCE = "hikyo-isolated-lab-authorization-target"
ASSERTION_SOURCE = "Hikyo lab independent assertions"
AUDIT_SOURCE = "Hikyo lab native audit evidence"
MONITORING_SOURCE = "Hikyo lab native audit > Prometheus > Alertmanager > lab inbox"
DISPATCH_MESSAGE = "The inject has been published and is now waiting to be consumed."

# These cases assert withheld plaintext or rejected access, not successful
# requests permitted by the fixture. Keep this list synchronized with the suite.
NEGATIVE_CASE_STATUS = {
    "opt-in-off-presence": 200,
    "read-config-secret-presence": 200,
    "wrong-environment": 404,
    "wrong-project": 404,
    "wrong-tenant": 404,
    "expired-token": 401,
    "missing-token": 401,
    "tampered-token": 401,
    "workload-human-plaintext-reveal": 404,
    "self-grant-reveal": 404,
    "create-service-account": 404,
    "mint-own-credential": 404,
    "create-project": 404,
    "write-without-edit": 404,
    "publish-without-publish": 404,
    "withdraw-machine-opt-in": 200,
    "remove-reveal-grant": 200,
    "remove-read-grant": 404,
    "cursor-after-read-removal": 404,
    "revoked-token": 401,
    "cursor-after-token-revocation": 401,
    "whack-cursor-sql": 200,
    "whack-cursor-xss": 200,
    "whack-cursor-traversal": 200,
    "whack-cursor-unicode": 200,
    "whack-cursor-boundary-128": 200,
    "whack-cursor-boundary-129": 400,
    "whack-cursor-8k": 400,
    "whack-cursor-projection-binding": 200,
    "whack-cursor-outside-scope": 404,
    "whack-long-workload-token": 401,
    "whack-tampered-workload-token": 401,
    "whack-hostile-scalar-write": 404,
    "whack-8k-scalar-write": 404,
    "whack-oversized-scalar-write": 400,
    "whack-structured-value": 400,
    "whack-deep-json-value": 400,
    "whack-value-mass-assignment": 400,
    "whack-grant-mass-assignment": 400,
    "whack-publish-mass-assignment": 400,
    "whack-publish-bypass-attempt": 404,
    "whack-after-read-removal": 404,
    "whack-after-credential-revocation": 401,
}
POSITIVE_CASES = frozenset(
    {"read-and-reveal", "config-only-projection", "conditional-current"}
)
STATE_CASES = frozenset(
    {
        "self-grant-reveal",
        "create-service-account",
        "mint-own-credential",
        "create-project",
        "write-without-edit",
        "publish-without-publish",
        "whack-hostile-scalar-write",
        "whack-8k-scalar-write",
        "whack-oversized-scalar-write",
        "whack-structured-value",
        "whack-deep-json-value",
        "whack-value-mass-assignment",
        "whack-grant-mass-assignment",
        "whack-publish-mass-assignment",
        "whack-publish-bypass-attempt",
    }
)
DETECTION_CASES = frozenset(
    {
        "wrong-environment",
        "wrong-project",
        "wrong-tenant",
        "write-without-edit",
        "publish-without-publish",
        "remove-read-grant",
        "cursor-after-read-removal",
        "whack-cursor-outside-scope",
        "whack-hostile-scalar-write",
        "whack-8k-scalar-write",
        "whack-publish-bypass-attempt",
        "whack-after-read-removal",
    }
)
TYPES = frozenset({"PREVENTION", "DETECTION", "VULNERABILITY"})


class ReportingError(Exception):
    """Sanitized code only, without bodies, headers or credentials."""


class PlatformTransport(Protocol):
    def request(
        self, method: str, path: str, payload: dict[str, object] | None = None
    ) -> object: ...


class NativeJSONTransport:
    """Extend the verified pilot transport to JSON arrays without relaxing TLS."""

    def __init__(self, platform_url: str, token_file: Path, ca_file: Path):
        self._base = openaev_security.OpenAEVRecorder(
            platform_url, token_file, ca_file
        )._api

    def request(
        self, method: str, path: str, payload: dict[str, object] | None = None
    ) -> object:
        request = Request(
            self._base.origin + "/api/" + path,
            data=json.dumps(payload).encode() if payload is not None else None,
            method=method,
            headers={
                "Authorization": "Bearer " + self._base.token,
                "Content-Type": "application/json",
                "Accept": "application/json",
            },
        )
        try:
            with self._base.opener.open(request, timeout=15) as response:
                raw = response.read(2_097_153)
        except (OSError, http.client.HTTPException, openaev_security._pilot.PilotError):
            raise ReportingError("reporting_platform_request_failed") from None
        if len(raw) > 2_097_152:
            raise ReportingError("reporting_response_too_large")
        return json.loads(raw)


def _uuid(value: object) -> str:
    if not isinstance(value, str):
        raise ReportingError("invalid_reporting_identifier")
    try:
        if str(uuid.UUID(value)) != value:
            raise ValueError()
    except ValueError:
        raise ReportingError("invalid_reporting_identifier") from None
    return value


def _object(value: object, code: str) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise ReportingError(code)
    return {k: v for k, v in value.items()}


def _page_count(value: object) -> int:
    if type(value) is not int or value < 0:
        raise ReportingError("invalid_asset_search_pagination")
    return value


def _objects(value: object, code: str) -> list[dict[str, object]]:
    if not isinstance(value, list):
        raise ReportingError(code)
    return [_object(row, code) for row in value]


def _case(name: str) -> None:
    if name not in NEGATIVE_CASE_STATUS and name not in POSITIVE_CASES:
        raise ReportingError("unknown_reporting_case")


def configure_payload(
    payload: dict[str, object],
    case_name: str,
    asset_id: str,
    *,
    detection_assessed: bool = False,
    monitoring_assessed: bool = False,
) -> dict[str, object]:
    """Set native target and expectations before creating a fresh inject."""
    _case(case_name)
    if monitoring_assessed and not detection_assessed:
        raise ReportingError("monitoring_requires_detection_expectation")
    _uuid(asset_id)
    result = copy.deepcopy(payload)
    if result.get("inject_title") != "Hikyo isolated lab security: " + case_name:
        raise ReportingError("reporting_case_title_mismatch")
    content = _object(result.get("inject_content"), "invalid_reporting_content")
    uri = content.get("uri")
    if not isinstance(uri, str) or not uri.startswith(HIKYO_ORIGIN + "/api/v1/orgs/"):
        raise ReportingError("reporting_target_outside_disposable_lab")
    # Reuse the transport's strict origin, traversal, and query checks.
    if openaev_security._target(uri[len(HIKYO_ORIGIN) :]) != uri:
        raise ReportingError("reporting_target_outside_disposable_lab")
    result["inject_assets"] = [asset_id]
    result["inject_asset_groups"] = []
    result["inject_teams"] = []
    result["inject_all_teams"] = False
    expectation_types: list[str] = []
    if case_name in NEGATIVE_CASE_STATUS:
        expectation_types = ["PREVENTION", "VULNERABILITY"]
        if detection_assessed:
            if case_name not in DETECTION_CASES:
                raise ReportingError("detection_requires_authenticated_denial_case")
            expectation_types.append("DETECTION")
    elif detection_assessed:
        raise ReportingError("positive_control_detection_rejected")
    descriptions = {
        "PREVENTION": "Independently asserted rejection or withheld plaintext for this input-validation or authorization case; not injector execution status.",
        "VULNERABILITY": "No input-validation or authorization exploit observed in this bounded case; not a general vulnerability scan.",
        "DETECTION": "Native Hikyo audit observation assessed for this authenticated denial, matched to actor, scope and dispatch window.",
    }
    if monitoring_assessed:
        descriptions["DETECTION"] = (
            "Independent native audit collector, Prometheus rule, Alertmanager notification and lab inbox receipt matched to this new execution; historical audit-only scores are unchanged."
        )
    content["expectations"] = [
        {
            "expectation_type": kind,
            "expectation_name": kind.title() + ": " + case_name,
            "expectation_description": descriptions[kind],
            "expectation_score": 100,
            "expectation_expiration_time": 3600,
            "expectation_expectation_group": False,
        }
        for kind in expectation_types
    ]
    result["inject_content"] = content
    return result


@dataclass(frozen=True)
class VerifiedOutcome:
    inject_id: str
    case_name: str
    expected_status: int
    actual_status: int
    recorded_body_asserted: bool
    direct_body_asserted: bool
    state_asserted: bool = False

    def validate(self, inject_id: str, case_name: str) -> None:
        if self.inject_id != inject_id or self.case_name != case_name:
            raise ReportingError("assertion_evidence_case_mismatch")
        if (
            self.expected_status != NEGATIVE_CASE_STATUS.get(case_name)
            or self.actual_status != self.expected_status
            or self.recorded_body_asserted is not True
            or self.direct_body_asserted is not True
            or (case_name in STATE_CASES and self.state_asserted is not True)
        ):
            raise ReportingError("independent_security_assertions_required")


@dataclass(frozen=True)
class AuditEvidence:
    inject_id: str
    case_name: str
    window_start: str
    window_end: str
    query_succeeded: bool
    actor_scope_verified: bool
    event_id: str | None = None

    def validate(self, inject_id: str, case_name: str) -> None:
        if (
            self.inject_id != inject_id
            or self.case_name != case_name
            or self.query_succeeded is not True
            or self.actor_scope_verified is not True
        ):
            raise ReportingError("native_audit_assessment_required")
        try:
            start = datetime.fromisoformat(self.window_start.replace("Z", "+00:00"))
            end = datetime.fromisoformat(self.window_end.replace("Z", "+00:00"))
            if (
                start.tzinfo is None
                or end.tzinfo is None
                or not 0 < (end - start).total_seconds() <= 600
            ):
                raise ValueError()
        except (ValueError, TypeError):
            raise ReportingError("invalid_native_audit_window") from None
        if self.event_id is not None:
            if not self.event_id.startswith("evt_"):
                raise ReportingError("invalid_native_audit_event_identifier")
            _uuid(self.event_id.removeprefix("evt_"))


@dataclass(frozen=True)
class MonitoringEvidence:
    """New independent-chain proof, never a reinterpretation of old audit scores.

    The independent monitor verifies actual native polling, rule evaluation and
    Alertmanager webhook delivery. The exercise runner cannot synthesize these
    observations from its own request or its direct control response.
    """

    native_audit: AuditEvidence
    run_id: str
    collector_principal_id: str
    collector_scope_only_verified: bool
    baseline_at: str
    native_occurred_at: str
    native_recorded_at: str
    collector_polled_at: str
    prometheus_firing_at: str
    alertmanager_observed_at: str
    inbox_received_at: str
    alertmanager_fingerprint: str
    inbox_receipt_sha256: str
    scope_actor_window_verified: bool
    rule_fired: bool
    notification_delivered: bool

    def validate(self, inject_id: str, case_name: str) -> None:
        self.native_audit.validate(inject_id, case_name)
        _uuid(self.run_id)
        if (
            self.native_audit.event_id is None
            or re.fullmatch(r"[A-Za-z0-9_-]{1,128}", self.collector_principal_id)
            is None
            or re.fullmatch(r"[a-f0-9]{16}", self.alertmanager_fingerprint) is None
            or re.fullmatch(r"[a-f0-9]{64}", self.inbox_receipt_sha256) is None
            or self.collector_scope_only_verified is not True
            or self.scope_actor_window_verified is not True
            or self.rule_fired is not True
            or self.notification_delivered is not True
        ):
            raise ReportingError("independent_monitoring_chain_required")
        try:
            values = [
                datetime.fromisoformat(value.replace("Z", "+00:00"))
                for value in (
                    self.native_audit.window_start,
                    self.native_audit.window_end,
                    self.baseline_at,
                    self.native_occurred_at,
                    self.native_recorded_at,
                    self.collector_polled_at,
                    self.prometheus_firing_at,
                    self.alertmanager_observed_at,
                    self.inbox_received_at,
                )
            ]
            if any(value.tzinfo is None for value in values):
                raise ValueError()
            (
                start,
                end,
                baseline,
                occurred,
                recorded,
                polled,
                firing,
                observed,
                received,
            ) = values
            if (
                not 0 <= (start - baseline).total_seconds() <= 600
                or not start <= occurred <= end
                or not start <= recorded <= end
                or not recorded <= polled <= firing <= received
                or not firing <= observed
                or not 0 <= (observed - start).total_seconds() <= 600
                or not 0 <= (received - start).total_seconds() <= 600
            ):
                raise ValueError()
        except (ValueError, TypeError):
            raise ReportingError("invalid_independent_monitoring_window") from None

    def source_name(self) -> str:
        return (
            MONITORING_SOURCE
            + " "
            + str(self.native_audit.event_id)
            + " run="
            + self.run_id
            + " am="
            + self.alertmanager_fingerprint
            + " receipt="
            + self.inbox_receipt_sha256
        )


class OpenAEVReporter:
    def __init__(
        self,
        platform_url: str,
        token_file: Path,
        ca_file: Path,
        owner_id: str | None = None,
    ):
        self._api: PlatformTransport = NativeJSONTransport(
            platform_url, token_file, ca_file
        )
        authenticated = self._request("GET", "me")
        self.owner_id = _uuid(authenticated.get("user_id"))
        if owner_id is not None and _uuid(owner_id) != self.owner_id:
            raise ReportingError("reporting_owner_identity_mismatch")
        self._asset_id: str | None = None

    def _request(
        self, method: str, path: str, payload: dict[str, object] | None = None
    ) -> dict[str, object]:
        try:
            return _object(
                self._api.request(method, path, payload), "invalid_reporting_object"
            )
        except ReportingError:
            raise
        except (OSError, ValueError, TypeError, http.client.HTTPException, openaev_security._pilot.PilotError):
            # Never surface raw HTTP errors whose details may contain secret bodies.
            raise ReportingError("reporting_platform_request_failed") from None

    def _array_request(
        self, method: str, path: str, payload: dict[str, object] | None = None
    ) -> list[dict[str, object]]:
        try:
            return _objects(
                self._api.request(method, path, payload), "invalid_reporting_array"
            )
        except ReportingError:
            raise
        except (OSError, ValueError, TypeError, http.client.HTTPException, openaev_security._pilot.PilotError):
            raise ReportingError("reporting_platform_array_request_failed") from None

    def _expectations(self, inject_id: str) -> list[dict[str, object]]:
        if self._asset_id is None:
            raise ReportingError("reporting_asset_not_verified")
        return self._array_request(
            "GET",
            "atomic-testings/"
            + inject_id
            + "/target_results/"
            + self._asset_id
            + "/types/ASSETS",
        )

    def _asset_matches(self) -> list[dict[str, object]]:
        """Read the complete bounded native Spring Page inventory before writes."""
        matches: list[dict[str, object]] = []
        observed_ids: set[str] = set()
        totals: tuple[int, int] | None = None
        for number in range(MAX_ASSET_SEARCH_PAGES):
            page = self._request(
                "POST", "endpoints/search",
                {"page": number, "size": ASSET_SEARCH_SIZE, "textSearch": ASSET_NAME,
                 "sorts": [{"property": "asset_external_reference", "direction": "asc"}]},
            )
            rows = _objects(page.get("content"), "invalid_asset_search_response")
            native_number = _page_count(page.get("number"))
            size = _page_count(page.get("size"))
            pages = _page_count(page.get("totalPages"))
            total = _page_count(page.get("totalElements"))
            count = _page_count(page.get("numberOfElements"))
            if pages > MAX_ASSET_SEARCH_PAGES or total > ASSET_SEARCH_SIZE * MAX_ASSET_SEARCH_PAGES:
                raise ReportingError("asset_search_page_limit")
            terminal = number + 1 >= pages
            if (native_number != number or size != ASSET_SEARCH_SIZE
                    or pages != (total + size - 1) // size
                    or count != len(rows)
                    or count != min(size, max(0, total - number * size))
                    or page.get("last") is not terminal):
                raise ReportingError("invalid_asset_search_pagination")
            if totals is not None and totals != (pages, total):
                raise ReportingError("asset_search_inventory_changed")
            totals = (pages, total)
            for row in rows:
                asset_id = _uuid(row.get("asset_id"))
                if asset_id in observed_ids:
                    raise ReportingError("asset_search_inventory_repeated")
                observed_ids.add(asset_id)
                if row.get("asset_external_reference") == ASSET_REFERENCE:
                    matches.append(row)
            if terminal:
                return matches
        raise ReportingError("asset_search_page_limit")

    def ensure_asset(self) -> str:
        self._asset_id = None
        found = self._asset_matches()
        if len(found) > 1:
            raise ReportingError("duplicate_disposable_reporting_assets")
        if found:
            summary = found[0]
            asset_id = _uuid(summary.get("asset_id"))
            full = self._request("GET", "endpoints/" + asset_id)
            if full.get("asset_id") != asset_id or any(
                full.get(key) != summary.get(key)
                for key in ("asset_name", "asset_category", "asset_subcategory")
            ):
                raise ReportingError("disposable_reporting_asset_identity_mismatch")
            # Search carries the external reference but no URL; overview carries
            # URL and agents but omits the external reference. Validate both
            # authenticated DTOs against the same native asset UUID.
            row = dict(full)
            row["asset_external_reference"] = summary["asset_external_reference"]
        else:
            row = self._request(
                "POST",
                "endpoints/agentless",
                {
                    "asset_name": ASSET_NAME,
                    "asset_description": "Disposable synthetic Hikyo authorization lab. No production data or credentials.",
                    "asset_external_reference": ASSET_REFERENCE,
                    "asset_url": HIKYO_ORIGIN,
                    "asset_category": "WEB_APPLICATION",
                    "asset_subcategory": "WEB_API",
                    "asset_internet_facing": False,
                    "asset_tags": [],
                },
            )
        self._asset_id = self._verify_asset(row)
        return self._asset_id

    def _verify_asset(self, row: dict[str, object]) -> str:
        if (
            row.get("asset_name") != ASSET_NAME
            or row.get("asset_url") != HIKYO_ORIGIN
            or row.get("asset_external_reference") != ASSET_REFERENCE
            or row.get("asset_category") != "WEB_APPLICATION"
            or row.get("asset_subcategory") != "WEB_API"
        ):
            raise ReportingError("disposable_reporting_asset_mismatch")
        asset_id = _uuid(row.get("asset_id"))
        agents = row.get("asset_agents")
        if not isinstance(agents, list) or agents:
            raise ReportingError("reporting_asset_must_be_agentless")
        return asset_id

    def execution_window(self, inject_id: str) -> tuple[str, str]:
        """Return the precise native dispatch trace and unchanged native finish.

        Pinned Executor.java creates this global INFO trace before its
        afterCommit RabbitMQ publication. InjectsExecutionJob executes the atomic
        launch inside TenantScopedJobRunner's tenant transaction.
        tracking_sent_date is stored as timestamptz(0), which rounds seconds;
        execution_traces.execution_time and tracking_end_date retain microseconds.
        Never turn that rounded metadata into a security correlation bound.
        """
        _uuid(inject_id)
        detail = self._request("GET", "atomic-testings/" + inject_id)
        if _uuid(detail.get("inject_id")) != inject_id:
            raise ReportingError("native_execution_inject_identity_mismatch")
        summary = _object(
            detail.get("inject_status"), "missing_native_execution_status"
        )
        status = self._request("GET", "injects/status-all-traces?injectId=" + inject_id)
        if (
            _uuid(status.get("status_id")) != _uuid(summary.get("status_id"))
            or status.get("status_name") != "EXECUTED"
            or summary.get("status_name") != "EXECUTED"
            or status.get("tracking_sent_date") != summary.get("tracking_sent_date")
            or status.get("tracking_end_date") != summary.get("tracking_end_date")
        ):
            raise ReportingError("native_execution_status_binding_mismatch")
        traces = _objects(
            status.get("status_main_traces"), "missing_native_dispatch_traces"
        )
        if not 2 <= len(traces) <= 128 or any(
            trace.get("execution_agent") is not None
            or trace.get("execution_context_identifiers", []) != []
            for trace in traces
        ):
            raise ReportingError("foreign_or_unbounded_native_dispatch_traces")
        dispatch = [
            trace
            for trace in traces
            if trace.get("execution_message") == DISPATCH_MESSAGE
        ]
        complete = [
            trace for trace in traces if trace.get("execution_action") == "COMPLETE"
        ]
        if (
            len(dispatch) != 1
            or dispatch[0].get("execution_action") != "EXECUTION"
            or dispatch[0].get("execution_status") != "INFO"
            or len(complete) != 1
            or complete[0].get("execution_status") != "EXECUTED"
        ):
            raise ReportingError("missing_or_ambiguous_native_dispatch_traces")
        start = dispatch[0].get("execution_time")
        completed = complete[0].get("execution_time")
        sent = status.get("tracking_sent_date")
        end = status.get("tracking_end_date")
        if (
            not isinstance(start, str)
            or not isinstance(end, str)
            or not isinstance(completed, str)
            or not isinstance(sent, str)
        ):
            raise ReportingError("missing_native_execution_window")
        try:
            start_date = datetime.fromisoformat(start.replace("Z", "+00:00"))
            end_date = datetime.fromisoformat(end.replace("Z", "+00:00"))
            completed_date = datetime.fromisoformat(completed.replace("Z", "+00:00"))
            sent_date = datetime.fromisoformat(sent.replace("Z", "+00:00"))
            if (
                any(
                    value.tzinfo is None
                    for value in (start_date, end_date, completed_date, sent_date)
                )
                or not start_date <= completed_date <= end_date
                or not 0 <= (end_date - start_date).total_seconds() <= 300
            ):
                raise ValueError()
        except ValueError:
            raise ReportingError("invalid_native_execution_window") from None
        return start, end

    def _validate_native_rows(
        self, rows: list[dict[str, object]], expected_types: set[str]
    ) -> None:
        kinds = [r.get("inject_expectation_type") for r in rows]
        if any(not isinstance(kind, str) or kind not in TYPES for kind in kinds):
            raise ReportingError("invalid_native_expectation_type")
        if len(rows) != len(expected_types) or set(kinds) != expected_types:
            raise ReportingError("native_expectation_set_mismatch")
        ids = [_uuid(row.get("inject_expectation_id")) for row in rows]
        if len(set(ids)) != len(ids):
            raise ReportingError("duplicate_native_expectation_identifier")
        # Validate all rows before writing any verdict; refuse foreign targets.
        for row in rows:
            _uuid(row.get("inject_expectation_id"))
            target = row.get("inject_expectation_asset")
            if isinstance(target, dict):
                target = target.get("asset_id")
            if (
                target != self._asset_id
                or row.get("inject_expectation_expected_score") != 100
            ):
                raise ReportingError("native_expectation_target_mismatch")

    def publish_verdict(
        self,
        inject_id: str,
        case_name: str,
        outcome: VerifiedOutcome,
        *,
        audit_evidence: AuditEvidence | None = None,
        monitoring_evidence: MonitoringEvidence | None = None,
    ) -> dict[str, object]:
        _uuid(inject_id)
        _case(case_name)
        if case_name not in NEGATIVE_CASE_STATUS:
            raise ReportingError("positive_control_has_no_attack_verdict")
        outcome.validate(inject_id, case_name)
        if monitoring_evidence is not None:
            if audit_evidence is not None:
                raise ReportingError("duplicate_detection_evidence_sources")
            monitoring_evidence.validate(inject_id, case_name)
            audit_evidence = monitoring_evidence.native_audit
        if self._asset_id is None:
            raise ReportingError("reporting_asset_not_verified")
        detail = self._request("GET", "atomic-testings/" + inject_id)
        if detail.get("inject_title") != "Hikyo isolated lab security: " + case_name:
            raise ReportingError("reporting_inject_case_mismatch")
        content = _object(detail.get("inject_content"), "invalid_reporting_content")
        uri = content.get("uri")
        if not isinstance(uri, str) or not uri.startswith(
            HIKYO_ORIGIN + "/api/v1/orgs/"
        ):
            raise ReportingError("reporting_target_outside_disposable_lab")
        openaev_security._target(uri[len(HIKYO_ORIGIN) :])
        rows = self._expectations(inject_id)
        expected_types = {"PREVENTION", "VULNERABILITY"}
        if audit_evidence is not None:
            if case_name not in DETECTION_CASES:
                raise ReportingError("detection_requires_authenticated_denial_case")
            audit_evidence.validate(inject_id, case_name)
            sent, finished = self.execution_window(inject_id)
            sent_date = datetime.fromisoformat(sent.replace("Z", "+00:00"))
            finished_date = datetime.fromisoformat(finished.replace("Z", "+00:00"))
            audit_start = datetime.fromisoformat(
                audit_evidence.window_start.replace("Z", "+00:00")
            )
            audit_end = datetime.fromisoformat(
                audit_evidence.window_end.replace("Z", "+00:00")
            )
            if audit_start != sent_date or audit_end != finished_date:
                raise ReportingError("native_audit_execution_window_mismatch")
            expected_types.add("DETECTION")
        self._validate_native_rows(rows, expected_types)
        results: list[dict[str, object]] = []
        source_names: dict[str, str] = {}
        for row in rows:
            kind = row["inject_expectation_type"]
            score = 100
            source_name = ASSERTION_SOURCE
            if kind == "DETECTION":
                if audit_evidence is None:
                    raise ReportingError("native_audit_assessment_required")
                score = 100 if audit_evidence.event_id is not None else 0
                source_name = AUDIT_SOURCE
                if audit_evidence.event_id is not None:
                    source_name += " " + audit_evidence.event_id
                if monitoring_evidence is not None:
                    source_name = monitoring_evidence.source_name()
            expectation_id = _uuid(row.get("inject_expectation_id"))
            self._request(
                "PUT",
                "expectations/" + expectation_id,
                {
                    "source_id": self.owner_id,
                    "source_type": "user",
                    "source_name": source_name,
                    "expectation_score": score,
                },
            )
            source_names[expectation_id] = source_name
            results.append(
                {
                    "id": expectation_id,
                    "type": kind,
                    "score": score,
                    "status": "SUCCESS" if score == 100 else "FAILED",
                }
            )
        # Read the persisted rows again, proving native score/status rather than
        # treating successful API submission or a local result as reporting proof.
        stored_rows = self._expectations(inject_id)
        self._validate_native_rows(stored_rows, expected_types)
        if {_uuid(row.get("inject_expectation_id")) for row in stored_rows} != {
            _uuid(row.get("inject_expectation_id")) for row in rows
        }:
            raise ReportingError("native_expectation_identity_changed")
        by_id = {_uuid(r.get("inject_expectation_id")): r for r in stored_rows}
        for result in results:
            persisted_row = by_id.get(str(result["id"]))
            if (
                persisted_row is None
                or persisted_row.get("inject_expectation_score") != result["score"]
                or persisted_row.get("inject_expectation_status") != result["status"]
            ):
                raise ReportingError("native_verdict_persistence_mismatch")
            sources = _objects(
                persisted_row.get("inject_expectation_results"),
                "invalid_native_source_results",
            )
            own = [s for s in sources if s.get("sourceId") == self.owner_id]
            if (
                len(own) != 1
                or own[0].get("score") != result["score"]
                or own[0].get("sourceType") != "user"
                or own[0].get("sourceName") != source_names[str(result["id"])]
                or (
                    monitoring_evidence is not None
                    and result["type"] == "DETECTION"
                    and own[0].get("sourceName") != monitoring_evidence.source_name()
                )
            ):
                raise ReportingError("native_verdict_provenance_mismatch")
        return {
            "inject_id": inject_id,
            "case_name": case_name,
            "asset_id": self._asset_id,
            "expectations": results,
            "native_expectation_readback": [
                {
                    key: row[key]
                    for key in (
                        "inject_expectation_id",
                        "inject_expectation_type",
                        "inject_expectation_score",
                        "inject_expectation_status",
                    )
                }
                | {
                    "inject_expectation_results": [
                        {
                            key: source[key]
                            for key in ("sourceId", "sourceType", "sourceName", "score")
                        }
                        for source in _objects(
                            row.get("inject_expectation_results"),
                            "invalid_native_source_results",
                        )
                        if source.get("sourceId") == self.owner_id
                    ]
                }
                for row in stored_rows
            ],
        }

    def home_metrics(self) -> dict[str, object]:
        """Read actual indexed type/status counts from the Home widget API."""
        series: list[dict[str, object]] = []
        for status in ("SUCCESS", "FAILED", "PENDING"):
            filters = [
                {
                    "id": str(uuid.uuid4()),
                    "key": key,
                    "mode": "or",
                    "values": [value],
                    "operator": "eq",
                }
                for key, value in (
                    ("base_entity", "expectation-inject"),
                    ("inject_expectation_status", status),
                )
            ]
            series.append(
                {"name": status, "filter": {"mode": "and", "filters": filters}}
            )
        response = self._array_request(
            "POST",
            "dashboards/adhoc/series",
            {
                "widget_config": {
                    "title": "Hikyo authorization lab native metrics",
                    "series": series,
                    "field": "inject_expectation_type",
                    "mode": "structural",
                    "limit": 100,
                    "widget_configuration_type": "structural-histogram",
                    "time_range": "LAST_QUARTER",
                    "date_attribute": "base_created_at",
                },
                "parameters": {},
            },
        )
        statuses = {"SUCCESS", "FAILED", "PENDING"}
        counts: dict[str, dict[str, int]] = {
            kind: {status: 0 for status in statuses} for kind in TYPES
        }
        seen: set[str] = set()
        for row in response:
            bucket_status = row.get("label")
            if (
                not isinstance(bucket_status, str)
                or bucket_status not in statuses
                or bucket_status in seen
            ):
                raise ReportingError("invalid_home_metric_status")
            seen.add(bucket_status)
            buckets = _objects(row.get("data"), "invalid_home_metric_buckets")
            seen_kinds: set[str] = set()
            for bucket in buckets:
                kind = bucket.get("key", bucket.get("label"))
                value = bucket.get("value")
                if (
                    not isinstance(kind, str)
                    or kind in seen_kinds
                    or not isinstance(value, int)
                    or isinstance(value, bool)
                    or value < 0
                ):
                    raise ReportingError("invalid_home_metric_bucket")
                kind = kind.upper()
                if kind not in TYPES | {"ARTICLE", "CHALLENGE", "MANUAL"}:
                    raise ReportingError("unknown_home_expectation_type")
                if kind in seen_kinds:
                    raise ReportingError("duplicate_home_expectation_type")
                seen_kinds.add(kind)
                if kind in counts:
                    counts[kind][bucket_status] = value
        if seen != statuses:
            raise ReportingError("incomplete_home_metric_series")
        return {kind: values for kind, values in sorted(counts.items())}

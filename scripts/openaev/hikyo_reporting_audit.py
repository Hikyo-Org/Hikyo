"""Assess native tenant audit evidence for fresh disposable Hikyo requests.

Pinned Hikyo source: 5c3f969f692268e5c0bc7f5b9acc860f3bff0f48.
Only resolvable grant denials are measured. Artifact admission refusals are
instance events without tenant scope and deliberately create no detection score.
No request bodies, credentials, actor identifiers or payloads leave this helper.
"""

from __future__ import annotations

import re
import uuid
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from urllib.parse import urlencode, urlsplit

DETECTION_OPERATIONS = {
    "wrong-environment": "delivery.fetch",
    "wrong-project": "delivery.fetch",
    "wrong-tenant": "delivery.fetch",
    "write-without-edit": "value.stage",
    "publish-without-publish": "value.publish",
    "remove-read-grant": "delivery.fetch",
    "cursor-after-read-removal": "delivery.fetch",
    "whack-cursor-outside-scope": "delivery.fetch",
    "whack-hostile-scalar-write": "value.stage",
    "whack-8k-scalar-write": "value.stage",
    "whack-publish-bypass-attempt": "value.publish",
    "whack-after-read-removal": "delivery.fetch",
}
_SCOPE_ROUTE = re.compile(
    r"^/api/v1/orgs/([A-Za-z0-9_-]+)/projects/([A-Za-z0-9_-]+)"
    r"/environments/([A-Za-z0-9_-]+)/"
)


class NativeAuditError(Exception):
    """A sanitized reason code, never native response data."""


@dataclass(frozen=True)
class NativeAuditEvidence:
    inject_id: str
    case_name: str
    window_start: str
    window_end: str
    query_succeeded: bool
    actor_scope_verified: bool
    event_id: str | None = None
    event_type: str | None = None
    correlation_id: str | None = None


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise NativeAuditError("invalid_native_audit_response")
    return {k: v for k, v in value.items()}


def _integer(value: object) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise NativeAuditError("invalid_native_audit_pagination")
    return value


def _timestamp(value: object) -> datetime:
    if not isinstance(value, str):
        raise NativeAuditError("invalid_native_audit_timestamp")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            raise ValueError()
    except ValueError:
        raise NativeAuditError("invalid_native_audit_timestamp") from None
    return parsed.astimezone(UTC)


def _event_identifier(value: object) -> str:
    if not isinstance(value, str) or not value.startswith("evt_"):
        raise NativeAuditError("invalid_native_audit_event_identifier")
    try:
        if str(uuid.UUID(value[4:])) != value[4:]:
            raise ValueError()
    except ValueError:
        raise NativeAuditError("invalid_native_audit_event_identifier") from None
    return value


def _safe_correlation(value: object) -> str | None:
    # Correlation is optional caller-supplied text in the native schema. Export
    # only canonical UUIDs, so arbitrary request text cannot reach reports.
    if value is None or value == "":
        return None
    if not isinstance(value, str):
        raise NativeAuditError("invalid_native_audit_correlation")
    try:
        if str(uuid.UUID(value)) == value:
            return value
    except ValueError:
        pass
    return None


def _scope(case_name: str, target_path: str) -> tuple[str, str, str]:
    parts = urlsplit(target_path)
    match = _SCOPE_ROUTE.match(parts.path)
    if parts.scheme or parts.netloc or parts.fragment or match is None:
        raise NativeAuditError("invalid_native_audit_target")
    suffix = parts.path[match.end() :]
    expected = {
        "value.stage": "values/LAB_CONFIG",
        "value.publish": "publish",
        "delivery.fetch": "delivery",
    }[DETECTION_OPERATIONS[case_name]]
    if suffix != expected:
        raise NativeAuditError("native_audit_operation_target_mismatch")
    return match.group(1), match.group(2), match.group(3)


def assess_native_audit(
    call: Callable[[str, str], dict[str, object]],
    *,
    inject_id: str,
    case_name: str,
    target_path: str,
    actor_id: str,
    window_start: str,
    window_end: str,
    max_pages: int = 20,
) -> NativeAuditEvidence:
    """Query every bounded page, then accept exactly one attributable denial.

    A completed empty assessment is a detection failure, not missing evidence.
    Query errors, invalid pages, ambiguous events and truncated paging raise.
    Callers must omit a metric when assessment cannot complete successfully.
    """
    if case_name not in DETECTION_OPERATIONS:
        raise NativeAuditError("native_audit_case_not_measured")
    if not actor_id or not isinstance(max_pages, int) or isinstance(max_pages, bool):
        raise NativeAuditError("invalid_native_audit_assessment")
    if not 1 <= max_pages <= 100:
        raise NativeAuditError("invalid_native_audit_page_bound")
    start, end = _timestamp(window_start), _timestamp(window_end)
    if not 0 < (end - start).total_seconds() <= 600:
        raise NativeAuditError("invalid_native_audit_window")
    org, project, env = _scope(case_name, target_path)
    query = {
        "limit": "1000",
        "from": start.isoformat().replace("+00:00", "Z"),
        "to": end.isoformat().replace("+00:00", "Z"),
    }
    upper: int | None = None
    after = 0
    matches: list[dict[str, object]] = []
    seen: set[str] = set()
    exhausted = False
    for _ in range(max_pages):
        if upper is not None:
            query["to_seq"] = str(upper)
            query["after_seq"] = str(after)
        try:
            page = _object(call("GET", f"/api/v1/orgs/{org}/audit?{urlencode(query)}"))
        except NativeAuditError:
            raise
        except (RuntimeError, OSError, ValueError):
            raise NativeAuditError("native_audit_query_failed") from None
        items = page.get("items")
        if not isinstance(items, list):
            raise NativeAuditError("invalid_native_audit_response")
        if _integer(page.get("count")) != len(items) or len(items) > 1000:
            raise NativeAuditError("invalid_native_audit_pagination")
        ceiling = _integer(page.get("upper_seq"))
        cursor = _integer(page.get("next_after_seq"))
        exhausted_value = page.get("exhausted")
        if not isinstance(exhausted_value, bool):
            raise NativeAuditError("invalid_native_audit_pagination")
        exhausted = exhausted_value
        if upper is not None and ceiling != upper:
            raise NativeAuditError("native_audit_ceiling_changed")
        if cursor < after or cursor > ceiling or (not exhausted and cursor <= after):
            raise NativeAuditError("native_audit_pagination_did_not_advance")
        previous_seq = after
        for value in items:
            event = _object(value)
            seq = _integer(event.get("seq"))
            if not previous_seq < seq <= cursor:
                raise NativeAuditError("invalid_native_audit_event_order")
            previous_seq = seq
            identifier = _event_identifier(event.get("id"))
            if identifier in seen:
                raise NativeAuditError("duplicate_native_audit_event")
            seen.add(identifier)
            if (
                event.get("type") != "grant.denied"
                or event.get("outcome") != "denied"
                or event.get("actor_id") != actor_id
                or event.get("actor_class") != "machine"
                or event.get("org_id") != org
                or event.get("project_id") != project
                or event.get("env_id") != env
                or event.get("occurred_asserted") is not False
            ):
                continue
            payload = _object(event.get("payload"))
            if (
                payload.get("operation") != DETECTION_OPERATIONS[case_name]
                or payload.get("resolution") != "resolvable"
            ):
                continue
            if not start <= _timestamp(event.get("occurred_at")) <= end:
                continue
            if not start <= _timestamp(event.get("recorded_at")) <= end:
                continue
            _safe_correlation(event.get("correlation_id"))
            matches.append(event)
        upper, after = ceiling, cursor
        if exhausted:
            break
    if not exhausted:
        raise NativeAuditError("native_audit_page_bound_exceeded")
    if len(matches) > 1:
        raise NativeAuditError("ambiguous_native_audit_events")
    event_id: str | None = None
    correlation_id: str | None = None
    if matches:
        event_id = _event_identifier(matches[0]["id"])
        correlation_id = _safe_correlation(matches[0].get("correlation_id"))
    return NativeAuditEvidence(
        inject_id=inject_id,
        case_name=case_name,
        window_start=window_start,
        window_end=window_end,
        query_succeeded=True,
        actor_scope_verified=True,
        event_id=event_id,
        event_type="grant.denied" if event_id is not None else None,
        correlation_id=correlation_id,
    )

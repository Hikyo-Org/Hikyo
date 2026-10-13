"""Finite synthetic cases for the isolated Hikyo lab, pinned to nightly66.

No networking, workload loops, raw paths, header overrides or live mutations.
The root runner supplies the fresh read-only fixture and its legitimate full
delivery cursor. Credential roles ``long-garbage`` and ``tampered`` are resolved
by that runner; each must retain the recorder's ``hik_1_wl_`` grammar prefix.

Pinned source: 5c3f969f692268e5c0bc7f5b9acc860f3bff0f48.
Contract validation precedes handler admission (internal/server/api.go790-888).
Delivery cursor maxLength128: api/openapi.yaml14374; unmatched admitted cursors
return full authorized delivery (internal/service/delivery.go448-476). Projection
is part of the cursor binding. SetValueRequest.value is a string; all mutation
schemas forbid unknown fields (openapi.yaml18303,20021,17395). Therefore invalid
envelopes are400, while valid unauthorized edits/publishes are404. Schema refusals
carry no authenticated tenant-denial detection claim.
"""

from __future__ import annotations

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from urllib.parse import parse_qsl, urlencode, urlsplit

SOURCE_REVISION = "5c3f969f692268e5c0bc7f5b9acc860f3bff0f48"
MAX_PATH_BYTES = 16_384
MAX_CASE_BYTES = 86_384
MAX_BODY_BYTES = 70_000
MAX_CURSOR_CHARS = 8_192
_ID = re.compile(r"[A-Za-z0-9_-]{1,128}")
_ORG = re.compile(r"/api/v1/orgs/[A-Za-z0-9_-]{1,128}")
_PROJECT = re.compile(
    r"/api/v1/orgs/[A-Za-z0-9_-]{1,128}/projects/[A-Za-z0-9_-]{1,128}"
)
_ENV = re.compile(
    r"/api/v1/orgs/[A-Za-z0-9_-]{1,128}/projects/[A-Za-z0-9_-]{1,128}/environments/[A-Za-z0-9_-]{1,128}"
)
_NAME = re.compile(r"whack-[a-z0-9][a-z0-9-]{0,65}")
DeliveryAssertion = Callable[[dict[str, object], str, dict[str, object]], None]


class AdversarialAssertionError(RuntimeError):
    """Sanitized failure code without request/response bodies or canary values."""


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise AdversarialAssertionError("invalid_object")
    return dict(value)


def _string(value: dict[str, object], field: str) -> str:
    result = value.get(field)
    if not isinstance(result, str) or not result:
        raise AdversarialAssertionError("invalid_fixture_field")
    return result


@dataclass(frozen=True)
class AdversarialCase:
    name: str
    method: str
    path: str
    role: str
    status: int
    mode: str
    body: dict[str, object] | None = None
    observer: str | None = None
    detection_operation: str | None = None

    def __post_init__(self) -> None:
        if not _NAME.fullmatch(self.name) or self.method not in {"GET", "POST", "PUT"}:
            raise AdversarialAssertionError("invalid_case_name_or_method")
        if self.role not in {"reader", "revealer", "long-garbage", "tampered"}:
            raise AdversarialAssertionError("invalid_credential_role")
        modes = {
            200: {"presence", "config-only"},
            400: {"bad_request"},
            401: {"unauthenticated"},
            404: {"not_found"},
        }
        if self.mode not in modes.get(self.status, set()):
            raise AdversarialAssertionError("invalid_expected_outcome")
        if self.observer not in {None, "published", "grants"}:
            raise AdversarialAssertionError("invalid_observer")
        if self.method != "GET" and self.observer is None:
            raise AdversarialAssertionError("mutation_requires_observer")
        if self.status != 404 and self.detection_operation is not None:
            raise AdversarialAssertionError("non_tenant_refusal_detection_claim")
        if self.detection_operation not in {
            None,
            "delivery.fetch",
            "value.stage",
            "value.publish",
        }:
            raise AdversarialAssertionError("invalid_detection_operation")
        _validate_path(self.path)
        if self.method == "GET" and self.body is not None:
            raise AdversarialAssertionError("get_body_rejected")
        if self.method != "GET" and self.body is None:
            raise AdversarialAssertionError("mutation_body_required")
        try:
            body = (
                json.dumps(self.body, allow_nan=False).encode()
                if self.body is not None
                else b""
            )
        except (ValueError, TypeError):
            raise AdversarialAssertionError("invalid_json_body") from None
        if (
            len(body) > MAX_BODY_BYTES
            or len(body) + len(self.path.encode()) > MAX_CASE_BYTES
        ):
            raise AdversarialAssertionError("payload_bound_exceeded")


def _validate_path(path: str) -> None:
    parsed = urlsplit(path)
    if (
        len(path.encode()) > MAX_PATH_BYTES
        or parsed.scheme
        or parsed.netloc
        or parsed.fragment
        or any(ord(char) < 33 or ord(char) > 126 for char in path)
    ):
        raise AdversarialAssertionError("unsafe_target")
    segments = parsed.path.split("/")[1:]
    if not parsed.path.startswith("/api/v1/orgs/") or any(
        not _ID.fullmatch(part) for part in segments
    ):
        raise AdversarialAssertionError("unsafe_target")
    try:
        query = parse_qsl(parsed.query, strict_parsing=True, keep_blank_values=True)
    except ValueError:
        raise AdversarialAssertionError("invalid_query") from None
    seen: set[str] = set()
    for field, value in query:
        if field not in {"cursor", "projection"} or field in seen or not value:
            raise AdversarialAssertionError("invalid_query")
        if any(ord(char) < 32 or ord(char) == 127 for char in value):
            raise AdversarialAssertionError("invalid_query")
        if field == "cursor" and len(value) > MAX_CURSOR_CHARS:
            raise AdversarialAssertionError("cursor_bound_exceeded")
        if field == "projection" and value not in {"full", "config-only"}:
            raise AdversarialAssertionError("invalid_projection")
        seen.add(field)
    if query and not parsed.path.endswith("/delivery"):
        raise AdversarialAssertionError("query_requires_delivery")


def _fixture_paths(fixture: dict[str, object]) -> tuple[str, str, str, str, str]:
    env, base, org = (_string(fixture, field) for field in ("env", "base", "org_path"))
    if (
        not _ENV.fullmatch(env)
        or not _PROJECT.fullmatch(base)
        or not _ORG.fullmatch(org)
        or not env.startswith(base + "/environments/")
        or not base.startswith(org + "/projects/")
    ):
        raise AdversarialAssertionError("fixture_scope_mismatch")
    outside = fixture.get("outside_envs")
    if (
        not isinstance(outside, list)
        or len(outside) != 3
        or not all(isinstance(item, str) and _ENV.fullmatch(item) for item in outside)
    ):
        raise AdversarialAssertionError("invalid_outside_scope")
    if env in outside or len(set(outside)) != 3:
        raise AdversarialAssertionError("outside_scope_must_differ")
    # Parse each string again for typing rather than treating unknown JSON as a path.
    outside_env = outside[0]
    if not isinstance(outside_env, str):
        raise AdversarialAssertionError("invalid_outside_scope")
    reader = _object(_object(fixture.get("accounts")).get("reader"))
    principal, version = (
        _string(reader, "principal"),
        _string(fixture, "publish_version"),
    )
    if not _ID.fullmatch(principal) or not _ID.fullmatch(version):
        raise AdversarialAssertionError("invalid_fixture_identifier")
    for field in ("config_value", "secret_value"):
        _string(fixture, field)
    return env, base, outside_env, principal, version


def build_cases(fixture: dict[str, object], own_cursor: str) -> list[AdversarialCase]:
    """Return twenty bounded cases; the supplied cursor must be freshly observed."""
    env, _base, outside, principal, version = _fixture_paths(fixture)
    if not isinstance(own_cursor, str) or not own_cursor or len(own_cursor) > 128:
        raise AdversarialAssertionError("fresh_cursor_required")
    if any(ord(char) < 32 or ord(char) == 127 for char in own_cursor):
        raise AdversarialAssertionError("fresh_cursor_required")
    delivery = env + "/delivery"
    result = []
    cursor_samples = (
        ("sql", "x' OR '1'='1'; SELECT current FROM delivery--"),
        ("xss", "<svg onload=alert('synthetic-lab')>"),
        ("traversal", "../../../../etc/passwd?projection=full"),
        ("unicode", "秘密-日本語-💾-\u202e-\u2066"),
    )
    for name, cursor in cursor_samples:
        result.append(
            AdversarialCase(
                "whack-cursor-" + name,
                "GET",
                delivery + "?" + urlencode({"cursor": cursor}),
                "reader",
                200,
                "presence",
            )
        )
    result.extend(
        [
            AdversarialCase(
                "whack-cursor-boundary-128",
                "GET",
                delivery + "?" + urlencode({"cursor": "B" * 128}),
                "reader",
                200,
                "presence",
            ),
            AdversarialCase(
                "whack-cursor-boundary-129",
                "GET",
                delivery + "?" + urlencode({"cursor": "B" * 129}),
                "reader",
                400,
                "bad_request",
            ),
            AdversarialCase(
                "whack-cursor-8k",
                "GET",
                delivery + "?" + urlencode({"cursor": "A" * 8192}),
                "reader",
                400,
                "bad_request",
            ),
            AdversarialCase(
                "whack-cursor-projection-binding",
                "GET",
                delivery
                + "?"
                + urlencode({"cursor": own_cursor, "projection": "config-only"}),
                "reader",
                200,
                "config-only",
            ),
            AdversarialCase(
                "whack-cursor-outside-scope",
                "GET",
                outside + "/delivery?" + urlencode({"cursor": own_cursor}),
                "reader",
                404,
                "not_found",
                detection_operation="delivery.fetch",
            ),
            AdversarialCase(
                "whack-long-workload-token",
                "GET",
                delivery,
                "long-garbage",
                401,
                "unauthenticated",
            ),
            AdversarialCase(
                "whack-tampered-workload-token",
                "GET",
                delivery,
                "tampered",
                401,
                "unauthenticated",
            ),
            AdversarialCase(
                "whack-hostile-scalar-write",
                "PUT",
                env + "/values/LAB_CONFIG",
                "reader",
                404,
                "not_found",
                {"value": "' OR 1=1; <script>synthetic-lab</script> ../../etc/passwd"},
                "published",
                "value.stage",
            ),
            AdversarialCase(
                "whack-8k-scalar-write",
                "PUT",
                env + "/values/LAB_CONFIG",
                "reader",
                404,
                "not_found",
                {"value": "S" * 8192},
                "published",
                "value.stage",
            ),
            AdversarialCase(
                "whack-oversized-scalar-write",
                "PUT",
                env + "/values/LAB_CONFIG",
                "reader",
                400,
                "bad_request",
                {"value": "S" * 65537},
                "published",
            ),
            AdversarialCase(
                "whack-structured-value",
                "PUT",
                env + "/values/LAB_CONFIG",
                "reader",
                400,
                "bad_request",
                {"value": {"$ne": None, "admin": True}},
                "published",
            ),
        ]
    )
    deep: dict[str, object] = {"attempt": "synthetic-lab"}
    for _ in range(16):
        deep = {"next": deep}
    result.extend(
        [
            AdversarialCase(
                "whack-deep-json-value",
                "PUT",
                env + "/values/LAB_CONFIG",
                "reader",
                400,
                "bad_request",
                {"value": deep},
                "published",
            ),
            AdversarialCase(
                "whack-value-mass-assignment",
                "PUT",
                env + "/values/LAB_CONFIG",
                "reader",
                400,
                "bad_request",
                {
                    "value": "synthetic-lab",
                    "classification": "secret",
                    "publish": True,
                    "scope": {"environment_id": outside.rsplit("/", 1)[1]},
                },
                "published",
            ),
            AdversarialCase(
                "whack-grant-mass-assignment",
                "POST",
                env + "/grants",
                "reader",
                400,
                "bad_request",
                {
                    "principal": principal,
                    "capability": "reveal",
                    "is_admin": True,
                    "scope": {"environment_id": outside.rsplit("/", 1)[1]},
                },
                "grants",
            ),
            AdversarialCase(
                "whack-publish-mass-assignment",
                "POST",
                env + "/publish",
                "reader",
                400,
                "bad_request",
                {"version_ids": [version], "force": True, "skip_authorization": True},
                "published",
            ),
            AdversarialCase(
                "whack-publish-bypass-attempt",
                "POST",
                env + "/publish",
                "reader",
                404,
                "not_found",
                {
                    "version_ids": [version],
                    "bypass": {"reason": "synthetic-lab bypass attempt"},
                },
                "published",
                "value.publish",
            ),
        ]
    )
    if len(result) != 20 or len({case.name for case in result}) != len(result):
        raise AdversarialAssertionError("case_plan_invalid")
    return result


def assert_response(
    fixture: dict[str, object],
    case: AdversarialCase,
    response: dict[str, object],
    delivery_assertion: DeliveryAssertion | None = None,
) -> None:
    """Assert recorded JSON; the root runner separately proves exact HTTP status."""
    response = _object(response)
    config, secret = (
        _string(fixture, field) for field in ("config_value", "secret_value")
    )
    raw = json.dumps(response, allow_nan=False)
    if secret in raw:
        raise AdversarialAssertionError("secret_plaintext_disclosed")
    if case.mode in {"bad_request", "not_found", "unauthenticated"}:
        if _object(response.get("error")).get("code") != case.mode:
            raise AdversarialAssertionError("incorrect_domain_error")
        if "keys" in response or "secrets" in response or config in raw:
            raise AdversarialAssertionError("refusal_disclosed_material")
        if delivery_assertion is not None and case.mode != "bad_request":
            delivery_assertion(fixture, case.mode, response)
        return
    if response.get("current") is not False:
        raise AdversarialAssertionError("hostile_cursor_forged_current")
    if case.mode == "config-only" and "LAB_SECRET" in raw:
        raise AdversarialAssertionError("projection_disclosed_secret_name")
    if delivery_assertion is not None:
        delivery_assertion(fixture, case.mode, response)
        return
    keys = response.get("keys")
    if not isinstance(keys, list):
        raise AdversarialAssertionError("delivery_missing_keys")
    entries = [_object(value) for value in keys]
    by_name = {_string(value, "name"): value for value in entries}
    expected = (
        {"LAB_CONFIG"} if case.mode == "config-only" else {"LAB_CONFIG", "LAB_SECRET"}
    )
    if set(by_name) != expected or len(by_name) != len(entries):
        raise AdversarialAssertionError("delivery_key_set_mismatch")
    cfg = by_name["LAB_CONFIG"]
    if (
        cfg.get("classification") != "config"
        or cfg.get("presence") != "set"
        or cfg.get("value") != config
    ):
        raise AdversarialAssertionError("config_delivery_mismatch")
    if case.mode == "config-only":
        if "LAB_SECRET" in raw:
            raise AdversarialAssertionError("projection_disclosed_secret_name")
        return
    sec = by_name["LAB_SECRET"]
    if (
        sec.get("classification") != "secret"
        or sec.get("presence") != "set"
        or sec.get("value") is not None
    ):
        raise AdversarialAssertionError("presence_delivery_disclosed_secret")

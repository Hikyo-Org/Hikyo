"""Record bounded, synthetic-only Hikyo security cases in OpenAEV.

OpenAEV stores submitted workload tokens and response bodies. Only disposable
lab credentials belong here. The upstream injector follows response redirects
and omits HTTP status from its callback; independent direct requests must reject
redirects and prove status. This module validates fixed lab targets and asserts
the actual recorded JSON response, rather than treating EXECUTED as security proof.
"""

from __future__ import annotations

import importlib.util
import json
import re
import time
import uuid
from collections.abc import Callable
from pathlib import Path
from urllib.parse import parse_qsl, urlencode, urlsplit

_pilot_path = Path(__file__).with_name("run-http-pilot.py")
_spec = importlib.util.spec_from_file_location(
    "openaev_http_pilot_transport", _pilot_path
)
if _spec is None or _spec.loader is None:
    raise RuntimeError("openaev_pilot_transport_unavailable")
_pilot = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_pilot)

HIKYO_ORIGIN = "https://hikyo.hikyo-lab.svc.cluster.local:8443"
CONTRACTS = {
    "GET": "611f223d-0e95-4f5b-ad89-a09ec2be50ae",
    "POST": "5948c96c-4064-4c0d-b079-51ec33f31b91",
    "PUT": "bb503f7c-1f17-49e1-ac31-f4c2e99fd704",
}
FORM_POST_CONTRACT = "a4794081-ccb5-41ef-9855-304ad5fdf4a9"
_CASE_NAME = re.compile(r"[a-z0-9][a-z0-9_-]{0,79}")
_ROUTE_SEGMENT = re.compile(r"[A-Za-z0-9_-]+")
_WORKLOAD_TOKEN = re.compile(r"hik_1_wl_[A-Za-z0-9_-]+")
_EXECUTION_TIMEOUT = 300
_PLATFORM_REQUEST_TIMEOUT = 15
_POLL_SECONDS = 3


class OpenAEVRecordingError(Exception):
    """Sanitized failure code. Never includes credentials, cursors or bodies."""

    def __init__(self, code: str, inject_id: str | None = None):
        super().__init__(code)
        self.inject_id = inject_id


def _target(path: str) -> str:
    if len(path) > 16_384 or any(ord(char) < 33 or ord(char) > 126 for char in path):
        raise OpenAEVRecordingError("invalid_lab_path")
    parsed = urlsplit(path)
    if parsed.scheme or parsed.netloc or parsed.fragment:
        raise OpenAEVRecordingError("absolute_or_fragment_target_rejected")
    if not parsed.path.startswith("/api/v1/orgs/"):
        raise OpenAEVRecordingError("target_outside_org_api")
    segments = parsed.path.split("/")[1:]
    if any(not _ROUTE_SEGMENT.fullmatch(segment) for segment in segments):
        raise OpenAEVRecordingError("encoded_or_traversal_target_rejected")
    try:
        query = parse_qsl(parsed.query, keep_blank_values=True, strict_parsing=True)
    except ValueError:
        raise OpenAEVRecordingError("invalid_lab_query") from None
    seen: set[str] = set()
    for key, value in query:
        if key not in {"cursor", "projection"} or key in seen or not value:
            raise OpenAEVRecordingError("unexpected_lab_query")
        if any(ord(char) < 32 or ord(char) == 127 for char in value):
            raise OpenAEVRecordingError("invalid_lab_query_value")
        if key == "projection" and value not in {"full", "config-only"}:
            raise OpenAEVRecordingError("invalid_delivery_projection")
        seen.add(key)
    if query and not parsed.path.endswith("/delivery"):
        raise OpenAEVRecordingError("query_requires_delivery_route")
    return HIKYO_ORIGIN + path


def _payload(
    name: str, method: str, path: str, token: str | None, body: dict[str, object] | None
) -> dict[str, object]:
    if not _CASE_NAME.fullmatch(name) or name.startswith("hik_"):
        raise OpenAEVRecordingError("invalid_case_name")
    if method not in CONTRACTS:
        raise OpenAEVRecordingError("unsupported_http_method")
    if token is not None and not _WORKLOAD_TOKEN.fullmatch(token):
        raise OpenAEVRecordingError("only_workload_credentials_allowed")
    if method == "GET" and body is not None:
        raise OpenAEVRecordingError("get_body_rejected")
    if method == "PUT" and body is None:
        raise OpenAEVRecordingError("put_json_body_required")
    headers: list[dict[str, str]] = [{"key": "Accept", "value": "application/json"}]
    if token is not None:
        headers.append({"key": "Authorization", "value": "Bearer " + token})
    content: dict[str, object] = {
        "uri": _target(path),
        "basicAuth": False,
        "headers": headers,
    }
    contract_id = CONTRACTS[method]
    if method == "POST" and body is None:
        # Raw POST's mandatory body fails platform readiness when blank. The
        # form contract permits empty parts; upstream converts [] to data={},
        # which pinned requests emits as a zero-byte POST with body=None.
        contract_id = FORM_POST_CONTRACT
        content["parts"] = []
    elif method != "GET":
        headers.append({"key": "Content-Type", "value": "application/json"})
        try:
            content["body"] = json.dumps(body)
        except (ValueError, TypeError):
            raise OpenAEVRecordingError("invalid_json_request_body") from None
    return {
        "inject_title": "Hikyo isolated lab security: " + name,
        "inject_description": "Synthetic lab credentials; independently asserted security outcome",
        "inject_injector_contract": contract_id,
        "inject_content": content,
        "inject_all_teams": False,
        "inject_teams": [],
        "inject_assets": [],
        "inject_asset_groups": [],
        "inject_documents": [],
        "inject_tags": [],
    }


class OpenAEVRecorder:
    def __init__(
        self,
        platform_url: str,
        platform_token_file: Path,
        ca_file: Path,
        reporting_config: Callable[[str, dict[str, object]], dict[str, object]]
        | None = None,
    ):
        try:
            self._api = _pilot.PlatformAPI(
                platform_url, _pilot.private_file(platform_token_file).strip(), ca_file
            )
        except (OSError, ValueError, TypeError, _pilot.PilotError):
            raise OpenAEVRecordingError("platform_initialization_failed") from None
        self._registered_contracts: set[str] = set()
        self._active_inject_id: str | None = None
        self._reporting_config = reporting_config

    def _configure_reporting(
        self, name: str, payload: dict[str, object]
    ) -> dict[str, object]:
        if self._reporting_config is None:
            return payload
        # Reporting may attach an asset and expectations, never change the
        # already validated target, workload credential or HTTP operation.
        baseline = json.dumps(payload, sort_keys=True)
        configured = self._reporting_config(name, payload)
        if not isinstance(configured, dict):
            raise OpenAEVRecordingError("invalid_reporting_payload")
        restored = dict(configured)
        content = configured.get("inject_content")
        assets = configured.get("inject_assets")
        if not isinstance(content, dict) or not isinstance(assets, list):
            raise OpenAEVRecordingError("invalid_reporting_payload")
        if len(assets) != 1 or not isinstance(assets[0], str):
            raise OpenAEVRecordingError("reporting_requires_one_lab_asset")
        try:
            if str(uuid.UUID(assets[0])) != assets[0]:
                raise ValueError("noncanonical identifier")
        except (ValueError, TypeError):
            raise OpenAEVRecordingError("invalid_reporting_asset") from None
        restored_content = dict(content)
        restored_content.pop("expectations", None)
        restored["inject_content"] = restored_content
        restored["inject_assets"] = []
        if json.dumps(restored, sort_keys=True) != baseline:
            raise OpenAEVRecordingError("reporting_changed_security_request")
        return configured

    def verify_missing_body_failure(
        self, inject_id: str, name: str
    ) -> dict[str, object]:
        """Read-only proof that blank mandatory body blocked execution dispatch.

        Recovery must require the exact platform-generated failure, with one
        COMPLETE/ERROR trace and no execution trace. Other errors cannot authorize
        skipping or replaying a recorded case.
        """
        try:
            if str(uuid.UUID(inject_id)) != inject_id:
                raise ValueError("noncanonical identifier")
        except (ValueError, TypeError, AttributeError):
            raise OpenAEVRecordingError("invalid_failed_inject_id") from None
        if not _CASE_NAME.fullmatch(name) or name.startswith("hik_"):
            raise OpenAEVRecordingError("invalid_case_name", inject_id)
        try:
            status = self._api.request(
                "GET", "injects/status-all-traces?" + urlencode({"injectId": inject_id})
            )
        except (OSError, ValueError, TypeError, _pilot.PilotError):
            raise OpenAEVRecordingError(
                "failure_verification_unavailable", inject_id
            ) from None
        traces = status.get("status_main_traces")
        expected = (
            "The inject is not ready to be executed (injectId="
            + inject_id
            + ", title=Hikyo isolated lab security: "
            + name
            + ", missing mandatory fields: body:MANDATORY_CONTENT)"
        )
        if (
            status.get("status_name") != "ERROR"
            or not isinstance(traces, list)
            or len(traces) != 1
            or not isinstance(traces[0], dict)
            or traces[0].get("execution_action") != "COMPLETE"
            or traces[0].get("execution_status") != "ERROR"
            or traces[0].get("execution_message") != expected
        ):
            raise OpenAEVRecordingError("resume_failure_not_verified", inject_id)
        return {
            "inject_id": inject_id,
            "failure_verified": True,
            "failure_code": "blank_mandatory_body_before_dispatch",
        }

    def record_case(
        self,
        name: str,
        method: str,
        path: str,
        token: str | None,
        body: dict[str, object] | None,
        assert_response: Callable[[dict[str, object]], None],
    ) -> dict[str, object]:
        """Record one case; the callback returns None only when assertions pass.

        Domain assertion callbacks raise AssertionError or RuntimeError on failure.
        ValueError, TypeError and KeyError from validating the body are sanitized
        as assertion failures too. Complete-case HTTP status and observer checks
        remain the caller's responsibility.
        """
        self._active_inject_id = None
        payload = _payload(name, method, path, token, body)
        payload = self._configure_reporting(name, payload)
        if not callable(assert_response):
            raise OpenAEVRecordingError("response_assertion_required")
        try:
            return self._record(name, payload, assert_response)
        except OpenAEVRecordingError as exc:
            exc.inject_id = self._active_inject_id
            raise
        except (OSError, ValueError, TypeError, _pilot.PilotError):
            raise OpenAEVRecordingError(
                "platform_recording_failed", self._active_inject_id
            ) from None

    def _record(
        self,
        name: str,
        payload: dict[str, object],
        assert_response: Callable[[dict[str, object]], None],
    ) -> dict[str, object]:
        contract_id = _pilot.string_field(payload, "inject_injector_contract")
        if contract_id not in self._registered_contracts:
            contract = self._api.request("GET", "injector_contracts/" + contract_id)
            if contract.get("injector_contract_id") != contract_id:
                raise OpenAEVRecordingError("http_contract_not_registered")
            self._registered_contracts.add(contract_id)
        created = self._api.request("POST", "atomic-testings", payload)
        inject_id = _pilot.string_field(created, "inject_id")
        try:
            uuid.UUID(inject_id)
        except ValueError:
            raise OpenAEVRecordingError("invalid_created_inject_id") from None
        self._active_inject_id = inject_id
        if self._reporting_config is not None:
            # CREATE fills an empty expectation list with injector defaults.
            # UPDATE preserves explicit [], keeping permitted control requests
            # out of the attack metrics. Normalize before any execution.
            self._api.request("PUT", f"atomic-testings/{inject_id}", payload)
            configured = self._api.request("GET", f"atomic-testings/{inject_id}")
            configured_content = configured.get("inject_content")
            wanted_content = payload.get("inject_content")
            if (
                not isinstance(configured_content, dict)
                or not isinstance(wanted_content, dict)
                or configured_content != wanted_content
            ):
                raise OpenAEVRecordingError("reporting_configuration_not_persisted")
        self._api.request("POST", f"atomic-testings/{inject_id}/launch", {})
        deadline = time.monotonic() + _EXECUTION_TIMEOUT
        while time.monotonic() + _PLATFORM_REQUEST_TIMEOUT < deadline:
            status = self._api.request(
                "GET", "injects/status-all-traces?" + urlencode({"injectId": inject_id})
            )
            try:
                raw = _pilot.recorded_body(status)
            except (_pilot.PilotError, ValueError, TypeError):
                raise OpenAEVRecordingError("injector_execution_failed") from None
            if raw is not None:
                try:
                    response = _pilot.object_value(
                        json.loads(raw), "recorded_response_not_object"
                    )
                except (ValueError, TypeError, _pilot.PilotError):
                    raise OpenAEVRecordingError(
                        "invalid_recorded_json_response"
                    ) from None
                try:
                    assert_response(response)
                except OpenAEVRecordingError:
                    raise
                except (AssertionError, ValueError, TypeError, KeyError, RuntimeError):
                    raise OpenAEVRecordingError(
                        "recorded_response_assertion_failed"
                    ) from None
                return {
                    "name": name,
                    "inject_id": inject_id,
                    "execution_recorded": True,
                    "domain_assertion_passed": True,
                    "http_status_recorded": False,
                }
            time.sleep(_POLL_SECONDS)
        raise OpenAEVRecordingError("execution_timeout_300_seconds")

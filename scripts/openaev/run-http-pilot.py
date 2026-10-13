#!/usr/bin/env python3
"""Record lab-only HTTP atomic tests and assert Hikyo domain outcomes.

API contract: OpenAEV 3.261005.0, source 430c0283e63573c2467d058b735aaf49b17166fa.
The upstream HTTP injector does not record HTTP status codes. This runner checks
the recorded response JSON; the direct Hikyo pilot separately proves HTTP status.
Only synthetic credentials/canaries may be submitted: OpenAEV stores the inject
content and response. Standard library only. No credentials or bodies are logged.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import re
import ssl
import stat
import sys
import time
import uuid
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlsplit
from urllib.request import (
    HTTPRedirectHandler,
    HTTPSHandler,
    ProxyHandler,
    Request,
    build_opener,
)

HTTP_GET_CONTRACT = "611f223d-0e95-4f5b-ad89-a09ec2be50ae"
HIKYO_ORIGIN = "https://hikyo.hikyo-lab.svc.cluster.local:8443"
DELIVERY_PATH = re.compile(
    r"/api/v1/orgs/[A-Za-z0-9_-]+/projects/[A-Za-z0-9_-]+"
    r"/environments/[A-Za-z0-9_-]+/delivery"
)
MAX_RESPONSE_BYTES = 1_048_576


class PilotError(Exception):
    """A sanitized failure reason safe for stdout/stderr."""


def object_value(value: object, reason: str) -> dict[str, object]:
    if not isinstance(value, dict) or not all(isinstance(k, str) for k in value):
        raise PilotError(reason)
    return {k: v for k, v in value.items()}


def string_field(value: dict[str, object], key: str) -> str:
    field = value.get(key)
    if not isinstance(field, str) or not field:
        raise PilotError(f"invalid_field_{key}")
    return field


def private_file(path: Path) -> str:
    """Read only caller-owned, non-symlink, owner-only regular files."""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "r", encoding="utf-8") as handle:
        info = os.fstat(handle.fileno())
        if (
            not stat.S_ISREG(info.st_mode)
            or info.st_uid != os.getuid()
            or stat.S_IMODE(info.st_mode) & 0o077
        ):
            raise PilotError("credential_file_requires_owner_only_permissions")
        return handle.read(MAX_RESPONSE_BYTES + 1)


def load_fixture(path: Path) -> dict[str, object]:
    raw = private_file(path)
    if len(raw) > MAX_RESPONSE_BYTES:
        raise PilotError("fixture_too_large")
    fixture = object_value(json.loads(raw), "invalid_fixture_object")
    if fixture.get("format") != "hikyo-openaev-pilot/v1":
        raise PilotError("unsupported_fixture_format")
    if string_field(fixture, "internal_instance_url") != HIKYO_ORIGIN:
        raise PilotError("hikyo_target_must_be_isolated_lab_service")
    for name in ("staging_delivery_path", "outside_scope_delivery_path"):
        if not DELIVERY_PATH.fullmatch(string_field(fixture, name)):
            raise PilotError("hikyo_target_must_be_delivery_route")
    if fixture["staging_delivery_path"] == fixture["outside_scope_delivery_path"]:
        raise PilotError("outside_scope_route_must_differ")
    for name in ("expected_key", "expected_value", "token_file"):
        string_field(fixture, name)
    if not Path(string_field(fixture, "token_file")).is_absolute():
        raise PilotError("token_file_must_be_absolute")
    return fixture


def validate_platform_url(url: str) -> str:
    parsed = urlsplit(url)
    if (
        parsed.scheme != "https"
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
    ):
        raise PilotError("platform_requires_https_lab_origin")
    if (
        parsed.hostname
        in (
            "openaev.openaev-lab.svc",
            "openaev.openaev-lab.svc.cluster.local",
        )
        and parsed.port == 8080
    ):
        return url.rstrip("/")
    try:
        address = ipaddress.ip_address(parsed.hostname or "")
    except ValueError as exc:
        raise PilotError("platform_requires_private_lan_tunnel_or_lab_service") from exc
    private_networks = (
        ipaddress.ip_network("10.0.0.0/8"),
        ipaddress.ip_network("172.16.0.0/12"),
        ipaddress.ip_network("192.168.0.0/16"),
    )
    if (
        not (address == ipaddress.ip_address("127.0.0.1") or any(address in network for network in private_networks))
        or parsed.port != 18080
    ):
        raise PilotError("platform_requires_private_lan_tunnel_port_18080")
    return url.rstrip("/")


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise PilotError("platform_redirect_rejected")


class PlatformAPI:
    def __init__(self, origin: str, token: str, ca_file: Path):
        self.origin = validate_platform_url(origin)
        if not token or "\r" in token or "\n" in token:
            raise PilotError("invalid_platform_token")
        self.token = token
        self.opener = build_opener(
            ProxyHandler({}),
            HTTPSHandler(context=ssl.create_default_context(cafile=str(ca_file))),
            NoRedirect(),
        )

    def request(
        self, method: str, path: str, payload: dict[str, object] | None = None
    ) -> dict[str, object]:
        body = json.dumps(payload).encode() if payload is not None else None
        request = Request(
            self.origin + "/api/" + path,
            data=body,
            method=method,
            headers={
                "Authorization": "Bearer " + self.token,
                "Content-Type": "application/json",
                "Accept": "application/json",
            },
        )
        try:
            with self.opener.open(request, timeout=15) as response:
                raw = response.read(MAX_RESPONSE_BYTES + 1)
        except HTTPError as exc:
            raise PilotError(f"platform_http_{exc.code}") from exc
        except (URLError, TimeoutError) as exc:
            raise PilotError("platform_connection_failed") from exc
        if len(raw) > MAX_RESPONSE_BYTES:
            raise PilotError("platform_response_too_large")
        return object_value(json.loads(raw), "invalid_platform_response_object")


def inject_payload(
    fixture: dict[str, object], case: str, credential: str
) -> dict[str, object]:
    path_name = (
        "outside_scope_delivery_path"
        if case == "outside-scope"
        else "staging_delivery_path"
    )
    if not credential or "\r" in credential or "\n" in credential:
        raise PilotError("invalid_hikyo_token")
    return {
        "inject_title": f"Hikyo isolated lab: {case}",
        "inject_description": "Synthetic-only lab credential and canary validation",
        "inject_injector_contract": HTTP_GET_CONTRACT,
        "inject_content": {
            "uri": HIKYO_ORIGIN + string_field(fixture, path_name),
            "basicAuth": False,
            "headers": [{"key": "Authorization", "value": "Bearer " + credential}],
        },
        "inject_all_teams": False,
        "inject_teams": [],
        "inject_assets": [],
        "inject_asset_groups": [],
        "inject_documents": [],
        "inject_tags": [],
    }


def recorded_body(status: dict[str, object]) -> str | None:
    traces = status.get("status_main_traces")
    if traces is None:
        return None
    if not isinstance(traces, list):
        raise PilotError("invalid_execution_traces")
    for trace_value in reversed(traces):
        trace = object_value(trace_value, "invalid_execution_trace")
        if trace.get("execution_action") != "COMPLETE":
            continue
        if trace.get("execution_status") != "EXECUTED":
            raise PilotError("injector_execution_failed")
        return string_field(trace, "execution_message")
    return None


def assert_domain(fixture: dict[str, object], case: str, body: str) -> None:
    response = object_value(json.loads(body), "invalid_hikyo_json_response")
    key = string_field(fixture, "expected_key")
    expected = string_field(fixture, "expected_value")
    if case == "staging":
        keys = response.get("keys")
        if not isinstance(keys, list) or response.get("current") is not False:
            raise PilotError("staging_delivery_missing_keys")
        delivered = [object_value(item, "invalid_delivered_key") for item in keys]
        matches = [item for item in delivered if item.get("name") == key]
        if (
            len(matches) != 1
            or matches[0].get("value") != expected
            or matches[0].get("classification") != "config"
            or matches[0].get("presence") != "set"
        ):
            raise PilotError("staging_canary_mismatch")
        return
    if key in body or expected in body or "keys" in response or "secrets" in response:
        raise PilotError("denied_request_disclosed_secret")
    error = object_value(response.get("error"), "denied_request_missing_domain_error")
    expected_code = "not_found" if case == "outside-scope" else "unauthenticated"
    if error.get("code") != expected_code:
        raise PilotError("denied_request_wrong_domain_error")


def run_case(args: argparse.Namespace) -> dict[str, object]:
    fixture = load_fixture(args.hikyo_pilot_config_file)
    token = private_file(Path(string_field(fixture, "token_file"))).strip()
    api = PlatformAPI(
        args.platform_url, private_file(args.platform_token_file).strip(), args.ca_file
    )
    contract = api.request("GET", "injector_contracts/" + HTTP_GET_CONTRACT)
    if contract.get("injector_contract_id") != HTTP_GET_CONTRACT:
        raise PilotError("http_injector_contract_not_registered")
    created = api.request(
        "POST", "atomic-testings", inject_payload(fixture, args.case, token)
    )
    inject_id = string_field(created, "inject_id")
    try:
        uuid.UUID(inject_id)
    except ValueError as exc:
        raise PilotError("invalid_created_inject_id") from exc
    api.request("POST", f"atomic-testings/{inject_id}/launch", {})
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        status = api.request(
            "GET", "injects/status-all-traces?" + urlencode({"injectId": inject_id})
        )
        body = recorded_body(status)
        if body is not None:
            assert_domain(fixture, args.case, body)
            return {
                "case": args.case,
                "passed": True,
                "inject_id": inject_id,
                "execution_recorded": True,
                "domain_assertion_passed": True,
                "http_status_recorded": False,
            }
        time.sleep(3)
    raise PilotError("execution_timeout_300_seconds")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform-url", required=True)
    parser.add_argument("--platform-token-file", required=True, type=Path)
    parser.add_argument("--ca-file", required=True, type=Path)
    parser.add_argument("--hikyo-pilot-config-file", required=True, type=Path)
    parser.add_argument(
        "--case", required=True, choices=("staging", "outside-scope", "revoked")
    )
    args = parser.parse_args()
    try:
        result = run_case(args)
    except PilotError as exc:
        print(json.dumps({"case": args.case, "passed": False, "reason": str(exc)}))
        return 1
    except (OSError, ValueError, TypeError):
        print(
            json.dumps(
                {
                    "case": args.case,
                    "passed": False,
                    "reason": "invalid_local_or_api_data",
                }
            )
        )
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    sys.exit(main())

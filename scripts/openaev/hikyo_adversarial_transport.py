"""Deadline-bound workload requests to the fixed loopback lab, without redirects."""

from __future__ import annotations

import http.client
import json
import re
import socket
import ssl
import threading
import time
from dataclasses import dataclass
from pathlib import Path

import openaev_security


class TransportError(RuntimeError):
    """Sanitized code only."""


@dataclass(frozen=True)
class Response:
    status: int
    body: dict[str, object]
    elapsed_ms: float
    response_bytes: int
    retry_after: int | None


class _BodyConnection(http.client.HTTPSConnection):
    """Keep a one-shot socket alive until a Connection:close body is consumed.

    getresponse normally calls close immediately for will-close responses.
    Delaying that call keeps timeout/watchdog access to the body's socket. Every
    request calls finish in finally, including response/handshake failures.
    """

    def close(self) -> None:
        pass

    def finish(self) -> None:
        super().close()


class WorkloadTransport:
    def __init__(self, ca_file: Path):
        self.context = ssl.create_default_context(cafile=str(ca_file))
        self.context.minimum_version = ssl.TLSVersion.TLSv1_2

    def request(
        self, method: str, path: str, token: str,
        body: dict[str, object] | None = None,
        *, timeout_s: float = 3.0, deadline: float | None = None,
    ) -> Response:
        # Reuse the recorded request's origin/path/credential controls.
        openaev_security._payload("transport-check", method, path, token, body)
        if not 0 < timeout_s <= 3:
            raise TransportError("invalid_request_timeout")
        start = time.monotonic()
        end = min(start + timeout_s, deadline) if deadline is not None else start + timeout_s
        if end <= start:
            raise TransportError("request_deadline_expired")
        headers = {"Accept": "application/json", "Authorization": "Bearer " + token}
        encoded = json.dumps(body).encode() if body is not None else None
        if encoded is not None:
            if len(encoded) > 70_000:
                raise TransportError("request_body_too_large")
            headers["Content-Type"] = "application/json"
        connection = _BodyConnection(
            "127.0.0.1", 18081, timeout=end - start, context=self.context
        )
        deadline_timer: threading.Timer | None = None
        secured: ssl.SSLSocket | None = None
        response: http.client.HTTPResponse | None = None
        try:
            # TCP connect consumes the same absolute budget as TLS, headers and
            # body. A watchdog shuts down the connected socket at the deadline,
            # including a slowly trickled HTTP header/body response.
            raw_socket = socket.create_connection(("127.0.0.1", 18081), timeout=end - time.monotonic())
            try:
                secured = self.context.wrap_socket(raw_socket, server_hostname="127.0.0.1", do_handshake_on_connect=False)
            except BaseException:
                raw_socket.close()
                raise
            connection.sock = secured

            def expire() -> None:
                if secured is not None:
                    try:
                        secured.shutdown(socket.SHUT_RDWR)
                    except OSError:
                        pass

            remaining = end - time.monotonic()
            if remaining <= 0:
                raise TimeoutError()
            deadline_timer = threading.Timer(remaining, expire)
            deadline_timer.daemon = True
            deadline_timer.start()
            secured.settimeout(remaining)
            secured.do_handshake()
            secured.settimeout(max(0.001, end - time.monotonic()))
            connection.putrequest(method, path, skip_accept_encoding=True)
            for key, value in headers.items():
                connection.putheader(key, value)
            if encoded is not None or method == "POST":
                connection.putheader("Content-Length", str(len(encoded or b"")))
            connection.endheaders()
            if encoded is not None:
                secured.settimeout(max(0.001, end - time.monotonic()))
                connection.send(encoded)
            remaining = end - time.monotonic()
            if remaining <= 0:
                raise TimeoutError()
            secured.settimeout(remaining)
            response = connection.getresponse()
            if 300 <= response.status < 400:
                raise TransportError("lab_redirect_rejected")
            chunks: list[bytes] = []
            size = 0
            while True:
                remaining = end - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError()
                secured.settimeout(remaining)
                chunk = response.read1(min(65_536, 1_048_577 - size))
                if not chunk:
                    break
                size += len(chunk)
                if size > 1_048_576:
                    raise TransportError("response_too_large")
                chunks.append(chunk)
            decoded: object = json.loads(b"".join(chunks))
            if not isinstance(decoded, dict) or any(not isinstance(k, str) for k in decoded):
                raise TransportError("response_not_object")
            retry_after: int | None = None
            if response.status == 429:
                retry = response.getheader("Retry-After")
                if retry is None or re.fullmatch(r"[1-9][0-9]{0,3}", retry) is None:
                    raise TransportError("invalid_native_retry_after")
                retry_after = int(retry)
            elapsed = (time.monotonic() - start) * 1000
            if time.monotonic() > end:
                raise TimeoutError()
            return Response(response.status, dict(decoded), elapsed, size, retry_after)
        except TransportError:
            raise
        except (OSError, ValueError, http.client.HTTPException):
            raise TransportError("lab_request_failed") from None
        finally:
            if deadline_timer is not None:
                deadline_timer.cancel()
            if response is not None:
                response.close()
            connection.finish()
            if secured is not None:
                secured.close()


def assert_refusal(
    fixture: dict[str, object], mode: str, response: dict[str, object]
) -> None:
    if mode not in {"bad_request", "too_many_requests", "not_found", "unauthenticated"}:
        raise TransportError("invalid_refusal_mode")
    error = response.get("error")
    if not isinstance(error, dict) or error.get("code") != mode:
        raise TransportError("wrong_refusal_domain")
    raw = json.dumps(response)
    if "keys" in response or "secrets" in response:
        raise TransportError("refusal_contains_delivery")
    for field in ("config_value", "secret_value"):
        canary = fixture.get(field)
        if not isinstance(canary, str) or not canary or canary in raw:
            raise TransportError("refusal_disclosed_canary")

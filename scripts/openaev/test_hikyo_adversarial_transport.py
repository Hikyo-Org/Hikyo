"""Local socket regressions for deadlines, response bounds and safe errors."""

from __future__ import annotations

import socket
import ssl
import threading
import time
import unittest
from collections.abc import Callable
from pathlib import Path
from typing import BinaryIO
from unittest.mock import patch

import hikyo_adversarial_transport as transport

PATH = "/api/v1/orgs/lab/projects/project/environments/staging/delivery"
TOKEN = "hik_1_wl_TEST_ONLY_TRANSPORT_CANARY"
BODY_CANARY = "TEST_ONLY_RAW_BODY_CANARY"


class FakeTLSSocket:
    """Forward real socket behavior, including close, timeout and shutdown."""

    def __init__(self, sock: socket.socket):
        self.sock = sock

    def settimeout(self, value: float) -> None:
        self.sock.settimeout(value)

    def do_handshake(self) -> None:
        pass

    def sendall(self, data: bytes) -> None:
        self.sock.sendall(data)

    def makefile(self, mode: str) -> BinaryIO:
        if mode != "rb":
            raise ValueError("unexpected test stream mode")
        return self.sock.makefile("rb")

    def shutdown(self, how: int) -> None:
        self.sock.shutdown(how)

    def close(self) -> None:
        self.sock.close()


def response_bytes(
    status: int = 200,
    body: bytes = b'{"verified":true}',
    headers: tuple[bytes, ...] = (),
) -> bytes:
    return (
        f"HTTP/1.1 {status} Test\r\nContent-Length: {len(body)}\r\n".encode()
        + b"\r\n".join(headers)
        + (b"\r\n" if headers else b"")
        + b"\r\n"
        + body
    )


class WorkloadTransportTests(unittest.TestCase):
    def setUp(self) -> None:
        with patch.object(transport.ssl, "create_default_context", return_value=ssl.create_default_context()):
            self.transport = transport.WorkloadTransport(Path("unit-test-ca"))

    def test_workload_transport_enforces_tls_1_2_or_newer(self) -> None:
        self.assertEqual(self.transport.context.minimum_version, ssl.TLSVersion.TLSv1_2)

    def exchange(
        self,
        send: Callable[[socket.socket, threading.Event], None],
        *,
        timeout_s: float = 0.8,
        deadline: float | None = None,
    ) -> transport.Response:
        client, server = socket.socketpair()
        stop = threading.Event()

        def serve() -> None:
            try:
                server.settimeout(1.0)
                received = b""
                while b"\r\n\r\n" not in received:
                    chunk = server.recv(4096)
                    if not chunk:
                        return
                    received += chunk
                send(server, stop)
            except OSError:
                # Expected when the client's absolute deadline closes its end.
                pass
            finally:
                server.close()

        def wrap(
            sock: socket.socket,
            *,
            server_hostname: str,
            do_handshake_on_connect: bool,
        ) -> FakeTLSSocket:
            self.assertEqual(server_hostname, "127.0.0.1")
            self.assertFalse(do_handshake_on_connect)
            return FakeTLSSocket(sock)

        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        try:
            with (
                patch.object(
                    transport.socket, "create_connection", return_value=client
                ) as dial,
                patch.object(ssl.SSLContext, "wrap_socket", side_effect=wrap),
            ):
                try:
                    return self.transport.request(
                        "GET", PATH, TOKEN, timeout_s=timeout_s, deadline=deadline
                    )
                finally:
                    dial.assert_called_once()
                    self.assertEqual(dial.call_args.args[0], ("127.0.0.1", 18081))
        finally:
            stop.set()
            client.close()
            thread.join(1.1)
            self.assertFalse(thread.is_alive(), "local server did not drain")

    def assert_safe_error(self, error: transport.TransportError, code: str) -> None:
        self.assertEqual(str(error), code)
        self.assertNotIn(TOKEN, str(error))
        self.assertNotIn(BODY_CANARY, str(error))
        self.assertIsNone(error.__cause__)

    def test_absolute_deadline_interrupts_trickled_headers_and_body(self) -> None:
        for prefix, tail in (
            (b"HTTP/1.1 200 OK\r\nX-Test: ", b"x" * 100),
            (b"HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n", b"x" * 100),
        ):
            with self.subTest(prefix=prefix):

                def trickle(
                    server: socket.socket,
                    stop: threading.Event,
                    initial: bytes = prefix,
                    remaining: bytes = tail,
                ) -> None:
                    server.sendall(initial)
                    for byte in remaining:
                        if stop.wait(0.025):
                            return
                        server.sendall(bytes((byte,)))

                began = time.monotonic()
                with self.assertRaises(transport.TransportError) as caught:
                    # Caller deadline is earlier than socket inactivity timeout.
                    self.exchange(trickle, timeout_s=0.8, deadline=began + 0.18)
                self.assert_safe_error(caught.exception, "lab_request_failed")
                elapsed = time.monotonic() - began
                self.assertGreaterEqual(elapsed, 0.13)
                self.assertLess(elapsed, 0.6)

    def test_connection_close_valid_json_is_accepted(self) -> None:
        def send(server: socket.socket, _: threading.Event) -> None:
            server.sendall(response_bytes(headers=(b"Connection: close",)))

        response = self.exchange(send)
        self.assertEqual(response.status, 200)
        self.assertEqual(response.body, {"verified": True})
        self.assertEqual(response.response_bytes, len(b'{"verified":true}'))
        self.assertIsNone(response.retry_after)

    def test_redirect_rejected_without_second_dial_or_location_follow(self) -> None:
        def send(server: socket.socket, _: threading.Event) -> None:
            server.sendall(
                response_bytes(302, headers=(b"Location: https://production.invalid/",))
            )

        with self.assertRaises(transport.TransportError) as caught:
            self.exchange(send)
        self.assert_safe_error(caught.exception, "lab_redirect_rejected")

    def test_response_body_limit_refuses_before_json_decode(self) -> None:
        body = b'{"message":"' + b"x" * 1_048_576 + b'"}'

        def send(server: socket.socket, _: threading.Event) -> None:
            server.sendall(response_bytes(body=body))

        with self.assertRaises(transport.TransportError) as caught:
            self.exchange(send)
        self.assert_safe_error(caught.exception, "response_too_large")

    def test_native_retry_after_is_positive_bounded_integer(self) -> None:
        for retry in (
            b"1",
            b"60",
            None,
            b"0",
            b"-1",
            b"1.5",
            b"10000",
            b"Wed, 21 Oct 2015 07:28:00 GMT",
        ):
            with self.subTest(retry=retry):

                def send(
                    server: socket.socket,
                    _: threading.Event,
                    value: bytes | None = retry,
                ) -> None:
                    headers = (b"Retry-After: " + value,) if value is not None else ()
                    server.sendall(response_bytes(429, headers=headers))

                if retry in (b"1", b"60"):
                    result = self.exchange(send)
                    self.assertEqual(result.status, 429)
                    self.assertEqual(result.retry_after, int(retry))
                else:
                    with self.assertRaises(transport.TransportError) as caught:
                        self.exchange(send)
                    self.assert_safe_error(
                        caught.exception, "invalid_native_retry_after"
                    )

    def test_invalid_json_error_does_not_include_token_or_response_body(self) -> None:
        def send(server: socket.socket, _: threading.Event) -> None:
            server.sendall(response_bytes(body=(BODY_CANARY + TOKEN).encode()))

        with self.assertRaises(transport.TransportError) as caught:
            self.exchange(send)
        self.assert_safe_error(caught.exception, "lab_request_failed")


if __name__ == "__main__":
    unittest.main()

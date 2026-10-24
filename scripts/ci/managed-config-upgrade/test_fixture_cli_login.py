"""Exercise both enrolled fixture callers over the HTTP ceremony boundary."""

import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import Mock, patch
import urllib.request


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ROOT = Path(__file__).parents[1]
rollout = load("rollout_login", ROOT / "check-config-rollout-kind.py")
rehearsal = load("rehearsal_login", Path(__file__).with_name("rehearsal-fixture.py"))


class LoginChecks(unittest.TestCase):
    def ceremony(self, owner, failure=""):
        calls = []
        binding = {}
        test = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                calls.append(self.path)
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"{}")

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
                path = self.path.removeprefix("/api/v1")
                calls.append(path)
                test.assertNotIn("Authorization", self.headers, "browser ceremony must not reuse a stale bearer")
                status, result, cookies = 200, {}, []
                if path == "/auth/local/login":
                    if body.get("artifact") != "browser":
                        status = 400
                    else:
                        status, result = 202, {"challenge_id": "challenge", "factors": ["totp"]}
                elif path == "/auth/login/challenge/challenge/totp":
                    test.assertEqual(body, {"code": "123456"})
                    status = 401 if failure == "factor" else 200
                    result = {"principal": {"id": "principal"}, "session": {"id": "browser", "artifact": "browser"}}
                    cookies = ["__Host-hikyo=browser-cookie; Secure; Path=/", "__Host-hikyo-csrf=csrf; Secure; Path=/"]
                elif path == "/auth/cli-login/start":
                    status = 201
                    binding.update(body)
                    result = {"state": body["state"]}
                elif path == "/auth/cli-login/approve":
                    test.assertEqual(self.headers.get("X-Hikyo-CSRF"), "csrf")
                    test.assertIn("__Host-hikyo=browser-cookie", self.headers.get("Cookie", ""))
                    test.assertEqual(body, {"reference": binding["state"]})
                    result = {"state": "wrong" if failure == "binding" else binding["state"], "redirect_uri": binding["redirect_uri"], "code": "one-use"}
                    if failure == "redirect":
                        status = 302
                elif path == "/auth/cli-login/redeem":
                    digest = hashlib.sha256(body["pkce_verifier"].encode()).digest()
                    test.assertEqual(base64.urlsafe_b64encode(digest).decode().rstrip("="), binding["pkce_challenge"])
                    test.assertEqual(body["redirect_uri"], binding["redirect_uri"])
                    test.assertEqual(body["code"], "one-use")
                    result = {"principal": {"id": "principal"}, "session_token": "cli-bearer", "session": {"id": "cli", "artifact": "cli"}}
                    if failure == "artifact":
                        result["session"]["artifact"] = "browser"
                    if failure == "principal":
                        result["principal"]["id"] = "different-principal"
                    if failure == "token":
                        result["session_token"] = {"invalid": "token shape"}
                elif path == "/auth/logout":
                    test.assertEqual(self.headers.get("X-Hikyo-CSRF"), "csrf")
                    status = 500 if failure == "logout" else 204
                else:
                    test.fail("unexpected ceremony path")
                self.send_response(status)
                if failure == "redirect" and path == "/auth/cli-login/approve":
                    self.send_header("Location", "/redirected")
                for cookie in cookies:
                    self.send_header("Set-Cookie", cookie)
                self.end_headers()
                if status != 204:
                    self.wfile.write(json.dumps(result).encode())

        with ThreadingHTTPServer(("127.0.0.1", 0), Handler) as server:
            worker = threading.Thread(target=server.serve_forever)
            worker.start()
            try:
                module = rollout if owner == "rollout" else rehearsal
                fixture = module.Fixture.__new__(module.Fixture)
                fixture.origin = f"http://127.0.0.1:{server.server_port}"
                fixture.password = "synthetic fixture password"
                fixture.token = "stale-cli-bearer"
                fixture.code = Mock(side_effect=["123456", "654321"])
                fixture.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                fixture.request = Mock(return_value={"session_token": "separate-step-up"})
                if failure:
                    expected_error = (
                        self.assertRaisesRegex(RuntimeError, "fixture login refused an HTTP redirect")
                        if failure == "redirect" else self.assertRaises(RuntimeError)
                    )
                    with expected_error:
                        if owner == "rollout":
                            fixture.login()
                        else:
                            fixture.login(fixture.password, fixture.code)
                    fixture.request.assert_not_called()
                    if failure != "factor":
                        self.assertEqual(calls[-1], "/auth/logout")
                    if failure == "binding":
                        self.assertNotIn("/auth/cli-login/redeem", calls)
                    if failure == "redirect":
                        self.assertNotIn("/redirected", calls)
                        self.assertNotIn("/auth/cli-login/redeem", calls)
                else:
                    if owner == "rollout":
                        fixture.login()
                        fixture.request.assert_called_once_with("POST", "/auth/totp/step-up", {"code": "654321"})
                        self.assertEqual(fixture.token, "separate-step-up")
                    else:
                        fixture.login(fixture.password, fixture.code)
                        fixture.request.assert_not_called()
                        self.assertEqual(fixture.token, "cli-bearer")
                    self.assertEqual(calls[-1], "/auth/logout")
                    self.assertEqual(calls, [
                        "/auth/local/login", "/auth/login/challenge/challenge/totp",
                        "/auth/cli-login/start", "/auth/cli-login/approve",
                        "/auth/cli-login/redeem", "/auth/logout",
                    ])
            finally:
                server.shutdown()
                worker.join()

    def proxy_ceremony(self, failure=""):
        proxy_requests = []

        class ProxyTrap(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_POST(self):
                # An honest trap refuses proxy traffic. It never impersonates
                # the fixture or copies credential bodies into diagnostics.
                proxy_requests.append(self.command)
                self.send_response(502)
                self.end_headers()

        with ThreadingHTTPServer(("127.0.0.1", 0), ProxyTrap) as proxy:
            worker = threading.Thread(target=proxy.serve_forever)
            worker.start()
            try:
                proxy_url = f"http://127.0.0.1:{proxy.server_port}"
                with patch.dict("os.environ", {
                    "http_proxy": proxy_url, "HTTP_PROXY": proxy_url,
                    "https_proxy": proxy_url, "HTTPS_PROXY": proxy_url,
                    "no_proxy": "", "NO_PROXY": "",
                }, clear=True):
                    try:
                        self.ceremony("rollout", failure)
                    finally:
                        self.assertEqual(proxy_requests, [], "fixture traffic reached the ambient proxy")
            finally:
                proxy.shutdown()
                worker.join()

    def test_default_login_ignores_ambient_proxy_through_mfa_and_handoff(self):
        self.proxy_ceremony()

    def test_default_login_still_refuses_redirects_with_ambient_proxy(self):
        self.proxy_ceremony("redirect")

    def test_both_enrolled_callers_complete_browser_mfa_and_pkce(self):
        for owner in ("rollout", "rehearsal"):
            with self.subTest(owner=owner):
                self.ceremony(owner)

    def test_refusals_never_fall_back_and_browser_is_logged_out(self):
        for owner in ("rollout", "rehearsal"):
            for failure in ("factor", "binding", "artifact", "principal", "token", "logout"):
                with self.subTest(owner=owner, failure=failure):
                    self.ceremony(owner, failure)

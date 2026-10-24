"""Native enrolled-TOTP browser authentication and separate CLI handoff.

Only synthetic fixture callers use this helper. The caller owns the TOTP
ledger and transport trust. Fresh operation proofs remain the caller's work.
"""

import base64
import hashlib
import json
import secrets
import urllib.error
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RuntimeError("fixture login refused an HTTP redirect")


def login(origin, username, password, draw_code, http=None):
    if http is None:
        http = urllib.request.build_opener(NoRedirect())

    def request(path, body, status=200, headers=None):
        req = urllib.request.Request(
            origin.rstrip("/") + "/api/v1" + path,
            data=json.dumps(body).encode(), method="POST",
            headers={"Content-Type": "application/json", **(headers or {})},
        )
        try:
            with http.open(req, timeout=30) as response:
                if response.status != status:
                    raise RuntimeError(f"fixture login {path} returned HTTP {response.status}, expected {status}")
                raw = response.read(2 * 1024 * 1024 + 1)
                if len(raw) > 2 * 1024 * 1024:
                    raise RuntimeError("fixture login response exceeded limit")
                value = json.loads(raw) if raw else {}
                if not isinstance(value, dict):
                    raise RuntimeError("fixture login response must be an object")
                return value, response.headers
        except urllib.error.HTTPError as error:
            error.close()
            raise RuntimeError(f"fixture login {path} returned HTTP {error.code}") from None

    challenge, _ = request("/auth/local/login", {
        "username": username, "password": password, "artifact": "browser",
    }, 202)
    challenge_id = challenge.get("challenge_id")
    if (not isinstance(challenge_id, str) or not challenge_id
            or not isinstance(challenge.get("factors"), list) or "totp" not in challenge["factors"]):
        raise RuntimeError("fixture login requires its enrolled TOTP challenge")
    browser, headers = request("/auth/login/challenge/" + challenge_id + "/totp", {"code": draw_code()})
    session = browser.get("session", {})
    principal = browser.get("principal", {})
    if (not isinstance(session, dict) or session.get("artifact") != "browser"
            or not isinstance(session.get("id"), str) or not session["id"]
            or "session_token" in browser or not isinstance(principal, dict)
            or not isinstance(principal.get("id"), str) or not principal["id"]):
        raise RuntimeError("fixture login returned invalid browser artifact")
    cookies = [value.split(";", 1)[0] for value in headers.get_all("Set-Cookie", [])]
    csrf = next((value.removeprefix("__Host-hikyo-csrf=") for value in cookies if value.startswith("__Host-hikyo-csrf=")), "")
    if not csrf or not any(value.startswith("__Host-hikyo=") and value != "__Host-hikyo=" for value in cookies):
        raise RuntimeError("fixture login omitted browser or CSRF cookie")
    browser_headers = {"Cookie": "; ".join(cookies), "X-Hikyo-CSRF": csrf}
    result = None
    failure = None
    try:
        state, verifier = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
        digest = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")
        redirect = "http://127.0.0.1:41234/callback"
        started, _ = request("/auth/cli-login/start", {
            "transport": "loopback", "state": state, "pkce_challenge": digest, "redirect_uri": redirect,
        }, 201)
        if started.get("state") != state:
            raise RuntimeError("fixture CLI start binding mismatch")
        approved, _ = request("/auth/cli-login/approve", {"reference": state}, headers=browser_headers)
        if approved.get("state") != state or approved.get("redirect_uri") != redirect or not isinstance(approved.get("code"), str) or not approved["code"]:
            raise RuntimeError("fixture CLI approval binding mismatch")
        result, _ = request("/auth/cli-login/redeem", {
            "code": approved["code"], "pkce_verifier": verifier, "redirect_uri": redirect,
        })
        cli = result.get("session", {})
        cli_principal = result.get("principal", {})
        if (not isinstance(cli, dict) or cli.get("artifact") != "cli"
                or not isinstance(cli.get("id"), str) or not cli["id"] or cli["id"] == session["id"]
                or not isinstance(result.get("session_token"), str) or not result["session_token"]
                or not isinstance(cli_principal, dict) or cli_principal.get("id") != principal["id"]):
            raise RuntimeError("fixture CLI redemption returned invalid artifact")
    except Exception as error:
        failure = error
    try:
        request("/auth/logout", {}, 204, browser_headers)
    except Exception as cleanup_error:
        if failure is not None:
            raise ExceptionGroup("fixture login and browser logout failed", [failure, cleanup_error]) from None
        raise
    if failure is not None:
        raise failure
    return result

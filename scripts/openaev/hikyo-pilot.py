#!/usr/bin/env python3
"""Bootstrap disposable lab through Hikyo's real auth/API; independently assert scope/revocation."""

import argparse
import base64
import hashlib
import hmac
import http.cookiejar
import json
import os
import secrets
import ssl
import stat
import struct
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


def private_bytes(path: Path) -> bytes:
    info = path.lstat()
    if (
        not stat.S_ISREG(info.st_mode)
        or info.st_uid != os.getuid()
        or stat.S_IMODE(info.st_mode) != 0o600
    ):
        raise SystemExit("Pilot config must be an owned regular file with mode 0600")
    return path.read_bytes()


def private_json(path: Path) -> dict:
    result = json.loads(private_bytes(path))
    if not isinstance(result, dict):
        raise SystemExit("Pilot config must be an object")
    return result


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class API:
    def __init__(self, origin: str, ca: Path):
        parsed = urllib.parse.urlsplit(origin)
        allowed_endpoints = {
            ("127.0.0.1", 18081),
            ("127.0.0.1", 28446),
            ("127.0.0.1", 18081),
            ("hikyo.hikyo-lab.svc.cluster.local", 8443),
        }
        if (
            parsed.scheme != "https"
            or (parsed.hostname, parsed.port) not in allowed_endpoints
            or parsed.username
            or parsed.password
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
        ):
            raise SystemExit("Refusing a non-lab HTTPS origin")
        self.origin = origin.rstrip("/")
        self.human_token: str | None = None
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            NoRedirect(),
            urllib.request.HTTPSHandler(
                context=ssl.create_default_context(cafile=str(ca))
            ),
            urllib.request.HTTPCookieProcessor(self.cookies),
        )

    def call(
        self,
        method: str,
        path: str,
        body: dict | None = None,
        token: str | None = None,
        expected: int | None = None,
    ) -> tuple[int, dict]:
        headers = {"Accept": "application/json", "Origin": "https://127.0.0.1:18081"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        if token is not None:
            self.cookies.clear()
            headers["Authorization"] = "Bearer " + token
        elif self.human_token is not None:
            headers["Authorization"] = "Bearer " + self.human_token
        elif method != "GET":
            for cookie in self.cookies:
                if cookie.name == "__Host-hikyo-csrf":
                    if not isinstance(cookie.value, str) or not cookie.value:
                        raise SystemExit("Lab CSRF cookie has no value")
                    headers["X-Hikyo-CSRF"] = cookie.value
        request = urllib.request.Request(
            self.origin + path,
            method=method,
            headers=headers,
            data=json.dumps(body).encode() if body is not None else None,
        )
        try:
            response = self.opener.open(request, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            code, raw = response.code, response.read(1048577)
        if 300 <= code < 400:
            raise SystemExit("Refusing lab API redirect")
        if len(raw) > 1048576:
            raise SystemExit("Hikyo response exceeded size bound")
        result = json.loads(raw) if raw else {}
        if not isinstance(result, dict):
            raise SystemExit("Hikyo returned a non-object response")
        if expected is not None and code != expected:
            # Never print response bodies: mint/login/reveal responses contain credentials.
            error_code = (
                result.get("error", {}).get("code", "unknown")
                if isinstance(result.get("error"), dict)
                else result.get("code", "unknown")
            )
            raise SystemExit(
                f"{method} {path}: HTTP {code}, expected {expected}, error {error_code}"
            )
        if isinstance(result.get("session_token"), str):
            self.human_token = result["session_token"]
        return code, result


def totp(uri: str) -> str:
    query = urllib.parse.parse_qs(urllib.parse.urlparse(uri).query)
    secret = base64.b32decode(query["secret"][0])
    digest = hmac.new(
        secret, struct.pack(">Q", int(time.time()) // 30), hashlib.sha1
    ).digest()
    offset = digest[-1] & 15
    return str(
        (struct.unpack(">I", digest[offset : offset + 4])[0] & 0x7FFFFFFF) % 1000000
    ).zfill(6)


def wait_new_totp() -> None:
    # Server correctly rejects replay of the enrolment/step-up proof.
    time.sleep(30 - (time.time() % 30) + 0.2)


def host_command(args: argparse.Namespace, command: list[str]) -> bytes:
    if args.docker_container:
        prefix = ["docker", "exec", args.docker_container]
    else:
        prefix = [
            "kubectl",
            "--kubeconfig",
            str(args.kubeconfig),
            "--context",
            os.environ.get("OPENAEV_KUBE_CONTEXT", "default"),
            "-n",
            "hikyo-lab",
            "exec",
            "deployment/hikyo",
            "-c",
            "server",
            "--",
        ]
    result = subprocess.run(prefix + command, check=True, capture_output=True)
    return result.stdout


def ensure_named(api: API, path: str, body: dict, resume: bool) -> dict:
    if resume:
        _, listing = api.call("GET", path, expected=200)
        matches = [item for item in listing["items"] if item["name"] == body["name"]]
        if len(matches) > 1:
            raise SystemExit("Ambiguous fixture entity name")
        if matches:
            return matches[0]
    return api.call("POST", path, body, expected=201)[1]


def bootstrap(args: argparse.Namespace) -> None:
    args.state.mkdir(mode=0o700, parents=True, exist_ok=True)
    if (
        args.state.is_symlink()
        or stat.S_IMODE(args.state.stat().st_mode) != 0o700
        or (any(args.state.iterdir()) and not args.resume)
    ):
        raise SystemExit("Bootstrap state directory must be empty and owner-only")
    api = API(args.origin, args.ca)
    metadata: dict[str, object] | None = None
    for attempt in range(30):
        try:
            metadata = api.call("GET", "/api/v1/meta", expected=200)[1]
            break
        except urllib.error.URLError:
            if attempt == 29:
                raise SystemExit("Lab TLS listener did not become available")
            time.sleep(1)
    if metadata is None:
        raise SystemExit("Lab metadata was not obtained")
    server_version = metadata.get("server_version")
    if not isinstance(server_version, str) or server_version != args.expected_version:
        raise SystemExit("Refusing to bootstrap a different Hikyo release")
    instance_identity = metadata.get("instance_identity")
    if not isinstance(instance_identity, str) or not instance_identity:
        raise SystemExit("Lab metadata has no verified instance identity")
    identity_file = args.state / "bootstrap-instance.json"
    if (
        identity_file.exists()
        and private_json(identity_file)["instance_identity"] != instance_identity
    ):
        raise SystemExit("Refusing to resume against a different lab instance")
    identity_file.write_text(
        json.dumps({"instance_identity": instance_identity}) + "\n"
    )
    if args.resume:
        admin = private_json(args.state / "admin.json")
        password, uri = admin["password"], admin["totp_uri"]
        api.call(
            "POST",
            "/api/v1/auth/local/login",
            {"username": "lab-admin", "password": password, "artifact": "cli"},
            expected=200,
        )
    else:
        # The explicit native print opt-in is captured only by this process, never
        # emitted to terminal or logs, and immediately saved in an owner-only file.
        authority = (
            host_command(
                args,
                [
                    "/usr/local/bin/hikyo",
                    "admin",
                    "create",
                    "--username",
                    "lab-admin",
                    "--display-name",
                    "Disposable Lab Administrator",
                    "--dangerously-print",
                ],
            )
            .decode()
            .strip()
        )
        (args.state / "bootstrap-authority").write_text(authority + "\n")
        password = secrets.token_urlsafe(32)
        (args.state / "admin.json").write_text(
            json.dumps({"username": "lab-admin", "password": password}) + "\n"
        )
        api.call(
            "POST",
            "/api/v1/auth/credential/establish",
            {"authority": authority, "password": password},
            expected=204,
        )
        api.call(
            "POST",
            "/api/v1/auth/local/login",
            {"username": "lab-admin", "password": password, "artifact": "cli"},
            expected=200,
        )
        _, enrolment = api.call(
            "POST",
            "/api/v1/auth/totp/enrol/start",
            {"password": password},
            expected=200,
        )
        uri = enrolment["otpauth_uri"]
        (args.state / "admin.json").write_text(
            json.dumps({"username": "lab-admin", "password": password, "totp_uri": uri})
            + "\n"
        )
        code, _ = api.call(
            "POST", "/api/v1/auth/totp/enrol/confirm", {"code": totp(uri)}
        )
        if code not in (200, 204):
            raise SystemExit(f"TOTP enrolment refused: HTTP {code}")
    wait_new_totp()
    api.call("POST", "/api/v1/auth/totp/step-up", {"code": totp(uri)}, expected=200)
    org = ensure_named(
        api, "/api/v1/orgs", {"name": "openaev-disposable-lab"}, args.resume
    )
    # Org creation increases authority and invalidates its initiating session.
    api.call(
        "POST",
        "/api/v1/auth/local/login",
        {"username": "lab-admin", "password": password, "artifact": "cli"},
        expected=200,
    )
    wait_new_totp()
    api.call("POST", "/api/v1/auth/totp/step-up", {"code": totp(uri)}, expected=200)
    project_path = f"/api/v1/orgs/{org['id']}/projects"
    project = ensure_named(api, project_path, {"name": "scope-pilot"}, args.resume)
    base = project_path + "/" + project["id"]
    environments = {}
    for name in ("staging", "production"):
        environments[name] = ensure_named(
            api, base + "/environments", {"name": name}, args.resume
        )
    key = ensure_named(
        api,
        base + "/keys",
        {
            "name": "LAB_CANARY",
            "classification": "config",
            "declaration": {"rule": {"type": "string"}},
        },
        args.resume,
    )
    expected_value = "synthetic-openaev-staging-" + secrets.token_hex(8)
    for name, env in environments.items():
        env_path = base + "/environments/" + env["id"]
        _, pending = api.call(
            "PUT",
            env_path + "/values/" + key["name"],
            {
                "value": expected_value
                if name == "staging"
                else "synthetic-openaev-production-denied"
            },
            expected=200,
        )
        api.call(
            "POST",
            env_path + "/publish",
            {"version_ids": [pending["version_id"]]},
            expected=200,
        )
    account = ensure_named(
        api,
        base + "/service-accounts",
        {"name": "openaev-staging-reader", "kind": "workload"},
        args.resume,
    )
    staging = base + "/environments/" + environments["staging"]["id"]
    api.call(
        "POST",
        staging + "/grants",
        {"principal": account["principal_id"], "capability": "read"},
        expected=200,
    )
    credential_path = base + "/service-accounts/" + account["id"] + "/credentials"
    if args.resume:
        _, credentials = api.call("GET", credential_path, expected=200)
        for existing in credentials["items"]:
            if not existing.get("revoked_at"):
                api.call("DELETE", credential_path + "/" + existing["id"], expected=204)
    _, credential = api.call(
        "POST", credential_path, {"lifetime_seconds": 86400}, expected=200
    )
    token_path = args.state / "staging-token"
    token_path.write_text(credential["value"] + "\n")
    config = {
        "format": "hikyo-openaev-pilot/v1",
        "instance_url": args.origin,
        "internal_instance_url": "https://hikyo.hikyo-lab.svc.cluster.local:8443",
        "instance_identity": instance_identity,
        "server_version": server_version,
        "staging_delivery_path": staging + "/delivery",
        "outside_scope_delivery_path": base
        + "/environments/"
        + environments["production"]["id"]
        + "/delivery",
        "token_file": str(token_path.resolve()),
        "ca_file": str(args.ca.resolve()),
        "expected_key": "LAB_CANARY",
        "expected_value": expected_value,
        "credential_id": credential["credential"]["id"],
        "revoke_path": credential_path + "/" + credential["credential"]["id"],
    }
    (args.state / "pilot.json").write_text(json.dumps(config, indent=2) + "\n")
    (args.state / "admin.json").write_text(
        json.dumps({"username": "lab-admin", "password": password, "totp_uri": uri})
        + "\n"
    )
    print("Lab bootstrap complete; synthetic staging token/config saved outside Git.")


def pilot(args: argparse.Namespace) -> None:
    config = private_json(args.config)
    if config.get("format") != "hikyo-openaev-pilot/v1":
        raise SystemExit("Unsupported pilot configuration")
    api = API(config["instance_url"], Path(config["ca_file"]))
    if (
        api.call("GET", "/api/v1/meta", expected=200)[1].get("instance_identity")
        != config["instance_identity"]
    ):
        raise SystemExit("Refusing to test a different lab instance")
    token_path = Path(config["token_file"])
    token = private_bytes(token_path).decode().strip()
    if args.revoke:
        admin = private_json(args.config.parent / "admin.json")
        api.call(
            "POST",
            "/api/v1/auth/local/login",
            {
                "username": admin["username"],
                "password": admin["password"],
                "artifact": "cli",
            },
            expected=200,
        )
        wait_new_totp()
        api.call(
            "POST",
            "/api/v1/auth/totp/step-up",
            {"code": totp(admin["totp_uri"])},
            expected=200,
        )
        api.call("DELETE", config["revoke_path"], expected=204)
    path = (
        config["outside_scope_delivery_path"]
        if args.case == "outside-scope"
        else config["staging_delivery_path"]
    )
    code, response = api.call("GET", path, token=token)
    if args.case == "staging":
        matching = [
            item
            for item in response.get("keys", [])
            if item.get("name") == config["expected_key"]
        ]
        if (
            code != 200
            or response.get("current") is not False
            or len(matching) != 1
            or matching[0].get("classification") != "config"
            or matching[0].get("presence") != "set"
            or matching[0].get("value") != config["expected_value"]
        ):
            raise SystemExit(
                "FAIL: staging delivery did not return its synthetic canary"
            )
    else:
        expected = 404 if args.case == "outside-scope" else 401
        expected_error = (
            "not_found" if args.case == "outside-scope" else "unauthenticated"
        )
        if (
            code != expected
            or response.get("error", {}).get("code") != expected_error
            or "keys" in response
            or "secrets" in response
            or config["expected_value"] in json.dumps(response)
            or config["expected_key"] in json.dumps(response)
        ):
            raise SystemExit(
                f"FAIL: {args.case} did not produce denial without canary disclosure"
            )
    print(f"PASS: {args.case}, HTTP {code}, independent assertion")


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="operation", required=True)
    create = sub.add_parser("bootstrap")
    create.add_argument("--origin", default="https://127.0.0.1:18081")
    create.add_argument(
        "--expected-version",
        default="0.0.1-nightly.20261003.66.g5c3f969f",
        help="exact independently verified candidate version required before bootstrap",
    )
    create.add_argument("--ca", type=Path, required=True)
    create.add_argument("--state", type=Path, required=True)
    create.add_argument("--docker-container")
    create.add_argument(
        "--kubeconfig", type=Path, default=Path(os.environ.get("KUBECONFIG", ""))
    )
    create.add_argument(
        "--resume",
        action="store_true",
        help="resume an interrupted disposable bootstrap using retained lab admin credentials",
    )
    check = sub.add_parser("check")
    check.add_argument("--config", type=Path, required=True)
    check.add_argument(
        "--case", choices=("staging", "outside-scope", "revoked"), required=True
    )
    check.add_argument("--revoke", action="store_true")
    args = parser.parse_args()
    if args.operation == "check" and args.revoke and args.case != "revoked":
        parser.error("--revoke is only valid with --case revoked")
    bootstrap(args) if args.operation == "bootstrap" else pilot(args)


if __name__ == "__main__":
    main()

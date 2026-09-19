#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyotp==2.9.0"]
# ///
"""Synthetic fixture using Hikyo's supported HTTP auth and machine CLI.

Only the owned local rehearsal runner may invoke this hook. No production
configuration, credential, database export or adapter is accepted as input.
All response bodies, including failures, remain in private fixture custody.
"""

import json
import os
import re
import secrets
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import pyotp

CLIENT_IMAGE = "ghcr.io/hikyo-org/hikyo@sha256:01e9cb6b51bfcbc953906daa733e4e4b5cefe23f5df80cc5419beca0b6064aeb"
IDENTITY = (
    "https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main"
)
ISSUER = "https://token.actions.githubusercontent.com"
BINARY = "/usr/local/bin/hikyo"


class Failure(Exception):
    """Only sanitized diagnostics may cross this boundary."""


def object_value(value):
    if not isinstance(value, dict):
        raise Failure("expected a JSON object")
    return value


def string_value(value):
    if not isinstance(value, str) or not value:
        raise Failure("expected a nonempty string")
    return value


class Fixture:
    def __init__(self):
        self.container = os.environ["HIKYO_REHEARSAL_CONTAINER"]
        self.origin = os.environ["HIKYO_REHEARSAL_URL"]
        parsed = urllib.parse.urlsplit(self.origin)
        if (
            parsed.scheme != "https"
            or parsed.hostname != "127.0.0.1"
            or not parsed.port
            or parsed.username
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
        ):
            raise Failure("fixture requires the runner's local HTTPS loopback endpoint")
        if not os.environ.get("DOCKER_HOST", "").startswith("unix://"):
            raise Failure("fixture requires an explicit local Docker socket")
        self.directory = (
            Path(os.environ["HIKYO_REHEARSAL_OUTPUT_DIR"]).resolve() / "fixture"
        )
        self.directory.mkdir(mode=0o700, exist_ok=True)
        if self.directory.is_symlink() or self.directory.stat().st_mode & 0o077:
            raise Failure("fixture custody must be private")
        self.token = ""
        self.counter = 0
        # Ignore ambient proxy settings; no request leaves the loopback origin.
        self.http = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            urllib.request.HTTPSHandler(
                context=ssl.create_default_context(
                    cafile=str(self.directory.parent / "tls" / "server.crt")
                )
            ),
            NoRedirect(),
        )

    def write(self, name, value):
        with (self.directory / name).open("x") as stream:
            os.chmod(stream.name, 0o600)
            stream.write(value)

    def command(self, args, label, data=None):
        self.counter += 1
        result = subprocess.run(
            args, input=data, capture_output=True, timeout=180, check=False
        )
        prefix = f"{sys.argv[1]}-{self.counter}-{label}"
        self.write(prefix + ".stdout", result.stdout.decode("utf-8", errors="replace"))
        self.write(prefix + ".stderr", result.stderr.decode("utf-8", errors="replace"))
        if result.returncode:
            raise Failure(f"{label} failed; inspect private fixture diagnostics")
        return result.stdout

    def request(self, method, path, body=None):
        print(f"fixture request: {method} {path}", flush=True)
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        request = urllib.request.Request(
            self.origin.rstrip("/") + "/api/v1" + path,
            data=None if body is None else json.dumps(body).encode(),
            method=method,
            headers=headers,
        )
        try:
            with self.http.open(request, timeout=30) as response:
                raw = response.read(2 * 1024 * 1024)
        except urllib.error.HTTPError as error:
            self.counter += 1
            self.write(
                f"{sys.argv[1]}-{self.counter}-http-error.json",
                error.read(65536).decode("utf-8", errors="replace"),
            )
            raise Failure(
                f"fixture API returned HTTP {error.code}; inspect private diagnostics"
            ) from None
        return {} if not raw else object_value(json.loads(raw))

    def session(self, result):
        self.token = string_value(object_value(result).get("session_token"))

    def cli(self, *args, data=None, machine=True):
        # Docker Desktop may expose a host-owned bind directory as UID 0.
        # Discover its container-visible owner; never chown the host directory
        # or weaken the CLI's private-output ownership checks.
        if not hasattr(self, "cli_user"):
            helper = os.environ.get("HIKYO_REHEARSAL_HELPER_IMAGE", "")
            if not re.fullmatch(r"postgres:18@sha256:[0-9a-f]{64}", helper):
                raise Failure("fixture requires the runner's pinned PostgreSQL helper")
            owner = (
                self.command(
                    [
                        "docker",
                        "run",
                        "--rm",
                        "--network",
                        "none",
                        "--read-only",
                        "--cap-drop",
                        "ALL",
                        "--security-opt",
                        "no-new-privileges",
                        "--mount",
                        f"type=bind,src={self.directory},dst=/fixture,readonly",
                        "--entrypoint",
                        "stat",
                        helper,
                        "-c",
                        "%u:%g",
                        "/fixture",
                    ],
                    "fixture-owner",
                )
                .decode()
                .strip()
            )
            if not re.fullmatch(r"[0-9]+:[0-9]+", owner):
                raise Failure("cannot determine container-visible fixture owner")
            self.cli_user = owner
        command = [
            "docker",
            "run",
            "--rm",
            "-i",
            "--network",
            "container:" + self.container,
            "--user",
            self.cli_user,
            "--workdir",
            "/fixture",
            "--read-only",
            "--cap-drop",
            "ALL",
            "--security-opt",
            "no-new-privileges",
            "--mount",
            f"type=bind,src={self.directory},dst=/fixture",
            "--env",
            "HIKYO_STATE_DIR=/fixture/cli-state",
            CLIENT_IMAGE,
            *args,
        ]
        if machine:
            identity = object_value(
                json.loads((self.directory / "fixture.json").read_text())
            )
            command.extend(
                [
                    "--instance",
                    "rehearsal",
                    "--org",
                    string_value(identity.get("org")),
                    "--project",
                    string_value(identity.get("project")),
                    "--auth=machine",
                    "--token-file",
                    "/fixture/machine-token",
                ]
            )
        return self.command(command, "cli", data)

    def owned_container(self):
        inspect = json.loads(
            self.command(["docker", "inspect", self.container], "inspect")
        )
        if not isinstance(inspect, list) or len(inspect) != 1:
            raise Failure("expected exactly one owned rehearsal container")
        config = object_value(object_value(inspect[0]).get("Config"))
        labels = object_value(config.get("Labels"))
        if labels.get("io.hikyo.dbugit.rehearsal") != string_value(
            os.environ.get("HIKYO_REHEARSAL_RUN_ID")
        ):
            raise Failure("fixture refuses a container not owned by this rehearsal")
        return config

    def seed(self):
        # Signature checks bind the pinned DBugIT .41 CLI, separately from the
        # source/candidate server images checked by the runner.
        self.command(
            [
                "cosign",
                "verify",
                "--certificate-identity=" + IDENTITY,
                "--certificate-oidc-issuer=" + ISSUER,
                CLIENT_IMAGE,
            ],
            "client-signature",
        )
        self.command(["docker", "pull", CLIENT_IMAGE], "client-pull")
        config = self.owned_container()
        env = config.get("Env")
        if not isinstance(env, list) or any(not isinstance(item, str) for item in env):
            raise Failure("invalid rehearsal container environment")
        settings = dict(item.split("=", 1) for item in env if "=" in item)
        state = string_value(settings.get("HIKYO_UPGRADE_STATE_DIR"))
        self.command(
            [
                "docker",
                "cp",
                self.container + ":" + state + "/unattended/image.json",
                str(self.directory / "image.json"),
            ],
            "descriptor",
        )
        descriptor = object_value(
            json.loads((self.directory / "image.json").read_text())
        )
        bundle = string_value(descriptor.get("BundleDirectory"))
        authority_path = state + "/rehearsal-authority"
        self.command(
            [
                "docker",
                "exec",
                "--env",
                "HIKYO_UPGRADE_BUNDLE=" + bundle,
                "--env",
                "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY="
                + state
                + "/unattended/operator.pub",
                self.container,
                BINARY,
                "admin",
                "create",
                "--username",
                "rehearsal-admin",
                "--output-file",
                authority_path,
            ],
            "admin",
        )
        self.command(
            [
                "docker",
                "cp",
                self.container + ":" + authority_path,
                str(self.directory / "authority"),
            ],
            "authority-copy",
        )
        authority = (self.directory / "authority").read_text().strip()
        password = secrets.token_urlsafe(32)
        self.request(
            "POST",
            "/auth/credential/establish",
            {"authority": authority, "password": password},
        )
        self.session(
            self.request(
                "POST",
                "/auth/local/login",
                {
                    "username": "rehearsal-admin",
                    "password": password,
                    "artifact": "cli",
                },
            )
        )
        uri = string_value(
            self.request("POST", "/auth/totp/enrol/start", {"password": password}).get(
                "otpauth_uri"
            )
        )
        otp = pyotp.parse_uri(uri)
        if not isinstance(otp, pyotp.TOTP):
            raise Failure("server did not return a TOTP authenticator")
        step = int(time.time()) // otp.interval
        self.session(
            self.request("POST", "/auth/totp/enrol/confirm", {"code": otp.now()})
        )
        time.sleep(max(0, (step + 1) * otp.interval + 1 - time.time()))
        self.session(self.request("POST", "/auth/totp/step-up", {"code": otp.now()}))
        org = string_value(self.request("POST", "/orgs", {"name": "dbugit"}).get("id"))
        # Org creation grants the creator admin and invalidates their session
        # generation. Reauthenticate through the supported flow before continuing.
        self.token = ""
        self.session(
            self.request(
                "POST",
                "/auth/local/login",
                {
                    "username": "rehearsal-admin",
                    "password": password,
                    "artifact": "cli",
                },
            )
        )
        step = int(time.time()) // otp.interval
        time.sleep(max(0, (step + 1) * otp.interval + 1 - time.time()))
        self.session(self.request("POST", "/auth/totp/step-up", {"code": otp.now()}))
        project = string_value(
            self.request("POST", f"/orgs/{org}/projects", {"name": "v3-preview"}).get(
                "id"
            )
        )
        base = f"/orgs/{org}/projects/{project}"
        # Provision machine grants while the project is empty. Widening a
        # machine into existing environments requires a separate per-environment
        # disclosure ceremony; this synthetic first bootstrap has none yet.
        self.request("PUT", base + "/machine-reveal", {"enabled": True})
        account = self.request(
            "POST",
            base + "/service-accounts",
            {"name": "rehearsal", "kind": "automation"},
        )
        principal = string_value(account.get("principal_id"))
        for capability in ["read", "edit", "publish", "definitions-edit", "reveal"]:
            self.request(
                "POST",
                base + "/grants",
                {"principal": principal, "capability": capability},
            )
        credential = self.request(
            "POST",
            base
            + "/service-accounts/"
            + string_value(account.get("id"))
            + "/credentials",
            {"lifetime_seconds": 7200},
        )
        self.write("machine-token", string_value(credential.get("value")))
        environment = string_value(
            self.request("POST", base + "/environments", {"name": "preview"}).get("id")
        )
        values = {
            "REHEARSAL_CONFIG": "representative-config",
            "REHEARSAL_SECRET": secrets.token_urlsafe(24),
        }
        versions = []
        for name, value in values.items():
            classification = "secret" if name.endswith("SECRET") else "config"
            self.request(
                "POST",
                base + "/keys",
                {
                    "name": name,
                    "classification": classification,
                    "declaration": {"rule": {"type": "string"}},
                },
            )
            versions.append(
                string_value(
                    self.request(
                        "PUT",
                        base + f"/environments/{environment}/values/{name}",
                        {"value": value},
                    ).get("version_id")
                )
            )
        self.request(
            "POST",
            base + f"/environments/{environment}/publish",
            {"version_ids": versions},
        )
        self.write(
            "fixture.json",
            json.dumps(
                {
                    "org": org,
                    "project": project,
                    "environment": environment,
                    "values": values,
                }
            ),
        )
        self.write(
            "trust.json",
            json.dumps(
                {
                    "name": "rehearsal",
                    "origin": "https://127.0.0.1:8080",
                    "spki_pin": string_value(
                        os.environ.get("HIKYO_REHEARSAL_TLS_SPKI_PIN")
                    ),
                }
            ),
        )
        self.cli(
            "context",
            "create",
            "rehearsal",
            "--instance",
            "https://127.0.0.1:8080",
            "--trust-file",
            "/fixture/trust.json",
            machine=False,
        )
        self.verify()

    def verify(self):
        self.owned_container()
        fixture = object_value(
            json.loads((self.directory / "fixture.json").read_text())
        )
        environment = string_value(fixture.get("environment"))
        output = "export-" + secrets.token_hex(8) + ".json"
        self.cli(
            "values",
            "export",
            "--env",
            environment,
            "--format",
            "json",
            "--reveal",
            "--output-file",
            "/fixture/" + output,
        )
        exported = object_value(json.loads((self.directory / output).read_text()))
        items = exported.get("items")
        if not isinstance(items, list):
            raise Failure("machine export did not return its value inventory")
        actual = {
            string_value(object_value(item).get("name")): object_value(item).get(
                "value"
            )
            for item in items
        }
        if actual != object_value(fixture.get("values")):
            raise Failure("published config or secret changed across the rehearsal")
        name = "pr-rehearsal-" + secrets.token_hex(4)
        created = object_value(
            json.loads(
                self.cli(
                    "env",
                    "create",
                    "--name",
                    name,
                    "--clone-from",
                    environment,
                    "-o",
                    "json",
                )
            )
        )
        identifier = string_value(object_value(created.get("environment")).get("id"))
        if created.get("uncopied_secrets") != []:
            raise Failure("preview clone did not copy all source secrets")
        value = "after-" + secrets.token_hex(4)
        staged = object_value(
            json.loads(
                self.cli(
                    "values",
                    "set",
                    "REHEARSAL_CONFIG",
                    "--env",
                    identifier,
                    "--stdin",
                    "-o",
                    "json",
                    data=value.encode(),
                )
            )
        )
        published = object_value(
            json.loads(
                self.cli(
                    "values",
                    "publish",
                    "--env",
                    identifier,
                    "--versions",
                    string_value(staged.get("version_id")),
                    "-o",
                    "json",
                )
            )
        )
        if not published.get("environments"):
            raise Failure("preview publication did not publish a revision")
        self.cli("pin", "list", "--env", identifier, "-o", "json")
        self.cli("env", "delete", identifier)
        print("fixture: persisted config/secret and machine preview lifecycle verified")
        self.verify_mcp_disabled()

    def verify_mcp_disabled(self):
        params = {
            "_meta": {
                "io.modelcontextprotocol/protocolVersion": "2026-07-28",
                "io.modelcontextprotocol/clientCapabilities": {},
                "io.modelcontextprotocol/clientInfo": {
                    "name": "issue-779-rehearsal",
                    "version": "1",
                },
            }
        }
        body = {"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": params}
        request = urllib.request.Request(
            self.origin + "/mcp",
            data=json.dumps(body).encode(),
            method="POST",
            headers={
                "Content-Type": "application/json",
                "Accept": "application/json, text/event-stream",
                "Mcp-Protocol-Version": "2026-07-28",
                "Mcp-Method": "tools/list",
            },
        )
        with self.http.open(request, timeout=30) as response:
            result = json.loads(response.read(2 * 1024 * 1024))
        tools = result.get("result", {}).get("tools")
        if not isinstance(tools, list) or not tools:
            raise Failure("MCP tool catalog absent")
        names = sorted(tool["name"] for tool in tools)
        if {"hikyo_stage_change", "hikyo_validate_change"}.intersection(names):
            raise Failure("MCP write tools unexpectedly enabled")
        self.write(
            "mcp-disabled-" + sys.argv[1] + ".json",
            json.dumps({"write_tools_absent": True, "tools": names}),
        )
        print("fixture: MCP enabled, both write tools absent")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, newurl):
        raise Failure("fixture refused an HTTP redirect")


if __name__ == "__main__":
    os.umask(0o077)
    try:
        fixture = Fixture()
        if sys.argv[1:] == ["seed"]:
            fixture.seed()
        elif sys.argv[1:] == ["verify"]:
            fixture.verify()
        else:
            raise Failure("usage: rehearsal-fixture.py seed|verify (runner hook only)")
    except (
        Failure,
        KeyError,
        ValueError,
        OSError,
        subprocess.SubprocessError,
    ) as error:
        # Unexpected third-party exceptions can contain secrets or responses.
        print(
            str(error)
            if isinstance(error, Failure)
            else "fixture failed; inspect private runner diagnostics",
            file=sys.stderr,
        )
        sys.exit(1)

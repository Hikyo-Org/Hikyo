#!/usr/bin/env python3
"""Synthetic, instance-bound Hikyo authorization tests recorded by OpenAEV.

prepare creates fresh lab fixtures; run executes bounded sequential requests.
Human administration stays outside OpenAEV. Only synthetic workload credentials
and canaries enter its stored inject content. Results never contain bodies/tokens.
"""

from __future__ import annotations

import argparse
import binascii
import importlib.util
import json
import os
import secrets
import stat
import sys
import uuid
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from types import ModuleType
from typing import Protocol
from urllib.parse import urlencode

import hikyo_reporting_audit as reporting_audit
import openaev_reporting as reporting

ROOT = Path(__file__).resolve().parents[2]
VERSION = "0.0.1-nightly.20261003.66.g5c3f969f"
LAN = "https://127.0.0.1:18081"
SERVICE = "https://hikyo.hikyo-lab.svc.cluster.local:8443"
RESUME_FAILED_CASE = "workload-human-plaintext-reveal"


def module(name: str, path: Path) -> ModuleType:
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError("lab_module_unavailable")
    result = importlib.util.module_from_spec(spec)
    sys.modules[name] = result
    spec.loader.exec_module(result)
    return result


pilot = module("hikyo_lab_pilot", Path(__file__).with_name("hikyo-pilot.py"))
platform = module("openaev_lab_pilot", Path(__file__).with_name("run-http-pilot.py"))


class SuiteError(RuntimeError):
    """Sanitized diagnostic: no credentials or response bodies."""


def obj(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or not all(isinstance(k, str) for k in value):
        raise SuiteError("invalid_object")
    return dict(value)


def text(value: dict[str, object], key: str) -> str:
    field = value.get(key)
    if not isinstance(field, str) or not field:
        raise SuiteError("invalid_" + key)
    return field


def rows(value: dict[str, object]) -> list[dict[str, object]]:
    items = value.get("items")
    if not isinstance(items, list) or value.get("count") != len(items):
        raise SuiteError("invalid_list")
    return [obj(item) for item in items]


def private(path: Path) -> dict[str, object]:
    return obj(json.loads(platform.private_file(path)))


def save(path: Path, value: dict[str, object], replace: bool = False) -> None:
    flags = os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW
    flags |= os.O_TRUNC if replace else os.O_EXCL
    fd = os.open(path, flags, 0o600)
    with os.fdopen(fd, "w") as handle:
        info = os.fstat(handle.fileno())
        if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600:
            raise SuiteError("state_file_permissions")
        json.dump(value, handle, indent=2)
        handle.write("\n")


def directory(path: Path, empty: bool = False) -> None:
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.lstat()
    if (
        not stat.S_ISDIR(info.st_mode)
        or info.st_uid != os.getuid()
        or stat.S_IMODE(info.st_mode) != 0o700
        or (empty and any(path.iterdir()))
    ):
        raise SuiteError("state_directory_requires_owned_0700")


class Admin:
    def __init__(self, admin_file: Path, ca: Path):
        self.credentials = private(admin_file)
        self.api = pilot.API(LAN, ca)
        self.login()

    def login(self) -> None:
        self.call(
            "POST",
            "/api/v1/auth/local/login",
            {
                "username": text(self.credentials, "username"),
                "password": text(self.credentials, "password"),
                "artifact": "cli",
            },
            expected=200,
        )
        self.assure()

    def assure(self, environment: str | None = None) -> None:
        pilot.wait_new_totp()
        body = {"code": pilot.totp(text(self.credentials, "totp_uri"))}
        path = "/api/v1/auth/totp/step-up"
        if environment is not None:
            body["environment_id"] = environment
            path = "/api/v1/auth/reauth/totp"
        self.call("POST", path, body, expected=200)

    def call(
        self,
        method: str,
        path: str,
        body: dict[str, object] | None = None,
        expected: int = 200,
    ) -> dict[str, object]:
        status, response = self.api.call(method, path, body)
        result = obj(response)
        if status != expected:
            error = result.get("error")
            domain = obj(error).get("code") if isinstance(error, dict) else "unknown"
            allowed = {
                "bad_request",
                "unauthenticated",
                "forbidden",
                "not_found",
                "conflict",
                "limit_exceeded",
                "unprocessable",
                "internal",
                "service_unavailable",
                "too_many_requests",
                "payload_too_large",
            }
            safe_domain = (
                domain if isinstance(domain, str) and domain in allowed else "unknown"
            )
            literals = {
                "api",
                "v1",
                "auth",
                "local",
                "login",
                "totp",
                "step-up",
                "reauth",
                "orgs",
                "projects",
                "environments",
                "settings",
                "grants",
                "machine-reveal",
                "keys",
                "values",
                "publish",
                "service-accounts",
                "credentials",
            }
            operation = (
                method
                + "_"
                + ".".join(
                    segment if segment in literals else "id"
                    for segment in path.split("?", 1)[0].split("/")
                    if segment
                )
            )
            raise SuiteError(
                f"lab_{operation}_http_{status}_expected_{expected}_domain_{safe_domain}"
            )
        return result

    def grant(
        self, env: str, principal: str, capability: str, remove: bool = False
    ) -> None:
        self.assure()
        if remove:
            self.call(
                "DELETE",
                env
                + "/grants?"
                + urlencode({"principal": principal, "capability": capability}),
                expected=204,
            )
        else:
            if capability == "reveal":
                self.assure(env.rsplit("/", 1)[1])
            self.call(
                "POST",
                env + "/grants",
                {"principal": principal, "capability": capability},
            )


def tamper(token: str) -> str:
    """Change a body character and recompute the native non-security CRC hint."""
    prefix = "hik_1_wl_"
    if not token.startswith(prefix) or len(token) < len(prefix) + 7:
        raise SuiteError("invalid_workload_token_grammar")
    body = token[len(prefix) : -6]
    changed = body[:-1] + ("A" if body[-1] != "A" else "B")
    checksum = binascii.crc32(changed.encode())
    alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
    suffix = ""
    for _ in range(6):
        suffix = alphabet[checksum % 62] + suffix
        checksum //= 62
    return prefix + changed + suffix


def identity(ca: Path, expected: str, release: str = VERSION) -> None:
    metadata = obj(pilot.API(LAN, ca).call("GET", "/api/v1/meta", expected=200)[1])
    if (
        metadata.get("instance_identity") != expected
        or metadata.get("server_version") != release
    ):
        raise SuiteError("lab_instance_or_release_mismatch")


def fixture_release(fixture: dict[str, object]) -> str:
    return text(fixture, "release") if "release" in fixture else VERSION


def prepare(args: argparse.Namespace) -> None:
    directory(args.state, empty=True)
    progress = args.state / "prepare-progress.json"

    def phase(name: str) -> None:
        print("PREPARE " + name, flush=True)
        save(progress, {"phase": name}, replace=progress.exists())

    phase("verify-instance")
    bound = private(args.pilot_config)
    ca = Path(text(bound, "ca_file"))
    instance = text(bound, "instance_identity")
    release = text(bound, "server_version") if "server_version" in bound else VERSION
    identity(ca, instance, release)
    phase("admin-login-and-step-up")
    admin = Admin(args.admin_file, ca)
    label = "security-" + uuid.uuid4().hex[:12]
    # The second tenant/project/environment are real existing resources.
    phase("create-synthetic-tenants")
    org = admin.call("POST", "/api/v1/orgs", {"name": label}, 201)
    admin.login()  # Organization creation invalidates its initiating session.
    org_path = "/api/v1/orgs/" + text(org, "id")
    other_org = admin.call("POST", "/api/v1/orgs", {"name": label + "-other"}, 201)
    admin.login()
    phase("create-projects-and-environments")
    project = admin.call("POST", org_path + "/projects", {"name": "fixture"}, 201)
    base = org_path + "/projects/" + text(project, "id")
    outside = admin.call("POST", org_path + "/projects", {"name": "outside"}, 201)
    other_base = org_path + "/projects/" + text(outside, "id")
    tenant_project = admin.call(
        "POST",
        "/api/v1/orgs/" + text(other_org, "id") + "/projects",
        {"name": "outside"},
        201,
    )
    tenant_base = (
        "/api/v1/orgs/"
        + text(other_org, "id")
        + "/projects/"
        + text(tenant_project, "id")
    )
    envs = []
    for owner, name in (
        (base, "staging"),
        (base, "outside"),
        (other_base, "outside"),
        (tenant_base, "outside"),
    ):
        created_env = admin.call("POST", owner + "/environments", {"name": name}, 201)
        envs.append(owner + "/environments/" + text(created_env, "id"))
    env = envs[0]
    phase("configure-fresh-synthetic-staging-window")
    # Genuine TOTP disclosure is supported only by a positive native window.
    # This applies solely to the fresh unprotected synthetic staging environment.
    if admin.call("GET", env + "/settings").get("protected") is not False:
        raise SuiteError("refusing_to_change_protected_environment")
    settings = admin.call(
        "PUT",
        env + "/settings",
        {"protected": False, "expected_protected": False, "reauth_window_seconds": 60},
    )
    if (
        settings.get("protected") is not False
        or settings.get("reauth_window_seconds") != 60
    ):
        raise SuiteError("synthetic_staging_window_not_60_seconds")
    phase("publish-synthetic-canaries")
    config_value = "synthetic-config-" + secrets.token_hex(12)
    secret_value = "synthetic-secret-" + secrets.token_hex(12)
    versions = []
    for name, classification, value in (
        ("LAB_CONFIG", "config", config_value),
        ("LAB_SECRET", "secret", secret_value),
    ):
        admin.call(
            "POST",
            base + "/keys",
            {
                "name": name,
                "classification": classification,
                "declaration": {"rule": {"type": "string"}},
            },
            201,
        )
        pending = admin.call("PUT", env + "/values/" + name, {"value": value})
        versions.append(text(pending, "version_id"))
    admin.call("POST", env + "/publish", {"version_ids": versions})
    phase("create-scoped-workload-credentials")
    accounts: dict[str, object] = {}
    for role in ("reader", "revealer"):
        account = admin.call(
            "POST", base + "/service-accounts", {"name": role, "kind": "workload"}, 201
        )
        principal = text(account, "principal_id")
        admin.grant(env, principal, "read")
        credential_path = (
            base + "/service-accounts/" + text(account, "id") + "/credentials"
        )
        credential = admin.call("POST", credential_path, {"lifetime_seconds": 3600})
        accounts[role] = {
            "principal": principal,
            "account_id": text(account, "id"),
            "token": text(credential, "value"),
            "credential_path": credential_path,
            "revoke_path": credential_path
            + "/"
            + text(obj(credential["credential"]), "id"),
        }
    phase("verify-machine-opt-in-refusal")
    revealer = obj(accounts["revealer"])
    if admin.call("GET", base + "/machine-reveal").get("enabled") is not False:
        raise SuiteError("fresh_project_machine_reveal_not_disabled")
    # Signed protocol demands a real disclosure ceremony even for grant refusal.
    admin.assure(env.rsplit("/", 1)[1])
    before = admin.call("GET", base + "/grants")
    if any(
        item.get("principal_id") == text(revealer, "principal")
        and item.get("capability") == "reveal"
        for item in rows(before)
    ):
        raise SuiteError("unexpected_preexisting_reveal_grant")
    refusal = admin.call(
        "POST",
        env + "/grants",
        {"principal": text(revealer, "principal"), "capability": "reveal"},
        409,
    )
    if obj(refusal.get("error")).get("code") != "conflict":
        raise SuiteError("opt_in_refusal_wrong_domain")
    if before != admin.call("GET", base + "/grants"):
        raise SuiteError("opt_in_refusal_changed_grants")
    # Workloads cannot hold edit/publish. Use a real admin-owned staged ID:
    # this probe checks artifact/capability admission before draft ownership.
    phase("stage-unpublished-version-and-expired-credential")
    reader = obj(accounts["reader"])
    pending = admin.call(
        "PUT",
        env + "/values/LAB_CONFIG",
        {"value": "synthetic-unpublished-" + secrets.token_hex(12)},
    )
    expired = admin.call(
        "POST", text(reader, "credential_path"), {"lifetime_seconds": 1}
    )
    fixture = {
        "format": "hikyo-openaev-security/v1",
        "instance_identity": instance,
        "release": release,
        "ca_file": str(ca),
        "admin_file": str(args.admin_file.resolve()),
        "base": base,
        "org_path": org_path,
        "env": env,
        "outside_envs": envs[1:],
        "config_value": config_value,
        "secret_value": secret_value,
        "accounts": accounts,
        "expired_token": text(expired, "value"),
        "expired_at": text(expired, "expires_at"),
        "publish_version": text(obj(pending), "version_id"),
        "opt_in_grant_refusal_status": 409,
        "opt_in_grants_unchanged": True,
        "synthetic_staging_reauth_window_seconds": 60,
    }
    save(args.state / "fixture.json", fixture)
    phase("complete")
    print("Prepared fresh synthetic fixture; no security probes executed.")


@dataclass(frozen=True)
class Case:
    name: str
    method: str
    path: str
    role: str
    status: int
    mode: str
    body: dict[str, object] | None = None
    observer: str | None = None


def assert_body(
    fixture: dict[str, object], mode: str, response: dict[str, object]
) -> None:
    raw = json.dumps(response)
    config, secret = text(fixture, "config_value"), text(fixture, "secret_value")
    if mode in ("not_found", "unauthenticated"):
        if obj(response.get("error")).get("code") != mode:
            raise SuiteError("wrong_denial_domain")
        if (
            "keys" in response
            or "secrets" in response
            or config in raw
            or secret in raw
        ):
            raise SuiteError("denial_disclosed_material")
        return
    keys = response.get("keys")
    if not isinstance(keys, list):
        raise SuiteError("delivery_missing_keys")
    if mode == "current":
        if response.get("current") is not True or keys:
            raise SuiteError("conditional_delivery_not_current")
        if config in raw or secret in raw:
            raise SuiteError("conditional_delivery_disclosed_value")
        return
    if response.get("current") is not False:
        raise SuiteError("unexpected_current_delivery")
    entries = [obj(item) for item in keys]
    by_name = {text(item, "name"): item for item in entries}
    expected_names = (
        {"LAB_CONFIG"} if mode == "config-only" else {"LAB_CONFIG", "LAB_SECRET"}
    )
    if set(by_name) != expected_names or len(entries) != len(by_name):
        raise SuiteError("delivery_key_set_mismatch")
    cfg = by_name["LAB_CONFIG"]
    if (
        cfg.get("classification") != "config"
        or cfg.get("presence") != "set"
        or cfg.get("value") != config
    ):
        raise SuiteError("config_delivery_mismatch")
    if mode == "config-only":
        if "LAB_SECRET" in raw or secret in raw:
            raise SuiteError("config_projection_disclosed_secret")
        return
    sec = by_name["LAB_SECRET"]
    if sec.get("classification") != "secret" or sec.get("presence") != "set":
        raise SuiteError("secret_presence_mismatch")
    if mode == "reveal":
        if sec.get("value") != secret:
            raise SuiteError("secret_reveal_mismatch")
    elif mode == "presence":
        if sec.get("value") is not None or secret in raw:
            raise SuiteError("read_only_disclosed_secret")
    else:
        raise SuiteError("unknown_assertion_mode")


def snapshot(admin: Admin, fixture: dict[str, object], observer: str) -> str:
    base, env = text(fixture, "base"), text(fixture, "env")
    if observer == "grants":
        listing = admin.call("GET", base + "/grants")
        values = rows(listing)
    elif observer == "accounts":
        values = [
            {key: item.get(key) for key in ("id", "name", "kind", "principal_id")}
            for item in rows(admin.call("GET", base + "/service-accounts"))
        ]
    elif observer == "credentials":
        account = obj(obj(fixture["accounts"])["reader"])
        values = [
            {key: item.get(key) for key in ("id", "kind", "revoked_at", "expires_at")}
            for item in rows(admin.call("GET", text(account, "credential_path")))
        ]
    elif observer == "projects":
        values = rows(admin.call("GET", text(fixture, "org_path") + "/projects"))
    elif observer == "published":
        reader = obj(obj(fixture["accounts"])["reader"])
        probe = pilot.API(LAN, Path(text(fixture, "ca_file")))
        response = obj(
            probe.call(
                "GET", env + "/delivery", token=text(reader, "token"), expected=200
            )[1]
        )
        keys = response.get("keys")
        if not isinstance(keys, list):
            raise SuiteError("observer_delivery_missing_keys")
        stable_keys = [
            {
                key: obj(item).get(key)
                for key in ("key_id", "name", "classification", "presence", "value")
            }
            for item in keys
        ]
        values = [
            {
                "revision": response.get("revision"),
                "schema_revision": response.get("schema_revision"),
                "keys": sorted(stable_keys, key=lambda item: text(item, "name")),
            }
        ]
    else:
        raise SuiteError("unknown_observer")
    return json.dumps(
        sorted(values, key=lambda row: json.dumps(row, sort_keys=True)), sort_keys=True
    )


def cases(f: dict[str, object], cursor: str) -> list[Case]:
    env, base = text(f, "env"), text(f, "base")
    accounts = obj(f["accounts"])
    reader = obj(accounts["reader"])
    outside = f["outside_envs"]
    if (
        not isinstance(outside, list)
        or len(outside) != 3
        or not all(isinstance(x, str) for x in outside)
    ):
        raise SuiteError("invalid_outside_resources")
    delivery = env + "/delivery"
    conditional = delivery + "?" + urlencode({"cursor": cursor})
    result = [
        Case("opt-in-off-presence", "GET", delivery, "revealer", 200, "presence"),
        Case("read-config-secret-presence", "GET", delivery, "reader", 200, "presence"),
        Case("read-and-reveal", "GET", delivery, "revealer", 200, "reveal"),
        Case(
            "config-only-projection",
            "GET",
            delivery + "?projection=config-only",
            "revealer",
            200,
            "config-only",
        ),
        Case("conditional-current", "GET", conditional, "revealer", 200, "current"),
    ]
    for name, path in zip(
        ("wrong-environment", "wrong-project", "wrong-tenant"), outside
    ):
        result.append(Case(name, "GET", path + "/delivery", "reader", 404, "not_found"))
    result.extend(
        [
            Case("expired-token", "GET", delivery, "expired", 401, "unauthenticated"),
            Case("missing-token", "GET", delivery, "anonymous", 401, "unauthenticated"),
            Case("tampered-token", "GET", delivery, "tampered", 401, "unauthenticated"),
            Case(
                "workload-human-plaintext-reveal",
                "POST",
                env + "/values/LAB_SECRET/reveal",
                "revealer",
                404,
                "not_found",
            ),
            Case(
                "self-grant-reveal",
                "POST",
                env + "/grants",
                "reader",
                404,
                "not_found",
                {"principal": text(reader, "principal"), "capability": "reveal"},
                "grants",
            ),
            Case(
                "create-service-account",
                "POST",
                base + "/service-accounts",
                "reader",
                404,
                "not_found",
                {"name": "forbidden-fixture-account", "kind": "workload"},
                "accounts",
            ),
            Case(
                "mint-own-credential",
                "POST",
                text(reader, "credential_path"),
                "reader",
                404,
                "not_found",
                {"lifetime_seconds": 3600},
                "credentials",
            ),
            Case(
                "create-project",
                "POST",
                text(f, "org_path") + "/projects",
                "reader",
                404,
                "not_found",
                {"name": "forbidden-fixture-project"},
                "projects",
            ),
            Case(
                "write-without-edit",
                "PUT",
                env + "/values/LAB_CONFIG",
                "reader",
                404,
                "not_found",
                {"value": "synthetic-denied-change"},
                "published",
            ),
            Case(
                "publish-without-publish",
                "POST",
                env + "/publish",
                "reader",
                404,
                "not_found",
                {"version_ids": [text(f, "publish_version")]},
                "published",
            ),
            Case(
                "withdraw-machine-opt-in",
                "GET",
                conditional,
                "revealer",
                200,
                "presence",
            ),
            Case("remove-reveal-grant", "GET", delivery, "revealer", 200, "presence"),
            Case("remove-read-grant", "GET", delivery, "revealer", 404, "not_found"),
            Case(
                "cursor-after-read-removal",
                "GET",
                conditional,
                "revealer",
                404,
                "not_found",
            ),
            Case("revoked-token", "GET", delivery, "reader", 401, "unauthenticated"),
            Case(
                "cursor-after-token-revocation",
                "GET",
                conditional,
                "reader",
                401,
                "unauthenticated",
            ),
        ]
    )
    return result


def validate_resume_report(
    fixture: dict[str, object], report: dict[str, object]
) -> list[dict[str, object]]:
    """Admit only the observed eleven-pass, one pre-dispatch failure prefix."""
    expected_report_fields = {
        "format",
        "instance_identity",
        "release",
        "started_at",
        "cases",
        "passed",
        "opt_in_grant_refusal_status",
        "opt_in_grants_unchanged",
        "failure",
    }
    if (
        set(report) != expected_report_fields
        or report.get("format") != "hikyo-openaev-security-results/v1"
        or fixture.get("format") != "hikyo-openaev-security/v1"
        or report.get("instance_identity") != fixture.get("instance_identity")
        or report.get("release") != VERSION
        or report.get("passed") is not False
        or report.get("failure") != "OpenAEVRecordingError"
        or report.get("opt_in_grant_refusal_status") != 409
        or fixture.get("opt_in_grant_refusal_status") != 409
        or report.get("opt_in_grants_unchanged") is not True
        or fixture.get("opt_in_grants_unchanged") is not True
        or fixture.get("synthetic_staging_reauth_window_seconds") != 60
    ):
        raise SuiteError("resume_report_binding_or_state_mismatch")
    try:
        started = datetime.fromisoformat(text(report, "started_at"))
    except ValueError:
        raise SuiteError("resume_started_at_invalid") from None
    if started.tzinfo is None or started.utcoffset() != datetime.now(UTC).utcoffset():
        raise SuiteError("resume_started_at_requires_utc")
    values = report.get("cases")
    if not isinstance(values, list) or len(values) != 12:
        raise SuiteError("resume_requires_exact_prefix_length")
    observed = [obj(item) for item in values]
    expected = cases(fixture, "resume-validation-cursor")
    if len(expected) != 24 or expected[11].name != RESUME_FAILED_CASE:
        raise SuiteError("resume_case_plan_changed")
    successful_fields = {
        "case",
        "passed",
        "expected_status",
        "name",
        "inject_id",
        "execution_recorded",
        "domain_assertion_passed",
        "http_status_recorded",
        "direct_status",
        "observer_unchanged",
    }
    seen = set()
    for item, case in zip(observed[:11], expected[:11]):
        if (
            set(item) != successful_fields
            or item.get("case") != case.name
            or item.get("name") != case.name
            or item.get("passed") is not True
            or item.get("expected_status") != case.status
            or item.get("direct_status") != case.status
            or item.get("execution_recorded") is not True
            or item.get("domain_assertion_passed") is not True
            or item.get("http_status_recorded") is not False
            or item.get("observer_unchanged") is not None
        ):
            raise SuiteError("resume_completed_case_proof_incomplete")
        inject = text(item, "inject_id")
        try:
            canonical = str(uuid.UUID(inject))
        except ValueError:
            raise SuiteError("resume_inject_id_invalid") from None
        if canonical != inject or inject in seen:
            raise SuiteError("resume_inject_id_invalid")
        seen.add(inject)
    failed = observed[11]
    if (
        set(failed) != {"case", "passed", "expected_status", "inject_id"}
        or failed.get("case") != RESUME_FAILED_CASE
        or failed.get("passed") is not False
        or failed.get("expected_status") != 404
    ):
        raise SuiteError("resume_failed_case_not_exact_pre_dispatch_candidate")
    failed_id = text(failed, "inject_id")
    try:
        if str(uuid.UUID(failed_id)) != failed_id or failed_id in seen:
            raise SuiteError("resume_failed_inject_id_invalid")
    except ValueError:
        raise SuiteError("resume_failed_inject_id_invalid") from None
    return observed[:11]


class LabProbe(Protocol):
    def call(
        self,
        method: str,
        path: str,
        body: dict[str, object] | None = None,
        token: str | None = None,
        expected: int | None = None,
    ) -> tuple[int, object]: ...


def verify_resume_native_state(
    admin: Admin, probe: LabProbe, fixture: dict[str, object]
) -> str:
    """Reprove positive native access without replaying fixture transitions."""
    base, env = text(fixture, "base"), text(fixture, "env")
    accounts = obj(fixture["accounts"])
    revealer, reader = obj(accounts["revealer"]), obj(accounts["reader"])
    if admin.call("GET", base + "/machine-reveal").get("enabled") is not True:
        raise SuiteError("resume_machine_opt_in_not_enabled")
    grants = rows(admin.call("GET", base + "/grants"))
    environment_id = env.rsplit("/", 1)[1]
    for account, capabilities in ((revealer, {"read", "reveal"}), (reader, {"read"})):
        held = {
            text(item, "capability")
            for item in grants
            if item.get("principal_id") == text(account, "principal")
            and obj(item.get("scope")).get("environment_id") == environment_id
        }
        if held != capabilities:
            raise SuiteError("resume_fixture_grants_changed")
    cursor: str | None = None
    for account, mode in ((revealer, "reveal"), (reader, "presence")):
        status, response = probe.call(
            "GET", env + "/delivery", token=text(account, "token")
        )
        if status != 200:
            raise SuiteError("resume_workload_credential_not_live")
        assert_body(fixture, mode, obj(response))
        if mode == "reveal":
            cursor = text(obj(response), "cursor")
    if cursor is None:
        raise SuiteError("resume_reveal_cursor_missing")
    return cursor


def run(args: argparse.Namespace) -> None:
    directory(args.state)
    fixture = private(args.state / "fixture.json")
    if fixture.get("format") != "hikyo-openaev-security/v1":
        raise SuiteError("unsupported_fixture")
    ca = Path(text(fixture, "ca_file"))
    resume = getattr(args, "resume_pre_dispatch_body_error", False)
    report_home = getattr(args, "report_home", False)
    if report_home and resume:
        raise SuiteError("home_reporting_requires_fresh_run")
    prior_report = private(args.state / "results.json") if resume else None
    prefix = (
        validate_resume_report(fixture, prior_report)
        if prior_report is not None
        else []
    )
    identity(ca, text(fixture, "instance_identity"), fixture_release(fixture))
    admin = Admin(Path(text(fixture, "admin_file")), ca)
    recorder_module = module(
        "openaev_lab_security", Path(__file__).with_name("openaev_security.py")
    )
    try:
        recorder = recorder_module.OpenAEVRecorder(
            args.platform_url, args.platform_token_file, ca
        )
    except recorder_module.OpenAEVRecordingError:
        raise SuiteError("recorder_initialization_failed") from None
    reporter: reporting.OpenAEVReporter | None = None
    asset_id: str | None = None
    if report_home:
        print("REPORTING configure-disposable-target-and-tenant-audit", flush=True)
        reporter = reporting.OpenAEVReporter(
            args.platform_url, args.platform_token_file, ca
        )
        verified_asset_id = reporter.ensure_asset()
        asset_id = verified_asset_id

        def reporting_config(
            name: str, payload: dict[str, object]
        ) -> dict[str, object]:
            return reporting.configure_payload(
                payload,
                name,
                verified_asset_id,
                detection_assessed=name in reporting_audit.DETECTION_OPERATIONS,
            )

        recorder = recorder_module.OpenAEVRecorder(
            args.platform_url,
            args.platform_token_file,
            ca,
            reporting_config=reporting_config,
        )
        principal = text(
            obj(admin.call("GET", "/api/v1/auth/whoami")["principal"]), "id"
        )
        addressed_orgs = {
            case.path.split("/projects/", 1)[0]
            for case in cases(fixture, "reporting-setup")
            if case.name in reporting_audit.DETECTION_OPERATIONS
        }
        for org_path in sorted(addressed_orgs):
            # This capability belongs to the human observer, never an injected
            # workload identity. A self-grant invalidates the acting session.
            admin.grant(org_path, principal, "audit-read")
            admin.login()
            admin.call("GET", org_path + "/audit?limit=1")
    accounts = obj(fixture["accounts"])
    reader, revealer = obj(accounts["reader"]), obj(accounts["revealer"])
    token = text(reader, "token")
    tampered = tamper(token)
    credentials = {
        "reader": token,
        "revealer": text(revealer, "token"),
        "expired": text(fixture, "expired_token"),
        "anonymous": None,
        "tampered": tampered,
    }
    env, base = text(fixture, "env"), text(fixture, "base")
    probe = pilot.API(LAN, ca)  # Never authenticate this object as a human.
    if prior_report is not None:
        prior_cases = prior_report.get("cases")
        if not isinstance(prior_cases, list):
            raise SuiteError("resume_requires_exact_prefix_length")
        failed_inject_id = text(obj(prior_cases[11]), "inject_id")
        try:
            verified = obj(
                recorder.verify_missing_body_failure(
                    failed_inject_id, RESUME_FAILED_CASE
                )
            )
        except recorder_module.OpenAEVRecordingError:
            raise SuiteError("resume_failed_execution_not_verified") from None
        if (
            verified.get("failure_verified") is not True
            or verified.get("inject_id") != failed_inject_id
            or verified.get("failure_code") != "blank_mandatory_body_before_dispatch"
        ):
            raise SuiteError("resume_failed_execution_not_verified")
        cursor = verify_resume_native_state(admin, probe, fixture)
        save(args.state / "results-before-missing-body-resume.json", prior_report)
        report = dict(prior_report)
        report.pop("failure")
        report["cases"] = prefix
        report["resumed_at"] = datetime.now(UTC).isoformat()
        report["prior_execution_failures"] = [
            {
                "case": RESUME_FAILED_CASE,
                "inject_id": failed_inject_id,
                "category": "openaev_pre_dispatch_validation",
                "hikyo_request_dispatched": False,
                "hikyo_finding": False,
                "verification": verified,
            }
        ]
    else:
        original = obj(
            probe.call(
                "GET", env + "/delivery", token=text(revealer, "token"), expected=200
            )[1]
        )
        cursor = text(original, "cursor")
        report = {
            "format": "hikyo-openaev-security-results/v1",
            "instance_identity": fixture["instance_identity"],
            "release": fixture_release(fixture),
            "started_at": datetime.now(UTC).isoformat(),
            "cases": [],
            "passed": False,
            "opt_in_grant_refusal_status": fixture["opt_in_grant_refusal_status"],
            "opt_in_grants_unchanged": fixture["opt_in_grants_unchanged"],
        }
    report_path = args.state / "results.json"
    save(report_path, report, replace=resume)
    results: list[dict[str, object]] = list(prefix)
    try:
        for case in cases(fixture, cursor)[len(prefix) :]:
            if case.name == "read-and-reveal":
                admin.assure()
                admin.call("PUT", base + "/machine-reveal", {"enabled": True})
                admin.grant(env, text(revealer, "principal"), "reveal")
                fresh = obj(
                    probe.call(
                        "GET",
                        env + "/delivery",
                        token=text(revealer, "token"),
                        expected=200,
                    )[1]
                )
                cursor = text(fresh, "cursor")
            # Regenerate conditional routes after the real grant widening.
            if "cursor=" in case.path:
                case = Case(
                    case.name,
                    case.method,
                    env + "/delivery?" + urlencode({"cursor": cursor}),
                    case.role,
                    case.status,
                    case.mode,
                    case.body,
                    case.observer,
                )
            if case.name == "withdraw-machine-opt-in":
                admin.assure()
                grants = snapshot(admin, fixture, "grants")
                admin.call("PUT", base + "/machine-reveal", {"enabled": False})
                if snapshot(admin, fixture, "grants") != grants:
                    raise SuiteError("opt_out_changed_grants")
            elif case.name == "remove-reveal-grant":
                admin.assure()
                admin.call("PUT", base + "/machine-reveal", {"enabled": True})
                admin.grant(env, text(revealer, "principal"), "reveal", remove=True)
            elif case.name == "remove-read-grant":
                still_current = obj(
                    probe.call(
                        "GET",
                        env + "/delivery?" + urlencode({"cursor": cursor}),
                        token=text(revealer, "token"),
                        expected=200,
                    )[1]
                )
                assert_body(fixture, "current", still_current)
                admin.grant(env, text(revealer, "principal"), "read", remove=True)
            elif case.name == "revoked-token":
                reader_view = obj(
                    probe.call(
                        "GET",
                        env + "/delivery",
                        token=text(reader, "token"),
                        expected=200,
                    )[1]
                )
                assert_body(fixture, "presence", reader_view)
                cursor = text(reader_view, "cursor")
                still_current = obj(
                    probe.call(
                        "GET",
                        env + "/delivery?" + urlencode({"cursor": cursor}),
                        token=text(reader, "token"),
                        expected=200,
                    )[1]
                )
                assert_body(fixture, "current", still_current)
                admin.assure()
                admin.call("DELETE", text(reader, "revoke_path"), expected=204)
            before = snapshot(admin, fixture, case.observer) if case.observer else None
            result: dict[str, object] = {
                "case": case.name,
                "passed": False,
                "expected_status": case.status,
            }
            results.append(result)
            report["cases"] = results
            print("RUN " + case.name, flush=True)
            recorded = recorder.record_case(
                case.name,
                case.method,
                case.path,
                credentials[case.role],
                case.body,
                lambda response, mode=case.mode: assert_body(
                    fixture, mode, obj(response)
                ),
            )
            result.update(recorded)
            result["passed"] = False
            # Fresh anonymous clients cannot inherit admin cookies/session token.
            client = pilot.API(LAN, ca)
            status, response = client.call(
                case.method, case.path, case.body, token=credentials[case.role]
            )
            result["direct_status"] = status
            if status != case.status:
                raise SuiteError("unexpected_direct_status")
            assert_body(fixture, case.mode, obj(response))
            if case.observer and snapshot(admin, fixture, case.observer) != before:
                raise SuiteError("denied_mutation_changed_observed_state")
            if (
                case.name == "withdraw-machine-opt-in"
                and text(obj(response), "cursor") == cursor
            ):
                raise SuiteError("opt_out_failed_to_invalidate_cursor")
            if case.name == "remove-reveal-grant":
                cursor = text(obj(response), "cursor")
            result["observer_unchanged"] = case.observer if case.observer else None
            result["passed"] = True
            save(report_path, report, replace=True)
            if reporter is not None and case.name in reporting.NEGATIVE_CASE_STATUS:
                audit: reporting.AuditEvidence | None = None
                if case.name in reporting_audit.DETECTION_OPERATIONS:
                    start, end = reporter.execution_window(text(result, "inject_id"))
                    assessed = reporting_audit.assess_native_audit(
                        admin.call,
                        inject_id=text(result, "inject_id"),
                        case_name=case.name,
                        target_path=case.path,
                        actor_id=text(obj(accounts[case.role]), "principal"),
                        window_start=start,
                        window_end=end,
                    )
                    audit = reporting.AuditEvidence(
                        assessed.inject_id,
                        assessed.case_name,
                        assessed.window_start,
                        assessed.window_end,
                        assessed.query_succeeded,
                        assessed.actor_scope_verified,
                        assessed.event_id,
                    )
                    result["native_audit"] = {
                        "event_id": assessed.event_id,
                        "event_type": assessed.event_type,
                        "window_start": assessed.window_start,
                        "window_end": assessed.window_end,
                    }
                result["home_reporting"] = reporter.publish_verdict(
                    text(result, "inject_id"),
                    case.name,
                    reporting.VerifiedOutcome(
                        text(result, "inject_id"),
                        case.name,
                        case.status,
                        status,
                        recorded.get("domain_assertion_passed") is True,
                        True,
                        state_asserted=case.observer is not None,
                    ),
                    audit_evidence=audit,
                )
                save(report_path, report, replace=True)
            print("PASS " + case.name + " HTTP " + str(status))
        report["passed"] = True
        if reporter is not None:
            if asset_id is None:
                raise SuiteError("home_reporting_asset_not_verified")
            report["home_reporting"] = {
                "asset_id": asset_id,
                "source_id": reporter.owner_id,
                "detection_scope": "seven authenticated tenant-audit denial cases",
                "reported_cases": sum("home_reporting" in result for result in results),
            }
        report["completed_at"] = datetime.now(UTC).isoformat()
        save(report_path, report, replace=True)
    except (
        SuiteError,
        platform.PilotError,
        recorder_module.OpenAEVRecordingError,
        SystemExit,
        ValueError,
        OSError,
        KeyError,
        TypeError,
        reporting.ReportingError,
        reporting_audit.NativeAuditError,
    ) as error:
        # Keep diagnostics bounded and sanitized; external exception text may
        # contain request paths or credentials, so record its type only.
        failed_inject = getattr(error, "inject_id", None)
        if isinstance(failed_inject, str) and results:
            results[-1]["inject_id"] = failed_inject
        report["failure"] = (
            str(error) if isinstance(error, SuiteError) else type(error).__name__
        )
        save(report_path, report, replace=True)
        raise SuiteError("suite_stopped_at_first_failed_assertion") from None


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "run"))
    parser.add_argument(
        "--report-home",
        action="store_true",
        help="Attach native metrics and publish independent verdicts during a fresh run",
    )
    parser.add_argument(
        "--resume-pre-dispatch-body-error",
        action="store_true",
        help="Resume only the verified case12 platform validation failure after11 completed cases",
    )
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument(
        "--pilot-config",
        type=Path,
        default=Path.home() / ".local/state/openaev-lab/hikyo-pilot/pilot.json",
    )
    parser.add_argument(
        "--admin-file",
        type=Path,
        default=Path.home() / ".local/state/openaev-lab/hikyo-pilot/admin.json",
    )
    parser.add_argument("--platform-url", default="https://127.0.0.1:18080")
    parser.add_argument(
        "--platform-token-file",
        type=Path,
        default=Path.home() / ".local/state/openaev-lab/openaev-token",
    )
    args = parser.parse_args()
    if args.resume_pre_dispatch_body_error and args.command != "run":
        parser.error("resume flag requires run command")
    if args.report_home and (
        args.command != "run" or args.resume_pre_dispatch_body_error
    ):
        parser.error("home reporting requires a fresh run command")
    try:
        prepare(args) if args.command == "prepare" else run(args)
    except (
        SuiteError,
        platform.PilotError,
        SystemExit,
        OSError,
        ValueError,
        KeyError,
        TypeError,
        reporting.ReportingError,
        reporting_audit.NativeAuditError,
    ) as error:
        reason = str(error) if isinstance(error, SuiteError) else type(error).__name__
        print(
            "FAIL: " + reason + "; review sanitized owner-only state.",
            file=sys.stderr,
        )
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()

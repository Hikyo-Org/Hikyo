"""Provision a separate native human audit collector for one disposable run.

Pinned native contract: Hikyo5c3f969f. Invitation without a role template creates
zero grants; credential establishment creates no session. Genuine local login,
TOTP enrolment/step-up and an environment-only audit-read grant establish the
collector. Only its session bearer enters the monitor config. Password/TOTP and
single-use invitation authority stay in a separate owner-only custody file.
"""

from __future__ import annotations

import argparse
import importlib.util
import os
import secrets
import sys
import uuid
from datetime import UTC, datetime
from pathlib import Path
from types import ModuleType

import openaev_security


def _suite() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "hikyo_detection_provision_suite", Path(__file__).with_name("security-suite.py")
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("security_suite_unavailable")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


suite = _suite()


class ProvisionError(RuntimeError):
    """Literal native preparation diagnostics without credentials or bodies."""


def obj(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ProvisionError("invalid_native_provision_object")
    return dict(value)


def text(value: dict[str, object], field: str) -> str:
    result = value.get(field)
    if not isinstance(result, str) or not result:
        raise ProvisionError("invalid_native_provision_field")
    return result


def grants(value: object, principal: str) -> list[dict[str, object]]:
    document = obj(value)
    items = document.get("items")
    if (
        not isinstance(items, list)
        or type(document.get("count")) is not int
        or document.get("count") != len(items)
    ):
        raise ProvisionError("native_grant_listing_invalid")
    rows = [obj(row) for row in items]
    return [row for row in rows if row.get("principal_id") == principal]


def verify_scope(
    tenant_listing: object,
    instance_listing: object,
    principal: str,
    target: str,
    invitation_grants: int,
) -> dict[str, object]:
    if invitation_grants != 0 or type(invitation_grants) is not int:
        raise ProvisionError("invitation_must_create_zero_grants")
    parts = target.removesuffix("/delivery").split("/")
    if (
        len(parts) != 9
        or parts[1:4] != ["api", "v1", "orgs"]
        or parts[5] != "projects"
        or parts[7] != "environments"
    ):
        raise ProvisionError("invalid_collector_target_scope")
    expected = {"org_id": parts[4], "project_id": parts[6], "environment_id": parts[8]}
    tenant = grants(tenant_listing, principal)
    instance = grants(instance_listing, principal)
    if (
        len(tenant) != 1
        or tenant[0].get("capability") != "audit-read"
        or obj(tenant[0].get("scope")) != expected
        or instance
    ):
        raise ProvisionError("collector_requires_exact_environment_audit_grant")
    return {
        "principal_id": principal,
        "target_path": target,
        "capability": "audit-read",
        "matching_tenant_grants": 1,
        "matching_instance_grants": 0,
        "invitation_grants_created": 0,
    }


def native(
    api: object,
    method: str,
    path: str,
    body: dict[str, object] | None = None,
    expected: int = 200,
) -> dict[str, object]:
    # The installed native pilot client owns TLS, no redirects/proxies, session
    # rotation and genuine TOTP protocol. Do not print its sensitive responses.
    call = getattr(api, "call", None)
    if not callable(call):
        raise TypeError("native_client_required")
    result = call(method, path, body)
    if not isinstance(result, tuple) or len(result) != 2:
        raise ProvisionError("native_response_shape_invalid")
    status, response = result
    if type(status) is not int or status != expected:
        raise ProvisionError("native_collector_operation_refused")
    return obj(response)


def session_identity(
    who: dict[str, object], principal: str, test_principal: str
) -> datetime:
    identity, session, capabilities = (
        obj(who.get(field)) for field in ("principal", "session", "capabilities")
    )
    assurance = obj(session.get("assurance"))
    factors = assurance.get("factors")
    if (
        identity.get("id") != principal
        or identity.get("kind") != "human"
        or principal == test_principal
        or session.get("artifact") != "cli"
        or capabilities.get("instance_operator") is not False
        or not isinstance(factors, list)
        or "totp" not in factors
        or who.get("enrolment_required") is True
    ):
        raise ProvisionError("independent_mfa_human_cli_session_required")
    try:
        expiry = min(
            datetime.fromisoformat(text(session, field).replace("Z", "+00:00"))
            for field in ("idle_expires_at", "absolute_expires_at")
        )
        if expiry.tzinfo is None or (expiry - datetime.now(UTC)).total_seconds() < 630:
            raise ValueError()
    except (ValueError, TypeError):
        raise ProvisionError("collector_session_requires_lease_margin") from None
    return expiry


def prepare(args: argparse.Namespace) -> dict[str, object]:
    suite.directory(args.state, empty=True)
    fixture_state: Path = (
        args.fixture_state if args.fixture_state is not None else args.state / "fixture"
    )
    if args.fixture_state is None:
        suite.prepare(
            argparse.Namespace(
                state=fixture_state,
                pilot_config=args.pilot_config,
                admin_file=args.admin_file,
            )
        )
    if any(
        (fixture_state / name).exists()
        for name in (
            "results.json",
            "adversarial-results.json",
            "detection-results.json",
        )
    ):
        raise ProvisionError("fresh_prepared_fixture_required")
    fixture = obj(suite.private(fixture_state / "fixture.json"))
    if fixture.get("format") != "hikyo-openaev-security/v1":
        raise ProvisionError("invalid_prepared_fixture")
    ca = Path(text(fixture, "ca_file"))
    suite.identity(ca, text(fixture, "instance_identity"), suite.fixture_release(fixture))
    outside = fixture.get("outside_envs")
    if (
        not isinstance(outside, list)
        or len(outside) != 3
        or not isinstance(outside[2], str)
    ):
        raise ProvisionError("wrong_tenant_fixture_required")
    target = outside[2] + "/delivery"
    openaev_security._target(target)
    target_env = target.removesuffix("/delivery")
    org_path = "/".join(target.split("/")[:5])
    admin = suite.Admin(Path(text(fixture, "admin_file")), ca)
    test_principal = text(
        obj(admin.call("GET", "/api/v1/auth/whoami")["principal"]), "id"
    )
    username = "lab-audit-" + secrets.token_hex(10)
    password = secrets.token_urlsafe(32)
    # Invitation carries no role template or implicit membership grant.
    invitation = obj(
        admin.call(
            "POST",
            org_path + "/invitations",
            {"username": username, "display_name": "Disposable lab audit collector"},
            expected=201,
        )
    )
    principal = text(invitation, "principal_id")
    initial = grants(admin.call("GET", org_path + "/grants"), principal)
    if initial:
        raise ProvisionError("invitation_must_create_zero_grants")
    custody_path = args.state / "collector-custody.json"
    custody: dict[str, object] = {
        "username": username,
        "password": password,
        "authority": text(invitation, "authority"),
        "principal_id": principal,
    }
    suite.save(custody_path, custody)
    api = suite.pilot.API(suite.LAN, ca)
    native(
        api,
        "POST",
        "/api/v1/auth/credential/establish",
        {"authority": text(invitation, "authority"), "password": password},
        expected=204,
    )
    native(
        api,
        "POST",
        "/api/v1/auth/local/login",
        {"username": username, "password": password, "artifact": "cli"},
    )
    enrolment = native(
        api, "POST", "/api/v1/auth/totp/enrol/start", {"password": password}
    )
    uri = text(enrolment, "otpauth_uri")
    custody["totp_uri"] = uri
    custody.pop("authority")
    suite.save(custody_path, custody, replace=True)
    suite.pilot.wait_new_totp()
    native(
        api, "POST", "/api/v1/auth/totp/enrol/confirm", {"code": suite.pilot.totp(uri)}
    )
    admin.grant(target_env, principal, "audit-read")
    scope = verify_scope(
        admin.call("GET", org_path + "/grants"),
        admin.call("GET", "/api/v1/instance/grants"),
        principal,
        target,
        len(initial),
    )
    # Changing native grants invalidates the old session. Reauthenticate with
    # the actual new account and a fresh genuine TOTP before any audit read.
    native(
        api,
        "POST",
        "/api/v1/auth/local/login",
        {"username": username, "password": password, "artifact": "cli"},
    )
    suite.pilot.wait_new_totp()
    native(api, "POST", "/api/v1/auth/totp/step-up", {"code": suite.pilot.totp(uri)})
    who = native(api, "GET", "/api/v1/auth/whoami")
    expiry = session_identity(who, principal, test_principal)
    native(api, "GET", target_env + "/audit?limit=1")
    token = getattr(api, "human_token", None)
    if not isinstance(token, str) or not token.startswith("hik_1_cli_"):
        raise ProvisionError("native_cli_bearer_required")
    config = {
        "format": "hikyo-lab-monitor/v1",
        "run_id": str(uuid.uuid4()),
        "instance_identity": text(fixture, "instance_identity"),
        "session_token": token,
        "collector_principal_id": principal,
        "test_principal_id": test_principal,
        "actor_id": text(obj(obj(fixture["accounts"])["reader"]), "principal"),
        "target_path": target,
        "session_expires_at": expiry.isoformat(),
        "lease_seconds": 600,
        "scope_proof": scope,
    }
    suite.save(args.state / "collector.json", config)
    suite.save(
        args.state / "provision-result.json",
        {
            "format": "hikyo-lab-monitor-provision/v1",
            "prepared": True,
            "fixture_state": str(fixture_state),
            "collector_config_file": str(args.state / "collector.json"),
            "collector_principal_id": principal,
            "scope_proof": scope,
        },
    )
    return config


def cleanup(args: argparse.Namespace) -> dict[str, object]:
    """Revoke only this collector session and its exact synthetic audit grant."""
    suite.directory(args.state)
    fixture_state: Path = (
        args.fixture_state if args.fixture_state is not None else args.state / "fixture"
    )
    fixture = obj(suite.private(fixture_state / "fixture.json"))
    config = obj(suite.private(args.state / "collector.json"))
    if (
        fixture.get("format") != "hikyo-openaev-security/v1"
        or config.get("format") != "hikyo-lab-monitor/v1"
    ):
        raise ProvisionError("invalid_cleanup_configuration")
    ca = Path(text(fixture, "ca_file"))
    instance = text(fixture, "instance_identity")
    if config.get("instance_identity") != instance:
        raise ProvisionError("cleanup_instance_binding_mismatch")
    suite.identity(ca, instance, suite.fixture_release(fixture))
    outside = fixture.get("outside_envs")
    if (
        not isinstance(outside, list)
        or len(outside) != 3
        or not isinstance(outside[2], str)
    ):
        raise ProvisionError("wrong_tenant_fixture_required")
    target = outside[2] + "/delivery"
    openaev_security._target(target)
    if config.get("target_path") != target or config.get("actor_id") != text(
        obj(obj(fixture["accounts"])["reader"]), "principal"
    ):
        raise ProvisionError("cleanup_scope_binding_mismatch")
    principal = text(config, "collector_principal_id")
    org_path = "/".join(target.split("/")[:5])
    admin = suite.Admin(Path(text(fixture, "admin_file")), ca)
    current_admin = text(
        obj(admin.call("GET", "/api/v1/auth/whoami")["principal"]), "id"
    )
    if current_admin != config.get("test_principal_id") or principal == current_admin:
        raise ProvisionError("cleanup_principal_binding_mismatch")
    tenant_listing = admin.call("GET", org_path + "/grants")
    instance_listing = admin.call("GET", "/api/v1/instance/grants")
    tenant, instance_grants = (
        grants(tenant_listing, principal),
        grants(instance_listing, principal),
    )
    if tenant or instance_grants:
        verify_scope(tenant_listing, instance_listing, principal, target, 0)
    api = suite.pilot.API(suite.LAN, ca)
    api.human_token = text(config, "session_token")
    session_status, session_response = api.call("GET", "/api/v1/auth/whoami")
    if session_status == 200:
        acting = obj(obj(session_response).get("principal"))
        session = obj(obj(session_response).get("session"))
        capabilities = obj(obj(session_response).get("capabilities"))
        if (
            acting.get("id") != principal
            or acting.get("kind") != "human"
            or session.get("artifact") != "cli"
            or capabilities.get("instance_operator") is not False
        ):
            raise ProvisionError("cleanup_collector_session_binding_mismatch")
    elif (
        session_status != 401
        or obj(obj(session_response).get("error")).get("code") != "unauthenticated"
    ):
        raise ProvisionError("cleanup_collector_session_not_verified")
    status, response = api.call("POST", "/api/v1/auth/logout")
    if status != 204 and (
        status != 401
        or obj(obj(response).get("error")).get("code") != "unauthenticated"
    ):
        raise ProvisionError("collector_logout_not_verified")
    if tenant:
        admin.grant(
            target.removesuffix("/delivery"), principal, "audit-read", remove=True
        )
    if grants(admin.call("GET", org_path + "/grants"), principal) or grants(
        admin.call("GET", "/api/v1/instance/grants"), principal
    ):
        raise ProvisionError("collector_grant_cleanup_not_verified")
    result: dict[str, object] = {
        "format": "hikyo-lab-monitor-cleanup/v1",
        "run_id": text(config, "run_id"),
        "collector_principal_id": principal,
        "logout_status": status,
        "audit_grants_remaining": 0,
        "instance_grants_remaining": 0,
        "completed_at": datetime.now(UTC).isoformat(),
        "verified": True,
    }
    output = args.state / "cleanup-result.json"
    suite.save(output, result, replace=output.exists())
    return result


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--fixture-state", type=Path)
    parser.add_argument(
        "--cleanup",
        action="store_true",
        help="Revoke only the collector session and exact lab environment audit grant",
    )
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
    args = parser.parse_args()
    try:
        cleanup(args) if args.cleanup else prepare(args)
    except (
        RuntimeError,
        OSError,
        ValueError,
        TypeError,
        KeyError,
        SystemExit,
    ) as error:
        print(
            "FAIL " + type(error).__name__ + "; retain private partial preparation",
            file=sys.stderr,
        )
        raise SystemExit(1) from None
    print(
        "Collector session/grant cleanup verified."
        if args.cleanup
        else "Prepared separate native audit collector; no detection exercise executed."
    )


if __name__ == "__main__":
    main()

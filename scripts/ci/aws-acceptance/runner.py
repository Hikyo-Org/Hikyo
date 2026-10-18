#!/usr/bin/env python3
"""Project private GitHub OIDC fixture inputs, never provision cloud resources.

The default-branch workflow and candidate are independently pinned by protected
environment variables. No fallback identities or cloud endpoints are accepted.
Only validated metadata from test2json reaches the evidence artifact. AWS CLI
responses, JWTs, descriptors, raw test output and exceptions are never printed.
"""

from __future__ import annotations

import base64
import datetime as dt
import math
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
import urllib.parse
import urllib.request


ACCOUNT = "099256073061"
REGION = "eu-central-1"
PREFIX = "hikyo-e2e-20261018a/"
REPOSITORY = "Hikyo-Org/Hikyo"
SUBJECT = (
    "repo:Hikyo-Org@316726515/Hikyo@1316165429:"
    "environment:hikyo-158-aws-sandbox"
)
AUDIENCE = "sts.amazonaws.com"
SUITE = "TestAWSSecretsManagerRealSmoke"
MODES = ("static", "ambient", "assume-role", "web-identity", "kms-denied")
ROLE_NAMES = {mode: "hikyo-158-" + mode for mode in MODES}
ROLE_NAMES.update({"assume-role": "hikyo-158-assume", "web-identity": "hikyo-158-web"})
ROLE_NAMES.update({name: "hikyo-158-" + name for name in ("source", "oracle", "cleanup")})
SHA = re.compile(r"[0-9a-f]{40}")
MAX_LINE = 65536
MAX_OUTPUT = 16 * 1024 * 1024
GROUPS = (
    "auth-boundaries", "lifecycle", "role-auth", "kms-denial", "iam-denial",
    "adoption-witness", "promoted-replay",
)
TEST_NAMES = {SUITE, *(SUITE + "/" + group for group in GROUPS)}
TEST_NAMES.update(SUITE + "/preflight-" + mode for mode in MODES)
TEST_NAMES.update(SUITE + "/auth-boundaries/expired-" + mode for mode in MODES if mode != "kms-denied")
TEST_NAMES.add(SUITE + "/auth-boundaries/configured-identity-refusal")
for group in ("lifecycle", "kms-denial"):
    TEST_NAMES.update(SUITE + "/" + group + "/" + engine for engine in ("sqlite", "postgres"))
for group, scenarios in (
    ("role-auth", ("ambient", "assume-role", "web-identity")),
    ("adoption-witness", ("version-before-adopt", "version-after-adopt", "replacement-after-adopt")),
    ("promoted-replay", ("finish-loss", "marker-refusal")),
):
    for scenario in scenarios:
        TEST_NAMES.add(SUITE + "/" + group + "/" + scenario)
        TEST_NAMES.update(SUITE + "/" + group + "/" + scenario + "/" + engine for engine in ("sqlite", "postgres"))


class Refused(Exception):
    """A failure whose raw cause must never enter the workflow log."""


def require(condition: object) -> None:
    if not condition:
        raise Refused()


def object_json(raw: bytes) -> dict:
    require(len(raw) <= MAX_LINE)
    value = json.loads(raw)
    require(isinstance(value, dict))
    return value


def timestamp(raw: str) -> float:
    value = dt.datetime.fromisoformat(raw.replace("Z", "+00:00"))
    require(value.tzinfo is not None and value.utcoffset() == dt.timedelta(0))
    return value.timestamp()


def role(actor: str) -> str:
    return f"arn:aws:iam::{ACCOUNT}:role/{ROLE_NAMES[actor]}"


def private_write(path: Path, data: bytes) -> None:
    require(not path.is_symlink())
    temporary = path.with_name(path.name + ".new")
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def private_json(path: Path, data: dict) -> None:
    private_write(path, json.dumps(data, separators=(",", ":")).encode())


def clean_environment() -> dict[str, str]:
    # Child tools do not inherit Actions bearer tokens, GitHub credentials,
    # AWS sources, proxy configuration, custom CAs, or unrelated test DSNs.
    return {key: os.environ[key] for key in ("PATH", "TMPDIR", "LANG") if key in os.environ}


def go_environment(custody: Path) -> dict[str, str]:
    # A clean Go process still needs writable caches and a workspace. Give it
    # private locations rather than inheriting a home directory, credential
    # stores, runner caches, or environment configuration from the host.
    env = clean_environment()
    for key, name in (("HOME", "build-home"), ("GOPATH", "go-path"),
                      ("GOMODCACHE", "go-modules"), ("GOCACHE", "go-cache"),
                      ("TMPDIR", "go-tmp")):
        path = custody / name
        path.mkdir(mode=0o700, exist_ok=True)
        require(not path.is_symlink() and path.is_dir() and path.stat().st_mode & 0o077 == 0)
        env[key] = str(path)
    env.update({"GOENV": "off", "GOTOOLCHAIN": "local", "GIT_TERMINAL_PROMPT": "0",
                "GOFLAGS": "-modcacherw"})
    return env


class Approval:
    def __init__(self, env: dict[str, str], now: float):
        self.candidate = env.get("AWS_ACCEPTANCE_CANDIDATE_SHA", "")
        self.workflow = env.get("GITHUB_SHA", "")
        self.run_id = env.get("GITHUB_RUN_ID", "")
        require(SHA.fullmatch(self.candidate) and SHA.fullmatch(self.workflow))
        require(self.candidate == env.get("AWS_ACCEPTANCE_APPROVED_SHA"))
        require(self.workflow == env.get("AWS_ACCEPTANCE_APPROVED_WORKFLOW_SHA"))
        require(self.run_id.isdecimal() and self.run_id == env.get("AWS_ACCEPTANCE_APPROVED_RUN_ID"))
        require(env.get("GITHUB_RUN_ATTEMPT") == "1")
        require(env.get("GITHUB_REPOSITORY") == REPOSITORY)
        require(env.get("GITHUB_REF") == "refs/heads/main")
        require(env.get("GITHUB_EVENT_NAME") == "workflow_dispatch")
        require(env.get("RUNNER_ENVIRONMENT") == "github-hosted")
        require(env.get("GITHUB_WORKFLOW_REF") == REPOSITORY + "/.github/workflows/aws-acceptance.yml@refs/heads/main")
        self.authorization = env.get("AWS_ACCEPTANCE_AUTHORIZATION_ID", "")
        require(re.fullmatch(r"[A-Za-z0-9._:/-]{1,128}", self.authorization))
        self.auth_expiry_raw = env.get("AWS_ACCEPTANCE_AUTH_EXPIRES_AT", "")
        self.auth_expiry = timestamp(self.auth_expiry_raw)
        self.cleanup_expiry = timestamp(env.get("AWS_ACCEPTANCE_CLEANUP_EXPIRES_AT", ""))
        require(now + 55 * 60 <= self.auth_expiry <= now + 2 * 3600)
        require(self.auth_expiry <= self.cleanup_expiry <= self.auth_expiry + 2 * 3600)
        self.kms = env.get("AWS_ACCEPTANCE_KMS_KEY_ARN", "")
        require(re.fullmatch(f"arn:aws:kms:{REGION}:{ACCOUNT}:key/[a-f0-9]{{8}}(?:-[a-f0-9]{{4}}){{3}}-[a-f0-9]{{12}}", self.kms))
        self.external_id = env.get("AWS_ACCEPTANCE_EXTERNAL_ID", "")
        require(re.fullmatch(r"[A-Za-z0-9_+=,.@:/-]{2,1224}", self.external_id))


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise Refused()


class Projection:
    def __init__(self, approval: Approval, custody: Path):
        self.approval = approval
        self.custody = custody
        self.stop = threading.Event()
        self.failed = threading.Event()
        self.child_stopped = True
        self.thread: threading.Thread | None = None
        self.env = clean_environment()
        empty = custody / "empty-config"
        private_write(empty, b"")
        self.env.update({
            "AWS_CONFIG_FILE": str(empty), "AWS_SHARED_CREDENTIALS_FILE": str(empty),
            "AWS_EC2_METADATA_DISABLED": "true", "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "true",
            "AWS_REGION": REGION, "AWS_DEFAULT_REGION": REGION,
            "AWS_MAX_ATTEMPTS": "1", "AWS_CLI_AUTO_PROMPT": "off",
        })

    def token(self, name: str) -> tuple[Path, float]:
        require(not self.stop.is_set())
        parsed = urllib.parse.urlsplit(os.environ["ACTIONS_ID_TOKEN_REQUEST_URL"])
        require(parsed.scheme == "https" and parsed.hostname is not None)
        require(parsed.hostname.endswith(".actions.githubusercontent.com"))
        require(parsed.username is None and parsed.password is None and parsed.port in (None, 443))
        query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
        query = [(key, value) for key, value in query if key != "audience"]
        query.append(("audience", AUDIENCE))
        url = urllib.parse.urlunsplit(parsed._replace(query=urllib.parse.urlencode(query)))
        request = urllib.request.Request(url, headers={
            "Authorization": "Bearer " + os.environ["ACTIONS_ID_TOKEN_REQUEST_TOKEN"],
        })
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        with opener.open(request, timeout=30) as response:
            raw = response.read(MAX_LINE + 1)
        require(not self.stop.is_set())
        token = object_json(raw).get("value")
        require(isinstance(token, str) and len(token) <= 32768)
        parts = token.split(".")
        require(len(parts) == 3)
        claims = object_json(base64.urlsafe_b64decode(parts[1] + "=" * (-len(parts[1]) % 4)))
        # Claim checks are an early refusal, not JWT signature verification.
        # AWS STS independently authenticates the original signed token.
        require(claims.get("iss") == "https://token.actions.githubusercontent.com")
        require(claims.get("aud") == AUDIENCE and claims.get("sub") == SUBJECT)
        require(claims.get("repository") == REPOSITORY)
        require(claims.get("repository_id") == "1316165429")
        require(claims.get("repository_owner_id") == "316726515")
        require(claims.get("sha") == self.approval.workflow)
        require(claims.get("run_id") == self.approval.run_id)
        require(claims.get("run_attempt") == "1" and claims.get("event_name") == "workflow_dispatch")
        expiry = claims.get("exp")
        require(isinstance(expiry, int) and time.time() + 90 < expiry <= time.time() + 1200)
        path = self.custody / name
        private_write(path, token.encode())
        return path, expiry

    def aws(self, arguments: list[str], credentials: dict | None = None) -> dict:
        require(not self.stop.is_set())
        env = self.env.copy()
        if credentials is not None:
            env.update({
                "AWS_ACCESS_KEY_ID": credentials["AccessKeyId"],
                "AWS_SECRET_ACCESS_KEY": credentials["SecretAccessKey"],
                "AWS_SESSION_TOKEN": credentials["SessionToken"],
            })
        result = subprocess.run(
            ["aws", "--region", REGION, "--no-cli-pager", "--output", "json", *arguments],
            env=env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=45, check=False,
        )
        require(result.returncode == 0 and not self.stop.is_set())
        return object_json(result.stdout)

    def session(self, actor: str, token: Path, duration: int) -> tuple[dict, float]:
        require(900 <= duration <= 14400 and actor != "assume-role")
        require(duration in (900, 3600) or actor == "cleanup")
        response = self.aws([
            "sts", "assume-role-with-web-identity", "--no-sign-request",
            "--role-arn", role(actor), "--role-session-name", "hikyo-158-" + self.approval.run_id,
            "--web-identity-token", "file://" + str(token), "--duration-seconds", str(duration),
        ])
        credentials = response.get("Credentials")
        require(isinstance(credentials, dict))
        for key in ("AccessKeyId", "SecretAccessKey", "SessionToken", "Expiration"):
            require(isinstance(credentials.get(key), str) and credentials[key])
        expiry = timestamp(credentials["Expiration"])
        require(time.time() + duration - 90 < expiry <= time.time() + duration + 90)
        identity = self.aws(["sts", "get-caller-identity"], credentials)
        require(identity.get("Account") == ACCOUNT)
        expected = f"arn:aws:sts::{ACCOUNT}:assumed-role/{ROLE_NAMES[actor]}/"
        arn = identity.get("Arn", "")
        require(isinstance(arn, str) and arn.startswith(expected) and "/" not in arn[len(expected):] and len(arn) > len(expected))
        descriptor = {"mode": "static", "region": REGION,
                      "access_key_id": credentials["AccessKeyId"],
                      "secret_access_key": credentials["SecretAccessKey"],
                      "session_token": credentials["SessionToken"]}
        require(len(json.dumps(descriptor).encode()) <= 4096)
        return descriptor, expiry

    def prepare(self) -> None:
        expiries = []
        provenance = {}
        for actor in ("static", "source", "ambient", "web-identity"):
            token, token_expiry = self.token(actor + "-expired.jwt")
            descriptor, session_expiry = self.session(actor, token, 900)
            if actor in ("static", "source"):
                private_json(self.custody / (actor + "-expired.json"), descriptor)
            expiries.extend((token_expiry, session_expiry))
            provenance[actor] = {"role_arn": role(actor), "token_expires_at": token_expiry,
                                 "session_expires_at": session_expiry,
                                 "authenticated_before_expiry": True}
        private_json(self.custody / "expiry-provenance.json", provenance)
        # STS sessions cannot be issued for less than 900 seconds. Never alter
        # signed JWT claims or use invalid tokens as a substitute for expiry.
        deadline = max(expiries) + 90
        require(deadline <= time.time() + 21 * 60)
        print("Waiting for authentic fixture sessions to expire.", flush=True)
        while time.time() < deadline:
            require(not self.stop.wait(min(30, deadline - time.time())))
        require(self.approval.auth_expiry >= time.time() + 32 * 60)
        for actor in ("static", "source", "kms-denied", "oracle", "cleanup"):
            token, _ = self.token(actor + "-fresh.jwt")
            duration = 3600
            if actor == "cleanup":
                duration = max(900, math.ceil(self.approval.cleanup_expiry - time.time()) + 90)
            descriptor, expiry = self.session(actor, token, duration)
            if actor == "cleanup":
                require(expiry > self.approval.cleanup_expiry)
            private_json(self.custody / (actor + ".json"), descriptor)
        private_json(self.custody / "ambient.json", {"mode": "ambient", "region": REGION})
        private_json(self.custody / "assume-role.json", {
            "mode": "assume-role", "region": REGION, "role_arn": role("assume-role"),
            "external_id": self.approval.external_id, "session_seconds": 900,
        })
        private_json(self.custody / "web-identity.json", {
            "mode": "web-identity", "region": REGION, "role_arn": role("web-identity"),
            "session_seconds": 900,
        })
        self.refresh_tokens()
        writers = {}
        for actor in MODES:
            entry = {"credential_file": actor + ".json", "principal_arn": role(actor)}
            if actor == "static":
                entry["expired_credential_file"] = "static-expired.json"
            elif actor == "assume-role":
                entry.update({"source_file": "source.json", "expired_source_file": "source-expired.json",
                              "source_principal_arn": role("source")})
            elif actor in ("ambient", "web-identity"):
                entry.update({"token_file": actor + ".jwt", "expired_token_file": actor + "-expired.jwt"})
                if actor == "ambient":
                    entry["workload_role_arn"] = role(actor)
            writers[actor] = entry
        manifest = {
            "account": ACCOUNT, "region": REGION, "prefix": PREFIX, "budget_usd": 1,
            "authorization_id": self.approval.authorization, "kms_key_arn": self.approval.kms,
            "expires_at": self.approval.auth_expiry_raw, "writers": writers,
            "oracle": {"credential_file": "oracle.json", "principal_arn": role("oracle")},
            "cleanup": {"credential_file": "cleanup.json", "principal_arn": role("cleanup")},
        }
        private_json(self.custody / "manifest.json", manifest)

    def refresh_tokens(self) -> None:
        for actor in ("ambient", "web-identity"):
            self.token(actor + ".jwt")

    def start_refresh(self) -> None:
        def project():
            while not self.stop.wait(60):
                try:
                    self.refresh_tokens()
                except Exception:
                    self.failed.set()
                    return
        self.thread = threading.Thread(target=project, daemon=True)
        self.thread.start()


class Evidence:
    def __init__(self, path: Path, approval: Approval):
        self.path = path
        self.data = {"schema": "hikyo.aws-acceptance/v1", "candidate_sha": approval.candidate,
                     "workflow_sha": approval.workflow, "run_id": approval.run_id,
                     "status": "preparing", "stage": "compile", "account": ACCOUNT, "region": REGION, "prefix": PREFIX,
                     "recorded_arns": [], "verified_absent_arns": [], "unresolved_names": [],
                     "tests": {}, "maximum_create_reservations": 32,
                     "creation_reservations": [], "complete_creation_ledger": False}
        self.save()

    def save(self) -> None:
        private_json(self.path, self.data)

    def event(self, event: dict) -> None:
        action, test = event.get("Action"), event.get("Test", "")
        if isinstance(test, str) and test in TEST_NAMES:
            if action in ("pass", "fail", "skip"):
                require(len(self.data["tests"]) < 1024)
                self.data["tests"][test] = action
        output = event.get("Output", "")
        require(isinstance(output, str) and len(output) <= MAX_LINE)
        # Only exact known fixture diagnostics are accepted, never general
        # provider messages, raw JSON, panic text or arbitrary test output.
        arn_pattern = re.escape(f"arn:aws:secretsmanager:{REGION}:{ACCOUNT}:secret:{PREFIX}") + r"[A-Za-z0-9/_+=.@-]{1,450}-[A-Za-z0-9]{6}"
        for label, key in (("recorded synthetic AWS resource: ", "recorded_arns"),
                           ("verified synthetic AWS resource absent: ", "verified_absent_arns")):
            match = re.fullmatch(r"\s+aws_real_fixture_test.go:[0-9]+: " + re.escape(label) + "(" + arn_pattern + r")\n?", output)
            if match and match[1] not in self.data[key]:
                require(len(self.data[key]) < 32)
                self.data[key].append(match[1])
        match = re.fullmatch(r"\s+aws_real_fixture_test.go:[0-9]+: reserved synthetic AWS create: ([0-9]+) (" + re.escape(PREFIX) + r"[A-Za-z0-9/_+=.@-]{1,450})\n?", output)
        if match:
            require(int(match[1]) == len(self.data["creation_reservations"]) + 1 <= 32)
            self.data["creation_reservations"].append(match[2])
        match = re.fullmatch(r"\s+aws_real_fixture_test.go:[0-9]+: unresolved synthetic AWS create; reconcile name (" + re.escape(PREFIX) + r"[A-Za-z0-9/_+=.@-]{1,450}) before rerunning\n?", output)
        if match and match[1] not in self.data["unresolved_names"]:
            require(len(self.data["unresolved_names"]) < 32)
            self.data["unresolved_names"].append(match[1])
        self.save()

    def finish(self, exit_code: int, projection_ok: bool) -> bool:
        tests = self.data["tests"]
        passed = exit_code == 0 and projection_ok and tests.get(SUITE) == "pass"
        passed = passed and all(tests.get(name) == "pass" for name in TEST_NAMES)
        passed = passed and not any(status in ("skip", "fail") for status in tests.values())
        passed = passed and bool(self.data["recorded_arns"]) and not self.data["unresolved_names"]
        passed = passed and set(self.data["recorded_arns"]) == set(self.data["verified_absent_arns"])
        reservations = self.data["creation_reservations"]
        arn_prefix = f"arn:aws:secretsmanager:{REGION}:{ACCOUNT}:secret:"
        passed = passed and bool(reservations) and all(arn[len(arn_prefix):-7] in reservations for arn in self.data["recorded_arns"])
        self.data["complete_creation_ledger"] = passed
        self.data["status"] = "passed" if passed else "failed-reconcile-before-rerun"
        self.save()
        return passed


def stop_suite_process(process: subprocess.Popen) -> None:
    # The session contains test2json, the suite and any replay children. Always
    # kill the remaining group, even if its leader exits first. A selector or
    # evidence failure must never release ownership of an AWS-capable child.
    failed = False
    try:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        except OSError:
            failed = True
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            pass
        except OSError:
            failed = True
    finally:
        try:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except OSError:
                failed = True
            try:
                process.wait(timeout=5)
            except (subprocess.TimeoutExpired, OSError):
                failed = True
        finally:
            if process.stdout is not None:
                try:
                    process.stdout.close()
                except OSError:
                    failed = True
    require(not failed)


def run_suite(binary: Path, candidate: Path, projection: Projection, evidence: Evidence) -> bool:
    env = go_environment(projection.custody)
    env.update({"TMPDIR": str(projection.custody), "HIKYO_TEST_AWS_REAL_REQUIRED": "1",
                "HIKYO_TEST_AWS_REAL_MUTATIONS": "approved",
                "HIKYO_TEST_AWS_REAL_MANIFEST": str(projection.custody / "manifest.json"),
                "HIKYO_TEST_POSTGRES_DSN": "postgres://hikyo:hikyo@127.0.0.1:5432/hikyo_test?sslmode=disable"})
    # test2json receives the verbose protocol in a pipe, never a log file.
    command = ["go", "tool", "test2json", "-p", "github.com/Hikyo-Org/hikyo/internal/isolation",
               str(binary), "-test.v=test2json", "-test.run=^" + SUITE + "$",
               "-test.count=1", "-test.parallel=1", "-test.timeout=30m"]
    process = None
    selector = None
    try:
        require(not projection.stop.is_set())
        process = subprocess.Popen(command, cwd=candidate, env=env, stdout=subprocess.PIPE,
                                   stderr=subprocess.DEVNULL, start_new_session=True)
        projection.child_stopped = False
        require(process.stdout is not None and not projection.stop.is_set())
        total = 0
        buffer = b""
        deadline = time.monotonic() + 32 * 60
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ)
        evidence.data["status"] = "running"
        evidence.data["stage"] = "real-suite"
        evidence.save()
        while selector.get_map():
            require(time.monotonic() < deadline and not projection.failed.is_set() and not projection.stop.is_set())
            for key, _ in selector.select(timeout=1):
                chunk = os.read(key.fileobj.fileno(), 8192)
                if not chunk:
                    selector.unregister(key.fileobj)
                    continue
                total += len(chunk)
                require(total <= MAX_OUTPUT)
                buffer += chunk
                while b"\n" in buffer:
                    line, buffer = buffer.split(b"\n", 1)
                    evidence.event(object_json(line))
                require(len(buffer) <= MAX_LINE)
        require(not buffer)
        return evidence.finish(process.wait(timeout=5), not projection.failed.is_set())
    finally:
        # Child termination and pipe closure precede optional selector cleanup.
        # Even selector creation/register/close and evidence persistence may fail.
        try:
            if process is not None:
                stop_suite_process(process)
                projection.child_stopped = True
        finally:
            if selector is not None:
                selector.close()


def main() -> int:
    os.umask(0o077)
    require(len(sys.argv) == 2 and sys.argv[1] in ("check", "run"))
    approval = Approval(dict(os.environ), time.time())
    if sys.argv[1] == "check":
        print("Exact workflow, candidate and single-run approvals verified.")
        return 0
    require(os.geteuid() != 0)
    runner_temp = Path(os.environ["RUNNER_TEMP"]).resolve(strict=True)
    workspace = Path(os.environ["GITHUB_WORKSPACE"]).resolve(strict=True)
    candidate = workspace / "acceptance-candidate"
    head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=candidate, env=clean_environment(),
                          stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=10, check=True)
    require(head.stdout.decode().strip() == approval.candidate)
    evidence_dir = runner_temp / "aws-acceptance-evidence"
    evidence_dir.mkdir(mode=0o700, exist_ok=False)
    evidence = Evidence(evidence_dir / "evidence.json", approval)
    custody = Path(tempfile.mkdtemp(prefix="aws-acceptance-private-", dir=runner_temp))
    projection = Projection(approval, custody)
    # A signal only requests cancellation. Raising from this handler could
    # interrupt Popen after fork but before its handle was assigned. The owned
    # child loop and credential projection observe the flag and refuse safely.
    def interrupted(_signal, _frame):
        projection.stop.set()
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        binary = custody / "isolation.test"
        print("Compiling the exact reviewed candidate without AWS sources.", flush=True)
        result = subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/isolation"],
                                cwd=candidate, env=go_environment(custody), stdout=subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL, timeout=10 * 60, check=False)
        require(result.returncode == 0)
        evidence.data["stage"] = "private-auth-projection"
        evidence.save()
        projection.prepare()
        projection.start_refresh()
        print("Running real AWS acceptance with private projection; raw output is discarded.", flush=True)
        require(run_suite(binary, candidate, projection, evidence))
        print("Both-engine AWS suite and recorded-resource cleanup passed.", flush=True)
        return 0
    except Exception:
        evidence.data["status"] = "failed-reconcile-before-rerun"
        evidence.save()
        raise Refused() from None
    finally:
        projection.stop.set()
        if projection.thread is not None:
            projection.thread.join(timeout=35)
        # Only the explicit metadata artifact survives. No private session,
        # token, test datastore or test output is ever uploaded.
        # Keep the nonsecret single-use marker after process exit. The
        # environment also binds this authorization to one GitHub run ID.
        if projection.child_stopped:
            for path in custody.iterdir():
                if re.fullmatch(r"\.hikyo-aws-run-[0-9a-f]{64}\.used", path.name) and path.is_file() and not path.is_symlink():
                    continue
                if path.is_dir() and not path.is_symlink():
                    shutil.rmtree(path)
                else:
                    path.unlink()
        else:
            print("Suite termination unconfirmed; private custody retained for reconciliation.", file=sys.stderr)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        print("AWS acceptance refused or failed. Inspect only validated evidence; reconcile before rerunning.", file=sys.stderr)
        sys.exit(1)

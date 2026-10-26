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

import historical_cleanup


ACCOUNT = "099256073061"
REGION = "eu-central-1"
RUN_PREFIX = re.compile(r"hikyo-e2e-[a-z0-9]{8,32}/")
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
MAX_DIAGNOSTICS = 128
DIAGNOSTIC_OPERATIONS = (
    "DescribeSecret", "CreateSecret", "PutSecretValue", "UpdateSecretVersionStage",
    "TagResource", "DeleteSecret", "RestoreSecret", "GetSecretValue",
)
DIAGNOSTIC_CODES = (
    "AccessDeniedException", "DecryptionFailure", "EncryptionFailure", "ExpiredToken",
    "ExpiredTokenException", "InvalidClientTokenId", "InvalidParameterException",
    "InvalidRequestException", "LimitExceededException", "ResourceExistsException",
    "ResourceNotFoundException", "ThrottlingException", "UnrecognizedClientException", "other",
)
DIAGNOSTIC_PATTERN = re.compile(
    r"[ \t]+aws_real_diagnostics_test\.go:[1-9][0-9]*: synthetic AWS diagnostic: ("
    + "|".join(DIAGNOSTIC_OPERATIONS) + r") (" + "|".join(DIAGNOSTIC_CODES) + r")\n?"
)
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


# Closed literal failure messages from the exact reviewed real-AWS fixture.
# Never retain free text, dynamic t.Fatal(err), values or credential material.
FAILURE_ASSERTIONS = {'AWS audit contains protected material': 'assertion-001', 'AWS canonical JSON or per-key delivery differs from source': 'assertion-002', 'AWS consent plan unavailable': 'assertion-003', 'AWS destination change bypassed the durable move workflow': 'assertion-004', 'AWS destination changed before its old names were scrubbed': 'assertion-005', 'AWS destination move was refused': 'assertion-006', "AWS did not deny a writer's synthetic value read": 'assertion-007', 'AWS did not enforce the cross-writer namespace mutation deny': 'assertion-008', 'AWS fixture client configuration refused': 'assertion-009', 'AWS fixture descriptor must match the approved region and use AWS STS': 'assertion-010', 'AWS fixture has not recorded the expected resource': 'assertion-011', 'AWS fixture identity failed or does not match the explicitly approved sandbox principal': 'assertion-012', 'AWS fixture metadata read failed': 'assertion-013', 'AWS metadata client failed': 'assertion-014', 'AWS move did not activate the new generation and destination': 'assertion-015', 'AWS replacement did not change the complete ARN': 'assertion-016', 'AWS scheduled deletion state differs from expected lifecycle': 'assertion-017', 'Finish fault did not retain exact promoted version and unresolved fence': 'assertion-018', 'Finish fault lost Owned custody': 'assertion-019', 'Finish fault preceded expected wire delivery': 'assertion-020', 'Finish recovery changed job, witness or exact single delivered version': 'assertion-021', 'Finish recovery lost unfinished-intent reconciliation': 'assertion-022', 'Finish-loss admission accepted the wrong interruption or error': 'assertion-023', 'IAM negative credential source unavailable': 'assertion-024', 'IAM negative namespaces are not distinct': 'assertion-025', 'IAM negative run window expired': 'assertion-026', 'IAM negative temporary credentials unavailable': 'assertion-027', 'IAM synthetic seed failed': 'assertion-028', 'KMS refusal recorded a successful value write': 'assertion-029', 'STS auth boundaries consumed a creation or mutation reservation': 'assertion-030', 'STS did not enforce the exact external ID trust condition': 'assertion-031', 'STS expiry classification lost its service-code binding': 'assertion-032', 'adoption replay did not converge exactly once': 'assertion-033', 'assume-role requires an explicit approved source and destination': 'assertion-034', 'auth boundary preflight failed before mutations': 'assertion-035', 'bounded receipt acceptance differed from exact metadata contract': 'assertion-036', 'cannot bind IAM negative identity': 'assertion-037', 'cannot create local wire client': 'assertion-038', 'cannot create replay metadata pipe': 'assertion-039', 'cannot derive negative role fixture': 'assertion-040', 'cannot encode bounded replay input': 'assertion-041', 'cannot encode local test descriptor': 'assertion-042', 'cannot encode negative role fixture': 'assertion-043', 'cannot encode region refusal fixture': 'assertion-044', 'cannot encode synthetic descriptor': 'assertion-045', 'cannot identify owned SQLite fixture': 'assertion-046', 'cannot inspect ordinary target representation': 'assertion-047', 'cannot isolate AWS config': 'assertion-048', 'cannot locate owned SQLite fixture': 'assertion-049', 'cannot locate the running test executable': 'assertion-050', 'cannot retain original fixture root for process replay': 'assertion-051', 'cannot return replay metadata receipt': 'assertion-052', 'cannot verify old synthetic ARN absence': 'assertion-053', 'cleanup seed bypassed the shared creation budget': 'assertion-054', 'completed markers were accepted as the intentional refusal state': 'assertion-055', 'concurrent AWS edit injection did not execute': 'assertion-056', 'configured account mismatch reached provider metadata or was accepted': 'assertion-057', 'could not install authentic expired fixture credential': 'assertion-058', 'creation guard is not atomic across the full suite': 'assertion-059', 'credential revocation requires exact existing owned custody': 'assertion-060', 'cross-process aggregation lost distinct-version proof': 'assertion-061', 'customer KMS key was not applied': 'assertion-062', 'delivery proof accepted missing, duplicate or foreign evidence': 'assertion-063', 'descriptor region mismatch survived construction': 'assertion-064', 'durable AWS adoption refused': 'assertion-065', 'durable AWS target did not converge': 'assertion-066', 'durable AWS target inspection failed': 'assertion-067', 'durable INTENT/OUTCOME linkage is missing': 'assertion-068', 'durable resync refused': 'assertion-069', 'durable target removal refused': 'assertion-070', 'exact current version did not contain the expected synthetic value': 'assertion-071', 'expected role session refused': 'assertion-072', 'expired auth fixture did not reach credential acquisition': 'assertion-073', 'expired credential did not fail the actual durable job loudly': 'assertion-074', 'expired credential mutated the synthetic destination': 'assertion-075', 'expired fixture requires a separate protected temporary static session': 'assertion-076', 'expired source was not a product auth refusal': 'assertion-077', 'expired workload fixture requires a separate protected token': 'assertion-078', 'explicit STS expiry was not recognized': 'assertion-079', 'explicit version consent failed': 'assertion-080', 'external conflict seed changed identity or approved version consent': 'assertion-081', 'external current version unavailable': 'assertion-082', 'external edit was not preserved as a loud conflict': 'assertion-083', 'external synthetic edit failed': 'assertion-084', 'failed create refunded its reservation or invented owned custody': 'assertion-085', 'fixture rejected exactly one new version because labeled count stayed constant': 'assertion-086', 'foreign name reserved': 'assertion-087', 'fresh fixture credential replacement failed': 'assertion-088', 'fresh process failed to close the crash window and finish the same job': 'assertion-089', 'fresh real adoption did not recover exactly once': 'assertion-090', 'generic OIDC refusal masqueraded as expiry': 'assertion-091', 'hard process exit did not retain exact owned custody and unfinished intent': 'assertion-092', 'import granted new creation authority': 'assertion-093', 'import retained mutable input aliases': 'assertion-094', 'intentional marker refusal was confused with completed delivery': 'assertion-095', 'interrupted adoption did not preserve its approved predecessor': 'assertion-096', 'interruption lost pending Owned custody': 'assertion-097', 'invalid IAM negative descriptor': 'assertion-098', 'invalid JSON budget accepted': 'assertion-099', 'invalid approved auth fixture': 'assertion-100', 'invalid approved region fixture': 'assertion-101', 'invalid bounded replay input': 'assertion-102', 'invalid creation identity entered cleanup custody': 'assertion-103', 'invalid custody import accepted': 'assertion-104', 'invalid original replay custody reservations': 'assertion-105', 'invalid protected expired descriptor': 'assertion-106', 'invalid replay authorization or custody': 'assertion-107', 'invalid replay crash resource identity': 'assertion-108', 'invalid replay datastore engine': 'assertion-109', 'invalid replay input shape': 'assertion-110', 'invalid replay metadata receipt': 'assertion-111', 'invalid replay proof resource identity': 'assertion-112', 'local diagnostic client unavailable': 'assertion-113', 'manifest decoder accepted or disclosed invalid input': 'assertion-114', 'manual consent client unavailable': 'assertion-115', 'metadata-only replay lost exact single-write evidence': 'assertion-116', 'missing replay metadata pipe': 'assertion-117', 'moved AWS destination did not receive the current selected source': 'assertion-118', 'multi-target retain deleted a remote secret': 'assertion-119', 'multi-target retain kept active custody': 'assertion-120', 'mutation accepted an unrecorded resource': 'assertion-121', 'mutation outlived its authorization': 'assertion-122', 'negative role fixture did not reach STS': 'assertion-123', 'nonprivate fixture directory accepted': 'assertion-124', 'old synthetic ARN deletion did not finish in time': 'assertion-125', 'oracle and cleanup credentials are not separate': 'assertion-126', 'oracle and cleanup require protected temporary static descriptors': 'assertion-127', 'oracle did not receive its first read': 'assertion-128', 'oracle did not stop after cancellation': 'assertion-129', 'oracle did not stop at the first visible current value': 'assertion-130', 'oracle followed an unrecorded ARN': 'assertion-131', 'oracle ignored cancellation or sent another read': 'assertion-132', 'oracle masked a mismatched value': 'assertion-133', 'oracle masked a permanent refusal or persistent invisibility': 'assertion-134', 'oracle value assertion changed': 'assertion-135', 'ordinary target read exposed credentials': 'assertion-136', 'original custody did not survive import': 'assertion-137', 'oversized fixture accepted': 'assertion-138', 'owned SQLite fixture path is missing': 'assertion-139', 'partial conflict seed failed': 'assertion-140', 'partial conflict wrote an unapproved or duplicate version': 'assertion-141', 'partial retry duplicated the successful sibling or failed the consented key': 'assertion-142', 'pause failed': 'assertion-143', 'paused target changed provider state': 'assertion-144', 'positive synthetic oracle control failed': 'assertion-145', 'process replay created a duplicate AWS version': 'assertion-146', 'process replay did not retain exactly one distinct write token': 'assertion-147', 'process replay requires existing exact owned custody': 'assertion-148', 'process replay requires the original signed admission material': 'assertion-149', 'projected workload token is empty': 'assertion-150', 'protected-material audit query failed': 'assertion-151', 'provider mutations continued after actual credential revocation': 'assertion-152', 'real AWS acceptance requires both datastore engines': 'assertion-153', 'real AWS adapter creation failed': 'assertion-154', 'real AWS durable worker failed': 'assertion-155', 'real AWS mutations are not authorized; set the mutation switch only after the sandbox scope is approved': 'assertion-156', 'real AWS target creation failed': 'assertion-157', 'real AWS worker did not drain within its bounded attempts': 'assertion-158', 'real KMS denial was not recorded as an unsuccessful auth refusal': 'assertion-159', 'real promoted replay changed job/witness or duplicated a version': 'assertion-160', 'real promotion crash window was not durably reconciled': 'assertion-161', 'real promotion did not precede the injected local interruption': 'assertion-162', 'real regional STS did not establish expiry-specific refusal': 'assertion-163', 'receipt lost job/resource binding': 'assertion-164', 'recorded synthetic replacement deletion failed': 'assertion-165', 'refusal changed the exhausted budget': 'assertion-166', 'replacement lookup lost current identity or historical cleanup custody': 'assertion-167', 'replay child attempted a write outside its proof resource': 'assertion-168', 'replay child gained a fresh creation budget': 'assertion-169', 'replay child returned invalid metadata evidence': 'assertion-170', 'replay could not reopen the existing datastore': 'assertion-171', 'replay failed existing signed-custody admission': 'assertion-172', 'replay metadata pipe did not close within process deadline': 'assertion-173', 'replay process did not start': 'assertion-174', 'replay process did not start or terminate normally': 'assertion-175', 'resource outside run accepted': 'assertion-176', 'restore did not deliver the new source': 'assertion-177', 'resume converged a different revision': 'assertion-178', 'resume did not name the catch-up job': 'assertion-179', 'retain did not remove both active targets together': 'assertion-180', 'retained adapter kept active durable custody': 'assertion-181', 'retrying the same token created or falsely reported another version': 'assertion-182', 'revocation did not retire the exact running job and destroy credential custody': 'assertion-183', 'revoked staged write changed current identity/version/value or exact owned custody': 'assertion-184', 'revoked worker did not return the expected job supersession': 'assertion-185', 'revoked write did not settle its exact effect and release the provider fence': 'assertion-186', 'role source borrows oracle or cleanup credentials': 'assertion-187', 'role-auth delivery differs from the synthetic source': 'assertion-188', 'same-name retries reset the creation budget': 'assertion-189', 'same-target partial failure hid conflict or blocked its sibling': 'assertion-190', 'sandbox identity preflight failed before mutations': 'assertion-191', 'signed create refusal classification changed': 'assertion-192', 'source unset failed': 'assertion-193', 'stale adoption consent overwrote an unreviewed AWS resource/version': 'assertion-194', 'stale-consent refusal lost custody': 'assertion-195', 'symlink fixture accepted': 'assertion-196', 'synthetic adoption resource creation failed': 'assertion-197', 'synthetic adoption seed failed': 'assertion-198', 'synthetic name remained unavailable within the bounded replacement attempts': 'assertion-199', 'synthetic name reuse deadline reached': 'assertion-200', 'synthetic publication failed': 'assertion-201', 'synthetic replacement creation failed; reconcile reservation': 'assertion-202', 'synthetic secret has ambiguous current version metadata': 'assertion-203', 'synthetic secret has no current version': 'assertion-204', 'synthetic source staging failed': 'assertion-205', 'synthetic-value oracle failed': 'assertion-206', 'unapproved role session accepted': 'assertion-207', 'unexpected AWS module': 'assertion-208', 'unexpected consent module': 'assertion-209', 'unexpected replay loader': 'assertion-210', 'unowned AWS secret did not produce an adoption artifact': 'assertion-211', 'unreserved resource accepted': 'assertion-212', 'unsafe fixture scope accepted': 'assertion-213', 'used namespace was reclaimable': 'assertion-214', 'verified create ARN lost cleanup custody after observation': 'assertion-215', 'version-bound AWS adoption failed': 'assertion-216', 'wire fixture did not omit the unlabelled deprecated version': 'assertion-217', 'wire fixture did not retain current and previous versions': 'assertion-218', 'workload descriptor bypassed the node opt-in': 'assertion-219', 'workload modes require an approved projected token and role': 'assertion-220', 'world-readable fixture accepted': 'assertion-221', 'writer borrows oracle or cleanup credentials': 'assertion-222', 'writer bypassed the shared creation budget': 'assertion-223', 'writer descriptor does not match its declared test mode': 'assertion-224'}
ASSERTION_LINE = re.compile('[ \\t]+(?:aws_real_adoption_test\\.go|aws_real_auth_test\\.go|aws_real_clock_test\\.go|aws_real_creation_test\\.go|aws_real_credential_fence_test\\.go|aws_real_delivery_test\\.go|aws_real_diagnostics_test\\.go|aws_real_durable_test\\.go|aws_real_finish_loss_test\\.go|aws_real_fixture_test\\.go|aws_real_iam_test\\.go|aws_real_move_test\\.go|aws_real_oracle_test\\.go|aws_real_process_test\\.go|aws_real_scope_test\\.go):[1-9][0-9]*: ([^\\r\\n]+)\\n?')


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
        self.prefix = env.get("AWS_ACCEPTANCE_RUN_PREFIX", "")
        require(RUN_PREFIX.fullmatch(self.prefix))
        self.historical_cleanup_arn = env.get("AWS_ACCEPTANCE_HISTORICAL_CLEANUP_ARN", "")
        require(self.historical_cleanup_arn in ("", historical_cleanup.ARN))
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
        self.cleanup_credentials: dict | None = None
        self.cleanup_session_expiry = 0.0
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
        if actor == "cleanup":
            self.cleanup_credentials = credentials.copy()
            self.cleanup_session_expiry = expiry
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
            "account": ACCOUNT, "region": REGION, "prefix": self.approval.prefix, "budget_usd": 1,
            "authorization_id": self.approval.authorization, "kms_key_arn": self.approval.kms,
            "expires_at": self.approval.auth_expiry_raw, "writers": writers,
            "oracle": {"credential_file": "oracle.json", "principal_arn": role("oracle")},
            "cleanup": {"credential_file": "cleanup.json", "principal_arn": role("cleanup")},
        }
        private_json(self.custody / "manifest.json", manifest)

    def refresh_tokens(self) -> None:
        for actor in ("ambient", "web-identity"):
            self.token(actor + ".jwt")

    def cleanup_historical(self, evidence: Evidence) -> None:
        if not self.approval.historical_cleanup_arn:
            return
        require(self.cleanup_credentials is not None
                and self.cleanup_session_expiry > self.approval.cleanup_expiry)
        env = self.env.copy()
        env.update({"AWS_ACCESS_KEY_ID": self.cleanup_credentials["AccessKeyId"],
                    "AWS_SECRET_ACCESS_KEY": self.cleanup_credentials["SecretAccessKey"],
                    "AWS_SESSION_TOKEN": self.cleanup_credentials["SessionToken"]})
        def persist(state):
            evidence.data["historical_cleanup"] = state
            evidence.save()
        evidence.data["stage"] = "historical-cleanup"
        evidence.save()
        cleanup = historical_cleanup.Cleanup(
            env, min(self.approval.auth_expiry, self.approval.cleanup_expiry),
            evidence.path.with_name("historical-cleanup-intent.json"), self.approval.run_id,
            persist, self.stop.is_set, wait=self.stop.wait)
        try:
            cleanup.execute(self.approval.historical_cleanup_arn)
        except historical_cleanup.Refused:
            raise Refused() from None

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
        self.prefix = approval.prefix
        self._pending_assertions: dict[str, tuple[str, bool]] = {}
        self.data = {"schema": "hikyo.aws-acceptance/v2", "candidate_sha": approval.candidate,
                     "workflow_sha": approval.workflow, "run_id": approval.run_id,
                     "status": "preparing", "stage": "compile", "account": ACCOUNT, "region": REGION, "prefix": self.prefix,
                     "recorded_arns": [], "verified_absent_arns": [], "unresolved_names": [],
                     "historical_cleanup": historical_cleanup.state_for(approval.historical_cleanup_arn),
                     "tests": {}, "maximum_create_reservations": 32,
                     "creation_reservations": [], "complete_creation_ledger": False,
                     "diagnostics": [], "diagnostics_truncated": False,
                     "failure_assertions": [], "failure_assertions_truncated": False}
        self.save()

    def _assertion_event(self, action, test, output: str) -> None:
        # Keep only a closed identifier in memory until its exact Go fail
        # footer AND terminal event arrive. A continuation or incomplete stream
        # drops it. Pending state is bounded by the fixed known-test inventory.
        if not isinstance(test, str) or test not in TEST_NAMES:
            self._pending_assertions.clear()
            return
        pending = self._pending_assertions.pop(test, None)
        if action == "output":
            if pending and not pending[1] and re.fullmatch(
                    r"--- FAIL: " + re.escape(test) + r" \([0-9]+(?:\.[0-9]+)?s\)\n", output):
                self._pending_assertions[test] = (pending[0], True)
                return
            # Go indents continuation lines by eight spaces. Accept exactly
            # four, one complete line, and the pinned basename/literal shape.
            framed = output.startswith("    ") and not output.startswith("     ") and output.endswith("\n")
            line = ASSERTION_LINE.fullmatch(output) if framed else None
            identifier = FAILURE_ASSERTIONS.get(line[1]) if line else None
            if identifier:
                self._pending_assertions[test] = (identifier, False)
        elif action == "fail" and pending and pending[1] and output == "":
            if len(self.data["failure_assertions"]) < MAX_DIAGNOSTICS:
                self.data["failure_assertions"].append({"test": test, "assertion": pending[0]})
            else:
                self.data["failure_assertions_truncated"] = True

    def save(self) -> None:
        private_json(self.path, self.data)
        directory = os.open(self.path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def event(self, event: dict) -> None:
        action, test = event.get("Action"), event.get("Test", "")
        if isinstance(test, str) and test in TEST_NAMES:
            if action in ("pass", "fail", "skip"):
                require(len(self.data["tests"]) < 1024)
                self.data["tests"][test] = action
        output = event.get("Output", "")
        require(isinstance(output, str) and len(output) <= MAX_LINE)
        if action == "output" and isinstance(test, str) and test in TEST_NAMES:
            diagnostic = DIAGNOSTIC_PATTERN.fullmatch(output)
            if diagnostic:
                if len(self.data["diagnostics"]) < MAX_DIAGNOSTICS:
                    self.data["diagnostics"].append({
                        "test": test, "operation": diagnostic[1], "code": diagnostic[2],
                    })
                else:
                    self.data["diagnostics_truncated"] = True
        self._assertion_event(action, test, output)
        # Only exact known fixture diagnostics are accepted, never general
        # provider messages, raw JSON, panic text or arbitrary test output.
        arn_pattern = re.escape(f"arn:aws:secretsmanager:{REGION}:{ACCOUNT}:secret:{self.prefix}") + r"[A-Za-z0-9/_+=.@-]{1,450}-[A-Za-z0-9]{6}"
        for label, key in (("recorded synthetic AWS resource: ", "recorded_arns"),
                           ("verified synthetic AWS resource absent: ", "verified_absent_arns")):
            match = re.fullmatch(r"\s+aws_real_fixture_test.go:[0-9]+: " + re.escape(label) + "(" + arn_pattern + r")\n?", output)
            if match and match[1] not in self.data[key]:
                require(len(self.data[key]) < 32)
                self.data[key].append(match[1])
        match = re.fullmatch(r"\s+aws_real_fixture_test.go:[0-9]+: reserved synthetic AWS create: ([0-9]+) (" + re.escape(self.prefix) + r"[A-Za-z0-9/_+=.@-]{1,450})\n?", output)
        if match:
            require(int(match[1]) == len(self.data["creation_reservations"]) + 1 <= 32)
            self.data["creation_reservations"].append(match[2])
        match = re.fullmatch(r"\s+aws_real_fixture_test.go:[0-9]+: unresolved synthetic AWS create; reconcile name (" + re.escape(self.prefix) + r"[A-Za-z0-9/_+=.@-]{1,450}) before rerunning\n?", output)
        if match and match[1] not in self.data["unresolved_names"]:
            require(len(self.data["unresolved_names"]) < 32)
            self.data["unresolved_names"].append(match[1])
        self.save()

    def finish(self, exit_code: int, projection_ok: bool) -> bool:
        self._pending_assertions.clear()
        tests = self.data["tests"]
        passed = exit_code == 0 and projection_ok and tests.get(SUITE) == "pass"
        passed = passed and all(tests.get(name) == "pass" for name in TEST_NAMES)
        passed = passed and not any(status in ("skip", "fail") for status in tests.values())
        passed = passed and bool(self.data["recorded_arns"]) and not self.data["unresolved_names"]
        passed = passed and set(self.data["recorded_arns"]) == set(self.data["verified_absent_arns"])
        historical = self.data["historical_cleanup"]
        passed = passed and (historical["status"] == "not-requested" or historical["verified_absent"] is True)
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
        projection.cleanup_historical(evidence)
        require(time.time() + 30 * 60 <= approval.auth_expiry)
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

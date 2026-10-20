"""Exact optional historical-secret cleanup; no general AWS client or value API."""

from __future__ import annotations

import datetime as dt
import json
import math
import os
from pathlib import Path
import re
import subprocess
import time


ARN = ("arn:aws:secretsmanager:eu-central-1:099256073061:secret:"
       "hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj")
NAME = "hikyo-e2e-20261018b/web-identity/postgres/role-document"
KEY_ID = "10908b8d-fe54-40e3-9210-a3eefea709b6"
KEY_ARN = "arn:aws:kms:eu-central-1:099256073061:key/" + KEY_ID
MAX_RESPONSE = 65536
CLI_TIMEOUT = 10
ABSENCE_READS = 6
ABSENCE_INTERVAL = 2
NOT_FOUND = re.compile(
    rb"(?:\r?\n)*(?:aws: \[ERROR\]: )?An error occurred \(ResourceNotFoundException\)"
    rb" when calling the DescribeSecret operation(?: \(reached max retries: 0\))?: [^\r\n]+(?:\r?\n)*"
)


class Refused(Exception):
    """No raw provider cause may escape this helper."""


def require(condition):
    if not condition:
        raise Refused()


def state_for(arn):
    return {"arn": arn or None, "status": "pending" if arn else "not-requested",
            "delete_attempts": 0, "delete_response_exact_arn": False,
            "metadata_verified": False, "absence_checks": 0, "verified_absent": False}


def object_json(raw):
    require(isinstance(raw, bytes) and len(raw) <= MAX_RESPONSE)
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result)
            result[key] = value
        return result
    value = json.loads(raw, object_pairs_hook=pairs)
    require(isinstance(value, dict) and not ({"SecretString", "SecretBinary"} & value.keys()))
    return value


def metadata_matches(value):
    require(value.get("ARN") == ARN and value.get("Name") == NAME)
    require(value.get("KmsKeyId") in (KEY_ID, KEY_ARN))
    deleted = value.get("DeletedDate")
    if isinstance(deleted, str):
        date = dt.datetime.fromisoformat(deleted.replace("Z", "+00:00"))
        require(date.tzinfo is not None)
        deleted = date.timestamp()
    require(type(deleted) in (int, float) and math.isfinite(deleted) and deleted > 0)


class Cleanup:
    def __init__(self, env, deadline, intent_path: Path, run_id, persist, stop,
                 now=None, wait=None, command=None):
        self.env = env.copy()
        self.env["AWS_MAX_ATTEMPTS"] = "1"
        self.deadline = deadline
        self.intent_path = intent_path
        self.run_id = run_id
        self.persist = persist
        self.stop = stop
        self.now = now or time.time
        self.wait = wait or time.sleep
        self.command = command or subprocess.run
        self.started = False

    def active(self):
        require(not self.stop() and self.now() < self.deadline)

    def call(self, operation):
        self.active()
        require(operation in ("describe-secret", "delete-secret"))
        arguments = ["aws", "--region", "eu-central-1", "--no-cli-pager", "--output", "json",
                     "secretsmanager", operation, "--secret-id", ARN]
        if operation == "delete-secret":
            arguments.append("--force-delete-without-recovery")
        result = self.command(arguments, env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              timeout=min(CLI_TIMEOUT, self.deadline - self.now()), check=False)
        self.active()
        require(isinstance(result.stdout, bytes) and isinstance(result.stderr, bytes))
        require(len(result.stdout) <= MAX_RESPONSE and len(result.stderr) <= MAX_RESPONSE)
        if (operation == "describe-secret" and result.returncode == 254
                and result.stdout == b"" and NOT_FOUND.fullmatch(result.stderr)):
            return None
        require(result.returncode == 0 and result.stderr == b"")
        return object_json(result.stdout)

    def claim_intent(self):
        require(re.fullmatch(r"[0-9]+", self.run_id))
        payload = {"schema": "hikyo.aws-historical-cleanup-intent/v1", "run_id": self.run_id,
                   "arn": ARN, "operation": "DeleteSecret", "force_delete_without_recovery": True,
                   "maximum_delete_attempts": 1, "state": "claimed; reconcile before any retry"}
        fd = os.open(self.intent_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(json.dumps(payload, separators=(",", ":")).encode())
            stream.flush()
            os.fsync(stream.fileno())
        directory = os.open(self.intent_path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def execute(self, arn):
        require(not self.started)
        self.started = True
        state = state_for(arn)
        try:
            require(arn in ("", ARN))
            if not arn:
                self.persist(state.copy())
                return
            if self.intent_path.exists() or self.intent_path.is_symlink():
                state["delete_attempts"] = 1  # A prior claim cannot be refunded or replayed.
                raise Refused()
            state["status"] = "metadata-precheck"
            self.persist(state.copy())
            before = self.call("describe-secret")
            state["absence_checks"] += 1
            if before is None:
                state.update(status="already-absent", verified_absent=True)
                self.persist(state.copy())
                return
            metadata_matches(before)
            state["metadata_verified"] = True
            self.active()
            self.claim_intent()
            state.update(status="delete-intent-recorded", delete_attempts=1)
            self.persist(state.copy())
            deleted = self.call("delete-secret")
            require(deleted is not None and deleted.get("ARN") == ARN
                    and deleted.get("Name", NAME) == NAME)
            state.update(status="verifying-absence", delete_response_exact_arn=True)
            self.persist(state.copy())
            for attempt in range(ABSENCE_READS):
                after = self.call("describe-secret")
                state["absence_checks"] += 1
                if after is None:
                    state.update(status="verified-absent", verified_absent=True)
                    self.persist(state.copy())
                    return
                metadata_matches(after)
                if attempt + 1 < ABSENCE_READS:
                    self.active()
                    require(not self.wait(min(ABSENCE_INTERVAL, self.deadline - self.now())))
            raise Refused()
        except Exception:
            state.update(status="held-reconcile-before-new-suite", verified_absent=False)
            try:
                self.persist(state.copy())
            except Exception:
                pass
            raise Refused() from None

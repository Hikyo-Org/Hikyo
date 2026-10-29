#!/usr/bin/env python3
"""External reference receiver, not part of the Hikyo server. See the runbook."""

import argparse
from datetime import datetime
from decimal import Decimal
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlsplit


class Refused(ValueError):
    """An incomplete or conflicting unit must not advance the checkpoint."""


def object_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Refused("duplicate-json-key")
        result[key] = value
    return result


def invalid_constant(_value):
    raise Refused("invalid-json-number")


def parse(raw):
    if isinstance(raw, bytes):
        raw = raw.decode("utf-8")
    return json.loads(raw, object_pairs_hook=object_pairs, parse_float=Decimal,
                      parse_constant=invalid_constant)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"),
                      ensure_ascii=True, allow_nan=False)


def event_content(value):
    # Typed structural comparison avoids float rounding or tag collisions.
    # Original event bytes remain in batches.packet for investigation.
    if isinstance(value, dict):
        return ["object", [[k, event_content(v)] for k, v in sorted(value.items())]]
    if isinstance(value, list):
        return ["array", [event_content(v) for v in value]]
    if isinstance(value, Decimal):
        return ["decimal", str(value)]
    return [type(value).__name__, value]


def config(path):
    value = parse(Path(path).read_bytes())
    if not isinstance(value, dict) or set(value) != {
        "source_id", "trail", "scope", "database", "max_bytes", "max_rows"
    }:
        raise Refused("invalid-config")
    if (not isinstance(value["source_id"], str) or not value["source_id"]
            or value["trail"] not in ("tenant", "instance")
            or not isinstance(value["scope"], dict)
            or not isinstance(value["database"], str) or not value["database"]):
        raise Refused("invalid-config")
    scope = value["scope"]
    if (set(scope) - {"org_id", "project_id", "env_id"}
            or any(not isinstance(v, str) or not v for v in scope.values())
            or (value["trail"] == "instance" and scope)
            or (value["trail"] == "tenant" and not scope.get("org_id"))
            or ("env_id" in scope and "project_id" not in scope)):
        raise Refused("invalid-scope")
    for key, ceiling in (("max_bytes", 64 * 1024 * 1024), ("max_rows", 100000)):
        if type(value[key]) is not int or not 0 < value[key] <= ceiling:
            raise Refused("invalid-limit")
    value["database"] = str(Path(path).resolve().parent / value["database"])
    return value


def binding(cfg):
    return canonical({key: cfg[key] for key in ("source_id", "trail", "scope")})


def connect(cfg, mode="rw"):
    uri = Path(cfg["database"]).resolve().as_uri() + "?mode=" + mode
    db = sqlite3.connect(uri, uri=True, timeout=5)
    try:
        db.execute("PRAGMA synchronous=EXTRA")
        if db.execute("SELECT binding FROM stream").fetchone() != (binding(cfg),):
            raise Refused("stream-binding-mismatch")
    except BaseException:
        db.close()
        raise
    return db


def initialize(cfg):
    # Exclusive creation refuses to reset custody at an existing destination.
    fd = os.open(cfg["database"], os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    os.close(fd)
    db = sqlite3.connect(cfg["database"])
    try:
        db.execute("PRAGMA journal_mode=DELETE")
        db.execute("PRAGMA synchronous=EXTRA")
        with db:
            db.execute("CREATE TABLE stream (binding TEXT NOT NULL)")
            db.execute("INSERT INTO stream VALUES (?)", (binding(cfg),))
            db.execute("CREATE TABLE events (id TEXT PRIMARY KEY, body TEXT NOT NULL)")
            db.execute("""CREATE TABLE batches (
                id TEXT PRIMARY KEY, digest TEXT NOT NULL, rows INTEGER NOT NULL,
                received_ns INTEGER NOT NULL, packet BLOB NOT NULL)""")
            db.execute("""CREATE TABLE failure (
                at_ns INTEGER NOT NULL, cause TEXT NOT NULL, digest TEXT NOT NULL)""")
    finally:
        db.close()


def validate(cfg, packet):
    if (len(packet) > cfg["max_bytes"] or not packet.endswith(b"\n")
            or b"\n\n" not in packet):
        raise Refused("incomplete-or-oversize-packet")
    framing, body = packet.split(b"\n\n", 1)
    metadata = {}
    for line in framing.split(b"\n"):
        key, separator, value = line.partition(b": ")
        if not separator or key in metadata:
            raise Refused("invalid-framing")
        metadata[key] = value.decode("utf-8")
    if (set(metadata) != {b"Hikyo-Audit-Receipt", b"Hikyo-Audit-Export-ID",
                          b"Hikyo-Audit-Snapshot-Time"}
            or metadata[b"Hikyo-Audit-Receipt"] != "v1"):
        raise Refused("missing-response-binding")
    lines = body.split(b"\n")[:-1]
    if not 1 <= len(lines) <= cfg["max_rows"] + 1:
        raise Refused("row-limit")
    if any(len(line) + 1 > 1024 * 1024 for line in lines):
        raise Refused("line-limit")
    rows = [parse(line) for line in lines]
    terminal = rows.pop()
    if not isinstance(terminal, dict) or set(terminal) != {"export_receipt"}:
        raise Refused("missing-terminal-receipt")
    receipt = terminal["export_receipt"]
    if (not isinstance(receipt, dict)
            or set(receipt) != {"version", "started_event_id", "snapshot_time", "rows_streamed",
                                "outcome", "outcome_recorded"}
            or type(receipt["version"]) is not int or receipt["version"] != 1
            or type(receipt["rows_streamed"]) is not int
            or receipt["rows_streamed"] != len(rows)
            or receipt["outcome"] != "success"
            or receipt["outcome_recorded"] is not True
            or not isinstance(receipt["started_event_id"], str)
            or not 1 <= len(receipt["started_event_id"]) <= 200
            or receipt["started_event_id"] != metadata[b"Hikyo-Audit-Export-ID"]
            or receipt["snapshot_time"] != metadata[b"Hikyo-Audit-Snapshot-Time"]):
        raise Refused("incomplete-receipt")
    snapshot = receipt["snapshot_time"]
    if (not re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)", snapshot)
            or datetime.fromisoformat(snapshot.replace("Z", "+00:00")).tzinfo is None):
        raise Refused("invalid-snapshot")
    seen = set()
    for row in rows:
        if (not isinstance(row, dict) or "export_receipt" in row
                or not {"seq", "id", "type", "schema_version", "occurred_at",
                        "occurred_asserted", "recorded_at", "actor_class", "scope_class",
                        "outcome", "origin", "payload"} <= set(row)
                or not isinstance(row.get("id"), str)
                or not 1 <= len(row["id"]) <= 200
                or row["id"] in seen
                or type(row.get("seq")) is not int or row["seq"] <= 0
                or not isinstance(row.get("type"), str) or not row["type"]):
            raise Refused("invalid-or-duplicate-event")
        seen.add(row["id"])
        if any(row.get(k) != v for k, v in cfg["scope"].items()):
            raise Refused("foreign-scope")
        if cfg["trail"] == "instance" and any(
                row.get(k) for k in ("org_id", "project_id", "env_id")):
            raise Refused("foreign-trail")
    return receipt, rows


def receive(cfg, packet):
    db = connect(cfg)
    try:
        receipt, rows = validate(cfg, packet)
        digest = hashlib.sha256(packet).hexdigest()
        batch_id = receipt["started_event_id"]
        with db:
            db.execute("BEGIN IMMEDIATE")
            previous = db.execute(
                "SELECT digest, rows, received_ns FROM batches WHERE id=?",
                (batch_id,)).fetchone()
            if previous is not None:
                if previous[:2] != (digest, len(rows)):
                    raise Refused("batch-id-conflict")
                received_ns = previous[2]  # A replay cannot freshen heartbeat.
            else:
                for row in rows:
                    body = canonical(event_content(row))
                    old = db.execute("SELECT body FROM events WHERE id=?",
                                     (row["id"],)).fetchone()
                    if old is not None and old != (body,):
                        raise Refused("event-id-content-conflict")
                    db.execute("INSERT OR IGNORE INTO events VALUES (?, ?)",
                               (row["id"], body))
                received_ns = time.time_ns()  # Independent destination clock.
                db.execute("INSERT INTO batches VALUES (?, ?, ?, ?, ?)",
                           (batch_id, digest, len(rows), received_ns, packet))
        # Commit has returned before this acknowledgment is exposed.
        return {"source_id": cfg["source_id"], "trail": cfg["trail"],
                "scope": cfg["scope"], "batch_id": batch_id, "sha256": digest,
                "rows": len(rows), "received_ns": received_ns}
    except (ValueError, sqlite3.Error, RecursionError):
        # Keep a receiver-observed failure independently of the polling host.
        # Never persist exception text or producer content in health output.
        db.rollback()
        with db:
            db.execute("INSERT INTO failure VALUES (?, 'receive-refused', ?)",
                       (time.time_ns(), hashlib.sha256(packet).hexdigest()))
        raise
    finally:
        db.close()


def status(cfg, max_age):
    if max_age <= 0:
        raise Refused("invalid-max-age")
    db = connect(cfg, "ro")
    try:
        latest = db.execute("SELECT MAX(received_ns) FROM batches").fetchone()[0]
        failed, failures = db.execute("SELECT MAX(at_ns), COUNT(*) FROM failure").fetchone()
        now = time.time_ns()
        age = (now - latest) / 1e9 if latest is not None else None
        healthy = (age is not None and 0 <= age <= max_age
                   and (failed is None or failed < latest))
        return {"source_id": cfg["source_id"], "trail": cfg["trail"],
                "scope": cfg["scope"], "healthy": healthy,
                "receipt_age_seconds": age,
                "failure_count": failures,
                "last_failure_ns": failed,
                "receive_failure": failed is not None and
                (latest is None or failed >= latest)}
    finally:
        db.close()


def durable_file(path, data):
    temporary = path.with_suffix(".new")
    with temporary.open("wb") as output:
        output.write(data)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, path)
    sync_directory(path.parent)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def collect(cfg, args):
    """One finite poll/retry. curl and OpenSSH own transport and authentication."""
    url = urlsplit(args.url)
    scope = cfg["scope"]
    route = "/api/v1/instance/audit/export"
    if cfg["trail"] == "tenant":
        route = "/api/v1/orgs/" + scope["org_id"]
        if "project_id" in scope:
            route += "/projects/" + scope["project_id"]
        if "env_id" in scope:
            route += "/environments/" + scope["env_id"]
        route += "/audit/export"
    if (url.scheme != "https" or not url.hostname or url.username or url.password
            or url.path != route or url.query != "receipt=v1" or url.fragment
            or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", args.ssh_host)):
        raise Refused("invalid-transport-binding")
    spool = Path(args.spool).resolve()  # Pre-created private directory.
    with (spool / "lock").open("ab") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        key = hashlib.sha256(binding(cfg).encode()).hexdigest()
        pending = spool / (key + ".pending")
        if not pending.exists():
            with tempfile.TemporaryDirectory(prefix="download-", dir=spool) as tmp:
                headers, body = Path(tmp) / "headers", Path(tmp) / "body"
                result = subprocess.run([
                    "curl", "-q", "--config", args.curl_config, "--proto", "=https",
                    "--tlsv1.2", "--fail", "--silent", "--show-error",
                    "--connect-timeout", "10", "--max-time", "120",
                    "--max-filesize", str(cfg["max_bytes"]), "--dump-header", str(headers),
                    "--output", str(body), "--write-out", "%{http_code}", "--url", args.url,
                ], capture_output=True, check=True, timeout=130)
                response = {}
                for name in (b"Content-Type", b"Hikyo-Audit-Receipt", b"Hikyo-Audit-Export-ID",
                             b"Hikyo-Audit-Snapshot-Time"):
                    values = [line.split(b":", 1)[1].strip() for line in
                              headers.read_bytes().splitlines() if
                              line.split(b":", 1)[0].lower() == name.lower()]
                    if len(values) != 1 or b"\n" in values[0] or b"\r" in values[0]:
                        raise Refused("missing-response-binding")
                    response[name] = values[0]
                if (result.stdout != b"200" or response[b"Hikyo-Audit-Receipt"] != b"v1"
                        or response[b"Content-Type"].split(b";", 1)[0] != b"application/x-ndjson"):
                    raise Refused("receipt-not-negotiated")
                with body.open("rb") as content:
                    framing = b"\n".join(name + b": " + value for name, value in
                                         response.items() if name != b"Content-Type")
                    packet = framing + b"\n\n" + content.read(cfg["max_bytes"] + 1)
                validate(cfg, packet)
                durable_file(pending, packet)
        with pending.open("rb") as content:
            packet = content.read(cfg["max_bytes"] + 1)
        receipt, rows = validate(cfg, packet)
        result = subprocess.run([
            "ssh", "-F", args.ssh_config, "-T", "-oBatchMode=yes",
            "-oStrictHostKeyChecking=yes", "-oClearAllForwardings=yes",
            "-oConnectTimeout=10", "-oServerAliveInterval=15",
            "-oServerAliveCountMax=2", args.ssh_host,
        ], input=packet, capture_output=True, check=True, timeout=150)
        ack = parse(result.stdout)
        expected = {"source_id": cfg["source_id"], "trail": cfg["trail"],
                    "scope": cfg["scope"], "batch_id": receipt["started_event_id"],
                    "sha256": hashlib.sha256(packet).hexdigest(), "rows": len(rows)}
        if (not isinstance(ack, dict) or set(ack) != set(expected) | {"received_ns"}
                or any(ack.get(k) != v for k, v in expected.items())
                or type(ack["rows"]) is not int
                or type(ack["received_ns"]) is not int or ack["received_ns"] <= 0):
            raise Refused("destination-ack-mismatch")
        durable_file(spool / (key + ".checkpoint"), canonical(ack).encode() + b"\n")
        pending.unlink()
        sync_directory(spool)
        return ack


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("init", "receive", "status", "collect"))
    parser.add_argument("config")
    parser.add_argument("--max-age", type=int, default=900)
    parser.add_argument("--url")
    parser.add_argument("--curl-config")
    parser.add_argument("--ssh-config")
    parser.add_argument("--ssh-host")
    parser.add_argument("--spool")
    args = parser.parse_args()
    os.umask(0o077)
    try:
        cfg = config(args.config)
        if args.command == "init":
            initialize(cfg)
        elif args.command == "receive":
            packet = sys.stdin.buffer.read(cfg["max_bytes"] + 1)
            print(canonical(receive(cfg, packet)), flush=True)
        elif args.command == "collect":
            if any(value is None for value in (args.url, args.curl_config,
                                              args.ssh_config, args.ssh_host, args.spool)):
                raise Refused("missing-collector-config")
            print(canonical(collect(cfg, args)), flush=True)
        else:
            result = status(cfg, args.max_age)
            print(canonical(result), flush=True)
            return 0 if result["healthy"] else 1
        return 0
    except (OSError, ValueError, sqlite3.Error, RecursionError,
            subprocess.SubprocessError):
        print("audit receiver: HOLD, no acknowledged complete unit", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

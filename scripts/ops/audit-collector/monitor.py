#!/usr/bin/env python3
"""Finite independent inventory check. No notifications or event content output."""

import argparse
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import time


def write_json(path, value):
    path = Path(path)
    temporary = path.with_suffix(".new")
    with temporary.open("wb") as target:
        target.write(json.dumps(value, sort_keys=True).encode() + b"\n")
        target.flush()
        os.fsync(target.fileno())
    os.replace(temporary, path)
    fd = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def check(inventory, receiver, state, output):
    latch = Path(state).with_suffix(".detector-hold")
    try:
        result = check_streams(inventory, receiver, state)
        if latch.exists():
            result["healthy"] = False
            result["reason"] = "unresolved-detector-history"
        write_json(output, result)
        return result
    except (OSError, ValueError, KeyError, TypeError, AttributeError, RecursionError, sqlite3.Error):
        result = {"healthy": False, "checked_ns": time.time_ns(),
                  "reason": "detector-unavailable-HOLD"}
        # Independent of inventory parsing and stream-state validity. Never
        # overwrite the retained per-stream history to recover a detector.
        write_json(latch, result)
        write_json(output, result)
        return result


def check_streams(inventory, receiver, state):
    spec = importlib.util.spec_from_file_location("receiver", receiver)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    manifest = module.parse(Path(inventory).read_bytes())
    if (not isinstance(manifest, dict)
            or set(manifest) != {"streams", "max_age", "min_free_bytes"}
            or not isinstance(manifest["streams"], dict) or not manifest["streams"]):
        raise ValueError("invalid inventory")
    for name, paths in manifest["streams"].items():
        if (not isinstance(name, str) or not name or not isinstance(paths, dict)
                or set(paths) != {"config", "poll_status"}
                or any(not isinstance(value, str) or not value for value in paths.values())):
            raise ValueError("invalid stream inventory")
    if (type(manifest["max_age"]) is not int or not 0 < manifest["max_age"] <= 900
            or type(manifest["min_free_bytes"]) is not int or manifest["min_free_bytes"] <= 0):
        raise ValueError("invalid bounds")
    # A refusal remains HOLD even after a newer successful batch. Reset requires
    # explicit independent reconciliation, not poller success or state deletion.
    previous = module.parse(Path(state).read_bytes()) if Path(state).exists() else {}
    if not isinstance(previous, dict) or any(not isinstance(value, dict) for value in previous.values()):
        raise ValueError("invalid detector history")
    now = time.time_ns()
    result = {"checked_ns": now, "healthy": True, "streams": {}}
    for removed in set(previous) - set(manifest["streams"]):
        result["streams"][removed] = {"healthy": False, "unresolved": True,
                                      "reasons": ["expected-stream-removed-HOLD"]}
        result["healthy"] = False
    for name, paths in manifest["streams"].items():
        row = {"healthy": False, "reasons": []}
        old = previous.get(name, {})
        sticky = bool(old.get("unresolved", False))
        if old.get("last_checked_ns") is not None:
            interval = (now - old["last_checked_ns"]) / 1e9
            if not 0 <= interval <= 120:
                row["reasons"].append("detector-interval-HOLD")
        try:
            cfg = module.config(paths["config"])
            status = module.status(cfg, manifest["max_age"])
            disk = os.statvfs(Path(cfg["database"]).parent)
            free = disk.f_bavail * disk.f_frsize
            row["receipt_age_seconds"] = status["receipt_age_seconds"]
            row["free_bytes"] = free
            if not status["healthy"]:
                row["reasons"].append("receipt-or-receiver-HOLD")
            if status["failure_count"] != old.get("failure_count", 0):
                sticky = True
            row["failure_count"] = status["failure_count"]
            if free < manifest["min_free_bytes"]:
                row["reasons"].append("capacity-HOLD")
            # The poll result is an operator diagnostic, not source completeness
            # proof. Its absence/failure cannot be cleared by a receiver replay.
            if paths.get("poll_status"):
                poll = module.parse(Path(paths["poll_status"]).read_bytes())
                if (not isinstance(poll, dict) or type(poll.get("attempt_ns")) is not int
                        or not isinstance(poll.get("outcome"), str)
                        or type(poll.get("failure_count", 0)) is not int):
                    raise ValueError("invalid poll history")
                row["poll_failure_count"] = poll.get("failure_count", 0)
                if row["poll_failure_count"] != old.get("poll_failure_count", 0):
                    row["reasons"].append("source-attempt-history-HOLD")
                age = (time.time_ns() - poll["attempt_ns"]) / 1e9
                if not 0 <= age <= manifest["max_age"] or poll["outcome"] != "success":
                    row["reasons"].append("source-auth-transport-or-poller-HOLD")
            row["healthy"] = not row["reasons"]
        except (OSError, ValueError, KeyError, TypeError, sqlite3.Error):
            row["reasons"].append("check-unavailable-HOLD")
        sticky = sticky or bool(row["reasons"])
        if sticky:
            row["reasons"].append("unresolved-collection-history")
        row["healthy"] = not row["reasons"]
        row["unresolved"] = sticky
        row["last_checked_ns"] = now
        if row["reasons"]:
            result["healthy"] = False
        result["streams"][name] = row
    module.durable_file(Path(state), module.canonical(result["streams"]).encode() + b"\n")
    return result


def poll(receiver, config, status, arguments, preflight=None):
    with (Path(status).parent / "poll-supervisor.lock").open("ab") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return poll_locked(receiver, config, status, arguments, preflight)


def poll_locked(receiver, config, status, arguments, preflight):
    # Fixed diagnostic classes only. Never log curl output, token, URL or bytes.
    attempted = time.time_ns()
    path = Path(status)
    previous = json.loads(path.read_bytes()) if path.exists() else {}
    failures = previous.get("failure_count", 0)
    if type(failures) is not int or failures < 0:
        raise ValueError("invalid poll failure history")
    if previous.get("outcome") == "pending":
        failures += 1  # Reconcile a killed/unknown previous attempt before retry.

    def persist(outcome, count):
        report = {"attempt_ns": attempted, "outcome": outcome, "failure_count": count}
        write_json(path, report)

    persist("pending", failures)  # Durable intent before curl/SSH or any retry.
    try:
        if preflight is not None:
            arguments = preflight()
        result = subprocess.run(["python3", "-I", "-B", receiver, "collect", config, *arguments],
                                capture_output=True, timeout=300, check=False)
        code = result.returncode
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        code = 1
    persist("success" if code == 0 else "HOLD", failures + (code != 0))
    return code


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    check_args = commands.add_parser("check")
    for name in ("inventory", "receiver", "state", "output"):
        check_args.add_argument(name)
    poll_args = commands.add_parser("poll")
    for name in ("receiver", "config", "status"):
        poll_args.add_argument(name)
    poll_args.add_argument("arguments", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    try:
        if args.command == "poll":
            return poll(args.receiver, args.config, args.status, args.arguments)
        result = check(args.inventory, args.receiver, args.state, args.output)
        print(json.dumps(result, sort_keys=True))
        return 0 if result["healthy"] else 1
    except (OSError, ValueError, KeyError, TypeError, sqlite3.Error, subprocess.SubprocessError):
        print('{"healthy":false,"reason":"monitor-unavailable-HOLD"}')
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""One approved poll. Instance streams require an explicit supervised invocation."""

import argparse
from datetime import datetime, timezone
import importlib.util
import json
import os
from pathlib import Path


CONFIG = Path("/etc/hikyo-evidence")


def arguments(stream, supervised):
    if stream.endswith("-instance") and not supervised:
        raise ValueError("instance collection requires a supervised human/MFA session")
    config = CONFIG / ("poll-" + stream + ".json")
    if config.stat().st_uid != 0 or config.stat().st_mode & 0o022:
        raise ValueError("poll configuration must be administrator-owned")
    value = json.loads(config.read_bytes())
    if set(value) != {"url", "approved_until"}:
        raise ValueError("invalid fixed poll configuration")
    expiry = datetime.fromisoformat(value["approved_until"].replace("Z", "+00:00"))
    if expiry.tzinfo is None or not datetime.now(timezone.utc) < expiry:
        raise ValueError("approved fixture window expired")
    # Secrets and source session are provisioned privately, never in arguments.
    spool = "/srv/hikyo-evidence/spool-" + stream
    return ["--url", value["url"], "--curl-config", spool + "/source.curl",
            "--ssh-config", spool + "/ssh_config", "--ssh-host", stream,
            "--spool", spool]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stream", choices=("pg-tenant", "pg-instance", "sqlite-tenant", "sqlite-instance"))
    parser.add_argument("--supervised", action="store_true")
    args = parser.parse_args()
    os.umask(0o077)
    spool = "/srv/hikyo-evidence/spool-" + args.stream
    spec = importlib.util.spec_from_file_location("monitor", Path(__file__).with_name("monitor.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.poll(str(Path(__file__).with_name("receiver.py")),
                       "/etc/hikyo-evidence/" + args.stream + ".json", spool + "/poll-status.json",
                       [], preflight=lambda: arguments(args.stream, args.supervised))


if __name__ == "__main__":
    raise SystemExit(main())

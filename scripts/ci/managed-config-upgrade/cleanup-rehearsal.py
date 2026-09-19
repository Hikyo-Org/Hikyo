#!/usr/bin/env python3
"""Remove only this local rehearsal's explicitly recorded and labelled resources."""

import argparse
import json
import re
import subprocess
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    report = json.loads(args.report.read_text())
    run_id = report["run_id"]
    endpoint = report.get("docker_endpoint", "")
    if not re.fullmatch(
        r"hikyo-rehearsal-[0-9a-f]{16}", run_id
    ) or not endpoint.startswith("unix:///"):
        parser.error("invalid local rehearsal report")
    docker = ["docker", "--host", endpoint]
    # Reverse creation order: stop servers before PostgreSQL, volumes before network.
    for resource in reversed(report["resources"]):
        kind, name = resource["kind"], resource["name"]
        if kind not in ("container", "volume", "network") or not name.startswith(
            run_id + "-"
        ):
            parser.error("invalid owned resource")
        result = subprocess.run(
            docker + [kind, "inspect", name], capture_output=True, check=False
        )
        if result.returncode:
            print("Absent or unavailable; retained: " + name)
            continue
        info = json.loads(result.stdout)[0]
        labels = (
            info.get("Config", {}).get("Labels", {})
            if kind == "container"
            else info.get("Labels", {})
        )
        if (labels or {}).get("io.hikyo.dbugit.rehearsal") != run_id:
            parser.error("ownership label mismatch: " + name)
        command = docker + [kind, "rm"]
        if kind == "container":
            command.append("-f")
        subprocess.run(command + [name], check=True, stdout=subprocess.DEVNULL)
        print("Removed " + name)
    print("Private local output retained: " + str(args.report.parent))


if __name__ == "__main__":
    main()

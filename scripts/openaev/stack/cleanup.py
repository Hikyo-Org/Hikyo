#!/usr/bin/env python3
"""Verify native cluster absence before deleting ephemeral private custody."""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path


def docker(arguments: list[str]) -> str:
    try:
        return subprocess.run(["docker"] + arguments, check=True, capture_output=True,
                              timeout=60).stdout.decode().strip()
    except (OSError, subprocess.SubprocessError) as error:
        raise RuntimeError("stack_cleanup_docker_unavailable") from error


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("state", type=Path)
    args = parser.parse_args()
    if not args.state.exists():
        return
    if args.state.is_symlink():
        raise RuntimeError("stack_cleanup_symlink_forbidden")
    state = args.state.resolve()
    marker = state / "cluster-name"
    if not marker.is_file() or marker.is_symlink():
        raise RuntimeError("stack_cleanup_identity_missing")
    cluster = marker.read_text().strip()
    if re.fullmatch(r"openaev-[0-9a-f]{10}", cluster) is None:
        raise RuntimeError("stack_cleanup_identity_invalid")
    binary = state / "bin/k3d"
    if binary.is_file():
        try:
            subprocess.run([str(binary), "cluster", "delete", cluster], capture_output=True,
                           check=False, timeout=180, env=dict(os.environ,
                               KUBECONFIG=str(state / "kubeconfig")))
        except (OSError, subprocess.SubprocessError) as error:
            raise RuntimeError("stack_cleanup_cluster_delete_failed") from error
    # k3d can exit successfully despite a failed runtime operation. Check the
    # native resources, including node-mounted volumes captured at creation.
    checks = [
        docker(["container", "ls", "--all", "--quiet", "--filter", "label=k3d.cluster=" + cluster]),
        docker(["network", "ls", "--quiet", "--filter", "name=^k3d-" + cluster + "$"]),
        docker(["volume", "ls", "--quiet", "--filter", "label=k3d.cluster=" + cluster]),
    ]
    inventory = state / "node-volumes.json"
    if inventory.exists():
        names = json.loads(inventory.read_text())
        existing = set(docker(["volume", "ls", "--format", "{{.Name}}"]).splitlines())
        if not isinstance(names, list) or any(not isinstance(name, str) for name in names):
            raise RuntimeError("stack_cleanup_volume_inventory_invalid")
        checks.extend(name for name in names if name in existing)
    if any(checks):
        raise RuntimeError("stack_cleanup_owned_resources_remain")
    shutil.rmtree(state)
    print("Owned cluster, volumes, and private custody removed")


if __name__ == "__main__":
    try:
        main()
    except RuntimeError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)

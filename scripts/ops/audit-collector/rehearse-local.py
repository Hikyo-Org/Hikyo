#!/usr/bin/env python3
"""Owned local product/Unix rehearsal, finite resources, exact teardown, no remote ops."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import uuid


def validate_output(output):
    ancestor = output.parent
    while not ancestor.exists():
        ancestor = ancestor.parent
    probe = subprocess.run(["git", "-C", str(ancestor), "rev-parse", "--show-toplevel"],
                           capture_output=True, text=True, check=False,
                           env=dict(os.environ, LC_ALL="C"))
    if probe.returncode == 128 and "not a git repository" in probe.stderr:
        return  # Outside all working trees; private external output is allowed.
    probe.check_returncode()
    repository = probe.stdout.strip()
    ignored = subprocess.run(["git", "-C", repository, "check-ignore", "-q", "--", str(output)], check=False)
    tracked = subprocess.run(["git", "-C", repository, "ls-files", "--", str(output)],
                             capture_output=True, text=True, check=True)
    if ignored.returncode != 0 or tracked.stdout:
        raise ValueError("output must be ignored and untracked, or outside Git working trees")


def confirmed_absent(kind, name):
    template = {"container": "{{.Names}}", "image": "{{.Repository}}:{{.Tag}}", "volume": "{{.Name}}"}[kind]
    listing = subprocess.run(["docker", kind, "ls", *(["-a"] if kind == "container" else []),
                              "--format", template],
                             capture_output=True, text=True, check=False)
    if listing.returncode:
        raise RuntimeError("resource absence cannot be verified, Docker unavailable")
    return name not in listing.stdout.splitlines()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="new private evidence directory, ignored or outside Git working trees")
    args = parser.parse_args()
    os.umask(0o077)
    root = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip())
    assets = root / "scripts/ops/audit-collector"
    output = args.output.resolve()
    validate_output(output)
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    task = "hikyo-1037-" + uuid.uuid4().hex[:12]
    postgres, custody, image = task + "-pg", task + "-custody", task + ":local"
    inventory = {"task": task, "containers": [postgres, custody], "image": image, "volumes": [],
                 "source_revision": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(),
                 "files": {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
                           for p in [*assets.rglob("*.py"), *assets.rglob("*.service"), *assets.rglob("*.timer"),
                                     root / "internal/isolation/audit_collector_rehearsal_test.go"]},
                 "remote_mutations": [], "cleanup": "pending", "retained": "private local evidence only"}
    (output / "ownership.json").write_text(json.dumps(inventory, indent=2) + "\n")
    label = "io.hikyo.task=" + task

    def run(arguments, name, env=None):
        with (output / name).open("wb") as log:
            result = subprocess.run(arguments, cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT, check=False)
        if result.returncode:
            raise RuntimeError("required command failed; inspect " + name)

    try:
        run(["docker", "run", "-d", "--name", postgres, "--label", label,
             "--cpus", "1", "--memory", "512m", "-e", "POSTGRES_HOST_AUTH_METHOD=trust",
             "-e", "POSTGRES_DB=hikyo1037", "-p", "127.0.0.1::5432", "postgres:18.6"], "postgres-start.log")
        mounts = json.loads(subprocess.check_output(["docker", "inspect", "--format", "{{json .Mounts}}", postgres], text=True))
        inventory["volumes"] = [mount["Name"] for mount in mounts if mount["Type"] == "volume"]
        (output / "ownership.json").write_text(json.dumps(inventory, indent=2) + "\n")
        run(["docker", "exec", postgres, "sh", "-eu", "-c",
             "i=0; until pg_isready -U postgres; do i=$((i+1)); [ \"$i\" -lt 30 ]; sleep 1; done"], "postgres-ready.log")
        port = subprocess.check_output(["docker", "port", postgres, "5432/tcp"], text=True).strip().rsplit(":", 1)[1]
        env = os.environ.copy()
        env.update(GOMAXPROCS="2", HIKYO_TEST_POSTGRES_DSN=f"postgres://postgres@127.0.0.1:{port}/hikyo1037?sslmode=disable",
                   HIKYO_AUDIT_REHEARSAL_DIR=str(output / "packets"))
        run(["go", "test", "-p", "2", "-parallel", "2", "./internal/isolation",
             "-run", "^TestAuditCollectorProductRehearsal$", "-count=1", "-v"], "product.log", env)
        if "--- SKIP" in (output / "product.log").read_text():
            raise RuntimeError("both engines must execute")
        run(["docker", "build", "--label", label, "-t", image, "-f", str(assets / "rehearsal.Dockerfile"), str(assets)], "image-build.log")
        run(["docker", "run", "-d", "--name", custody, "--label", label, "--network", "none",
             "--cpus", "1", "--memory", "256m", "--pids-limit", "128",
             "--mount", f"type=bind,src={assets},dst=/assets,readonly",
             "--mount", f"type=bind,src={output / 'packets'},dst=/packets,readonly", image], "custody-start.log")
        run(["docker", "exec", custody, "python3", "-I", "-B", "/assets/rehearse-linux.py"], "custody.json")
        run(["docker", "exec", custody, "systemd-analyze", "verify",
             *["/assets/systemd/" + p.name for p in sorted((assets / "systemd").iterdir())]], "systemd.log")
        inventory["result"] = "LOCAL PASS; protected-host acceptance NOTRUN"
    finally:
        failures = []
        for container in (custody, postgres):
            inspect = subprocess.run(["docker", "inspect", "--format", '{{index .Config.Labels "io.hikyo.task"}}', container], capture_output=True, text=True, check=False)
            if inspect.returncode == 0:
                if inspect.stdout.strip() != task:
                    failures.append("unexpected owner: " + container)
                    continue
                removed = subprocess.run(["docker", "rm", "-f", "-v", container], capture_output=True, check=False)
                if removed.returncode:
                    failures.append("remove failed: " + container)
            try:
                if not confirmed_absent("container", container):
                    failures.append("container remains or inspection failed: " + container)
            except RuntimeError:
                failures.append("container absence unknown: " + container)
        inspect = subprocess.run(["docker", "image", "inspect", "--format", '{{index .Config.Labels "io.hikyo.task"}}', image], capture_output=True, text=True, check=False)
        if inspect.returncode == 0:
            if inspect.stdout.strip() != task:
                failures.append("unexpected image owner")
            elif subprocess.run(["docker", "image", "rm", image], capture_output=True, check=False).returncode:
                failures.append("image removal failed")
        try:
            if not confirmed_absent("image", image):
                failures.append("image remains or inspection failed")
        except RuntimeError:
            failures.append("image absence unknown")
        for volume in inventory["volumes"]:
            try:
                if not confirmed_absent("volume", volume):
                    failures.append("volume remains: " + volume)
            except RuntimeError:
                failures.append("volume absence unknown: " + volume)
        inventory["cleanup"] = failures or "verified, task containers/anonymous volumes/image removed; shared caches preserved"
        (output / "ownership.json").write_text(json.dumps(inventory, indent=2) + "\n")
        if failures:
            raise RuntimeError("owned resource cleanup incomplete")
    print("LOCAL PASS; private receipts retained; owned runtime removed")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Helm post-renderer for scripts/ci/security-pi-rehearsal.sh.

Adds only what a rehearsal namespace needs and the chart deliberately does not offer:
local names for GitHub, ghcr.io and hikyo.app (hostAliases), one node, a memory hint and a
short coordinator deadline. Everything else the chart renders reaches the cluster unchanged,
including the RBAC, the CronJob and the staging Pod's source (the Deployment's pod template).

usage: helm ... --post-renderer security-pi-rehearsal-render.py, with the settings in the environment:
  REHEARSAL_STANDIN_IP, REHEARSAL_NODE, REHEARSAL_TIMEOUT_SECONDS
Helm does not pass hooks to a post-renderer. REHEARSAL_HOOK=1 patches a hook Job rendered by
`helm template` instead, so the script can apply the chart's own enrollment Job with the same overlay.
"""
import json
import os
import sys

import yaml

ip = os.environ["REHEARSAL_STANDIN_IP"]
node = os.environ["REHEARSAL_NODE"]
timeout = int(os.environ["REHEARSAL_TIMEOUT_SECONDS"])
names = ["api.github.com", "github.com", "raw.githubusercontent.com", "ghcr.io", "hikyo.app"]

objects = [item for item in yaml.safe_load_all(sys.stdin.read()) if item]
hook = os.environ.get("REHEARSAL_HOOK") == "1"
if hook:
    objects = [item for item in objects if item["kind"] == "Job"]
patched = {"Deployment": 0, "CronJob": 0, "Job": 0, "ConfigMap": 0}


def pod_spec(spec):
    spec["hostAliases"] = [{"ip": ip, "hostnames": names}]
    spec["nodeSelector"] = {"kubernetes.io/hostname": node}
    for container in spec.get("containers", []) + spec.get("initContainers", []):
        env = container.setdefault("env", [])
        # The binaries hold whole releases in memory, and a Pi has little to spare.
        env.append({"name": "GOMEMLIMIT", "value": "400MiB"})


for item in objects:
    kind = item["kind"]
    if kind == "Deployment":
        pod_spec(item["spec"]["template"]["spec"])
    elif kind == "CronJob":
        pod_spec(item["spec"]["jobTemplate"]["spec"]["template"]["spec"])
    elif kind == "Job":
        pod_spec(item["spec"]["template"]["spec"])
    elif kind == "ConfigMap" and "security-maintenance.json" in (item.get("data") or {}):
        config = json.loads(item["data"]["security-maintenance.json"])
        config["deployment"]["timeout_seconds"] = timeout
        item["data"]["security-maintenance.json"] = json.dumps(config, indent=2)
    else:
        continue
    patched[kind] += 1

# A render that silently skipped an object would rehearse something else.
if hook:
    assert patched == {"Deployment": 0, "CronJob": 0, "Job": 1, "ConfigMap": 0}, patched
else:
    assert patched == {"Deployment": 1, "CronJob": 1, "Job": 0, "ConfigMap": 1}, patched
sys.stdout.write(yaml.safe_dump_all(objects, sort_keys=False))

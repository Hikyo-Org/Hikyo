#!/usr/bin/env python3
"""Add only acquisition fixtures to the real unattended chart deployment."""
import json
import sys
from pathlib import Path

import yaml

source, image, output = sys.argv[1:]
objects = [item for item in yaml.safe_load_all(Path(source).read_text()) if item]
deployments = [item for item in objects if item["kind"] == "Deployment"]
assert len(deployments) == 1, "expected the singleton server deployment"
deployment = deployments[0]
assert deployment["spec"]["replicas"] == 1
assert deployment["spec"]["strategy"] == {"type": "Recreate"}
# Seed the offline descriptor while fully stopped before first enrollment.
deployment["spec"]["replicas"] = 0
pod = deployment["spec"]["template"]["spec"]
assert pod["automountServiceAccountToken"] is False
assert [item["name"] for item in pod["initContainers"]] == ["root-key-stage"]
for container in pod["containers"] + pod["initContainers"]:
    container["image"] = image
server = next(item for item in pod["containers"] if item["name"] == "server")
assert server["readinessProbe"]["httpGet"]["path"] == "/readyz"
assert server["livenessProbe"]["httpGet"]["path"] == "/healthz"
assert server["startupProbe"]["httpGet"]["path"] == "/healthz"
server["volumeMounts"].append(
    {"name": "acquisition-fixture", "mountPath": "/fixtures", "readOnly": True}
)
pod["volumes"].append(
    {"name": "acquisition-fixture", "persistentVolumeClaim": {"claimName": "hikyo-upgrade-public", "readOnly": True}}
)
Path(output).write_text(json.dumps({"apiVersion": "v1", "kind": "List", "items": objects}))

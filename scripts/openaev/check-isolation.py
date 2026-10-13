#!/usr/bin/env python3
"""Prove live isolated-runner network boundaries against reachable canaries."""

import argparse
import ipaddress
import json
import os
import subprocess
from pathlib import Path


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    kubeconfig = os.environ.get("KUBECONFIG")
    if not kubeconfig:
        raise SystemExit("runner_kubeconfig_required")
    kubectl = ["kubectl", "--kubeconfig", kubeconfig, "--context",
               os.environ.get("OPENAEV_KUBE_CONTEXT", "default")]

    def resource(arguments: list[str]) -> dict:
        result = subprocess.run(kubectl + arguments + ["-o", "json"], check=True,
                                capture_output=True, timeout=30)
        value = json.loads(result.stdout)
        if not isinstance(value, dict):
            raise SystemExit("invalid_isolation_resource")
        return value

    service = resource(["get", "svc", "canary", "-n", "openaev-forbidden"])
    ingress = resource(["get", "svc", "canary-ingress", "-n", "openaev-forbidden"])
    pods = resource(["get", "pods", "-n", "openaev-forbidden"])
    ready = [row for row in pods["items"] if row["status"].get("phase") == "Running"]
    if len(ready) != 1:
        raise SystemExit("one_live_forbidden_canary_required")
    api = resource(["get", "svc", "kubernetes", "-n", "default"])
    cluster_file = os.environ.get("OPENAEV_CLUSTER_FILE")
    if not cluster_file:
        raise SystemExit("runner_cluster_file_required")
    cluster = json.loads(Path(cluster_file).read_bytes())
    node_ip = str(ipaddress.ip_address(cluster["node_ip"]))
    metadata_ip = str(ipaddress.ip_address(cluster["metadata_canary"]))
    if metadata_ip != "169.254.169.254":
        raise SystemExit("invalid_metadata_canary")
    targets = [
        ["lab-rabbitmq", "rabbitmq.openaev-lab.svc.cluster.local", 5672, True],
        ["lab-hikyo", "hikyo.hikyo-lab.svc.cluster.local", 8443, True],
        ["forbidden-canary-service", str(ipaddress.ip_address(service["spec"]["clusterIP"])), 8080, False],
        ["forbidden-canary-pod", str(ipaddress.ip_address(ready[0]["status"]["podIP"])), 8080, False],
        ["forbidden-canary-ingress", str(ipaddress.ip_address(ingress["spec"]["clusterIP"])), 443, False],
        ["kubernetes-api", str(ipaddress.ip_address(api["spec"]["clusterIP"])), 443, False],
        ["internet", "1.1.1.1", 443, False],
        ["node-kubernetes-api", node_ip, 6443, False],
        ["runner-metadata-canary", metadata_ip, 80, False],
    ]
    baseline_program = """
import json,socket,sys
for label,host,port,allowed in json.loads(sys.argv[1]):
    if allowed: continue
    with socket.create_connection((host,port),timeout=3): pass
print('all_connected')
"""
    command = kubectl + ["exec", "-n", "openaev-control", "deployment/control", "--", "python", "-c"]
    control = subprocess.run(command + [baseline_program, json.dumps(targets)],
                             check=True, capture_output=True, timeout=25)
    if control.stdout.strip() != b"all_connected":
        raise SystemExit("forbidden_destinations_control_unverified")
    program = """
import json,socket,sys
results=[]
for label,host,port,allowed in json.loads(sys.argv[1]):
    try:
        with socket.create_connection((host,port),timeout=3): outcome='connected'
    except TimeoutError: outcome='blocked'
    except OSError as error:
        outcome='blocked' if error.errno in (101,111,113) else 'indeterminate'
    results.append({'target':label,'outcome':outcome,'pass':outcome==('connected' if allowed else 'blocked')})
print(json.dumps({'checks':results,'pass':all(r['pass'] for r in results)}))
"""
    result = subprocess.run(kubectl + ["exec", "-n", "openaev-lab", "deployment/http-injector", "--", "python", "-c", program, json.dumps(targets)],
                            check=True, capture_output=True, timeout=60)
    report = json.loads(result.stdout)
    report["forbidden_destination_control"] = "all_connected"
    report["scope"] = "isolated runner canaries; no production access or assertion"
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    args.output.chmod(0o600)
    print("PASS: network isolation" if report.get("pass") is True else "FAIL: network isolation")
    if report.get("pass") is not True:
        raise SystemExit("isolation_failed_or_indeterminate")


if __name__ == "__main__":
    main()

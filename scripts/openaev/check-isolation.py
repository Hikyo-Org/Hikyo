#!/usr/bin/env python3
"""Prove live isolated-runner network boundaries against reachable canaries."""

import argparse
import ipaddress
import json
import os
import subprocess
from pathlib import Path


DNS_PROGRAM = """
import json,secrets,socket,struct,sys
server=sys.argv[1]
def query(name):
    ident=secrets.randbelow(65536)
    labels=name.rstrip('.').split('.')
    question=b''.join(bytes([len(label)])+label.encode('ascii') for label in labels)+b'\\0'+struct.pack('!HH',1,1)
    request=struct.pack('!HHHHHH',ident,0x100,1,0,0,0)+question
    with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as channel:
        channel.settimeout(3)
        channel.connect((server,53))
        channel.send(request)
        answer=channel.recv(4096)
    if len(answer)<12 or answer[12:12+len(question)]!=question:
        raise RuntimeError('invalid_dns_answer')
    reply,flags,questions,count,authority,additional=struct.unpack('!HHHHHH',answer[:12])
    if reply!=ident or not flags&0x8000 or questions!=1:
        raise RuntimeError('invalid_dns_answer')
    return {'rcode':flags&15,'answers':count}
print(json.dumps({'internal':query('rabbitmq.openaev-lab.svc.cluster.local.'),'external':query('example.com.')}))
"""


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
    dns = resource(["get", "svc", "kube-dns", "-n", "kube-system"])
    dns_ip = str(ipaddress.ip_address(dns["spec"]["clusterIP"]))
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
    dns_control = subprocess.run(command + [DNS_PROGRAM, dns_ip], check=True, capture_output=True, timeout=10)
    control_dns = json.loads(dns_control.stdout)
    if control_dns.get("internal", {}).get("rcode") != 0 or control_dns.get("internal", {}).get("answers", 0) < 1:
        raise SystemExit("internal_dns_control_unverified")
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
    dns_result = subprocess.run(kubectl + ["exec", "-n", "openaev-lab", "deployment/http-injector", "--", "python", "-c", DNS_PROGRAM, dns_ip],
                                check=True, capture_output=True, timeout=10)
    dns_observed = json.loads(dns_result.stdout)
    internal = dns_observed.get("internal", {})
    external = dns_observed.get("external", {})
    report = json.loads(result.stdout)
    report["checks"].extend([
        {"target": "lab-cluster-dns", "outcome": "resolved" if internal.get("rcode") == 0 and internal.get("answers", 0) >= 1 else "indeterminate",
         "pass": internal.get("rcode") == 0 and internal.get("answers", 0) >= 1},
        {"target": "forbidden-external-dns", "outcome": "nxdomain" if external.get("rcode") == 3 and external.get("answers") == 0 else "indeterminate",
         "pass": external.get("rcode") == 3 and external.get("answers") == 0},
    ])
    report["pass"] = all(row["pass"] is True for row in report["checks"])
    report["dns_positive_control"] = "cluster_resolved"
    report["forbidden_destination_control"] = "all_connected"
    report["scope"] = "isolated runner canaries; no production access or assertion"
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    args.output.chmod(0o600)
    print("PASS: network isolation" if report.get("pass") is True else "FAIL: network isolation")
    if report.get("pass") is not True:
        raise SystemExit("isolation_failed_or_indeterminate")


if __name__ == "__main__":
    main()

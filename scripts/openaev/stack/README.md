# Disposable hosted OpenAEV stack

Run from the trusted Hikyo checkout on a Linux ARM64 GitHub-hosted runner:

```sh
scripts/openaev/stack/start.sh "$RUNNER_TEMP/openaev-stack" "$HIKYO_IMAGE"
source "$RUNNER_TEMP/openaev-stack/environment.sh"
# The native suite owns deployment, port forwards, and deletion of the candidate Hikyo.
scripts/openaev/stack/stop.sh "$RUNNER_TEMP/openaev-stack"
```

`HIKYO_IMAGE` must already be signature verified and use the immutable
`ghcr.io/hikyo-org/hikyo@sha256:...` form. The state directory must not already
exist. Prerequisites are Docker with a Buildx OCI exporter, kubectl, OpenSSL,
curl, Go, and Python with PyYAML. Setup downloads checksum-verified k3d v5.8.3;
K3s v1.36.1+k3s1 and all service and system images use committed digest pins.
The HTTP injector is built from a checksum-pinned upstream commit and
hash-locked ARM64 wheels, then imported and referenced by its OCI digest.

Six fresh OpenAEV pods run in `openaev-lab`. The four backend workloads
start first. RabbitMQ must prove its application is running and its configured
TCP listeners accept connections during startup, then uses the AMQP TCP port
for ongoing readiness; PostgreSQL, Elasticsearch, and Silo retain
their native database/HTTP readiness probes. Only after all four become Ready
does setup create the OpenAEV and injector Deployments. This prevents an early
connection failure from aborting Spring bootstrap while leaving the JVM alive. `hikyo-lab` has its namespace,
service, custody Secret, operator public key, and NetworkPolicies but remains
idle until the native lifecycle creates its own seventh target pod and PVC.
No existing database, report, performance baseline, credentials, registry,
cluster, or production workloads are imported.

Every run generates independent CA, server certificates, SQLite root key,
upgrade operator key, and OpenAEV credentials. Private state lives in the
owner-only directory, and environment exports contain paths and public URLs.
Never upload the state directory or Kubernetes secrets as workflow artifacts.
Subprocess output is captured. Setup failures emit a precise stage and bounded
startup diagnostics: allowlisted Pod/event reasons, health HTTP status, and
fixed logger, exception-class, and signal counts from current and previous
container JSON logs. A safe checkpoint after all six services become ready
preserves any recovered cold-start failure before the remount check. Messages,
arguments, response bodies, unknown classes, and secret values never enter
public diagnostics. Raw logs remain private in process memory.

The K3s NetworkPolicy controller remains enabled. Default deny allows only
named dependency connections, local cluster DNS, and the injector's target HTTPS port.
CoreDNS serves cluster.local and NodeHosts locally and returns authoritative
NXDOMAIN from an empty root zone for other names; it has no external forwarder.
Isolation includes an independent internal DNS positive control and a public
FQDN rejection check.
An independent node INPUT rule uses the controller-managed live source sets for the two lab namespaces
to reject their traffic to the node, closing
Kubernetes NetworkPolicy's local-node exception. A separate unrestricted
control pod in `openaev-control` remains outside those source sets; ingress
to this control pod remains denied. The native isolation verifier compares
that control with the restricted injector against reachable canary service,
pod, ingress, Kubernetes API, external endpoint, node API, and metadata listener.
`openaev-forbidden` is a synthetic canary, with no claim to production state.

RabbitMQ repairs its cookie to owner-owned `0600` on every container startup,
after volume permissions have been applied. Setup deletes and recreates its
pod, waits for its replacement to exist before checking readiness within one
180-second budget, and verifies a new UID plus `999:999:600` before handing
over the lab.

The storage controller uses explicit PV/PVC read and mutation verbs. Helper
Pod get/create/delete and log access are confined to kube-system; its ConfigMap
read is limited to the named local-path-config object; claim
updates and event writes are confined to the two lab namespaces. It has no
cluster Pod mutation or wildcard permissions. Helper Pods receive no service
account token. The trusted storage helper still requires its bounded node
storage hostPath to create and reclaim local volumes.

The caller must run cleanup after success or failure. Cleanup removes only the
random cluster named in the owner-only state marker, then removes generated
custody. Release publication and result attachment are outside this module.

# Network policy probes

Use these probes to prove that the chart's server NetworkPolicy blocks clients
that you did not approve. Run them on a disposable cluster. Repeat on an actual
installation only under its separately approved change and acceptance plan. The values model and the migration
steps are in the
[Kubernetes operator guide](https://hikyo.app/docs/kubernetes-operator/#network-policy).

## Automated disposable exercise

The existing `scripts/ci/chart-kind.sh` runner now disables kindnet, verifies
and installs the pinned Calico v3.33.0 manifest, and calls
`scripts/ci/chart-network-policy.sh`. It refuses an existing cluster/registry,
uses synthetic local fixture trust, and tears down its owned runtime. It runs
from the repository root with Helm v4.2.3, Go from `go.mod`, Node from `.nvmrc`,
frozen web/client dependencies, a Docker daemon and kind/kubectl. The default
TLS helper ceiling is 128Mi, retaining its 50m CPU ceiling and 16Mi request.
The full multicall binary initializes shared application packages even for
hidden staging modes; 32Mi on a no-swap node can stall before the helper runs.

Prepare a release-shaped candidate using the same source-owned ephemeral trust
fixture as CI. Keep this directory private; it contains synthetic local keys.
Never substitute production recovery/signing material:

```sh
set -eu
fixture_dir=$(mktemp -d)
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
scripts/ci/build-spa.sh
HIKYO_CHART_FIXTURE_OUTPUT="$fixture_dir/fixture" \
  HIKYO_CHART_FIXTURE_COMMIT="$(git rev-parse HEAD)" \
  go test -p 2 -parallel 2 -count=1 -run '^TestWriteChartFixture$' ./scripts/ci/chartfixture
# Match GOARCH to the local Docker daemon (amd64 or arm64).
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -p 2 -trimpath -tags ui \
  -ldflags "$(cat "$fixture_dir/fixture/ldflags")" \
  -o "$fixture_dir/hikyo" ./cmd/hikyo
# Choose a new evidence directory outside tracked source. It must not exist.
export HIKYO_CHART_NETWORK_POLICY_EVIDENCE="$fixture_dir/network-evidence"
HIKYO_CHART_KIND_BINARY="$fixture_dir/hikyo" \
  HIKYO_CHART_KIND_PUBLIC_DIR="$fixture_dir/fixture/public" \
  scripts/ci/chart-kind.sh
# Review/copy network-evidence before exiting this shell and removing fixture_dir.
cat "$HIKYO_CHART_NETWORK_POLICY_EVIDENCE/result.json"
```

Run this finite batch through the shared resource queue when required by the
contributor environment. CI's existing `scripts/ci/k8s-e2e.sh` builds and runs
the same fixture. Do not run the internal helper against another cluster.
The `cluster.json`, `cni.json`, Kubernetes version, default/restricted/recovered
policy UID/spec snapshots, source head/diff and `probes.jsonl` retain identity
and every curl result, including enforcement-convergence attempts.

The exercise first proves every denied destination is live without restrictions.
It then tests trusted proxy/workload/operator peers, same-namespace and
wrong-label denial, PodIP and Service paths, operational-port denial, UDP/TCP
DNS, real PostgreSQL boot/reboot, synthetic IdP/adapter/remote/collector HTTP
transport, a synthetic DELETE transport request, blocked egress, loopback metrics,
API-server port-forward and policy-only Helm recovery with the same policy UID.
It also runs `TestNetworkPolicySyntheticProviderCleanup` in a pod selected by
the same chart policy: real adapter creation, encrypted value publication,
outbox worker and durable journal on SQLite and PostgreSQL, stateful synthetic
GitLab wire calls, direct provider value readback, target-removal cleanup,
provider deletion readback, ledger release and subsequent credential revocation.
Cleanup runs before credential revocation so it retains provider authority.
This proves the standard chart on that CNI and IPv4 fixture. The provider is a
protocol fixture, not real GitLab. IdP and remote probes establish transport;
they do not establish real OAuth exchanges, real remote pairing, actual
provider acceptance, production destination ranges, another CNI or IPv6.

## Manual installation-specific probes

Retain the context/cluster UID, CNI versions, exact chart/image digests, all
selecting policies and each probe's status. First verify Kubernetes API access,
namespace-scoped pod creation/deletion and port-forward rights. Use a unique
probe namespace and retain a tested policy-only recovery command independent
of Hikyo login. Do not reuse an existing namespace or delete unrelated pods.

## Requirements

- A cluster whose CNI enforces NetworkPolicy, including egress rules. For kind,
  set `networking.disableDefaultCNI: true` and install Calico or Cilium with the
  CNI's own kind instructions.
- A Hikyo release installed with `networkPolicy.ingress.restricted: true` and
  `networkPolicy.egress.restricted: true`.
- A probe image with `curl`, `nc`, and `dig`. These steps use
  `nicolaka/netshoot:v0.14@sha256:7f08c4aff13ff61a35d30e30c5c1ea8396eac6ab4ce19fd02d5a4b3b5d0d09a2`.

The steps use these shell variables. Change them to match the release:

```sh
probe_image=nicolaka/netshoot:v0.14@sha256:7f08c4aff13ff61a35d30e30c5c1ea8396eac6ab4ce19fd02d5a4b3b5d0d09a2
ns=hikyo
release=hikyo
service="$release-hikyo.$ns.svc"
pod=$(kubectl -n "$ns" get pod \
  -l app.kubernetes.io/name=hikyo,app.kubernetes.io/instance="$release" \
  -o jsonpath='{.items[0].metadata.name}')
pod_ip=$(kubectl -n "$ns" get pod "$pod" -o jsonpath='{.status.podIP}')
scheme=https # Use http when the chart runs in trusted-proxy mode.
```

`curl` exit code 28 is a timeout and 7 is a connection failure. They count as
denial evidence only after a matching positive control proves the target
listener is live and the same request worked without the restriction. A dead
listener, DNS failure or wrong address is not isolation proof. Any HTTP status
code shows that the connection reached the server. Use fresh TCP connections
because treatment of established flows during a policy update varies by CNI.

## 1. Positive control from an approved peer

Start a pod that matches a peer in `networkPolicy.ingress.from`. For a
`namespaceSelector` peer, start the pod in that namespace with the selected
labels. The request must return an HTTP status code:

```sh
kubectl -n ingress-nginx run np-approved --rm -i --restart=Never \
  --labels app.kubernetes.io/name=ingress-nginx \
  --image="$probe_image" -- \
  curl -sk --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "$scheme://$service:8080/"
```

If this probe fails, the server is not ready or the peer is wrong. Do not
continue until it passes, because the negative probes need this control.

## 2. Negative probe from an unapproved namespace

```sh
kubectl create namespace np-probe
kubectl -n np-probe run np-denied --rm -i --restart=Never \
  --image="$probe_image" -- \
  curl -sk --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "$scheme://$service:8080/"
```

The probe must fail with exit code 28 or 7. An HTTP status code means that the
policy does not block unapproved pods. Check that the CNI enforces
NetworkPolicy and that `networkPolicy.ingress.restricted` is true.

## 3. Negative probe from an unapproved pod in the release namespace

```sh
kubectl -n "$ns" run np-denied --rm -i --restart=Never \
  --image="$probe_image" -- \
  curl -sk --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "$scheme://$service:8080/"
```

The probe must fail with exit code 28 or 7.

## 4. Negative probe of port 8081 from the approved peer

Port 8081 has no ingress rule, so even an approved peer must not reach it:

```sh
kubectl -n ingress-nginx run np-ops --rm -i --restart=Never \
  --labels app.kubernetes.io/name=ingress-nginx \
  --image="$probe_image" -- \
  curl -s --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "http://$pod_ip:8081/metrics"
```

The probe must fail with exit code 28 or 7. Kubelet probes still reach port
8081 because most CNIs allow traffic from the local node.

## 5. Egress probes from the server pod network

The server image has no shell. A restricted ephemeral container shares the
pod network, so the server NetworkPolicy also applies to it. Keep the chart's
UID, read-only filesystem, seccomp profile and dropped capabilities:

```sh
np_debug_profile=$(mktemp)
trap 'rm -f "$np_debug_profile"' EXIT HUP INT TERM
cat >"$np_debug_profile" <<'JSON'
{"securityContext":{"runAsNonRoot":true,"runAsUser":65532,"runAsGroup":65532,"readOnlyRootFilesystem":true,"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"seccompProfile":{"type":"RuntimeDefault"}}}
JSON
kubectl -n "$ns" debug "pod/$pod" -it --container=np-manual \
  --image="$probe_image" --profile=restricted --custom="$np_debug_profile" -- sh
rm -f "$np_debug_profile"
trap - EXIT HUP INT TERM
```

Run these commands in the debug shell. Replace the datastore host and port
with the values from `HIKYO_DB`:

```sh
dig +short +time=2 +tries=1 kubernetes.default.svc.cluster.local # Must answer.
nc -z -w 5 postgres.example.internal 5432                        # Must connect.
nc -z -w 5 kubernetes.default.svc 443                            # Must fail without an extraRules entry.
curl -s --max-time 5 -o /dev/null https://example.com            # Must fail without a matching destination.
```

Repeat the last command with one host for each destination that you allowed,
for example the OIDC issuer. That request must connect.

## 6. Clean up

```sh
kubectl delete namespace np-probe
```

Also test one actual configured IdP exchange, adapter call, remote operation
and pending cleanup under the installation's approved fixture plan. A successful
TCP handshake alone does not establish any provider's functional semantics.
Retain the before/after status and expected side effect without credentials.

Ephemeral debug containers stay in the pod status until the pod restarts. They
stop when the debug shell exits.

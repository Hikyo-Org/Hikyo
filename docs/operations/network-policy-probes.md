# Network policy probes

Use these probes to prove that the chart's server NetworkPolicy blocks clients
that you did not approve. Run them first on a disposable cluster, then on the
production cluster after each policy change. The values model and the migration
steps are in the
[Kubernetes operator guide](https://hikyo.app/docs/kubernetes-operator/#network-policy).

## Requirements

- A cluster whose CNI enforces NetworkPolicy, including egress rules. For kind,
  set `networking.disableDefaultCNI: true` and install Calico or Cilium with the
  CNI's own kind instructions.
- A Hikyo release installed with `networkPolicy.ingress.restricted: true` and
  `networkPolicy.egress.restricted: true`.
- A probe image with `curl`, `nc`, and `dig`. These steps use
  `nicolaka/netshoot`. Pin it by digest in your environment.

The steps use these shell variables. Change them to match the release:

```sh
ns=hikyo
release=hikyo
service="$release-hikyo.$ns.svc"
pod=$(kubectl -n "$ns" get pod \
  -l app.kubernetes.io/name=hikyo,app.kubernetes.io/instance="$release" \
  -o jsonpath='{.items[0].metadata.name}')
pod_ip=$(kubectl -n "$ns" get pod "$pod" -o jsonpath='{.status.podIP}')
scheme=https # Use http when the chart runs in trusted-proxy mode.
```

`curl` exit code 28 is a timeout, which shows that the CNI dropped the
connection. Some CNIs reject the connection instead, which gives exit code 7.
Any HTTP status code shows that the connection reached the server.

## 1. Positive control from an approved peer

Start a pod that matches a peer in `networkPolicy.ingress.from`. For a
`namespaceSelector` peer, start the pod in that namespace with the selected
labels. The request must return an HTTP status code:

```sh
kubectl -n ingress-nginx run np-approved --rm -i --restart=Never \
  --labels app.kubernetes.io/name=ingress-nginx \
  --image=nicolaka/netshoot -- \
  curl -sk --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "$scheme://$service:8080/"
```

If this probe fails, the server is not ready or the peer is wrong. Do not
continue until it passes, because the negative probes need this control.

## 2. Negative probe from an unapproved namespace

```sh
kubectl create namespace np-probe
kubectl -n np-probe run np-denied --rm -i --restart=Never \
  --image=nicolaka/netshoot -- \
  curl -sk --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "$scheme://$service:8080/"
```

The probe must fail with exit code 28 or 7. An HTTP status code means that the
policy does not block unapproved pods. Check that the CNI enforces
NetworkPolicy and that `networkPolicy.ingress.restricted` is true.

## 3. Negative probe from an unapproved pod in the release namespace

```sh
kubectl -n "$ns" run np-denied --rm -i --restart=Never \
  --image=nicolaka/netshoot -- \
  curl -sk --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "$scheme://$service:8080/"
```

The probe must fail with exit code 28 or 7.

## 4. Negative probe of port 8081 from the approved peer

Port 8081 has no ingress rule, so even an approved peer must not reach it:

```sh
kubectl -n ingress-nginx run np-ops --rm -i --restart=Never \
  --labels app.kubernetes.io/name=ingress-nginx \
  --image=nicolaka/netshoot -- \
  curl -s --max-time 5 -o /dev/null -w '%{http_code}\n' \
  "http://$pod_ip:8081/metrics"
```

The probe must fail with exit code 28 or 7. Kubelet probes still reach port
8081 because most CNIs allow traffic from the local node.

## 5. Egress probes from the server pod network

The server image has no shell. An ephemeral debug container shares the pod
network namespace, so the server NetworkPolicy also applies to it:

```sh
kubectl -n "$ns" debug "pod/$pod" -it --image=nicolaka/netshoot --target=server -- sh
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

Ephemeral debug containers stay in the pod status until the pod restarts. They
stop when the debug shell exits.

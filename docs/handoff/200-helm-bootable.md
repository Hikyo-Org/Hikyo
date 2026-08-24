# Issue #200 handoff: bootable Helm deployment

## Delivered contract

- Helm requires a datastore Secret, root-key Secret, canonical external origin,
  image digest, and native-TLS or trusted-proxy boundary.
- The root key enters through a projected Secret only. A non-root init mode
  validates and stages it from Kubernetes' fsGroup-readable projection into an
  owner-only `0400` runtime file; the server mount is read-only.
- Liveness uses operational `/healthz`; startup and readiness use operational
  `/readyz`. A writable `/tmp` emptyDir preserves the read-only root filesystem.
- Optional `database.tls` values mount a private PostgreSQL CA without placing
  it or the DSN in the chart manifest.
- The existing required `k8s-e2e` job invokes `chart-kind` after its operator
  suite. This trusted-CI-compatible bridge makes the introducing PR run the
  new gate; a newly added workflow job would not execute until after merge.
- `chart-kind` builds the exact release-shaped app artifact into
  `Dockerfile.release`, installs the actual chart, proves HTML and probe health,
  removes PostgreSQL, and proves readiness fails and recovers without a server
  restart.

## Validation

```bash
scripts/ci/check-chart.sh
scripts/ci/check-chart_test.sh
scripts/ci/classify-changed-paths_test.sh
scripts/ci/check-required-jobs_test.sh
go test -count=1 ./scripts/ci -run '^TestCIJobRegistry'
HIKYO_CHART_KIND_BINARY=ci-artifacts/hikyo-ui scripts/ci/chart-kind.sh
```

The Kind command owns only a fresh cluster named `hikyo-chart-e2e`; it refuses
to reuse or delete a pre-existing cluster with that name.

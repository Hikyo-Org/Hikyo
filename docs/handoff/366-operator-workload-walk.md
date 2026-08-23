# Handoff: #366 operator workload walk

Issue: https://github.com/Hikyo-Org/Hikyo/issues/366 (parent #326; audit
finding `F-S23-2`). Implementation base:
`428dd6a5e347479a7a3697e2953ce10b7543db58`.

## Contract

- `walkWorkloads` is the private owner for Deployment, StatefulSet, and
  DaemonSet opt-in filtering, pod-template stamp comparison, stalled reporting,
  and optional patching.
- The per-kind table retains Deployment → StatefulSet → DaemonSet traversal and
  each Kubernetes list's item order.
- Patch mode requests a rollout only when the pod-template stamp differs.
  Observe mode remains read-only and reports only workloads already carrying
  the current stamp.
- Trigger gating, empty-stamp gating, patch error text, list errors, condition
  text, wire/audit contracts, and generated outputs are unchanged. Database
  migrations: none. Generated outputs: none.

## Regression evidence

- `TestStalledObservedOnCurrentPath` now drives all three supported workload
  kinds through a full delivery followed by a read-only current response.
- The test asserts the exact persisted stalled-condition message and the
  Deployment → StatefulSet → DaemonSet order.
- The initial full delivery also proves all three workload kinds use the shared
  patch path before the read-only observation.

## Validation

```text
go test -count=1 ./internal/operator -run '^TestStalledObservedOnCurrentPath$'  passed
go test -count=1 ./internal/operator                                     passed
go test -count=1 ./...                                                    passed (61 packages)
go vet ./...                                                             passed
gofmt -l <changed-go-files>                                              clean
git diff --check                                                         clean
```

## Review

- Standards round 1 found duplicated test helpers; fixed. Round 2: `CLEAN`.
- Spec round 1: `CLEAN`.

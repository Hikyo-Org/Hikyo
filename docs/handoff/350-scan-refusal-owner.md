# Handoff: #350 Surface-2 scan refusal owner

Issue: https://github.com/Hikyo-Org/Hikyo/issues/350 (parent #326; audit
finding `F-S12-2`). Implementation PR:
https://github.com/Hikyo-Org/Hikyo/pull/405. Integrated base:
`b891007b0d8a6ec8f318af506172cc217640089e`.

## Contract

- `captureScanRefusal` now owns the Surface-2 refuse, audit-capture, and typed
  return sequence used by declaration writes, preflight scans, and definitions
  apply-skew scans.
- The helper returns `nil` when a result does not refuse. On refusal, it
  captures one `finding_blocked` event per blocked finding and returns the
  pointer-form `*scanRefusalErr` carrying blocked findings and rejected tokens.
- Callers still choose and pass their audit scope explicitly. Definitions apply
  receives the project-only scope produced by `projectScope`; passing it
  verbatim preserves current audit bytes while removing the drifting local
  reconstruction.
- Audit event order, trail, payload, object identity, error behavior, and wire
  values are unchanged. Database migrations: none. Generated outputs: none.

## Regression evidence

- `TestCaptureScanRefusalNil` pins the accepted path: no error and no captured
  audit events.
- `TestCaptureScanRefusalRejectionsOnly` pins the typed refusal with zero
  captured events when no blocked finding exists.
- `TestCaptureScanRefusalOneEventPerBlockedFinding` pins event cardinality,
  tenant trail, full caller scope, object locator, and ingress from
  `Finding.Surface`.

## Validation

```text
rtk go test -count=1 ./internal/service -run '^(TestCaptureScanRefusal|TestScanRejectionsNamedByClass)'
                                                               9 passed
rtk go test -count=1 ./internal/isolation -run 'Scanning'      2 passed
rtk go test -count=1 ./internal/conformance -run 'Scanning'    no matching tests
rtk go vet ./internal/service/...                              passed
rtk go build ./...                                             passed
rtk go test -count=1 ./...                     3533 passed / 61 packages
rtk gofmt -w <changed-go-files>                                clean
rtk git diff --check                                           clean
```

## Review

- Standards round 1: `CLEAN`.
- Spec round 1 questioned the apply scope. Production call-chain evidence
  showed the only server caller supplies `projectScope`, whose `Env` is empty;
  passing it verbatim preserves behavior. Spec round 2: `CLEAN`.

# Issue #365 — crypto token/scanning rotation handles

Issue: https://github.com/Hikyo-Org/Hikyo/issues/365 (parent #326; audit
finding `F-S17-1`).

**State: implemented.** Token and scanning derivation-key rotation now share
one atomic, monotonic handle owner.

## Contract

- `swapHandle` owns one immutable `keyHandle` behind an `atomic.Pointer`.
- `get` returns one atomic snapshot. `adopt` advances only to a strictly newer
  version, so a late predecessor callback cannot regress live key material.
- `PrepareTokenKeyRotation` and `PrepareScanningKeyRotation` delegate their
  common mint/adopt/abort sequence to `prepareDerivationKeyRotation`; callers
  defer abort so rolled-back candidates are zeroed, then adopt only after
  persistence commits.
- Superseded key bytes remain unzeroed because an in-flight derivation may
  still hold the immutable handle.
- `rootRotationPending` is an `atomic.Bool`, removing the finalize/read race
  between the service and app warning path.

`swapHandle` embeds `redactor`, is pinned by the crypto compile-time redaction
surface and `lint.SensitiveTypes`, and is covered by the planted-secret test.

Generated outputs: none. Database migrations: none.

## Regression evidence

Before the atomic pending-state change,
`TestRootRotationPendingConcurrentAccess` failed under `-race` with concurrent
accesses in `RootRotationPending` and `ClearRootRotationPending`. The same test
passes after the change. `TestTokenKeyRotationAdoptIsMonotonic` additionally
proves through token output that a late predecessor adopt cannot replace the
newer live key.

## Validation

```text
go test -race -count=1 ./internal/crypto                         135 passed
go test -count=1 ./internal/lint                                 30 passed
go test -count=1 ./internal/service -run 'Rotation|TokenKey|ScanningKey'
                                                                   1 passed
go test -count=1 ./internal/conformance \
  -run '^TestConformanceSQLite/(keyring_lifecycle|keyring_one_active_key_per_scope)$'
                                                                   3 passed
go test -count=1 ./...                                           3595 passed
git diff --check                                                   passed
```

## Review

- Standards axis: `CLEAN` after round 2; nil-handle refusal and candidate-key
  abort zeroing findings fixed and verified.
- Specification axis: `CLEAN`; #365 is complete without merging instance or
  master key ownership into the new handle.

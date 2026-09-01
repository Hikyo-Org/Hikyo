# Issue #516: sanctioned store-to-transport alias seam

Issue: https://github.com/Hikyo-Org/Hikyo/issues/516. Base:
`82eb89c35c7641370aaadc291b0b70223f1568f2`.

## Contract

- Six `internal/service` aliases intentionally expose store-owned types to the
  transport-mapping layer: five adapter types and `RetentionConsequence`.
- Every alias declaration names this sanctioned seam and cites the
  system-architecture ADR plus its mechanical enforcement.
- `internal/boundary/boundary_test.go` prevents handlers from importing
  `internal/store` directly; the aliases do not weaken that import direction.
- Service-owned view types remain a separate, optional decision. This change
  does not alter runtime behavior.

## Verification

- `go test ./...`: 4,046 passed across 69 packages.
- `go test ./internal/boundary/ ./internal/service/`: 353 passed.
- Two-axis review: Standards CLEAN; Spec CLEAN.
- Native Codex adversarial review at high effort: CLEAN after the required
  fix-verification loop.

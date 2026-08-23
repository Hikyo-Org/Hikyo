# Handoff: #331 operator status summary ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/331 (parent #326; audit
finding `F-S23-1`). Implementation is based on `origin/main` commit
`8cc1bbf570e57c5836b1533f1abdc00a600d68b4`.
Before delivery, latest `origin/main` (`94da1b003467cfe0ee7e1e87c98f061abe8a5b9b`)
was merged normally and the full validation was repeated.

## Contract

- Conditions are the sole source of truth for both `Ready` and `Lifecycle`.
- `summarize` applies deterministic fail-closed precedence: Unreconciled,
  Scrubbed, refusal, retained sync failure, then synced.
- `done` is the only production writer of `status.lifecycle`; the 24 outcome
  branches no longer duplicate that invariant.
- Every active reconcile clears stale `Unreconciled` evidence before authority
  is re-evaluated. A forbidden access reasserts it before status is persisted.
- An invalid `resyncInterval` now records `Synced=False/FetchFailed`, so a
  previously ready resource becomes not ready. A recovered ServiceAccount
  designation is cleared before token minting, so a later TokenRequest failure
  is retained rather than misclassified as refused.

Contract or migration decisions: none. Generated outputs: none. CRD changes:
none. Database migrations: none.

## Validation

```text
rtk go test -count=1 ./internal/operator
Go test: 89 passed in 1 package

rtk go test -count=1 ./...
Go test: 3473 passed in 61 packages

rtk go vet ./...
passed

rtk gofmt -l <changed-go-files>
no output

rtk git diff --check
passed
```

## Review

- Spec axis: CLEAN.
- Standards axis round 1 found stale designation could misclassify a transient
  TokenRequest failure as refusal, plus a misleading summary-writer name.
- Both findings were fixed with a public-seam regression; round 2: CLEAN.

# Handoff: #335 reauthentication-window binding classifier

Issue: https://github.com/Hikyo-Org/Hikyo/issues/335 (parent #326; audit finding
`F-S07-1`).

## Contract

- The four persisted binding columns form exactly one of three modes: unbound,
  operation-bound `(operation, key set)`, or adapter-bound `(purpose,
  operation, environment set)`.
- Partial or contradictory rows fail closed with `ErrReauthUnitMismatch`; a
  key-set-only row can no longer inherit unbound-window authority.
- Disclosure consumption applies each mode through the shared classifier.
  CLI disclosure approval intentionally accepts unbound sliding windows and
  exact single-decision ceremonies, but refuses operation-bound windows.
- Existing CLI audit cause strings and approved-window JSON fields are
  unchanged. Generated outputs: none.

## Coverage

- The classifier table covers all three valid modes plus key-set-only and
  purpose-without-environment-set refusals.
- SQLite and PostgreSQL isolation fixtures pin consume refusal for a
  key-set-only row and CLI refusal for an operation-bound sliding window.

## Validation

```text
go test -count=1 ./internal/service/...                         237 passed
go test -count=1 ./internal/isolation                         1,112 passed
go vet ./internal/service/... ./internal/isolation/...        passed
go test -count=1 ./...                         3,496 passed / 61 packages
go vet ./...                                                   passed
gofmt -l <changed Go files>                                    clean
git diff --check                                               clean
```

# Issue #341 — complete instance reencrypt registry

Issue: https://github.com/Hikyo-Org/Hikyo/issues/341 (parent #326; audit
finding `F-S10-1`).

**State: implemented.** One six-table registry now owns the instance reencrypt
walk and its retirement dryness gate.

## Contract

- Every registry row binds the table name, paged lister, version source, AAD,
  and row-taking compare-and-swap reseal operation.
- The five row-version tables read their `dek_version` column. `remotes` reads
  only the authenticated ciphertext header and never interprets its zero-value
  `DEKVersion` field.
- `walkInstance` and `instanceStraggler` consume the same registry. Adding a
  table to one path without the other is no longer representable.
- Retry purity and `AfterChunk` table names are unchanged. Malformed remote
  headers fail loudly during both the walk and retirement gate.
- Contract or migration decisions: none. Generated outputs: none.

## Coverage

- `TestReencryptInstanceRetireRefusesRemoteStraggler` proves both SQLite and
  PostgreSQL keep the old instance key retiring when a remote regresses after
  the walk.
- `TestReencryptInstanceRejectsMalformedRemoteHeader` proves both engines fail
  with contextual decrypt errors during the walk and retirement gate.
- Existing retry-safe, multi-chunk, credential-movement, and full-recovery
  instance checks remain unchanged and pass.

## Validation

- Focused new tests: 7 passed in `internal/isolation`.
- Required named regression set: 10 passed in `internal/isolation`.
- `go test -count=1 ./internal/service`: 236 passed.
- Scoped `go vet`: passed for service, store, and isolation.
- `go vet ./...`: passed.
- `go test -count=1 ./...`: 3,501 passed in 61 packages.
- Spec review: CLEAN. Standards review: CLEAN in round 2/3 after both initial
  documentation findings were fixed.

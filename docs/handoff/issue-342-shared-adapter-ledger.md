# Handoff: #342 shared adapter ledger representation

Issue: https://github.com/Hikyo-Org/Hikyo/issues/342. Base:
`052cc9b23ec325473eae48753102ad04a04b506a`.

## Contract

- `adapter.LedgerKey` and `IndexLedger` are the single normalized ledger index.
  Indexing preserves `LedgerEntry.Missing` and refuses missing rows outside
  owned or dispatched custody.
- `DesiredRows`, `PlanChanges`, `Undesired`, and their ordering helpers own the
  provider-neutral desired/plan/prune invariants. Both providers consume them;
  no provider builds and reparses opaque ledger keys.
- Durable missing remains an accessor-validated state-plus-flag representation,
  as permitted by the issue. No persisted enum or schema migration is needed.
  `Finish` refuses a missing completion for a concurrently released row, so
  `released+missing` cannot be written.
- Forgejo now treats an owned-missing variable as a create and honors the same
  completed-name resume cursor as GitHub Actions.
- Existing change ordering, sentinels-first writes, sentinels-last pruning,
  audit payloads including `owned_missing`, and module/journal interfaces remain
  unchanged.

## Coverage

- Shared adapter regressions pin missing preservation, invalid missing custody,
  and sentinel-first desired ordering.
- Forgejo regressions pin completed-name skipping and owned-missing create-only
  replay.
- Store regressions pin both valid owned-missing audit persistence and refusal
  of a late owned-missing completion after release.
- Scoped adapter, store, and service package suites pass with `-count=1`.
- Full Go validation passes with `go test -p 4 -count=1 ./...`.
- Standards and issue-spec review reached `CLEAN` in round 2 of 3 after fixing
  missing-custody retention and all bulk-release writers.
- Generated outputs: none.

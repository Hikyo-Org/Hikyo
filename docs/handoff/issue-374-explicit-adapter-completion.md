# Handoff: #374 explicit adapter completion

Issue: https://github.com/Hikyo-Org/Hikyo/issues/374. Base:
`84359a75e2cd457141a2967c16c982a19b1422a7`.

## Contract

- `adapter.Outcome` closes completion outcomes over the existing byte values
  `success`, `failure`, and `unknown`. `unknown` is retained because both
  providers already emit it for indeterminate writes and the audit registry
  permits it.
- `Completion.ReleaseLedger` is the only command that deletes a ledger row.
  Empty `State` no longer carries deletion semantics.
- Every completion must provide a valid outcome and exactly one ledger
  disposition: a non-empty `State` or `ReleaseLedger: true`.
- `State: Released` remains a retained history row. `Conflict`, `Missing`,
  provider status, finding payloads, and success-only key-delivery audit
  behavior remain independent and byte-identical.
- Forgejo and GitHub Actions producers use the closed outcome constants and
  mark each formerly empty-state release explicitly.

## Coverage

- Adapter validation regressions cover all valid outcomes, typo refusal,
  zero-value refusal, and state/release exclusivity.
- Store regressions prove `Completion{}` cannot change a ledger or audit row,
  explicit release deletes the intended row, and `State: Released` retains it.
- Provider fakes now model deletion from `ReleaseLedger`, so module coverage
  fails if a producer falls back to an empty-state sentinel.
- Local Go validation was deferred before the first push because host memory
  pressure was high. Hosted CI and final local validation results will be
  recorded before merge.
- Generated outputs: none.

# Handoff: #345 compose render-row state

Issue: https://github.com/Hikyo-Org/Hikyo/issues/345. Pull request:
https://github.com/Hikyo-Org/Hikyo/pull/389. Implementation base:
`b891007b0d8a6ec8f318af506172cc217640089e`.

## Contract

- `AbsentKeyPolicy` solely owns config-only versus full-render absence behavior;
  the duplicate `RenderProjection` field and cross-check are removed.
- Every source row declares exactly one semantic state: valued, no value, or
  unrevealed secret. Only valued rows may carry plaintext.
- Unknown row states and non-valued rows carrying plaintext fail before any
  target bytes or snapshot rows are built.
- Live and offline producers set the state explicitly. Existing target order,
  omission/refusal behavior, raw dotenv bytes, and snapshot rows are unchanged.

## Coverage

- The render-plan golden and live/offline-equivalence tests preserve existing
  output behavior.
- Regression tests cover unknown absent-key policy, unknown row state, and both
  non-valued states carrying contradictory plaintext.
- Focused render-plan tests passed 8/8. Scoped compose/CLI tests passed 434/434;
  scoped `go vet` passed. The two-axis standards/spec review returned clean
  after its fail-closed findings were fixed.
- No generated source, API contract, database migration, or wire/audit value
  changes.

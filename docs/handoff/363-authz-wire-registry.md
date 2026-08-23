# Handoff: #363 authz wire-registry ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/363 (parent #326; audit
finding `F-S15-1`). Implementation base:
`428dd6a5e347479a7a3697e2953ce10b7543db58`.

## Contract

- `wireRegistry` is the single owner of each entry point's probe class,
  operation linkage, and direct audit-event linkage.
- `newWireRegistry` validates every row during package initialization. Unknown
  classes, operations, and events; duplicate linkage; and any `ClassStub` row
  with operation or event linkage fail closed.
- `RegistryFacts.Wire`, `WireRoutes`, and `WireEvents` remain the consumer-facing
  projections and return copies. Existing wire and audit bytes, entry ordering,
  dual-dialect behavior, and generated outputs are unchanged.
- Database migrations: none. Generated outputs: none.

## Regression evidence

- `TestWireRegistrySnapshot` pins 280 wire entries, 193 operation-linked rows,
  and 56 direct-event rows.
- Three constructor-negative tests reject an unknown class and both forbidden
  `ClassStub` linkage shapes.
- Existing contract, audit-invariant, and eligibility suites continue consuming
  the unchanged projections.

## Validation

Validation results are recorded after the host memory-pressure gate clears and
in exact-head CI on the pull request.

## Review

Standards and spec review results are recorded before merge.

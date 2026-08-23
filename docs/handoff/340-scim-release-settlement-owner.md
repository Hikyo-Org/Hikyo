# Handoff: #340 SCIM release settlement owner

Issue: https://github.com/Hikyo-Org/Hikyo/issues/340 (parent #326; audit finding
`F-S08-1`). Implementation base:
`052cc9b23ec325473eae48753102ad04a04b506a`.

## Contract

- `releaseAndSettle` owns SCIM-origin release, session-generation advancement,
  session sweeping, and lockout-retention attention entry in one transaction.
- Deprovision and user deletion use `advanceAlways`; every other trigger uses
  `advanceIfAuthorityChanged`.
- Release events remain before attention events. Audit payloads, wire contracts,
  failure behavior, and SQLite/PostgreSQL storage remain unchanged.
- Mapping deletion and narrowing share one member loop. Narrowing supplies a
  grant filter; deletion supplies no filter.

## Coverage

- Existing `TestSCIMLockoutAcrossEveryReleasePath{SQLite,Postgres}` covers all
  six triggers, retention conversion, attention entry, audit order, and cure.
- Existing deprovision coverage proves zero-delta deprovision still advances.
- `TestSCIMMappingNarrowingWithoutAuthorityDeltaDoesNotAdvance{SQLite,Postgres}`
  proves an origin-only narrowing preserves effective grants and sessions.
- No schema, API, domain, migration, wire, or generated artifact changed.
- Local validation passed: focused service tests, all named SQLite/PostgreSQL
  SCIM regressions, and `go test -count=1 ./...`.

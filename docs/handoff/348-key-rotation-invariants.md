# Handoff: #348 key-rotation invariant ownership

Issue: https://github.com/Hikyo-Org/Hikyo/issues/348 (parent #326; audit
finding `F-S05-1`). Implementation base:
`b891007b0d8a6ec8f318af506172cc217640089e`.

## Contract

- `keyMutationAdapter` is the private two-dialect seam. SQLite and PostgreSQL
  adapters own generated-query parameter shapes, timestamp encoding, driver
  no-row translation, and exact unique-violation mapping.
- Hierarchy/scope fence ordering, predecessor compare-and-swap counts,
  exactly-one-master checks, root/master rotation exclusion, stranded tier-3
  refusal, and root-finalize epoch ordering now have one implementation.
- Token and scanning-key rotation share `rotateRootScopedTier3`; the operation
  proof and fixed purpose remain explicit inputs.
- SQLite retains `BEGIN IMMEDIATE` serialization; PostgreSQL retains
  `SELECT ... FOR UPDATE`. Wire/audit bytes, schema, and generated outputs are
  unchanged. Database migrations: none. Generated outputs: none.

## Regression evidence

- One public `store.KeyRepo` corpus runs against SQLite and PostgreSQL.
- It covers stale token, scanning, and DEK predecessors; dual-wrapped master
  refusal; root-prepare master mismatch; finalize without dual wrapping;
  newest-epoch finalize; stranded tier-3 refusal; and master/tier-3 unique
  conflict mapping.
- PostgreSQL uses a dedicated scratch database and fails loudly in CI if
  `HIKYO_TEST_POSTGRES_DSN` is absent.

## Validation

```text
rtk go test -count=1 ./internal/store -run '^TestKeyRotationInvariantsSQLite$'
                                                        11 passed
rtk go test -count=1 ./internal/store                   58 passed
rtk go test -count=1 ./...                 3,541 passed / 61 packages
rtk go vet ./...                                        passed
rtk gofmt -l <changed-go-files>                         clean
rtk git diff --check                                    clean
```

Local PostgreSQL execution was unavailable because `HIKYO_TEST_POSTGRES_DSN`
was unset; required CI supplies it. Full-suite and exact-head CI results are
recorded in the pull request.

## Review

- Standards round 1: `CLEAN` (0 violations, 0 smells).
- Spec round 1 found lost dialect fence comments; fixed. Round 2: `CLEAN`.

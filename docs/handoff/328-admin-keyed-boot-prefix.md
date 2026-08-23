# Handoff: #328 shared keyed boot prefix

Issue: https://github.com/Hikyo-Org/Hikyo/issues/328 (parent #326; audit
finding `F-S34-1`). Implementation is based on `origin/main` commit
`0068c499c155b05086fc1cf62927962532d90b6d`.

## Contract

- `openKeyed` owns the startup sequence shared by server and local-admin
  commands: process hardening, optional migration with pre-migration export and
  durable record, exact schema check, root-key resolution, datastore open,
  keyring load, and unfinished-root-rotation warning.
- `bootResources` and `bootGuard` preserve exact-once datastore cleanup on
  shared-prefix failure. Successful server boot transfers ownership to
  `Server`; successful admin authentication transfers ownership to its returned
  cleanup function.
- Local-admin configuration remains environment-only and auto-migration remains
  enabled by default. No admin `--auto-migrate` flag was added, so operators
  cannot create a divergent CLI-only migration policy.
- Server error wrapping, migration ordering, root-key zeroing, wire/audit values,
  and SQLite/PostgreSQL store behavior are unchanged.

Generated outputs: none. Database migrations: none.

## Regression evidence

- A pending migration plus configured backup recipient now produces one `.age`
  archive and one durable `backup.exported` instance event whose trigger is
  exactly `pre-migration` through `adminAuth`.
- A dual-wrapped root-key state now emits the same unfinished-rotation warning
  through `adminAuth` as server boot.
- Admin datastore ownership tests cover failure after acquisition and successful
  transfer to the returned cleanup function with exact close counts.

## Validation

```text
rtk go test -count=1 ./internal/app
Go test: 48 passed in 1 package

rtk go test -count=1 ./internal/app ./internal/crypto
Go test: 179 passed in 2 packages

rtk go test -count=1 ./...
Go test: 3459 passed in 61 packages

rtk go vet ./...
passed

rtk git diff --check
passed
```

## Review

- Standards axis: `CLEAN` with no documented violations or baseline smells.
- Spec axis: first pass found that the audit regression checked only the event
  type. The test now pins `payload.trigger == TriggerPreMigration`; round 2:
  `CLEAN`.

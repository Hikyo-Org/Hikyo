# Handoff: #370 isolation bootstrap-admin fixtures

Issue: https://github.com/Hikyo-Org/Hikyo/issues/370 (parent #326; audit finding
`F-S33-1`). Implementation base:
`428dd6a5e347479a7a3697e2953ce10b7543db58`.

## Contract

- `bootstrapAdmin` in the isolation harness owns first-administrator creation,
  password establishment, optional CLI login, and the returned administrator
  representation.
- `service.BootstrapResult` is the source of truth for account and principal
  identity; bootstrap fixtures no longer re-query account rows for those IDs.
- Per-suite usernames, passwords, Auth configuration, read grants, passkey
  enrollment, and ceremony ordering remain explicit at their existing seams.
- Factor and backup-drill fixtures still create no login during bootstrap. The
  backup drill still mints its reset authority before any session exists.
- Direct bootstrap calls remain only where bootstrap refusal/intermediate state
  or the CLI establishment ceremony is itself under test. Wire/audit bytes,
  schema, migrations, and generated outputs are unchanged.

## Validation

```text
go test -count=1 -timeout 90m ./internal/isolation/   1,122 passed
go test -count=1 -timeout 120m ./...                  3,591 passed / 61 packages
go vet ./...                                          passed
gofmt -l <changed-go-files>                           clean
git diff --check                                      clean
```

Local PostgreSQL execution was unavailable because `HIKYO_TEST_POSTGRES_DSN`
was unset; required CI supplies it. Exact-head CI results are recorded in the
pull request.

## Review

- Standards round 1 found two SQL identity re-derivations and one missed
  lock-test bootstrap sibling; fixed. Round 2: `CLEAN`.
- Spec round 1 found the same missed sibling; fixed. Round 2: `CLEAN`.

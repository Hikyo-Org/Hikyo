# Issue #357 — TS client responseless-operation staleness

Issue: https://github.com/Hikyo-Org/Hikyo/issues/357 (parent #326; audit
finding `F-S27-1`). Pull request: https://github.com/Hikyo-Org/Hikyo/pull/384.

**State: implemented.** The TypeScript operation generator now refuses stale
entries in its explicit `KNOWN_RESPONSELESS` allowlist.

## Contract

- `KNOWN_RESPONSELESS` remains the fail-closed allowlist for SDK operations
  that intentionally have no generated response type.
- `buildOperationsModule` records every allowlisted operation that it actually
  skips, then requires every allowlist entry to appear in that recorded set.
- An allowlisted operation that is removed, renamed, no longer exported, or
  gains a generated response now stops generation with an actionable error.
- An unlisted operation without a generated response continues to stop
  generation through the existing forward check.
- Valid generated output and operation ordering are unchanged.

Generated outputs: regenerated and byte-identical; no generated file changed.
Database migrations: none.

## Regression evidence

Removing the implementation while retaining the new public-boundary test
fails with `Missing expected exception`. Restoring it makes the same test pass.

## Validation

```text
pnpm --dir clients/ts install --frozen-lockfile                  passed
pnpm --dir clients/ts run verify                                14 passed
go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
                                                                  passed
git diff --check                                                 passed
generated and committed files                                   identical
```

## Review

- Standards axis: `CLEAN`; no documented violations or baseline smells.
- Specification axis: `CLEAN`; no missing requirements, scope creep, or
  incorrect behavior.

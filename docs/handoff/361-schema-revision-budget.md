# Issue #361 — single-owner schema-revision budget

Issue: https://github.com/Hikyo-Org/Hikyo/issues/361 (parent #326; audit
finding `F-S06-2`). Draft source: https://github.com/Hikyo-Org/Hikyo/pull/385.

**State: implemented.** Every production `BumpSchemaRevision` call now passes
through the service budget helper that charges the § 151 project rate first.

## Contract

- `bumpSchemaRevision` owns the inseparable charge-then-bump sequence.
- The helper runs inside each caller's write transaction after its no-op return.
- `chargeOnce` remains idempotent across transaction retries.
- A rate refusal returns before the revision bump and rolls back the transaction.
- An AST guard refuses production calls outside the helper.

Contract or migration decisions: none. Generated outputs: none.

## Coverage

- `TestBumpSchemaRevisionOnlyThroughBudget` pins the single production call site.
- Existing schema-revision rate-limit and no-op scenarios preserve behavior.
- Existing bound-registry coverage preserves budget classification totality.

## Validation

- Draft PR #385 passed build, test, race, fuzz, web, generated, analysis, and
  CodeQL jobs at `c468f9e2`; only DCO failed because its bot commit lacked a
  sign-off.
- Focused guard, schema-rate, no-op, totality, bound-pin, service, and
  conformance checks passed (330 tests across the two changed packages).
- `go test -count=1 ./...` passed 3,592 tests in 61 packages.
- `go build ./...`, `go vet ./internal/service`, and `git diff --check` passed.
- Local PostgreSQL conformance was not run because `HIKYO_TEST_POSTGRES_DSN`
  was unset; trusted CI owns the mandatory SQLite + PostgreSQL leg.
- Three-round review finished Standards `CLEAN` and Spec `SOUND` after the AST
  guard was hardened to distinguish receiver-less functions, methods, and
  package declarations.

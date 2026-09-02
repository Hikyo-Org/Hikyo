# #490 WebUI parity registry: handoff

Executable inventory of every public operation's browser disposition, with CI
gates that fail when a new operation lands undispositioned, when the SPA and the
registry disagree in either direction, or when a referenced implementation
issue is closed without the surface landing.

## What shipped

| Piece | Where | Gate |
| --- | --- | --- |
| Registry | `api/parity.yaml` | one row per `operationId`, exactly one of `webui` / `exception` / `issue` |
| Structural gate | `api/parity_test.go` | `go test ./api` (runs in `test`, and in `web-go` so web-only PRs pay it) |
| Issue liveness | `scripts/ci/check-parity-issues.sh` (+ `_test.sh`) | `freeze-guard` job step, `github.token`, fails loud on a denied read |
| Spec index | `docs/spec/README.md` | one document-map row |

## How the gate decides

- **Coverage:** registry keys equal the contract's operationIds, both ways.
  Generator-skipped protocol operations (`scimBulk`, `scimMe`, `scimSearch*`)
  are still contract operations, so they cannot slip past.
- **Evidence for `webui`:** the generated `<operationId>Op` export is imported
  by a runtime module under `web/src` (tests and `testkit` excluded), or with
  `reach: path` a literal `/api/v1` path matching the operation's route. The
  surface id must be in `SURFACES` in `web/src/app/navigation.ts`.
- **Spelling rule:** the test computes the generated export name itself
  (`rotateDEK` to `rotateDekOp`) and pins the rule against
  `clients/ts/src/generated/operations.gen.ts`, so a generator change fails
  here instead of emptying the evidence.
- **Symmetry:** an operation the SPA imports must be a direct `webui` row. This
  is what forces the adapter (#157), invite (#568), recovery (#571) and scoped
  audit (#572) PRs to flip their rows when they land.
- **Equivalent outcome vs mirroring:** `via: [ops]` records that the same user
  outcome is delivered through other operations, which must themselves be
  direct rows (no chains). Used for single `get`/`show` reads whose list
  already shows the row, the batch value verbs the matrix edits cell by cell,
  the definitions bundle verbs (the repository's path into the catalogue the
  key-detail editor writes directly), and the instance directory.
- **Closed exceptions:** `identity-protocol` (paths under `/api/v1/auth/` or
  `/scim/v2/`), `client-local-delivery` (admits a machine credential and no
  human session). `host-local-authority` and `k8s-controller-reconciliation`
  are named in the enum with a predicate that admits nothing: the ADR gives
  host-local verbs no HTTP endpoint, and the operator speaks only the delivery
  wire. The predicates are also asserted pairwise disjoint over the contract.

## Current dispositions (245 operations)

| Disposition | Count | Notes |
| --- | --- | --- |
| `webui` direct | 175 | evidence: Op import (172) or path literal (3) |
| `webui` via | 18 | equivalent outcomes, see file comments |
| `exception: identity-protocol` | 24 | 5 auth legs + 19 SCIM wire |
| `exception: client-local-delivery` | 2 | `fetchDelivery`, `reconcileOfflineRecords` |
| `issue: 157` | 19 | adapters and adapter targets (browser AC is in #157) |
| `issue: 568` | 2 | `establishCredential`, `resetCredential` |
| `issue: 571` | 1 | `beginRecovery` (filed by this PR) |
| `issue: 572` | 4 | project and environment audit query/export (filed by this PR) |

Counts are from the file at merge time; the tests, not this table, are the
source of truth.

## Not in scope here

- Proving each journey end to end from its surface (browser acceptance) is
  #504. The registry proves reach and a served surface, and says so in its
  header.
- The api-cli-surface ADR is locked and was not amended. The registry header
  cites it and the spellings document; the exception classes match the ADR's
  amended text (identity-protocol restatement, parity principle).

## Gotchas for the next person

- The `freeze-guard` and `web-go` workflow edits are loaded from the base
  branch on PRs (`pull_request_target`), so they first execute on the main push
  after merge. The Go test itself ran on this PR through the `test` job.
- `check-parity-issues.sh` extracts `issue: N` from non-comment lines with
  grep; keep prose like that out of `note:` values or add the issue to the
  stub in the fixture.
- Flipping a row from `issue` to `webui` is the only registry change a surface
  PR should need; the test names the exact row and the missing import.

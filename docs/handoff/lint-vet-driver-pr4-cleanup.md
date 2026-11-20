# Lint vet driver PR 4: remove the go/packages harness

Work: PR 4 of `docs/adr/lint-analysis-driver.md` (branch `dunky13/lint-vettool-cleanup`, stacked on `dunky13/lint-vettool-rawsql`, PR #1487, which stacks on #1483 and #1464). This is the last PR of the stack. The ADR's step 4 amendment is proposed, not owner-accepted.

## Result

- **Removed.** `LoadRepo`, `LoadRepoIn`, `Load`, `LoadIn`, the per-context caches, `flatten`, `RunOver` and `Result`, every old `Check*` entry point and `Check.OnDisk`, `diskInputs`, `ProductionOnly`, the `Test*Repo` loops, and the parity legs (`TestVetDriverParity`, `requireRawSQLParity`, the fixtures' `old`/`oldIn` legs). Line delta against PR 3's head (`a781d3389`): 50 files, +636 and -1,893 lines. Non-test files of `internal/lint`: +39 -845; test files of `internal/lint`: +497 -911; `internal/isolation`: +85 -99; CI definitions: +14 -37.
- **Kept.** `Contexts` and the `BuildContext` helpers (`Env`, `Overlay`, `WriteOverlay`), because the vet driver and the fixtures use them. `osSource` (documented deviation from the ADR's step 2): the exported helpers outside the vet driver and `CheckUnanalyzedGoFiles`, which runs in the test process, still read the disk. A proposed amendment, not an accepted decision.
- **`internal/isolation` callers.** `TestInvariant07`, `08`, `09a`, `09`, `09b`, `TestInvariantAuditAppendOnly` and `TestInvariantAuditRedaction` were thin wrappers over analyzers that `TestVetDriverRepo` already runs over the whole module under every context (it fails on any finding and on an unanalyzed target package). They are deleted, with a comment where each sat naming the analyzer that now owns the invariant. No invariant is dropped. `TestInvariant12CacheDiscipline` is not an analyzer: its sweep now walks the module with `go/parser` (`packageScopeNames`), over every file whatever its build constraints, test files included, so it reads more than the old default-context load did.
- **Release record.** `docs/release/acceptance-1.0.md` row A6 named `TestSensitiveFormattingRepo` and `TestInvariantAuditRedaction`; both are replaced by `TestVetDriverRepo`, because `scripts/ci/qualification` refuses an acceptance row that names a missing test.
- **Reviewed-pin updater.** `TestUpdateReviewedPins` runs the vet tool once per context with `-rawsql.dump -rawsql.native=linux`. The pass reports `rawdump` summaries (`fn <key> <hash>` for each protocol owner and reviewed caller, `dep <owner> <helper> <hash>` for each helper an owner reaches), the test rewrites the pins from them, and then verifies the result with `rawSQLSummaryFindings` for each context. `checkReviewedPinAuthorities` and its tests are unchanged, so the updater still refuses new owners, callers and helpers. Run on the repository it left the pins byte-identical (518.7 s).
- **Test adapter.** `analyzer_helpers_test.go` holds the only `go/packages` use left: `loadFixture` (a handful of type-checked packages), `runAnalyzer` (one analyzer over them) and `checkRawSQL` (the RawSQL pass with facts flowing dependency to importer, then the summary aggregate). The typed-fixture RawSQL, MCP fixture, alert collector, artifact harness and driver-handle tests use them. It also holds test-only copies of `rawSQLDeclarations`, `rawSQLDependencies` and `rawSQLBodyHash`, kept as an independent oracle for the hashes (production code uses the facts).
- **CI definitions (separate commit).** The `race-analyzers` group in `reliability.yml` listed the isolation tests that called `LoadRepo`. Its only remaining member is `TestInvariant12CacheDiscipline`, so the planner list names it and `scripts/ci/race_isolation_workflow_test.go` finds members by their call to `packageScopeNames`. The race-shard exclusion for `internal/lint` is untouched.

## Evidence

- `go test -count=1 -timeout 40m -p 2 ./internal/lint/` passes at `a16abc2bc`, cold and warm (memory table below).
- `go test ./internal/isolation/` passes (1,180.6 s). PostgreSQL-backed tests skip without a DSN here, so the engine-backed probes were not exercised locally.
- `go build ./...` and `go vet ./...` pass at `133567358` (every package that imported a removed symbol is covered by the whole-module vet).
- `go test ./internal/boundary/ ./scripts/ci/...` pass (`scripts/ci/qualification` after the A6 edit, `scripts/ci` in 598.9 s).
- Negative fixtures: `TestVetDriverFixtures` under the four contexts; the jsonv1 and appendonly fixtures now assert their aggregate findings (`wantAggregate`), which only the parity leg compared before.

### Memory (process tree of the `go test` invocation only)

Method: the queued command is started, the `go test` PID is found by its command line, and `ps -o pid=,ppid=,rss=,comm=` is sampled every 0.5 s. RSS is summed over that PID and its descendants (walking `ppid`), and the maximum sum and the largest single process are recorded. Cold means a freshly built vet tool binary (a unique `-ldflags` value, so no cached vet result applies); `go clean -cache` was not used, so the compiler cache was warm. Warm reuses the cached vet results.

| Run | Wall | Peak tree RSS | Largest process | Result |
|---|---|---|---|---|
| Cold, before the fix (`133567358`) | 358.6 s | 897 MiB | 825 MiB (`lint.test`) | pass; largest process over 0.8 GB |
| Cold, final (`a16abc2bc`) | 347.0 s | 826 MiB | 527 MiB (`lintvet`) | pass; target met |
| Warm, final (`a16abc2bc`) | 90.7 s | 537 MiB | 472 MiB (`lint.test`) | pass; target met |

What dominated: `TestMCPFixtureHelperPinsMatchTargetPlatform` cached five type-checked module slices keyed by (target, context), all distinct, and peaked at 851 MiB alone. Loading each row once and letting it go brings it to 271 MiB. The largest remaining processes are the vet tool (527 MiB) and `TestAlertCollectorSnapshotProtocolRejectsDrift` (466 MiB).

## Remaining work and executable next steps

1. Merge the stack together once green: #1464 (carries CI-definition changes that must land via a `main` hotfix first), #1483, #1487 and this PR.
2. Follow-ups (CI definitions, not done here): lift the race-shard exclusion for `internal/lint`, and retire the `race-analyzers` job (its one remaining sweep no longer needs `GOMAXPROCS=1`).
3. Follow-up issue for the deferred `handles` test sites: #1488. Redactfmt's deferred sites are tracked by #1468. Both stay pinned by `strictDeferred` and `deferred_sites.json`.
4. Re-run: `agent-heavy run --label 'lint full' -- go test -count=1 -timeout 40m -p 2 ./internal/lint/`, then `scripts/ci/golangci-lint-new.sh origin/dev`.

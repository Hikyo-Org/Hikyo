# Lint vet driver PR 3: RawSQL with facts and summaries

Work: PR 3 of `docs/adr/lint-analysis-driver.md` (branch `dunky13/lint-vettool-rawsql`, stacked on `dunky13/lint-vettool-inputs`, PR #1483, which stacks on #1464). The ADR's step 3 amendment is proposed, except the production-only scope, which the owner accepted on 2026-11-20 (see below).

## Result

- `rawsql` is a `go/analysis` pass in the vet tool (`internal/lint/rawsql_vet.go`). The old harness (`CheckRawSQL`) is unchanged in behavior and still runs.
- **Facts.** Object facts (`rawSQLFunc`) on every production function another package can name (exported function or method): owner key (`file:Name`), body hash, and the helpers it reaches (closure, cut at reviewed owners). vet hands a package only its direct imports' facts and method facts, so a fact is self-contained: an owner resolves a helper two imports away through the closure its direct import exported (fixture: `deep/` is two imports below `owner/`). Same-package references resolve from the pass's own functions. Positions of imported functions are never used as owner keys.
- **Summaries** (reserved category, kinds valid for `rawsql` only): `rawowner`, `rawcaller`, `rawhelper`, `rawunreached`. `RawSQLSummaryFindings` aggregates them per context in the old wording: absent protocol owner, retired caller pin, helper dependency pin absent from source, or no longer reached. It reads the pins through `OpenInputs` with the digest the vet run used.
- **Inputs.** `rawsql` declares `internal/lint/testdata/reviewed_pins.json` and both `internal/store/queries/{sqlite,postgres}` directories (generated-source detection, via `generatedSQLSourceIn`). The pin JSON is decoded with `jsonwire.Decode` and `json.RejectUnknownMembers(true)` (`parseReviewedPins`, now also used by the embedded copy; duplicate members, trailing content and malformed input are refused: `TestParseReviewedPinsStrict`).
- **Context.** `-rawsql.context` is set by `runVet` for every run and is a cache-key flag; a missing or unknown value is a refusal. `BuildHelpers`, `BuildDependencies`, `build_contexts` and `rawProtocolsForGOOS` are unchanged; the host GOOS comes from the tool's `runtime.GOOS`.
- `-rawsql.prefix` (default the module) lets the fixture select its own package tree.
- `RawSQL` is registered in `Analyzers()`, not `Checks()`: `RunOver` cannot supply facts, and the parity loop over `Checks()` would panic.

## Evidence

- Fixture `testdata/badrawsql` (run by `TestVetDriverFixtures` under all four contexts): unpinned call, stale body hash, stale helper dependency through an imported package, stale dependency two imports away (facts), missing dependency, unreviewed caller of a restricted protocol, a method of a package the owner does not import reached through an imported helper (`deep.Q.Lit` via `helper.NewQ`), an unexported helper in an imported package (`helper.quote2`), restricted callers in another package (`other.Sneak`, `other.sneakLocal`), and a clean owner, with `// want`. Findings with no source line are asserted exactly by `wantAggregate`: absent owner (all contexts), retired caller pin, dropped and absent helper pins, and a `build_contexts: ["windows"]` owner absent only under windows.
- Parity: `TestVetDriverParity` compares old `CheckRawSQL` with the driver (per-package findings plus the aggregate) on the repository and on the fixture, under every context. The repository leg is vacuous when the repository is clean; the fixture leg is not (the guard fails on an empty old result).
- Stale digest: `TestVetDriverEveryInputAnalyzerRefusesStaleDigest` now covers `rawsql` (every rawsql finding is an `inputs refused:` refusal).
- Absent directories: `TestInputAnalyzersReportAbsentDirectories` covers RawSQL: with the pins present and the query directories absent, a generated sqlc file is judged as production code (fails closed, as `generatedSQLSourceIn` already did).
- Summary kind validation: `TestDecodeVetOutput` cases; aggregate refusals, including unknown kinds and multi-field `rawowner`/`rawhelper` subjects: `TestRawSQLSummaryFindingsRefusals`.
- Measurements (this machine, queued): At `09aa6ba4f`: `TestVetDriverRepo` cold (new tool binary, vet results not cached) 307 s wall, 609 MB max RSS of the test process; warm 37.5 s. `TestVetDriverParity` plus the stale-digest, bad-input and decode tests: 308 s wall, 4.57 GB max RSS of the test process (the old loader holds all four contexts). Full `go test -count=1 -timeout 40m -p 2 ./internal/lint/` (queued, passing): 421 s wall, 4.67 GB max RSS of the test process at `09aa6ba4f`; 884.9 s test time at `e6f19b00d` (loaded host, RSS not re-measured). No baseline comparison was run for PR 2 and no reduction is claimed; PRs 1 to 3 run both harnesses, so these numbers are interim.

## Newly visible violations

None from the driver itself. Owner decision (2026-11-20): RawSQL's production-only scope is accepted. It keeps excluding `_test.go` and generated sqlc files, which leaves about 1,100 raw-SQL-shaped call sites in about 212 test files (in-package and external) out of scope (a grep estimate and an upper bound, not a typed count), together with those files' references to restricted protocols and the SQL-outside-a-named-function check. The exclusion pre-dates the vet driver: the old harness skipped them too. `TestVetDriverRepo` reports no `rawsql` finding and no aggregate finding under any context. `strictDeferred` and `deferred_sites.json` are unchanged; no allowlist or pin was added.

## Known limits and costs

- Declaring a fact type makes vet run `rawsql` on the standard library and dependencies; it returns at once outside the module.
- Every module package opens the declared inputs, so an edit to `reviewed_pins.json` or a `.sql` query changes flags that apply to every package and re-vets every package, standard library and dependencies included, in all four contexts. Editing the body of any production function in some exported function's helper closure (an exported method's own body included) changes its package's facts; the re-vet then spreads to transitive importers whenever their own facts change, and always for methods, whose facts every importer re-exports.
- Facts carry a closure per exported function, so their size grows with the helper graph; the fact is not truncated. Watch vetx size in the build cache if a hub package's closure grows.
- A reference to a method of a type in a package the referrer does not import resolves through the method's object fact (vet re-exports method facts). A package-level function can only be named by importing its package, so direct-import facts suffice.
- The updater is unchanged (below).

## The reviewed-pin updater: decision and what PR 4 must do

`TestUpdateReviewedPins` (`-update-reviewed-pins`, `reviewed_pins_update_test.go` lines 78 to 176) stays on the old loader in this PR. It calls `LoadIn(context, Module+"/...")` per context, then `rawSQLDeclarations`, `rawSQLBodyHash` and `rawSQLDependencies` to compute the actual owner, caller and dependency hashes, and finally `CheckRawSQL` to verify. Moving it was not simple: the driver reports findings, not the hashes themselves. PR 4 removes `LoadIn` and `LoadRepo`, so it must:

1. Obtain the actual owner hash, caller hashes and dependency closure hashes per context from the vet tool. The facts already hold them (`rawSQLFunc`); add a flag (for example `-rawsql.dump`) under which the pass reports them as summaries (or writes them through a declared output), and decode them in the test.
2. Replace the final `CheckRawSQL` verification with a vet run plus `RawSQLSummaryFindings` for each context (`rawSQLAggregate` in `rawsql_vet_test.go`).
3. Keep `checkReviewedPinAuthorities`: the updater must not widen authorities (new owners, callers or helpers).
4. Delete the old-harness pieces: `CheckRawSQL`, `rawSQLDeclarations`, `rawSQLDependencies` and `rawSQLHelperReferences`, the `oldIn` fixture leg and `requireRawSQLParity`, and `generatedSQLSource` (the bool wrapper) goes with `osSource`.

`internal/isolation` has no RawSQL entry-point callers (`grep CheckRawSQL` finds only `internal/lint`).

## Next steps

1. Merge or rebase PR 3 once #1483 and #1464 are accepted; do not merge it alone (the stack merges to `dev` together once PR 4 is green).
2. `go test -count=1 -timeout 40m -p 2 ./internal/lint/` (queued), `go test ./internal/boundary/`, `scripts/ci/golangci-lint-new.sh origin/dev`.
3. Start PR 4 from this branch with the updater plan above.

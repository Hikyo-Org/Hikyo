# Lint analysis driver

Status: accepted. Decision date: 2026-11-20. The owner accepted every
recommended option below. Follows
[#1453](https://github.com/Hikyo-Org/Hikyo/pull/1453).

## Context

The repository analyzers in `internal/lint` load the whole module with
`go/packages` (syntax and full type information) under each analysis context
in `lint.Contexts`, cache every load for the test process, and run each check
over all packages at once. After #1453 the four cached loads still peak at
about 3.9 GB footprint and 2.2 GB max RSS, and the suite takes about 21
minutes.

A spike ran two analyzers as `golang.org/x/tools/go/analysis` passes through
`go vet -vettool` (unitchecker). Each package was analyzed in its own process
from export data, and the build cache replays results for unchanged packages.
Per context, the largest process peaked at 0.4 to 0.6 GB, the whole process
tree at about 0.5 GB with compiles cached, and a warm rerun took about 6 s.
The spike also established that:

- vet analyzes the in-package test variant (`P [P.test]`). The `flatten`
  helper keeps the production variant, so most current checks never see
  in-package `_test.go` files; vet found 33 such findings.
- vet cannot skip test files. The windows reason for `ProductionOnly`
  (`internal/upgradecustody/local_test.go`) no longer applies; that file is now
  unix-tagged.
- Analyzer flag values are part of the vet cache key. Non-Go files an analyzer
  reads from disk are not, so editing them replays a stale result.
- Facts propagate along imports, but declaring a fact type makes vet analyze
  stdlib and third-party dependencies too.

## Decisions

1. **Driver.** Analyzers become `analysis.Analyzer` values, built into one vet
   tool under `internal/lint/cmd/`. `go test ./internal/lint` stays the single
   entry point: a test builds the tool, runs `go vet -json -p 2` once per
   context with that context's environment and overlay flag, decodes the JSON
   output into typed structures, and fails on diagnostics. CI workflows and the
   analysis shard configuration keep invoking `go test ./internal/lint`.
2. **Non-Go inputs.** Each analyzer that reads non-Go files (`.sql`, pin JSON,
   migrations) declares them. The driver hashes the declared inputs and passes
   the digest as an analyzer flag, so any edit changes the vet cache key. The
   analyzer reads those inputs only through a helper that rejects an undeclared
   path and a digest mismatch, so a stale or incomplete declaration fails
   closed instead of replaying an old result.
3. **Whole-repository checks.** Checks that need every package at once (stale
   or absent RawSQL pins, service-seam and fence coverage, target-package
   presence) emit per-package summaries as diagnostics in a reserved category.
   The driver separates them from findings and aggregates them per context.
   Facts are used only for information that follows imports, such as RawSQL
   helper dependencies.
4. **Test variants and hidden violations.** Analyzers judge the files of the
   package variant they are given, test files included. A rule such as "this
   package handles `authz.Proof`" is decided from those files. Violations that
   become visible are fixed in the port PRs, not allowlisted.

   Amendment (PR 1): a test whose subject is a nil `authz.Proof` (a boundary
   test asserting the store refuses it), or that can reach no real proof, may
   carry an inline `// forgeguard:nil-proof <reason>` marker on the flagged
   line. forgeguard honours it only in `_test.go` files and requires a reason;
   a marker in a production file, or without a reason, is a finding of its own.
   It is a reviewed, reasoned, test-only exemption at the site, not an inventory
   entry. Test helpers that seed an isolated fixture database may likewise
   carry `fence:fixture <reason>`, honoured by the fence analyzer only in
   `_test.go` files; `fence:exempt` keeps its original meaning.
5. **Rollout.** A stack of PRs, each runs both harnesses for the analyzers it
   moves until every old finding is also reported by the new driver. The parity
   leg runs the same analyzer core through the old loader, so it detects only
   loader and variant differences (what the go/packages walk sees against what
   vet sees); it is not an independent check of the analyzer logic, which the
   `// want` fixtures cover:
   1. driver, summary aggregation, and the per-package analyzers;
   2. input hashing and the analyzers that read non-Go inputs;
   3. RawSQL with facts and summaries;
   4. removal of `LoadRepo`, `flatten`, and the old harness, including the
      `internal/isolation` callers.

   Negative fixtures keep their packages under `testdata/` and run through the
   same vet driver, with expected diagnostics written as `// want "regexp"`
   comments. analysistest is not used, because it loads every dependency from
   source, which is the cost this decision removes.
6. **Interim.** `flatten` is not changed on its own; the port closes the gap.

## Consequences

- Acceptance for the final stack: the full `go test ./internal/lint` stays
  green, and its measured peak whole-tree RSS stays under 1.5 GB with no single
  process above 0.8 GB.
- All four contexts analyze test files. `ProductionOnly` stays only while the
  boundary test or `prepare-lint-cache` need it.
- The race shard exclusion for `internal/lint` stays until the old harness is
  removed.
- A changed vet tool binary invalidates every cached result, so analyzer
  changes still pay one full run.

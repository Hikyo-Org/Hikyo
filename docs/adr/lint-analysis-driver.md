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

   Owner decision (2026-11-20): no inline exemption markers. A nil
   `authz.Proof` is spelled only in `internal/authz`, which forgeguard exempts
   with its tests. Store-boundary tests present a real but invalid proof (for
   example one captured from a finished transaction), and test helpers that
   seed an isolated fixture database seal through `internal/testutil/sealfixture`
   instead of carrying an exemption in `internal/service`. forgeguard's stated
   limits: an unassigned local `Proof` passed on, an omitted `Proof` struct
   field, a naked named return and a generic zero value each still yield a nil
   proof that the guard does not see; the store refuses it at runtime, which is
   the enforcement.
5. **Rollout.** A stack of PRs, each runs both harnesses for the analyzers it
   moves until every old finding is also reported by the new driver. The parity
   leg runs the same analyzer core through the old loader, so it detects only
   loader and variant differences (what the go/packages walk sees against what
   vet sees); it is not an independent check of the analyzer logic, which the
   `// want` fixtures cover:
   1. driver, summary aggregation, and the per-package analyzers (including
      `recoverylookup` and `containmentsetters`, the name-confinement checks
      that arrived on `dev`: they read only Go syntax of the package they are
      given, and their stale-allowlist and target-presence results travel as
      summaries);
   2. input hashing and the analyzers that read non-Go inputs: `containmentgates`
      (with its `sqlscope` scanner), `sqlpredicate`, `reencryptcoverage`,
      `stablecopies`, `appendonly`, `grantlock`, `denialwriter` and `jsonv1`.
      RawSQL stays in step 3. See the step 2 amendment below;
   3. RawSQL with facts and summaries;
   4. removal of `LoadRepo`, `flatten`, and the old harness, including the
      `internal/isolation` callers.

   Negative fixtures keep their packages under `testdata/` and run through the
   same vet driver, with expected diagnostics written as `// want "regexp"`
   comments. analysistest is not used, because it loads every dependency from
   source, which is the cost this decision removes.
6. **Interim.** `flatten` is not changed on its own; the port closes the gap.

## Consequences

- The memory and wall-time reduction lands when the old harness is removed
  (PR 4 of the rollout). Until then `LoadRepoIn` still caches all four
  contexts for the process and the vet driver runs on top of it, so PRs 1 to 3
  raise the peak memory and wall time of `go test ./internal/lint` compared
  with the base, and the CI budgets are sized for that interim cost. The stack
  (PRs 1 to 4) merges to `dev` together, once PR 4 is green.
- Acceptance for the final stack: the full `go test ./internal/lint` stays
  green, and its measured peak whole-tree RSS stays under 1.5 GB with no single
  process above 0.8 GB.
- All four contexts analyze test files. `ProductionOnly` stays only while the
  boundary test or `prepare-lint-cache` need it.
- The race shard exclusion for `internal/lint` stays until the old harness is
  removed.
- A changed vet tool binary invalidates every cached result, so analyzer
  changes still pay one full run.

## Amendment: step 2 (input hashing)

Decided while implementing step 2; accepted by the same owner decision that
accepted the ADR, as an implementation of decision 2 and 3.

- **Helper.** `internal/lint/inputs.go`. An analyzer declares `InputSpec`s
  (a file, or a directory with extensions, optionally recursive) relative to a
  root. The driver hashes them and passes `-<analyzer>.root` and
  `-<analyzer>.inputs`. The analyzer snapshots the declared files once, refuses
  unless the digest equals the flag, and serves reads only from the snapshot, so
  a file edited after hashing cannot change what it sees. An undeclared path,
  a missing flag and a digest mismatch are refusals, reported as findings at
  the anchor's package clause (not as a vet error, so `// want` can state
  them). A declared file that is absent hashes as absent and reads as not-exist.
- **Never declare analyzed Go files.** Vet flags are part of every package's
  action ID, so any edit to a declared input re-vets every package of the
  context. Declared inputs are therefore limited to files vet does not key on
  (`.sql`, the generated query APIs, the `scripts/ci` copies) plus the test
  files that `reviewed_pins.json` cites for scoped queries (read by
  `sqlpredicate`; an edit to one of them re-vets the context, which is the cost
  of that check's old behavior).
- **File-input analyzers run once, on an anchor package** (`internal/store`,
  `internal/jsonwire`) and report a target summary, so a renamed anchor is a
  finding. `grantlock` and `denialwriter` are per-package analyzers that open
  the declared query files only in packages that can reach a generated query
  package (`grantlock`) or are the resolution surface (`denialwriter`).
- **`appendonly` splits.** Its `.sql` side is a declared input; the
  `synchronous_commit` ban over Go files runs per package on the package's own
  files (vet keys on them), and Go files no analyzed package contains are
  judged by the test-process check below.
- **`jsonv1` deviates from decision 3's pure summary shape.** Each analyzed
  package judges its files' imports and reports a `gofile` summary per file;
  the package allowed to import `encoding/json` reports an `exception` summary.
  `CheckUnanalyzedGoFiles` runs in the test process after every context: it
  parses the Go files of the repository that no context analyzed (excluded by
  build tags in every context, lone programs), applies the jsonv1 and
  `synchronous_commit` rules to them, and reports a stale exception when no
  context saw it import. It runs on every test run, so it is never a replayed
  vet result. The check only runs when every context ran.
- `Check.OnDisk` builds the same analyzer reading straight from disk, for the
  old harness and the parity leg; the registered analyzers refuse without the
  driver's flags.

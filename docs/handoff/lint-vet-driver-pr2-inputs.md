# Lint vet driver PR 2: declared inputs and the non-Go-input analyzers

Work: PR 2 of `docs/adr/lint-analysis-driver.md` (branch `dunky13/lint-vettool-inputs`, stacked on `dunky13/lint-vettool-driver`, PR #1464). The owner accepted only the `jsonv1` hybrid's deviation from decision 3 (2026-11-20). The rest of the ADR's step 2 amendment is implementation notes for this PR, not owner-accepted decisions.

## Result

- Input hashing: `internal/lint/inputs.go`. Analyzers declare `InputSpec`s; the driver test hashes them and passes `-<analyzer>.root` and `-<analyzer>.inputs`; the analyzer snapshots the declared files, refuses on a digest mismatch, a missing flag or an undeclared path, and reads only from the snapshot. A refusal is a finding at the anchor's package clause.
- Ported to the vet tool (old harness entry points kept): `containmentgates` (with `sqlscope`), `sqlpredicate`, `reencryptcoverage`, `stablecopies`, `appendonly`, `grantlock`, `denialwriter`, `jsonv1`.
- Not ported: RawSQL (PR 3). Nothing else from the PR 1 "Not ported" list remains.
- Newly visible violations: none. `grantlock` (every module package, test files included), `denialwriter`, and the per-package `synchronous_commit` scan found nothing in the in-package test files of any context. `strictDeferred` is unchanged (`handles`, `redactfmt`); `deferred_sites.json` is unchanged. Nothing was allowlisted.
- Fail-closed checks: every input-reading analyzer has a `// want` fixture under `internal/lint/testdata/` (`badcontainmentgates`, `badsqlpredicate`, `badreencrypt`, `badstablecopies`, `badappendonly`, plus `badgrant` and `badauthn` carrying wants); `jsonv1` has `badjsonv1`. `TestVetDriverEveryInputAnalyzerRefusesStaleDigest` asserts the fixtures with inputs cover exactly `InputAnalyzers()`. `TestVetDriverRefusesBadInputs` (stale digest, absent flags, undeclared path, `badinputsrefusal`) and `TestInputsRefuseUndeclaredPathAndStaleDigest` (including absent, symlinked and non-directory declared directories) cover the refusals; `TestInputAnalyzersReportAbsentDirectories` runs each file-input analyzer with its declared directories absent, through both readers, plus the old `Check*` entry points that read declared paths (`CheckAuditAppendOnly`, `CheckSQLPredicates`, `CheckReencryptCoverage`, `CheckStableCopies`); `CheckContainmentGates` takes parsed queries, so for it the test covers the reading helper `parseQueries` through both readers.

## Per-analyzer decisions

| Analyzer | Decision | Why |
|---|---|---|
| containmentgates + sqlscope | ported, file-input, anchor `internal/store` | reads `.sql` query text only |
| sqlpredicate | ported, file-input, anchor `internal/store` | reads migrations, queries, generated query APIs and the test files `reviewed_pins.json` cites |
| reencryptcoverage | ported, file-input | reads sqlite migrations |
| stablecopies | ported, file-input, anchor `internal/jsonwire` | reads `scripts/ci` programs that are not analyzed packages |
| appendonly | ported, split | `.sql` side declared; Go `synchronous_commit` ban per package of `internal/store/...`; unanalyzed Go files judged in the test process |
| grantlock | ported, per package | writer set derived from declared query files; only packages that reach a generated query package open them |
| denialwriter | ported, per package | mutating set derived from declared query files; runs on `internal/store/authn` |
| jsonv1 | ported, hybrid | per-package imports plus `gofile`/`exception` summaries; `CheckUnanalyzedGoFiles` in the test process for files no context analyzes and for a stale exception |

## Evidence

See the PR body for command output and measurements. Wall time and memory numbers are interim: PRs 1 to 3 run both harnesses.

## Known limits and costs

- Any edit to a declared input re-vets every package of the context (the flag is part of every action ID). Declared inputs are therefore the files the anchor package's action ID does not cover; the one exception is `internal/jsonwire/jsonwire.go` (the anchor's own file, see the ADR amendment). The test files cited by the reviewed pins follow the rule: the anchor's action ID does not cover them.
- `prepare-lint-cache` warms only compiles (`go list -export -test`), not vet results, so CI stays as cold for vet as before; the file was not changed.
- The unanalyzed-Go-file check runs only when every context ran; a focused `-run 'TestVetDriverRepo/<ctx>'` skips it with a log line.
- Symlinks: the snapshot reads through `os.OpenRoot(root)` and refuses any irregular declared file or a symlinked parent. The old-harness disk reader (`osSource`) has no root, keeps `pathutil.ReadFileWithin` on the file's own directory, and still follows a symlinked parent; it goes in PR 4.
- The `-root`/anchor tie: file-input analyzers refuse a `-root` that does not contain every file of the anchor package (`TestVetDriverRefusesBadInputs`, "foreign root with a matching digest"). `grantlock` and `denialwriter` have no anchor, so it does not apply; their root is bound only by the digest of the declared query files, and a hand-run vet tool is trusted.
- `redactfmt` narrowing remains issue #1468; `redaction.go` is untouched.

## Remaining work

- PR 3: RawSQL with facts and summaries.
- PR 4: remove `LoadRepo`, `flatten`, the old harness and the `Check.OnDisk` variants, and the `internal/isolation` callers; the old `Check*` entry points here go with it.

## Next steps

1. `go test -count=1 -timeout 40m ./internal/lint/` (queue it as your environment requires; it takes 10 to 16 minutes).
2. `go test ./internal/boundary/` and `scripts/ci/golangci-lint-new.sh origin/dev`.
3. Start PR 3 from this branch.

# Lint vet driver PR 2: declared inputs and the non-Go-input analyzers

Work: PR 2 of `docs/adr/lint-analysis-driver.md` (branch `dunky13/lint-vettool-inputs`, stacked on `dunky13/lint-vettool-driver`, PR #1464). Step 2 amendment is recorded in the ADR.

## Result

- Input hashing: `internal/lint/inputs.go`. Analyzers declare `InputSpec`s; the driver test hashes them and passes `-<analyzer>.root` and `-<analyzer>.inputs`; the analyzer snapshots the declared files, refuses on a digest mismatch, a missing flag or an undeclared path, and reads only from the snapshot. A refusal is a finding at the anchor's package clause.
- Ported to the vet tool (old harness entry points kept): `containmentgates` (with `sqlscope`), `sqlpredicate`, `reencryptcoverage`, `stablecopies`, `appendonly`, `grantlock`, `denialwriter`, `jsonv1`.
- Not ported: RawSQL (PR 3). Nothing else from the PR 1 "Not ported" list remains.
- Newly visible violations: none. `grantlock` (every module package, test files included), `denialwriter`, and the per-package `synchronous_commit` scan found nothing in the in-package test files of any context. `strictDeferred` is unchanged (`handles`, `redactfmt`); `deferred_sites.json` is unchanged. Nothing was allowlisted.
- Fail-closed checks: each analyzer has a `// want` fixture under `internal/lint/testdata/` (`badcontainmentgates`, `badsqlpredicate`, `badreencrypt`, `badstablecopies`, `badjsonv1`, plus the existing `badgrant` and `badauthn` now carrying wants). `TestVetDriverRefusesBadInputs` (stale digest, absent flags, undeclared path, with the `badinputsrefusal` want fixture) and `TestVetDriverEveryInputAnalyzerRefusesStaleDigest` cover the refusals; `TestInputsRefuseUndeclaredPathAndStaleDigest` covers the helper.

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

- Any edit to a declared input re-vets every package of the context (the flag is part of every action ID). Declared inputs are therefore never analyzed Go files, except the test files cited by the reviewed pins, which `sqlpredicate` reads from disk as it always did.
- `prepare-lint-cache` warms only compiles (`go list -export -test`), not vet results, so CI stays as cold for vet as before; the file was not changed.
- The unanalyzed-Go-file check runs only when every context ran; a focused `-run 'TestVetDriverRepo/<ctx>'` skips it with a log line.
- Replacing symlink handling: the disk reader keeps `pathutil.ReadFileWithin`; the snapshot refuses any irregular declared file.
- `redactfmt` narrowing remains issue #1468; `redaction.go` is untouched.

## Remaining work

- PR 3: RawSQL with facts and summaries.
- PR 4: remove `LoadRepo`, `flatten`, the old harness and the `Check.OnDisk` variants, and the `internal/isolation` callers; the old `Check*` entry points here go with it.

## Next steps

1. `go test -count=1 -timeout 40m ./internal/lint/` (queue it: `/Users/developwent/.local/bin/agent-heavy run --label 'lint full' -- go test -count=1 -timeout 40m -p 2 ./internal/lint/`).
2. `go test ./internal/boundary/` and `scripts/ci/golangci-lint-new.sh origin/dev`.
3. Start PR 3 from this branch.

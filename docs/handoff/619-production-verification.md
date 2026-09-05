# Production verification gaps from #619

Baseline: `e114b56e2423a0996fa266fafbb59b561712d35d`.

The audit's runtime update-state reload and dynamic transaction isolation defects
are already fixed on this baseline. The dynamic runtime shares the serializable
adapter transaction helper; the CLI checks the state reload error. The capability
ledger already includes dynamic-secret browser management. These are verified
current facts, not additional changes in this slice.

Three remaining verification gaps are closed:

1. The core CI job starts a digest-pinned, disposable PostgreSQL TLS target and
   runs the real dynamic-provider lifecycle with the required flag enabled.
   Setup refuses plaintext TCP, exposes the target only on loopback, uses
   container-owned key permissions without host sudo, and cleans up only a
   container it successfully created. Only the public test CA remains outside
   the container after setup. CI runner disposal owns successful-run cleanup;
   local users can remove the named `HIKYO_TEST_DYNAMIC_PG_CONTAINER` afterward.
2. The audit invariant inspects the effective migrated schema on each engine.
   PostgreSQL's `commit_seq` export-order column is explicitly pinned; SQLite
   uses its serialized writer's `seq`. Later `ALTER TABLE` changes can no longer
   evade a parser that saw only the original `CREATE TABLE` statement.
3. CI executes the existing analysis-shard fixture suite, proving complete,
   disjoint package/fuzz/isolation planning rather than leaving its test orphaned.

The unreachable-provider fixture now explicitly permits loopback so it reaches
the refused-port network path, and requires `ErrUnreachable`. The separate
default-deny egress fixture still proves refusal before any private dial.

Validation: both-engine `TestInvariantAuditNoAggregates` passed; all nine
dynamic-provider tests passed against real PostgreSQL over verified TLS; analysis
shard fixtures, ShellCheck, actionlint and `git diff --check` passed. Standards
review was clean. Spec review found the unreachable fixture's missing allowance;
the fix passed its second review. Exact-head CI and merge evidence are recorded
in the HTML release reports when available.

The broader #619 cleanup remains a separate maintainability effort. No tests,
security gates or product features were removed to obtain green results.

## CI integration repair

After MCP PR #639 merged, the core job failed before running tests: Corepack
selected pnpm 11.25.0 from the repository root, while the new tool package pins
11.24.0. The `--dir` flag is consumed by pnpm after Corepack chooses its version.
The workflow now starts in the package directory, and the conformance script
changes directory before invoking its tools. Frozen installation selects
11.24.0; real Inspector and all pinned conformance scenarios pass. No version
check is suppressed and no dependency pin is changed.

Trusted-workflow bootstrap follow-up: the second CI run still executes the base branch command. Added a private root packageManager pin matching all nested packages (pnpm 11.24.0), validated the old root --dir invocation with a fresh Corepack cache. This preserves trusted orchestration. Newly added CI steps require post-merge main proof; their exact local equivalents already passed.

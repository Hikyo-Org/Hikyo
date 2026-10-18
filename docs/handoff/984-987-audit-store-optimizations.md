# Handoff: #984 and #987 audit store optimizations

Issues: [#984](https://github.com/Hikyo-Org/Hikyo/issues/984) and
[#987](https://github.com/Hikyo-Org/Hikyo/issues/987).
Base: `cb78b6be111029335e744aa3e4dcc956d5194ba1`.
Branch: `dunky13/audit-store-optimizations`.
Agent endpoint: signed and DCO-signed-off commit, push and PR. Agents do not
enqueue, merge, enable auto-merge, release or deploy. Human queue actions remain
under the user's control.

## Implementation and invariants

- The existing admission transaction owner retains one authoritative actor kind
  for consecutive audit inserts. Both trails share that transaction-owned state.
  Ordinary statements invalidate it before and after execution; commit and
  rollback disable it. A prepared SQLite statement disables reuse because its
  later executions do not return through the owner. PostgreSQL reuse requires
  SERIALIZABLE or REPEATABLE READ. Other DBTX adapters retain uncached lookups.
- Proof/event verification still runs before actor resolution. The unchanged
  `auditrow.ResolveActorClass` still rejects asserted human/machine classes,
  enforces the system-proof rules and clears actor/credential ids for a missing
  principal. Kind changes, deletion and recreation are observed within the same
  transaction. Actor credentials, authority, row construction and durable insert
  behavior remain on the existing paths. No service emitter supplies a class.
- `AuditFilter.Compile` returns a separate immutable matcher with split wildcard
  segments and a detached outcome slice. Interactive pages compile once across
  their bounded chunk loop. Exports compile after authorization and the durable
  INTENT, then reuse the matcher across pages. Changing a source filter requires
  compiling a new snapshot; it cannot mutate an existing matcher.
- The filter struct, normalized payload, export fingerprints, actor-name owner,
  case sensitivity, wildcard ordering/empty segments, cursors, page cap and
  16-chunk ceiling are unchanged. There are no migrations or new driver-authority
  exceptions. The derived identity slot is registered as `store.audit-actor-kind`
  with an exact declaration check in the cache invariant; it is not an
  authorization-result cache. The repository pin updater refreshes existing
  protocol-owner hashes and their helper closure without adding protocol
  authorities; the resulting diff was reviewed and normal lint passed.

## Evidence

Completed on the final production source with SQLite and PostgreSQL 18.6:

- Focused store/service audit, glob, page and normalization tests passed.
  Both-engine audit/isolation selection passed, including AuditCore, export
  commit ordering and cutoff registration, tenant-isolation probes, retention
  and the cache discipline invariant.
- Full lint and scoped vet passed. The official reviewed-pin updater passed;
  a semantic comparison confirms unchanged query/API contracts, protocol owner
  inventory, authority reasons and caller restrictions. Changes are existing
  owner hashes and the invalidate/disable helper closure, plus canonical JSON
  key ordering. SQLC generation passed with no tracked or untracked binding
  changes.
- The broad Go invocation passed all 121 test-bearing packages outside isolation;
  28 other packages reported no tests. This includes full store, service,
  boundary, lint, auditrow and transaction package runs. Isolation alone reached
  the timeout described below; the broad command therefore exited nonzero.

Additional evidence:

- For each engine and each trail, 300 actual generated inserts issued exactly
  one principal-kind query. A compiler overlay selecting untouched base source
  measured 300 queries for the same four cases. Warmed asserted principal
  classes were refused without another lookup. Kind changes, deletion, recreation and
  credential/authority attribution assertions passed. These count fixtures use
  minimal temporary schemas on public signed-admitted transaction owners.
  SQLite counts generated statements at the driver; PostgreSQL uses a temporary
  volatile kind projection and sequence. Every bulk lookup returns one existing
  principal, so one projection corresponds to one lookup; the base-source
  300-query control validates that instrumentation. Inspection SELECTs run after
  the warmed refusal probes. Migrated-schema durability is covered separately
  by AuditCore. Prepared mutation, RETURNING before Scan and independent owners
  retaining different kinds for the same actor ID also passed.
- Full `auditrow` and transaction package suites passed. The new resolver tests
  pin assertion refusal before lookup and authoritative lookup failures.
- Glob equivalence checks cover 14,641 pattern/string pairs against the prior
  algorithm. Matcher snapshot tests cover source string and outcome-slice edits;
  the existing row-filter table also compares compiled results with the prior
  predicate implementation.
- An earlier isolation compile was interrupted at the user's resource
  coordination request. It did not complete. Analyzer, vet, generation and
  benchmark work had not started when interrupted. No interrupted result counts
  as passing. Final reruns use the global `agent-heavy` queue and ephemeral Go
  settings `GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50`, with `-p 2`.

## Benchmarks

On darwin/arm64 (Apple M2 Pro), Go 1.27.2, with the settings above:

- `BenchmarkAuditFilter16000`, three samples of five 16,000-row predicate scans:
  wildcard-heavy matching measured 48,000 allocations and 2,816,000 to
  2,816,048 bytes per scan before, versus three allocations and 176 bytes with
  compilation included after. Exact matching remained zero bytes and zero
  allocations on both paths. Wildcard timings ranged from 5.566 to 9.867 ms
  before and 0.808 to 0.926 ms after; exact timings ranged from 0.517 to
  0.563 ms before and 0.255 to 0.588 ms after.
- Start/end workload snapshots recorded four active queue jobs at both ends of
  that benchmark, with one-minute host load 12.03 and 12.13. These are contended
  observations, not isolated speedup or production-latency evidence.
- `BenchmarkAuditBulkDisclosure300`, three samples of three operations:
  before measured 1,376,810 to 1,380,080 bytes and 33,329 to 33,338 allocations
  per operation; after measured 1,080,376 to 1,083,477 bytes and 24,662 to 24,672
  allocations. Timings overlapped: 16.456 to 18.543 ms before, and 16.428 to
  16.845 ms after. This sample does not establish a bulk latency improvement.
- Filter workload snapshots were taken at 20:18:13 and 20:18:23 UTC. Bulk-before
  snapshots at 20:46:03 and 20:46:16 UTC showed two then one active jobs, with
  one-minute load 4.82 then 4.60. Bulk-after snapshots at 20:46:16 and
  20:46:29 UTC showed one active job at both ends, with load 4.60 then 4.38.
  Operator-controlled overlap and changing host load prevent an isolated
  timing comparison. Full snapshots and failed-attempt logs remain outside Git
  for this session; they are ephemeral evidence.
- The full Go invocation reached its 30-minute isolation package timeout after
  386 top-level isolation tests completed, without a preceding assertion
  failure. Its raw log is retained. The compiled isolation inventory contains
  632 top-level tests. A finite controller subtracts completed top-level
  pass/skip results, runs the remaining 246 in sequential batches of at most 20
  anchored names with an 18-minute timeout per batch, and checks the completed
  union against the inventory. All 13 batches passed and the inventory check
  succeeded: 623 top-level tests passed and nine retained their existing skip
  conditions. The unsharded command remains recorded as a timeout, rather than
  a passing command.
- The final two-worker `graphify update . --no-cluster` completed successfully:
  57,124 nodes and 178,591 edges. Its 17 existing Astro syntax warnings limit
  extraction of those unrelated documentation components. This is structural
  navigation evidence, not runtime or security proof.

`BenchmarkAuditBulkDisclosure300` uses synthetic already-rendered secret cells,
local-principal authorization, disk-backed SQLite and a durable commit. It verifies 300
resulting disclosure records outside the timed section and restores the fixture
between samples. It excludes decryption, reauthentication, network and reset
costs. Baseline comparison uses the same benchmark with untouched base source
selected through a compiler overlay.

## Earlier attempts and boundaries

- An initial test-fixture build failed on two unused imports, corrected before
  the passing focused runs.
- A broader exploratory selection on the default PostgreSQL 14.24 installation
  failed in `TestExportPostgresManifestUsesCopySnapshotDuringMigration` and
  `TestPreparedPostgresPoolPreservesTransactionsAndCoordination` with
  `upgrade: corrupt control state`; in
  `TestPreparationExportUsesLiveSessionWithoutRuntimeAdmission/postgres` with
  `upgrade: unrecognized genesis`; and in
  `TestSelfConfigExpiredApplyRetryDoesNotRestartPreparation/postgres` and
  `TestSelfConfigApplyRetryReturnsOriginalJobAfterCollection/postgres` with
  `upgrade: corrupt control state`. These failures are preserved. All five
  passed on PostgreSQL 18.6 against both the changed and untouched base source.
  The earlier failures did not reproduce there; their precise cause was not
  established by that comparison.
- Benchmark verification initially lacked audit-read, then used an incorrect
  event-name literal. The fixture now creates the separate grant through the
  authorized Grants service before snapshot capture and filters with the
  canonical `audit.EventValueRevealed`. Both failed logs are preserved, and the
  unchanged 300-record assertion passes before and after.
- The first full-suite attempt exposed an authn import in the store test
  fixture. Instrumentation moved to the already approved isolation audit owner,
  using public signed-admitted transactions. The four-context importer check
  passes without changing an allowlist. That failed full run was interrupted
  before completion and its partial log retained.
- A queued mode probe measured inherited umask 077: requested 0644 became 0600.
  Untouched base source reproduced the CLI trust-store, signing-key-file and
  local/root-key loose-mode fixture failures under 077. Both base and changed
  source passed those same refusal tests with per-Go-child umask 022, matching
  the tool shell. The final batch uses that mask; no repository security checks,
  shared wrapper or production file-mode policy changed.
- The first global-queue batch was cancelled through its own job id when queue
  status reported four running jobs with limit one. The pin refresh was
  interrupted before producing output; no check from that batch completed.
  The user subsequently clarified that the concurrency overrides were
  intentional operator actions. The batch was resubmitted normally. This is a
  scheduling interruption, not a passing or failed source check.
- The first full graph update was interrupted before completion. A subsequent
  no-cluster update completed before resource coordination with existing Astro
  syntax warnings. The final no-cluster refresh also completed after validation.
- No production latency, full-product runtime, browser, remote-CI, external
  review, merge, release or deployment proof is claimed by these local results.
  This is backend-only work; the parent owns combined review and any later merge
  decision. No Claude subscription or paid substitute reviewer was used.
- Dedicated race-detector runs and extended fuzz campaigns were not run locally.
  Ordinary fuzz seed execution is part of the full Go suite. External-provider
  contracts and explicitly configured opt-in drills retain their existing skip
  conditions; they are not covered by the local PostgreSQL audit evidence.
  The broad run recorded 24 test/subtest skips: external provider and native
  interoperability contracts, dynamic-provider PostgreSQL, cosign/supply-chain,
  generated-client and container/deployment/artifact lanes, the explicit pin
  updater and a parent-only subprocess helper. The pin updater was run explicitly
  and passed in its separate update invocation.
  The nine top-level isolation skips are AWS emulator/real smoke, frozen client
  skew, dynamic-provider validity, Forgejo/GitLab lifecycles, two native Codex
  contracts and the opt-in MCP deployment fixture. Vault KV end-to-end subtests
  also retained their external-environment skip.
- Main advanced after this branch's baseline through unrelated catalogue,
  admission-eviction and disclosure-clock changes. A live comparison showed no
  overlapping changed files. Local evidence applies to this branch's source;
  integration with the current base is a separate remote-CI result.

## Reproduce

Run from the repository root with `HIKYO_TEST_POSTGRES_DSN` pointing to a
disposable PostgreSQL 18 database. The local queue supplies EOF; the finite shell
sets the test mask explicitly so loose-mode refusal fixtures exercise their
intended modes. The operator controls concurrency overrides.

```sh
run_go() {
  agent-heavy -- sh -c 'umask 022; exec env GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go "$@"' sh "$@"
}
run_go test -p 2 -count=1 ./internal/store/auditrow ./internal/store/tx
run_go test -p 2 -count=1 ./internal/store ./internal/service -run 'Audit|Glob|FillPage|FilterPage|NormalizedOutcomeAndActorName'
run_go test -p 2 -count=1 -timeout=18m ./internal/isolation -run 'Audit|^TestIsolation$|^TestInvariant12CacheDiscipline$'
run_go test -p 2 -count=1 ./internal/lint ./internal/boundary
run_go vet -p 2 ./internal/store ./internal/store/auditrow ./internal/store/tx ./internal/service ./internal/isolation
run_go tool sqlc generate
git diff --exit-code -- internal/store/sqlitegen internal/store/pggen
run_go test -p 2 ./internal/store -run '^$' -bench '^BenchmarkAuditFilter16000$' -benchtime=5x -count=3 -benchmem
run_go test -p 2 ./internal/service -run '^$' -bench '^BenchmarkAuditBulkDisclosure300$' -benchtime=3x -count=3 -benchmem
run_go test -p 2 -parallel 2 -count=1 -timeout=30m ./...
agent-heavy -- env GRAPHIFY_MAX_WORKERS=2 graphify update . --no-cluster
```

For a host where the unsharded isolation package exceeds its budget, enumerate
compiled names with `run_go test -p 2 ./internal/isolation -list '^Test'`. Run
anchored groups of at most 20 names sequentially with `-parallel 2 -count=1
-timeout=18m`, retaining JSON results. Compare the union of top-level pass/skip
names with that complete inventory. The local completion controller performed
that comparison and refused missing tests; its manifest and raw outputs remain
ephemeral session evidence outside Git.

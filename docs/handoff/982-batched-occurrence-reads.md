# Issue #982: batched occurrence membership

## Contract and implementation

The issue was re-read live on 2026-10-18: open, no comments and no matching open
PR. Implementation started at `cb78b6be111029335e744aa3e4dcc956d5194ba1`.
The authorized endpoint is a tested, signed and pushed PR. Merge, queue,
auto-merge, release and deployment remain excluded.

Both snapshot repositories verify the operation and transaction proof, then
require environment depth before inspecting candidates. Org, project and
environment binds come exclusively from that verified chain. Candidates are
read in chunks of 500. SQLite uses sqlc's canonical slice expansion, with at
most 503 variables, below the historical 999-variable limit. PostgreSQL uses a
bounded `text[]` with `ANY`. Repeated candidates preserve set membership;
empty input returns a non-nil empty map without SQL after proof verification.
Every batch error returns a nil map and the original error, discarding partial
membership. Existing transaction isolation, service authorization, disclosure
and pin gates are unchanged. No migration or index changes are included.

Current main already supports this predicate and generated-bind grammar through
`sqlpredicate_narrow.go` and `sqlcontract_slice.go`. No analyzer implementation,
security exemption, reviewed pin or allowlist was broadened. Regression coverage
requires complete chain equality even beside a batch predicate, rejects unsafe
list/array substitutions, disjunctions and subqueries, and exercises the
canonical generated prelude with both two and three scalar scope binds.

## Evidence

- `TestSecretOccurrenceBatches` passed on SQLite and PostgreSQL 18.6, then
  passed again in the broader isolation run. It checks counts 0, 1, 4, 499,
  500, 501, 1,000, 1,001 and 10,000; mixed org/project/env membership; repeated,
  missing, empty and SQL-shaped IDs; invalid, mismatched, foreign-transaction and
  expired proofs; cancellation; and a later-batch error. SQLite's connection
  variable limit is explicitly lowered to 999. Instrumented membership counts
  are zero for empty input and `ceil(N/500)` otherwise, including 20 queries
  for 10,000 candidates on each engine.
- Store, transaction and service package tests passed: 198.602, 2.900 and
  188.861 seconds respectively.
- Revision/pin conformance passed 24 scenarios per engine, 48 total, including
  historical sticky-secret export, restore and pin lifecycle. Selected isolation
  passed 32 top-level tests in 108.705 seconds, including `TestIsolation`, all
  invariants, history, pins, retention and snapshot rollback/proof binding.
- Three-sample local comparison benchmarks passed on both engines. Results and
  workload limits are below.
- Focused repository SQL predicate and generated-bind tests passed. Pinned sqlc
  regeneration reproduced all 114 generated Go files byte for byte.

- Complete analyzer selection passed in four disjoint groups: 221.104, 350.990,
  98.757 and 141.049 seconds. Runtime discovery matched all 86 package test
  names in source: 85 passed, and `TestUpdateReviewedPins` retained its expected
  opt-in skip, "explicit reviewed-pin update only". All ordinary subtests ran,
  including default/UI/Darwin/Windows context fixtures where defined. No reviewed
  pins were rewritten.
- Boundary tests passed in 7.398 seconds; scoped `go vet` and documentation source
  checks passed. The AST-only graph refresh completed with two workers and no
  clustering: 57,085 nodes and 178,458 edges. Its 17 Astro syntax warnings match
  the initial extraction; those files may be only partially represented. This
  graph is navigation evidence, not security proof. Generated graph files are
  not committed.

The initial full analyzer package hit Go's existing ten-minute timeout while
`TestArtifactDisposableDatabaseProtocolsRejectDrift` was active for 20 seconds.
That failed run is retained separately from the completed grouped selection.
No timeout, assertion or analyzer security rule was weakened. The initial
dependency build and a later store/service attempt were interrupted under host
pressure and at the coordinator's serialization request respectively; both
remain incomplete attempts, superseded by the completed results above. Initial
graph materialization was also interrupted, then completed by the structural
refresh above. Expensive retries used the global heavy-command queue.

## Reproduction

Provide an isolated PostgreSQL test DSN with database-creation permission through
`HIKYO_TEST_POSTGRES_DSN`, as used by the existing engine harnesses. Use the
repository-pinned Go toolchain and sqlc tool. Commands below record temporary
resource settings used for this run, without changing repository or CI defaults.
Run from the repository root through the installed shared heavy-command queue.
The outer wrapper can alternatively run these commands in one finite script,
without nested wrappers:

```sh
export GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50
agent-heavy -- go tool sqlc generate
agent-heavy -- go test -p 2 ./internal/isolation -run '^TestSecretOccurrenceBatches$' -count=1 -v
agent-heavy -- go test -p 2 ./internal/store ./internal/store/tx ./internal/service -count=1
agent-heavy -- go test -p 2 ./internal/store -run '^$' -bench '^BenchmarkSecretOccurrenceMembership$' -benchtime=100ms -count=3
agent-heavy -- go test -p 2 ./internal/conformance -run '^TestConformance(SQLite|Postgres)$/.*(revision|publish|restore|historical|sticky|pin|secret_classification)' -count=1 -v
agent-heavy -- go test -p 2 ./internal/isolation -run '^Test(Isolation|Invariant.*|SecretOccurrence.*|History.*|ProjectRevisions.*|SnapshotInserts.*|WorkloadRevealHistoryPin|DeliveryPinned.*|PinRelease.*|RetentionPin.*)$' -count=1 -v
agent-heavy -- go test -p 2 ./internal/boundary -count=1
agent-heavy -- go vet -p 2 ./internal/store/... ./internal/isolation ./internal/lint ./internal/boundary
agent-heavy -- node docs/site/scripts/prepare-content.mjs --check --source-root .
GRAPHIFY_MAX_WORKERS=2 agent-heavy -- graphify update . --no-cluster
```

The local analyzer retry ran the complete discovered test set in groups of 22,
with the existing timeout unchanged. No test filter was used inside each named
test, so all of its subtests ran. A portable equivalent is:

```sh
agent-heavy -- python3 -c '
import re, subprocess
base = ["go", "test", "-p", "2", "./internal/lint"]
listing = subprocess.check_output(base + ["-list", "^Test"], text=True)
names = [line for line in listing.splitlines() if re.fullmatch(r"Test\w+", line)]
for offset in range(0, len(names), 22):
    pattern = "^(" + "|".join(names[offset:offset+22]) + ")$"
    subprocess.run(base + ["-run", pattern, "-count=1", "-v"], check=True)
'
```

## Local comparison benchmark

Median of three 100 ms benchmark samples per case on darwin/arm64, Go 1.27.2,
modernc.org/sqlite v1.60.1 and loopback PostgreSQL 18.6. Values include SQL execution
and membership-map construction inside one admitted read transaction. These
are a local synthetic all-hit workload with 10,000 stored short IDs, not a
production performance forecast. No statistical significance claim is made.

| Engine | Candidates | Point ms/op | Batch ms/op | Point/batch ratio | Queries point/batch |
| --- | ---: | ---: | ---: | ---: | ---: |
| sqlite | 1 | 0.0093 | 0.0112 | 0.83x | 1/1 |
| sqlite | 100 | 0.9262 | 0.1785 | 5.19x | 100/1 |
| sqlite | 500 | 4.6951 | 0.8393 | 5.59x | 500/1 |
| sqlite | 1,000 | 9.0544 | 1.5317 | 5.91x | 1,000/2 |
| sqlite | 10,000 | 93.3360 | 13.8960 | 6.72x | 10,000/20 |
| postgres | 1 | 0.0317 | 0.0458 | 0.69x | 1/1 |
| postgres | 100 | 3.5615 | 0.2744 | 12.98x | 100/1 |
| postgres | 500 | 23.3458 | 1.6931 | 13.79x | 500/1 |
| postgres | 1,000 | 73.9305 | 2.9833 | 24.78x | 1,000/2 |
| postgres | 10,000 | 338.2484 | 31.7913 | 10.64x | 10,000/20 |

One-candidate reads were slower: SQLite 0.0093 to 0.0112 ms/op and PostgreSQL
0.0317 to 0.0458 ms/op. Increasing candidate counts reduced executions and
local elapsed query cost. At 10,000 candidates, median allocation bytes/op fell
from 11,477,360 to 2,388,044 on SQLite and 7,477,248 to 2,169,840 on PostgreSQL.

The benchmark compares the old point SQL against generated batch SQL plus map
construction. Both algorithms hold one read transaction and connection, matching
the helper and excluding repeated pool checkout from the point baseline.
The hosted benchmark smoke and optional measurement package lists do not include
`internal/store`; these comparisons are local acceptance evidence. No paid
measurement workflow was requested. Proof verification and service work are
excluded. The samples do not establish an optimal batch size.

## Delivery and limits

Local acceptance is complete with the failed monolithic analyzer attempt and
expected maintenance-test skip recorded above. A manual final diff review checked
the issue requirements, proof-derived binds, refusal ordering, batch bounds,
error propagation and generated-code freshness; it is not independent review.

Remote exact-head feedback, the complete required merge-group graph, integrated
validation with other issue groups, independent review and production/runtime
performance are not established by these local checks. The new store benchmark
is not in the current hosted benchmark package lists. No frontend code changed,
so application browser flows and web checks were not run. Merge, queue,
auto-merge, release and deployment remain outside this task.

## PR #995 merge follow-through, 2026-10-19

The subsequent babysitting request authorizes CI repairs, review responses and
merge of PRs #995 and #996, later extended to #1002. Release and deployment are
not part of that request.
The original implementation commit was rebased with signing and DCO onto main
`b14f4c416571e66a0b891d6301c3d6588eafb186` before the CI repair.

The [latest failed merge-group run](https://github.com/Hikyo-Org/Hikyo/actions/runs/39204141729)
failed desktop flow group 3 in `workspace.spec.ts`: the popup-ceremony test read
the redemption response body after Chromium could no longer retrieve it
(`Network.getResponseBody: No data found for resource with given identifier`).
Its successor expected one revoked workspace session but the retained browser
error context shows two, consistent with the first test exiting before its
kill-switch checks. The unchanged pair passed locally in 3.7 minutes; the
intermittent CI failure was not reproduced locally.

The test now reads the exact bearer from the already-observed first liveness
request's Authorization header. This avoids the delayed response-body read and
retains the actual-value storage checks, both origins' cookie checks, ambient
cookie refusal, popup isolation and both kill switches. It rejects missing or
wrong-kind bearer headers without printing their contents. Product code,
timeouts, retries and assertions about exact revocation counts are unchanged.

Independent Standards and Spec source reviews found no actionable issues at
original head `690e23731718cd945505e4bf00f6f841106a1776`. A separate source review
of the CI repair found no actionable issues and checked transport provenance,
bearer grammar and retention of the security assertions. These are source
reviews, not fresh runtime evidence. Qodo was billing-blocked and CodeRabbit
rate-limited; neither is counted as a completed review. The initial Jev advisory
readiness score was 0.18 on a 0-3 scale, with 85% blocked probability and 0.83
confidence, while the recorded CI failures remained open.

An older merge-group failure in `TestRangeMergeOnlyScansNewCombinedContent`
reported temporary Git directory cleanup failure. The latest merge group passed
`validation / test_core`; no unsupported fix to that earlier failure was made.

Fresh local validation passed: `node --run typecheck`, `node --run lint`, all
212 unit-test files / 1,965 tests with at most two workers, and the complete
`workspace.spec.ts` suite on desktop and mobile, 17 tests per viewport. Both
browser runs used fresh real instance pairs, one worker and zero retries;
the previously failing pair and both kill switches passed on each viewport.
These filtered file runs do not establish full browser-suite closure.
Protected merge-group proof remains pending after publication. Queue admission
is not merge evidence.

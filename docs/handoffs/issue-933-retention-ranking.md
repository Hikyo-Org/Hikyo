# Issue 933: bounded retention ranking

## Behavior and ownership

Each retention batch derives an environment's Nth newest immutable lineage
revision through at most N indexed predecessor lookups. It collects revisions
below that cutoff only when they are also older than the effective age policy.
Collected headers count. Sparse revision numbers do not change the result.
Histories shorter than N have no usable cutoff. A very large allowed N stops
when history is exhausted rather than looping N times.

The owning SQL is `internal/store/queries/{sqlite,postgres}/retention.sql`.
Output is generated with `go tool sqlc generate`; parameters and result types
are unchanged. Reviewed instance-query content pins change with the SQL.
No migration, index, store API or service orchestration changes are needed.
Existing mark-time pin/root rechecks and the mark/delete/audit transaction stay
intact. Cutoffs are recomputed in each batch's transaction snapshot, never cached
across policy changes or retries.

This bounds ranking, not total sweep work. Outer candidate scans, repeated
batches, transaction commits and audit writes still contribute.

## Measurement method and limits

Baseline source: `c4c626a105d0139fcc8a6f5444e585e7a4e0a761`. Comparison is this
issue's working-tree implementation, identified by its reviewed SQL content pins.
Measurements use the real migrated store and `service.Retention.Sweep` on macOS
arm64 with Go 1.27.2, modernc SQLite v1.60.1 and native PostgreSQL 18.6.
`GOMAXPROCS=2` and `-p 1` bound Go concurrency.

The synthetic corpus has sparse revisions, old headers, one opaque payload entry
per live snapshot, the default 90-day/last-10 policy and 100-row batches. Admission,
migrations and seeding are untimed. First batch times a separate owning-store
eligibility transaction. Full sweep includes audit pruning, collection, deletion,
per-payload audit writes, commits and health recording. Encrypted reads,
publishing and the production scheduler loop are not measured.

These are single local samples. A sibling Node workload was concurrent with the
bounded PostgreSQL baseline and optimized runs. Cache state and planner statistics
were not controlled for a repeated statistical study. These are not production,
release-floor or hardware-SLO results.

## Matched complete observations

| Engine | Workload | Lineage | First batch before | First batch after | Full sweep before | Full sweep after |
| --- | --- | ---: | --- | --- | --- | --- |
| sqlite | catchup | 5,000 | 42.726667ms | 1.203417ms | 1.66917575s | 700.297167ms |
| sqlite | catchup | 10,000 | 86.830709ms | 1.22525ms | 5.78760825s | 1.490586709s |
| sqlite | catchup | 20,000 | 372.077542ms | 1.141042ms | 21.511991291s | 3.070160625s |
| postgres | catchup | 1,000 | 67.316625ms | 6.164208ms | 606.253042ms | 307.95625ms |
| postgres | catchup | 2,000 | 244.186667ms | 6.80725ms | 2.666414875s | 699.854458ms |
| postgres | catchup | 4,000 | 928.390041ms | 10.510875ms | 14.260832292s | 1.295326292s |
| postgres | catchup | 5,000 | 1.343460833s | 10.715917ms | 25.280090542s | 2.071112208s |
| sqlite | collected_history | 5,000 | 43.723916ms | 1.070625ms | 56.025458ms | 14.480291ms |
| sqlite | collected_history | 10,000 | 88.779ms | 1.148583ms | 99.680042ms | 18.092458ms |
| sqlite | collected_history | 20,000 | 231.232917ms | 1.064417ms | 239.257291ms | 17.928334ms |
| postgres | collected_history | 1,000 | 13.723833ms | 4.273667ms | 60.1675ms | 50.953083ms |
| postgres | collected_history | 2,000 | 23.090958ms | 4.288875ms | 68.498625ms | 50.213292ms |
| postgres | collected_history | 4,000 | 45.783042ms | 4.577167ms | 95.68775ms | 48.872917ms |
| postgres | collected_history | 5,000 | 61.374541ms | 4.966417ms | 105.5745ms | 55.503709ms |
| sqlite | protected_roots | 5,000 | 42.769375ms | 1.067583ms | 1.709563875s | 652.030834ms |
| sqlite | protected_roots | 10,000 | 84.265958ms | 1.088667ms | 5.652332709s | 1.336472667s |
| sqlite | protected_roots | 20,000 | 217.555208ms | 1.120083ms | 21.830134583s | 3.103116166s |
| postgres | quiet_projects | 1,000 | 4.870209ms | 5.9435ms | 11.247042ms | 11.685584ms |
| postgres | quiet_projects | 2,000 | 7.879667ms | 8.292583ms | 15.978125ms | 12.920833ms |
| postgres | quiet_projects | 4,000 | 13.932917ms | 12.821584ms | 19.668584ms | 17.131584ms |

`collected_history` has 100 eligible old live payloads and five live members of
last-10; the newest five headers are already collected. `protected_roots` retains
three old self-configuration roots, an old live pin, a young old-numbered snapshot
and last-10. Tests assert exact retained IDs, permanent lineage, deleted entries,
audit counts, repeat sweeps and policy widening without resurrection.

## Counterevidence and incomplete cases

- Original PostgreSQL 10k catch-up completed: first batch 5.304165416s and full
  sweep 129.676462625s. It was not interrupted.
- Original PostgreSQL 20k catch-up failed with `context deadline exceeded` in
  the first eligibility transaction, before `service.Sweep`. The server log
  records cancellation of `ListEligibleSnapshotPayloads`. The existing 15-second
  transaction deadline was unchanged. Optimized execution of the same case
  passed: first batch 30.900791ms, full sweep 9.636248792s, 19,990 collected and
  10 retained. This is a local synthetic transaction failure, not an established
  production SLO failure.
- Original PostgreSQL collected-history 5k and 10k completed in 105.5745ms and
  158.067333ms. The deliberate interrupt landed during collected-history 20k.
  Optimized 20k passed in 52.410458ms; no complete old 20k timing exists.
  The interrupted run as a whole is not PASS.
- Original `quiet_environments` puts lineage/5 environments in one project.
  Large cases exceed the supported 50-environment cap. It is retained as
  explicitly labelled `quiet_environment_stress`. SQLite 20k full-sweep time
  increased from 62.335375ms to 93.917417ms. This out-of-contract stress is
  counterevidence against claiming every workload improves.
- New `quiet_projects` distributes those quiet histories over projects with at
  most 50 environments each. Supported PostgreSQL 1k/2k/4k sweeps remained in the
  observed 11-20ms range. Optimized SQLite 5k/10k/20k sweeps were 25.900083ms,
  46.038334ms and 94.144667ms. There is no same-size old multi-project SQLite
  measurement. The fixture asserts the per-project environment cap.
- With 20 populated environments, SQLite 5k full-sweep time was 710.486333ms
  before and 733.091166ms after despite faster eligibility. Full-sweep linearity
  and improvements in every workload are not claimed.
- The first small run exposed a duplicate organization name in the new reference
  fixture. It was corrected, and the reference rerun passed both engines.
  Original failure and interrupted logs are preserved outside Git.

## Actual query plans

On a 20k-lineage, mostly-collected fixture, PostgreSQL 18.6 `EXPLAIN (ANALYZE,
BUFFERS)` produced 10 recursive rows for last-10: the newest lookup once and the
predecessor lookup nine times, each using `LIMIT 1` and the existing scope/revision
index. The pinned modernc SQLite plan selected covering index searches for newest
and predecessor lookups. Outer candidate scans, filtering and sorting remain.
This evidence substantiates bounded ranking only.

The temporary local plan harness reads generated SQL verbatim and is not shipped.
Original plan logs remain local evidence; portable reproduction uses the owning
generated query and scaling fixture.

## Validation and review status

Completed local checks:

- Original PostgreSQL 1k/2k/4k: all 18 workload cases passed.
- Proper sqlc generation, with unchanged Go query API. A final regeneration
  reproduced every generated Go file byte for byte.
- Optimized independent Go reference and all 36 small workload cases passed on
  SQLite and PostgreSQL.
- Optimized PostgreSQL 1k/2k/4k/5k: all 24 cases passed.
- Optimized SQLite 5k/10k/20k: all 18 cases passed.
- Optimized PostgreSQL 20k catch-up and collected-history passed.
- Actual plans inspected on both engines; `gofmt` and `git diff --check` passed.
- Full `go test -p 1 ./internal/store/... -count=1` passed with PostgreSQL
  configured, including the final header no-resurrection assertion.
- Existing both-engine retention conformance passed: C6, subsecond pin expiry,
  pin release and concurrent GC, failure auditing/candidate counts, parameter
  contract reclamation and query-content pinning. Both real CLI startup sweep
  tests passed, exercising the booted application and scheduler catch-up path.

The deliberately simple quadratic Go reference checks exact candidates and batch
ordering across empty/short histories, N=1, sparse revisions, collected newest
headers, age boundaries, expired pins, self-configuration roots, inherited and
overridden policies, unlimited organizations, policy changes and maximum-int64 N.

Standards and spec source reviews found no supported implementation defect.
Their evidence correction and environment-cap finding were independently verified
and addressed. A second standards/spec review checked the generated queries,
fixture authority exception, measurements and their limitations and found no
new supported defect. Source-only review is not CLEAN or execution acceptance.
The full local lint process failed at its unchanged 10-minute timeout while
`TestArtifactDisposableDatabaseProtocolsRejectDrift/tenant_update` was parsing
and type-checking the existing harness through `packages.Load`. No assertion
failure preceded the timeout. All 37 completed top-level tests passed, including
the four-context driver-handle checks. The original failed run remains FAIL.
Two serialized continuation jobs used the unchanged source and ordinary timeout:
the artifact protocol test passed all 13 subcases in 15.162s, and the remaining
46-name group completed in 69.785s. Exact union accounting covers all 84 top-level
names: 83 passed and the explicit maintenance-only `TestUpdateReviewedPins`
remained normally skipped. All 235 unique observed subcases passed; none remained
unfinished. No updater flag or timeout override was used. This is complete
partitioned lint coverage, not a passing monolithic run. Signed delivery and
remote merge gates remain separate from these local checks.

## Merge-group boundary repair

Merge-group runs `38976827157` and `39036727545` both failed
`TestForbiddenEdges` in the default, UI, Darwin and Windows contexts: the new
store test imported `internal/service`, contrary to the service-to-store
dependency direction. The failing jobs were `validation / test_core` and
`validation / race shard (2)`; `ci-required`, `merge-group-proof` and
`ci-queue-required` failed as downstream gates. Every isolation shard was green
in both runs, so the fix only had to keep the boundary rule satisfied.

The scaling test and its independent reference companion move to
`internal/isolation`, which already owns both-engine retention service probes.
They use the existing empty admitted database fixtures and `execRaw`/`queryInt`
helpers. The new raw-driver exception is removed, with no architecture exception
added. All corpus construction, eligibility comparisons, sweep measurements and
retention/deletion/audit/no-resurrection assertions are retained. Fixture setup
remains outside the timed operations. SQLite outcome reads now use the existing
read-pool helper after committed operations. PostgreSQL uses the isolation
harness's reset sibling database rather than the former per-case store database.
Earlier measurements remain observations of the original fixture at the pushed
head; they are not new measurements of the relocated harness.

The repair was verified locally on the moved head:

- `go test ./internal/boundary -run TestForbiddenEdges` passes, and
  `gofmt -l` / `go vet` are clean over the touched packages.
- `go test ./internal/store` and `go test ./internal/lint` pass.
- `go test -p 1 ./internal/isolation -run '^TestRetentionRanking(Reference|Scaling)$' -count=1`
  passes on SQLite and on a disposable PostgreSQL 18 container, and the same
  selection passes under `-race`.
- The shard planner discovers both tests as isolation tests and no longer lists
  them under `internal/store`.

A full local run of the whole isolation package is not a meaningful signal on
this machine: its shared PostgreSQL container is resource limited, and 128
unrelated Postgres cases failed with deadlocks and missing relations in the same
run. CI covers that package through the sharded isolation job, which was green on
the pushed head. Remote checks and the full merge-group proof must run again on
the repaired head before merge.

## Reproduction

Run from the repository root with the pinned toolchain. Set
`HIKYO_TEST_POSTGRES_DSN` to a disposable PostgreSQL admin database whose role can
create/drop isolated test databases. PostgreSQL must use UTC for the signed
schema-admission fixture. Tests create and drop their own databases.

```sh
GOMAXPROCS=2 go test -p 1 ./internal/isolation -run '^TestRetentionRanking(Reference|Scaling)$' -count=1 -v
HIKYO_RETENTION_SCALING_SIZES=1000,2000,4000 GOMAXPROCS=2 go test -p 1 ./internal/isolation -run '^TestRetentionRankingScaling$/postgres' -count=1 -v
HIKYO_RETENTION_SCALING=1 GOMAXPROCS=2 go test -p 1 ./internal/isolation -run '^TestRetentionRankingScaling$/sqlite' -count=1 -v
```

Without the PostgreSQL DSN, local PostgreSQL cases explicitly skip; that is not
both-engine proof. CI requires the DSN and fails rather than silently skipping.
Scaling logs include workload, lineage, collected/retained counts, first-batch and
full-sweep timings. Wall-clock thresholds are not asserted on shared machines;
retention semantics and outcomes are asserted.

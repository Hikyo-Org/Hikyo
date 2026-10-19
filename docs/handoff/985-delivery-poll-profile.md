# Issue #985: Authorized delivery polling profile

## Scope and conclusion

Investigation of unchanged delivery against baseline
`cb78b6be111029335e744aa3e4dcc956d5194ba1`. Measurements use synthetic values,
real encrypted snapshots, minted workload credentials and production service
transactions. This is a local service experiment, not production latency,
HTTP throughput or a native operations-floor acceptance claim.

Retain the current delivery algorithm. Repeated snapshot opens are real, but
this experiment does not justify the additional cache/invalidation surface.
At the render limit, sampled cumulative `OpenField` CPU was about 0.720 ms per
SQLite poll and 0.870 ms per PostgreSQL poll; typical values were 0.010 and
0.060 ms. Full unchanged polls still allocate about 13.7/12.5 MB at that limit.
These local costs warrant keeping the benchmark for future representative
profiles, not assuming that plaintext caching or a snapshot-only cursor is safe.
No production bottleneck, statistical speedup or latency SLO is established.

## Reproducible experiment

[`BenchmarkDeliveryPoll`](../../internal/service/delivery_benchmark_test.go)
compares these datasets on disk-backed SQLite and disposable PostgreSQL:

- `typical`: 100 entries with 64-byte values, 6,400 rendered bytes.
- `key-limit`: `schema.MaxKeysPerProject` (currently 1,000), 64-byte values.
- `render-limit`: 1,000 entries with a total of exactly
  `MaxRenderBytesPerTarget` (currently 1,048,576) rendered value bytes.

Each dataset is 80% config and 20% secret. `full` has read and reveal;
`config-only` omits secrets; `presence-only` is a read-only caller whose secret
entries have no plaintext; `file-target` selects the first 10% of entries,
including both classifications. Presence-only is a secret projection, not a
third wire mode: that caller still receives config values.
Profiled snapshots have no parameter templates. Parameter/schema movement is
validated by isolation tests; timings do not cover maximum template complexity
or every legal distribution of individual value sizes.

Fixture creation, import/publication, minting, the warm first delivery, and work
accounting are untimed. A selected config value changes through a real publish.
`changed` repeatedly presents the valid pre-publication cursor against the new
snapshot; `unchanged` presents the post-publication cursor. This measures stale
versus current polling of fixed content, not publication or continuous mutation.
Every timed call uses `Delivery.Fetch` with a bearer. The production sealer
preflight, fresh in-transaction authentication/authorization, expiry, reveal
policy, signer read, receipts, audit inserts and durable commit remain timed.
Rate budgets are nil so admission throttling does not determine the samples.
These are warm keyring/store measurements, not cold-start costs.
Provisioning starts after a synthetic human factor verification: a real stored
session, mint-bound ceremony and single-decision window exercise the normal
grant and credential mint gates. Login and passkey verification are not timed.

The test-only opener counts successful calls to the real project sealer in a
separate authorized `deliveryRows` invocation. Included-entry counts describe
rows used for the manifest after selection/projection; an unchanged response
itself carries zero keys and emits zero value disclosures. The AEAD counter
covers snapshot values, excluding key unwrapping and other crypto. Both cursor dispositions call
that row builder before comparing cursors. The production change is solely an
unexported opener interface; no row-builder logic, cursor inputs, cache or
refusal ordering changes. Audit deltas are independently read through the
proof-gated repository, outside the timer. Untimed checks pin exact values, key
selection and publication-induced cursor movement. Timed calls check errors,
disposition, value/key cardinality, receipt presence and the post-publication
cursor. Each sample also verifies its durable audit delta.
The work-count test also presents a current cursor with an undeclared parameter
and after credential expiry, requiring fresh refusal in both cases.
In this warm single-node fixture, the live token signer additionally performs
one tier-3 AEAD unwrap per fetch, visible in
[`DeliveryTokenSnapshot`](../../internal/crypto/offline_receipt.go) and
`Keyring.unwrapTier3`. That source-counted constant is excluded from the
`snapshot-opens/op` counter and remains included in timed/profiled work.

For a short repeatable run from the repository root (on a coordinated shared
host, use its installed `agent-heavy` queue wrapper as below):

```sh
agent-heavy -- env GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 -count=1 -run '^TestDeliveryPollWork$' -v ./internal/service
agent-heavy -- env GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 -run '^$' -bench '^BenchmarkDeliveryPoll$' \
  -benchtime=5x -count=3 ./internal/service
```

Set `HIKYO_TEST_POSTGRES_DSN` to a disposable test server with database creation
rights for the PostgreSQL legs. The existing signed-admission fixture creates
unique owned databases and drops them on cleanup. Without a DSN the optional PostgreSQL benchmark explicitly skips, matching
the current SQLite-only CI smoke/CodSpeed provisioning. The work-count test
retains the normal CI-fatal PostgreSQL requirement and skips locally without
a DSN. Both engines are required and configured for this investigation. Use the same low parallelism for
other checks; do not run multiple broad Go suites together.

CPU profiles carry a `delivery-poll` label only around the timed loop, excluding
expensive fixture construction and work accounting. Choose an external artifact
directory and profile a selected case:

```sh
profile_dir=$(mktemp -d)
agent-heavy -- env GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 -run '^$' \
  -bench '^BenchmarkDeliveryPoll$/sqlite/render-limit/full/unchanged$' \
  -benchtime=1000x -cpuprofile="$profile_dir/cpu.pprof" \
  -o "$profile_dir/service.test" ./internal/service
agent-heavy -- go tool pprof -top -relative_percentages -tagfocus='delivery-poll=.' \
  "$profile_dir/service.test" "$profile_dir/cpu.pprof"
```

The warm service path includes a read transaction for authorized sealer
preflight and a write transaction for delivery and immutable audit commit.
The experiment checks durable row cardinality rather than adding SQL tracing
or changing transaction boundaries. It does not isolate SQL query counts or
server transaction CPU.

The native harness reports timed B/op and allocations/op. Process-wide heap
profiles include setup unless separately isolated; they must not be interpreted
as polling-only allocation evidence. PostgreSQL CPU profiles measure the Go
client, not the database server. Label filtering includes
work carrying the polling label, including runtime/inherited stacks, and
excludes unlabeled background work. Transaction wall time is included in each
service sample; cumulative CPU stacks do not measure database wait duration.

## Measurements

Local host: Apple M2 Pro, 16 GiB RAM, macOS 27.0.1, arm64, Go 1.27.2.
Runs are sequential with `GOMAXPROCS=2`, `GOMEMLIMIT=512MiB`, `GOGC=50`
and `-p 2`. These are process controls, not a host memory or latency guarantee.
PostgreSQL uses the [CI-pinned PostgreSQL 18 image](../../scripts/ci/start-ci-postgres.sh)
in a disposable local container configured for two CPUs and 512 MiB memory.

The before/after control overlays the original `delivery.go` from the baseline
commit onto this benchmark fixture. Its timed fetch/check loop is identical.
Only untimed work accounting is adapted to the original concrete sealer
signature: baseline open counts are explicitly reported as expected counts;
candidate counts wrap actual successful opens. No optimization is being
compared, and three short samples do not establish statistical equivalence.
Host background activity and container I/O are not fully controlled.

Both matrices completed all 48 cases with three samples of five requests per
case, 144 samples per revision. The tables show medians. U means unchanged;
C means changed. The original PostgreSQL typical sample was much slower than
the later candidate sample despite unchanged delivery logic. This variation
precludes attributing a speedup to the counting seam or claiming equivalence.

Actual candidate row-builder work, identical for the two cursor dispositions:

| Dataset | Full included / opens | Config-only included / opens | Presence-only included / opens | File-target included / opens |
| --- | ---: | ---: | ---: | ---: |
| typical | 100 / 100 | 80 / 80 | 100 / 80 | 10 / 10 |
| key-limit | 1,000 / 1,000 | 800 / 800 | 1,000 / 800 | 100 / 100 |
| render-limit | 1,000 / 1,000 | 800 / 800 | 1,000 / 800 | 100 / 100 |

Every sample verified one durable fetch audit row per unchanged poll. Changed
polls verified that row plus one disclosure per delivered value: respectively
101/81/81/11 rows for the typical modes and 1,001/801/801/101 at both limits.
These counts measure audit cardinality, not isolated SQL statement latency.

**SQLite candidate medians**

| Dataset | Projection | Unchanged ms/op | Changed ms/op | B/op unchanged / changed | Allocations/op unchanged / changed |
| --- | --- | ---: | ---: | ---: | ---: |
| typical | full | 1.742 | 9.003 | 591,468 / 1,450,990 | 6,127 / 20,440 |
| typical | config-only | 1.581 | 7.572 | 532,144 / 1,218,606 | 5,755 / 17,202 |
| typical | presence-only | 1.362 | 7.676 | 533,176 / 1,218,761 | 5,725 / 17,169 |
| typical | file-target | 1.585 | 2.601 | 380,780 / 465,060 | 5,259 / 6,685 |
| key-limit | full | 5.769 | 77.461 | 4,824,080 / 13,397,043 | 43,078 / 186,111 |
| key-limit | config-only | 5.965 | 70.236 | 4,376,228 / 11,237,822 | 39,272 / 153,713 |
| key-limit | presence-only | 5.685 | 63.446 | 4,382,542 / 11,242,217 | 39,236 / 153,670 |
| key-limit | file-target | 4.691 | 11.948 | 2,800,715 / 3,658,344 | 33,383 / 47,685 |
| render-limit | full | 10.081 | 92.823 | 13,712,867 / 22,282,206 | 43,100 / 186,116 |
| render-limit | config-only | 9.934 | 69.342 | 12,829,358 / 19,686,547 | 39,293 / 153,714 |
| render-limit | presence-only | 10.484 | 67.398 | 12,835,219 / 19,692,707 | 39,253 / 153,677 |
| render-limit | file-target | 6.446 | 14.388 | 5,454,579 / 6,312,564 | 33,393 / 47,702 |

**PostgreSQL candidate medians**

| Dataset | Projection | Unchanged ms/op | Changed ms/op | B/op unchanged / changed | Allocations/op unchanged / changed |
| --- | --- | ---: | ---: | ---: | ---: |
| typical | full | 8.246 | 76.822 | 547,006 / 1,386,307 | 4,119 / 15,122 |
| typical | config-only | 8.011 | 64.380 | 486,470 / 1,158,425 | 3,740 / 12,544 |
| typical | presence-only | 9.123 | 61.177 | 486,200 / 1,158,728 | 3,717 / 12,525 |
| typical | file-target | 10.176 | 16.765 | 336,992 / 421,008 | 2,997 / 4,098 |
| key-limit | full | 14.070 | 809.940 | 4,628,374 / 13,028,481 | 32,067 / 142,111 |
| key-limit | config-only | 13.714 | 714.343 | 4,179,625 / 10,899,532 | 28,258 / 116,295 |
| key-limit | presence-only | 16.488 | 779.440 | 4,186,878 / 10,905,489 | 28,239 / 116,270 |
| key-limit | file-target | 15.661 | 96.725 | 2,607,193 / 3,445,428 | 20,325 / 31,321 |
| render-limit | full | 26.100 | 975.888 | 12,521,977 / 20,919,142 | 32,074 / 142,106 |
| render-limit | config-only | 21.783 | 791.287 | 11,642,337 / 18,358,619 | 28,283 / 116,304 |
| render-limit | presence-only | 24.502 | 730.635 | 11,646,748 / 18,367,720 | 28,250 / 116,289 |
| render-limit | file-target | 19.274 | 87.900 | 4,266,598 / 5,108,150 | 20,326 / 31,336 |



**Original-source control versus counting interface, full projection**

| Engine / dataset | Control ms/op U / C | Candidate ms/op U / C | Control B/op U / C | Candidate B/op U / C | Control allocs/op U / C | Candidate allocs/op U / C |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| sqlite / typical | 1.476 / 8.948 | 1.742 / 9.003 | 593,534 / 1,451,267 | 591,468 / 1,450,990 | 6,137 / 20,441 | 6,127 / 20,440 |
| sqlite / key-limit | 5.699 / 82.334 | 5.769 / 77.461 | 4,823,787 / 13,397,083 | 4,824,080 / 13,397,043 | 43,079 / 186,112 | 43,078 / 186,111 |
| sqlite / render-limit | 11.488 / 90.265 | 10.081 / 92.823 | 13,711,072 / 22,282,104 | 13,712,867 / 22,282,206 | 43,092 / 186,115 | 43,100 / 186,116 |
| postgres / typical | 37.458 / 166.020 | 8.246 / 76.822 | 547,764 / 1,386,502 | 547,006 / 1,386,307 | 4,120 / 15,121 | 4,119 / 15,122 |
| postgres / key-limit | 22.406 / 911.753 | 14.070 / 809.940 | 4,627,220 / 13,024,763 | 4,628,374 / 13,028,481 | 32,063 / 142,097 | 32,067 / 142,111 |
| postgres / render-limit | 31.122 / 1374.882 | 26.100 / 975.888 | 12,522,478 / 20,920,787 | 12,521,977 / 20,919,142 | 32,077 / 142,112 | 32,074 / 142,106 |

**CPU profiles, full unchanged polling**

Each profile contains 1,000 polls; CPU is sampled and divided by that count.
Cumulative columns overlap: `OpenField` is inside the row builder and must not
be added to its parent. Tagged CPU includes runtime/inherited stacks carrying
the polling label. Unlabeled background work and database-server CPU are
excluded. Sampling resolution and small totals limit typical-case precision.

| Engine / dataset | Revision | Wall ms/poll | Tagged Go CPU ms/poll | OpenField cumulative ms/poll | Row builder cumulative ms/poll |
| --- | --- | ---: | ---: | ---: | ---: |
| sqlite / typical | baseline | 1.620 | 1.260 | 0.010 | 0.050 |
| sqlite / typical | candidate | 1.603 | 1.340 | 0.010 | 0.050 |
| sqlite / render-limit | baseline | 11.133 | 8.910 | 0.850 | 2.070 |
| sqlite / render-limit | candidate | 12.209 | 9.410 | 0.720 | 2.150 |
| postgres / typical | baseline | 11.296 | 1.770 | 0.130 | 0.310 |
| postgres / typical | candidate | 9.929 | 1.200 | 0.060 | 0.190 |
| postgres / render-limit | baseline | 28.159 | 7.970 | 1.060 | 3.380 |
| postgres / render-limit | candidate | 25.349 | 7.910 | 0.870 | 3.170 |

Completed measurement commands recorded no other running queue job at their
start/end observations; periodic status checks likewise observed none during
those measurements. Earlier interrupted matrices are excluded. These
observations do not establish atomic or whole-host isolation.


## Security and cursor invariants

The existing
[machine identity contract](../adr/machine-identities.md#authentication-authorization-and-the-fetch-path)
requires the same current authority checks on both dispositions. No
snapshot-only cursor shortcut or plaintext process cache is introduced.
Authentication, authorization, expiry, project reveal policy, parameter
validation and immutable audit semantics remain in their original positions.

Any future derived-data design must account for the selected snapshot and
captured schema/parameter contract, canonical supplied parameters and resolved
manifest, mode, selected immutable key IDs and file-target configuration
generation, current effective reveal/history
authority and its generation, principal authorization generation, pin generation
and historical transition, and the live database-locked token signer/version.
It must preserve secret-presence noninterference, target narrowing before opens,
collected-payload refusals, recorded pin-authority checks and disclosure/audit
behavior. A token/key cache cannot substitute for fresh policy evaluation.
The [revision parameter amendment](../adr/revision-model.md) and
[token rotation contract](../adr/encryption-model.md) remain applicable.

## Validation and limits

| Check | Completed evidence |
| --- | --- |
| `TestDeliveryPollWork` | Both engines pass, 4.820 s; all projections, exact opens, current/stale cursor, audit deltas, invalid parameter and expiry refusal. |
| Benchmark matrix | 48 cases per revision, three samples of five polls each; 144 samples per revision, no skipped matrix legs. |
| CPU profiles | Eight full unchanged profiles, 1,000 polls each, typical/render-limit and both engines/revisions. |
| Package suites | `internal/service` 176.427 s, `internal/delivery` 0.255 s, `internal/parameters` 0.218 s; full `internal/crypto` passes on retry, 0.638 s, with the fixture-mode correction below. |
| Delivery/parameter/pin/developer isolation | 44 top-level cases, both engines, zero skips, 131.279 s. Includes current authority/revocation, historical pin transitions, reveal opt-in, parameter/schema behavior and stale-replica token rotation. |
| Repository invariants/analyzers | `TestInvariant(07\|08\|09\|12\|13)`, default build context, pass in 54.193 s: proof signatures, SQL confinement, raw handles, forgery, detached results, cache discipline and allowlist pins. |
| Vet | `go vet -p 2 ./internal/service ./internal/delivery ./internal/parameters ./internal/crypto`, pass. |
| Imports and formatting | `scripts/ci/check-go-imports.sh`, 2,035 handwritten files, pass; scoped gofmt and staged whitespace check pass. |
| Generated freshness | sqlc, OpenAPI, scanning and authorization generation completed without generated changes. This PR changes no generator input specifications. |
| Graph navigation | Bounded AST refresh completed with two workers and no clustering. An earlier clustering attempt was interrupted; unrelated Astro extraction warnings are not runtime or security evidence. |

The completed isolation command was:

```sh
go test -p 2 -count=1 -timeout=20m -v ./internal/isolation \
  -run '^Test(Delivery|EnvironmentParameters|Parameter|WorkloadRevealHistoryPin|FileTarget|MachineRevealOptIn|OfflineReceiptReplicaRotation|Developer)'
```

It ran inside the finite admitted validation batch with the documented Go
settings and disposable PostgreSQL DSN. The same batch ran the analyzer
selection, vet and imports sequentially. Full-repository Go/race/fuzz suites,
the platform matrix and the release gate were not run locally. Remote checks
must be assessed on the pushed PR head; local evidence is separate.


The initial full crypto suite failed existing loose-file/directory refusal
fixtures under the queued process's measured `0077` umask: requested `0640`
and `0755` became `0600` and `0700`. The finite validation batch restored
its own process umask to `0022`, verified the exact requested modes, and
reran the unchanged crypto suite successfully. No test, production file-mode
check, shared queue setting or security boundary was relaxed. The original
failure and both mode probes are retained. Later correctness validation
records intentional operator-scheduled overlap; it is not timing evidence.

Initial fixture runs failed closed on a missing reauthentication seam, then on
a reused synthetic WebAuthn challenge verifier. The fixture now wires Auth,
uses real stored mint ceremony bindings and gives every synthetic challenge a
unique verifier. Those original failures are retained as investigation evidence;
they are not production findings. A subsequent compilation was interrupted for
the grouped task's resource-coordination gate, so that attempt is not a pass.
The first serialized run then refused the audit observer because admin does not
imply `audit-read`. The fixture grants that separate capability through the
normal grant service before creating the observer's session; no store boundary
or analyzer exception is relaxed.
The corrected work-count test then passed on both engines. Two global
benchmark attempts were interrupted after queue status reported two running jobs
with a limit of one and process inspection confirmed concurrent expensive work.
Only this investigation's jobs were cancelled, each exiting 130. Partial
measurements were retained and excluded from completed results. The trigger
was initially unattributed. The user subsequently confirmed that
operator start/parallel-limit overrides were intentional; the overlap is not
evidence of a backend fault.
CI source review also found that benchmark smoke/CodSpeed have no PostgreSQL
DSN. The optional benchmark leg now explicitly skips there; the both-engine
work-count test retains its CI-fatal requirement. No workflow or test
requirement changes. An idle guarded attempt was stopped before any benchmark
started to apply this correction and rebuild both binaries.
The resume uses the corrected frozen benchmark binaries and task-local
read-only queue observations. A preflight already in progress retains its
ten-minute bound. After the operator clarification, subsequent commands record
start/end workload observations and continue under normal queue admission.
No isolated-performance claim is made where contention is observed; queue
snapshots are not atomic exclusivity.

No production traffic, HTTP/browser workflow, concurrent-load SLO, hosted
CodSpeed measurement, independent human security review, merge, release or
deployment is asserted. Revisit optimization with representative production
profiles if unchanged polling materially consumes the service budget. Preserve
the above dependencies and rerun both-engine delivery, parameter, pin,
developer and rotation cases for any behavioral change.

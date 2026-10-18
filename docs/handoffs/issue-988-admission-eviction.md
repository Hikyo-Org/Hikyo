# Issue 988: admission eviction at the tracking bound

## Outcome and contract

Full local tracking maps now find their fallback victim during the stale sweep,
avoiding a second traversal under `Limiter.mu` when all 4,096 subjects are live.
The change is confined to `internal/admission/admission.go`.

Expired entries still leave first, including zero account deadlines and deadlines
exactly equal to the clock. If reclamation frees nothing, the earliest live
account deadline or oldest IP window is forgotten. Equal-deadline victims remain
unspecified because Go map iteration is unordered. Count and deadline leave
together. No eviction extends a deadline or increments a failure count.

The existing expired sweep already amortizes reclamation by freeing many slots.
This patch removes duplicate full-live work; it does not claim constant-time
insertion or introduce cross-request batch eviction. Worst-case work remains a
bounded scan of 4,096 entries (and each window's bounded hit list). A heap, expiry
cache or timer is not justified by these noisy local measurements. No additional
tracking memory, timer, goroutine or shutdown obligation is added.

Account identifiers remain SHA-256 keys. Failure thresholds, threshold-crossing
audit signals, success reset, account/IP/discovery/issuer allowances, refusal
ordering, queue and semaphore budgets, lifecycle locks and generation inheritance
are unchanged. The shared backend still delegates counters to the datastore and
uses its clock for account delay. Its fixed windows differ from local sliding
windows; these measurements do not compare datastore performance.

Relevant accepted policy: [human authentication](../adr/human-auth.md),
[operations specification](../adr/ops-spec.md) and
[threat model](../adr/threat-model.md). No policy amendment is proposed.

## Measurement

Baseline production source: `cb78b6be111029335e744aa3e4dcc956d5194ba1`.
Candidate production source: `bcd08edcfbae552c1849f87463be869a02655567`.
The comparison uses the identical new `BenchmarkEvictionPressure` harness;
subsequent handoff-only changes do not alter the measured algorithm.
Environment: Go 1.27.2, macOS arm64, Apple M2 Pro,
`GOMAXPROCS=2`, `GOMEMLIMIT=512MiB`, `GOGC=50`, three repetitions of 25
epochs per scenario, mutex profile fraction 1. The final comparison ran
sequentially while owning the parent's shared validation allocation, using the
previously compiled baseline and candidate test binaries with identical
harnesses. Each epoch seeds exactly 4,096 IP and account entries
outside the benchmark timer, then releases two concurrent workers:

- 128 legitimate flows revisit seeded IPs within their allowance, read account
  delay, record success and release their real admission slot.
- 128 synthetic failure flows use new IP/account subjects, acquire a real slot,
  read delay, record six failures to reach the first backoff, then release.
  These six calls isolate failure bookkeeping, not six HTTP/password requests.
- `full_live`: every seeded window and account delay is live.
- `partially_expired`: one quarter expires exactly at the cutoff; reclamation
  frees room for the remainder of the burst without live eviction.
- `churn`: seeded ages/deadlines span 0 through 60 seconds; half of the new
  failure subjects are subsequently reset by success.

The injected clock stays fixed during an epoch. Worker latency includes lock
waits and scheduling. Setup and final sample sorting are untimed. Each reported
latency below is the median of the three run-specific percentiles, not a pooled
percentile. A benchmark operation is a 256-flow epoch, not a request.

| Scenario | Failure p50 before/after | Failure p99 before/after | Legitimate p99 before/after | Epoch before/after |
| --- | --- | --- | --- | --- |
| Full live | 167.417 / 117.584 us | 205.208 / 147.791 us | 1.145 / 1.097 ms | 21.551 / 15.154 ms |
| Partially expired | 1.208 / 1.125 us | 21.167 / 21.459 us | 11.708 / 12.333 us | 0.379 / 0.390 ms |
| Churn | 58.417 / 54.542 us | 98.708 / 76.500 us | 11.375 / 15.542 us | 5.442 / 3.948 ms |

Aggregate mutex profiles across all three scenarios and repetitions attributed
848.95 ms before and 720.96 ms after to `sync.Mutex.Unlock` contention. Cumulative
`allowIPIn` attribution was 444.30 / 396.95 ms; `RecordFailure` was
404.62 / 324.02 ms. These are sampled aggregate waiter delays, not lock hold
durations or exclusive eviction CPU time. Profiles include the whole benchmark
process, including fixture setup and benchmark calibration.

The final full-live failure p50 and epoch medians decreased by about 30%.
Counterevidence remains: the partially expired epoch median increased by about
3%, and churn legitimate p99 increased despite its faster epoch. The churn
legitimate p99 ranges overlap (before 9.667-25.042 us; after 11.709-16.084 us).
This is a local non-dedicated host, not an isolated statistical study. No HTTP
latency, Argon2id throughput, audit throughput, production runtime, hardware floor
or SLO claim is made. The evidence supports a smaller bounded traversal, not a
more complex eviction subsystem or a universal latency improvement.

Before resource coordination, the same matched harness recorded full-live
failure p50 237.958 / 128.292 us and aggregate mutex delays 1,191.96 / 549.91 ms
while other agents built and graphed. That original comparison remains in this
PR's first commit and its original local logs; it is not the final table above.
It also showed a churn failure p50 increase and overlapping legitimate tails.

An earlier exploratory 100-epoch run used an unattributed legitimate source and
no churn resets. It is not the matched comparison above. Its original output is
retained locally and was not substituted for the corrected harness results.

## Portable reproduction

Run from the repository root on each compared revision with the same benchmark
file and toolchain. Keep the baseline production source unchanged. Use a writable
artifact directory supplied by the operator as `ADMISSION_PROFILE_DIR`; output
files are local evidence. It must already exist and be writable.

```sh
: "${ADMISSION_PROFILE_DIR:?Set a writable artifact directory}"
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 ./internal/admission -run '^$' \
  -bench '^BenchmarkEvictionPressure$' -benchtime=25x -count=3 \
  -mutexprofile="$ADMISSION_PROFILE_DIR/mutex.pprof" -mutexprofilefraction=1 \
  -o "$ADMISSION_PROFILE_DIR/admission-profile.test"
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go tool pprof -unit=ms -top \
  "$ADMISSION_PROFILE_DIR/admission-profile.test" "$ADMISSION_PROFILE_DIR/mutex.pprof"
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 ./internal/admission -count=1
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -race -p 2 ./internal/admission -count=1
```

The mutex profiles and exploratory raw output are ephemeral local artifacts,
not committed portable artifacts. Re-run the harness to obtain new observations.

## Acceptance evidence and limits

The authorized endpoint is push and review. Merge, auto-merge, queue admission,
release and deployment remain held. Completed local evidence:

- Admission package tests passed, with final rerun 0.732 s. The admission race
  suite passed (2.888 s), retaining local/shared and generation-inheritance tests.
- The new benchmark passed all three scenarios under the race detector (one
  epoch each, 4.105 s). Race timings are not performance comparisons.
- `go vet -p 2 ./internal/admission`, changed-file formatting and working-tree
  fast feedback passed (three Go files, package compilation).
- `TestCoordinationSQLite` and `TestCoordinationPostgres` passed against a
  disposable PostgreSQL 18.6 server (combined package run: 42.331 s). Both engines
  exercised admission windows, account counts, datastore clocks, success reset,
  pruning and the existing coordination invariants.
- The final allocated isolation run passed all six engine cases in 6.310 s:
  `TestEmailLoginCanonicalThrottle`,
  `TestDeveloperUnknownCredentialFloodBoundsDurableRefusals` and
  `TestHumanRequestAdmissionPrecedesProtectedWork`, each on SQLite and PostgreSQL
  18.6. It used the environment above and the documented
  `HIKYO_TEST_POSTGRES_DSN` fixture setting.
- Fourteen selected repository guard checks passed (194.288 s), using their
  supported default/UI/Darwin/Windows contexts where applicable: proof signatures
  and forgery, transaction results, fences, SQL predicates, driver handles,
  redaction surfaces, sensitive formatting, audit append-only rules, grant locks,
  raw-SQL protocols, service seams, test-only observers and upgrade build authority.
  The final allocated batch ran one command at a time and released its allocation
  only after owned subprocesses exited.
- Documentation preparation passed in read-only mode. No generated source inputs,
  SQL, migrations, API contracts or generated files change; regeneration is not
  required for this plain Go algorithm change.

Regression reproduction with `HIKYO_TEST_POSTGRES_DSN` set to a disposable
PostgreSQL test datastore (fixtures create and drop owned scratch databases):

```sh
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 ./internal/isolation -count=1 -v \
  -run '^(TestEmailLoginCanonicalThrottle|TestDeveloperUnknownCredentialFloodBoundsDurableRefusals|TestHumanRequestAdmissionPrecedesProtectedWork)$'
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 ./internal/lint -count=1 -v \
  -run '^(TestProofSignaturesRepo|TestProofForgeryRepo|TestTransactionResultsRepo|TestFenceCompletenessRepo|TestSQLPredicatesRepo|TestDriverHandlesRepo|TestRedactionSurfacesRepo|TestSensitiveFormattingRepo|TestAuditAppendOnlyRepo|TestGrantLockRepo|TestObserversAreTestOnlyRepo|TestServiceSeamsRepo|TestRawSQLProtocolsRepo|TestUpgradeBuildAuthorityRepo)$'
```

Retained failed or interrupted attempts:

- PostgreSQL 14.24 failed in upgrade admission with `upgrade: corrupt control
  state`, before counter assertions. SQLite passed in that attempt. The later
  PostgreSQL 18.6 pass does not erase that failure or bypass the gate.
- Initial isolation dependency builds and repository analyzer loading were
  interrupted to reduce resource pressure, before completed assertion results.
  Logs and caches were retained; no test or analyzer assertion was weakened.
- The clustered graph run completed AST extraction before interruption. Its
  two-worker `--no-cluster` retry also completed extraction before interruption.
  Both reported 17 partially parsed Astro files outside this Go scope. Graphify
  remains paused under the parent's resource instruction. No completed graph
  navigation proof is claimed.

Independent maintainer security review and full current-base hosted validation
remain required before any later merge. No independent or cross-provider review
is claimed here. The optional hosted performance job was skipped by the existing
paid-run budget; no extra credit or substitute reviewer was requested. No
production runtime, release or deployment verification was performed.

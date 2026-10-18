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
Comparison: the single-pass implementation in this PR, using the identical new
`BenchmarkEvictionPressure` harness. Go 1.27.2, macOS arm64, Apple M2 Pro,
`GOMAXPROCS=2`, `go test -p 2`, three repetitions of 25 epochs per scenario,
mutex profile fraction 1. Each epoch seeds exactly 4,096 IP and account entries
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
| Full live | 237.958 / 128.292 us | 36.917 / 8.364 ms | 2.399 / 2.306 ms | 228.191 / 75.273 ms |
| Partially expired | 1.459 / 1.458 us | 315.625 / 163.125 us | 197.750 / 112.583 us | 4.090 / 1.230 ms |
| Churn | 36.375 / 59.292 us | 7.207 / 2.379 ms | 456.750 / 394.625 us | 43.513 / 19.224 ms |

Aggregate mutex profiles across all three scenarios and repetitions attributed
1,191.96 ms before and 549.91 ms after to `sync.Mutex.Unlock` contention. Cumulative
`allowIPIn` attribution was 846.29 ms / 394.01 ms; `RecordFailure` was about
327.34 ms / 151.64 ms. These are sampled aggregate waiter delays, not lock hold
durations or exclusive eviction CPU time. Profiles include the whole benchmark
process, including fixture setup and benchmark calibration.

Counterevidence matters: churn failure p50 increased, and the full-live legitimate
p99 observations overlap (before 1.790-2.694 ms; after 1.777-2.491 ms). Other agents
were building and graphing on the same host. No isolated-host statistical speedup,
HTTP latency, Argon2id throughput, audit throughput, production runtime, hardware
floor or SLO claim is made. The evidence supports a smaller bounded traversal,
not a more complex eviction subsystem or a universal latency improvement.

An earlier exploratory 100-epoch run used an unattributed legitimate source and
no churn resets. It is not the matched comparison above. Its original output is
retained locally and was not substituted for the corrected harness results.

## Portable reproduction

Run from the repository root on each compared revision with the same benchmark
file and toolchain. Keep the baseline production source unchanged. Use a writable
artifact directory supplied by the operator; output files are local evidence.

```sh
GOMAXPROCS=2 go test -p 2 ./internal/admission -run '^$' \
  -bench '^BenchmarkEvictionPressure$' -benchtime=25x -count=3 \
  -mutexprofile=mutex.pprof -mutexprofilefraction=1 -o admission-profile.test
GOMAXPROCS=2 go tool pprof -top admission-profile.test mutex.pprof
GOMAXPROCS=2 go test -p 2 ./internal/admission -count=1
GOMAXPROCS=2 go test -race -p 2 ./internal/admission -count=1
```

The mutex profiles and exploratory raw output are ephemeral local artifacts,
not committed portable artifacts. Re-run the harness to obtain new observations.

## Acceptance evidence and limits

Validation results are recorded below after completion. The PR endpoint is
push and review only. Merge, auto-merge, queue admission, release and deployment
are explicitly held for later. Hosted full-candidate proof remains a separate
requirement before any future merge.

Completed local evidence:

- Admission package tests passed; admission race suite passed (2.888 s).
  Existing local/shared and generation-inheritance tests are retained.
- `go vet -p 2 ./internal/admission` and changed-file formatting passed.
- `TestCoordinationSQLite` and `TestCoordinationPostgres` passed against a
  disposable PostgreSQL 18.6 server (combined package run: 42.331 s). Both
  engines exercised admission windows, account counts, datastore clocks,
  success reset and pruning, plus the existing coordination invariants.
- The initial PostgreSQL 14.24 attempt failed in upgrade admission with
  `upgrade: corrupt control state`, before counter assertions. SQLite passed
  in that attempt. It is retained as a failed attempt, not represented as
  PostgreSQL 18 proof or fixed by changing the gate.
- The documentation preparation check passed in read-only mode. No generated
  source inputs, SQL, migrations, API contracts or generated files change;
  no regeneration is required for this plain Go algorithm change.

Outstanding at initial publication:

- Targeted authentication/isolation flows are compiling dependencies with both
  engine fixtures configured. They are not yet passing evidence.
- Repository analyzer checks and graph refresh were interrupted to reduce
  concurrent memory pressure. The same analyzer selection will be retried after
  isolation compilation; no analyzer or test assertion was weakened.
- The initial clustered graph run completed AST extraction but was interrupted
  during later processing. A two-worker `--no-cluster` retry also completed
  extraction before interruption. Both reported 17 partially parsed Astro files
  outside this Go scope. No completed graph navigation proof is claimed.
- Independent maintainer security review and full current-base hosted validation
  remain required before any later merge. No independent or cross-provider
  review is claimed by this implementation handoff.

Final package rerun passed (0.732 s), the new benchmark completed all three
scenarios under the race detector (one epoch each, 4.105 s), and the repository's
working-tree fast-feedback checks passed (three Go files, package compilation).
Race-instrumented benchmark timings are not performance comparisons.

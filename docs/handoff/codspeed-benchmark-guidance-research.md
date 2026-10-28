# CodSpeed benchmark relevance: research guidance

Researched 2026-10-26. Research only: public documentation and first-party source; no benchmarks, authentication, Macro usage, product/CI changes, commits, or pushes. The parent investigation owns local benchmark and historical PR attribution. This note does not establish the cause of any existing regression.

**Recommendation, judgment:** select benchmarks by the user operation and workload they represent, then assess absolute cost, frequency, and measurement stability. A large percentage on a tiny helper is useful diagnostic evidence, but does not by itself establish a meaningful user-facing slowdown.

## Workload selection and tiny benchmarks

**First-party guidance:** CodSpeed's harness authoring guidance calls for realistic inputs and sizes, critical paths, multiple scenarios, isolation of setup, and protection against dead-code elimination. It identifies public APIs, processing pipelines, I/O, and algorithms as candidates. Its advice to keep benchmarks short is about an individual workload and framework-managed repetition; it is not a documented minimum duration or a reason to reject microsecond operations. [Harness guidance, pinned source](https://github.com/CodSpeedHQ/codspeed/blob/d9612e39afc89b405b31d2849bb81393acb43b62/skills/codspeed-setup-harness/SKILL.md#step-4-write-good-benchmarks).

**Documented:** CodSpeed's Go guide recommends deterministic data, fixed workload inputs rather than using `b.N` as the input size, subbenchmarks for input variation, repeated native measurements, and allocation tracking. It explicitly warns that `StopTimer`/`StartTimer` overhead can distort cheap operations. Native `-count`/`benchstat` recipes are not automatically valid CodSpeed commands; see compatibility below. [Go guide](https://codspeed.io/docs/guides/how-to-benchmark-go-with-testing).

**Judgment for the parent investigation:**

- Give each benchmark a contract: actual caller/user operation, input size and distribution, cold/warm state, included work, excluded setup, and calls per operation. Cover representative and large workloads, plus important hit/miss or changed/unchanged paths where applicable.
- Keep a tiny helper benchmark if it protects a frequent hot path or localizes regressions. Pair it with a representative operation benchmark before inferring product impact.
- Estimate additive cost as `extra time per call × calls per user operation`. A 1 microsecond increase used once adds 1 microsecond; used 1,000 times adds 1 millisecond, assuming serial additive work. These are illustrative calculations, not Hikyo measurements.
- Repetition improves measurement precision; it does not make an unrepresentative input representative. Excluding setup defines a narrower question, so record whether startup or per-request initialization matters to the user flow.

## Walltime and simulation answer different questions

**Documented:** walltime measures elapsed time, including syscalls, I/O, and parallelism effects. CodSpeed recommends dedicated Macro hardware for stable CI walltime and warns against multiple competing benchmark processes. Its sampling-based hardware-event profiles become less reliable deep in leaf functions. A Macro runner does not imply that only large workloads are valid. [Walltime instrument](https://codspeed.io/docs/instruments/walltime).

**Documented:** CPU simulation runs a workload once, models instruction/cache/memory costs, and converts simulated cycles into estimated time. Syscall time is excluded from the primary measurement. Consequently, simulated time is not an observed endpoint latency. [CPU simulation](https://codspeed.io/docs/instruments/cpu).

**Go limitation:** the Go integration currently supports **walltime only** and recommends `b.Loop()`, while retaining `b.N` compatibility. This overrides generic advice to use simulation for stable CPU microbenchmarks. The runner also explicitly refuses other modes. [Go integration](https://codspeed.io/docs/benchmarks/go), [v5.2.1 Go interception script](https://github.com/CodSpeedHQ/codspeed/blob/v5.2.1/src/executor/helpers/introspected_golang/go.sh#L88-L92).

**Version difference:** CodSpeed v5, announced 2026-08-03, changed simulation cycle weighting and makes older simulation comparisons N/A. It also switched the default walltime profiler to its samply fork. Do not transfer simulation baseline rules or historical profiler assumptions blindly to a Go walltime report. [v5 announcement](https://codspeed.io/changelog/2026-08-03-codspeed-v5-only-your-code-at-its-real-cost).

## Go timer compatibility and integration hazards

Native semantics below come from [Go 1.26 benchmark source](https://github.com/golang/go/blob/go1.26.0/src/testing/benchmark.go). CodSpeed-specific observations are pinned to **codspeed-go v1.3.0**, tree `a5f6b7c15e68ac9a854acffe323b7e5177d26844`. They are static findings, not executed reproductions.

| API | Native contract and CodSpeed relevance |
| --- | --- |
| `b.StartTimer()` | Resumes timing; timing starts automatically. CodSpeed wraps it with timestamp-marker bookkeeping. |
| `b.StopTimer()` | Pauses timing. CodSpeed records marker boundaries; frequent calls add instrumentation work. |
| `b.ResetTimer()` | Clears elapsed time, allocation counters, and custom metrics without changing whether timing is running. CodSpeed additionally clears its sample and timestamp arrays. |
| `b.Loop()` | Available since Go 1.24; excludes setup before the first call and cleanup after completion. Use the exact `for b.Loop() { ... }` form for compiler protection against eliminated calls. CodSpeed replaces the loop implementation and records individual iterations. |
| `b.N` loop | Supported, but setup can execute repeatedly during calibration and measurement. The CodSpeed runner has its own warmup and round logic. |

Sources: [CodSpeed timer wrappers and reset](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/benchmark1.26.0.go#L136-L185), [CodSpeed loop](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/benchmark1.26.0.go#L419-L549), [warmup and measurement helpers](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/codspeed.go#L261-L378).

**Source-backed hazards to check against existing run evidence:**

1. **Native output is not instrumentation equivalence.** The runner overlays `testing`, forces cgo, preserves debug information, and uses isolated build/module caches. It supplies its own `-run=^$`. A native benchmark passing establishes neither the same harness nor the same timed work. [Runner command construction](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/src/runner/mod.rs#L41-L87).
2. **Flag documentation lags source.** The Go integration page lists only `-bench`. v1.3.0's parser also implements `-benchtime`, defaults to **3s**, and warns/ignores unknown flags. A separated unsupported flag value can become a package argument. Flags after the first package are collected as package arguments. Do not paste native `-run`, `-count`, `-cpu`, or profile recipes into this integration. The help text mentions `--dry-run`, but the parser has no corresponding implementation. [Parser and tests](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/src/cli.rs#L21-L124).
3. **Manual pauses inside `b.Loop()` need scrutiny.** `SaveMeasurement()` uses elapsed time since the last start for a Loop sample, whereas `b.N` samples use accumulated `b.duration`. Multiple timed segments, or cleanup after a final `StopTimer`, can therefore differ from the native accumulated-time contract. This is a static compatibility concern; it does not prove any local benchmark uses the problematic pattern. Allocation-stat updates in the marker-free start/stop helpers are also commented out, so do not assume native `ReportAllocs()` equivalence. [Measurement and timer helpers](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/codspeed.go#L261-L320).
4. **Internal round targets do not prove retained sample counts.** The warmup helper targets up to 100 measurement rounds for `b.N`, but `runN` invokes `ResetTimer`, which clears sample arrays. Check actual retained rounds and dispersion before describing a report as 100 independent samples. Duration prediction also includes overhead and has a three-times-duration guard. [Warmup helper](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/codspeed.go#L322-L378), [reset and runN](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/benchmark1.26.0.go#L160-L230).

**Version pinning matters:** v0.6.0 fixed Loop markers; v0.6.1 fixed incomplete `b.N` time reading; v1.0.0 introduced the overlay rewrite; v1.3.0, released 2026-08-24, added Go 1.26/1.27 support. The v1.3.0 selector uses the 1.26 overlay for Go versions at least 1.26. [Changelog](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/CHANGELOG.md), [overlay selection](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/src/runner/overlay/mod.rs#L20-L39).

The inspected Hikyo workflow pins Action v5.2.1. That Action defaults to runner 5.2.1; its `go-runner-version` input description says latest, but runner v5.2.1's installation script actually defaults to Go runner **1.3.0**, and reuses an already installed runner when present. Historical jobs still require their own version receipts. [Action defaults](https://github.com/CodSpeedHQ/action/blob/373d6868929f444bc08d901fd0eb0ad52a8875ea/.codspeed-runner-version), [input description](https://github.com/CodSpeedHQ/action/blob/373d6868929f444bc08d901fd0eb0ad52a8875ea/action.yml#L77-L79), [actual runner selection](https://github.com/CodSpeedHQ/codspeed/blob/v5.2.1/src/executor/helpers/introspected_golang/go.sh#L37-L37), [installed-runner reuse](https://github.com/CodSpeedHQ/codspeed/blob/v5.2.1/src/executor/helpers/introspected_golang/go.sh#L123-L136). Local sources read: `.github/workflows/codspeed.yml` and `go.mod` (Go 1.27.3).

## Thresholds, percentages, and practical impact

**Documented:** CodSpeed defines impact using **speed**, not elapsed-time percentage: `impact = speed_head / speed_base - 1`. Negative values mean slower. The overall impact uses a geometric mean of changed benchmarks, with a fallback to all benchmarks when none exceed threshold. Individual regressions still fail the performance check even if the aggregate is positive. [Impact metrics](https://codspeed.io/docs/features/understanding-the-metrics).

**Derived arithmetic**, using `speed = 1/time`:

| Time per operation | Absolute increase | Duration increase | CodSpeed speed impact |
| --- | --- | --- | --- |
| 2 to 3 microseconds | 1 microsecond | +50% | -33.3% |
| 2 to 4 microseconds | 2 microseconds | +100% | -50% |

Thus `-10%` speed impact corresponds to about `+11.1%` duration. Always label the quantity when quoting a percent.

**Documented:** the default regression threshold is 10%; per-benchmark settings override the global value, and a benchmark exceeding its threshold can fail the check. CodSpeed documents percentage thresholds; these sources do not document an additional absolute microsecond floor. The 2025-10-31 announcement recommends stricter thresholds for stable simulation microbenchmarks and more tolerance for variable walltime workloads. [Threshold settings](https://codspeed.io/docs/features/customization), [per-benchmark announcement](https://codspeed.io/changelog/2025-10-31-per-benchmark-regression-thresholds).

**Judgment:** report both absolute and relative deltas, the represented operation, and existing evidence of variability. Choose tolerances from stable baseline evidence and practical cost; tiny absolute size alone neither invalidates a regression nor justifies a blocker. A geometric mean of relative benchmark changes is not a workload-frequency-weighted estimate of user latency. Adjusting a threshold is a policy decision, not a diagnosis.

## Evidence the parent should preserve

For each existing comparison, capture base/head run and commit identities, benchmark identity/input contract, absolute values, speed impact, retained rounds/dispersion, loop/timer pattern, architecture, Go/Action/runner/integration versions, and profiler settings. Current docs select a PR baseline from the latest measured base-branch commit, and non-PR events use branch selection; do not assume a displayed comparison is against the Git merge base. [Baseline selection](https://codspeed.io/docs/features/understanding-the-metrics#baseline-report-selection).

No empirical minimum measurable duration, actual dashboard thresholds, historical installed versions, or cause of a Hikyo slowdown was established here. Those remain questions for existing artifacts and run metadata, within the parent's no-new-benchmark boundary.

# CodSpeed benchmark relevance review

The accepted recommendation was implemented locally afterward. See the [implementation and measured verification](codspeed-benchmark-selection.md) for the current patch and remaining hosted qualification. The investigation below preserves the original evidence and scope.

Investigated 2026-10-26 against Hikyo `ae2f1dda8bac294fa5bd3a20927115f5f3f00844`, also the live main head when checked. Endpoint: investigation and recommendations. No product or CI edits, benchmark execution, paid-run requests, account changes, commits, or pushes.

## Verdict

The recurring roughly 2 microsecond publish/import/audit results are invalid measurements of the named operations. Existing hosted logs show the same operations taking tens or hundreds of milliseconds. A source trace identifies a CodSpeed Go timer-accounting defect that explains the mismatch: these three benchmarks resume the timer after their post-operation checks, and CodSpeed's Loop samples retain that short final interval rather than accumulated operation time.

This supports repairing those measurements, not deleting useful workflow coverage. Separately, the suite mixes useful workflow benchmarks, diagnostic microbenchmarks, obsolete comparison algorithms, and correctness/work-count assertions under one broad paid selection. Some of the latter belong primarily in invariants. Small duration alone is not a sound removal criterion.

## Historical evidence

The following are live CodSpeed bot comments read on the investigation date. Comments can be edited by subsequent runs; the linked comments identify the inspected evidence. Percentages below are CodSpeed speed impact, not percentage growth in elapsed time.

| PR and comparison | Reported measurement | Interpretation |
| --- | --- | --- |
| [#892, filesync](https://github.com/Hikyo-Org/Hikyo/pull/892#issuecomment-6128505802), `21773f3` to `d259293` | Publish 1.9 to 2.3 microseconds, -18.32%; import 2.3 to 1.9 microseconds, +21.29% | Both reset-heavy workflows have the defective Loop timer pattern. The comment also warns that the intended main baseline was unavailable and an older one was substituted. |
| [#883, invitation claims](https://github.com/Hikyo-Org/Hikyo/pull/883#issuecomment-6101192713), `2a50a75` to `533428b` | Publish 2.1 to 1.8 microseconds, +13.07%; import 1.8 to 2.2 microseconds, -18.68% | The same two workloads move in opposite directions at the timer-tail scale. This is not evidence of corresponding workflow speed changes. |
| [#994, presence indexing](https://github.com/Hikyo-Org/Hikyo/pull/994#issuecomment-6254238149), `a192cfa` to `a02b0a9` | Import 1.8 to 2.2 microseconds, -17.03%; dense maximum-size presence scan 552.2 ms versus indexed 16.4 ms | One report mixes an invalid import measurement with a useful algorithm comparison. The presence result compares implementations in that head, rather than proving deployed end-to-end latency. |
| [#1013, Go dependencies](https://github.com/Hikyo-Org/Hikyo/pull/1013#issuecomment-6299836569), `ad65a00` to `e91751b` | Publish 1.8 to 2.1 microseconds, -14.62%; bulk audit 2.0 to 1.7 microseconds, +19.29%; router POST 423.7 to 574 microseconds | The first two have the timer defect. The router result measures a different path and cannot be dismissed on that basis. |
| [#969, AWS adoption](https://github.com/Hikyo-Org/Hikyo/pull/969#issuecomment-6390045214), `059902e` to `327dec1` | Publish 1.7 to 2.0 microseconds, -12.2%; bulk audit 1.8 to 2.0 microseconds, -12.5%; import 2.1 to 1.9 microseconds, +14.84% | All three are affected. The aggregate -4.09% headline is not an estimate of application latency. |

Existing Actions output supplies a particularly strong control: these are not comparisons between different laptops or hardware architectures.

| Existing run | Operation | Go harness stdout in that hosted run | CodSpeed comment for that head |
| --- | --- | ---: | ---: |
| [#892, run 38277263724, attempt 2](https://github.com/Hikyo-Org/Hikyo/actions/runs/38277263724/job/114907301804) | Publish | 192,265,902 ns/op = 192.266 ms/op | 2.3 microseconds |
| Same run | Import | 382,596,880 ns/op = 382.597 ms/op | 1.9 microseconds |
| [#969, run 40232421613, attempt 2](https://github.com/Hikyo-Org/Hikyo/actions/runs/40232421613/job/121287339753) | Publish | 169,641,929 ns/op = 169.642 ms/op | 2.0 microseconds |
| Same run | Import | 340,741,043 ns/op = 340.741 ms/op | 1.9 microseconds |
| Same run | Bulk audit, 300 disclosures | 96,546,855 ns/op = 96.547 ms/op | 2.0 microseconds |

Both runs identify CodSpeed runner v5.2.1 and Linux ARM64. The later run uses Go 1.27.3. Its upload hash is `5dc9d47ab94fe5ae072e4c36a2d2a57efd2214f7aa13b79e8ff3fc9925bc9f76`; the earlier upload hash is `b5801dc7b56367e6dae0f48fb06c210fe0dc501aeaa3988a95158dfc6ce8866d`. Stdout and uploaded statistics are different measurement paths, so these stdout values establish the scale mismatch, not a replacement statistical analysis.

## Why the measurements are so small

The three affected owners are:

- [Publish and import](../../internal/service/ux_benchmark_test.go): each iteration stops timing for fixture restoration, starts timing for the real operation, stops timing for readback, then starts timing again immediately before returning to `b.Loop()`.
- [Bulk audit](../../internal/service/audit_benchmark_test.go): the same pattern surrounds a real authorized transaction, 300 durable audit inserts, and an untimed query checking their existence.

Native Go accumulates the timed operation and the short tail in `b.duration`. CodSpeed v1.3.0 uses another path for uploaded Loop samples:

1. The overlaid `Loop()` stops timing and calls `SaveMeasurement()` on each iteration.
2. `StartTimerWithoutMarker()` sets `b.start` to the latest start time.
3. While Loop is running, `b.N` is zero. `SaveMeasurement()` consequently uses elapsed time since `b.start`, not accumulated `b.duration`.
4. The final `StartTimer()` in the Hikyo benchmark moves `b.start` past the operation and its readback. The uploaded sample therefore represents the small tail after that restart.
5. Printed Go `ns/op` still uses accumulated duration. That explains how one execution can print hundreds of milliseconds while its report shows about two microseconds.

First-party pinned sources: [Loop interception](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/benchmark1.26.0.go#L522-L549), [sample capture and timer helpers](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/overlay/codspeed.go#L261-L320). Runner [v5.2.1 defaults to Go integration 1.3.0](https://github.com/CodSpeedHQ/codspeed/blob/v5.2.1/src/executor/helpers/introspected_golang/go.sh#L37), and the integration [selects the 1.26 overlay for Go 1.26 and later](https://github.com/CodSpeedHQ/codspeed-go/blob/a5f6b7c15e68ac9a854acffe323b7e5177d26844/go-runner/src/runner/overlay/mod.rs#L20-L39).

Evidence boundary: the operation/report mismatch is observed directly in existing hosted evidence. The mechanism above is a source-supported diagnosis, not a new executed minimal reproduction. The runner can reuse a preinstalled Go integration; the inspected logs do not independently attest that executable's hash. Raw uploaded per-round JSON was not retained as an Actions artifact in the inspected #969 run. Confirm installed integration identity and raw samples when qualifying a repair. This diagnosis does not explain separate scanner or router regressions.

## How the suite reached this state

| Origin | What entered continuous reporting | Original purpose |
| --- | --- | --- |
| [#847](https://github.com/Hikyo-Org/Hikyo/pull/847), authored by CodSpeed's bot | Existing scanner, router and acknowledgement benchmarks; new single-record envelope seal/open and 500-entry dotenv parse/encode benchmarks | Bootstrap performance tracking around existing benchmarks and selected hot paths. This explicitly introduced intentional microbenchmarks, without measured production call-frequency evidence. |
| [#850](https://github.com/Hikyo-Org/Hikyo/pull/850) | Nine real list/export/signals cases at 10, 100 and 1,000 keys; dedicated Graviton measurement lane | Add meaningful authorized, encrypted, disk-backed service reads. |
| [#863](https://github.com/Hikyo-Org/Hikyo/pull/863) | Publish/import/history/diff workflows, browser Matrix measurements, daily main runs and budgeted requested PR runs | Cover user operations with accumulated data. Publish/import introduced the timer pattern at issue. Original documented local publish/import results were already milliseconds. |
| [#879](https://github.com/Hikyo-Org/Hikyo/pull/879) | Automatic admission for broadly classified performance-relevant PRs | Control the shared allowance. Admission did not select individual affected benchmark families. |
| [#994](https://github.com/Hikyo-Org/Hikyo/pull/994), [#998](https://github.com/Hikyo-Org/Hikyo/pull/998), [#1000](https://github.com/Hikyo-Org/Hikyo/pull/1000), [#1001](https://github.com/Hikyo-Org/Hikyo/pull/1001) | Presence algorithm comparisons, authorized delivery polling, advisory fanout and audit measurements | Diagnose particular performance questions. Package-wide discovery also enrolled these experiments into every subsequent full report. |

Sources: PR bodies and merged history; [original service handoff](service-performance-benchmarks.md), [original UX measurements](ux-performance-benchmarks.md), [delivery profile](985-delivery-poll-profile.md), [advisory investigation](974-986-983-indexes-advisory.md), [audit investigation](984-987-audit-store-optimizations.md).

The current [PR workflow](../../.github/workflows/pr-benchmark.yml) always runs `go test -bench=.` over five packages. The [admission classifier](../../scripts/ci/benchmark/automatic.go) includes broad runtime changes and web/client changes, then returns one boolean. Thus a web-only admitted PR also runs the complete Go suite. Every new `Benchmark*` in these packages joins reporting automatically. This is the structural source of unnecessary scope and recurring diagnostic noise.

The inspected #969 run completed **66 Go cases**. The current source has the same family matrix below. PostgreSQL delivery cases are explicitly skipped in this CI lane because it does not provision that database.

## Disposition by benchmark family

These are recommendations, not implemented policy changes. Case counts account for all 66 completed cases in the inspected run.

| Family | Cases | Recommendation and reason |
| --- | ---: | --- |
| Publish, mixed import, bulk audit | 3 | **Repair and retain.** They time valuable durable operations, but their current uploaded timings cannot support regression decisions. Preserve fixture restoration and result/durability checks. Keep audit work-count invariants alongside timing. |
| Values list, revision export, environment signals | 9 | **Retain representative service coverage.** Real authorization, SQLite, decryption and projection are useful. Prioritize typical and large scenarios; select relevant families on PRs. These measure warm service calls, not full endpoint latency. |
| Long-lived history and sparse revision diff | 2 | **Retain.** They cover growth and sparsity in real user operations; the inspected stdout is 2.56 ms and 45.12 ms, respectively. |
| Authorized delivery polling | 24 | **Curate PR selection; retain scheduled/diagnostic breadth.** Typical/key-limit/render-limit, four projections, changed/unchanged are valuable axes. Keep a small representative PR set and the full matrix for relevant changes or scheduled investigation. Audit rows accumulate across samples, so document or control initial volume when assessing steady-state behavior. |
| Advisory fanout | 9 | **Separate stable throughput from overload behavior.** Large bursts deliberately drop streams and process fewer events than offered. A lower elapsed time with fewer processed events is not an improvement. Preserve drop/revocation correctness tests; pair time with processed events, drops and read transactions. |
| Catalogue presence, old scan versus indexed algorithm | 8 | **Move the historical comparator to explicit diagnostics.** The original comparison demonstrated substantial algorithmic cost. Re-running the pre-optimization implementation continuously is less useful than representative current service coverage. Retain equivalence tests and suitable work/scale invariants. |
| Maximum acknowledgement match/reject | 2 | **Make work invariants primary; keep timing diagnostic.** `TestAckSetOpensEachPresentedTokenOnce` already protects the intended decode-once property. Timing can still reveal crypto/allocation costs, but it does not replace that invariant. |
| Envelope seal/open at 32 B and 4 KiB | 4 | **Diagnostic microbenchmarks unless call frequency justifies a gate.** The inspected stdout is genuinely about 2.9 to 23.2 microseconds per record. These measure a crypto primitive plus envelope handling, not a request. Real service benchmarks expose aggregate impact; crypto correctness and binding remain tests. |
| Dotenv parse and encode, 500 entries | 2 | **Keep as import/export diagnostics, selected on related changes.** These are complete bounded document operations, approximately 0.74/0.53 ms in the inspected stdout. Low absolute cost makes an application-wide gate hard to justify without usage evidence. |
| Authenticated router GET/POST with stub services | 2 | **Retain as explicitly named admission/router overhead diagnostics.** The original OpenAPI warmup question is valid. Stub authentication/services and `httptest` mean this is not real authenticated endpoint latency. Pair with real service or HTTP flows for user-facing claims. |
| Scanner, 64 KiB buffer | 1 | **Retain and improve workload coverage.** About 0.81 ms in the inspected stdout. One repeated-text buffer with an AWS-shaped candidate cannot represent clean, dense-finding and mixed-rule inputs. Add representative scenarios when redesigning it; keep corpus correctness and native floor acceptance separate. |

Existing deterministic checks support the user's invariant suggestion: [acknowledgement opens](../../internal/service/scan_ack_test.go), [delivery work and audit cardinality](../../internal/service/delivery_poll_test.go), [presence equivalence](../../internal/service/presence_index_test.go), and the [audit single-lookup evidence](984-987-audit-store-optimizations.md). Invariants answer how much work and what result is required. Timings answer how long representative work takes. Both are useful, but should not be conflated.

## Applying CodSpeed best practices

1. **Validate the sensor before interpreting regressions.** Repair the three Loop measurements against pinned integration source or an upstream fix. A narrowly scoped `b.N` implementation is a compatibility candidate, not a verified repair: this integration also clears retained samples during `runN` resets. Verify raw retained rounds, measured boundaries, printed totals, and report values. Use a deliberate known-duration control to show that real operation time is captured. Preserve the pre-repair history as invalid evidence and establish a fresh comparable baseline.
2. **Define a workload contract for every continuous benchmark.** Record caller, representative input, expected frequency, included work, excluded setup and warm/cold state. Keep inputs and state controlled. Repeating a helper more often improves sampling but cannot make it representative of a user operation. This follows CodSpeed's [first-party harness guidance](https://github.com/CodSpeedHQ/codspeed/blob/d9612e39afc89b405b31d2849bb81393acb43b62/skills/codspeed-setup-harness/SKILL.md#step-4-write-good-benchmarks) and [Go benchmarking guide](https://codspeed.io/docs/guides/how-to-benchmark-go-with-testing).
3. **Use the supported instrument.** CodSpeed's [Go integration](https://codspeed.io/docs/benchmarks/go) currently supports walltime only. Keep dedicated Macro hardware and avoid competing measurement processes, as its [walltime guidance](https://codspeed.io/docs/instruments/walltime) recommends. Switching this Go suite to simulation is not an available shortcut. Use only documented integration flags in CI; native statistical recipes do not transfer automatically.
4. **Select affected work.** CodSpeed explicitly supports [partial runs](https://codspeed.io/docs/features/partial-runs). Map changed owners and shared consumers to benchmark families; web-only changes should select browser performance without automatically selecting all Go cases. Keep broad scheduled coverage. Mark inherited results as skipped rather than implying they ran on that head. Archive intentionally retired benchmark identities so they do not persist as skipped forever.
5. **Interpret absolute cost and baseline provenance.** Report microseconds/milliseconds, percent speed impact, calls per user operation and exact compared commits. A 0.6 microsecond change used once adds 0.6 microseconds; used serially 1,000 times adds 0.6 ms. These are illustrative arithmetic, not observed Hikyo impact. Daily main sampling and available-run fallback are not an exact paired merge-base experiment.
6. **Set thresholds after measurement validity and variability are understood.** CodSpeed documents [per-benchmark percentage thresholds](https://codspeed.io/changelog/2025-10-31-per-benchmark-regression-thresholds), with different tolerances for stable and variable work. Its default is 10%, but live Hikyo dashboard settings were not inspected. Do not raise a threshold to conceal the invalid timer tails. A practical absolute-impact criterion would be an explicit repository policy, not a verified built-in absolute floor. The [aggregate metric](https://codspeed.io/docs/features/understanding-the-metrics) is based on relative benchmark changes, not product traffic weights.
7. **Preserve correctness and existing acceptance boundaries.** The required one-iteration benchmark smoke executes assertions but does not enforce performance. Move durable work contracts into ordinary invariant tests where appropriate; preserve them when curating benchmarks. The separate native floor and browser checks answer different questions and should retain their own evidence. Respect the repository's CI conservation/inventory rules when implementing changes.

## Recommendation for the next implementation

**Recommended: targeted measurement repair plus benchmark selection.** Fix and qualify the three invalid measurements first; keep useful service/user workflows; make work-count invariants primary for algorithm contracts; move historical comparators and narrow helpers out of routine PR gating; add affected-family selection while preserving scheduled breadth and budget controls.

Deleting every microsecond benchmark would remove useful diagnostics and miss the actual timer defect. Keeping all 66 cases on every admitted PR preserves unnecessary scope. Raising global thresholds would hide both invalid samples and potentially real regressions. The targeted option offers the best balance of useful performance evidence, understandable failures and maintenance cost.

## Verification and limits

Read-only checks covered current source and Git history, originating PR descriptions, five historical CodSpeed reports, two existing hosted logs, official guidance and pinned integration source. The later hosted log contains exactly 66 completed benchmark lines. No new performance or correctness run was needed for this investigation; no new latency, hosted qualification or review-CLEAN claim is made.

The research companion, [CodSpeed benchmark guidance](codspeed-benchmark-guidance-research.md), separates documented behavior from additional static compatibility hazards. The full report and that research also have local self-contained HTML companions shared in chat; HTML is outside version control. Existing public comments/logs were saved as local evidence snapshots. No account thresholds, billing state, raw CodSpeed backend sample distributions, field workload frequencies or separate scanner regression cause were established.

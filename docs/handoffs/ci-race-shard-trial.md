# Complete race-shard hosted trial

**Verdict: do not adopt the measured batched compile-then-run variant in production.** On the complete predetermined shard, it preserved coverage but took **40.185 seconds longer (+5.188%)**, including its entire compilation phase. The earlier repeated tiny-target pilot does not predict this production-shaped workload.

[Successful hosted run 38645912061](https://github.com/Hikyo-Org/Hikyo/actions/runs/38645912061) and [job 116132591133](https://github.com/Hikyo-Org/Hikyo/actions/runs/38645912061/job/116132591133) completed on 14 October 2026. Historical trial source was `29ae9b726750bd6b0b77aa978b2da47ab60b7a2f`; the immutable product checkout was `48189fff82e94b32f32637b1920464e278d9caff`, using Go 1.27.2 on Linux/amd64. This does not measure the current main branch or the complete CI suite.

## Inclusive timing and runner occupancy

| Measurement | Baseline | Candidate |
| --- | ---: | ---: |
| Complete timed shard, including preparation specific to the variant | 774.533 s | 814.717 s |
| Candidate batch compilation, already included above | n/a | 198.513 s |
| Candidate remainder after compilation, not a fair standalone comparison | n/a | 616.204 s |
| Fresh PostgreSQL setup, recorded separately | 28.371 s | 10.424 s |
| Shard plus PostgreSQL setup | 802.903 s | 825.141 s |
| Timed shard occupancy of one runner | 12.909 min | 13.579 min |

The candidate/base ratio was **1.051882**; speedup expressed as baseline/candidate was **0.950677x**, a slowdown. The postcompile remainder was 158.329 seconds shorter than the baseline, but precompilation cost 198.513 seconds and erased that apparent benefit. The baseline can overlap compilation with another package execution; introducing a whole-shard compile barrier changes that overlap. These figures do not isolate compiler CPU savings from startup or fixture variability.

Even including the candidate's cheaper PostgreSQL setup, it remained **22.238 seconds slower**. The common setup phase took 35.953 seconds. The entire experimental job occupied one standard Ubuntu hosted runner for **28m 40s** across both variants, validation, setup, and evidence upload. Run creation to job start was 98 seconds, shared by the experiment and not attributable to either variant. These are elapsed occupancy measurements, not a monetary billing claim. No extra runner capacity was added.

## Independently checked coverage

Raw JSON streams and process receipts conserve **16 unique packages**, comprising 13 test-bearing packages and three genuine packages without test files. Each variant ran all **367 expected roots** exactly once: 366 passed and the maintained Cosign fixture skipped for its documented reason. All **560 subtests passed**, with no subtest skips. Both complete outcome sets were identical, every started target completed once, and all 16 process exits were zero.

The only allowed root skip was `internal/releasetrust/TestCosignLegacyBundleInProcessInterop`: `release fixture job supplies pinned COSIGN_BIN`. Packages `internal/filedurability`, `internal/store/sqlitegen`, and `scripts/ci/pr-benchmark-control` retained successful race-compilation receipts and genuine no-test package SKIP status, not fabricated test passes. The two-wide peer pool and service-after-peers ordering were verified from receipts. The frozen Linux plan hash matched the independently preserved trial inventory.

## Limits and follow-up

This was one cold, baseline-first A/B pair on one runner, using separate initially empty compiler caches, shared prefetched modules, and fresh pinned PostgreSQL before each variant. It has no reverse-order replication or statistical uncertainty estimate. Image and kernel page-cache effects persist; the first PostgreSQL setup also pulled its image. The candidate ran second and had a measured cheaper PostgreSQL setup, yet lost this pair; the direction and size of other order effects were not measured. The sample cannot establish the distribution of differences.

The comparison already keeps binaries on the same runner. Cross-runner upload, download, scheduling, and provenance checks were not measured here and would add costs to this version. This verdict applies to the tested whole-shard precompile barrier, not every possible binary-sharing design.

Retire the temporary harness and retain this evidence. Keep the existing production scheduler. Any materially different future design needs an inclusive, coverage-equivalent comparison within existing hosted capacity before adoption; this result does not justify production implementation or more runners.

[Portable evidence JSON](ci-race-shard-trial.json) retains provenance, artifact digest, inclusive timings, all common root/subtest outcomes, per-package receipts, independent checks, and raw JSON hashes. The original hosted artifact was `complete-race-shard-cold-pair-38645912061-1` (394,174 bytes; SHA256 `25680122ead6e516972a9ddea4ceb4512d818a302831879b55e2d0d1d60d4f81`), with scheduled expiry 21 October 2026. The portable evidence is intended to survive that expiry.

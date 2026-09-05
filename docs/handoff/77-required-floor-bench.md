# #77 required floor measurement gate

Implementation worktree: `/tmp/hikyo-floor-bench`, branch
`feat/required-floor-bench`, stacked base `aca25f2b692332821aa94e93f71f25ae2569de9a`
(PR #671, `fix/operator-memory-floor`).
No worker commits or pushes. Parent owns review, signed delivery and green merge.

## Contract and implementation

Authority: [#77 comment 5354008780](https://github.com/Hikyo-Org/Hikyo/issues/77#issuecomment-5354008780).
`scripts/bench/floor.sh` builds outside native Linux ARM64 measurement, requires
four CPUs / 4 GiB / zero swap, and emits `floor-<sha>.json` plus raw evidence.
CPU factors are committed 4.0, calibration fields null; memory factor 1.0.
`--raw` cannot satisfy required CI. Existing destinations and emulated targets
refuse. CI requires a clean exact candidate before and after build/measurement.

`internal/bench` owns evidence validation, refusal tests and the tagged process
driver. `internal/isolation/floor_bench_test.go` uses existing signed development
admission, valid encrypted synthetic cells and actual services: 100,000-cell
schema fan-out plus full authorized snapshot export; 250-row rewrap plus
committed 0/100/200 progression, production default pauses and full new-key
readback. Its native handles have one exact source-file lint exception.

The actual CLI boots with full normal defaults through `server --dev`. Five
idle process RSS samples follow readiness. Four real floor Argon2id derivations
hold four production limiter slots under the 272 MiB admission arithmetic.

Scanner runs directly as PID1 in an identically limited container, preserving
its genuine startup RSS rather than inheriting the large KDF parent address
space before exec. Its image, entrypoint, limits and successful exit are bound
to the composite. Benchmark-only ARM CPU ID parsing handles native ARM VMs
without inventing a board model; the scanner algorithm is unchanged.

The operator uses the existing actual five-phase, 50-CR workload, with matching
source commit/diff and identical Hikyo executable hash. **Dependency integrated:** corrected producer commit
`aca25f2b692332821aa94e93f71f25ae2569de9a` is the current stacked base. Retarget
the floor PR after #671 merges. The composite requires `operator_rss_peak_bytes`
and its measured process identity. Total cgroup `memory.peak` is diagnostic,
not process RSS. RSS stays strictly below 128 MiB, hard cgroup limit stays
128 MiB, OOM/restart refusals remain in the producer.

CI classifier, registry and `ci-required` include the planned reusable gate.
Git extraction uses `--no-renames` so moving measured code into docs cannot hide
its deletion. Real Git-tree rename/deletion tests and missing/skipped/failed
aggregate tests cover this. Standalone PR trigger is only for workflow changes;
main's trusted caller covers workload paths. All tags run the gate. Release
construction additionally depends directly on it with read-only permissions;
the rare duplicated tag run is intentional to enforce publication gating.

## Validation and honest remaining evidence

Focused ordinary Go evidence/parser/registry tests, tagged native-handle lint,
ShellCheck, Actionlint, preflight refusals, classifier, required-job aggregation,
trusted CI scripts and release binary reuse checks passed during implementation.
No broad package test flood was launched.

The isolated native local rehearsal is preserved in
`docs/reports/1.0/evidence/floor-bench/local-r6.json` and explained in
`docs/reports/1.0/floor-bench.html`. It is **failed dirty-source diagnostic
evidence**, not a release acceptance artifact:

| Metric | Raw | Conservative estimate or outcome |
| --- | --- | --- |
| 100,000 committed readable cells | 6,370.999 ms | ×4 = 25,483.997 ms, within 30 seconds |
| Scanner p99 | 1.439 ms | ×4 = 5.756 ms, above the unchanged 5 ms gate |
| Scanner compile / boot RSS | 0.633 ms / 11,878,400 bytes | Fits declared compile/memory bounds |
| Four actual Argon2 operations | 235.843 ms | Four admitted, four completed |
| Rewrap 250 rows | 217.196 ms | 0/100/200 progression, 102.995 ms minimum callback interval, readback complete |
| Cgroup peak | 691,249,152 bytes | Zero OOM kills |

Earlier timing runs overlapped other local work and are not used to assert a
production performance defect. No publish/scanner optimization, threshold or
factor change was made. Next: independent review, then exact-head GitHub native ARM measurement. Any hosted failure stays
blocking and receives diagnosis; do not fabricate or relabel a passing artifact.

The pre-refresh stash `floor-bench-before-d98-refresh` was retained by Git after
two additive conflicts. Both insertions were resolved preserving upstream's
exact ops-floor cache policy and native-handle entry. Do not blindly pop that
old stash over the completed worktree.

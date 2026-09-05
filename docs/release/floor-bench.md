# Required floor measurements

The [#77 amendment](https://github.com/Hikyo-Org/Hikyo/issues/77#issuecomment-5354008780)
makes the `floor-bench` CI gate required. Native Linux ARM64 runs inside a
four-CPU, 4 GiB, zero-swap cgroup. It uses development custody and synthetic
tenant content, without production secrets. Physical Pi calibration is optional.

Run from a checkout on native ARM64 Docker with cgroup v2, Go, kind, kubectl,
Helm and jq installed:

```sh
scripts/bench/floor.sh /absolute/new/evidence-directory
```

The destination must not exist. Builds happen outside the measured cgroup.
The existing real operator workload runs in an owned temporary kind cluster,
which is deleted before the app measurement container starts. SQLite uses a
private disk-backed directory, avoiding charging database pages as tmpfs RAM.

The result is `floor-<source-commit>.json`, accompanied by raw scenario results,
operator evidence, executable SHA-256 identities, source diff/status and the
Docker exit/OOM state. CI refuses a dirty source checkout. Local dirty runs are
diagnostic evidence, not release artifacts. Commit the passing exact-release
artifact under `docs/release/measurements/` and cite it in release acceptance.

| Measurement | Acceptance |
| --- | --- |
| Default `server --dev` startup and five idle RSS samples | Actual readiness, positive boot/RSS measurements; no invented latency bound |
| Four actual Argon2id operations through the production limiter | 272 MiB admission, 64 MiB/t3/p2 per operation, four concurrent slots, all complete, no cgroup OOM |
| Schema publish across 10 environments × 10,000 populated cells | Actual commit, all 100,000 values exported and compared afterward; CPU estimate ≤ 30 seconds |
| Reencrypt 250 rows | Observed committed progress 0/100/200, default 100-row batches and 100 ms pauses, all values readable on the new key |
| Scanner at the 64 KiB item cap | Direct scanner PID1 launch, current compiled rules/corpus; estimated p99 ≤ 5 ms and boot compile ≤ 2 seconds; actual boot RSS ≤ 32 MiB |
| Real operator under load | Same source/diff/Hikyo binary; measured process peak RSS below 128 MiB (cgroup charge peak remains diagnostic), 200m CPU, zero swap; existing five-phase reconciliation proof |

CPU measurements multiply the committed per-metric factors in
[derate.json](measurements/derate.json). Uncalibrated factors are **4.0**, with
null calibration commit/date. Missing or invalid factors fail closed. Memory
factor is **1.0**. Estimates are not measurements on a Raspberry Pi. Reencrypt
elapsed duration includes required pauses and has no throughput deadline.

Optional physical calibration uses the same workload:

```sh
scripts/bench/floor.sh --raw /absolute/new/raw-evidence-directory
```

Raw output is `raw-measurement-only`, cannot pass required CI and never replaces
a passing gate artifact. Record paired hardware/CI measurements and the physical
run's source commit/date before changing any committed factor.

`HIKYO_FLOOR_OPERATOR_EVIDENCE` can name a previously completed **same-source**
operator run to avoid duplicating that expensive workload during diagnosis. The
gate checks source commit, source diff and the actual Hikyo executable hash;
unrelated historical or different-build evidence is refused.

Main CI plans the reusable floor job for relevant app, datastore, crypto,
admission, publish, scanner, operator, chart and dependency changes. Its result
is included in the registry and `ci-required`; missing, failed or unexpectedly
skipped results cannot satisfy the aggregate. A standalone PR trigger for the
workflow file provides premerge proof of workflow changes without duplicating
the workload on ordinary source PRs. Every tag also runs the gate. The release workflow additionally depends directly on the same measurement job before constructing its unsigned draft. Rare release tags deliberately run twice so independent evidence cannot be mistaken for a blocking release dependency.

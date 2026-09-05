# #79 operator floor acceptance

Base: `f4175a5dbffc63a5d1e34bf450a7a52956a54668`. Implementation: Codex.
Parent owns integration, signed commit, CI dispatch and evidence disposition.

## Scope

`OBL-OPERATOR-PI-FIT`: actual operator process under shipped 200m CPU / 128 MiB
limits, with a complete native arm64 kind node constrained to 4 CPU / 4 GiB and
zero swap. The August 20 maintainer comment on #79 permits CI floor evidence;
physical Pi calibration is optional. No production cluster is used.

The script renders operator deployment/RBAC/service account from the chart,
retains its defaults/security/probes/leader election, and adds a synthetic TLS
delivery sidecar. It exercises 50 CRs with separate designated credentials,
64 keys each, 1 KiB per key: initial delivery, new values, conditional current,
HTTP 503 retain, recovery. Kubernetes API/CRD admission and operator are real.
The full Hikyo server is not inside this fixture and is not claimed as measured.

Every phase has a 300-second whole-fleet deadline chosen from the shipped
5-minute fetch cadence. This does not change any existing 60-second E2E bound
or promise arbitrary fleet capacity. Decisions and alternatives are recorded
in [operator-floor.html](../reports/1.0/operator-floor.html).

## Files

- `scripts/operator-floor/main.go`: synthetic TLS source and external load driver.
- `scripts/operator-floor/main_test.go`: evidence checker rejects stale status,
  changed values, unintended writes, missing ownership and missing CRs.
- `scripts/ci/operator-floor.sh`: scoped kind lifecycle, chart render, limits,
  workload and fail-closed resource evidence.
- `.github/workflows/operator-floor.yml`: secret-free manual native arm64 gate.
- `internal/operator/client/client.go`: releases the per-reconcile private
  transport's idle connections after closing the response body. The new real
  TLS lifecycle test failed on base and passes with this fix.

## Validation

Local native arm64 Docker/kind run passed September 5, 2026:

- Operator cgroup: CPU `20000 100000`, memory `134217728`, swap `0`,
  peak `76185600` bytes (72.66 MiB), zero OOM and restarts.
- Node cgroup: CPU `400000 100000`, memory `4294967296`, swap `0`.
- Five complete 50-CR phases: 2.60, 3.64, 3.78, 3.73 and 3.68 seconds.
- `GOMAXPROCS=2 go test -p 1 ./internal/operator/... ./scripts/operator-floor`
  passed; scoped vet, ShellCheck, actionlint and whitespace checks passed.
- New real TLS test proved the leak before the fix. Before the fix, non-invasive
  cgroup peak was 134328320 bytes with 64 max-pressure events; the harness
  correctly refused that run. Idle connections have no reuse value across the
  reconciler's discarded private clients.

Evidence is committed under `docs/reports/1.0/evidence/operator-floor/`.
Complete local artifacts are in `artifacts/operator-floor/` in this worktree,
including source diff and image/binary identity. Docker's Linux VM has 2.35 GiB
physical memory; the cgroup cap is 4 GiB, and this is explicitly local evidence,
not a GitHub run or physical Pi calibration.

Keep the obligation open until reviewed candidate CI evidence is available.
The manual operator-floor workflow requires no custody secret. Existing operator
tests and resource limits remain unchanged; the server and backup floor gate
are separate acceptance work. No production clusters were read or changed.

CI integration correction: the existing cache policy allowed only ubuntu-latest and rejected native arm64. The policy now names only the two manual floor workflows as ubuntu-24.04-arm exceptions, forbids shared cache actions there, and retains ordinary GitHub-hosted runner restrictions. Both positive and injected wrong-runner/cache/trigger cases are checked before push.

## Exact-head CI repair, 5 September 2026

At head `b1c8eee30f57fee777bc8413ed1466bac1125096`, test_core job 101219751343 passed all Go tests but failed MCP conformance when Corepack chose newly available pnpm 11.25.0 outside the package pinned to 11.24.0. The workflow now installs inside scripts/mcp-conformance; the launcher enters that package before invoking pnpm. Strict pins and baselines remain unchanged. The launcher builds and owns its server executable directly so failure cleanup does not leave go-run children behind.

Original version mismatch reproduced. Fixed frozen installation and all three conformance scenarios passed under the same Corepack default 11.25.0. An injected tool failure returned 23 and closed the server port; successful completion also closed it. ShellCheck, actionlint, cache-policy fixture and whitespace checks passed. [Decision report](../reports/1.0/mcp-conformance-ci.html) and [evidence](../reports/1.0/mcp-conformance-ci.json). Parent owns review, signing, push and exact-head CI.

Merge with main85e5c133 retained the owned compiled conformance server and owning-package Corepack working directory. Main had already adopted the cwd correction; the compiled process preserves cleanup on failure. No baseline or dependency pin changed.


## RSS acceptance correction, 5 September 2026

The controlling [#77 maintainer acceptance](https://github.com/Hikyo-Org/Hikyo/issues/77#issuecomment-5354008780) names operator **RSS strictly below 128 MiB**. `memory.peak` is the whole cgroup charge, including file cache and kernel memory; it is not process RSS. The [kernel memory interface](https://docs.kernel.org/admin-guide/cgroup-v2.html#memory-interface-files) explicitly permits temporary `memory.max` overshoot. The chart limit remains exactly 128 MiB and CPU remains 200m.

Later runs were rejected by the old cgroup-peak comparison at 134,295,552 and 134,328,320 bytes with roughly 30 MB anonymous memory, 95 MB file cache, zero OOM events and zero restarts. Those failures and original artifacts remain historical evidence. They do not establish an RSS failure or retroactively provide a passing RSS measurement. No chart resource/admission adjustment is included in this correction.

The runner now resolves the exact operator CRI container cgroup, requires its sole process ID, and reads that process's `/proc/PID/status` from the kind node. It checks the PID before/after, process start time, `/hikyo operator` command, executable path and source-built executable SHA-256. The reader runs outside the operator cgroup. `VmHWM` is the process peak RSS; `VmRSS` is the current RSS. Both must be valid positive KiB measurements, current RSS cannot exceed peak, and peak must be strictly below 134,217,728 bytes.

`result.json` retains `operator_memory_peak_bytes` as a cgroup diagnostic and adds `operator_rss_peak_bytes` plus `rss_measurement` identity/provenance. `operator-process.txt`, `operator-memory-stat.txt`, and `resource-verification.json` retain raw process/cgroup evidence and its checked projection. The existing exact CPU/memory/zero-swap limits, single ready process, zero restarts and all five 50-CR phases remain required. Missing, duplicate, malformed or nonzero OOM counters refuse acceptance; absence cannot look like zero.

Implementation adds `scripts/ci/operator-process-capture.sh`, `scripts/operator-floor/resources.go`, and its refusal tests. Coordinated native ARM64 execution passed: peak process RSS 97,161,216 bytes (92.66 MiB), cgroup peak 87,834,624 bytes; all five phases passed in 4.06, 3.65, 2.61, 2.62 and 4.05 seconds. Exact caps, zero swap, explicit zero OOM counters and no restarts passed. Focused Go tests passed in 0.688 seconds; ShellCheck and whitespace checks passed. Parent R1 was CLEAN. Artifact: `/Users/developwent/.codex/artifacts/hikyo-1.0-20260905/operator-rss-correction/native-local/`. This remains dirty-source local rehearsal, not hosted exact-candidate evidence. Parent owns independent review and exact-candidate workflow delivery. See [RSS correction report](../reports/1.0/operator-rss-correction.html).

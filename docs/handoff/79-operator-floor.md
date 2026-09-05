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

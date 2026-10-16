# Benchmark Matrix latency repair

> Historical local artifact names below identify the original run only and are not included in this checkout. Recorded results were not rerun for this path cleanup.

## Confirmed failure

- PR #924 benchmark run [38784589948, attempt 2](https://github.com/Hikyo-Org/Hikyo/actions/runs/38784589948/attempts/2), head `38803eb90b0aeea211c78e5cb41ccda254f87932`: the browser job failed the unchanged 200 ms median ceiling. Desktop save measured 265.0 ms and live update 224.9 ms. All five desktop save samples exceeded the ceiling; the Go walltime job passed.
- Controller run [38784611845](https://github.com/Hikyo-Org/Hikyo/actions/runs/38784611845) waited for that exact authorized attempt, published its result, then returned `PR benchmark failure`. The second email was downstream propagation, not a separate controller timeout.
- The relevant Matrix and benchmark sources were unchanged by PR #924. Both main `c12e7d39` and the PR passed the native local benchmark. Controlled CPU-throttled runs reproduced slow actions on both revisions.

## Cause and change

`useQueries` returns a new result array on each render. Four such arrays invalidated `environmentRows`; Matrix then rebuilt unchanged values, signals, problem calculations, history data and column sizing across the 1,000-key, 20-environment fixture.

The repair supplies stable `combine` callbacks that reuse structurally equal, environment-bound domain states. Matrix also memoizes its environment projection. Existing query keys, requests, status mapping, permission checks, stream invalidations and polling remain intact. HTTP 403 still removes cached material from the exposed row immediately.

No benchmark limit, timeout, dataset, trace setting, retry count or controller admission policy changed.

## Measurements

Same local hardware, production-mode fixture, Chromium, 4x CPU throttling, tracing enabled:

| Action | Main before | Repaired | Change |
| --- | ---: | ---: | ---: |
| Save draft median | 394.9 ms | 249.2 ms | -36.9% |
| Live update median | 334.7 ms | 221.4 ms | -33.8% |
| Filter problems median | 223.3 ms | 80.9 ms | -63.8% |
| Show all median | 254.7 ms | 88.8 ms | -65.1% |

These throttled diagnostic values are not equivalent to GitHub runner measurements and still exceed 200 ms for save/live update. A trace-off baseline measured 393.0/325.1 ms for save/live update, so recording overhead did not explain the main slowdown. One run overlapping another worker's tests was discarded; the final comparison ran without that contention.

The unchanged native desktop/mobile benchmark passed all four tests, including the blocked-input/asynchronous-completion negative controls. Save/live medians were 76.0/73.5 ms desktop and 43.7/40.2 ms mobile. Native results alone do not establish an improvement because the original also passed on this hardware.

## Regression and review evidence

The real-hook regression failed before the fix because an unrelated render recreated identical rows. It passes after the fix and checks payload updates, cached-data denial on HTTP 403, explicitly stale data on HTTP 500, and recovery. Existing reorder/removal, query ownership and stream-recovery tests remain.

Independent Sol 6.1 high review found no actionable issues in the implementation/test change; 36 focused tests passed. Full web validation passed: 198 files, 1,692 tests, typecheck and lint on Node 26.7.0. The hosted benchmark on the repaired revision remains the acceptance boundary.

Full validation also reproduced two existing fixture failures: Node's bundled fetch rejected the installed Undici 8.11 Agent handler ABI. This repair reuses PR #924's identical small fixture correction, importing fetch and Agent from the same package and matching the fixture Response type. Both real-HTTP fixture tests and 31 sensitive-mutation tests passed independently. The Matrix sensitivity inventory pin was refreshed after explicit review; mutation functions and revocation behavior are unchanged.

## Scope and remaining verification

This is a baseline Matrix performance fix, separate from PR #924's CLI-login feature. No changes were made to that PR's branch. No paid benchmark was manually rerun; automatic runs remain subject to the existing budget gate. Local evidence and original logs are retained under `hikyo-benchmark-failures-20261015` in the development workspace.

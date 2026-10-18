# Issue #971: settings route bundles

Issue: https://github.com/Hikyo-Org/Hikyo/issues/971. Implementation base:
`cb78b6be111029335e744aa3e4dcc956d5194ba1`.

## Change

Each of the twelve settings screens now has a direct `React.lazy` import. The
unused shared settings barrel is removed. Vite still extracts shared dependencies.
Auth and workspace groups, route registration, scoped component keys, Suspense,
authorization, CSP, private document caching, immutable assets and precompressed serving are unchanged.

## Production bundle measurements

Node 26.7.0, pnpm 11.24.0, frozen lockfiles, Vite 8.2.2. All sizes below are
bytes, not decimal kB. Gzip and Brotli use the production sidecars (level 9 and
quality 11). A tiny chunk with no smaller sidecar contributes its identity size.

The old `settings-CDxfuDmR.js` is 530,602 raw / 133,617 gzip / 106,617 Brotli
bytes. The production warning disappears without changing its threshold.

The table counts each screen's lazy import entry and its transitive static JS dependencies,
once each. This includes shared application code; it excludes runtime dynamic
imports such as the InstanceConfig workspace wrapper. Browser traces below
measure the actual complete navigation instead.

| Screen | Raw before / after | Gzip before / after | Brotli before / after |
| --- | ---: | ---: | ---: |
| AccountSecurity | 1,409,427 / 743,350 | 385,088 / 211,506 | 323,696 / 181,393 |
| OrgSettings | 1,409,427 / 710,330 | 385,088 / 199,847 | 323,696 / 171,119 |
| ProjectSettings | 1,409,427 / 843,595 | 385,088 / 241,011 | 323,696 / 208,004 |
| Members | 1,409,427 / 796,968 | 385,088 / 227,651 | 323,696 / 195,648 |
| ScimProvisioning | 1,409,427 / 750,200 | 385,088 / 212,743 | 323,696 / 182,394 |
| Audit | 1,409,427 / 707,484 | 385,088 / 199,111 | 323,696 / 170,569 |
| ChangeApprovals | 1,409,427 / 716,912 | 385,088 / 203,115 | 323,696 / 174,072 |
| TemporaryAccess | 1,409,427 / 717,319 | 385,088 / 202,450 | 323,696 / 173,507 |
| MachineAccess | 1,409,427 / 853,882 | 385,088 / 238,808 | 323,696 / 204,262 |
| Adapters | 1,409,427 / 759,206 | 385,088 / 212,625 | 323,696 / 182,393 |
| InstanceAdmin | 1,409,427 / 799,550 | 385,088 / 221,737 | 323,696 / 190,085 |
| InstanceConfig | 1,409,427 / 709,649 | 385,088 / 199,023 | 323,696 / 170,471 |

Before, all twelve lazy loaders reach all twelve settings implementations
through the shared graph. InstanceConfig has a separate emitted chunk, but
is still reached through that shared import boundary. After, each entry
closure contains only its own settings implementation. This is checked
against Vite module ownership, not inferred from third-party library names.

Total emitted JS grows from 13 to 58 files: 1,770,526 to 1,784,293 raw bytes,
484,832 to 508,136 effective gzip bytes, and 408,454 to 438,532 effective Brotli
bytes. The tradeoff is less code per screen at the cost of more requests and
compression overhead across the complete application.

## Browser evidence

Candidate browser-tested source commit: `9855696b54a3a5a430fbdd694b4fc9c4406cbed9`.
Unit/typecheck/lint ran on the identical reviewed index immediately before this
signed commit. The follow-up handoff commit changes only Markdown and JSON.

Chromium 151.0.7922.34; desktop 1280 x 800 (DPR 1),
mobile Pixel 5 emulation 393 x 727 (DPR 2.75). Each screen has five cold browser
contexts in each phase and viewport: 240 measured cold navigations. The real
Go binary serves the embedded production SPA under the repository auth/seed
fixture. API responses are not mocked by these probes. Commands within this
batch ran sequentially with the documented process-local Go resource settings.
Operator-authorized overlapping queue jobs were observed at candidate
batch completion. These timings do not represent isolated host performance.

| Phase / viewport | Start UTC | End UTC |
| --- | --- | --- |
| before / desktop | 2026-10-18T18:48:45.265Z | 2026-10-18T18:51:40.511225+00:00 |
| before / mobile | 2026-10-18T18:51:55.346Z | 2026-10-18T18:55:37.014877+00:00 |
| after / desktop | 2026-10-18T19:41:24.307Z | 2026-10-18T19:44:44.732407+00:00 |
| after / mobile | 2026-10-18T19:44:45.615Z | 2026-10-18T19:48:17.257125+00:00 |

At 19:48 UTC, queue job 5 overlapped operator-started jobs 11 and 14. The
queue observation is not continuous CPU or memory profiling.

The DOM-heading metric runs from the navigation time origin to insertion of
the addressed h1. It is not fully interactive completion. FCP, last JS response,
all five sample totals/timings, and a representative JS resource waterfall per
screen/viewport/phase are in the [measurement data](971-settings-route-bundles.json).
Raw and selected encoded JS bodies were checked against the production inventory
for every sample. Before, all twelve settings implementations are fetched; after,
only the addressed settings implementation is fetched. InstanceConfig still
loads its existing workspace wrapper, which is outside this change.

| Screen | JS requests before / after | Encoded JS body bytes before / after | Desktop heading median ms before / after | Mobile heading median ms before / after |
| --- | ---: | ---: | ---: | ---: |
| AccountSecurity | 9 / 18 | 323,696 / 181,393 | 432.3 / 524.0 | 436.0 / 748.8 |
| OrgSettings | 9 / 13 | 323,696 / 171,119 | 431.8 / 455.8 | 431.6 / 691.7 |
| ProjectSettings | 9 / 17 | 323,696 / 208,004 | 428.5 / 447.0 | 461.7 / 608.5 |
| Members | 9 / 25 | 323,696 / 195,648 | 429.8 / 451.9 | 438.5 / 642.8 |
| ScimProvisioning | 9 / 21 | 323,696 / 182,394 | 431.1 / 456.1 | 429.5 / 650.8 |
| Audit | 9 / 16 | 323,696 / 170,569 | 438.1 / 445.9 | 436.2 / 679.0 |
| ChangeApprovals | 9 / 20 | 323,696 / 174,072 | 441.7 / 447.4 | 452.1 / 691.4 |
| TemporaryAccess | 9 / 19 | 323,696 / 173,507 | 430.5 / 448.6 | 437.7 / 677.2 |
| MachineAccess | 9 / 22 | 323,696 / 204,262 | 432.0 / 443.9 | 444.9 / 669.7 |
| Adapters | 9 / 18 | 323,696 / 182,393 | 436.8 / 444.1 | 434.7 / 707.1 |
| InstanceAdmin | 9 / 21 | 323,696 / 190,085 | 440.0 / 450.2 | 428.9 / 585.3 |
| InstanceConfig | 11 / 31 | 388,569 / 280,399 | 434.6 / 453.3 | 432.3 / 724.7 |

Account settings' measured transfer size is 326,396 before and
186,793 after, with 1,409,427 and 743,350 decoded JS bytes.
Encoded body bytes exclude transfer overhead; raw bytes are not transfer size.
Transfer size is the browser-reported ResourceTiming value, not a packet capture.
All five samples agree on script counts and byte totals.

These are local loopback measurements on a shared ARM64 Mac, without CPU or
network throttling. Browser caches are cold; server/filesystem caches are not
claimed cold. Host activity was not controlled beyond heavy-command admission.
Candidate medians were higher in these runs. More requests and host contention
are both possible contributors; this experiment does not isolate their effects.
The timing samples do not establish a general latency improvement, WAN/mobile
hardware performance, memory improvement, or a deployed-production result.

The implementation follows [React's lazy contract](https://react.dev/reference/react/lazy)
and preserves [Vite's shared async dependencies](https://vite.dev/guide/features#async-chunk-loading-optimization).

## Reproduction

From the repository root with the pinned toolchain, frozen dependencies, and
the host's `agent-heavy` admission command available:

```sh
mkdir -p web/.artifacts
agent-heavy -- pnpm --dir web exec node scripts/measure-settings-bundles.ts .artifacts/settings-bundles.json
agent-heavy -- env GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 GOFLAGS=-p=2 HIKYO_SETTINGS_BUNDLE_SAMPLES=5 pnpm --dir web exec playwright test settings.spec.ts --project=desktop --grep 'cold settings navigation|anonymous settings navigation|settings navigation history'
agent-heavy -- env GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 GOFLAGS=-p=2 HIKYO_SETTINGS_BUNDLE_SAMPLES=5 pnpm --dir web exec playwright test settings.spec.ts --project=mobile --grep 'cold settings navigation|anonymous settings navigation|settings navigation history'
```

The inventory command uses the normal production Vite configuration, demo
isolation check and precompression script. It records portable route ownership,
static/dynamic chunk edges and exact on-disk representation sizes outside the
shipped SPA. Browser JSON attachments contain only JS resource paths, transfer
sizes, resource timing, heading marks and selected static response headers;
session storage and request cookies are excluded.


## Validation and boundaries

- Web typecheck and oxlint with deny-warnings: passed.
- Full web unit project, two workers: Test Files  212 passed (212); Tests  1948 passed (1948).
- Candidate production inventory/build, demo isolation and precompression: passed.
- Go UI binary build with `-p 2`: passed, then used for both candidate browser runs.
- Candidate desktop: 14 passed; mobile: 14 passed. Each includes twelve cold
  screen probes with five samples, anonymous sign-in gating, and navigation/history
  retaining the shell. Static asset compression/cache and document CSP/private
  caching checks passed.
- Baseline desktop: 14 passed. Baseline mobile cold probes and sign-in gate:
  13 passed; the history helper initially timed out because its role locator
  excluded the hidden drawer. `includeHidden` fixed the locator; the focused
  baseline mobile rerun passed. This failure is retained, not relabeled passing.
- Two earlier unit attempts were interrupted: the first invocation did not apply
  its intended worker cap; the capped second run was stopped for host pressure.
  The first baseline browser setup was interrupted for resource coordination.
  Its completed executable/cache was reused for the successful baseline probes.
- Initial Graphify update: interrupted (143 / KeyboardInterrupt), not completed;
  not restarted under the later explicit resource-coordination instruction.
- T3 preview was opened first; navigation during initial server startup timed out.
  The measured flows use CONTRIBUTING.md's production-server Playwright workflow.
- Full Go/isolation/PostgreSQL suites, independent review, hosted CI, deployment
  and memory profiling are not claimed by these frontend-local checks. CI state
  and the final documentation head are reported separately in the PR/chat.

Go validation uses process-local `GOMAXPROCS=2`, `GOMEMLIMIT=512MiB`, `GOGC=50`
and `-p 2`. These controls are not a hard memory guarantee.

Agents remain prohibited from merge, auto-merge, enqueue, release and deployment.
Human-controlled queue entries and merges are left intact.

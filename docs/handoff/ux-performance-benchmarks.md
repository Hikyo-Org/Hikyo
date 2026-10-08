# UX performance benchmark additions

Implemented locally on 8 October 2026. This extends the daily and requested PR
benchmark setup. Live CodSpeed execution remains unverified; activation requires the workflows to be merged first.
No billing settings or account credit were changed.

## Coverage

Four fixed Go cases in `internal/service/ux_benchmark_test.go` cover operations
where a small user action encounters substantial accumulated data:

- `BenchmarkPublishSingleDraftLargeProject`: publish one config draft in a
  1,000-key project with five populated environments and a linked pair of keys.
  The requested environment must advance exactly one revision and the selected
  value must actually change. Unselected environments are fixture context;
  this does not measure publishing across all five at once.
- `BenchmarkImportMixedExisting`: import 1,000 existing names, explicitly
  overwrite 200 (including 40 secrets), and skip 800. A synthetic, non-live AWS
  candidate in one config entry exercises the real warning scanner. Checks
  require successful writes, the expected counts, and a scanning finding.
- `BenchmarkHistoryPageLongLived`: fetch the newest 20 revisions after 1,024
  real publishes on a 16-key project. Checks enforce the expected newest
  revision and descending page boundary. The smaller catalogue keeps history
  construction cheap without hiding lifetime-history growth.
- `BenchmarkRevisionDiffSparseLargeSnapshot`: compare two retained 1,000-key
  snapshots with one changed config value. Checks enforce the full result
  cardinality and the exact changed key and value. Secrets remain masked.

These use real authorization, disk-backed SQLite, encryption, validation and
production transactions. External adapters, rate/concurrency budgets,
notification delivery, PostgreSQL, real login/session admission and concurrent
load remain outside these service cases.

Write benchmarks checkpoint the fixture WAL, close the datastore and capture
its encrypted baseline. Each iteration restores that same file, reopens it with
its **original signed admission**, reloads the keyring with a disposable root
copy and warms reads. Restoration and post-write validation are untimed; the
operation's durable transaction commit is timed. Repeated samples cannot
accumulate history, consume the 100-draft limit, or silently become no-ops.
Read fixtures remain unchanged during measurement.

The native Go harness reports allocations. The configured CodSpeed lane reports
walltime; fixture construction and restoration still consume runner minutes.

## Browser check

`web/e2e/performance.config.ts` builds a dedicated production-mode bundle using
the application's Vite configuration, styles, fonts, Shell, Matrix, query hooks,
Zod parsing, virtualizer, editor and advisory invalidation logic. It writes to
`web/test-results/matrix-performance/app`, leaving the embedded application
bundle untouched.

Only API transport is synthetic. The fixed dataset has **1,000 keys across 20
environments**, 20 percent secrets, ten folder groups, and 100 missing required
keys in one environment. Unexpected API requests and browser exceptions fail.
The live update is a real metadata SSE event from the synthetic transport, which
must cause the production hooks to refetch and render the changed config value.

Desktop Chromium and an emulated Pixel 5 each exercise six actions: Problems
filter, Show all, open a cell editor, type a short value, save one draft, and
receive a live update. One cycle warms the screen; five further cycles supply
median and maximum duration for each action. Every save and update uses new
material. Mobile emulation covers viewport and touch behavior, not physical
phone CPU performance.

Timing starts at a trusted browser input and ends when the expected DOM state
is present followed by another animation frame. This is a laboratory rendering
proxy, **not field INP or proof of pixel presentation**. Event Timing samples are
also retained when supported, without calling them INP. A deliberate 120 ms
input handler plus 100 ms asynchronous completion validates that the sensor
includes both delays.

The initial guard requires each action's median below **200 ms** and every
measured sample below **500 ms**. These are provisional lab ceilings, not a
measured hosted-runner baseline or a production SLO. JSON attachments include
all samples, summaries and Event Timing entries; failed tests retain traces.

## CI and allowance

The existing Go package selection automatically discovers the four new cases.
The budget ledger, 15-minute Macro timeout and conservative 600-minute allowance
remain unchanged. Actual Macro duration and account access still require a
post-merge run.

A reusable `.github/workflows/matrix-performance.yml` runs the browser check
on GitHub-hosted Chromium with read-only contents permission, no secrets and no
shared-cache writes. Daily/main manual runs call it independently of the Macro
allowance. An admitted PR checkbox request calls it for the same exact head as
the Go suite. A declined request starts neither requested measurement.

Browser results are retained in `matrix-performance-RUN_ID-RUN_ATTEMPT` artifacts
for 14 days. The linked Actions run contains both the CodSpeed job and the browser
job. Browser execution adds GitHub Actions usage, **zero CodSpeed Macro minutes**.
The bot comment explains both checks. Existing required CI remains separate.

## Local evidence

On this Apple M2 Pro, three native rounds at `-benchtime=200ms` measured:

- Publish: 27.1 to 27.8 ms per operation.
- Mixed import: 57.7 to 58.4 ms per operation.
- History page: 0.426 to 0.440 ms per operation.
- Sparse revision diff: 7.16 to 7.33 ms per operation.

The four-case repeated command took **43.1 seconds including fixture setup**.
The complete five-package, one-iteration smoke command took **36.6 seconds**
while other local checks were running. These are diagnostic laptop measurements,
not estimates of cold CI build time or Macro charges.

The desktop/mobile browser suite, including the sensor controls, passed locally
in roughly 15 seconds including its production build. Measured action medians
were approximately 17 to 57 ms; the largest sample was below 70 ms.

Local validation passed: the complete native benchmark smoke, repeated new
benchmarks, selected shared-fixture service tests with the race detector, Go
vet, web typechecking, Oxlint, all 1,511 web unit tests, four browser checks,
controller/workflow regression tests with the race detector, Actionlint,
runner/cache and trusted-CI policy checks, and whitespace checks. The web unit
suite emitted connection-refused diagnostics for its unavailable local port
3000 backend while returning success. The browser harness had no unmatched
requests or page errors. Graphify structural output was refreshed.

Remote CI, bot posting, CodSpeed uploads and actual hosted-runner timing remain
unverified. Local validation does not establish live CI or hosted-runner results.

## Operator commands

Run from the repository root:

```sh
go test -run '^$' -bench . -benchtime=1x \
  ./internal/crypto/ ./internal/dotenv/ ./internal/scanning/ \
  ./internal/server/ ./internal/service/
go test ./internal/service -run '^$' \
  -bench 'Benchmark(PublishSingleDraftLargeProject|ImportMixedExisting|HistoryPageLongLived|RevisionDiffSparseLargeSnapshot)$' \
  -benchtime=200ms -count=3
pnpm --dir clients/ts install --frozen-lockfile
pnpm --dir web install --frozen-lockfile
pnpm --dir web exec playwright install chromium
pnpm --dir web exec playwright test --config e2e/performance.config.ts
```

Expected results are successful Go benchmark output and four passed Playwright
checks. CI already supplies Chromium through the pinned Playwright image.
Native Go flags above are for local measurement; CodSpeed still uses only
`go test -bench=.` with its selected package paths.

See [daily and requested PR setup](codspeed-daily-pr-benchmarks.md) for activation,
credit opt-in, trust boundaries and allowance accounting. References:
[CodSpeed Go integration](https://codspeed.io/docs/benchmarks/go) and
[Google's INP definition](https://web.dev/articles/inp).

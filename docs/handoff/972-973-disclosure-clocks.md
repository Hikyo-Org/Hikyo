# #972 and #973: disclosure clocks

Issues: <https://github.com/Hikyo-Org/Hikyo/issues/972> and
<https://github.com/Hikyo-Org/Hikyo/issues/973>.
Baseline: `cb78b6be111029335e744aa3e4dcc956d5194ba1`.
Authorized endpoint: implement, validate, signed+DCO commit, push and PR.
Agent merge, queue admission, auto-merge, release and deployment are explicitly
held. Human queue entries and manual merges remain under the user's control.

## Behavior and boundaries

`Values.tsx` installs one 250 ms clock only while disclosures exist or a live,
timed reveal guard has a future deadline. Locked, single-decision and expired
windows with fully masked values do not keep a periodic clock. Blur and
visibility remasking retain their independent listeners. A live timed guard
can keep counting down after focus loss removes the plaintext. The clock stops
when the last deadline is no longer active, and cleans up on unmount.

`RevisionDiff.tsx` installs its one-second clock only while a disclosure exists.
It refreshes the clock sample when revealing, so a long idle period does not
inflate the initial 30-second countdown. Comparison loading and focus/visibility
listeners remain in their mount effect, separate from disclosure-dependent
timer setup and cleanup. Manual mask, expiry, focus loss and unmount stop the
clock.

Both surfaces retain their absolute wall-clock deadlines and the existing
ceremony/session/focus fences. Delayed callbacks read current wall time rather
than accumulating elapsed ticks. Values retains its one-shot accessibility
announcement and visual-only countdown. Server authorization, audit behavior,
clipboard behavior and MatrixRowEditor are unchanged. The permission-model ADR's
reveal guard and client-remasking obligations remain the contract; no ADR or API
amendment is needed. No generated API boundary changed. Both changed route
modules were read in full before refreshing their two hashes and custody note
in the sensitivity inventory.

## Validation evidence

The new focused tests were run against unchanged production source first:
13 tests failed and 13 tests passed. Failures confirmed an idle timer,
surviving timers after clearing and RevisionDiff's stale initial clock sample
(90 seconds after a simulated minute of idle time).

After the fix, both focused files passed all 29 tests. A final focused run
including the sensitivity inventory passed all 31 tests on pinned Node 26.7.0
and pnpm 11.24.0. Coverage includes locked and single-decision idle states, reveal-one/reveal-all countdowns, normal and
delayed expiry, timed guard countdown and expiry, focus clearing and return,
manual revision masking, unmount cleanup, and exact comparison/reveal request
counts. Existing ceremony-task and session-retirement tests are preserved.

```sh
pnpm --dir clients/ts install --frozen-lockfile
pnpm --dir web install --frozen-lockfile
pnpm --dir web exec vitest run --project unit src/routes/Values.ceremony-task.test.tsx src/routes/RevisionDiff.test.tsx --maxWorkers=2
```

The first full unit run completed 212 files and 1,965 tests: 1,964 passed and
one inventory test failed on the stale hashes for exactly the two edited
routes. The source review above preceded refreshing those pins; no inventory test or matching rule was relaxed. Final web checks and
browser evidence follow.

Pinned Node 26.7.0 / pnpm 11.24.0 checks completed:

```sh
GOMAXPROCS=2 pnpm --dir web run typecheck
GOMAXPROCS=2 pnpm --dir web run lint
GOMAXPROCS=2 pnpm --dir web run build
```

All three passed. Lint first rejected an unnecessary synchronous Values clock
refresh inside the timer effect. It was removed, preserving the existing clock
refresh in disclosure actions; the final lint and focused tests pass without
suppressions or configuration changes. The production build's existing large
chunk warning remains advisory; demo isolation and asset precompression pass.

T3 preview was attempted first. Navigation and status failed with
`Preview automation client server-browser disconnected during navigate.` and
`Preview automation client server-browser disconnected during status.` A
recovery open failed with `Preview automation open timed out after 660000ms.`
The prototype HTTP endpoint returned 200, which is reachability evidence only.
A later metadata-only T3 status retry succeeded after resource coordination;
that recovery is not browser-flow evidence. The prototype server and its two
owned tabs were closed to release resources while waiting for validation.

The first full unit run above completed with only the two stale route pins
failing its inventory test. The subsequent final-tree full run and the local
Go UI binary build were interrupted at the user's resource-coordination request.
They are incomplete, not passing evidence. Their logs and build caches were
preserved. The initial manual-mutex wait never acquired ownership and was
replaced by the user's shared `agent-heavy` queue. Remaining validation uses
one finite sequential batch through that outer wrapper, with no nested lock
or wrapper. Go jobs use ephemeral `GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50`
and `-p 2`, Vitest uses at most two workers, and browser flows use one worker.
Repository CI and policy are unchanged.

The first global-queue batch completed the full unit suite with 1,964 passes
and one fixture failure: its prebuilt IdP variable pointed to a helper that
had not yet been built. This was an orchestration ordering error. Its log is
retained; the corrected batch builds all referenced helpers before testing.
No product code, fixture tests, timeouts or coverage were changed for it.

The corrected global-queue batch completed both fixture helper builds and the
Go UI binary build, then passed the full final-tree unit suite: 212 files and
1,965 tests, zero failures. The run printed nonfatal connection-reset and Node
warnings; Vitest exited zero. Sensitivity inventory and both changed route
test files passed in this full run.

```sh
pnpm --dir web run test --maxWorkers=2
```

The documented Playwright fallback passed all five targeted flows on desktop
and all five on mobile
against the built Go server and embedded production SPA: retained-revision
comparison and selected-secret disclosure, purpose-bound reveal ceremony,
automatic remasking, live-window prompt gating and navigation remasking.
Desktop used one Chromium worker at 1280 by 800; mobile used the Pixel 5 project.
Each project ran separately with one worker. Both runs were filtered, so their
full flow-registry execution checks were intentionally skipped; no complete
browser-suite closure is claimed. The finite global-queue batch exited zero
after all six build, unit and browser steps passed.

```sh
pnpm --dir web exec playwright test e2e/flows/reveal.spec.ts e2e/flows/history.spec.ts --project=desktop --workers=1 --grep 'a reveal takes|the value re-masks on its own|the window gates the prompt|navigating to another environment re-masks|compares retained revisions'
pnpm --dir web exec playwright test e2e/flows/reveal.spec.ts e2e/flows/history.spec.ts --project=mobile --workers=1 --grep 'a reveal takes|the value re-masks on its own|the window gates the prompt|navigating to another environment re-masks|compares retained revisions'
```

Final self-review against both tickets found no remaining implementation
findings. Timer ownership is bounded to one scheduler per surface; comparison
requests and focus listeners retain their independent lifetimes. Regression
tests verify interval cleanup and request counts without relaxing existing
ceremony or session tests. The final diff contains only the two route changes,
their tests, their two sensitivity pins and this handoff. Independent review
has not been performed.

Graphify's initial whole-repository update was interrupted after AST extraction
without producing a current graph. Its one-worker, no-clustering retry was also
stopped under resource coordination. Structural refresh is not completed; no
current graph or documentary semantic coverage is claimed. It is not restarted
unnecessarily.

## Limits and remaining gates

Timer instrumentation establishes scheduling and deadline behavior. No
production CPU, battery, latency or performance improvement was measured.
Independent review, hosted exact-head CI and merge remain separate evidence.
No backend, PostgreSQL or generated-code tests are required by this TypeScript
route-only change; no backend, schema, API or generated files were modified.

# Project directory query states (#932)

Overview and the desktop/mobile Shell project lists distinguish loading, failed,
successful-empty and successful-nonempty reads. A failed read shows an alert and
a native Retry projects button. Loading and failure do not suggest creation or
expose retained project names. A successful single-project read still redirects
to its matrix; multiple projects retain the existing list.

## Cache contract

`useProjects` uses the canonical `metadataQueryOptions` helper. HTTP 401, 403 and
404 retire cached metadata to null. A subsequent network failure preserves that
marker; a successful read replaces it. An aborted old request cannot retire a
newer successful result. Null means unavailable, never an authoritative empty
topology. All project-list consumers accept the nullable result. Project
creation invalidates the query instead of writing over the retirement marker.

The original candidate copied the shared helper, environment options and
nullable consumers from #948 commit
`728e347d84d3ebe7a44fff70c04ff5f3b3a8d9a3`. Source integration now contains full
#948 head `3a331d2d69ec726cf20555c94bd4916774bb3b9b`, including #940 head
`ed908f0013fdbe5139388852072eeae387caf354` and the file-target feature, plus
#807 head `03ad8f0243fc0b2ee75fb138a507513ada8c9714`. Settings retains both
project guards and canonical environment retirement. ProjectSettings retains
nullable environment guards alongside the developer credential panel. #807
does not change MachineAccess. Dependency presence here does not establish that
those PRs have merged to main.

## Original candidate validation

These results belong to candidate
`c90eee1ce8d5607ee7ca5fdb0c77b4bb6003e8f1`, before full dependency integration.
They are retained as historical acceptance, not proof of the combined source.

- Real-hook and QueryClient regressions: the corrected original-source baseline
  failed 19 of 24 cases; the implementation passes all 24. Coverage includes
  initial loading/500, retry, true empty, redirect/list, denied stale metadata,
  failed retry after denial, organisation changes, remote mode and cancelled
  old requests finishing after a new successful read.
- Removing only the helper's abort guard makes both late-old-403 regressions
  fail. Restoring it passes the directory and sensitive-mutation suites, 55 tests.
- Full web unit suite: 1,793 tests across 205 files pass with one worker and a
  source-compatible prebuilt OIDC fixture. Typecheck, lint, design checks and
  production bundle pass. Final E2E source edits also pass typecheck and lint.
- The bundle was embedded with `GOMAXPROCS=2 go build -p 1 -tags ui`.
- Repository Playwright shell flow: desktop and mobile each pass 25 tests, with
  two viewport-specific skips. Both new resilience cases also pass visual
  capture runs. Keyboard retry, focus/touch/contrast checks and axe assertions
  pass. Recovery reads the real seeded server list; its two-project fixture
  exercises the list. Single-project navigation is covered by real-hook tests.
- Original harness, typecheck and sensitivity-inventory failures are retained
  in the external evidence. The inventory hashes changed only after source,
  schema, lifetime and sensitive-mutation review.

Browser launches supplied prebuilt UI and OIDC binaries but omitted
`HIKYO_E2E_MAIL_BINARY`. The fixture therefore compiled its mail helper during
each of four launches. Those runs were not free of implicit Go compilation.
Future resource-isolated runs must supply all three documented prebuilt binary
environment variables after source compatibility checks.

## Integration and delivery gates

The full dependencies have been integrated locally under a source-only grant.
Signature/DCO and source-fidelity checks passed. Independent full-file sensitive
module review precedes pin reconciliation; the merged settings hash is the only
new source pin. Canonical dependency pins remain unchanged. The environment
retirement effect returns when `listing.data === null` before dereferencing or
purging caches. The project directory's cancellation and status guards remain.

Combined validation ran on clean source
`071598053bf5d1656d172e8d338a767d7c622e30` with Node 26.7.0 and pnpm 11.24.0:

- Affected regressions: 249 tests across 17 files pass with one worker.
- Canonical client generation produces no diff; client typecheck and 37 tests
  pass. The verify components run in canonical order, with test concurrency 1.
- Web typecheck, lint and design checks pass. Full units pass all 1,909 tests
  across 209 files with one worker and the compatible prebuilt OIDC fixture.
- Production bundle, demo isolation and precompression pass. Its 53 files are
  unchanged before and after the single `GOMAXPROCS=2`, `-p 1` embedded UI build.
  Build metadata records this revision with `vcs.modified=false`.
- Desktop and mobile each pass 28 browser tests with two viewport-specific
  skips. Both run sequentially with one worker and explicit source-compatible
  prebuilt UI, OIDC and mail binaries, avoiding implicit Go compilation.
- Actual combined flows cover the complete Shell suite, project query states
  and recovery, developer-policy expiry clamp/restore/revocation and permission
  denial, and file-target report/conflict/credential-binding/deletion behavior.
  The filtered run does not claim whole-suite flow-registry execution closure.
- Desktop/mobile captures were inspected. Project states and focused Retry fit
  both viewports. The mobile credential table scrolls horizontally; the first
  capture does not show every column, while the live revoke interaction passes.
  The mobile file-target completion capture is above the deletion message;
  live assertions establish deletion. These capture limits are retained.

No source repair was needed during combined validation. Original failures and
candidate evidence remain preserved. No unfinalized #820 feature or shared
assertion patch is included. Node, Go and browser lanes were explicitly released
when their handles completed; owned browser listeners were confirmed closed.

## Rebase onto main (2026-10-17)

The branch was replayed onto main
`b11e86c40606873b52506445ac28d8a845a2a097`. The pre-rebase head
`464d75b3294f6aef1fabce341ea7ab64cdb77e9b` is preserved as
`recovery/pr-954-pre-rebase-20261017`. The dependency heads named above
(`3a331d2d6` for #948, `03ad8f024` for #807) are no longer ancestors of the
branch: their content is replayed in the linear order #932, #948, #807, and the
merge commits that had integrated #940/#931 collapse into that order. Main had
already merged #940/#931 and has since extended `api/matrix.ts` and
`routes/Matrix.tsx`; the replayed commits do not touch those modules, so the
rebase keeps main's revision.

Conflicts and their resolutions:

- `web/src/api/sensitiveInventory.json`: both prose logs are kept and every pin
  was checked against the replayed source. `api/settings.ts` is the only module
  changed on both sides; it was re-merged with main's environment guard, and its
  pin is refreshed to the merged revision. The other 63 pins still match the
  sources the branch reviewed.
- `web/src/api/settings.ts`: resolved to the branch's merged revision, which
  already carries main's environment guard and retirement behaviour.
- `docs/handoff/932-project-query-states.md`: this accepted integration record
  replaces the earlier "not yet integrated" note.

Deterministic checks re-run on the replayed tree, with Node 26.7.0:

- `web` typecheck and lint pass.
- The `web` unit suite passes all 1,911 tests across 209 files.

Ancestry is now linear on current main, so the mergeability gate no longer
depends on sibling PRs' merge status. The earlier combined-validation evidence
above still describes the same application source for every file except the
re-merged `api/settings.ts` pin.

Required CI, current-head review, protected merge and verified main ancestry
remain separate gates. Local logs and offline screenshot reports are external
chat deliverables and are not committed.

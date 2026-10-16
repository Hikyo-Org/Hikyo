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

No combined Node, Go or browser checks have run yet. Run affected regressions,
client verification, typecheck/lint, full units and bundle under the allocated
lane, then compile and exercise the actual combined UI under separate grants.
Do not push the integration before its allocated combined checks pass. Preserve
original candidate logs and all new failures. No unfinalized #820 feature or
shared assertion patch is included in this integration.

Required CI, current-head review, protected merge and verified main ancestry
remain separate gates. Local logs and offline screenshot reports are external
chat deliverables and are not committed.

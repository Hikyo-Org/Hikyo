# Storybook setup handoff

Completed 2026-09-14 on `t3code/set-up-storybook`. Scaffolded with
`npm create storybook@latest` (Storybook 10.6.0, `@storybook/react-vite`), then
the story-authoring pass from `storybook skills setup`.

## What landed

- **Runner split.** Two named vitest projects in `web/vite.config.ts`: `unit`
  (browserless, the CI gate, `pnpm run test` = `vitest run --project unit`) and
  `storybook` (chromium via `@vitest/browser-playwright`, opt-in,
  `pnpm run test-storybook` = `vitest run --project storybook`). The frozen
  `test` script and `scripts/ci/build-spa.sh` were left untouched; the SPA verify
  path is unchanged.
- **Addons:** `@storybook/addon-vitest`, `-a11y`, `-docs`, `-mcp` (local MCP
  server). `@chromatic-com/storybook` was dropped, it is a SaaS addon, inert
  without a Chromatic account, and the project self-hosts. Zero lockfile refs
  remain.
- **Stories (colocated in `src/routes/`, prop-driven, no API/provider harness):**
  `Sections.stories.tsx` (Panel variants, JumpIndex, Alert, Done, Explain,
  DisplayOnceCopy, ConsequencesDialog native `<dialog>`, and the TypedNameConfirm
  type-to-arm interaction play), `ProviderDiscoveryAlert`, `OpsDiagnosticBanners`
  (`ChromeDiagnostic` severities), `RetentionBoundsFields` (days/exact/absent),
  `Placeholder` (`NotFound`). Second pass added six pure dialog/control
  components: `ScanBlockDialog`, `ScanWarnDialog`, `FolderCleanupDialog`,
  `MatrixKeyCreate`, `InviteDialog`, `ChromeIdentityControls`. Two component quirks worth knowing: `ChromeIdentityControls` only
  renders its interactive hue/glyph/upload controls under
  `import.meta.env.MODE === 'prototype'` (never true in Storybook's Vite mode),
  so its stories exercise the read-only branch only; `InviteDialog` submits
  through the real `inviteMember` API (not a prop), so its play fills the form
  but never clicks Invite, a real screen harness (see "Not done" below) is what
  that would need.
- **Real-screen harness + four flagship screens (`.storybook/withApp.tsx`).**
  Data-driven route pages run their real hooks against stubbed transport. A story
  opts in via `parameters.app` (`AppParameters`): `auth: true` mounts the real
  `AuthProvider` (answering whoami from `identity`); `path`/`routePath` set the
  `MemoryRouter` entry + `Route` pattern so `useParams` resolves; `outlet` feeds
  `useOutletContext`; `responses: [{ url, method?, status?, body? }]` is a canned
  API table installed on `globalThis.fetch` by the preview `beforeEach`
  (`installAppFetch`) before the screen mounts. There is **no transport provider
  to inject**, `useTransport` falls back to `globalThis.fetch`, and
  `vi.stubGlobal` does not exist in the Storybook dev UI, so the harness
  reassigns `globalThis.fetch` directly (works in both dev UI and the vitest
  browser run). URL match is **exact pathname** for strings (no prefix-swallow),
  RegExp `.test` on the href otherwise; an unmatched request 404s **fail-loud**
  rather than hanging. Bodies are JSON-serialized and Zod-parsed by the screen,
  so each is typed `satisfies z.infer<typeof zXxx>` from `@hikyo/zod`, tsc
  catches shape drift before the 4s chromium run. Two harness correctness
  points: it reuses the app's own `makeQueryClient` via `useState` (the theme
  toolbar re-runs every decorator, and a fresh client there would refetch
  mid-story); and the whoami row is appended last so a story can override it
  (Login supplies its own 401). No storage teardown is needed, AuthProvider's
  session-change fence (`sessionEpoch.ts`) keeps its `blocked` state in a
  module `let` that `installSessionFence` resets to `false` on every mount, so a
  401 story's blocked state cannot leak into a later 200 story; the fence's
  `hikyo-session-change` key is a write-only cross-tab notification, never read
  on mount. Screens storied:
  **`Login`** (logged-out; whoami 401 + auth methods; WithProviders/LocalOnly),
  **`Projects`** (outlet `activeOrgId`; Empty/Populated/LoadError-500; a
  non-operator identity disables the `useInSystemScope` config fetch),
  **`ProjectSettings`** (`useParams({org,project})`; five load-time GETs mirrored
  from `ProjectSettings.test.tsx`; Administrable/MemberWithEnvironments/LoadError),
  **`Members`** (`useParams().org` + auth; org/grants/projects GETs;
  Populated/LoadError-500). Adding another screen is now a new `*.stories.tsx`
  with its own `parameters.app`, no harness change. What was skipped is the
  **full `Matrix` grid screen** (`Matrix.tsx`), not the whole Matrix feature:
  20+ hooks, `useWorkspaceContext` (no provider in the harness), and a
  `useVirtualizer` that renders zero rows in an unmeasured container. Matrix
  decomposes into subcomponents that vary in storiability: `MatrixKeyCreate` is
  **already storied** (prop-driven dialog, above). `MatrixPublishSheet` and
  `MatrixRowEditor` are **now storied too** (`MatrixPublishSheet.stories.tsx`
  Default/Blocked/Protected; `MatrixRowEditor.stories.tsx` Default/Edits).
  `MatrixPublishSheet` is prop-driven in its own signature and calls
  `useProtectedPublishCeremony`, but that hook's only render-time call is
  `useTransport()`, which is null-safe (`transport.tsx:86` returns `{}` when
  `WorkspaceContext` is absent), and its transport fetches
  (`fetchApprovalCeremony`/`fetchRevealWindow`) fire only from `run()`, invoked
  by the sheet's publish handler, not on mount. So its static states render with
  **plain props, no `parameters.app`** (the `withApp` decorator no-ops without
  it); the `Protected` play stops at enabling Publish rather than clicking, since
  clicking would open the ceremony and fetch. `MatrixRowEditor` calls
  `useWorkspaceContext` + `useTransport` and links via `generatePath`/`<Link>`,
  so it needs `parameters.app` purely for the **QueryClientProvider + Router**,
  with an **empty `responses` table**, because a config key (classification
  `config`) never opens a reveal window; a secret-key story would add a
  reveal-window row. `MatrixCell`/`MatrixLegend` are internal (`function`, not
  exported) and need grid row data, so storying them means exporting + fixture
  work.
- **`.storybook/preview.tsx`** imports the same five font/token/app CSS entries as
  `src/main.tsx`, in the same order. A **theme toolbar** (`globalTypes.theme`,
  light/dark) drives `data-theme` via a decorator; `initialGlobals.theme` is
  `dark` (the app's CSS default), so a11y contrast and the vitest browser run,
  which never touches the toolbar, stay on the real default. **`tags:
  ['autodocs']`** at preview level gives every component a generated Docs page.
  Pure components need no providers; the real screens (the four flagship ones
  plus `MatrixRowEditor`) get theirs from the `withApp` decorator (above), keyed
  off `parameters.app`.
- **CssCheck** (one, in `OpsDiagnosticBanners.stories.tsx`): asserts
  `getComputedStyle('.retention-warning').borderRadius === '6px'` (from
  `--radius-container`), proving tokens.css + app.css actually loaded. Chosen over
  an OKLCH colour literal, which Chromium serializes as-authored and which flaps
  between light/dark.
- **a11y is a real gate.** `preview.tsx` sets `a11y.test: 'error'` (not the
  scaffold's `'todo'`): axe runs per story in the vitest browser run and fails on
  a violation. Proven live, a temporary no-name `<button/>` probe story turned
  the run red with axe `button-name`; removed after. All stories pass clean,
  including the four real screens (no component needed an axe fix).
- **CI wiring.** A standalone `storybook` job (own `playwright:v1.62.1-noble`
  container, `needs: [changes]`, `if: plan.web`) runs `pnpm run test-storybook`.
  It is NOT piggybacked on the `web` job: the frozen
  `check-build-artifact-reuse` guard forbids a browser shard in the
  `web`→`web-closure` block from repeating app-build frontend work, and its
  unanchored `pnpm run (typecheck|test|build)` regex matches `test-storybook` as
  a substring, so the step had to leave that block entirely. The job carries no
  app-build artifact / chmod / viewport (stories are theme- and prop-driven).
  - **Governance.** The repo's `ci-job-registry.json` + `TestCIJobRegistry`
    (in `supply-chain-checks`) enforce two invariants: every ci.yml job must be
    registered (completeness), and every registered job must be enforced,
    directly (in `ci-required.needs`) or indirectly (a required job `needs:` it).
    There is no "registered-but-unenforced" state, so B1 ("standalone now,
    register later") was impossible. A *directly*-required new job also can't be
    introduced in its own PR: `ci-required` reads the registry from `BASE_SHA`,
    so head-only `ci-required.needs` additions fail the base-pinned check
    (anti-self-authorization). Resolution: register `storybook` as
    `required_gate: indirect`, `plan_jobs: [web]`, and add it to
    `web-closure`'s `needs` (the same pattern as `app-build`→`web`). A red story
    fails `web-closure` → fails `ci-required`, enforcing stories from this PR on
    without touching `ci-required.needs`. No follow-up on main is required.
  - Story edits under `web/**` already select the `web` class in
    `scripts/ci/classify-changed-paths.sh`, so the job fires on story changes.
- Dropped the `../src/**/*.mdx` stories glob from `main.ts` (no `.mdx` docs
  exist; it only printed a "No story files found" warning). autodocs is
  tag-driven and unaffected. Demo `src/stories/` scaffold deleted. `.gitignore`
  gained `storybook-static` + `*storybook.log`.

## Primitives layer (`src/ui/`, Storybook-first)

Decision **B** on the follow-up ("build primitives layer, start with storybook,
fine-tune the design before moving everything over"). A thin React wrapper per
control, **emitting the exact CSS classes the screens already hand-write**, so
the design tunes in isolation now and the later app migration is a grep-and-swap,
not a re-style. One new rule only, `.menu--pop` (positioning for the Menu
primitive, no radius/colour literals), so the Playwright token assertions
(`e2e/fixtures/assertions.ts`) keep passing; every other class already exists in
`app.css`. No new dependency: a 3-line `cx.ts` joins classes (clsx would be a
package for that).
React 19, `ref` is a plain prop, no `forwardRef`; native `type`/`ref`/events
pass through, so behaviour is identical to the raw element.

Migration map (class → component), all co-located with a `*.stories.tsx`
(`tags: ['ai-generated']`, an `AllStates`/`AllVariants` story per primitive for
side-by-side design review under the theme toolbar, plus one interaction `play`):

| Component | Emits | Notes |
|---|---|---|
| `Button` | `btn` / `btn--primary` / `btn--danger` / `btn--quiet` / `btn--icon` | `type` not defaulted (native pass-through); `icon` variant makes `aria-label` **required in the type** so the a11y gate can't be tripped by an unnamed icon button |
| `Input` | `.field` + `<label>` + `<input>` | label required + wired via `useId`; `className` reaches the wrapper for `field--inline`/`field--readonly` |
| `Checkbox` | `.field.chk` (input then label) | box drawn by `src/ui/ui.css`, input stays the hit target (24px fine, 44px coarse); `type` locked to `checkbox` |
| `Select` | `.field` + `<label>` + native `<select>` | options are children; native control keeps platform/keyboard behaviour |
| `Badge` | `badge` / `badge--danger` / `badge--warn` / `badge--changed` / `badge--ok` (+ `mono`) | colour only echoes a state the child word already names |
| `Menu` / `MenuItem` | `menu` / `menu--pop` / `menu__item` | native Popover API (`popover="auto"` + `popoverTarget`): top-layer stacking, light-dismiss, Escape, and an implicit anchor for free, no open/close state, no outside-click listener (unlike the account menu in `Shell.tsx`). `MenuItem` runs its `onClick` then `hidePopover()`s. `.menu--pop` anchors the panel under the trigger via `position-area`, `@supports`-guarded (below) |

**Already components, not duplicated:** the toast (`src/app/notifications.tsx`
`ToastViewport` + its module store) and `Alert` (`Sections.tsx`) already exist.
Toast got a story only, `notifications.stories.tsx` drives the real store
(publish on mount, `clearNotification` on unmount so a tone can't leak to the
next story), covering all three tones + the update action + a dismiss play.
`Alert` was already storied in `Sections.stories.tsx`. Building a second
component for either would be churn.

**Not migrated yet (deliberate):** no `src/routes/` consumer was touched, "fine
tune before moving everything over". Swapping screens onto these primitives is
the next, separate pass. `Shell.tsx`'s bespoke account menu (`useState` +
`useEffect` outside-click/blur/Escape) is the prime `Menu` migration candidate,
but it anchors to the **right** of the trigger (sidebar), not below, so it needs
a side variant (e.g. `position-area: right span-bottom`) rather than
`.menu--pop`'s `bottom span-right`, and its rows include a `NavLink` that
`MenuItem` (a `<button>`) can't emit yet; fold both into that migration, don't
retrofit now. Open design questions / not-yet-expressible for Marc:
(a) the action/overflow **`Menu`** is now built (decision **B**, native Popover
API), `Menu` for actions, `Select` for native form selection, no overlap;
(b) an `Input` without a visible label
isn't expressible (label is required), matches every current field, revisit if a
search box needs a placeholder-only field; (c) a **read-only field with the
`field__readonly-tag`** isn't expressible either, `app.css` (L157-161) styles
that only via `.field--readonly` + the tag inside the label, which `Input` can't
emit yet. Bare `readOnly` gets no visual treatment, so the primitive omits it;
add the tag variant in the migration pass if a consumer needs it.

`Menu` positioning ceiling: `.menu--pop` uses physical `position-area: bottom
span-right` (left-aligned, opens down-right), not the logical
`span-inline-start`, the logical keyword is unimplemented even in Chrome 153
(verified: `CSS.supports('position-area','bottom span-inline-start')` is
`false`). The anchored placement is wrapped in `@supports (position-area:
bottom span-right)`; the **base** `.menu--pop` rule sets `position: fixed;
inset: 0; margin: auto` so a browser without anchor positioning (Firefox as of
2026) gets a centred top-layer panel. The base rule is required, not cosmetic:
without it the `@supports` block contributes nothing on Firefox and the shared
`.menu` account-menu offset (`left: calc(100% + 8px)`) wins the cascade, shoving
the panel one panel-width past the right viewport edge (off-screen, not
centred). Both paths verified in Chrome 153: anchored path panel top = trigger
bottom + 4px, left edges aligned; fallback path (with the `@supports` rule
deleted at runtime) `getBoundingClientRect().x` = `(innerWidth − width) / 2`,
i.e. centred.

Closed-state footgun (found + fixed this pass): `.menu` sets `display: flex`,
and an author `display` beats the UA `[popover]:not(:popover-open) { display:
none }` on cascade origin, so the closed panel stayed laid out under the
trigger (verified: computed `display: flex`, 200×158 rect while
`:popover-open` was false). Fixed with `.menu--pop:not(:popover-open) {
display: none }`. The `Menu` stories now assert `toBeVisible()` /
`not.toBeVisible()` instead of `:popover-open`, which passes regardless of
`display` and never caught this.

Autodocs quirk (harmless): the toast Docs page mounts all three `Emit` stories
against the one module store, so the last publish wins and three fixed-position
toasts overlap. The individual stories are correct; only the combined Docs page
looks odd. The `MatrixPublishSheet` and `MatrixRowEditor` Docs pages are the same
class of quirk: each mounts every story on one page, so the three publish sheets
duplicate `matrix-publish`/env ids and the two row editors open two
`showModal()` dialogs (the second inert-s the first). The isolated stories, what
the vitest browser run and Canvas view exercise, are correct.

## Publishing to hikyo.app/storybook

The static Storybook is published as a **subpath of the docs Pages site**, not a
separate deploy, GitHub Pages serves one artifact per domain
(`docs.yml` holds `concurrency: group: github-pages`), so a second
`deploy-pages` would clobber the docs. `docs.yml`'s build job, after
`verify-docs.sh` builds `docs/site/dist` **and its service worker**, installs
`clients/ts` + `web` (frozen), runs `pnpm run build-storybook`, and copies
`web/storybook-static` → `docs/site/dist/storybook`. The existing tar/upload/
deploy then ship it under `/storybook/` with zero further changes. Key points:

- **Relocatable, no Vite base.** Storybook's Vite builder emits `./`-relative
  asset URLs (verified: `iframe.html` has zero `"/assets/` refs), so the build
  drops into `/storybook/` unmodified, no `base` override needed.
- **No precache bloat.** The copy runs *after* the SW is generated, so
  `build-pwa.mjs`'s `generateSW` never globs Storybook's bundle into the
  precache manifest (same intent as the `prototype/**` exclusion in #738). There
  is no `navigateFallback` in that config, so `/storybook/` navigations hit the
  network normally.
- **Trigger + live check.** `web/**` was added to the workflow `paths:` so a
  primitives change republishes; the deploy job curls
  `https://hikyo.app/storybook/index.json?<sha>` after `check-docs-live.sh`,
  failing the run if the subpath 404s.
- **Telemetry off.** `main.ts` sets `core.disableTelemetry` (zero-telemetry ADR)
  so the CI `storybook build` never phones home.

Pre-merge evidence (the deploy is `push: main` only, no preview env): the
subpath was rehearsed locally by serving `dist`-shaped `/storybook/` over
`http.server` and loading it in Chrome 153 (all assets 200, a story renders,
Menu anchors correctly); the deploy-job curl is the first live check.

## Verification (all under the pinned toolchain: `fnm exec --using=26.7.0`)

- `pnpm run typecheck` (`tsc --noEmit`, strict + `noUncheckedIndexedAccess`,
  `include` covers `.storybook`): clean.
- `pnpm run test-storybook`: 24 files / 76 tests, exit 0, a11y gate clean
  (was 15/40 before the `src/ui/` primitives + toast + Menu stories landed;
  22/71 before the two Matrix subcomponent story files).
- `pnpm run build-storybook`: exit 0 (autodocs pages build clean).
- `scripts/ci/build-spa.sh --verify`: tsc clean, 113 unit test files pass, Vite
  builds. **Run it through `fnm exec --using=26.7.0`**, the script does not
  self-pin Node, and system Node 20 fails pnpm 11.24.0 with a `node:sqlite`
  `ERR_UNKNOWN_BUILTIN_MODULE`. CI supplies Node via `.nvmrc`, so this is a
  local-only gotcha.

## One bug the pass caught

`OpsDiagnosticBanners.stories.tsx` originally exported a story named `Error`,
shadowing the global. The CssCheck null-guard `throw new Error(...)` would have
thrown `TypeError: Error is not a constructor`, but only on the missing-element
branch a green run never reaches, so the browser tests passed with broken code.
`tsc` caught it (TS2351); the export is now `ErrorSeverity`.

## Scope landed, and what's left

The scope decision was **B** (dialogs + the `withApp` harness + a handful of
flagship screens), taken over C (all ~48 route pages: per-screen seeded data
would mirror the 113 test files, high maintenance, low design-review return).
B is done: the harness (above) plus `Login`, `Projects`, `ProjectSettings`, and
`Members` in their key states.

The remaining ~44 route pages are **addable, not blocked**, each is a new
`*.stories.tsx` with a `parameters.app` table, no harness change. The recipe:
read the screen's load-time hooks, map each to its operation URL in
`clients/ts/src/generated/sdk.gen.ts`, take Zod-valid bodies from the matching
schema (`clients/ts/src/generated/zod.gen.ts` / `@hikyo/zod`) and the screen's
own `*.test.tsx` fixtures, then add one `responses` row per call (a missing row
404s fail-loud). Any screen under `a11y.test: 'error'` may trip axe, fix the
component, never flip the gate; a genuine axe false positive gets that one rule
disabled on that one story with a reason (none of the four screens needed one).
The **full `Matrix` grid screen** is the one known non-starter without harness
work (see the harness bullet above: `useWorkspaceContext`, `useVirtualizer`).
Its subcomponents differ, see that bullet for the per-subcomponent breakdown
(`MatrixKeyCreate`, `MatrixPublishSheet`, and `MatrixRowEditor` are all now
storied; `MatrixPublishSheet` plain-props, `MatrixRowEditor` under
`parameters.app` with an empty `responses` table for a config key).

## Before committing

`web/pnpm-workspace.yaml` carries one T3-sandbox-injected local key,
`allowBuilds: esbuild: true`, which must NOT land, strip it at commit. The
other two blocks stay: `virtualStoreType: global` is already committed in HEAD
(see `docs/handoff/global-pnpm-store.md`), and `overrides: playwright-core:
1.62.1` is the intended new change. The lockfile has no refs to any of these
keys, so CI's `--frozen-lockfile` is unaffected either way.

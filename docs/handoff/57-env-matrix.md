# Handoff — #57 Environment matrix UI + row editor + problems filter

Ticket: [#57](https://github.com/Hikyo-Org/Hikyo/issues/57) (parent #41). Blocked-by #51
(revisions/publish) and #56 (UI shell / flow registry) — both merged before this work
started. Authored by Codex `gpt-5.6-sol` (high) under a Claude orchestrator, per the
ticket's model routing; reviewed two-axis (standards + spec) and through the native
Codex blocking review loop.

## What shipped

The signature surface per the frozen prototype `prototype/env-matrix/31` (branch
`wayfinder-docs`) and `docs/spec/ui-spec.md`, on the flat value model
(`docs/adr/flat-model.md` — set|absent only; no inheritance, mask, provenance chain, or
drift signal anywhere in this surface).

- `web/src/routes/Matrix.tsx` — project-scoped matrix surface
  (`/orgs/:org/projects/:project/matrix`, surface id `matrix` in
  `src/app/navigation.ts`). Cascade lanes: sticky key column + sticky header,
  horizontally scrolling environment lanes, mono cells.
- Density valves: environment show/hide picker (min 1 visible,
  `toggleVisibleEnvironment`), collapsible groups (collapsed header shows the
  comma-separated key list), both per prototype iterations 10/12. Rows are
  virtualized and cell problems pre-indexed, so the 1,000-key × 50-environment
  project limit does not create a 50,000-cell DOM or per-cell full problem scan.
- Problems filter per iterations 30/31: client-computed problems
  (`required_in` × absent — including a staged `unset` — plus server validation
  refusals), filter bar "⚠ filter active: problems — showing n of m keys" +
  "✕ show all keys", filter survives group jumps, filtered-out groups render dimmed
  and inert (title "hidden by the problems filter"), group badges carry counts.
- `web/src/routes/MatrixRowEditor.tsx` — centered row editor opened from a cell
  (since #59 the key NAME opens that key's history, per revision-history it-6): one field per readable environment, protected markers, fill-all,
  write-only secret placeholders, live per-field declaration checks, per-cell clear,
  and provenance per environment. Dirty state distinguishes untouched, explicit
  empty `set`, and `unset`. Config copy-to keeps its protected confirmation + ceremony;
  secret reveal/copy stays on Values (#58).
- `web/src/routes/MatrixPublishSheet.tsx` — selective publish per the frozen
  `renderPublishSheet`: one section per environment holding drafts (checkbox default
  checked, `rN → rN+1`, draft preview with secrets masked and clears labelled),
  problem environments disabled with the veto naming key and environment — they hold
  back, never veto the clean ones; one atomic `publishPendingChanges` over the
  selected version ids. Protected environments in the selection require an explicit
  confirmation and run the #21 ceremony (`useProtectedPublishCeremony.ts`, purpose
  `publish`, same convention as Values' copy-into-protected).
- `web/src/routes/matrix-state.ts` — pure domain seam (problems computation, filter
  projection, blocked/selectable publish sets, protected-confirmation predicate,
  visibility toggling), vitest-covered in `matrix-state.test.ts`.
- `web/src/api/matrix.ts` — API boundary: catalogue + per-env values/settings/signals
  fan-out (tanstack-query `useQueries`), signals polled at 2s (the documented SSE
  polling fallback). A revision advance refreshes its matching values query; the
  boundary refuses half-present pending signals. The first signal snapshot refreshes
  values once to establish ordering; later refreshes happen only on revision advance.
  Config previews come from the server (`listPendingDrafts`, below), bound to the
  immutable pending version id, so reload — or a second browser — keeps publish review
  exact without any client-side material cache. The UI applies the service's Unicode
  trim before staging and previewing, and tells the user when normalization occurred.
  Everything crosses `parsed()` + generated Zod.
- `web/src/api/client.ts` parses the generated error contract before retaining a
  caller-safe detail. Publish adds a matrix validation problem only when that detail
  names one known key/environment; authorization, stale conflicts, network failures,
  and unparsed refusals remain retryable mutation errors.
- `web/src/routes/Projects.tsx` — the projects list became a real surface (links each
  project to its matrix); extracted out of `Placeholder.tsx`, which is a chrome
  skeleton again.
- Signals never colour-only: draft dot + "draft set/cleared" text, `pending_by_others`
  marker, "changed in rN" chip, problem pill — each carries glyph/text + ARIA.

## e2e

`web/e2e/flows/matrix.spec.ts`, registry surface `matrix` (closed-registry closure
green). Serial flow, desktop + mobile projects, dark + light pinned-assertion passes
(axe serious/critical = 0, colour-stripped state text, focus, contrast, ≥44px,
computed styles vs tokens). Covers: problems filter persistence + veto naming
key/environment, selective publish holding back the blocked env while publishing the
clean one, protected publish confirmation + passkey ceremony, density valves, protected
config copy + secret routing to Values, and the acceptance demo — multi-environment
fill/edit, staged-preview reopen, reload-bound publish preview, centered 375px modal,
publish, signals update.

Fixtures: `seed.ts` adds `MATRIX_REQUIRED` (required only in production, staged-clear
to create the veto — a required-absent state cannot be *created*, schema publication
correctly refuses it, so the fixture walks the real user path). The Chromium virtual
passkey installer moved to `fixtures/instance.ts` (`installPasskeyAuthenticator`),
shared with `reveal.spec.ts`; its counter-persistence contract is documented there.

## Pending-draft preview seam (server + API)

`GET /orgs/{org}/projects/{project}/environments/{environment}/pending`
(`listPendingDrafts`, op `value.pending-list`, formula `read@environment`, audited-none
like every read of the same shape) returns the CALLER'S OWN drafts in one environment.
Owner and environment are SQL predicates (`ListPendingChangesForOwnerInEnvironment`,
both engines) — the store's rule that no statement hands one principal another's
ciphertext holds. A `config` `set` draft opens its material (`revealed: true`, `value`)
through the same sealer + `pendingAAD` path publish uses; `secret` drafts, `unset`
drafts and secret-origin material (`material_secret`, the sticky bit #52 restores set)
never carry a value; the gate reads the key's CURRENT classification, so a config
draft staged before a config→secret reclassification stops previewing without anyone
re-staging. Other principals' drafts are invisible here (signals carries presence). No
migration. Registry (`OpValuePendingList`), classify, contract `hierarchyRoutes`,
noproxy and formula pins updated; conformance scenario
`pending_draft_preview_is_owner_filtered_and_classification_safe` (both engines).
Web: `zMatrixPendingDraftList` refines the contract at the boundary (`value` iff
`revealed`; secret/unset never revealed); `pendingConfigPreview` binds a signal's
pending version to its draft and fails loud on a key mismatch. `PendingChange` itself
still never carries the staged value.

## Decisions

- **Copy from the matrix is config-only**; secret copy lives on Values with its
  disclosure ceremony. Wiring reveal-window plumbing into the matrix for secret copy
  ballooned scope with no S3 criterion behind it. Revisit only if a ticket asks.
- **Signals poll (2s) instead of SSE** — the signals endpoint is documented as the
  advisory stream's polling fallback; one live-update protocol per surface.
- **Publish addressed to one env, version ids spanning envs** — the API permits it and
  authorizes each affected environment separately; the sheet's selection is the
  affected set, atomic per flat-model.
- No Go changes; the #50/#51 API surface sufficed.

## Verification record (2026-08-15)

`pnpm typecheck` clean · `pnpm test` 77/77 · full Playwright suite 116/116 (3.2 min,
both viewport projects). PR review fixes cover all nine threads; protected publish is
enforced and regression-tested by rebased `main`'s transactional service path, while
the remaining eight fixes live in this PR. All findings from the capped three-round
native Codex review were repaired; the final post-fix suite is the record above.

## Alignment pass against env-matrix/31 (2026-08-26)

A re-read of the frozen prototype against the shipped surface, on top of the app-chrome
alignment in [#509](https://github.com/Hikyo-Org/Hikyo/pull/509). The surface was already
close; this closes the remaining gaps and records the ones that stay open on purpose.

Governing rule, from [ui-spec.md](../spec/ui-spec.md): the prototype is the reference for
structure and interaction, **DESIGN.md is the reference for visual language**, and the
owning ADR is the reference for semantics. Every conflict below resolves that way.

### Closed

- **The group row now sticks under a MEASURED header.** It was pinned at `top: var(--touch)`
  — a guess that the 44px minimum is also the actual height. `--gh` is written from
  `getBoundingClientRect().height` by a `ResizeObserver` on the `<thead>`, matching the
  prototype's `syncGh`; the comment there records that a rounded `offsetHeight` left a
  visible seam, and it does here too once the webfont lands.
- **The pencil is a pseudo-element, and not on the problem states.** It was a real
  `<span>` in every cell including the red ones. `::after` on
  `.matrix-cell:not(.matrix-cell--problem)`, faint at rest and accent on hover, per the
  affordance decided in iteration 22. A problem cell already carries a glyph and a colour;
  a third mark beside them is noise on the cell that most needs to be read fast.
- **Striping restarts per group** and counts only the rows the problems filter left
  standing. `nth-child(even)` could express neither, and the virtualiser's two spacer rows
  shift its parity regardless, so the stripe is decided in `displayRows`.
- **The chevron rotates** instead of swapping `▸`/`▾`, and the group header carries the
  prototype's `0.14em` tracking.
- **Protected environments are named in text** in the column header and in the environment
  chooser — DESIGN.md asks for it ("with protected state named in text") and neither
  surface said so.
- **A problem cell names the offending value** (`✕ ten`) instead of the generic
  `✕ value problem`. `offendingValue` refuses for any secret regardless of what the wire
  carries: a validation failure is not a reason for the matrix to become a disclosure
  surface.
- **A legend.** The density this surface buys with `·`, `••••••••`, `Δ` and `✕` is only
  honest if the expansion is one gesture away; the prototype's `#legendpop` had no
  counterpart here.
- **Cells are 240px** (was 250px) and narrow to 170px on a phone. The prototype narrows
  only its problem pills at `≤700px`, which leaves two cell scales on the same row; both
  narrow here.

### Deliberately not ported

- **No inheritance, mask, provenance-chain or drift vocabulary**, whatever the prototype's
  older iterations show. [flat-model.md](../adr/flat-model.md) supersedes the inheritance
  ADR *because of* iteration 31, and the wire contract has one boolean where the mask used
  to be.
- **Not the prototype's reveal.** It is a `window.confirm()` that says so in its own text
  ("prototype stand-in — ticket #21"), with a silent 8s `setTimeout` and no clipboard. The
  real ceremony (WebAuthn/TOTP/OIDC, deadline-derived remask with a visible counter,
  `writeExpiringClipboard`) is better on every axis and stays.
- **Cell geometry follows DESIGN.md, not the prototype's CSS**: `--row`/`--touch` heights
  rather than 30px, `--radius-control` rather than 8px, the modal at `--radius-container`
  rather than 14px, and no 999px pills for cell states. The prototype predates the radius
  roles; DESIGN.md governs visual language.
- **The prototype's wording where the ADR has since fixed a term.** It says
  `! required · unset`; the flat model's vocabulary is `set | absent`, and DESIGN.md spells
  the absent cell `· absent`. The ADR term wins.
- **`+ key` and the schema editor.** Key creation is CLI-only by decision, and the
  prototype's `required in:` / constraints / JSON-schema builder belongs to definitions
  governance rather than to the matrix.

### Still open

- `RetentionBoundsFields.tsx` (the maximum-age editor) has no entry point since the
  compact retention rows landed. Nothing is destroyed — the value is preserved on save —
  but a previously editable policy field is unreachable.
- The row editor's "History for …" opens the history drawer *behind* the still-open modal.
- The `Decisions` note above — "copy from the matrix is config-only; secret copy lives on
  Values" — was superseded by #509, which moved secret reveal and copy into the cell modal.

### Also repaired in this pass (app chrome, #29)

The alignment sat on top of [#509](https://github.com/Hikyo-Org/Hikyo/pull/509), whose flow
suite was red on both viewport projects — 10 desktop and 18 mobile failures, with 27 more
tests never reaching a first assertion. Seven root causes, each one hiding the next:

- **`activeProjectId` is never empty.** It falls back to the org's first project so the rail
  always has a tile to mark, so gating the project sidebar on it removed the section list —
  and Overview and Projects with it — from every route. Now gated on `routeProjectId`.
- **The chrome density had no mobile partner.** DESIGN.md's 36px row is a desktop-pointer
  density; `.page--members` restored the 44px floor on a phone and `.page--chrome` never did.
  Neither did the settings controls, the environment chips, or the icon-only revoke button.
  The overrides now sit LAST in `app.css`: a media query adds no specificity, so an earlier
  `@media` block loses to a plain rule declared below it, which is how they looked fixed and
  were not.
- **Light-mode chrome was on the wrong surface.** `--bg-panel` (the prototype's `--bg2`) is
  raised above the page in dark and recessed below it in light; the rail, sidebar and modals
  used `--bg-raise`, which in light is brighter than the paper.
- **The rail's active tile was overridden by its own inline style**, along with three CSS
  rules that had become dead.
- **Two assertions #509 added could not pass**: a sidebar group label missing the trailing
  slash that says "folder", and a mobile `beforeEach` that closed the drawer by clicking the
  toggle the open drawer covers.
- **`.project-sidebar__groups` was declared twice**, the second one transparent.

Two things the suite itself needed:

- `pnpm run e2e` ran both viewport projects against ONE instance, so the instance-admin
  flow's created organisation was visible to the second project and its "exactly two
  organisations" assertion failed for a reason that was not the code. It now invokes each
  project separately, the shape CI's per-viewport sharding already had.
- `expectVisibleFocusIndicator` focused once and then retried only the assertion. A settling
  background query re-renders the row under the cursor, so it waited for a state nothing
  would restore. It now retries the focus WITH the check, and `expectEveryFocusIndicator`
  names the control it failed on — the elements are discovered, so there is no line number
  to work back from.

### Verification record (2026-08-26)

`pnpm --dir web typecheck` clean · `pnpm --dir web test` 382/382 · flow suite green on both
viewport projects, each on its own instance:

| | before | after |
|---|---|---|
| desktop | 10 failed · 7 never ran · 135 passed | **152 passed · 3 skipped · 0 failed** |
| mobile | 18 failed · 20 never ran · 116 passed | **154 passed · 1 skipped · 0 failed** |

The flow-registry closure check and its run-log counterpart both pass, so every surface a
flow claims had the pinned assertion set actually execute on it, in both themes.

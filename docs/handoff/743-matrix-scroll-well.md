# PR #743: outside-click close + matrix scroll well

Two related shell/panel fixes on one branch.

## 1. Close key detail and history panels on outside click (commit 930cfa34)

`.key-detail` and `.history` panels stayed open when clicking outside them.
Both now close on a `pointerdown` outside the panel, matching the escape-key
behaviour they already had. `KeyDeclarationDetail.tsx` and `HistoryDrawer.tsx`
carry the listener; the `HistoryDrawer.tsx` hash in `sensitiveInventory.json`
was refreshed because the file is mutation-capable and pinned.

## 2. Matrix scroll owns its own well at short viewports

### Reported symptom (nightly 42)

> Scrolling on long single folder matrix doesn't work. On nightly 42 I have to
> scroll through the sidebar, not on the matrix.

### The reported bug is already fixed on main by #740

Nightly 42 is tag `v0.0.1-nightly.20260913.42.g106bc87c` = commit `106bc87c`.
`git merge-base --is-ancestor 1180ad0f 106bc87c` returns non-zero: #740
(`1180ad0f`) is **not** in nightly 42. It merged two commits later. So the
build that showed the symptom predates the fix.

Mechanism, reproduced live (A/B of the pre- and post-#740 `.chrome`/`.sidebar`
rules on the real app at 1280×520 with a long single-folder sidebar of 2695px):

| | `.chrome` grid row | `.content` scrolls | sidebar scrolls | document scrolls |
| --- | --- | --- | --- | --- |
| pre-#740 (nightly 42) | 2695px (grew to full list) | no (dragged to 2634px) | no | **yes — the whole page** |
| post-#740 (main) | 520px (capped) | yes | yes | no |

A `1fr` grid track keeps an implicit `min-content` floor, so the unscrolled
long sidebar set the shell row height and dragged the content column along with
it; nothing could scroll internally and the whole page scrolled instead —
exactly "scroll the sidebar, not the matrix". #740's `minmax(0, 1fr)` row plus
the sidebar `max-height: 100dvh` cap the shell to the viewport. This lands in
nightly 43+.

### Residual bug A found and fixed here — `min-height` floor lacked a height gate (campsite rule)

`.matrix__layout { min-height: 420px }` was applied unconditionally. #57/#136
(`b90bd1b6`) added it when the element was `display: block`; #681 (`90b4ca6a`)
converted it to `display: flex; flex: 1; flex-direction: column`, where a px
`min-height` stops the flex column from shrinking to its bounded parent. Below
roughly 570–630px of viewport height (layout available ≈ innerHeight − 61 nav −
48 padding − matrix head, where the head is ~44px but grows to ~100px once the
toolbar wraps at narrow widths; below that the layout wants < 420) the matrix
overflows into `.content`, so the page column scrolls and the `.matrix__scroll`
virtualiser well goes dead — the same whole-page-scroll symptom, on short
viewports, independent of #740.

The floor is **not stale** — it is load-bearing on tall viewports. `.matrix` is
a flex column: the operational-diagnostics banners, `.matrix__head`, and the
inline `.matrix__publish` sheet all keep their full natural height, and
`.matrix__layout` is the only `min-height: 0` sibling, so the flex algorithm
shrinks the well and nothing else. With no floor the well starves to 0px on a
tall viewport too, the virtualiser renders no rows, and the matrix vanishes
below the fold. The floor was masking that; only its **unconditional
application** at short viewports was the bug.

Fix (height-gated floor): base `min-height: 0` so the short-viewport
no-overflow contract holds, plus `@media (min-height: 640px) {
.matrix__layout { min-height: 420px } }` to restore the working floor once the
viewport is tall enough to hold it. 640px clears every e2e viewport (desktop
800, Pixel 5 851), so the floor applies there = main's proven-green layout;
below 640 the floor drops and the well fits the bounded column.

The 640px gate is e2e-derived, not content-derived. With both Warning banners
present (~367px, per the short-viewport sweep) a 640–~800px viewport gets the
420px floor and still overflows into `.content` — the same bug-A symptom. This
is **identical to main** (unconditional floor), so it is not a regression and
this branch is ≥ main everywhere; but the short-viewport class is not fully
closed. It rides on the banner-height deferred item below.

Verified against the seeded e2e instance at 844×380 (below the gate, so the
floor is absent), toggling the floor in the running DOM (A/B in one
measurement):

| `.matrix__layout` floor at 844×380 | `.matrix__scroll` clientHeight | well owns scroll | page column |
| --- | --- | --- | --- |
| `420px` (ungated, broken) | 419px — ballooned past the 197px column | 0 (dead well) | scrolls |
| gated off below 640 (fixed) | 36px — fits the column | 329px | table scroll lives in the well |

### Regression bug D — removing the floor unconditionally starved tall viewports

The first cut of this branch (`d32f58c1`) removed the floor outright as "stale".
That turned CI red: on the e2e viewports (desktop 800, Pixel 5 851 — all above
640px) the floorless well starved to 0 rows, so `matrix.spec.ts` 88 / 595 / 910
and `history.spec.ts` 559 could not see their rows. These were **my regression,
not pre-existing failures** (an earlier PR-body claim that they reproduced on
`origin/main` was wrong and has been retracted). The height-gated floor above is
the complete fix — it keeps the short-viewport contract (test 595 at 844×380,
below the gate) *and* restores the tall-viewport floor (all other e2e viewports,
above the gate = main's green layout).

Note for anyone running the e2e suite **locally**: `playwright.config.ts` is
`workers: 1`, `fullyParallel: false`, and a multi-project local run shares one
seeded instance across the `desktop` and `mobile` projects. `history.spec.ts`
559 stages a `WORKERS`-dev draft and publishes only the restore set, leaving a
lingering pending draft (no `finally` cleanup); in a combined local run the
desktop leg pollutes the shared DB and the mobile leg's editor then shows the
leftover draft, so mobile-559 fails with "Save 0 drafts". CI never hits this —
each matrix leg is its own runner with its own seeded instance (`ci.yml`, "each
MATRIX LEG is its own runner"). mobile-559 run alone on a fresh instance passes.
The draft-cleanup weakness is pre-existing on main; see the deferred list.

### Residual bug B found and fixed here — well was not a containing block

With the floor gone the well fits the column, but `.content` *itself* still
gained ~287px of blank scroll at 844×380. Root cause, instrumented on the seed
(not guessed): `.matrix__scroll` was **unpositioned**. A `.visually-hidden`
span (`position: absolute`, app.css `.visually-hidden`) rendered inside a
virtualised matrix cell (`<span class="visually-hidden">draft</span>`,
`Matrix.tsx`) therefore resolved its containing block against the nearest
positioned ancestor — `.matrix__surface`, *above* the `overflow: auto` well —
and so escaped the well's clip. An absolute descendant whose containing block
is outside an `overflow` ancestor is not clipped by it; that span's box
extended to scroll-bottom 484 and became the sole contributor to `.content`'s
scroll extent. Scrolling `.content` to the bottom revealed blank space, matrix
gone — a phantom page scroll.

Fix: `.matrix__scroll { position: relative }`. The well becomes the containing
block for its own absolute descendants, so they clip inside it again. Sticky
`th` is unaffected (sticky is relative to the scrollport); no `z-index`, so no
new stacking context; the virtualiser measures via the scroll element rect and
is unaffected.

Instrumented A/B on the seed at 844×380 (per absolute descendant, hidden one at
a time, measuring `.content` overflow):

| element | `position` | scroll-bottom | hiding it → `.content` overflow |
| --- | --- | --- | --- |
| `.visually-hidden` "draft" (in a virtualised cell) | absolute | 484 | **287 → 0** |
| `.matrix__legend-body` (closed `<details>`) | absolute | 438 | 287 → 287 (no effect) |
| `.matrix__environment-picker fieldset` (closed) | absolute | 312 | 287 → 287 (no effect) |

The two closed-`<details>` bodies lay out (their rects report a height) but a
closed `<details>` applies `content-visibility` containment to its content,
which clips both paint and scrollable overflow — so they never entered
`.content`'s scroll extent. **This corrects an earlier, wrong reading in this
handoff** that attributed the 287px to those popover bodies "overhanging while
closed"; the instrumented per-element A/B above shows only the in-cell
`.visually-hidden` span contributed. The popovers were a red herring.

### Residual bug C found and fixed here — env-picker popover clipped off-screen

At ≤375px wide the "Visible environments" popover ran off the right viewport
edge — its "PROTECTED" marker was unreachable. The `fieldset` is
`position: absolute; left: 0`, and its containing block was
`.matrix__environment-picker` (`position: relative`), which sits at the right
end of `.matrix__key-heading` (`justify-content: space-between`). `left: 0`
therefore anchored the popover to the button's far-right position and
`width: max-content` grew it further right, off-screen. A width cap alone can't
fix it (button ≈110px in + 351px popover > 375px).

Fix: move the containing block to the Key cell — `.matrix__key-heading {
position: relative }`, `.matrix__environment-picker { position: static }` (its
`z-index` dropped; it's ignored on a static box and the `fieldset` carries its
own). `left: 0` now anchors to the Key column's left edge, and
`max-width: calc(100vw - 24px)` caps it. Verified by screenshot at 375 (popover
fully on-screen, "PROTECTED" visible) and 1280 (popover shifts to the column
left edge, reads clean, no clip).

### Regression test

`web/e2e/flows/matrix.spec.ts` gains one test in the `environment matrix`
describe: at 844×380 it asserts `.matrix__scroll.clientHeight < .content.clientHeight`
(the well fits the viewport column), that the well owns the table's overflow,
and — catching bug B — that `.content.scrollHeight - .content.clientHeight === 0`
(`.content` does not scroll; everything lives in the well). Under the 420px
floor the well is 419px against a 197px column (first assertion fails); without
`position: relative` the escaped `.visually-hidden` span adds 287px of blank
`.content` scroll (last assertion fails). The test catches both bugs.

happy-dom has no layout engine, so this must be a Playwright test, not a vitest
one. It carries no pinned-assertion claim, so it adds no registry surface; it
rides the existing spec file so the merge gate (which loads the base branch's
spec-group lists) still runs it. Confirmed passing on both the `desktop` and
`mobile` viewport projects.

### Deferred (Marc's call, not folded in)

Surfaced during the short/mobile UX sweep; none auto-fixed, each is a design
decision:

- Two non-dismissible Warning banners (root-escrow-not-verified, pins-expiring)
  consume ~55% of a 375×667 portrait viewport before any matrix is visible.
  Plausibly e2e/dev-instance states — confirm a healthy prod instance before
  proposing collapse/dismiss.
- At 844×380 the well is one row tall (36px): head + nav + padding leave almost
  nothing. Option would be to let the matrix head scroll away on short
  viewports.
- The env-picker popover, being inside the well, is still clipped by the well
  whenever the well is short (shares the 36px-well root above). Escaping it
  needs the popover in the top layer (`popover` attribute) — a design change.
- On mobile the picker `<summary>` button paints over the open legend popover
  (the sticky `th` has its own stacking context).
- `history.spec.ts` 559 leaves a lingering `WORKERS`-dev pending draft, so a
  local multi-project run (which shares one seeded instance across `desktop` and
  `mobile`) or a re-execution of 559 fails "Save 0 drafts" (see bug D note).
  **Root cause is harness-level, not test 559:** the local runner shares one
  instance across the two projects; CI does not (each matrix leg = its own
  runner + seed). Within a single leg later tests survive because staging is
  delete-then-insert (test 685 re-stages `WORKERS` fresh), so this never bites
  CI. A per-test `finally` would be a band-aid — and there is no clean fix for
  it: there is no single-draft discard endpoint (`DELETE values/{key}` stages a
  *clear*, still a pending row; `DiscardKey`/`DiscardEnvironment` are
  store-internal with no HTTP surface), and the only removal, `publish`, creates
  a revision that the revision-count/ordering-sensitive tests after 559 (759,
  789, 905) would see — an unverifiable-by-CI change that can only add failures.
  The real fix is a fresh instance per project in the local harness
  (`playwright.config.ts` / `instance.ts`). Out of scope for a CSS fix — Marc's
  call: separate issue vs. fold the harness change here.

### Validation

web typecheck passes. The regression test passes on the `desktop` and `mobile`
projects against a freshly built instance; the instrumented A/B tables above are
the ground-truth measurements. `app.css` and `matrix.spec.ts` are not in
`sensitiveInventory.sources`, so no inventory hash refresh was needed.

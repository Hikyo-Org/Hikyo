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

### Residual bug A found and fixed here — stale `min-height` floor (campsite rule)

`.matrix__layout { min-height: 420px }` is a stale floor. #57/#136 (`b90bd1b6`)
added it when the element was `display: block`; #681 (`90b4ca6a`) converted it
to `display: flex; flex: 1; flex-direction: column`, where a px `min-height`
stops the flex column from shrinking to its bounded parent. Below roughly
570–630px of viewport height (layout available ≈ innerHeight − 61 nav − 48
padding − matrix head, where the head is ~44px but grows to ~100px once the
toolbar wraps at narrow widths; below that the layout wants < 420) the matrix
overflows into `.content`, so the page column scrolls and the `.matrix__scroll`
virtualiser well goes dead — the same whole-page-scroll symptom, on short
viewports, independent of #740.

Fix: `min-height: 0`. `flex: 1` still fills the bounded column, so the empty
matrix does not collapse; only the artificial floor is removed.

Verified against the seeded e2e instance at 844×380, toggling the floor in the
running DOM (A/B in one measurement):

| `.matrix__layout` floor | `.matrix__scroll` clientHeight | well owns scroll | page column |
| --- | --- | --- | --- |
| `420px` (broken) | 419px — ballooned past the 197px column | 0 (dead well) | scrolls |
| `0` (fixed) | 36px — fits the column | 329px | table scroll lives in the well |

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

### Validation

web typecheck passes. The regression test passes on the `desktop` and `mobile`
projects against a freshly built instance; the instrumented A/B tables above are
the ground-truth measurements. `app.css` and `matrix.spec.ts` are not in
`sensitiveInventory.sources`, so no inventory hash refresh was needed.

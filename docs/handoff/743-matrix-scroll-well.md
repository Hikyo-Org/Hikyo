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

### Residual bug found and fixed here (campsite rule)

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

### Regression test

`web/e2e/flows/matrix.spec.ts` gains one test in the `environment matrix`
describe: at 844×380 it asserts `.matrix__scroll.clientHeight < .content.clientHeight`
(the scroll well fits inside the viewport column) and that the well owns the
table's overflow. Under the 420px floor the well is 419px against a 197px
column, so the first assertion fails — the test genuinely catches the bug.

It deliberately does NOT assert `.content.scrollHeight`. The absolutely-
positioned legend body (~364px) and environment-picker fieldset (~125px) lay
out and can overhang the content bottom **even while their `<details>` is
closed** (measured `[open]` = false, body still `position:absolute;
visibility:visible`), inflating `.content.scrollHeight` with no in-flow
overflow. Proven on the e2e seed by hiding every absolute descendant: content
overflow 287 → 0 while the well is unchanged (`clientHeight` 36, owns 329). On a
slightly taller seed the legend fits and content overflow is already 0 — the
signal is seed/height dependent, so it is not asserted. The well's clientHeight
is the honest in-flow discriminator.

(Aside, out of scope: the legend `<details>` renders its body visibly while
closed. It only bites here because it is `position:absolute`. Worth a separate
look, not folded into this scroll fix.)

happy-dom has no layout engine, so this must be a Playwright test, not a vitest
one. It carries no pinned-assertion claim, so it adds no registry surface; it
rides the existing spec file so the merge gate (which loads the base branch's
spec-group lists) still runs it. Confirmed passing on both the `desktop` and
`mobile` viewport projects.

### Validation

web typecheck passes. The new regression test passes on the `desktop` and
`mobile` projects against a freshly built instance; the A/B table above is the
ground-truth measurement that proves it flips under the stale floor. app.css and
matrix.spec.ts are not in `sensitiveInventory.sources`, so no inventory hash
refresh was needed.

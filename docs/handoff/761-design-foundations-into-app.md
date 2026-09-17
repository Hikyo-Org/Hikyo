# #761 layer 1: the design foundations moved into the app

Branch `feat/761-design-foundations-app`. Layer 1 of the migration planned in
[storybook-ui-consistency.md §5](./storybook-ui-consistency.md): the foundations
authored in Storybook under #755 now ship in the app, the rules they supersede
are deleted, and the desktop e2e density pins read the token the app actually
uses. Layer 2 (route markup onto the `ui/` atoms) is #762 and is not in this
branch.

Working map, with every line number verified against the tree at `dc8b8c41`:
`.superpowers/sdd/761-css-map.md` (not committed; it is scratch).

## What moved

`web/src/ui/ui.css` (952 lines) is gone. Lines 16 to 952 were appended byte for
byte to the end of `web/src/styles/app.css` under a provenance banner at the
move commit (`312251e9`). No interleaving, no reordering: the foundations are
last in the file, so within equal specificity they win by source order, which is
exactly how they won when ui.css was a separate later stylesheet.

The body is no longer byte-identical to ui.css: seven edits landed inside it
after the move, each reviewed as an authorised change to the moved section.
Only two change a declaration (4 and 5); the rest are comments.

1. `.context-sidebar > h2` added to the eyebrow selector list.
2. The "five rules app.css states with more specificity" comment rewritten
   three times: first to three rules, then to state the true reason each of
   the three exists, then to correct the sidebar media copy from 800px to
   700px.
3. The checkbox-block comment rewritten after the two `(max-width: 800px)`
   checkbox bumps were deleted.
4. A `@media (pointer: coarse) { button.settings-tag { min-height: var(--touch) } }`
   bridge added, to be deleted with #762's markup swap.
5. `:root :is(.sidebar__link, .environment-lifecycle > summary)` split into two
   rules so the summary keeps the UA's `list-item` display (and therefore its
   disclosure marker) and centres with `align-content` instead of flex.
6. The badge-block header no longer claims to supersede `.history__current`
   and `.matrix__problem-count`, which carry no `.badge`.
7. The field-controls header corrected from (0,2,1) to the rule's real
   (0,1,1).

**No tokens moved.** The premise in the ticket was stale: ui.css declared no
custom properties at all (every `:root` in it was a descendant combinator used
to raise specificity). The token move happened in #755;
`web/src/styles/tokens.css` already held all 48 properties the foundations
consume. `DESIGN.md:54`, which still claimed `--fs-*` lived in ui.css, is
corrected.

Also updated with the move: `web/.storybook/preview.tsx` (import dropped),
`web/scripts/design/adherence-budget.json` (the `src/ui/ui.css` key dropped),
`web/scripts/design/adherence-check.ts` (comment), `web/src/ui/Checkbox.tsx:7`,
and `docs/handoff/storybook-setup.md:158` (both said the checkbox box is drawn
by `src/ui/ui.css`; it is `src/styles/app.css`).

## What was deleted, per commit

The governing test for every delete, applied identically by all four tasks:
**delete a declaration only when the foundation block names the rule AND the
trailing foundations section restates that same property for that same
selector.** Where only some declarations were restated, only those went; where
a selector list was shared, the list was split. This matters because moving a
block *earlier* in app.css than a rule it used to beat flips every
equal-specificity win, so the move and the delete had to be atomic per block.

| commit | what |
|---|---|
| `312251e9` | ui.css appended to app.css, file deleted, references updated |
| `1453fb2f` | plan correction: all twenty desktop pins counted |
| `d216d101` | B1 type scale, B2 buttons, B3/B9 field controls |
| `9df304f6` | cascade and comment corrections from those deletions |
| `8dffd823` | B5 alert/notice, B6 publish sheet, B7 folder cleanup, B12/B14 badge and chip |
| `c52d4f45` | plan note: the badge fold layer 1 leaves open falls to #762 |
| `4063f417` | B10 choice controls, B15 dialogs, B4 the touch-height fold |
| `e7956597` | the last four local checkbox and radio size rules |
| `981e531b` | the twenty e2e pins, the dead matrix checkbox bump, the handoffs |
| `b7e57020` | review fixes: the stale "five rules" comment, ui.css tense in the handoffs, em-dashes in the touched files |

## What stayed, and why

Rules the map or a brief named but that survive, each because the trailing
foundations section does not restate the property for that selector:

- `.stepup .alert { flex-basis }`, `.alert__glyph { font-weight, color }`,
  `.toast--info/.toast--success .alert__glyph` (colour): deleting the base
  glyph colour would unpaint the danger glyph in `.alert` and `.toast`.
- `.matrix__publish h2`, `.matrix__publish > p`, `.matrix__publish-env` and its
  `--blocked` and `li` variants: the foundation targets children, not the
  container, and the `h2` treatment is B1 territory.
- `.history__current` and both `.matrix__problem-count` definitions: neither
  element carries `badge` in the markup, so `:is(.badge, .chip, .settings-tag)`
  never matches them. #762 swaps them onto `Badge`.
- `.audit__outcome-choice`: a `<label>` with no `.chk` class; nothing restates
  its size or layout.
- `.matrix-key-create__type select { min-width }`, the 700px `.header >
  .btn--icon { flex: none }`, and the 700px `min-height: var(--touch)` group on
  `.sidebar__switcher, .settings-row__title--link, .settings-row__detail a,
  .identity-hue`: map entries that did not match the file.

Unnamed `var(--touch)` survivors keep the 44px floor by design (map §3b):
`.avatar`, `.matrix__group-row th`, `.matrix-cell`, `.matrix__key-row > th|td`,
`.matrix__key`, `.matrix__drafts`, `.history__settings-pointer`, the settings-row
link group above, and `.matrix__key-cell > .matrix__key` under
`(pointer: coarse)`.

One bridge rule was added rather than deleted: a coarse-pointer touch floor for
`button.settings-tag`, because the foundation sizes `.settings-tag` from
`--badge-height` and the element is a button. The root fix is the `Button` swap
in #762.

Deleted in `981e531b`: the `@media (max-width: 800px)` bump
`.matrix__environment-picker input, .matrix-editor__copy input,
.matrix__publish input { width/height: var(--touch) }`. Every input under those
three containers is a checkbox (verified in `Matrix.tsx`, `MatrixKeyCreate.tsx`,
`MatrixRowEditor.tsx`, `MatrixPublishSheet.tsx`), so at (0,1,1) the rule loses
to the foundations' `:root input[type='checkbox']` at (0,2,1) on every viewport.
It was dead, and the foundations comment claimed it.

## The adherence ratchet

`web/scripts/design/adherence-budget.json`, app.css literal sizes:

| point | budget |
|---|---|
| `origin/main` | 798 (plus a `src/ui/ui.css: 0` key, now gone) |
| after the type/button/field deletions (`d216d101`) | 762 |
| after the cascade and comment corrections (`9df304f6`) | 759 |
| after the alert/publish/badge deletions (`8dffd823`) | 744 |
| after the choice/dialog/fold deletions (`4063f417`) | 736 |
| after the last local size rules (`e7956597`) | 728 |
| head of this branch | 728 |

The budget only ratchets down; `pnpm run design:check` fails if the count is
below the budget and the budget was not lowered with it.

## The e2e density pins

Twenty-one desktop pins now read the control token. Twenty moved from
`'--touch'` to `'--control'`. `expectDensity`
(`web/e2e/fixtures/assertions.ts`) reads the token off `:root` and compares the
element's own box, so this is a pure token-name swap: no assertion is weakened,
and on a coarse pointer `tokens.css` resolves `--control` to 44px, so the mobile
project measures exactly what it measured before.

| file | lines | target |
|---|---|---|
| `login.spec.ts` | 67, 96, 140, 182 | "Sign in", "Close this window", "Establish credential", "Continue" |
| `matrix.spec.ts` | 351, 435, 1021 | "Close row editor", environment chooser summary, `.key-detail__close` |
| `shell.spec.ts` | 506 | theme toggle |
| `members.spec.ts` | 679, 718, 744 | dialog "Cancel", composition "Cancel", "Back, change scope" |
| `machine-access.spec.ts` | 673, 800 | mint button, dialog "Done" |
| `history.spec.ts` | 296 | `.history__tab` |
| `settings.spec.ts` | 586, 842 | `#project-metadata input`, identity control input |
| `reveal.spec.ts` | 416, 449, 753 | "Use a passkey", new-value input, "Reveal all" |
| `scanning.spec.ts` | 157 | environment chooser summary |

The twenty-first is a different shape: `shell.spec.ts:86` pinned the sidebar
link's `min-height` to the LITERAL `'38px'`, so the `'--touch'` sweep that found
the other twenty never saw it. `.sidebar__link` is named in the control-height
fold, so the foundations put it on `--control` (36px on a fine pointer) and the
local `min-height: 38px` at `app.css:731` was dead; the desktop run failed with
`Expected: "38px" / Received: "36px"`. The dead declaration is deleted and the
pin now reads `--control` off `:root` inside the test, since this assertion is a
plain `toHaveCSS` rather than an `expectPinnedAssertionSet` density entry. The
adjacent `font-size: 13px` (which is `--fs-sm`) and the 28px avatar are
unaffected and were left alone.

Unchanged: the seven `testInfo.project.name === 'mobile' ? '--touch' : '--row'`
row-density lines (`members.spec.ts:614/938/1061`,
`instance-admin.spec.ts:1082/1114`, `machine-access.spec.ts:1035/1267`). After
this branch, `grep -n "'--touch'" web/e2e/flows/*.ts` lists exactly those seven.

## What the controller's e2e runs found

Two findings, both from rules the fold superseded on fewer axes than the
deleted originals covered.

Desktop (181 passed, 1 failed): `shell.spec.ts:86` expected the sidebar link at
`38px` and got `36px`. See the pin note above; the dead `min-height: 38px` on
`.sidebar__link` is deleted and the pin reads `--control`.

Mobile (122 passed, 3 failed, one root cause): `touch width of Self`, 41px
against the 44px floor, in `machine-access.spec.ts:1253` and
`members.spec.ts:1042` (dark and light). "Self" is the quiet button in the audit
filter (`web/src/routes/Audit.tsx` around line 213). The deleted
`@media (pointer: coarse) .btn--quiet` rule set `min-width` as well as
`min-height`; the Buttons block restated only the height, which is enough for
every quiet button with a wide label and not for a four-character one.
`min-width: var(--control)` is added to the foundation `.btn` rule, so every
button holds the control size on both axes (36px on a fine pointer, 44px on a
coarse one through the tokens.css override). No compact tier was introduced.
The block's header now says the coarse `.btn--quiet` bump is superseded on both
axes.

The Constraints list in `storybook-ui-consistency.md` named twelve of the twenty
and had `settings.spec.ts:575` for `:586`; it is corrected there, with the
twenty-first pin added, along with the
"five rules app.css states with more specificity" note, which resolves to three
rules.

## Known visual changes on a fine pointer

Intended, and the reason the preview checks below exist:

- Every page-content control is `--control` (36px) instead of 44px, including
  dialog buttons, which lost `.page--chrome .ceremony .btn` and its siblings.
- `.matrix__history-link` grows from 11.5px to 14px under the button rule.
- `.settings-tag` drops from 32px to 20px inside settings rows aligned to 36px.
- The unclassed `<a>` on the overview page loses browser blue for body ink plus
  an underline.
- Dialogs gain a shadow (the ceremony family had none).
- The members capability row grows to 36px: the revoke button is now one control
  height with no negative margins.
- `DefinitionsBundlePanel`'s file input renders native; the field-control
  foundation excludes `[type='file']` on purpose.
- `.chip--armed`, `.chip--wide` and `.settings-tag--on` lose their accent
  emphasis until #762 maps those states onto `Badge` tones. All three were
  already dead once the foundations loaded.

## Preview verification (controller)

Run 2026-09-17 against the prototype mode (`pnpm run prototype`, mock API) at
1280 and 390 wide, both themes, reading computed styles through the preview
tooling. The mock seeds six keys, so the 50-key matrix check rests on the
matrix e2e spec (desktop 182 passed) rather than a screenshot.

- [x] Dialogs opened from routes: members invite dialog (`.ceremony`) 520px,
      shadow `0 14px 42px`, h2 16/700 sentence case, lede 13px dim, actions
      gap 8px, buttons 36px
- [x] Matrix: history link 36px / 13px (was 11.5px); thead th 37px on the row
      token; add-key 36px / 13px; checkbox 24px with `appearance: none`;
      environment chooser summary 36px; legend toggle 36px min-width 36px
- [x] Settings identity controls: hue swatch 36x36, glyph 36x36; text inputs
      36px (14px, mono 13px); selects 36px; section h2 16px sentence case
- [x] 390px: no horizontal overflow, sidebar collapsed, controls 36px (fine
      pointer; the coarse floor is the mobile e2e project's job: 126 passed)
- [x] Light theme: bg `oklch(0.965 0.008 200)`, ink `oklch(0.25 0.03 225)`
- [x] `.settings-tag` button 20px / 11px inside settings rows (fine pointer;
      the coarse bridge rule keeps it at 44px on a phone)
- [x] Sidebar eyebrow 11px / 500 / uppercase; h1 20/700; panel h2 16 sentence
      case
- [x] Overview "Choose a project" link: ink colour, underline (not browser
      blue)
- [x] Members capability row 44px (the row has its own padding; the revoke
      control is 36x36 with no negative margins); chips 20px / 11px
- [x] `environment-lifecycle` summary: `display: list-item`, `align-content:
      center`, 36px, marker rendered (d58b3c4c)
- [ ] `DefinitionsBundlePanel` file input (native picker): not reachable in
      the mock; accepted by ruling, confirm on a real instance
- [ ] `.chip--armed` / `.chip--wide` / `.settings-tag--on` emphasis: expected
      loss until #762 maps them onto Badge tones; not seeded by the mock

Observed and handed to #762: the invite dialog lists its primary button first
(markup order, Dialog puts it last); `.matrix__group-row .matrix__group-toggle`
stays uppercase at 13px (a #755 gap, not an eyebrow).

## e2e (controller)

Run on `cb88085c` from this worktree, on alternate port families so another
session's suite on the default ports could not collide (`HIKYO_E2E_PORT*`):

| project | specs | result |
|---|---|---|
| desktop (29789 family) | login, matrix, shell, members, machine-access, history, settings, reveal, scanning | 182 passed, 0 failed |
| mobile (30789 family) | login, matrix, members, settings, machine-access | 126 passed, 0 failed |

The two findings the first runs produced (the sidebar link's literal 38px pin
and the quiet button's missing width floor on a coarse pointer) are fixed in
cb88085c and described under "What the controller's e2e runs found". CI runs
the complete suite on the pull request.

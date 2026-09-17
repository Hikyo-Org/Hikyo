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

Twenty desktop pins moved from `'--touch'` to `'--control'`. `expectDensity`
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

Unchanged: the seven `testInfo.project.name === 'mobile' ? '--touch' : '--row'`
row-density lines (`members.spec.ts:614/938/1061`,
`instance-admin.spec.ts:1082/1114`, `machine-access.spec.ts:1035/1267`). After
this branch, `grep -n "'--touch'" web/e2e/flows/*.ts` lists exactly those seven.

The Constraints list in `storybook-ui-consistency.md` named twelve of the twenty
and had `settings.spec.ts:575` for `:586`; it is corrected there, along with the
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

Issue #761 items 4 and 5, plus the three items the tasks surfaced. To be filled
in by the controller against a real instance.

- [ ] Dialogs opened from routes (not only from stories) read the one anatomy
- [ ] A 50-key matrix at full width
- [ ] The settings identity controls (hue range, glyph)
- [ ] Phone layouts at 390px
- [ ] Both themes, light and dark
- [ ] `.matrix__history-link` size
- [ ] `.settings-tag` 20px inside settings rows
- [ ] Sidebar eyebrow 11px
- [ ] The overview page's unclassed link (ink + underline, not browser blue)
- [ ] Dialogs gain a shadow
- [ ] The members capability row is 36px tall
- [ ] `DefinitionsBundlePanel`'s file input renders native
- [ ] `.chip--armed` / `.chip--wide` / `.settings-tag--on` lost their accent
      emphasis (expected until #762)
- [ ] `environment-lifecycle` summary shows its disclosure marker and centres
      its text at 36px

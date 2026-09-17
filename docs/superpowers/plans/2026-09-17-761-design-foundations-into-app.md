# #761 Design foundations into the app (layer 1, CSS and tokens) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Load the approved design foundations (`web/src/ui/ui.css`) in the app, delete the app.css rules they supersede, retarget the desktop e2e density pins from `--touch` to `--control`, and delete `ui.css`.

**Architecture:** Today Storybook loads `tokens.css`, `app.css`, then `ui.css`; every `ui.css` rule wins by cascade order (and, for the "fold" rules, by a `:root` specificity bump). Task 1 preserves that exact cascade by moving the whole of `ui.css` to the END of `app.css` as one marked section, so the first commit is a pure relocation with zero visual change in Storybook and the full visual change in the app. Tasks 2 to 4 then delete the superseded app.css rules block by block, each verified against the story suite. Task 5 retargets the e2e pins. The Appendix (the migration map, verified against commit dc8b8c41) is the source of every line number; line numbers drift as rules are deleted, so always grep the selector before editing.

**Tech Stack:** plain CSS, Vitest Storybook browser project (`pnpm run test-storybook`, `pnpm run test-storybook:light`), `pnpm run design:check` (literal-size ratchet), Playwright e2e.

**Spec:** GitHub issue #761; `docs/handoff/storybook-ui-consistency.md` §2, §4, §5; `web/src/ui/ui.css` block header comments (each names what it supersedes).

## Global Constraints

- **Cascade ruling:** the ui.css blocks live as ONE trailing section of `app.css` headed `/* === Design foundations (authored in Storybook, #755; moved by #761) === */`, in their original order, `:root` specificity prefixes kept. Do not interleave them beside the rules they supersede: that reorders the cascade and flips every equal-specificity win. Deleting a superseded rule is the only edit the earlier part of app.css gets.
- **Delete only what a block names.** A rule is deleted when a ui.css block header (or the Appendix map) names it as superseded AND the trailing section restates the same selector-and-property. Declarations the fold covers by a broader selector (the 179 raw `font-size` sites, the unnamed `var(--touch)` survivors in Appendix §3b) stay in place: they are dead under the fold, and the adherence ratchet retires them over time. When a superseded rule shares a selector list with a surviving one, split the list, do not delete the survivor.
- **Zero story change:** after every task, `pnpm run test-storybook` and `pnpm run test-storybook:light` pass with the same 232 tests, no story or a11y assertion edited. A story that fails after a delete means the delete removed a rule the trailing section did not restate: restore it and report under Concerns.
- **Ratchet down, never up:** `pnpm run design:check` after each task; when it prints "Lower the budget", set `web/scripts/design/adherence-budget.json` `src/styles/app.css` to the printed count in the same commit.
- **Do not weaken e2e assertions.** The density sweep still reads the input's own box and the element's own outline; only the token NAME changes on desktop pins.
- **Commits:** Conventional Commits, `git commit -s` (DCO), signing enabled, never `--no-verify`. No em-dash anywhere.
- **Per-task checks:** `cd web && node --run typecheck && node --run test && pnpm run test-storybook && pnpm run test-storybook:light && pnpm run design:check`.
- **Environment:** Node from `.nvmrc` (26) via `fnm use`; deps already installed in `web/` and `clients/ts/`.

---

### Task 1: Move ui.css into app.css and delete it

**Files:**
- Modify: `web/src/styles/app.css` (append), `web/.storybook/preview.tsx:11` (delete the import), `web/scripts/design/adherence-budget.json` (drop the `src/ui/ui.css` entry), `web/scripts/design/adherence-check.ts:2-4` (comment), `web/src/ui/Checkbox.tsx:7` (doc comment says "ui.css draws the box": say "app.css"), `DESIGN.md` line ~54 (find with `grep -n "ui.css" DESIGN.md`; the `--fs-*` tokens already live in `tokens.css`, fix the sentence).
- Delete: `web/src/ui/ui.css`.

**Interfaces:**
- Produces: the trailing section of app.css that Tasks 2 to 4 delete against.

- [ ] **Step 1: Append**

Take `web/src/ui/ui.css` from its first block (`/* --- Type scale` at line 16) to the end. Drop only the file header (lines 1 to 14). Append to `app.css` after a blank line and this banner:
```css
/* =========================================================================
   Design foundations, authored in Storybook (#755) and moved here by #761.
   Each block names the earlier app.css rules it supersedes; those are
   deleted as the migration lands. Order and the `:root` specificity
   prefixes are kept on purpose: the blocks win by cascade, exactly as they
   did when Storybook loaded ui.css after this file.
   ========================================================================= */
```
Then `git rm web/src/ui/ui.css`.

- [ ] **Step 2: Remove every other reference**

Delete `import '../src/ui/ui.css'` from `web/.storybook/preview.tsx`. Remove the `"src/ui/ui.css": 0` line from `adherence-budget.json`. Reword the `adherence-check.ts` comment ("app.css carries its legacy count until #761 and #762 retire the rules"). Fix the Checkbox doc comment and the DESIGN.md sentence. `grep -rn "ui\.css" web/src web/.storybook web/scripts DESIGN.md` must return nothing.

- [ ] **Step 3: Verify**

Run the per-task checks. Expected: typecheck clean, 1030 unit tests, 232 storybook tests in both themes, `design:check` prints app.css literal count unchanged at 798 (ui.css had zero literals).

- [ ] **Step 4: Commit**

```bash
git add -A web/src web/.storybook web/scripts DESIGN.md
git commit -s -m "feat(web): load the design foundations in the app (ui.css moved into app.css)"
```

---

### Task 2: Delete the superseded type, button and field-control rules (blocks B1, B2, B3, B9)

**Files:**
- Modify: `web/src/styles/app.css` (deletions only, above the trailing section).

Use Appendix §2 B1, B2, B3, B9 for the selectors. For each named rule: open it, delete the declarations the trailing block restates (for most rules that is the whole rule; for `.btn` at app.css:63 it is `min-height`, for `.field label` at 115 it is the size/colour/weight, and so on), and delete the rule entirely when nothing is left. Named list to work through (grep each selector; line numbers are from the Appendix and will drift):

- B1 type: `.panel h2` (4083) text-transform and size, `.page--chrome .panel h2` (4188), `:focus-visible` (48), `.btn[disabled]` (95), `.field label` (115), `.field__hint` (4123); heading sizes `.card h1, .card h2` (1158), `.page h1` (4025), `.page--chrome h1` (4151), `.login__title` (417), `.matrix__head h1` (1498), `.panel h3` (4094), `.sidebar__section h2` (751), `.context-sidebar > h2` (897).
- B2 buttons: `.btn` min-height (63), `.page--chrome .btn` (4500), `.page--chrome .ceremony .btn, .page--chrome .matrix-editor .btn` (4507), `.btn--icon` (103) and its 700px media copy (1390), `.btn--quiet` (4130) and its coarse copy (5039), `.tab` (3021) and `.menu__item` (1189) min-heights, `.matrix__add-key` (2013, 2718) size, `.capability__revoke` (4758, 5156) size, `.matrix__history-link` (3983) size, `.identity-hue` (4295, 5176, 5183) and `.identity-glyph` (4308) sizes, `.login__links a` (6299), `.page--chrome .btn` in the 700px media (5144) and `.self-config-controls .btn` (6336).
- B3 fields: `.field input` (121), `.field select` (2820), `.settings-input`/`.settings-select` (4387, 5144), `.change-approvals select|input|textarea` (5824, 5835), `.stepup__form input` (5104), `.catalogue-manage__row-main input, .catalogue-manage__create input` (5463), `.matrix-key-create__type select` (2510, 2407), `.history-sheet input[type='date']` (3826), `.matrix-row-editor__fill textarea` (2378), `.field input:focus-visible` (130).
- B9: `.field__hint` (4123, if not already gone in B1).

Rules the map flags as strictly higher specificity (Appendix §3a) still get deleted when a block names them (that is what makes the block win); the ones the map lists as unnamed survivors stay.

- [ ] **Step 1: Delete, block by block, running `pnpm run test-storybook` after each block**
- [ ] **Step 2: Per-task checks; lower the budget**
- [ ] **Step 3: Commit**

```bash
git add web/src/styles/app.css web/scripts/design/adherence-budget.json
git commit -s -m "refactor(web): delete the type, button and field rules the design foundations supersede"
```

---

### Task 3: Delete the superseded alert, publish-sheet, folder-cleanup and badge rules (blocks B5, B6, B7, B12, B14)

**Files:**
- Modify: `web/src/styles/app.css`.

Named list (Appendix §2):
- B5: `.alert` (455) flex defaults, `.stepup .alert` (5110), `.notice` (2763), `.retention-warning` (1058, 1070, 1075) flex defaults only (keep its severity colours), `.alert__glyph` (465) and the toast tones (500, 513) where the trailing block restates them.
- B6: `.matrix__publish-env` children padding (1563, 1601, 1606, 1571, 2687), `.matrix__publish` (1541, 1551, 1558, 2726).
- B7: `.catalogue-manage__row-main` (5456) and its input rule (5463, if not already gone in Task 2).
- B12: `.badge` (172, 182, 187, 192, 196, 6247), `.chip` (2791, 2803, 4622, 4633, 4638), `.settings-tag` (4408, 4426, 5402, and the 5144 media copy), `.history__current` (3518), `.matrix__problem-count` (1687 and the duplicate at 2046: split `.matrix__count, .matrix__problem-count` so `.matrix__count` survives). Leave `.count` alone.
- B14: `.alert__glyph` colour (465) where `.notice .alert__glyph` restates it.

- [ ] **Step 1: Delete, block by block, running `pnpm run test-storybook` after each block**
- [ ] **Step 2: Per-task checks; lower the budget**
- [ ] **Step 3: Commit**

```bash
git add web/src/styles/app.css web/scripts/design/adherence-budget.json
git commit -s -m "refactor(web): delete the alert, publish, badge and chip rules the design foundations supersede"
```

---

### Task 4: Delete the superseded choice, dialog and fold rules (blocks B10, B15, B4)

**Files:**
- Modify: `web/src/styles/app.css`.

Named list (Appendix §2, §3):
- B10 choice: `.chk` (3273), `.chk input[type='checkbox']` (3279), `.chk label` (3286), the 800px media copy (3314), the coarse copy (5031), `.capitem .chk` (4836), `.audit__outcome-choice` (5551) size declarations.
- B15 dialog: `.ceremony` (2931, backdrop 2945, `> form` 2986, `__window` 6187), `.ceremony__title` (2949), `.ceremony__lede, .ceremony__scope` (2954), `.ceremony__actions` (2980), `.matrix-editor` (2200, backdrop 2213, form 2217, 800px copy 2733), `.matrix-editor__head h2` (2242, 2235), `.matrix-editor__head, .matrix-editor__actions` (2223: split so `__head` survives if the trailing block does not restate it). The trailing block RESTYLES these legacy classes to match `.dialog` (handoff §"Dialogs"), so what you delete is the earlier definition, not the class.
- B4 fold: only the `min-height: var(--touch)` declarations on the 23 selectors the fold's control-height sub-block (trailing section, "Control height" comment) lists explicitly, the `height: var(--touch)` on `.matrix__table thead th` (1932), and the `text-transform: uppercase` on the headings the `:root :is(h1, h2, h3)` rule covers (754, 901, 1555, 4088; NOT 4601 `.page--members .grants th` or 5892 `.adapters__adapter-head h2`, which the fold names separately or does not cover; check the trailing block's own list before each delete). Everything else in Appendix §3b stays.

- [ ] **Step 1: Delete, block by block, running `pnpm run test-storybook` after each block**
- [ ] **Step 2: Per-task checks; lower the budget**
- [ ] **Step 3: Commit**

```bash
git add web/src/styles/app.css web/scripts/design/adherence-budget.json
git commit -s -m "refactor(web): delete the checkbox, dialog and touch-height rules the design foundations supersede"
```

---

### Task 5: Retarget the desktop e2e density pins and correct the handoff

**Files:**
- Modify (18 pins, `'--touch'` to `'--control'`): `web/e2e/flows/login.spec.ts:67,96,140,182`; `matrix.spec.ts:351,435,1021`; `shell.spec.ts:506`; `members.spec.ts:679,718,744`; `machine-access.spec.ts:673,800`; `history.spec.ts:296`; `settings.spec.ts:586,842`; `reveal.spec.ts:416,449,753`; `scanning.spec.ts:157`.
- Leave unchanged: the seven `testInfo.project.name === 'mobile' ? '--touch' : '--row'` lines (`members.spec.ts:614,938,1061`, `instance-admin.spec.ts:1082,1114`, `machine-access.spec.ts:1035,1267`) and every comment.
- Modify: `docs/handoff/storybook-ui-consistency.md` "Constraints" list: `settings.spec.ts:575` is `:586`; add the six pins the list omits (`login.spec.ts:96/140/182`, `reveal.spec.ts:416/449/753`, `scanning.spec.ts:157`, `settings.spec.ts:842`); the "five rules app.css states with more specificity" note resolves to three rules (`.page--members .inspect select`, `.sidebar__link`, `.environment-lifecycle > summary`), say so. In §5 mark the layer-1 items (tokens, blocks moved, pins retargeted) done with this branch's commits; leave layer-2 items open for #762.
- Create: `docs/handoff/761-design-foundations-into-app.md`: what moved, what was deleted per task, what stayed (the §3b survivors and why), the ratchet number before and after, the pin list, and the preview checks still owed (issue item 4 and 5) for the controller to fill in.

- [ ] **Step 1: Edit the 18 pins**

`grep -n "'--touch'" web/e2e/flows/*.ts` afterwards must list exactly the seven conditional lines plus comments.

- [ ] **Step 2: Run the affected specs on both projects**

Run: `cd web && pnpm run build && pnpm exec playwright test --project=desktop e2e/flows/login.spec.ts e2e/flows/matrix.spec.ts e2e/flows/shell.spec.ts e2e/flows/members.spec.ts e2e/flows/machine-access.spec.ts e2e/flows/history.spec.ts e2e/flows/settings.spec.ts e2e/flows/reveal.spec.ts e2e/flows/scanning.spec.ts && pnpm exec playwright test --project=mobile e2e/flows/login.spec.ts e2e/flows/matrix.spec.ts e2e/flows/members.spec.ts`
Expected: green. The suite builds the Go binary itself when `HIKYO_E2E_BINARY` is unset (Go 1.27 is installed).

- [ ] **Step 3: Handoff edits, per-task checks, commit**

```bash
git add web/e2e docs/handoff
git commit -s -m "test(web): desktop density pins read --control; handoff lists every pin"
```

---

## Appendix: migration map (verified against dc8b8c41)

# Issue #761 , ui.css → app.css / tokens.css migration map

Built at commit `dc8b8c41`. All line numbers verified by grep/sed against the
working tree. `web/src/ui/ui.css` = 952 lines, `web/src/styles/app.css` = 6340,
`web/src/styles/tokens.css` = 198.

Convention below: `app.css:N` is the line the **selector** starts on (comments
stripped). `@media … @M` means the rule is nested inside the at-rule opening at
line M , deleting it is not the same as deleting a top-level rule.

---

## 1. Tokens

**The premise in the ticket is stale: `ui.css` declares no custom properties.**

- `grep -nE "^\s*--[a-z0-9-]+:" web/src/ui/ui.css` → 0 matches.
- `grep -n ":root\s*{" web/src/ui/ui.css` → 0 matches. Every `:root` in ui.css
  is a *descendant combinator* used purely to raise specificity
  (`:root :is(...)`), never a declaration block.
- The token move already happened in PR #755 , see
  `docs/handoff/storybook-ui-consistency.md:302` ("the `ui.css` tokens moved
  in"). `tokens.css:15-131` holds them.

All 48 custom properties ui.css consumes resolve from `tokens.css`, except one:

| property | tokens.css | property | tokens.css |
|---|---|---|---|
| `--accent` | 29 | `--on-accent` | 30 |
| `--accent-soft` | 41 | `--opacity-disabled` | 109 |
| `--badge-height` | 67 | `--overlay-shadow` | 102 |
| `--bg` | 19 | `--radius-badge` | 48 |
| `--bg-panel` | 21 | `--radius-container` | 46 |
| `--changed` | 35 | `--radius-control` | 47 |
| `--chk-box` | 73 (+188 coarse) | `--ring-color` | 108 |
| `--chk-gap` | 75 (+190 coarse) | `--ring-offset` | 107 |
| `--chk-visual` | 74 (+189 coarse) | `--ring-width` | 106 |
| `--control` | 65 (+187 coarse) | `--row` | 57 |
| `--danger` | 34 | `--space-1..6` | 89-94 |
| `--dur` | 130 (+196 reduced) | `--tracking-eyebrow` | 85 |
| `--ease` | 129 | `--tx` | 26 |
| `--font-mono` | 54 | `--tx-dim` | 27 |
| `--fs-xs/sm/md/lg/xl/mono` | 79/80/81/82/83/84 | `--tx-faint` | 28 |
| `--lh-tight/heading/body` | 112/113/114 | `--width-dialog` | 97 |
| `--line` | 23 | `--width-dialog-wide` | 98 |
| `--measure` | 99 | | |

- **`--choice-columns` (ui.css:716) is not a token** , set inline by
  `web/src/ui/ChoiceGroup.tsx:46` (grid layout only). Correct as is; do not
  move it to tokens.css.

**Tokens still to move (the real section-1 work):** `app.css` declares four
component-local custom properties that are not in tokens.css ,
`app.css:237 --sun`, `238 --moon`, `239 --moon-shine` (theme icon),
`4986 --qr-paper`. They are scoped, not `:root`, and are out of #761 scope
unless the ticket wants them tokenised.

**Required doc fix:** `DESIGN.md:54` still says `--fs-*` live "in
`web/src/ui/ui.css` until the migration moves them to `tokens.css`". False
today; must be corrected in the #761 PR.

---

## 2. Blocks

17 top-level blocks. "Supersedes" = named by the block's own header comment.

### B1 , Type scale · ui.css 16-106
Superseded: `.panel h2` text-transform/size → **app.css:4083** (also
**4188** `.page--chrome .panel h2`, a second definition at higher
specificity); `:focus-visible` 2px ring → **app.css:48**; `.btn[disabled]`
0.6 → **app.css:95**; per-surface uppercase `text-transform` (see §3);
`.field label` → **app.css:115** (`.field label, .field .field__label`);
`.field__hint` → **app.css:4123**.
Heading sizes it replaces: `.card h1, .card h2` **1158** (18px),
`.page h1` **4025** (20), `.page--chrome h1` **4151** (19), `.login__title`
**417** (20), `.matrix__head h1` **1498** (20), `.panel h3` **4094** (14),
`.sidebar__section h2` **751** (10), `.context-sidebar > h2` **897** (10).
Pure additions: `.eyebrow`, `.field__label`, `.field > label`,
`.choice-group > legend`, `.page__lede` (bare; app.css only has
`.page--chrome .page__lede` **4159**), `.settings-note` (**4255** exists),
`:where(a:not([class]))`.

### B2 , Buttons · ui.css 108-168
Superseded: `.btn` min-height → **app.css:63** (decl 68 `var(--touch)`);
`.page--chrome .btn` → **4500**; `.page--chrome .ceremony .btn,
.page--chrome .matrix-editor .btn` → **4507**; `.btn--icon` → **103**
(+ **1390** `@media (max-width:700px) @1264 .header > .btn--icon`);
`.btn--quiet` → **4130** (+ **5039** `@media (pointer:coarse) @5030
.explain__toggle, .btn--quiet`); `.tab` → **3021** (decl 3022);
`.menu__item` → **1189** (decl 1192).
Also: `.matrix__add-key` → **2013** `.matrix__group-row .matrix__add-key`
(+ **2718** in `@media (max-width:800px) @2658`); `.capability__revoke` →
**4758** (+ **5156** in `@media (max-width:700px) @5143`);
`.matrix__history-link` → **3983**; `.identity-hue` → **4295**
(+ **5176**, **5183** in the 700px media); `.identity-glyph` → **4308**;
`.login__links a` → **6299**.
Note `.btn` is also re-specified at **5144** (`@media (max-width:700px)
@5143`, `.page--chrome .btn`) and **6336** (`@media (max-width:640px)
@6334 .self-config-controls .btn`).

### B3 , Field controls · ui.css 170-214
Superseded: `.field input` → **app.css:121**; `.field select` → **2820**;
`.settings-input`/`.settings-select` → **4387** (+ **5144** in the 700px
media; siblings **4398**, **4404**, **4433** keep their own sizes);
`.change-approvals select|input|textarea` → **5824** (+ **5835**
`.change-approvals textarea`); `.stepup__form input` → **5104**;
`.catalogue-manage__create input` → **5463**
(`.catalogue-manage__row-main input, .catalogue-manage__create input`);
`.matrix-key-create__type select` → **2510** (+ **2407**, the 5-selector
key-create input rule); `.history-sheet input[type='date']` → **3826**;
`.matrix-row-editor__fill textarea` → **2378**;
`.field input:focus-visible` → **130**.
Pure addition: `:is(input,select,textarea).mono`.

### B4 , Whole-app fold · ui.css 216-378
Not a supersede list , a specificity-tie override of every app.css rule
still on `var(--touch)` or an off-scale px size. The full target set is in
§3 (61 `var(--touch)` decls, 179 raw `font-size: Npx` decls in app.css).
Sub-blocks:
- 222-258 control height (23 selectors) → app.css `var(--touch)` sites
  **1250** `.skip`, **1656** `.matrix__group-link`, **1575/1622**
  publish heading/confirmation, **1978** `.matrix__group-row button`,
  **2253** `.matrix-editor__close`, **2298** `.matrix-editor__schema
  summary`, **2848** `.values__keyname`, **3983** `.matrix__history-link`,
  **4056** `.jump__link`, **4546** `.environment-lifecycle > summary`,
  **5220/5283** import-wizard, **5824/5839** change-approvals,
  **504/517** toast, **4840** `.explain__toggle`, **5588** `.audit__row`,
  **782** `.sidebar__link`, **1512** `.matrix__legend-toggle.btn`,
  **1864** `.matrix-editor__copy label`, **2653** `.scan-block__locator`,
  **5122** `.sidebar__empty code`, **3290** `.ceremony__stepup`.
- 260-265 `.matrix__table thead th` → **app.css:1927** (height
  `var(--touch)` at 1932); the sticky offset reads `--gh` at **1972**.
- 267-344 type fold (≈70 selectors) → the off-scale `font-size` sites
  listed by owner in `/tmp` scan; every one is a raw px value in the
  179-line set (see §3 command).
- 346-358 `fieldset.field` , pure addition (no app.css `fieldset.field`).
- 360-365 `.chk input[type='number']` , pure addition.
- 367-378 the five higher-specificity rules , see §3.

### B5 , Alert and notice · ui.css 380-403
Superseded: `.alert` → **app.css:455** (+ **5110** `.stepup .alert`);
`.notice` → **2763**; `.retention-warning` → **1058** (+ **1070**,
**1075** severity variants); `.alert__glyph` → **465** (+ **500**, **513**
toast tones).

### B6 , Publish sheet · ui.css 405-429
Superseded: `.matrix__publish-env` children padding → **app.css:1563**,
`.matrix__publish-env ul` **1601**, `.matrix__publish-env li` **1606**
(+ **1571** blocked variant, + **2687** in `@media (max-width:800px)
@2658`); `.matrix__publish` → **1541** (+ **1551** `h2`, **1558** `> p`,
**2726** in the 800px media).

### B7 , Folder cleanup rows · ui.css 431-440
Superseded: `.catalogue-manage__row-main` → **app.css:5456**
(+ **5463** its input rule).

### B8 , Field control + reveal · ui.css 442-462
`.field__control`, `.field__control > input`, `.field__reveal` , **pure
addition**, zero app.css matches.

### B9 , Field hint and error · ui.css 464-491
`.field__hint` → **app.css:4123** (sizing already in B1).
`.field__error`, `[aria-invalid='true']` rules , **pure addition**.

### B10 , Choice controls · ui.css 493-664
Superseded: `.chk` → **app.css:3273**; `.chk input[type='checkbox']` →
**3279**; `.chk label` → **3286**; `@media (max-width:800px) @3301
.chk input[type='checkbox']` → **3314**; `@media (pointer:coarse) @5030
.chk input[…], .chk input[type='radio'], .field.chk input[…],
.audit__outcome-choice input[…]` → **5031**; local size rules ,
**4836** `.capitem .chk`, **5551** `.audit__outcome-choice`.

### B11 , ChoiceGroup · ui.css 666-753
**Pure addition** (`.choice-group*`). It does supersede the `label.chip`
markup (`.chip` → **app.css:2791**) but that is a #762 markup change, not a
rule deletion here.

### B12 , Badges · ui.css 755-800
Superseded: `.badge` → **app.css:172** (+ **182** `--danger`, **187**
`--warn`, **192** `[data-state='ok']`, **196** the 7-state list, **6247**
a second `[data-state='duplicate-identity']`); `.chip` → **2791**
(+ **2803**, **4622** `.page--members .chip`, **4633**, **4638**);
`.settings-tag` → **4408** (+ **4426** `--danger`, **5402** `--on`,
**5144** in the 700px media); `.history__current` → **3518**;
`.matrix__problem-count` → **1687** (`.matrix__count,
.matrix__problem-count`) **and** **2046** , defined twice.
Left alone by design: `.count`.

### B13 , Glyph · ui.css 802-813 , **pure addition** (`.glyph`).

### B14 , Alert tones · ui.css 815-823
`.notice .alert__glyph` overrides `.alert__glyph` colour at **app.css:465**.
Also flagged: `routes/Sections.tsx` `Done` has the same bug (#762).

### B15 , Dialog · ui.css 825-891
Superseded: `.ceremony` → **app.css:2931** (+ **2945** backdrop, **2986**
`> form`, **6187** `__window`); `.ceremony__title` → **2949**;
`.ceremony__lede` → **2954** (`.ceremony__lede, .ceremony__scope`);
`.ceremony__actions` → **2980**; `.matrix-editor` → **2200** (+ **2213**
backdrop, **2217** form, **2733** in `@media (max-width:800px) @2658`);
`.matrix-editor__head h2` → **2242** (+ **2235** shared rule);
`.matrix-editor__actions` → **2223** (`.matrix-editor__head,
.matrix-editor__actions`) , shared selector, split before deleting.
Pure addition: `.dialog`, `.dialog--wide`, `.dialog__title`,
`.dialog__lede`, `.dialog__actions`.

### B16 , Spacing · ui.css 893-901
`.card.panel` gap , **pure addition**. app.css defines `.card` **1147** and
`.panel` **4071** (+ **2673** in `@media (max-width:800px) @2658`) but no
`.card.panel` compound; the new rule (0,2,0) outranks both.

### B17 , Sign-in flow · ui.css 903-952
`.field.login__code input`, `.login__actions`, `.login__account`,
`.login__qr`, `.login__secret` , **pure addition**; only `.login__links`
(**6291**) / `.login__links a` (**6299**) pre-exist and are handled in B2.

**Pure-addition blocks: B8, B11, B13, plus `.dialog*` in B15, `.glyph`,
`.field__error`, `.eyebrow`, `.choice-group*`, `fieldset.field`,
`.chk input[type='number']`, and all of B17 except `.login__links a`.**

---

## 3. Conflicts

**Headline risk.** Every fold rule in B4 (and most of B1-B3) wins *today*
only because ui.css is loaded **after** app.css. Moving each block "beside
the rule it supersedes" places it **earlier** in app.css than many rules it
currently beats. Every equal-specificity win flips unless the superseded
rule is deleted in the same commit. This is the single largest correctness
hazard in #761 , the move and the delete must be atomic per block.

**Decision the parent must make, not the implementer:** ui.css comments
(lines 218-220, 511-514) say the `:root` prefix "can go" on migration.
Dropping it lowers specificity from (0,2,1)/(0,1,1) to (0,1,1)/(0,0,1) and
multiplies the conflicts below. Recommend keeping the prefixes in the first
PR and retiring them in a follow-up once the superseded rules are gone.

### 3a. Strictly higher specificity than the ui.css rule (survive the move)

| app.css | selector | beats | property |
|---|---|---|---|
| 4500 | `.page--chrome .btn` (0,2,0) | `.btn` (0,1,0) | min-height |
| 4507 | `.page--chrome .ceremony .btn, .page--chrome .matrix-editor .btn` (0,3,0) | `.btn` | min-height `--touch` (4509) |
| 1390 | `@media (max-width:700px) @1264 .header > .btn--icon` (0,2,0) | `.btn--icon` (0,1,0) | width/height (1396/1412-13 area) |
| 5144 | `@media (max-width:700px) @5143 … .page--members .inspect select` (0,3,0) | fold `:root :is(…)` (0,2,1) | min-height `--touch` (5150) |
| 5031 | `@media (pointer:coarse) @5030 … .field.chk input[type='checkbox']` (0,3,1) | `:root input[type='checkbox']` (0,2,1) | width/height `--touch` |
| 4687 | `.page--members .inspect select` (0,2,1) | field-control rule (0,1,1) | min-height 42, padding, 13.5px |
| 3826 | `.history-sheet input[type='date']` (0,2,1) | field-control rule (0,1,1) | min-height `--touch` (3827) |
| 4356 | `.field input.identity-hue-range` (0,2,1) | field-control rule (0,1,1) | sizing |
| 4188 | `.page--chrome .panel h2` (0,3,0) | `h3, .panel h2, .settings-panel h2` (0,2,0) | font-size 13px |
| 2046 | `.matrix__problem-count` , second definition, later than 1687 | badge rule | font-size/transform |

The "five rules app.css states with more specificity" (ui.css:367) resolve
to three app.css rules covering three selectors:
`.page--members .inspect select` **app.css:4687** (+ the 700px copy at
**5144**), `.sidebar__link` **app.css:782** (`padding: 9px 13px`,
`min-height: 38px`), `.environment-lifecycle > summary` **app.css:4546**
(`padding: 10px 0`, `min-height: var(--touch)` at 4547). The comment's
count of five appears to count selectors + media copies; flag as a
docs inaccuracy to fix in the PR.

### 3b. Equal specificity, order-dependent (break on move unless deleted)

Discriminator commands, all run and counted:

```sh
grep -c "var(--touch)" web/src/styles/app.css            # 61
grep -c "text-transform" web/src/styles/app.css          # 22
grep -cE "font-size: *[0-9.]+px" web/src/styles/app.css  # 179
```

- **`var(--touch)` , 61 declarations.** Each is either a rule a block names
  as superseded (delete) or an unnamed survivor. Unnamed survivors that the
  fold does NOT list and that therefore keep `--touch` after #761:
  `.avatar` **1412/1413** (700px media), `.matrix__group-row th`
  **1970**, `.matrix-cell` **2698**, `.matrix__key-row > th|td` **2704**,
  `.matrix__key` **2708-2711**, `.matrix__drafts` **2723**,
  `.matrix__environment-picker input, .matrix-editor__copy input,
  .matrix__publish input` **2729-2730** (all in `@media (max-width:800px)
  @2658`), `.history__settings-pointer` **5051**,
  `a.settings-row__title, a.settings-row__detail` **5173**,
  `.sidebar__switcher, .settings-row__title--link, .settings-row__detail a,
  .identity-hue` **5180**, `.matrix__key-cell > .matrix__key` **6136**
  (`@media (pointer:coarse) @6134`).
- **`text-transform: uppercase` , 19 of the 22.** Owners:
  **154** `.field__readonly-tag`, **737** `.sidebar__version-label`,
  **754** `.sidebar__section h2`, **901** `.context-sidebar > h2`,
  **1555** `.matrix__publish h2`, **1992** `.matrix__group-row button`,
  **2290** `.matrix-editor__eyebrow`, **3122** `.machine__subhead`,
  **3247** `.journey__state`, **4088** `.panel h2`, **4206**
  `.page--chrome .settings-grid label, … .identity-controls__range label`,
  **4418** `.settings-tag`, **4601** `.page--members .grants th`,
  **4684** `.page--members .inspect label`, **5353** `.key-detail__*-editor
  legend`, **5388** `.key-detail__presence-impact-title`, **5892**
  `.adapters__adapter-head h2`, **6179** `.matrix__publish-group-name`.
  `:root :is(h1,h2,h3)` (ui.css:60, (0,1,3)) beats the bare-element ones
  but **loses to** `.page--members .grants th` (0,2,1) and
  `.adapters__adapter-head h2` (0,1,1 , ties, order decides).
  Already `none` at **2026**, **2050**, **2060**, **6106**.
- **`font-size: Npx` , 179 declarations**, the type fold's target set. All
  owners enumerated by:
  `grep -nE "font-size: *[0-9.]+px" web/src/styles/app.css`.
  The adherence budget (`web/scripts/design/adherence-budget.json`) starts
  app.css at 798 literals and only ratchets down.

---

## 4. Storybook

- **`web/.storybook/preview.tsx:11`** , `import '../src/ui/ui.css'`
  (after `tokens.css` line 9 and `app.css` line 10). Delete this line; the
  cascade order it creates is what every ui.css override currently relies on.
- **No story or test file imports `ui.css`.** Repo-wide grep
  (`grep -rn "ui\.css" --exclude-dir=node_modules .`) finds only:
  - `web/.storybook/preview.tsx:11` , the import.
  - `web/scripts/design/adherence-budget.json:2` , `"src/ui/ui.css": 0`.
    **Must be removed with the file**, or `design:check` reads a budget for
    a stylesheet that does not exist.
  - `web/scripts/design/adherence-check.ts:2` , comment naming ui.css.
  - `web/src/ui/Checkbox.tsx:7` , doc comment ("ui.css draws the box").
  - `DESIGN.md:54` , stale token-location claim (see §1).
  - `docs/handoff/storybook-ui-consistency.md` lines 9, 19, 27, 28, 77, 105,
    123, 302, 312, 323, 366-367, 385; `docs/handoff/storybook-setup.md:158`.

---

## 5. e2e , every `'--touch'` under `web/e2e/flows/`

29 total matches for `--touch` in `web/e2e/`; one is the doc comment at
`web/e2e/fixtures/assertions.ts:632`. The 28 under `flows/` split 20 / 8 (7 conditional lines + 1 comment).

### (a) Desktop density pins that must become `'--control'` (20)

Handoff §"Constraints" (lines 88-94) names 12; all 12 are desktop
button/input pins:

| file:line | target | in handoff list |
|---|---|---|
| `login.spec.ts:67` | `submit` = button "Sign in" (34) | yes |
| `matrix.spec.ts:351` | button "Close row editor" | yes |
| `matrix.spec.ts:435` | `chooser` = `.matrix__environment-picker summary` (412) | yes |
| `matrix.spec.ts:1021` | `close` = `.key-detail__close` (1000) | yes |
| `shell.spec.ts:506` | `theme` = button /theme/i (456) | yes |
| `members.spec.ts:679` | dialog button "Cancel" | yes |
| `members.spec.ts:718` | composition button "Cancel" | yes |
| `members.spec.ts:744` | button "Back, change scope" | yes |
| `machine-access.spec.ts:673` | `mint` = mint button (649) | yes |
| `machine-access.spec.ts:800` | dialog button "Done" | yes |
| `history.spec.ts:296` | `tab` = `.history__tab` (258) , markup is `btn history__tab` (`HistoryDrawer.tsx:556`), so a `.btn` pin | yes |
| `settings.spec.ts:586` | `#project-metadata input` first | yes , but handoff writes **`settings.spec.ts:575`**, off by 11; correct it |

**Eight pins the handoff list omits, all desktop button/input pins that
must also retarget:**

| file:line | target | verdict |
|---|---|---|
| `login.spec.ts:96` | `close` = button "Close this window" (81) | desktop button pin → retarget |
| `login.spec.ts:140` | `submit` = button "Establish credential" (105) | desktop button pin → retarget |
| `login.spec.ts:182` | `submit` = button "Continue" (149) | desktop button pin → retarget |
| `reveal.spec.ts:416` | dialog button "Use a passkey" | desktop button pin → retarget |
| `reveal.spec.ts:449` | `field` = `getByLabel("New value for …")` (423) , an input | desktop input pin → retarget |
| `reveal.spec.ts:753` | `revealAll` = "Reveal all" button (729) | desktop button pin → retarget |
| `scanning.spec.ts:157` | `chooser` = `.matrix__environment-picker summary` (134) | same locator as `matrix.spec.ts:435` → retarget |
| `settings.spec.ts:842` | `control` (814) = `#org-identity input:not([type=range]):not([type=file])` or `#project-metadata input` | desktop input pin → retarget |

Four of the eight (`login.spec.ts:96/140/182`, `settings.spec.ts:842`) sit
on surfaces the handoff already names, so its "login submit" and "metadata
input" entries are under-counted rather than absent. **Net: 20 pins
retarget.**

### (b) Mobile-conditional row density , leave unchanged (7)

Each reads `testInfo.project.name === 'mobile' ? '--touch' : '--row'`, so
the desktop branch is already `--row` and is unaffected by #761:

`members.spec.ts:614`, `members.spec.ts:938`, `members.spec.ts:1061`,
`instance-admin.spec.ts:1082`, `instance-admin.spec.ts:1114`,
`machine-access.spec.ts:1035`, `machine-access.spec.ts:1267`.
Plus the explanatory comment at `members.spec.ts:611`.

`expectDensity` (`assertions.ts:698`) reads the token name off `:root` and
compares the element's computed box, so retargeting is a pure token-name
swap , no fixture change needed.

---

## 6. Required non-CSS steps in the same PR

1. Delete `web/.storybook/preview.tsx:11`.
2. Delete the `"src/ui/ui.css": 0` entry from
   `web/scripts/design/adherence-budget.json` (line 2) and update the
   comment at `web/scripts/design/adherence-check.ts:2`.
3. Fix `DESIGN.md:54` (tokens already live in `tokens.css`).
4. Update `web/src/ui/Checkbox.tsx:7` doc comment.
5. Retarget the 20 e2e pins in §5a; correct the handoff's
   `settings.spec.ts:575` → `:586` and add the 8 missing pins to its list.

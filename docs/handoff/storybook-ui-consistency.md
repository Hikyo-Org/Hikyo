# Storybook UI consistency: audit, proposal, and what needs approval

Status: on PR #755. Approved: checkbox (1A), auth direction (2B strict, 3A), control tiers (4a), type scale (4b), badge (4c). Signed off 2026-09-16 (1a): ChoiceGroup, Field hint/error, Alert, Dialog, the spacing fold, the 24px fine-pointer hit box. Panel titles sentence case (2a). The e2e port race is fixed in this PR (3b, `web/e2e/fixtures/instance.ts` header). Scope per Marc (2026-09-16): Storybook only.
Nothing under `web/src/routes`, `web/src/app` or `web/src/styles` changed;
the app renders exactly as before. The work lives in `web/src/ui/**` and
`web/.storybook/preview.tsx`, and migrates into the app with the rest of the
component move.

Run: `cd web && pnpm storybook`, open `ui/`. `src/ui/ui.css` was unscoped
(#761 moved it into `src/styles/app.css` and deleted the file);
the comparison scaffold (Design toolbar, `compare.tsx`, `CurrentVsProposed`
stories) is removed. The work lives in `web/src/ui/**`, `web/.storybook/`
(`main.ts`, `preview.tsx`, `withApp.tsx` comment cleanup), and
`web/scripts/audit-styles.mjs`.

## Status by group

| Group | Status | Where |
|---|---|---|
| Checkbox, Radio | APPROVED (1A), unscoped; the rules now live in `src/styles/app.css` | `ui/Checkbox`, `ui/Radio` |
| Auth flow: challenge, enrolment gate | APPROVED direction (2B strict, 3A), built, backend spec below | `ui/auth/*` |
| Control height tokens, Button, Input, Select, Textarea | APPROVED (4a), unscoped | `ui/Button`, `ui/Input`, `ui/Textarea` |
| Type scale, eyebrow, captions | APPROVED (4b), unscoped | `ui/Typography` |
| Badge (folds chip, settings-tag) | APPROVED (4c), unscoped | `ui/Badge` |
| ChoiceGroup | APPROVED (1a) | `ui/ChoiceGroup` |
| Field (hint, error), Alert | APPROVED (1a) | `ui/Input` WithHint/WithError/ErrorIsWired, `ui/Alert` |
| Dialog (folds `.ceremony` and `.matrix-editor`) | APPROVED (1a) | `ui/Dialog` |
| Spacing | APPROVED (1a) | Spacing block in `src/styles/app.css` |
| Tabs atom, Glyph atom, compact in-row sizes, identity controls, login links, menu rows | built ("do all", 2026-09-16) | `ui/Tabs`, `ui/Glyph`, `src/styles/app.css` |
| ChoiceGroup layouts, columns, chips; Checkbox/Radio mono | built (1B, 2026-09-16) | `ui/ChoiceGroup` AllStates |
| Light-theme a11y run in CI | added (`test-storybook:light`, ci.yml storybook job) | |
| Design system: one token file, spacing/measure/layer tokens, adherence check, Tokens page | built (decision A, 2026-09-16) | `src/styles/tokens.css`, `scripts/design/adherence*.ts`, `ui/Tokens` |
| Migration into app routes | NOT STARTED (§5) | |

## 1. Audit

### Checkbox root cause (measured with Playwright)

| Where | Fine pointer, 1280 wide | Fine pointer, 700 wide | Coarse pointer |
|---|---|---|---|
| Storybook atom `Checkbox` (`.field.chk`) | 24 x 44 | 44 x 44 | 44 x 44 |
| Remotes radios (`div.chk` in `fieldset.field`) | 13 x 44 | 13 x 44 | 44 x 44 |
| Matrix environment picker (own 22px rule) | 22 x 22 | 44 x 44 | 44 x 44 |
| Audit outcome filter (own `min-height: 0`) | 13 x 13 | 13 x 13 | 44 x 44 |

1. `.field input` (app.css:121) gives every input `min-height: 44px`, side
   padding and a border; the checkbox in `.field.chk` inherits the
   min-height (`.chk input` sets `height`, not `min-height`). Atom = 24 x 44;
   bare `.chk` in routes = 24 x 24. That is the atom-vs-route mismatch.
2. `@media (max-width: 800px)` (app.css:3314) bumps to 44px by viewport
   width, not pointer. Storybook's preview pane is usually under 800px.
3. Four local size rules (22/18/20px, `min-height: 0`); `InstanceConfig` and
   `Adapters` render raw inputs with no `.chk`.
4. Chromium's native checkbox cannot separate hit box from drawn box
   (padding is ignored, probed), so the 44px floor forced a 44px visual.

### Whole-app audit (every shell route, prototype mock API, 1280 and Pixel 5)

Computed styles of every visible button, field control, label, heading,
badge, link and text node, grouped by signature. Full table in the audit
output (`web/scripts/audit-styles.mjs` regenerates it against `pnpm prototype`). Findings on a fine
pointer:

- **Buttons**: `.btn` is 44px on most pages, 36px on `.page--chrome` pages
  (a deliberate tier, app.css:4500, with 44px reserved for dialogs). Quiet
  32px. Tabs 44px. In-row actions 26 to 27px with their own rules.
- **Field controls**: input 44px, `settings-input` 36px at 13px type,
  select 44 / 42 (members, padding) / 36px, textarea unstyled outside
  change-approvals.
- **Headings**: h1 at 18, 19, 20px; h2 at 10 (sidebar, uppercase, x32),
  13 (panel), 14, 15, 17, 18px; h3 at 14, 16px.
- **Labels / legends**: 14 ink, 13 dim, 12 dim 500, 10 and 11px uppercase
  bold; legends 12 to 14px.
- **Badges**: `.badge` 12px, `.chip` 26px 11px, `.settings-tag` 32px 10px
  uppercase bold, `.count` pill 10px, problem count 11px. Five vocabularies
  for one job.
- **Links**: one unclassed `<a>` on the overview page in browser blue
  (`rgb(158,158,255)`). The design foundations in `web/src/styles/app.css`
  give unclassed anchors body ink and an underline (`:where()`, so classed
  links keep their rule); the route gets it on migration.
- Coarse pointer is consistent at 44px except `identity-glyph` at 38px
  (settings; touch floor candidate).

### Constraints (from `web/e2e/fixtures/assertions.ts`)

- The pinned sweep measures the INPUT's own box (>= 44 on coarse) and reads
  the focus ring from the element's own `outline`; forced colours need a
  non-none `outline`. Hit box and ring stay on the input element.
- Density pins read `--touch` on desktop for: login submit and the three other
  login buttons, editor close, environment chooser (matrix and scanning),
  theme toggle, dialog Cancel/Back/Done, machine-access mint, history tab,
  the reveal dialog and its value input, and the two settings inputs. That is
  **twenty** pins under `web/e2e/flows/`, not the twelve this list first
  counted: `login.spec.ts:67/96/140/182`, `matrix.spec.ts:351/435/1021`,
  `shell.spec.ts:506`, `members.spec.ts:679/718/744`,
  `machine-access.spec.ts:673/800`, `history.spec.ts:296`,
  `settings.spec.ts:586/842` (the first was listed as `:575`, off by 11),
  `reveal.spec.ts:416/449/753`, `scanning.spec.ts:157`. A twenty-first pin,
  `shell.spec.ts:86` (sidebar link), was a LITERAL `'38px'` rather than a
  `--touch` read, so the sweep missed it; #761 deletes the dead
  `min-height: 38px` on `.sidebar__link` and has the test read `--control` off
  `:root`. Row density already
  switches token by project (seven `mobile ? '--touch' : '--row'` lines, left
  alone). #761 retargeted all twenty to `--control`; on a coarse pointer
  `tokens.css` resolves `--control` to `--touch`, so the mobile project is
  unchanged.
- The "five rules app.css states with more specificity" note is now a
  three-rule block in the design-foundations section of
  `web/src/styles/app.css`, and only one of the three is a specificity
  override. `.page--members .inspect select` is stated at (0,2,1) by app.css
  (plus a `max-width: 700px` copy), which outranks the (0,1,1) field-controls
  foundation, so it has to be restated. `.sidebar__link` and
  `.environment-lifecycle > summary` are not outranked: the control-height
  list reaches (0,3,0) through `.matrix__legend-toggle.btn` inside its
  `:is()`, so the height already lands. Those two rules exist only to zero
  the block padding app.css gives them (`9px 13px` plus a `max-width: 700px`
  copy on the link, `10px 0` on the summary) and to centre the text; they are
  written separately so the summary never gets a `display`, which would drop
  its disclosure marker. The count of five was counting selectors plus media
  copies.

### Dialogs (every dialog story, 1280 wide)

Two families. `.ceremony` (520px): h2 20/700, lede 14px full ink, action
row gap 12, no shadow. `.matrix-editor` (760px): h2 15/700, lede 13px dim,
action row gap 8, shadow. Primary button position varies (first in some,
last in others). DESIGN.md reserves shadows for modal overlays, so the
shadowless ceremony is the one off the rule. `ConsequencesDialog` sits
between at h2 16. `ui/Dialog` is the one anatomy: `narrow` 520 / `wide`
760, title h2 on the scale (16/700), lede caption (13 dim), actions gap 8
with the primary LAST, shadow on both. The legacy classes are restyled to
match in the design-foundations section of `web/src/styles/app.css` so
unmigrated route dialogs read the same.

### Spacing (padding and gap on every route)

Already 4-based and mostly held: panels 16/18 with gap 12, forms 12,
fields 6, settings rows 10, page sections 18, action rows 8. Outliers:
two panels at gap 8 and 11, ceremony actions at 12. Folded to 12 and 8;
no new spacing tokens, the existing values are the scale.

### Prototypes (`web/prototype/directions/`)

Not adopted; the README says nothing ships and every direction changes the
structure the brief excludes (drops the rail, bans Instrument Sans,
light-first, single theme). What is adoptable is their diagnosis: a button
hierarchy instead of identical outlined buttons (applied: `danger` and
`quiet` variants), no ledes that restate the heading, counts over ids (route
copy, next slice).

## 2. Approved: Checkbox and Radio (unscoped; authored in `ui.css`, now in `app.css`)

`appearance: none`; the input stays the hit box (24px fine, the WCAG 2.5.8
minimum, raised from 18px after review; 44px coarse), the drawn box is a
pseudo-element (18 / 20px, decision 1A) with the control radius,
`--line` border, `--accent` fill, `--on-accent` masked check; focus ring
pulled in with a negative `outline-offset`; hover (enabled only), disabled
0.6, indeterminate (checkbox only), forced colours fall back to native. The
44px floor keys on `(pointer: coarse)` only. Labels 14px ink in both markup
shapes. `Radio` atom emits the same row. `ChoiceGroup` wraps rows in a
fieldset with a legend.

Migration deletes in app.css: `.chk` and `.chk input[type='checkbox']`
(3273-3284), `.chk label` (3286-3288), the `(max-width: 800px)` checkbox
bump (3314-3318), the checkbox lines of the `(pointer: coarse)` block
(5031-5037), and the four local size rules (`.matrix__environment-picker
input` 1880-1885, whose selector list also covers `.matrix-editor__copy
input`, which keeps the `.chk` treatment on migration, `.matrix-key-create__presence-modes input` 2491-2495,
`.matrix-key-create__secret input` 2520-2524, `.audit__outcome-choice
input` 5554-5556); and wraps the raw inputs in `InstanceConfig.tsx:178` and
`Adapters.tsx:1161/1185/1607`.

## 3. Approved direction: sign-in flow (`ui/auth`)

Rules locked 2026-09-16:

- Password sign-in on an account with a factor enrolled: `SecondFactorChallenge`
  (authenticator code, or passkey assertion). **Never skippable.** The story
  `NoSkipControl` asserts no skip, later, or link exists.
- Password sign-in on an account with nothing enrolled, instance policy
  `require-second-factor`: `SecondFactorSetup` gate. Choose authenticator
  (QR, secret, confirm one code) or passkey; then recovery codes shown once
  and acknowledged. **No way past it** until a factor stands.
  Policy `allow-unenrolled`: the password is the whole ceremony.
- Passkey sign-in: primary authentication with multi-factor assurance,
  worded "Sign in with a passkey", never a second step.
- Identity provider sign-in: assurance is the provider's (`acr`/`amr`
  policy); no local factor asked. `StepUpBanner` stays for such sessions
  (3A) but its title "This session is password-only" is wrong for them;
  the copy fix is deferred to migration because the route is outside the
  Storybook-only scope.

`ui/auth/LoginFlow` walks all of it with mocked transport (password
`correct`, code `123456`); six play tests assert the sequencing.

### Backend work this needs (ticket-grade; Storybook mocks it until then)

Today `localLoginOp` mints the browser session at password assurance and
`stepUpTotpOp` / `stepUpPasskeyFinishOp` elevate it afterwards. The flow
above is therefore SEQUENCING only until the server enforces it. Needed:

1. **Login returns a challenge, not a session, when a factor stands.**
   `POST /auth/login` with `artifact: browser` answers
   `{ challenge: { id, expires_at, factors: ['totp' | 'webauthn'] } }`
   instead of `{ session }` for an account with an enrolled factor. The
   challenge is random, single-use, expiring, bound to the account and to
   the purpose `login` (same discipline as the WebAuthn challenges in the
   ADR). No cookie is set.
2. **Challenge-bound finish operations that mint.**
   `POST /auth/login/challenge/{id}/totp` `{ code }` and
   `POST /auth/login/challenge/{id}/webauthn/{start,finish}` consume the
   challenge and mint the session with the factor recorded in the
   assurance. Refusals mirror `stepUpFailureText` (401 wrong code, 409
   replayed time step, 429).
3. **Enrolment-required session state.** For an unenrolled account under
   `require-second-factor`, login mints a session flagged
   `enrolment_required`. The authorization chokepoint refuses every
   capability for such a session except: TOTP begin/confirm, passkey
   registration, recovery-code generation, `whoami`, `logout`. `whoami`
   exposes the flag so the SPA renders the gate and nothing else.
4. **Instance policy field.** `second_factor: 'required' | 'optional'` on
   instance configuration, operator-set, default `required` on a fresh
   install (greenfield = strict). Optional preserves the current local
   floor.
5. **ADR amendment** (`docs/adr/human-auth.md`, "Assurance" and
   "Account-security mutations"). Text it changes:
   - "A `viewer` on a development environment is not forced to enrol"
     becomes conditional on the instance policy.
   - The step-up banner's premise ("the login page asks for nothing
     else, by design: the local floor must work with no second factor
     enrolled") holds only under `optional`.
   Text it must NOT weaken: "A new credential may never authorize its own
   enrolment." The setup gate runs on a session that has just presented a
   password, which is the authority the enrolment endpoints already
   require; the flag restricts that session, it does not widen it.
6. **SPA wiring** (app change, migration slice): `/login` while
   authenticated is a hard `Navigate` (`App.tsx:203`); it becomes a gate
   that renders the challenge or setup while `whoami` reports a pending
   challenge or `enrolment_required`.

## 4. Approved 2026-09-16 (4a, 4b, 4c all option a)

### 4a. Control height (Button, Input, Select, Textarea)

Amended 2026-09-16 after review in Storybook: ONE height, no per-surface
override ("if the button is styled a certain way, we don't override the
height"). Dialogs, ceremonies and the sign-in card use `--control` like
page content: 36px on a fine pointer, 44px on a coarse one. Option (a)
below is superseded on that point; its tokens and the compact tier stand.
Migration consequence: EVERY desktop density pin that asserts `--touch`
on a button or input (login submit, editor close, chooser, theme toggle,
dialog Cancel/Back/Done, mint, history tab, metadata input) moves to
`--control`; the mobile project keeps `--touch` through the coarse token.

Options:
- (a) **Two tiers, tokenised.** `--control` = 36px page content on a fine
  pointer, `--touch` on coarse; `--touch` (44px) inside dialogs, ceremonies
  and the sign-in card on every pointer; `--control-compact` 28px for
  `.btn--quiet`. Formalises what `.page--chrome` already does and what the
  dialog e2e pins already assert.
- (b) 36px everywhere on a fine pointer, including dialogs and login.
- (c) 44px everywhere (today's default outside settings pages).

Decision: **(a)**. It matches DESIGN.md density (36 desktop, 44
touch), keeps the login and dialog pins untouched, and collapses the 42px
members select and the 36px `settings-input` into the same token.
Migration: e2e density pins for page-content buttons read `--control` on
the desktop project the way row pins read `--row` (all of the pins listed under Constraints; the mobile project is unchanged).

Migration risks to check on the real screens (no story renders them under
the proposal yet): `.matrix__history-link` (a column-header control on
`.btn`) goes from 11.5px to 14px under the button rule; `.settings-tag`
drops from 32px to 20px inside settings rows that align to 36px inputs.
Verified on stories: the Login route keeps 44px inputs and buttons, the
Projects route drops to 36px, `input.mono` keeps 13px, `.login__links a`
keeps its own rule.

### 4b. Type scale

11 eyebrow and badge / 13 caption, label, legend, lede, hint, mono / 14 body,
h3, panel title / 16 h2 / 20 h1. Weights 400, 500 (eyebrow, badge), 700
(headings). Eyebrow: uppercase, 0.06em tracking, `--tx-faint`. Folds the
10px sidebar h2s (x32), the 15/17/18/19px headings and the 10/11px
uppercase labels. Panel titles are sentence case (2a, replacing app.css's uppercase
`.panel h2`; only the eyebrow is uppercase); DESIGN.md's "scale ratio >= 1.25" line is replaced by the fixed
scale, which the 13/14/16 steps did not satisfy; making panel titles sentence case (the prototype brief's
"uppercase eyebrow on every block" diagnosis) is a separate decision.

### 4c. Badge

One atom, 11px / 500 / badge radius / 1px 6px padding / 20px min-height,
tones `neutral | danger | changed (warn alias) | ok`, `mono` modifier.
`.chip` and `.settings-tag` fold in (their markup renders with the proposed
rules in the compare story). `.count` pill unchanged (DESIGN.md exception).

## 4d. Remaining consistency items ("do all", 2026-09-16)

- Tabs: `ui/Tabs` (APG keyboard model, `.tabs`/`.tab` markup) on `--control`;
  supersedes app.css `.tab` min-height. MachineAccess migrates to it.
- Menu rows: `.menu__item` on `--control` (was the touch floor).
- In-row micro controls folded onto the tokens: `.matrix__add-key` and
  `.capability__revoke` on `--control-compact` at caption size,
  `.matrix__history-link` at caption size, `Button icon variant="quiet"` is
  the atom they migrate to.
- Identity controls: `.identity-hue` and `.identity-glyph` on `--control`
  (the glyph was 38px on a coarse pointer, under the floor).
- Sign-in quiet links: `.login__links a` on `--control`.
- Glyph: `ui/Glyph` renders the state vocabulary as monochrome inline SVG in
  the current colour; routes replace 🔒 (12 sites) and 🔗 (2) and the text
  glyphs ✓ ✕ Δ ◌ ⋯ on migration. DESIGN.md updated.
- Every checkbox and radio, not only `.chk` rows: a sweep of all 111 route
  stories found raw inputs (`label.chip` in TargetForm, the audit filter,
  the adapter and folder dialogs, the key-create sheet) still on native or
  leaked sizes. The box rules now match `input[type='checkbox']` and
  `input[type='radio']` anywhere; `.chk` keeps the row layout. Sweep after:
  zero off-size inputs (`web/.xreview/chk-sweep.mjs`, disposable).
- ChoiceGroup layout (decided 2026-09-16, 1B): `layout` stack | wrap | nowrap
  (one line, horizontal scroll), `columns` N (grid, one column under 480px of
  container width), `variant="chips"` for dense key sets (supersedes the
  `label.chip` checkbox markup in the adapter TargetForm), `Checkbox` and
  `Radio` gain `mono`. Layout is a prop, never an array of rows.
- Light theme in CI: `pnpm run test-storybook:light` sets `STORYBOOK_THEME`,
  which `preview.tsx` reads into the initial theme global; the ci.yml
  storybook job runs both. Verified that the light run really renders light
  (a probe story asserting `data-theme=light` passed under the env and
  failed without it).

## 4e. Design system (decision A, 2026-09-16)

- `src/styles/tokens.css` is the single source: the `ui.css` tokens moved in
  (`--control`, `--control-compact`, `--badge-height`, `--hit-min`,
  `--chk-*`, `--fs-*`, `--tracking-eyebrow`, `--overlay-shadow`) with the
  coarse-pointer overrides, plus a 4-based spacing scale `--space-1..6`,
  measures (`--width-dialog`, `--width-dialog-wide`, `--measure`) and named
  layers `--z-chrome/sticky/drawer/overlay`. `design:seed` mirrors them into
  `design/hikyo.pen` (50 variables); `design:check` proves no drift.
- DESIGN.md "Tokens" states the rule per family.
- `design:check` now also runs `scripts/design/adherence-check.ts`: it counts
  literal px sizes on token-covered properties per stylesheet against
  `scripts/design/adherence-budget.json`, a ratchet. `ui.css` was held at 0
  (its 24 literals were converted) and its key went with the file in #761;
  `app.css` started at its measured count (798) and the budget only goes down
  as #761/#762 retire rules (728 at the head of #761). Hairlines
  (1px) and the 2px ring are exempt. Unit-tested (`adherence.test.ts`).
- `ui/Tokens` (Storybook) lists every token with its live value per theme and
  the family rule, from `src/ui/tokens.ts`; its play test fails when a listed
  token stops resolving.
- Chips stand at `--control` (a chip beside a button lines up).

## 4f. Live review pass (Marc in Storybook, 2026-09-16)

Findings and fixes, all in `ui.css`, `.storybook/preview.tsx` or story files:
- Sizes everywhere: a 232-story sweep (`.xreview/size-sweep.mjs`, controls,
  type, radii against the tokens) found jump links, summaries, tabs, menu
  rows, toast buttons, sidebar links, the matrix header row and ~60 rules at
  12/11.5/10/15/17/18/19px. All folded onto the tokens in the "Whole-app
  fold" block; sweep now reports zero. Every input, select and textarea
  takes the control rule wherever it sits. `--control-compact` retired:
  quiet buttons differ in weight and padding, never height (Revoke
  credential vs Revoke connection had 28 next to 36). Buttons never wrap.
- Docs pages: the docs theme and canvas background now use the app surface
  tokens, so a sticky strip painted `--bg` (the jump index) no longer reads
  as a dark bar with flush buttons.
- Alert: `action` slot; glyph, text and action centre on one line and the
  action drops under on narrow (ProviderDiscoveryAlert, the retention
  "Replace with whole days" warning). Headings never uppercase.
- Publish sheet: key rows and state align to the environment name; the two
  full-width buttons became an action row (Close, then Publish).
- Folder cleanup rows: checkbox, label, input as a grid so inputs line up.
- Input `revealable` for password fields (show/hide, aria-pressed).
- ProfileUpdateBadge story mounts the dot on an avatar; HealthChip stories
  keep bigint fixtures out of the docs args table; Badge drops the `warn`
  alias (`changed` is the tone).
- Open, need a decision or a route change: MatrixRowEditor density
  ("cramped"), publish sheet copy (what the rows mean), MintConnectionForm's
  inline "Expires after [n] days" sentence (route markup), FleetUpdateNotice
  "no updates" story (not a component, drop on migration), Dialog close X
  (recommendation: no; Escape and the Cancel action are the two ways out).

## 5. Migration into the app (next, outside the Storybook-only scope)

Decided 2026-09-16: #755 stays Storybook-only and merges as is; the
migration is its own PR series. Tickets: #761 (layer 1, CSS and tokens
into the app, e2e pins retargeted, preview checks with real content),
#762 (layer 2, route markup onto the atoms, plus the route copy findings),
#760 (backend enforcement of the second factor at sign-in, §3).

Known risks on migration, to verify on the real screens: `.matrix__history-link` grows from 11.5px to 14px under the button rule; `.settings-tag` drops from 32px to 20px inside settings rows aligned to 36px inputs; `StepUpBanner` title is wrong for provider sessions; the browser-blue unclassed link on the overview page.

Also superseded by `ui/`: `routes/Sections.tsx` `Alert` and `Done` (by
`ui/Alert`), `routes/useModalDialog.ts` `useModalDialog` (by
`ui/useModalDialog.ts`; `useFeedback` stays), `routes/AccountSecurity.tsx`
`QrCode` (by `ui/auth/QrCode.tsx`).

Move each `ui.css` block into app.css beside the rules it names as
superseded and delete those; move the `:root` tokens in `ui.css` into
`tokens.css`; swap route markup to the `ui/` atoms; retarget
the desktop density pins listed under 4a; fix the browser-blue link and the
`StepUpBanner` copy; wire `/login` to the challenge and setup gates once
the backend in §3 exists.

**Layer 1 (#761) is done** on branch `feat/761-design-foundations-app`; see
[761-design-foundations-into-app.md](./761-design-foundations-into-app.md) for
the full account.

- Tokens: nothing to move. `ui.css` declared no custom properties; the token
  move happened in #755 and `web/src/styles/tokens.css` already held all 48.
  The stale claim in `DESIGN.md:54` is corrected. (`312251e9`)
- Blocks moved: `ui.css` appended to `web/src/styles/app.css` under a
  provenance banner and the file deleted, with its Storybook import and its
  budget key. (`312251e9`)
- Superseded rules deleted, block by block, each delete checked against the
  trailing section that restates it. (`d216d101`, `9df304f6`, `8dffd823`,
  `4063f417`, `e7956597`)
- Pins retargeted: all twenty desktop density pins now read `--control`; the
  dead `(max-width: 800px)` matrix checkbox bump is gone. (`981e531b`)

Layer 2 (#762) still open: route markup onto the `ui/` atoms, the badge fold
(`.history__current`, `.matrix__problem-count` and the `chip--armed` /
`chip--wide` / `settings-tag--on` states onto `Badge` tones), the
`button.settings-tag` coarse-floor bridge that layer 1 left in place, the
`StepUpBanner` copy, and the route copy findings.

## 6. Verification

```sh
cd web
node --run typecheck
pnpm exec vitest run --project unit
pnpm exec vitest run --project storybook
```

Last run (at the commit): typecheck clean; `node --run test` 985 passed;
`node --run test-storybook` 56 files / 214 stories passed with the a11y
addon on `error`, every route story rendering under the approved
`ui.css`; `node --run build` and `node --run build-storybook` both pass.
The unit log prints `ECONNREFUSED :3000` noise from a test that probes a
local server; pre-existing, tests pass. Em-dashes in `ui/Menu*` and
`.storybook/withApp.tsx` comments were replaced (AGENTS.md ban).

Fixed: `node --run test` used to die loading the vitest config with
`Cannot find package '@storybook/react-vite' imported from
.../storybook/dist/_node-chunks/...`. Root cause: pnpm 11's
`virtualStoreType: global` links `storybook` from a store outside the
project, so a bare framework import from inside storybook's chunks has no
resolution path; `pnpm exec` masks it by injecting `NODE_PATH` and an ESM
loader hook through `NODE_OPTIONS`, `node --run` does not. Fix:
`.storybook/main.ts` resolves the framework and addons to absolute
directories (Storybook's documented answer for strict layouts). Both
`node --run test` and `node --run test-storybook` now pass without pnpm.
Alternative not taken: dropping `virtualStoreType: global` from
`web/pnpm-workspace.yaml` (the actual root). That changes the install
layout for every package; the Storybook-side fix is local to the one
consumer that broke.

The route sweep is committed as `web/scripts/audit-styles.mjs`. The
one-off probe and screenshot scripts stayed disposable under the
gitignored `web/.xreview/`.

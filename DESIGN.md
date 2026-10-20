# Design

The reference for any change under `web/`: what the UI system has, which part to use for what, and the rules. Read it before touching a screen, and update it in the same change when a shared rule moves. `PRODUCT.md` holds users, voice and principles; this file holds the system that serves them.

Sources of truth, in order: `web/src/styles/tokens.css` (every design value), `web/src/ui/` (the atoms, each with a story), `web/src/styles/app.css` (route and chrome styles). Where this file and the code disagree, the code is current and this file has a bug: fix one of them.

## Register

Precise, calm, trustworthy infrastructure tooling (`PRODUCT.md`). Dense but calm: hairlines over shadows, weight over size, one teal accent, nothing decorative that is not also informative. Dark is the default theme; light is fully supported. Every surface works on a phone.

## Tokens

`tokens.css` is the only place a value is written; components, routes and stories consume `var(--…)`. `web/src/ui/tokens.ts` (`TOKEN_FAMILIES`) lists each token with its role and renders as the `Design system/Tokens` story. `design:check` enforces the rest (see Verify). Hairlines (1px) and the 2px focus ring are constants, not drift.

**Colour**, OKLCH, tinted neutrals plus one teal accent. Dark values are the `:root` defaults; light is a `prefers-color-scheme` override plus `[data-theme]`.

| Token | Role | Dark | Light |
|---|---|---|---|
| `--bg` | page | 0.19 0.012 220 | 0.965 0.008 200 |
| `--bg-raise` | raised rows; sheets on chromeless pages (login, ceremonies) | 0.23 0.014 220 | 0.995 0.004 200 |
| `--bg-panel` | persistent chrome, cards, panels, side panel | 0.225 0.014 220 | 0.935 0.01 200 |
| `--bg-hover` | hover and selected inside chrome | 0.27 0.016 220 | 0.895 0.012 200 |
| `--line` | hairlines, control boundaries (3:1 on `--bg`) | 0.5 0.016 220 | 0.64 0.012 210 |
| `--chrome-line` | structural rules inside chrome | 0.3488 0.01488 220 | 0.8388 0.00752 204.4 |
| `--panel-line` | dense settings-panel boundaries | 0.34 0.014 220 | 0.82 0.014 210 |
| `--tx` | primary ink (about 13:1) | 0.93 0.008 200 | 0.25 0.03 225 |
| `--tx-dim` | secondary: captions, labels, ledes (about 7:1) | 0.76 0.01 210 | 0.44 0.02 220 |
| `--tx-faint` | tertiary: eyebrows, counts, label notes | 0.71 0.012 215 | 0.46 0.02 218 |
| `--accent` | interactive teal; ok-state ink; focus ring | 0.82 0.09 195 | 0.45 0.085 210 |
| `--on-accent` | text on the accent | 0.2 0.02 220 | 0.99 0.005 200 |
| `--danger` | violation, refusal, error | 0.72 0.13 25 | 0.48 0.14 30 |
| `--changed` | pending, drafted, changed (slate, never red) | 0.78 0.07 250 | 0.45 0.08 250 |

Derived (`color-mix`, not mirrored in the design file): `--accent-soft`, `--danger-soft`, `--changed-soft` (14% dark; 10%, 10%, 12% light) and `--neutral-soft` (`--tx-dim` at 14% / 10%) are the tag fills; `--btn-line` and `--btn-tx` pull the secondary button's border and label toward the accent. `--tx-on-accent` is ink on an identity colour, the same in both themes.

`--bg-panel` sits above `--bg` in dark and below it in light. Use it for chrome and cards; `--bg-raise` there is invisible in dark and brighter than the paper in light.

**Type.** Instrument Sans (`--font-ui`) 400/500/700; IBM Plex Mono (`--font-mono`) for keys, values and identifiers. Five sizes, no others: `--fs-xs` 11 (eyebrow 500, tag 700, both uppercase with `--tracking-eyebrow`), `--fs-sm` 13 (labels, legends, ledes, hints), `--fs-md` 14 (body, h3, panel titles 700), `--fs-lg` 16 (h2, dialog and side-panel titles 700), `--fs-xl` 20 (h1 700); `--fs-mono` 13. Headings win by weight. Only the eyebrow, the tag, the matrix column headings and the matrix environment picker are uppercase, and only through CSS, so source text keeps its case. Line heights `--lh-tight/heading/body`. Prose stays within `--measure` (68ch).

**Space.** `--space-1..6` = 4/8/12/16/24/32 for every gap, padding and margin. Containers space children with `gap`, never with child margins. Two exceptions remain: an `.actions` row after content in a non-flex `.card` or `.remote` takes `margin-top: --space-3`, and the action stack in `.access-person__who` tops its `--space-1` gap up by `--space-2`.

**Radius carries a role.** `--radius-container` 6 (cards, panels, dialogs), `--radius-control` 4 (buttons, inputs, selects, matrix value boxes), `--radius-badge` 3 (tags, chips, phone tab pills). `--radius-pill` only for identity circles and count badges.

**Control.** `--control` 36px is the height of every button, input, select, tab, menu row, summary, chip and control-shaped link; a coarse pointer raises it to `--touch` 44. `--hit-min` 24 is the fine-pointer hit floor and the compact button height. `--badge-height` 20. Choice controls: hit box `--chk-box` (24 / 44), drawn box `--chk-visual` (18 / 20). `--row` 36 for table and matrix rows; `--rail` 56. Icons: a glyph is 1em of its text; `--icon` 16 is the standalone icon box; `--icon-in-button` (half of `--control`) is the glyph in an icon-only button.

**Depth and motion.** `--overlay-shadow` only on floating surfaces: dialogs, the side panel, the phone drawer, the matrix key tip. `--scrim` under a dialog backdrop, the side panel and the phone drawer. Layers are `--z-chrome/sticky/drawer/overlay`; no other z-index. Motion: `--dur` 180ms, `--ease` ease-out-quart, transform and opacity only; `--dur` is 0 under `prefers-reduced-motion`.

**Atmosphere.** The site's raked sand sits behind the shell (`.sand::before`, fixed): `--rake` (per theme), `--rake-aa`, `--rake-opacity` (0.28, softer than the site's 0.4), the gap breathing 28-52px over 300s, smooth as on the site (off under reduced motion; `web/src/app/sandMotion.ts` pauses it while the page is hidden or unfocused, `sand-paused`, and keeps it static without GPU acceleration, `sand-static`; phones, 700px and below, and coarse pointers never animate it). Each page root in the content well (`.content.veil`) carries `--veil` (50% page wash) and `--veil-blur` (2px) on its `::before`, reaching `--veil-gutter` (48px) past its box and starting flush under the header, so the open area right of an 860px column shows the sand unblurred while a full-width root (the matrix) is veiled across; every card-style surface, the chrome, tables and the matrix grid stay opaque. `--z-backdrop` (-1) is the layer of both. Storybook's Sand toolbar toggle shows or hides it behind route screens.

**Focus and disabled.** `--ring-width/offset/color` is the one focus ring, never removed from a control. A non-control focus target (a heading, section, aside, form or div with `tabindex="-1"`, focused by script for a panel, fragment jump or error) shows no ring: Tab never reaches it. `--opacity-disabled` for non-button controls; buttons have their own disabled skin (Buttons).

**Breakpoints** are not custom properties: `BREAKPOINTS` in `ui/tokens.ts`. `700px` is the phone layout (`max-width: 700px`; desktop rules use `min-width: 701px`); a `ChoiceGroup` grid collapses at a 480px container width. Use no other breakpoint. Two documented exceptions: the header drops its identity label at `max-width: 960px` (crumbs and identity share one row there), and the matrix well gets its height floor at `min-height: 640px`.

**Measures.** `--width-dialog` 520, `--width-dialog-wide` 760, `--width-side-panel-min` 640, `--chip-id-max` 17ch (an identifier chip ellipsises past it).

## Layout

- **Shell.** `.chrome` is a grid: rail (`--rail`), sidebar (218px), content. At 700px and below the rail hides and the sidebar becomes a drawer that slides in from the left. The content well is `.content`; its children get one top offset (`--space-5`, `--space-4` on a phone) so every h1 sits at the same height. A page never adds its own top margin.
- **Page column.** Chrome pages (settings, members, projects, audit, approvals, machine access) use `.page.page--chrome`: left-aligned in the well, max 860px, h1 then `.page__lede` then content. Never centre a page.
- **Sections are tabs.** A page with several sections renders `SectionTabs` (`routes/Sections.tsx`), one `Panel` card per section: selected section in `?tab=` through `useTabParam`, the first tab leaves no param, an old `#section` link opens the tab that holds it, a single section shows no tab bar. Sections that must be read together share one tab; Audit has no tabs (Filter and Events on one page). Every tab-like control (environment switchers, mode rows) is `ui/Tabs`.
- **Side menu.** One table (`app/navigation.ts`) renders desktop sidebar and phone drawer. Blocks: a context block (project or instance) above the organisation block, which never hides. On an organisation page the project block shows the project last opened in that organisation (sessionStorage, per tab); before any was opened, the organisation's project list takes its place. A page with neither (account settings) shows a `.sidebar__back` link to the project. Overview is disabled, with a reason, when the organisation has one project. Rows run flush to the sidebar's left edge; the current row has the accent edge and `--accent-soft`.
- **Breadcrumb.** Header trail `Hikyo / org / project / page`, built by `breadcrumbs()` in `routes/Shell.tsx`; scope crumbs link to their level, the root and current page do not.
- **Card header.** Title and its badges at the start, the card's action at the end of the same row (`margin-left: auto` on the last `.btn` or `.actions`).
- **Settings rows.** `.settings-row`: copy at the start, one control at the end. Two or more buttons: `.settings-row--stacked` (copy, badges, then one `.actions` row), never buttons split around the copy.

**QR dimensions.** Authenticator setup codes retain their compact 176px size.
Dense pairing invitations use `--width-qr-large` (320px), shrink to their
container width, and remain square. Both sizes preserve scanner contrast.

## Components

Use a `web/src/ui` atom wherever one exists; a missing generic pattern becomes a new atom with its own story, never a one-off in a route. Each atom's story (`web/src/ui/<Name>.stories.tsx`, `Design system/<Name>`) is its usage reference.

### Button (`ui/Button.tsx`, `Button.css`)

- **Variants carry a role**: `primary` (the one action a surface leads with), `secondary` (default), `danger` (destructive; the word says so), `quiet` (tertiary, inside a row or header). A variant changes colour and fill only, never size.
- **One size**: `--control` tall, `0 --space-4`, `--fs-md`. The `btn--compact` class (not a prop) is `--hit-min` tall on a fine pointer, `--control` on a coarse one, `0 --space-2`, `--fs-xs`; it is only for the matrix column header's `rev N · history` link and `envs N/N` summary. A new use needs a rule here.
- **Icon-only**: `icon` with a required `aria-label`; square, glyph at `--icon-in-button`. `btn--icon-phone` with a `.btn__label` span keeps the label visible on desktop and collapses to icon-only at 700px without changing the accessible name (the label is hidden by the `.visually-hidden-phone` rule).
- **One line.** Labels never wrap (`white-space: nowrap`).
- **Disabled** (`:disabled`, `[disabled]`, `[aria-disabled='true']`, including inside `<fieldset disabled>`): flat `--tx` 8% tint, no border, `--tx-faint` label, not-allowed cursor, no hover. A rule in `app.css` that restyles a `.btn` state must exclude disabled.
- **Read-only previews** (demo): `ReadOnlyContext` disables every `Button`; pass `readOnlySafe` to openers that only navigate or reveal read-only information.
- Links and summaries may wear `.btn`; in route `.tsx`, a raw `<button className="btn…">` fails `design:check`.

**Action rows.** A run of buttons sits in `.actions`: flex, wrapping, `--space-2` gap, start-aligned, primary first, no margin of its own (the container's `--space-3` gap spaces it). A dialog footer is `.dialog__actions`, end-aligned, primary last. Buttons in a table cell go in `<div className="actions">`, never `.actions` on the `<td>`. Only buttons go in `.actions`. At 700px a row of two or more buttons becomes an equal-width two-column grid; `.actions--stack` makes it one full-width column for long labels.

### Badge (`ui/Badge.tsx`)

One tag vocabulary. Tones: `neutral` (default), `ok` (measured-good; accent), `changed` (pending, drafted; slate), `danger` (violation, refusal). Each is a soft fill, its colour as ink, a 40% ink border, `--badge-height`, badge radius, `0 --space-2`, `--fs-xs` 700 uppercase by CSS. Ink on fill clears 4.5:1 in both themes (the Badge story checks it). There is no warn or info tone: pick changed or danger by consequence.

- **Icon before the word**, `icon={name}`; tones default to ok `check`, changed `history`, danger `alert`; `icon={false}` drops it. Pick by meaning: success, live, enabled `check`; denied credential `denied`; failure `alert`; degraded, drift, attention `warn`; refused, revoked `cross`; unknown `question`; reported `info`; pending, expiry `wait`; converging `sync`; paused `pause`; never, removed, not applicable (`NA`, full words in `title`) `minus`; current `own-draft`; added `plus`; edited, Edit permission `edit`; Publish `arrow-up`; Pin `pin`; protected, secret, locked `lock`; reveal, See permission `reveal`; inherits, intent `arrow-right`; credential, bearer `passkey`; federated `link`; workload, machine `workload`; automation `automation`; you `draft`; Administration `settings`; disconnected `disconnected`.
- **Identifiers keep case.** `mono` marks a key name, revision, id, URL or `kind: subject` origin: value face, 500, exact case, no default icon. An identifier inside a tag (`current r4`) is wrapped in `.mono` and keeps its case.
- **Capability tags**: `CapabilityTag` (`routes/capabilityTags.tsx`) maps every capability id to one tone and icon; use it wherever capabilities are listed, in a `.capability-tags` row.
- **Count badges** are not tags: the pill (`.tab__count`, matrix counts).

### Glyph (`ui/Glyph.tsx`)

Every icon is a `Glyph`: inline SVG, 24px grid, 1em, stroked in `currentColor`, `aria-hidden` unless `label` is given. Text pictographs (✓ ✕ ⚠ ↺ ▾ ✎ and the like) fail `design:check`. The set is in `GLYPH_NAMES` and the `Design system/Glyph` story. Artwork is Rune Icons outline (Apache-2.0); `key` alone is Lucide `key-round` (ISC).

To add one: copy the path data from Rune Icons `public/normal/` at the commit pinned in `Glyph.LICENSE` (never the site code, never the `runeicons-react` package), join subpaths into one `d`, add it to `PATHS` with the source id as a trailing comment. An icon from any other source needs its licence notice appended to `web/src/ui/Glyph.LICENSE` with source, pinned commit and changes.

### Tabs (`ui/Tabs.tsx`)

APG tabs: one tab stop, arrows and Home/End select. Render the panel with `TabPanel`. `count` shows a pill after the label (muted at zero, accent otherwise, filled on the selected tab, the word `unknown` when the listing failed); the accessible name reads "Service accounts, 2". Selection changes ink and the accent underline only, never weight or box size. Above 700px the row scrolls sideways with a thin scrollbar and keeps the selected tab in view; at 700px it wraps into equal-growing pills (badge radius, `--touch` tall). On a page use `SectionTabs` (Layout).

### SidePanel (`ui/SidePanel.tsx`, `SidePanel.css`)

The surface for a detail or for any form a button opens. A form, a typed-name confirmation or a detail never expands inline under a list; it opens here.

- Slides in from the right over a scrim that covers the content area and stops at the side menu. Its left edge is one sidebar width past the menu, or at the menu when that leaves it narrower than `--width-side-panel-min`; full width at 700px.
- Header pinned, body scrolls (`.side-panel__body`, `--space-3` gap). `title` gives the standard h2 plus a Close button (icon-only on a phone); `header` with `initialFocus` and `opener` is for a custom header.
- Modal over the content: everything else in `<main>` is `inert` and Tab wraps. Escape, Close and a press outside close it (the side menu counts as outside; toasts and an open `<dialog>` do not). Focus returns to the opener.
- Unsaved input is never lost to a stray dismissal. Once an `input` or `change` event fires in the body (outside a nested `<dialog>` or a `role="search"` lookup such as Who can reach one key?), an outside press does nothing, and Escape or the built-in Close shows "Discard unsaved changes?" in the header (`role="alertdialog"`, Keep editing focused first and returning focus where it was, Discard in danger). The caller closing the panel itself (after a save, a Cancel in the body) never asks. No `window.confirm`. A custom `header` with its own close link bypasses the question, so keep custom headers for detail panels.
- No footer: actions are an `.actions` row in the body.

**SidePanel or Dialog.** Side panel for reading and for editing. `Dialog` for a modal ceremony that needs a decision before anything else: a display-once value, reauthentication, a widening or destructive confirmation, the rule editor, the matrix cell editor.

### Dialog (`ui/Dialog.tsx`)

Native `<dialog>` with `showModal()`. `size`: `narrow` (520, a decision with two or three buttons) or `wide` (760, an editor). Title h2, optional `lede`, `actions` row with the primary last. Decision dialogs omit a corner X: Escape and Cancel are the exits. Editors can opt into a corner close through `onClose`, using their existing dismissal handler (#959); the value editor does so. `onBackdropClick` is opt-in for editors; `pinActions` keeps title and actions fixed while the body scrolls.

### Forms (`ui/Field.tsx`, `Input`, `Select`, `Textarea`, `Checkbox`, `Radio`, `ChoiceGroup`, `ToggleChip`)

- `Input`, `Select`, `Textarea` render through `Field`: label, control, `hint`, per-control `error` (glyph, `role="alert"`, `aria-invalid`), `--space-1` apart. A form-level refusal is an `Alert` above the actions. `.form` stacks fields at `--space-3`.
- A string label's trailing `(…)` ("Kind (immutable)", "(optional)") renders in `--tx-faint`; the accessible name is unchanged.
- `mono` sets the control in the value face. `revealable` is only for a password being typed; a stored secret is revealed through the reveal ceremony, never a field toggle.
- Date, time and datetime inputs take the shared control box, tabular digits, faint ink until filled, and a drawn calendar indicator (no fetched image: CSP).
- Choices: `Radio` for two to four short options; a longer list with descriptions is a `Select` with a live hint. Group them in `ChoiceGroup` (`layout` stack/wrap/nowrap, `columns`, `variant="chips"`). `ToggleChip` for dense include/exclude pick sets. Checkboxes and radios are never hand-written (`design:check`).

### Combobox (`ui/Combobox.tsx`)

Editable suggestions for a field that can reuse an existing value or accept a
new one. Use it for folder assignment (declare, edit, cleanup). A visible
chevron opens all choices; typing filters them. Arrow keys explore, Enter
selects without submitting, Escape dismisses before the enclosing dialog,
and Tab preserves typed text. Selection is explicit, never inferred on blur.
The popover stays above modal and scrolling surfaces and flips above the field
when space below is limited. Keep ordinary selects for finite choices and text
inputs for secrets, prose, patterns and unique names being created or renamed.

### Disclosure (`ui/Disclosure.tsx`)

Text plus a chevron that turns right to down, no border or fill, `--control` tall, `aria-expanded`. Use it to reveal supporting content in place. A `<details>` that opens a whole management block wears the button skin with a leading `chevron` glyph (`<summary className="btn">`: project settings "Manage …", change approvals "Advanced", the matrix editor's schema and raw declaration, the matrix `envs N/N` picker); that is the one place an expander looks like a button. One rule in `Button.css` (`summary.btn`) drops the native marker and turns the chevron down when open; a summary needs no CSS of its own for that.

### Alert, CeremonyNotice, Menu, Sep

- `Alert`: inline feedback that stays. `danger` (`role="alert"`), `done`, `warn`, `info` (`role="status"`), each with its glyph; one optional `action`. Never auto-dismissed.
- `CeremonyNotice`: a standing fact above a ceremony's controls.
- `Menu` / `MenuItem`: overflow menu on the Popover API with the APG keyboard contract; the trigger is an icon-only button named by `label`.
- `Sep`: the visible ` · ` between inline text ("Payments · every environment"), larger and spaced. It is real text, so screen readers hear a separator. Use it in visible text; `aria-label`, `title` and `<option>` text keep a plain ` · `.

### Tables

Every data table except the matrix sits in `.table-scroll`, so the table scrolls sideways and the page never does. At 700px tables inside it go compact: `--fs-xs` text (identifiers included) and `--space-1 --space-2` cell padding. Use the existing table and panel anatomy for listings before inventing one. Lists of people, keys or rules get a filter input from the start.

## The matrix (`routes/Matrix.tsx`)

The product's signature surface: keys as rows, environments as columns, each value set or absent with no inheritance.

- **Columns.** The table spans the page. Each environment column is exactly its content width (longest value, marks, pencil, padding); a trailing `.matrix__filler` column (hidden from assistive tech) takes spare width. Short of room, columns shrink toward their header with an ellipsis, then the table scrolls.
- **Key column.** Sticky and opaque. Desktop: 236px while the environments fit, compacting to a 160px floor under overflow; the name ellipsises with the full name in the row `title` and the link's accessible name. Phone: `--touch` wide, one glyph per key (`lock` secret, `config` config); the first tap shows the name in `.matrix__key-tip`, the second opens the key. The `envs N/N` picker moves from the key header to the head row.
- **Rows.** `--row` tall (`--touch` on a phone), alternate rows striped, no hairline between key rows; a rule sits under the header and above each group row. Group rows collapse (chevron, instant), showing comma-separated key names when collapsed. A linked key is a `link` glyph after the name, never a second line.
- **Icons** are 40% of the row height (`--matrix-icon`). The edit pencil sits at the cell's end in muted tertiary ink, `--tx-dim` on hover and focus, absent on problem cells.
- **Column header.** Environment name as an eyebrow, then `rev N · history`: a quiet compact button, icon-only (`history`) on a phone. Keep `.matrix__history-link`: the history panel finds its opener by it.
- **Marks.** A value is plain mono text. Absent: `· absent`. Required and absent: `alert` + `required · absent`, danger cell. Unreadable column: `· unreadable`. Unknown is the word `unknown`, never a dash. Secret: masked, `lock` on the key. Changed in the latest published revision: `history` in `--changed`, never for r1. Your own draft: `own-draft` in `--changed`. Another editor's draft: `draft` in `--tx-dim`. The words ("changed in r4", "draft", "another editor has a draft here") ride in `title` and the accessible name. Warnings (measured, not violations) use `warn` and the changed tone; unmeasured diagnostics use `question` and a dashed hairline. Red is only for errors and violations. Retention expiry tiers `!`, `!!`, `!!!` are text.
- **Opening things.** A cell opens the cell editor (`MatrixRowEditor`, a wide `Dialog`: one environment first, other environments behind explicit disclosure); the key name opens its declaration (`KeyDeclarationDetail`) in the side panel; revision history (`routes/HistoryDrawer.tsx`) is a side panel showing one revision at full width: the heading is a native revision select (`?rev=`), the environment switcher is `ui/Tabs`, and restore, pin and the two diffs are one `.actions` row, each with its glyph.

## Phone (700px and below)

- Every route at 390px: no page-level horizontal scroll. Wide content scrolls inside its own container (`.table-scroll`, the matrix well, the tab row above 700px).
- Tabs wrap into pills; action rows become a two-column grid (`.actions--stack`: one column); tables go compact; the matrix key column becomes icons.
- Touch targets are 44px (`--touch`), raised automatically by `--control` on a coarse pointer.
- The side menu is a drawer; the side panel is full width; wide dialogs inset to the viewport.
- Labelled buttons that must save room use `btn--icon-phone`, never a second element. Other text that only sighted phone users lose (the matrix key names) uses `.visually-hidden-phone`.

## Accessibility

- Contrast: body text 7:1, large text 4.5:1, tag ink on fill 4.5:1, non-text marks and disabled labels 3:1, in both themes.
- State is never colour-only and never glyph-only: the word is beside the mark, or in its `title` and accessible name where the matrix abbreviates.
- Touch targets 44px on a coarse pointer; the 24px hit floor on a fine one.
- Every icon-only control has an accessible name (`aria-label`; the `Button` type requires it with `icon`). A width-dependent label keeps the same name at every width.
- Focus: the one ring on every control; overlays move focus in on open and return it to the opener on close; a modal surface traps Tab; Escape closes it.
- Essential meaning never lives only in a tooltip (touch has no hover).
- Respect `prefers-reduced-motion` through `--dur`; never hard-code a duration.

## Copy

- No em-dash anywhere in user-visible text, comments or docs; use a comma, colon or full stop.
- One concept, one name, rendered from one table everywhere (editor, list, answer, glossary). Anything a user chooses between carries its one-line meaning at the point of choice.
- Action labels say what happens and carry no trailing "…" ("Restore to r4", "Pin workload to r4").
- Lengths of time are spelled out with a correct plural ("30 days", "1 hour"), never `30 d`, `8h`, `45s`: use `duration()` / `plural()` from `web/src/app/duration.ts`. A live countdown counts in seconds.
- Placeholders are words from the state vocabulary, never a dash.
- Sentence case everywhere; capitals come from CSS (tags, eyebrows), so source, tests and screen readers keep the real case.

## Behaviour rules

Distilled from owner review; each names the failure it prevents.

1. **Ask in dependency order**: inputs that constrain others come first (permission, project, environment, key).
2. **Nothing moves under the pointer**: conditional notes get a reserved line; hints do not change length with state; selection never resizes a tab or chip.
3. **Short before the primary action**: the summary above Save reuses the list's components plus one reach line.
4. **Each function has its own look**: buttons act, toggle chips include or exclude, disclosures expand. Separators such as `›` imply navigation; use labelled parts or real links.
5. **Hide the impossible, disable the conditional**: options a subject can never have are hidden behind one line naming them; options another input unlocks stay visible and disabled, with the reason on its own line.
6. **No opaque bundles**: presets tick visible boxes; secrets are always an explicit tick.
7. **Same data, same component**: anything shown in two places uses one shared component.
8. **One home per kind of thing**: people and machines do not share a list; cross-kind questions mark the kind.
9. **Omit what the user cannot act on**: empty, non-actionable groups are left out.
10. **Name who is affected before a widening change commits**, and require explicit confirmation.

## Security-sensitive edits

A UI change that touches a module using mutations or sensitive state (`useMutation`, `useSensitiveMutation`, `useSensitiveState`, `useRemaskOnFocusLoss` and the rest in `web/src/api/sensitiveInventory.test.ts`) changes that file's pinned hash in `web/src/api/sensitiveInventory.json`. Refresh the pin only with an accurate per-file description in its `review` note of what moved (markup only, or which data path), and flag the change for maintainer security review (`CONTRIBUTING.md`, Security-sensitive contributions).

## Storybook and verification

Every UI change ships a story for what it changes, with a `play` that asserts the behaviour the story exists to show. Stories render the product's reusable piece, not a prototype; simulators stay in `docs/site/public/prototypes`. Every story runs, with accessibility checks, in dark and light; add a phone story where the phone layout differs, on the one shared `phone` viewport (390px, defined in `web/.storybook/preview.tsx`): `globals: { viewport: { value: 'phone', isRotated: false } }`. Token probes in plays use `tokenColor` / `tokenPx` from `web/.storybook/tokens.ts`. Storybook conventions: `web/.storybook/README.md`. Designs live in `web/design/hikyo.pen` (OpenPencil), mirrored from `tokens.css`; a story links its node with `parameters.design = design('Title/Variant')` ([design-tooling ADR](docs/adr/design-tooling.md)).

From `web/`:

```sh
node --run typecheck && node --run lint && node --run test
node --run design:check          # tokens match hikyo.pen; literal-size budget; route markup uses ui/ atoms
node --run test-storybook        # every story, dark
node --run test-storybook:light  # every story, light
```

`design:check` legs: `tokens-check` (`tokens.css` against `hikyo.pen`; after a token change run `node --run design:seed`), `adherence-check` (literal px sizes per stylesheet against `scripts/design/adherence-budget.json`, a ratchet that only goes down: `ui/*.css` are held at 0), `markup-check` (no hand-written alert, notice, checkbox, radio, dialog, chip, `btn` button or text pictograph in `routes/` and `app/`; a deliberate exception carries an indented `markup-check:` comment naming the reason).

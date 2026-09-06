# Design

Living reference for the implemented web app and documentation-site design system. Update it alongside changes to shared visual language and interaction patterns.

## Theme

Dual theme, **dark default** (self-hosters at night, server closets, OLED phones), light fully supported and switchable; respect `prefers-color-scheme` when no explicit choice. Dark is desaturated graphite tinted toward the brand teal: never neon, never glow.

## Color

OKLCH throughout. Restrained strategy: tinted neutrals + one teal accent, state colors as a small fixed vocabulary.

Dark (default):

- `--bg` oklch(0.19 0.012 220) surface
- `--bg-raise` oklch(0.23 0.014 220) raised rows, and sheets on a page that has no chrome (login, the CLI and workspace ceremonies)
- `--bg-panel` oklch(0.225 0.014 220) persistent chrome: rail, sidebar, in-chrome cards and panels, and the overlays that open over them
- `--bg-hover` oklch(0.27 0.016 220) hover and selected surfaces inside chrome
- `--line` oklch(0.5 0.016 220) hairlines and control boundaries (>=3:1 on `--bg`)
- `--chrome-line` oklch(0.3488 0.01488 220) low-contrast structural rules inside persistent chrome
- `--panel-line` oklch(0.34 0.014 220) dense settings-panel boundaries
- `--tx` oklch(0.93 0.008 200) primary text (≈13:1 on bg)
- `--tx-dim` oklch(0.76 0.01 210) secondary (≈7:1)
- `--tx-faint` oklch(0.71 0.012 215) tertiary: eyebrows, counts, section labels
- `--accent` oklch(0.82 0.09 195) interactive teal; text on accent uses oklch(0.2 0.02 220)

**`--bg-panel` is the chrome surface, and its relationship to the page inverts between themes**: raised above `--bg` in dark (0.225 over 0.19), recessed below it in light (0.935 under 0.965). Reaching for `--bg-raise` instead is invisible in dark and wrong in light, where it lands at 0.995, brighter than the paper.

Each of the three state colours carries a tinted fill for the surface it names (`--accent-soft`, `--danger-soft`, `--changed-soft`), derived from its base colour so the pair cannot drift, at 14% in dark and 10% (12% for changed) in light.

Light: linen-tinted paper oklch(0.965 0.008 200), chrome and dense panels oklch(0.935 0.01 200), chrome hover oklch(0.895 0.012 200), panel boundaries oklch(0.82 0.014 210), hairlines and control boundaries oklch(0.64 0.012 210) (>=3:1 on the paper), structural chrome rules oklch(0.8388 0.00752 204.4), ink oklch(0.25 0.03 225), secondary oklch(0.44 0.02 220), tertiary oklch(0.46 0.02 218), same accent hue at oklch(0.45 0.085 210).

State vocabulary (always paired with a glyph or text, never color-only):

- explicit value: plain monospaced text; no decorative border
- explicit absence: muted `· absent`
- unknown (a read that failed or has not happened): the word `unknown`, never a dash; a column the caller may not read renders `· unreadable`
- secret present: masked text and a lock on the key label
- missing/violation: red-family oklch(0.72 0.13 25) dark / oklch(0.48 0.14 30) light, + `!` or `✕`
- pending/changed: `Δ` in blue-violet-free slate oklch(0.78 0.07 250); an unpublished draft carries a 6px slate dot beside the value with the word "draft" in the accessible text
- warning (a measured condition that is not a violation): neutral hairline with a slate tint and `!`; unmeasured/unknown diagnostics use a dashed hairline and `?`. Red is reserved for errors and violations.

Copy: no em-dash anywhere in user-visible text; use a comma, colon, or full stop. Placeholders are words from this vocabulary, never a dash.

## Typography

- UI: Instrument Sans (400/500/700). Headings weight-contrast over size-inflation.
- Keys & values: IBM Plex Mono, 12-13px, tabular feel.
- Body line length ≤ 72ch. Scale ratio ≥ 1.25.

## Shape & Space

- **Radius carries a role** (decided app-chrome iteration 8, ticket #29): containers/cards 6px; controls (buttons, inputs, selects) 4px; badges/tags/chips 3px.
- **The 999px pill is reserved** for identity circles, count badges, and status dots of 12px or less. Matrix values are table content, not badges: the hover and focus box around a value is a control (4px). Labelled chips (problem counts, `current rN`, staging summaries) are badges (3px).
- Hairline borders (1px) over shadows; shadows only on modal overlays.
- Density: matrix rows ~36px desktop, ~44px touch targets mobile.

## Motion

- 150-220ms, ease-out-quart (`cubic-bezier(0.25, 1, 0.5, 1)`). Transform/opacity only.
- Group collapse is instant: matrix rows are virtualised, so there is no box to animate; the chevron rotates.
- `prefers-reduced-motion`: transitions off.

## Components

- Environment controls: toggleable visibility in the table header, with protected state named in text.
- Cells: plain monospaced value/state text; borders only name problems or active draft state.
- Centred cell modal: one selected key/environment first, with provenance, schema, edit, copy, clear, reveal, and history actions. Multi-environment editing is explicit secondary disclosure.
- Group headers: collapsible; collapsed state shows comma-separated key names. Key rows stay one line: a linked-key membership is a `🔗` glyph after the name with the group named in the accessible text, never a second line.
- Revision history on a phone is an explicit drill-in (list, then detail with a back action); the desktop drawer keeps list and detail side by side.
- Sidebar: one table (`web/src/app/navigation.ts`) renders desktop and mobile; a context block (project or instance) stacks above the organisation block, which is never hidden; instance and account destinations live in the rail on desktop and in the drawer on mobile. Every scope is a {Members, Settings} pair with the same page anatomy (h1 · lede · jump index · panels).

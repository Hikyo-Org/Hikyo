# Design

Seed DESIGN.md (pre-implementation). Re-run `/impeccable document` once real frontend code exists.

## Theme

Dual theme, **dark default** (self-hosters at night, server closets, OLED phones), light fully supported and switchable; respect `prefers-color-scheme` when no explicit choice. Dark is desaturated graphite tinted toward the brand teal: never neon, never glow.

## Color

OKLCH throughout. Restrained strategy: tinted neutrals + one teal accent, state colors as a small fixed vocabulary.

Dark (default):

- `--bg` oklch(0.19 0.012 220) surface
- `--bg-raise` oklch(0.23 0.014 220) raised rows/sheets
- `--line` oklch(0.5 0.016 220) hairlines and control boundaries (>=3:1 on `--bg`)
- `--chrome-line` oklch(0.3488 0.01488 220) low-contrast structural rules inside persistent chrome
- `--tx` oklch(0.93 0.008 200) primary text (≈13:1 on bg)
- `--tx-dim` oklch(0.76 0.01 210) secondary (≈7:1)
- `--accent` oklch(0.82 0.09 195) interactive teal; text on accent uses oklch(0.2 0.02 220)

Light: linen-tinted paper oklch(0.965 0.008 200), hairlines and control boundaries oklch(0.64 0.012 210) (>=3:1 on the paper), structural chrome rules oklch(0.8388 0.00752 204.4), ink oklch(0.25 0.03 225), same accent hue at oklch(0.45 0.085 210).

State vocabulary (always paired with a glyph or text, never color-only):

- explicit value: plain monospaced text; no decorative border
- explicit absence: muted `· absent`
- secret present: masked text and a lock on the key label
- missing/violation: red-family oklch(0.72 0.13 25) dark / oklch(0.48 0.14 30) light, + `!` or `✕`
- pending/changed: `Δ` in blue-violet-free slate oklch(0.78 0.07 250)

## Typography

- UI: Archivo (400/500/700). Headings weight-contrast over size-inflation.
- Keys & values: IBM Plex Mono, 12-13px, tabular feel.
- Body line length ≤ 72ch. Scale ratio ≥ 1.25.

## Shape & Space

- **Radius carries a role** (decided app-chrome iteration 8, ticket #29): containers/cards 6px; controls (buttons, inputs, selects) 4px; badges/tags/chips 3px.
- **The 999px pill is reserved** for identity circles and count badges. Matrix values are table content, not badges.
- Hairline borders (1px) over shadows; shadows only on modal overlays.
- Density: matrix rows ~36px desktop, ~44px touch targets mobile.

## Motion

- 150-220ms, ease-out-quart. Transform/opacity only.
- Group collapse animates via grid-template-rows.
- `prefers-reduced-motion`: transitions off.

## Components

- Environment controls: toggleable visibility in the table header, with protected state named in text.
- Cells: plain monospaced value/state text; borders only name problems or active draft state.
- Centred cell modal: one selected key/environment first, with provenance, schema, edit, copy, clear, reveal, and history actions. Multi-environment editing is explicit secondary disclosure.
- Group headers: collapsible; collapsed state shows comma-separated key names.

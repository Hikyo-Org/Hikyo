# Design directions (exploration, 2026-09-06, round 2)

Three alternative directions (a fourth, Manual, was dropped after phone review) for the web app, built as static mockups from the
real matrix and projects surfaces (same fixture content as `prototype/mock-api.ts`
populated scenario). Nothing here ships. The chosen direction gets
re-implemented in `src/` against DESIGN.md, tokens.css and the e2e pinned set.

View: `pnpm prototype` then open `/prototype/directions/index.html`.
Screenshots at 1280 and 390 are in `shots/` (plus `current-*` for the shipped UI).

## Diagnosis of the current build

What reads as generated is composition, not palette:

- rail + sidebar double navigation
- every section boxed in a rounded card, identical outlined buttons in a row
- ledes that restate the heading, uppercase eyebrow label on every block
- blue-graphite dark + teal accent + moon toggle: the 2024 AI dashboard set

## Directions

| | A. Console | B. Switchboard | C. Ledger |
|---|---|---|---|
| Concept | terminal surface on the web | industrial instrument panel | the grid is the app |
| Theme | dark first | light first | light first |
| Nav | text tree, collapsible | one column, org/project switch plates | none, top bar with tabs |
| Chrome | status line + key strip | 2px rules, no cards | 44px bar + grid toolbar |
| Provenance | strip under header on focus | right inspector column | right inspector pane |
| Mobile matrix | one env, segmented switch | sticky key col, snap per env | one env, text tabs |
| UI face | Commit Mono | Schibsted Grotesk | Atkinson Hyperlegible Next |
| Value face | Commit Mono | Martian Mono | Atkinson Hyperlegible Mono |
| Contrast text / secondary | 14.7 / 6.6 | 15.9 / 5.7 | 13.3 / 5.5 |

All fonts OFL and on fontsource (slugs in each file header).

## Non-negotiables check

Every direction keeps the full cell vocabulary (`· absent`, `! required · absent`,
`✕ value`, `••••••••` + lock, `Δ`, draft dot, `◌`, `🔗`, `req`), 44px mobile
targets, no colour-only state, no serif, no neon/gradient/glass, no shields.

Known trade-offs:

- A: mono-everywhere; the concept only holds if keyboard affordances are real.
- B: PROTECTED hatch is a repeating-linear-gradient; key names wrap at 390 in a 156px column.
- B and C: light-first against the dark default in DESIGN.md; a dark theme is needed before either matches the "night in the closet" context.

## Adoption cost (rough)

- Keeping token names but changing values, type and removing cards is cheap:
  tokens.css + app.css, DESIGN.md update, pinned assertions re-read the tokens.
- All four drop the icon rail. Adoption order by distance from the current
  shell: B (single sidebar, same sections) cheapest, then A and C (nav model changes). The last three also touch
  `src/app/navigation.ts`, the shell, the mobile drawer, and the e2e flows
  that walk the sidebar.
- Dual theme is deferred to the picked direction; each mockup is single-theme.

## Round 2 (phone review)

Applied: Manual dropped. Console: plain thumb actions on phone, full-width
grid, one-line provenance, ids replaced by counts. Switchboard: header
compacted (first group header at y=194 on 390x844), real buttons on projects,
ids replaced by counts, key names truncate instead of wrap. Ledger: inspector
closed by default, env tabs on two lines with rev and history, header bug
fixed, ids removed, toolbar merged to one row (first group at y=232).


## Round 3: merged candidate `bench`

Switchboard visual system as base. Grafts: Console tree sidebar (desktop),
Switchboard top plate + Menu tree (phone), Ledger filter field and
`Δ 8 unpublished · Publish` indicator, inspector closed by default, fixed
phone bottom bar for publish state and `+ New key`, Ledger-compact projects
with Switchboard buttons, no ids. Dual theme (`prefers-color-scheme` +
LIGHT | DARK plate). First group header at y=193 on 390x844.

Contrast: light 15.9 / 5.7, dark 14.9 / 7.0 (text / secondary on paper).
Files: `bench-matrix.html`, `bench-projects.html`. Shots: `shots/bench-*`.

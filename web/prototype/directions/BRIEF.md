# Design direction mockups: shared brief

Throwaway exploration. Static HTML, one direction per pair of files. Nothing
here ships; the chosen direction gets re-implemented in the real app.

## Why this exists

The current build reads as "generated": rail + sidebar double navigation,
every section boxed in a rounded card, identical outlined button rows, ledes
that restate the heading, uppercase eyebrow labels on everything, graphite
dark + teal accent, moon toggle. Each direction below must answer that with a
different STRUCTURE, not a recolour.

## Files each direction produces

- `<name>-matrix.html`: the environment matrix inside the app shell.
- `<name>-projects.html`: the projects list page inside the same shell.

Rules for the files:

- Self-contained: inline `<style>`, no build step, no framework. A few lines
  of inline `<script>` are allowed only for mobile env switching or a
  collapsed group toggle.
- Fonts: Google Fonts `<link>` is fine for the mockup, but every face MUST be
  OFL-licensed and available on fontsource (`@fontsource/<slug>`), because
  production is self-hosted with a CSP that forbids third-party origins. Put
  the fontsource slugs in an HTML comment at the top.
- Do NOT use Instrument Sans, Inter, Roboto, Arial, Open Sans, or system-ui as
  the primary face.
- Colours in OKLCH. No pure #000 / #fff. No neon, no gradients, no glass, no
  glow, no purple-blue. No shield icons, no badge walls.
- Responsive: must look intentional at 1280x800 AND 390x844. On 390 the
  matrix must show a REAL answer to "how do three env columns fit", not a
  cropped or horizontally-overflowing table. State the answer in a comment.
- 44px minimum touch targets on the 390 layout. Body text contrast about
  7:1, secondary text about 4.5:1. Put the two computed ratios (text on
  background, secondary on background) in a comment near the tokens.
- No em-dash anywhere in visible text.

## Fixture content (identical in every direction)

Shell context: breadcrumb or equivalent `hikyo / acme / demo / Environment
matrix`. Signed in as `alex`, 2 orgs, `org+instance admin`. Organisations:
`acme` (active), `sandbox`. Projects: `demo` (active), `website`, `mobile-app`.
Version `0.9.5`. A theme control must exist somewhere (a control, not a moon
icon).

Navigation destinations (all must be reachable in the shell; how is up to
the direction):

- Project demo: Environment matrix (active), Machine access, Change
  approvals, Deployment adapters, Project audit, Members, Project settings.
- Organisation acme: Overview, Projects, Remotes, Members, Organisation
  settings, SCIM provisioning, Audit.
- Instance settings. Account (Alex Lee, initials AL).

Matrix page header: title "Environment matrix". Actions: legend `?`,
"Import", "Folders & linked keys", primary "+ New key". Environment chooser
"envs 3/3". Columns: `development`, `staging`, `production` (production is
PROTECTED). Each column carries `rev 12 · history`.

Rows (folder groups, each header collapsible, shows key count, has `+ Key`):

```
▾ app/  3
  LOG_LEVEL 🔗            debug Δ            info Δ             warn Δ ◌
  FEATURE_CHECKOUT 🔗     true Δ             true Δ  ●draft     false Δ
  PUBLIC_APP_URL 🔗       http://localhost:8080 Δ   https://staging.example.test Δ   https://app.example.test Δ
▾ db/  2
  🔒 DATABASE_URL 🔗 req  •••••••• Δ         •••••••• Δ         •••••••• Δ
  🔒 REDIS_URL 🔗         •••••••• Δ         •••••••• Δ         •••••••• Δ
▾ auth/  1   !1 problem
  🔒 AUTH_SECRET 🔗 req   •••••••• Δ         •••••••• Δ         ! required · absent
▸ mail/  2   (collapsed: shows "SMTP_HOST, SMTP_PASSWORD")
```

Cell vocabulary, all of it must appear, never colour-only:

- plain value: set in that environment
- `••••••••` with 🔒 on the key: secret set
- `· absent`: not set
- `! required · absent`: violation, publish blocked (red family + glyph)
- `✕ value`: fails declaration (not in fixture rows; show it in the legend)
- `Δ`: changed since last publish (slate, never red)
- a 6px dot beside the value: unpublished draft of mine ("draft" in a11y text)
- `◌`: another editor has a draft here
- `🔗`: linked key, group named in a11y text
- `req`: required

Sidebar / nav counts for the project: app/ 3, db/ 2, auth/ !1, "problems 1".

Projects page: title "Projects", rows `demo`, `website`, `mobile-app` each
with its id (`prj_11111111-1111-4111-8111-111111111111`,
`prj_22222222-2222-4222-8222-222222222222`,
`prj_33333333-3333-4333-8333-333333333333`) and actions "Open matrix",
"Settings". A "New project" affordance with a name field and "Create
project". Do NOT write a lede that restates the heading.

## Non-negotiables (from PRODUCT.md)

- Mobile-first: phone at night in a server closet is a primary context.
- State is never colour-only.
- Provenance one gesture away: show where a cell's value came from without
  a modal if the direction can (inspector, drawer, inline).
- Dense but calm: 50 keys x 4 envs must still scan. Do not pad the matrix
  into a hero. At least the 6 fixture rows plus the collapsed group.
- Secrets feel deliberate: revealing is a ceremony, never a hover.
- Anti-references: editorial serif print (rejected), generic cloud console
  grey chrome, AI dark mode (neon, gradient, glass, glow), security theater.
- Liked: Infisical's env/field arrangement (structure), Linear/Tailscale
  calm.

## Slop test before you finish

Screenshot in your head: if someone said "an AI made this" would it be
believed instantly? If yes, change the structure again. Specifically avoid:
everything in cards, cards in cards, identical outlined buttons in a row,
uppercase eyebrow above every block, big rounded icon above headings,
centred everything, same padding everywhere, moon/sun toggle.

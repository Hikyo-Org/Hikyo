# Hikyo homepage prototypes handoff

> Historical local artifact names below identify the original run only and are not included in this checkout. Recorded results were not rerun for this path cleanup.

Completed 2026-09-11 from base `5cb857b4`, committed 2026-09-12 on branch
`t3code/create-hikyo-marketing-prototypes`. Linked from the static
`hikyo.app/prototypes/` index; the Astro routes stay under `/prototype/hikyo/`
because the CSP gate reserves `/prototypes/` for static pages without a meta.

Round three (same day): one more page, 11, that enhances the live landing page
rather than replacing it; see the table. The pinned theme bootstrap `is:inline`
script is copied byte-identical so the CSP gate's hash allowlist still matches.

Round two (same day): user reviewed round one on a phone. Verdict: 1 and 3 have
the styling they want, 3's matrix animation is the thing to keep, 2 is dull on
mobile, 4 and 5 feel alike. Five more directions were built mobile-first
(375px designed first, matrix animates on load, no scroll-scrubbed stages).

## What exists

Eleven isolated marketing homepage prototypes on the Astro docs site, plus an index
and a switcher:

| Route | Name | Direction | Theme | Fonts |
| --- | --- | --- | --- | --- |
| `/prototype/hikyo/` | index | list of the five | dark | Instrument Sans, Plex Mono |
| `/prototype/hikyo/1/` | Schematic | annotated engineering drawing, leader lines and balloons over the matrix, title block | light | Instrument Sans, Plex Mono |
| `/prototype/hikyo/2/` | Split stage | scroll-driven seven-step scenario, terminal and web UI in lock-step (IntersectionObserver) | dark | Archivo (width axis), Plex Mono |
| `/prototype/hikyo/3/` | Departure board | the matrix as a full-bleed split-flap board, CSS flap-in, `<details>` reveal | dark, warm | Martian Mono (width axis), Hanken Grotesk |
| `/prototype/hikyo/4/` | Manifesto | five numbered statements, oversized grotesk, one accent, sticky numbers | light, bone | Bricolage Grotesque, Azeret Mono |
| `/prototype/hikyo/5/` | Audit trail | the page reads as an append-only log with aligned event lines and a spine | dark, slate | Familjen Grotesk, JetBrains Mono |
| `/prototype/hikyo/6/` | Receipt | the matrix printed on a thermal strip, stacked per key, prints top-down, "INHERITED 0" subtotal | warm paper on dark | Courier Prime, Inter Tight |
| `/prototype/hikyo/7/` | Transit map | key groups as coloured lines, environments as stations, state = marker shape; vertical SVG on mobile, horizontal on desktop; lines draw in | flat light print | Overpass, Overpass Mono |
| `/prototype/hikyo/8/` | Passport | one page per environment, every state a rotated ink stamp (SVG turbulence filter); stamps slam in, production page first | cream | Bebas Neue, Special Elite, Fraunces |
| `/prototype/hikyo/9/` | Inventory | 16-bit RPG bag per environment, 5x2 bevelled slots, pixel glyphs; items drop in, violations blink twice | indigo, parchment | Pixelify Sans, VT323, Silkscreen |
| `/prototype/hikyo/10/` | Patch bay | channels x jacks, SVG cables and LEDs, "no normalled connections"; cables plug in, LEDs light | brushed anodized | Barlow, Barlow Condensed, Red Hat Mono |
| `/prototype/hikyo/11/` | Live page, enhanced | the current `index.astro` kept whole (imports `landing.css`, same copy, working theme toggle) plus: full 10-key matrix resolving column by column on load, schematic callouts on the three notable production cells (>= 80rem; numbered list below), per-environment tabs on mobile instead of a scrolling table, feature artifacts that play once on scroll (coverage wipe, reveal countdown that re-masks, checks stagger, terminal stream), scroll-progress hairline, kicker cursor, mobile-only stack marquee, theme cross-fade. Fake `sk_live…` fragment replaced with `cliRead` output. Drops ld+json, OG tags, PostHog. | dark default, light | Instrument Sans, IBM Plex Mono |

Every page renders from one dataset, `docs/site/src/lib/prototype/hikyo.ts`:
the `platform/payments-api` example matrix (10 keys, 4 groups, one all-or-none
group, a draft, a forbidden cell, two violations), the verified claim set, and
the CLI snippets. Secrets are always the `masked` constant; no page renders
plaintext or a fake plaintext fragment. `cliRead` targets `STRIPE_SECRET_KEY`,
which the matrix records as set in production, so static reveal demos (3 and 4)
never read a key the matrix shows as absent. Pages 2 and 5 tell a scenario that
declares and sets `STRIPE_WEBHOOK_SECRET` before reading it.

Shared pieces: `src/components/prototype/HikyoHead.astro` (noindex, PWA head,
title) and `src/components/prototype/HikyoSwitcher.astro` (fixed 1 to 10 bar;
on screens under 40rem it collapses to an "N / 10" pill that expands on tap via
a small bundled script, so it does not cover the matrix).

## Constraints honoured

- Astro-governed pages under strict CSP: no `is:inline` scripts, only bundled
  `<script>` blocks (pages 2 and 5, plus the switcher toggle on every page),
  fonts self-hosted through `@fontsource`.
- `scripts/build-pwa.mjs` now ignores `prototype/**/*`, so the prototypes are
  not precached into visitors' offline cache.
- No em-dash, no `as` casts, no `any`, no external assets, root-relative links
  with trailing slashes.
- Claims come from README and shipped docs only: no customers, testimonials,
  adoption numbers, pricing, compliance, or competitor table. Each page states
  0.x, pre-1.0, MPL-2.0, and no `/ee`.
- Reduced motion disables every animation. No page overflows horizontally at
  375px or 1280px (tables scroll inside their own containers).

## Verification

- `pnpm run verify` (Node 26.7.0), re-run after round three: astro check 0
  errors, build, CSP gate (59 governed pages), OSS policy gate, PostHog test,
  PWA offline test all passed; `dist/sw.js` has zero `prototype/hikyo` entries.
- Playwright (`hikyo-audit.mjs`, not in repo) at 375 and 1280 for every
  page: zero console errors, `scrollWidth - clientWidth = 0` on all twenty-two
  page/width pairs.
- CI builds with PostHog enabled (`POSTHOG_REQUIRED=true` plus host and token
  vars), which puts `array.js` in every page's CSP; `test-csp.mjs` then requires
  each governed page to render `<PostHog />`. The switcher and the index render
  it. Reproduce locally with
  `POSTHOG_REQUIRED=true PUBLIC_POSTHOG_HOST=https://eu.i.posthog.com PUBLIC_POSTHOG_PROJECT_TOKEN=phc_x pnpm run verify`.
- Mobile review is on the LAN: start the dev server bound to 0.0.0.0 with a
  tiny script calling Astro's `dev()` API via `pnpm exec node` (bare `astro
  dev` from the Bash tool exited before ready; `pnpm exec` is also needed for
  module resolution under the global virtual store).

## Known judgement calls

- Product docs disagree on inheritance (PRODUCT.md mentions "inherited" and a
  base chain, hierarchy.mdx says no environment inherits). All pages follow the
  shipped docs and the brief: no inheritance.
- Page 5 event 4 advances `STRIPE_WEBHOOK_SECRET` production to set (scenario
  state), so its full matrix differs from the dataset in that one cell. The page
  says so.
- Page 2 step 4 and page 5 event 4 describe publishing into a protected
  environment as confirmation plus a fresh ceremony, paraphrasing
  browser-operations and configuration docs; no `hikyo publish` CLI command is
  documented, so publish is shown as a web UI action.

- Round-two agent finding worth keeping: `steps(1, end)` plus a `calc()`
  animation delay over ~1s can leave the last staggered items stuck at frame 0
  in Chrome (finished `currentTime` lands an epsilon short). `steps(1, start)`
  fixes it (see 9.astro).
- Page 11 keeps the live page's nine em-dashes (comparison "partial" glyph,
  copied verbatim); AGENTS.md bans the character in new text, and 11 adds none.
- Round-two pages show per-key presence in words (`presence` field) rather
  than the raw `rule` JSON where the metaphor needed it; 6 prints both.

## Next steps

- Pick a direction (or combine), then port it into `src/pages/index.astro`.
- Delete the losing prototypes; the shared dataset can move under the live page.
- If more rounds happen: three agents asked for a shared short env label map
  (`dev/stg/prod`) and a per-kind stamp word next to `cellText`; add those to
  `hikyo.ts` once rather than per page.

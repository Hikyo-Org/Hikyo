# PR 507 — landing-page improvements

## Scope

- Redesigned the Astro landing page hierarchy, responsive layout, examples, comparison, FAQ, and calls to action.
- Aligned the roadmap underline with the complete clock and version label.
- Added the ordered workflow reveal described by `plans/001-ordered-flow-step-reveal.md`.
- Kept documentation navigation, documentation content, and documentation components outside the redesign.

## Integration

- Merged current `origin/main` after the feature commit.
- Preserved consent-gated PostHog initialization and CTA attribution from PR 506.
- Added attribution to the new FAQ documentation CTA.

## Verification

- `pnpm --dir docs/site verify`
- Astro: zero errors, warnings, and hints.
- Static build, OSS policy, PostHog, PWA, and offline-browser gates passed.
- Rendered motion checks covered exact timing, stationary borders, no replay, reduced motion, deep links, no JavaScript, and a 390px viewport.

Merged as 69efde32.

# Handoff: dev CI red on the 320px matrix legend story

## Result

`ci-dev` on `dev` failed from #1226 (9 pushes) on one Storybook story,
`routes-matrix--phone-legend-stays-on-screen-minimum`: a legend description was
99px wide where the story requires 100px, deterministically, on hosted Linux
Chromium. The story was added in #1226 and checked only on macOS, which renders
the widest term (`⚠ required · absent`) a few pixels narrower.

At phone widths the legend's description column now has a 100px floor
(`minmax(100px, 1fr)`), and a term wraps to make room instead of squeezing it
(`web/src/styles/app.css`, after the base `dl`/`dt` rules so source order lets
it win). A no-break space keeps `· absent` together, so the long term wraps as
`⚠ required` / `· absent`. Above 700px nothing changes.

## Evidence

- Hosted failures: runs 42196062576, 42199648752, 42205828306, 42211311154, 42217117470, 42218215732, 42226616295, 42273951723, all `expected 99 to be greater than or equal to 100`.
- Local mutation proof (macOS Chromium), widening the terms with `letter-spacing` to recreate the narrower column:
  - `0.06em`: `dev` fails (description 88.5px); the fix passes.
  - `0.15em`: `dev` fails (67.5px, text overflowing); the fix passes.
- `Matrix.stories.tsx`: 17/17 pass in dark and light. tsc, oxlint and `design:check` pass. `storybook build` and `check:storybook-docs` passed on the first revision of this fix.

## Remaining

- Hosted proof comes only from the `ci-dev` run after this merges into `dev`.
- The Storybook dark step failed first in every red run, so the light pass, `storybook build`, `check:storybook-docs` and `web-closure` have not run on dev since #1226. That run may surface further failures.
- The `Dependabot Updates` failure on `dev` (run 42279313491) is a separate workflow and was not investigated.

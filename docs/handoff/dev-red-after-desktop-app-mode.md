# Dev CI red after the desktop app mode and value-tip merges

## Result

`ci-dev` on `dev` had been red since #1458 (desktop app mode, #1215) and #1467 (matrix value tip). This change fixes each failure at its cause:

- **Storybook `Matrix > Value Tip`:** `visibleBox` in `web/src/routes/Matrix.tsx` measured a stuck group label by its key-column `th`. Since env-matrix 31 (#1449), the label's bar (`.matrix__group-row-inner`) overruns to the visible width with an opaque background. The check now measures the bar's box, and keeps reading "stuck" off the `th`, which pins 1px into the header. The story counts the stuck bar as covering.
- **CLI login e2e (`login.spec.ts`):** these tests still expected the CLI-specific copy that #1215 made caller-neutral, including the page's own second-factor alert. The server's refusal detail is still asserted at the API.
- **Workspace multi-instance e2e:**
  - `getByText` matched B's value twice once the hidden value tip carried the same text, so the test now targets `.matrix-cell__value`.
  - The "revoked 1 workspace session" failure was a knock-on from that first failure, which left a session behind.
  - CLI reauth's button is now `Authorize`.
- **Anonymous settings gate (`settings-bundles.ts`):** this test matched chunk file names. The shell's shared chunk is now named after `api/settings.ts`, which the shell already imported on green dev. The test now checks route-module ownership against the production inventory, and fails on any asset the inventory doesn't know.
- **Stale labels:** the `navigation.ts` surface labels now follow the neutral headings, as `docs/adr/desktop-app.md` requires.

## Evidence (local, final source)

- `tsc --noEmit` and `oxlint` on the touched files: clean.
- `vitest --project unit`, `src/routes/Matrix` plus `src/app`: 289 passed.
- `vitest --project storybook` on Matrix, CLILogin and CLIReauth stories: 37 passed. A control run without the `Matrix.tsx` change still fails Value Tip.
- Playwright groups 3 and 4 (`instance-admin`, `settings`, `workspace`, `machine-access`, `login`, `reveal`) against a fresh `-tags ui` build: 204/204 passed on desktop and 204/204 on mobile.

## Main

Main's red checks are not its own CI:

- `promote` (nightly-promote) refuses because no green `dev` commit exists. Its source proof also needs the policy files listed in `scripts/ci/source-proof/resolve.go` to match main byte-for-byte.
- The remaining failures are gate publisher attempts for superseded PR heads, attributed to main's head SHA. They clear on main's next commit.

Many gate attempts fail their 60s budget because the Jobs API `started_at` precedes the runner's real start by up to about 60s when runners are saturated. This chronic noise is not addressed here.

## Next steps

1. Merge this PR into `dev`, then confirm `ci-dev` is green on the merge commit.
2. If #1498 (which changes `scripts/ci/test-core-packages.sh`) lands on main first, backmerge main into dev before the next nightly promote, or the source proof refuses again.
3. Confirm the next `nightly-promote` run succeeds, which turns main's head green.
4. Optional follow-up: move the workspace-spec remote cleanup into `finally`, so one failure no longer cascades.

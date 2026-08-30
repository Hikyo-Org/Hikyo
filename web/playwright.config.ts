import { defineConfig, devices } from '@playwright/test';

import { BASE_URL } from './e2e/fixtures/instance.ts';

/**
 * The flow suite (mvp-boundary S3).
 *
 * Two projects, one browser. Desktop and mobile are the two shapes the locked
 * design has to work in — DESIGN.md is mobile-first and the matrix "earns
 * phone screens" — so a flow that only passes at 1280px has not passed.
 *
 * Chromium only, and that is a decision rather than an omission: the pinned
 * assertion set needs `forced-colors` emulation and axe-core's colour
 * sampling, and Chromium is where both are reliable. Cross-browser rendering
 * is a different question from "is this surface accessible and on-token".
 *
 * ## Why `workers: 1`, and where the parallelism lives instead
 *
 * A run holds ONE administrator per instance, and three pieces of its state are
 * strictly sequential: the TOTP ledger (a code is single-use per step, and the
 * spent step travels between processes in a file), the passkey's signature
 * counter (a counter that does not advance is how a CLONED authenticator is
 * detected, so a replayed one disables the credential), and the shared session
 * itself (a flow that changes grants re-mints it for everybody). Two workers in
 * one process race all three, and each race surfaces as an `unauthenticated`
 * several tests away from its cause.
 *
 * So the suite is parallelised across RUNNERS instead — see the `web` job's
 * matrix in .github/workflows/ci.yml, which fans out over (viewport project ×
 * flow group). Every runner boots its own instances, seeds its own tenant and
 * mints its own passkey, so it shares none of that state: splitting the FLOWS
 * across runners races nothing that two workers in one process would.
 *
 * Each leg runs its group's specs positionally, which makes the teardown skip
 * its per-run execution-closure check (a positional run is partial by design);
 * the `web-closure` aggregator job then merges every leg's run log and runs the
 * closure over the whole set, so no claim escapes. Sharding with `--shard`
 * would split the flows the same way but WITHOUT that positional marker, so the
 * log would look complete while missing flows — global teardown refuses it by
 * name rather than failing as a pile of unexecuted claims.
 */
export default defineConfig({
  testDir: './e2e/flows',
  outputDir: './test-results',
  fullyParallel: false,
  forbidOnly: process.env['CI'] !== undefined,
  retries: 0,
  workers: 1,
  reporter: process.env['CI'] !== undefined ? [['github'], ['list']] : [['list']],
  globalSetup: './e2e/global-setup.ts',
  globalTeardown: './e2e/global-teardown.ts',
  use: {
    baseURL: BASE_URL,
    trace: 'retain-on-failure',
    // Colour scheme is emulated per assertion, not fixed here. Chromium never
    // reports `no-preference` — the CSS keyword exists, the platform signal
    // does not — so "dark unless the platform asks for light" is proven by
    // emulating each preference in turn rather than by picking one globally.
  },
  projects: [
    {
      name: 'desktop',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } },
    },
    {
      name: 'mobile',
      // A real phone shape with touch and a device pixel ratio, because the
      // 44px target assertion is meaningless at a desktop viewport.
      use: { ...devices['Pixel 5'] },
    },
  ],
});

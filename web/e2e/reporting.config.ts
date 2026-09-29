import { defineConfig, devices } from '@playwright/test';

/**
 * Delivery-target condition reporting, controller to browser (#791).
 *
 * Not part of the flow suite and not in its registry: the server, the kind
 * cluster and the operator belong to the Go test that launches this config
 * (internal/isolation/k8s_reporting_e2e_test.go, `-tags k8se2e`), which hands
 * over the origin below. Run it through scripts/ci/reporting-e2e.sh.
 *
 * The timeout is a budget for cluster work, not a wait: one step creates a CR,
 * lets the operator win its lease, reconcile and report, and each of those
 * polls is bounded at 60 s on the Go side.
 */
const origin = process.env['HIKYO_REPORTING_E2E_ORIGIN'];
if (origin === undefined || origin === '') {
  throw new Error('HIKYO_REPORTING_E2E_ORIGIN is not set: run scripts/ci/reporting-e2e.sh');
}

export default defineConfig({
  testDir: './reporting',
  outputDir: '../test-results/reporting',
  fullyParallel: false,
  forbidOnly: process.env['CI'] !== undefined,
  workers: 1,
  retries: 0,
  timeout: 240_000,
  reporter: [['list']],
  use: {
    ...devices['Desktop Chrome'],
    // Tall enough that every target row is inside the viewport, which is what
    // the spec captures: the page scrolls inside its own frame, so a full-page
    // capture would not reach further than this does.
    viewport: { width: 1280, height: 1600 },
    baseURL: origin,
    // The in-process server presents the Go test certificate.
    ignoreHTTPSErrors: true,
    actionTimeout: 15_000,
    trace: 'retain-on-failure',
  },
});

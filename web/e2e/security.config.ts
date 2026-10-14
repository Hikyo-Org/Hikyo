import { defineConfig, devices } from '@playwright/test';
import { BASE_URL } from './fixtures/instance.ts';

export default defineConfig({
  testDir: './security', testMatch: '*.spec.ts',
  outputDir: '../test-results/security',
  fullyParallel: false, workers: 1, retries: 0,
  forbidOnly: process.env['CI'] !== undefined,
  globalSetup: './security/global-setup.ts',
  globalTeardown: './security/global-teardown.ts',
  reporter: [['list'], ['./security/receipt-reporter.ts']],
  use: {
    ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 },
    baseURL: BASE_URL, trace: 'retain-on-failure',
  },
});

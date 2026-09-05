import { fileURLToPath } from 'node:url';

import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './session-epoch',
  outputDir: '../test-results/session-epoch',
  fullyParallel: false,
  forbidOnly: process.env['CI'] !== undefined,
  workers: 1,
  retries: 0,
  use: {
    ...devices['Desktop Chrome'],
    baseURL: 'http://localhost:4319',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 4319 --strictPort',
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    url: 'http://localhost:4319/e2e/session-epoch/harness.html',
    reuseExistingServer: false,
  },
});

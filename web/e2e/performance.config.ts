import { fileURLToPath } from 'node:url';
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './performance', testMatch: '*.spec.ts',
  outputDir: '../test-results/matrix-performance/results',
  fullyParallel: false, workers: 1, retries: 0,
  forbidOnly: process.env['CI'] !== undefined,
  reporter: [['list']],
  use: { baseURL: 'http://127.0.0.1:4320', trace: 'retain-on-failure' },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } },
    { name: 'mobile', use: { ...devices['Pixel 5'] } },
  ],
  webServer: {
    command: 'pnpm exec vite build --config e2e/performance/vite.config.ts && pnpm exec vite preview --config e2e/performance/vite.config.ts --host 0.0.0.0 --port 4320 --strictPort',
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    url: 'http://127.0.0.1:4320/e2e/performance/index.html', reuseExistingServer: false,
    timeout: 120_000,
  },
});

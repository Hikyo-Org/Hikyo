import { existsSync } from 'node:fs';
import { defineConfig, devices } from '@playwright/test';

if (!existsSync('demo-dist/index.html')) {
  throw new Error('Run pnpm run build:demo before testing the static demo.');
}

// Exercise the shipped /demo/ artifact, without starting Hikyo or a mock server.
export default defineConfig({
  testDir: './demo',
  outputDir: '../test-results/demo',
  fullyParallel: true,
  forbidOnly: process.env['CI'] !== undefined,
  workers: 2,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:5196/demo/',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } },
    { name: 'mobile', use: { ...devices['Pixel 5'] } },
  ],
  webServer: {
    command: 'pnpm run preview:demo --port 5196 --strictPort',
    cwd: process.cwd(),
    url: 'http://127.0.0.1:5196/demo/',
    reuseExistingServer: false,
  },
});

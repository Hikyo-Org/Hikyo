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
    { name: 'desktop', testIgnore: 'replay.spec.ts', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } },
    { name: 'mobile', testIgnore: 'replay.spec.ts', use: { ...devices['Pixel 5'] } },
    { name: 'replay', testMatch: 'replay.spec.ts', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 }, baseURL: 'http://127.0.0.1:5197/demo/' } },
  ],
  webServer: [{
    command: 'pnpm run preview:demo --port 5196 --strictPort',
    cwd: process.cwd(),
    url: 'http://127.0.0.1:5196/demo/',
    reuseExistingServer: false,
  }, {
    // Replay needs an analytics-enabled build, whatever the main artifact was given.
    // The placeholder token is never sent: replay.spec.ts answers every PostHog request.
    command: 'vite build --mode demo --outDir demo-dist-replay && node ../docs/site/scripts/test-csp.mjs --demo demo-dist-replay && vite preview --mode demo --outDir demo-dist-replay --port 5197 --strictPort',
    cwd: process.cwd(),
    env: { PUBLIC_POSTHOG_HOST: 'https://eu.i.posthog.com', PUBLIC_POSTHOG_PROJECT_TOKEN: 'phc_demo_replay_test_placeholder' },
    url: 'http://127.0.0.1:5197/demo/',
    timeout: 300_000,
    reuseExistingServer: false,
  }],
});

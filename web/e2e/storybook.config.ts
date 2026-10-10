import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

import { defineConfig, devices } from '@playwright/test';

const output = resolve(process.cwd(), process.env['STORYBOOK_STATIC_DIR'] ?? 'storybook-static');
for (const file of ['index.html', 'iframe.html', 'index.json']) {
  if (!existsSync(resolve(output, file))) {
    throw new Error(`Built Storybook artifact missing: ${resolve(output, file)}. Run build-storybook first or set STORYBOOK_STATIC_DIR to an immutable build.`);
  }
}

function shellArgument(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

// One central mechanics contract, separate from the full per-story palette
// suites. Serve existing artifacts only; never rebuild underneath the browser.
export default defineConfig({
  testDir: './storybook',
  outputDir: '../test-results/storybook-theme',
  fullyParallel: false,
  forbidOnly: process.env['CI'] !== undefined,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: {
    ...devices['Desktop Chrome'],
    viewport: { width: 1280, height: 900 },
    baseURL: 'http://127.0.0.1:4326/storybook/',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: `node node_modules/vite/bin/vite.js preview --host 0.0.0.0 --port 4326 --strictPort --base /storybook/ --outDir ${shellArgument(output)}`,
    cwd: process.cwd(),
    url: 'http://127.0.0.1:4326/storybook/',
    reuseExistingServer: false,
  },
});

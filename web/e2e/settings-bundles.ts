import { expect, test } from '@playwright/test';
import { z } from 'zod';

import { BASE_URL, readSeed, STORAGE_STATE } from './fixtures/instance.ts';

const samples = z.coerce.number().int().min(1).max(20)
  .parse(process.env['HIKYO_SETTINGS_BUNDLE_SAMPLES'] ?? '1');
const seed = readSeed();
const org = `/orgs/${seed.org}`;
const project = `${org}/projects/${seed.project}`;
const screens = [
  { module: 'AccountSecurity', path: '/settings', heading: 'Account & security' },
  { module: 'OrgSettings', path: `${org}/settings`, heading: 'Organisation settings' },
  { module: 'ProjectSettings', path: `${project}/settings`, heading: 'Project settings' },
  { module: 'Members', path: `${org}/members`, heading: 'Members' },
  { module: 'ScimProvisioning', path: `${org}/scim`, heading: 'SCIM provisioning' },
  { module: 'Audit', path: `${org}/audit`, heading: 'Audit' },
  { module: 'ChangeApprovals', path: `${project}/change-approvals`, heading: 'Change approvals' },
  { module: 'TemporaryAccess', path: `${project}/temporary-access`, heading: 'Temporary access' },
  { module: 'MachineAccess', path: `${project}/machine-access`, heading: 'Machine access' },
  { module: 'Adapters', path: `${project}/adapters`, heading: 'Deployment adapters' },
  { module: 'InstanceAdmin', path: '/instance', heading: 'Instance settings' },
  { module: 'InstanceConfig', path: '/instance/config', heading: 'Hikyo configuration' },
];

// Run against the embedded production SPA. Each sample gets a fresh browser
// context, so its resource timings include cold network fetches, not cache hits.
for (const screen of screens) {
  test(`cold settings navigation: ${screen.module}`, async ({ browser }, testInfo) => {
    const measurements = [];
    test.slow();
    for (let sample = 0; sample < samples; sample += 1) {
      const context = await browser.newContext({
        storageState: STORAGE_STATE,
        viewport: testInfo.project.use.viewport,
        userAgent: testInfo.project.use.userAgent,
        isMobile: testInfo.project.use.isMobile,
        hasTouch: testInfo.project.use.hasTouch,
        deviceScaleFactor: testInfo.project.use.deviceScaleFactor,
      });
      try {
        const page = await context.newPage();
        const errors: string[] = [];
        const headers: Promise<{ file: string; status: number; encoding: string | null; cache: string | null }>[] = [];
        page.on('response', (asset) => {
          const file = new URL(asset.url()).pathname;
          if (file.startsWith('/assets/') && file.endsWith('.js')) {
            headers.push(Promise.all([asset.headerValue('content-encoding'), asset.headerValue('cache-control')])
              .then(([encoding, cache]) => ({ file, status: asset.status(), encoding, cache })));
          }
        });
        page.on('pageerror', (error) => errors.push(error.message));
        await page.addInitScript((heading) => {
          performance.setResourceTimingBufferSize(1000);
          const observer = new MutationObserver(() => {
            if (Array.from(document.querySelectorAll('h1')).some((node) => node.textContent?.startsWith(heading))) {
              performance.mark('settings-heading');
              observer.disconnect();
            }
          });
          observer.observe(document, { childList: true, subtree: true });
        }, screen.heading);
        const response = await page.goto(`${BASE_URL}${screen.path}`);
        expect(response?.status()).toBe(200);
        const csp = await response?.headerValue('content-security-policy');
        expect(csp).toContain("script-src 'self'");
        expect(csp).not.toContain("'unsafe-inline'");
        expect(await response?.headerValue('cache-control')).toBe('private, no-cache');
        await expect(page.getByRole('heading', { level: 1, name: new RegExp(`^${screen.heading}`) })).toBeVisible();
        await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
        const measurement = await page.evaluate(() => ({
          headingMs: performance.getEntriesByName('settings-heading')[0]?.startTime,
          viewport: { width: innerWidth, height: innerHeight, deviceScaleFactor: devicePixelRatio },
          userAgent: navigator.userAgent,
          paints: performance.getEntriesByType('paint').map((entry) => ({ name: entry.name, ms: entry.startTime })),
          scripts: performance.getEntriesByType('resource')
            .filter((entry): entry is PerformanceResourceTiming => entry instanceof PerformanceResourceTiming && new URL(entry.name).pathname.endsWith('.js'))
            .map((entry) => ({
              file: new URL(entry.name).pathname,
              startMs: entry.startTime,
              endMs: entry.responseEnd,
              durationMs: entry.duration,
              transferBytes: entry.transferSize,
              encodedBytes: entry.encodedBodySize,
              decodedBytes: entry.decodedBodySize,
            })),
        }));
        expect(measurement.headingMs).toBeGreaterThan(0);
        expect(measurement.scripts.length).toBeGreaterThan(0);
        const fetched = measurement.scripts.map((entry) => entry.file);
        expect(fetched.some((file) => file.startsWith(`/assets/${screen.module}-`))).toBe(true);
        for (const other of screens) {
          if (other.module !== screen.module) {
            expect(fetched.some((file) => file.startsWith(`/assets/${other.module}-`))).toBe(false);
          }
        }
        for (const asset of measurement.scripts) expect(asset.transferBytes).toBeGreaterThan(0);
        expect(errors).toEqual([]);
        const representations = await Promise.all(headers);
        for (const asset of representations) {
          expect(asset.status).toBe(200);
          expect(asset.cache).toBe('public, max-age=31536000, immutable');
          // Tiny shared chunks may correctly retain identity when compression grows them.
          expect([null, 'br', 'gzip']).toContain(asset.encoding);
          const resource = measurement.scripts.find((entry) => entry.file === asset.file);
          if ((resource?.decodedBytes ?? 0) > 1024) expect(asset.encoding).not.toBeNull();
        }
        measurements.push({ ...measurement, representations });
      } finally {
        await context.close();
      }
    }
    await testInfo.attach(`${screen.module}-cold-navigation.json`, {
      body: JSON.stringify({ screen: screen.module, viewport: testInfo.project.name, browserVersion: browser.version(), samples: measurements }, null, 2),
      contentType: 'application/json',
    });
  });
}

test('anonymous settings navigation retains the sign-in gate', async ({ page }) => {
  await page.goto(`${BASE_URL}/settings`);
  await expect(page).toHaveURL(`${BASE_URL}/login`);
  await expect(page.getByRole('heading', { name: 'Sign in to Hikyo', exact: true })).toBeVisible();
  const assets = await page.evaluate(() => performance.getEntriesByType('resource').map((entry) => new URL(entry.name).pathname));
  expect(assets.some((file) => /\/(?:settings|AccountSecurity)-/.test(file))).toBe(false);
});

test.describe('settings navigation history', () => {
  test.use({ storageState: STORAGE_STATE });
  test('switches lazy screens through the navigation and browser history', async ({ page }, testInfo) => {
    await page.goto(`${BASE_URL}${org}/settings`);
    await expect(page.getByRole('heading', { level: 1, name: /^Organisation settings/ })).toBeVisible();
    const navigation = page.getByRole('navigation', { name: 'Sections', exact: true, includeHidden: true });
    await navigation.evaluate((node) => node.setAttribute('data-settings-shell', 'retained'));
    const menu = page.getByRole('button', { name: 'Menu', exact: true });
    if (await menu.isVisible()) await menu.click();
    await navigation.getByRole('link', { name: 'SCIM provisioning', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'SCIM provisioning' })).toBeVisible();
    await expect(navigation).toHaveAttribute('data-settings-shell', 'retained');
    await testInfo.attach('settings-navigation', { body: await page.screenshot(), contentType: 'image/png' });
    await page.goBack();
    await expect(page.getByRole('heading', { level: 1, name: /^Organisation settings/ })).toBeVisible();
    await expect(navigation).toHaveAttribute('data-settings-shell', 'retained');
  });
});

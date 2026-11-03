import { gunzipSync } from 'node:zlib';
import { expect, test, type Page } from '@playwright/test';
import { z } from 'zod';

// Runs against a build given a placeholder PostHog token (demo.config.ts). Every
// request to PostHog is answered here, so these tests send nothing to a project.
const ingestion = 'https://eu.i.posthog.com';
const assets = 'https://eu-assets.i.posthog.com';
const consentKey = 'hikyo.demo.replay.consent';
const canary = 'sk-typed-canary-7f3a';

// Loose, so a field added outside `properties` still reaches the canary search.
const eventSchema = z.looseObject({ event: z.string(), properties: z.record(z.string(), z.unknown()) });
const bodySchema = z.union([
  z.object({ batch: z.array(eventSchema) }).transform(({ batch }) => batch),
  z.array(eventSchema),
  eventSchema.transform((event) => [event]),
]);

type Sent = { url: string; path: string; events: z.infer<typeof eventSchema>[] };

const gunzipIfCompressed = (value: Buffer) => (value[0] === 0x1f && value[1] === 0x8b ? gunzipSync(value) : value);

function decodeEvents(body: Buffer | null): Sent['events'] {
  if (body === null) return [];
  const text = gunzipIfCompressed(body).toString('utf8');
  const form = text.startsWith('data=') ? new URLSearchParams(text).get('data') : null;
  return bodySchema.parse(JSON.parse(form === null ? text : Buffer.from(form, 'base64').toString('utf8')));
}

/** Everything sent, including URLs. Recorder payloads hold gzip inside latin1 strings. */
function sentText(sent: Sent[]): string {
  const expand = (value: unknown): unknown => {
    if (typeof value === 'string' && value.charCodeAt(0) === 0x1f && value.charCodeAt(1) === 0x8b) {
      return gunzipSync(Buffer.from(value, 'latin1')).toString('utf8');
    }
    if (Array.isArray(value)) return value.map(expand);
    if (typeof value === 'object' && value !== null) return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, expand(item)]));
    return value;
  };
  return JSON.stringify(sent.map(({ url, events }) => ({ url, events: expand(events) })));
}

async function observePostHog(page: Page) {
  const sent: Sent[] = [];
  const contacted: string[] = [];
  await page.context().route(`${assets}/**`, async (route) => {
    contacted.push(route.request().url());
    await route.fulfill({
      contentType: 'application/json',
      headers: { 'access-control-allow-origin': '*' },
      // This project's real remote config, including everything the demo must override.
      body: JSON.stringify({
        analytics: { endpoint: '/i/v0/e/' },
        captureDeadClicks: true,
        capturePerformance: { network_timing: true, web_vitals: true, web_vitals_allowed_metrics: null },
        hasFeatureFlags: false,
        supportedCompression: ['gzip', 'gzip-js'],
        heatmaps: { captureMode: 'url_allowlist', urlAllowlist: [], urlAllowlistEnforced: false },
        sessionRecording: { consoleLogRecordingEnabled: true, endpoint: '/s/', masking: { maskAllInputs: true }, networkPayloadCapture: null, recordCanvas: false, recorderVersion: 'v2', sampleRate: null },
      }),
    });
  });
  await page.context().route(`${ingestion}/**`, async (route) => {
    const request = route.request();
    contacted.push(request.url());
    sent.push({ url: request.url(), path: new URL(request.url()).pathname, events: decodeEvents(request.postDataBuffer()) });
    await route.fulfill({ contentType: 'application/json', headers: { 'access-control-allow-origin': '*' }, body: '{"status":"Ok"}' });
  });
  const assetsRequested: string[] = [];
  page.on('request', (request) => assetsRequested.push(request.url()));
  return { sent, contacted, assetsRequested };
}

const snapshots = (sent: Sent[]) => sent.filter(({ path }) => path === '/s/');

const campaignCanary = 'campaign-canary-52c1';
const clickCanary = 'click-canary-52c1';

/** Uploads batch for seconds, so wait for proof that the moment under test has arrived. */
async function interactUntilSent(page: Page, sent: Sent[], marker: string) {
  let step = 0;
  await expect(async () => {
    step += 1;
    await page.mouse.move(200 + (step % 2) * 120, 200 + (step % 2) * 120);
    expect(sentText(sent)).toContain(marker);
  }).toPass({ timeout: 30_000 });
}

/**
 * The recorder holds its buffer until it sees an interaction, and starts only once
 * its chunk has loaded. Keep interacting so that order cannot decide the outcome.
 */
async function interactUntilUpload(page: Page, sent: Sent[]) {
  const before = snapshots(sent).length;
  let step = 0;
  await expect(async () => {
    step += 1;
    await page.mouse.move(200 + (step % 2) * 120, 200 + (step % 2) * 120);
    expect(snapshots(sent).length).toBeGreaterThan(before);
  }).toPass({ timeout: 30_000 });
}

// The SDK drops events when the user agent, client hints or webdriver flag look
// automated, so headless Chromium must clear all three to reach the recorder.
test.use({ userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36' });

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'webdriver', { get: () => false });
    Object.defineProperty(navigator, 'userAgentData', { get: () => undefined });
    // Routes do not intercept beacons, so send an unload flush where they can see it.
    Object.defineProperty(navigator, 'sendBeacon', {
      value: (url: string, data: BodyInit) => {
        void fetch(url, { method: 'POST', body: data, keepalive: true }).catch(() => undefined);
        return true;
      },
    });
  });
  page.on('pageerror', (error) => { throw error; });
  page.on('console', (message) => {
    if (message.type() === 'error') throw new Error(`Browser console error: ${message.text()}`);
  });
});

test('nothing loads or leaves before consent, and declining keeps it that way', async ({ page }) => {
  const { contacted, assetsRequested } = await observePostHog(page);
  await page.goto('./');
  const sdkChunks = () => assetsRequested.filter((url) => /module\.full|posthog/i.test(url));

  await page.getByRole('button', { name: 'No thanks', exact: true }).click();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Allow session recording', exact: true })).toBeVisible();
  await page.waitForTimeout(1500);
  expect(contacted).toEqual([]);
  expect(sdkChunks()).toEqual([]);
});

test('consent given on the landing page does not start recording', async ({ page }) => {
  const { contacted } = await observePostHog(page);
  await page.addInitScript(() => localStorage.setItem('hikyo.analytics.consent', 'granted'));
  await page.goto('./');
  await expect(page.getByRole('button', { name: 'No thanks', exact: true })).toBeVisible();
  await page.waitForTimeout(1500);
  expect(contacted).toEqual([]);
});

test('consent records the session, hides typed values, survives an account switch, and stops', async ({ page }) => {
  test.setTimeout(120_000);
  const { sent, contacted } = await observePostHog(page);
  await page.goto(`./?account=admin&utm_campaign=${campaignCanary}&gclid=${clickCanary}`);
  await page.getByRole('button', { name: 'Allow session recording', exact: true }).click();
  await expect(page.getByText('Recording this demo session.')).toBeVisible();
  await interactUntilUpload(page, sent);
  expect(new Set(contacted.map((url) => new URL(url).origin))).toEqual(new Set([ingestion, assets]));

  // Publishing makes the app render the typed value as cell text and in its label.
  await page.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
  await page.getByRole('textbox', { name: 'development value' }).fill(canary);
  await page.getByRole('button', { name: 'Save 1 draft', exact: true }).click();
  await page.getByRole('button', { name: /Publish drafts/ }).click();
  await page.getByRole('button', { name: /Publish selected/ }).click();
  const cell = page.getByRole('button', { name: new RegExp(`^LOG_LEVEL in development: ${canary}`) });
  await expect(cell).toHaveText(canary);
  // The revision link renders with the published cell, so this batch holds that change.
  await interactUntilSent(page, sent, 'rev 2 · history');

  const recorded = sentText(sent);
  expect(recorded).toContain('Environment matrix');
  expect(recorded).not.toContain(canary);
  expect(recorded).not.toContain(campaignCanary);
  expect(recorded).not.toContain(clickCanary);
  expect(recorded).toContain('account=admin');

  // Only the recording and its pageviews leave, despite what the project config enables.
  const events = sent.flatMap(({ events: batch }) => batch);
  const sessions = () => new Set(sent.flatMap(({ events: batch }) => batch).map(({ properties }) => properties['$session_id']));
  expect(events.filter(({ event }) => event !== '$snapshot' && event !== '$pageview')).toEqual([]);
  expect(events.every(({ properties }) => properties['$geoip_disable'] === true)).toBe(true);
  expect(sessions().size).toBe(1);
  expect(events.filter((event) => '$set' in event || '$set_once' in event)).toEqual([]);
  const pageview = events.find(({ event }) => event === '$pageview');
  expect(Object.keys(pageview?.properties ?? {}).toSorted()).toEqual([
    '$current_url', '$geoip_disable', '$host', '$pathname', '$process_person_profile', '$session_id', '$window_id', 'distinct_id', 'token',
  ]);

  // An account switch reloads the document, and the recording must carry on.
  await page.getByRole('combobox', { name: 'Try as' }).selectOption('viewer');
  await expect(page.getByRole('combobox', { name: 'Try as' })).toHaveValue('viewer');
  await expect(page.getByRole('button', { name: /^LOG_LEVEL in development:/ })).toBeVisible();
  await interactUntilUpload(page, sent);
  expect(sessions().size).toBe(1);

  await page.getByRole('button', { name: 'Stop session recording', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Allow session recording', exact: true })).toBeVisible();
  // Let a flush already in flight finish, then require silence while the page changes.
  await page.waitForTimeout(3000);
  const afterStop = contacted.length;
  await page.getByRole('button', { name: /^LOG_LEVEL in development:/ }).click();
  await page.waitForTimeout(6000);
  expect(contacted.length).toBe(afterStop);
  expect(await page.evaluate((key) => localStorage.getItem(key), consentKey)).toBe('denied');
  await page.getByRole('button', { name: 'Close', exact: true }).click();

  // Allowing again resumes, because the SDK kept no opt-out state, as a new visitor.
  await page.getByRole('button', { name: 'Allow session recording', exact: true }).click();
  await interactUntilUpload(page, sent);
  expect(sessions().size).toBe(2);
  expect(sentText(sent)).not.toContain(canary);
  expect(await page.context().cookies()).toEqual([]);
});

test('stopping in one tab stops recording in another', async ({ page, context }) => {
  test.setTimeout(120_000);
  const { sent } = await observePostHog(page);
  await page.goto('./');
  await page.getByRole('button', { name: 'Allow session recording', exact: true }).click();
  await interactUntilUpload(page, sent);

  const other = await context.newPage();
  await other.goto('./');
  await other.getByRole('button', { name: 'Stop session recording', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Allow session recording', exact: true })).toBeVisible();
  await other.close();
  await page.waitForTimeout(3000);
  const afterStop = sent.length;
  await page.mouse.move(420, 420);
  await page.getByRole('combobox', { name: 'Try as' }).focus();
  await page.waitForTimeout(6000);
  expect(sent.length).toBe(afterStop);
});

test('Back cannot restore campaign parameters once recording', async ({ page }) => {
  test.setTimeout(120_000);
  const { sent } = await observePostHog(page);
  await page.goto(`./?utm_campaign=${campaignCanary}`);
  await expect(page.getByRole('link', { name: 'Machine access', exact: true })).toBeVisible();
  expect(page.url()).not.toContain(campaignCanary);
  await page.getByRole('link', { name: 'Machine access', exact: true }).click();
  await page.getByRole('button', { name: 'Allow session recording', exact: true }).click();
  await interactUntilUpload(page, sent);
  await page.goBack();
  await expect(page.getByRole('heading', { name: 'Environment matrix', exact: true })).toBeVisible();
  await interactUntilUpload(page, sent);
  expect(sentText(sent)).not.toContain(campaignCanary);
});

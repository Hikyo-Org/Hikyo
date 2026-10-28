import { expect, test, type Page } from '@playwright/test';
import { zOrgList, zEnvironmentSettings } from '@hikyo/zod';
import { browserApi } from '../fixtures/api.ts';
import { ADMIN, BASE_URL, STORAGE_STATE } from '../fixtures/instance.ts';
import { startCanary } from './canary.ts';
import { SECURITY_FIXTURE_CONFIGURATION } from './configuration.ts';
import { readSecuritySeed } from './seed.ts';
import { readStoredContent } from './stored-content.ts';

test.use({ storageState: STORAGE_STATE });

type Observation = { executed: boolean; canaryRequests: number; leakedStorage: string[] };

function violations(observation: Observation): string[] {
  const found: string[] = [];
  if (observation.executed) found.push('script-execution');
  if (observation.canaryRequests !== 0) found.push('forbidden-egress');
  if (observation.leakedStorage.length !== 0) found.push('credential-storage');
  return found;
}

async function observe(page: Page, canaryRequests: number): Promise<Observation> {
  // Compare real session material as well as recognizable issued artifact
  // grammar. Harmless preference keys are not treated as credentials.
  const cookies = await page.context().cookies(BASE_URL);
  const protectedValues = cookies.filter((cookie) => cookie.httpOnly).map((cookie) => cookie.value);
  const seed = readSecuritySeed();
  const factorSecret = new URL(seed.otpauth).searchParams.get('secret');
  const sensitive = [...protectedValues, seed.token, ADMIN.password, seed.otpauth,
    ...(factorSecret === null ? [] : [factorSecret])];
  return page.evaluate(({ sensitive, canaryRequests }) => {
    const leakedStorage: string[] = [];
    for (const { name, storage } of [{ name: 'localStorage', storage: localStorage }, { name: 'sessionStorage', storage: sessionStorage }]) {
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index);
        if (key === null) continue;
        const value = storage.getItem(key) ?? '';
        if (sensitive.some((secret) => secret.length > 0 && value.includes(secret)) ||
            /hik_1_[a-z]+_[0-9A-Za-z]{7,49}/.test(value)) {
          leakedStorage.push(`${String(name)}:${key}`);
        }
      }
    }
    return { executed: document.documentElement.dataset['hikyoSecurity'] !== undefined, canaryRequests, leakedStorage };
  }, { sensitive, canaryRequests });
}

test('stored-content: native authenticated production SPA renders hostile tenant names, values and URLs safely', async ({ page }) => {
  const seed = readSecuritySeed();
  const settingsBase = `/api/v1/orgs/${seed.org}/projects/${seed.project}/environments`;
  const { payloads } = readStoredContent();
  // Inspect the real settings reads the matrix needs instead of issuing
  // duplicate fixture reads against the same source allowance.
  const developmentRead = page.waitForResponse(`${BASE_URL}${settingsBase}/${seed.dev}/settings`);
  const protectedRead = page.waitForResponse(`${BASE_URL}${settingsBase}/${seed.prod}/settings`);
  const matrix = `${BASE_URL}/orgs/${seed.org}/projects/${seed.project}/matrix`;
  const response = await page.goto(matrix);
  expect(response?.headers()['content-security-policy']).toContain('script-src');
  await expect(page.getByRole('button', { name: `Organisation ${seed.orgName}`, exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: `Organisation ${seed.orgBName}`, exact: true })).toBeVisible();
  expect(violations(await observe(page, readStoredContent().hits))).toEqual([]);
  expect(new URL(page.url()).origin).toBe(BASE_URL);
  await expect(page).toHaveURL(matrix);
  const developmentResponse = await developmentRead;
  const protectedResponse = await protectedRead;
  expect(developmentResponse.status()).toBe(200);
  expect(protectedResponse.status()).toBe(200);
  const development = zEnvironmentSettings.parse(await developmentResponse.json());
  const protectedEnvironment = zEnvironmentSettings.parse(await protectedResponse.json());
  expect(development.protected).toBe(false);
  expect(development.reauth_window_seconds).toBe(SECURITY_FIXTURE_CONFIGURATION.development_disclosure_window_seconds);
  // The product caps every protected environment's effective window at zero,
  // even when its stored optional setting has no explicit window override.
  expect(protectedEnvironment.protected).toBe(true);
  for (const value of payloads) await expect(page.locator('.matrix-cell__value').getByText(value, { exact: true })).toBeVisible();
  await expect(page.locator('[onload], [onerror], a[href^="javascript:"]')).toHaveCount(0);
  await expect.poll(async () => violations(await observe(page, readStoredContent().hits))).toEqual([]);
  // A real completion barrier after two paints and all DOM assertions;
  // arbitrary sleeps cannot prove the malicious payload reached a sink.
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  expect(violations(await observe(page, readStoredContent().hits))).toEqual([]);
  await page.reload();
  await expect(page.locator('.matrix-cell__value').getByText(payloads[0] ?? '', { exact: true })).toBeVisible();
  expect(violations(await observe(page, readStoredContent().hits))).toEqual([]);
});

test('hostile-origin: cross-origin page cannot read authenticated data or mutate protected tenant state', async ({ page, context }) => {
  const canary = await startCanary();
  try {
    // A native discovery document establishes the application's storage origin
    // without starting an unrelated SPA refetch tree. The authorized controls
    // use real saved cookies; attacks run on the canary-origin page below.
    const document = await page.goto('/api/v1/meta');
    expect(document?.status()).toBe(200);
    const before = await browserApi(page, 'GET', '/api/v1/orgs', zOrgList);
    const attacker = await context.newPage();
    await attacker.goto(canary.origin);
    const results = await attacker.evaluate(async (base) => {
      async function probe(path: string, method: string): Promise<boolean> {
        try {
          const response = await fetch(`${base}${path}`, {
            method, credentials: 'include',
            ...(method === 'POST' ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'forbidden-origin-org' }) } : {}),
          });
          await response.text();
          return true;
        } catch { return false; }
      }
      return { read: await probe('/api/v1/orgs', 'GET'), write: await probe('/api/v1/orgs', 'POST') };
    }, BASE_URL);
    expect(results).toEqual({ read: false, write: false });
    const after = await browserApi(page, 'GET', '/api/v1/orgs', zOrgList);
    expect(after).toEqual(before);
    expect(violations(await observe(page, 0))).toEqual([]);
    await attacker.close();
  } finally { await canary.close(); }
});

test('oracle-negative-controls: isolated broken fixture triggers execution, egress and credential-storage assertions', async ({ browser }) => {
  const canary = await startCanary();
  const isolated = await browser.newContext();
  try {
    // This separate origin has deliberately missing CSP and unsafe rendering.
    // No product header, application response or authentication is mocked.
    const page = await isolated.newPage();
    await page.goto(canary.origin);
    canary.hits.length = 0;
    await page.evaluate((origin) => {
      document.body.innerHTML = `<img src="${origin}/deliberate-broken-sink" onload="document.documentElement.dataset.hikyoSecurity=1" onerror="document.documentElement.dataset.hikyoSecurity=1">`;
      localStorage.setItem('deliberate-broken-credential', 'correct horse battery staple e2e');
    }, canary.origin);
    await expect.poll(async () => (await observe(page, canary.hits.length)).executed).toBe(true);
    expect(violations(await observe(page, canary.hits.length))).toEqual(['script-execution', 'forbidden-egress', 'credential-storage']);
  } finally { await isolated.close(); await canary.close(); }
});

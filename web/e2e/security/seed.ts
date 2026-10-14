import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { z } from 'zod';
import type { Page } from '@playwright/test';
import { zLoginResult } from '@hikyo/zod';
import { fixtureApiCall as call, fixtureBearer } from '../fixtures/api.ts';
import { browserApi } from '../fixtures/api.ts';
import { ADMIN } from '../fixtures/instance.ts';
import { totpCode } from '../fixtures/seed.ts';
import { SECURITY_FIXTURE_CONFIGURATION } from './configuration.ts';

export const SECURITY_SEEDED = fileURLToPath(new URL('../.auth/security-seed.json', import.meta.url));
const zSecuritySeed = z.object({
  org: z.string(), orgName: z.string(), orgB: z.string(), orgBName: z.string(),
  project: z.string(), dev: z.string(), prod: z.string(),
  token: z.string(), otpauth: z.string(), lastTotpStep: z.number(),
});
type SecuritySeed = z.infer<typeof zSecuritySeed>;

export function readSecuritySeed(): SecuritySeed {
  return zSecuritySeed.parse(JSON.parse(readFileSync(SECURITY_SEEDED, 'utf8')));
}

/** Only the native-auth tenant and settings needed by the security campaign.
 * No unrelated identity, remote, history, pin or secret-disclosure fixtures.
 */
export async function seedSecurityTenant(
  page: Page,
  runAdminGrant: (args: readonly string[]) => void,
  authenticate: (draw: () => Promise<string>) => Promise<void>,
): Promise<SecuritySeed> {
  const signIn = () => fixtureBearer('security fixture native login');
  const token = await signIn();
  const who = await call(token, 'GET', '/api/v1/auth/whoami', z.object({ principal: z.object({ id: z.string() }) }));
  for (const capability of ['read', 'edit', 'publish', 'manage-projects', 'definitions-edit', 'project-settings']) {
    runAdminGrant(['--principal', who.principal.id, '--capability', capability]);
  }
  let otpauth = '';
  let lastStep = -1;
  async function draw(): Promise<string> {
    const step = () => Math.floor(Date.now() / 30_000);
    const want = Math.max(step(), lastStep + 1);
    // TOTP's single-use ledger and accepted clock skew are genuine product
    // limits. Wait for a valid step rather than increasing any admission limit.
    while (want - step() > 1) await new Promise((resolve) => setTimeout(resolve, 250));
    lastStep = want;
    if (otpauth === '') throw new Error('TOTP cannot be drawn before native enrollment starts');
    return totpCode(otpauth, new Date(want * 30_000 + 1_000));
  }
  await authenticate(draw);
  const started = await browserApi(page, 'POST', '/api/v1/auth/totp/enrol/start',
    z.object({ otpauth_uri: z.string() }), { password: ADMIN.password });
  otpauth = started.otpauth_uri;
  for (const path of ['/api/v1/auth/totp/enrol/confirm', '/api/v1/auth/totp/step-up']) {
    const issued = await browserApi(page, 'POST', path, zLoginResult, { code: await draw() });
    if (issued.session_token !== undefined) throw new Error('native browser session escaped its HTTP-only cookie into JSON');
  }
  const orgName = '<svg onload="document.documentElement.dataset.hikyoSecurity=1">';
  const orgBName = 'javascript:document.documentElement.dataset.hikyoSecurity=1';
  const created = z.object({ id: z.string() });
  const { id: org } = await browserApi(page, 'POST', '/api/v1/orgs', created, { name: orgName });
  await authenticate(draw);
  const { id: orgB } = await browserApi(page, 'POST', '/api/v1/orgs', created, { name: orgBName });
  await authenticate(draw);
  const { id: project } = await browserApi(page, 'POST', `/api/v1/orgs/${org}/projects`, created, { name: 'security-campaign' });
  const base = `/api/v1/orgs/${org}/projects/${project}/environments`;
  const { id: dev } = await browserApi(page, 'POST', base, created, { name: 'development' });
  const { id: prod } = await browserApi(page, 'POST', base, created, { name: 'production' });
  const settings = z.object({ protected: z.boolean(), reauth_window_seconds: z.number().nullish() });
  await browserApi(page, 'PUT', `${base}/${dev}/settings`, settings, {
    protected: false, reauth_window_seconds: SECURITY_FIXTURE_CONFIGURATION.development_disclosure_window_seconds,
  });
  await browserApi(page, 'PUT', `${base}/${prod}/settings`, settings, { protected: true });
  return { org, orgName, orgB, orgBName, project, dev, prod, token, otpauth, lastTotpStep: lastStep };
}

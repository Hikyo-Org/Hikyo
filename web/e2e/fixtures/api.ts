import { createHash, randomBytes } from 'node:crypto';
import { z } from 'zod';
import { request, type APIRequestContext, type Page } from '@playwright/test';
import { zCliLoginApproved, zCliLoginStart, zLoginChallenge, zLoginResult } from '@hikyo/zod';

import { ADMIN, BASE_URL } from './instance.ts';

export const zFixtureStaged = z.object({ version_id: z.string() });
export const zFixtureRevisionList = z.object({
  items: z.array(
    z.object({
      revision: z.number(),
      changed_keys: z.array(
        z.object({
          key_id: z.string(),
          name: z.string(),
          change: z.enum(['added', 'edited', 'removed']),
        }),
      ),
    }),
  ),
});

/** Preserve restricted CLI bootstrap before enrolment; enrolled callers supply
 * their existing spent-step ledger for browser MFA and an approved CLI handoff.
 */
export async function fixtureBearer(label: string, drawTotp?: () => Promise<string>): Promise<string> {
  const contexts: APIRequestContext[] = [];
  async function mint(): Promise<string> {
    if (drawTotp === undefined) {
      // Required-enrolment sessions can enrol a factor but cannot approve a
      // browser handoff yet. Keep the original bootstrap artifact and let the
      // server refuse this path once a factor stands; never fall back from MFA.
      const cli = await request.newContext({ baseURL: BASE_URL });
      contexts.push(cli);
      const response = await cli.post('/api/v1/auth/local/login', {
        data: { username: ADMIN.username, password: ADMIN.password, artifact: 'cli' },
        maxRedirects: 0,
      });
      if (response.status() !== 200) {
        throw new Error(`${label} could not sign in: ${response.status()}; enrolled accounts require a TOTP ledger and browser MFA`);
      }
      const result = zLoginResult.parse(await response.json());
      if (result.session.artifact !== 'cli' || result.session_token === undefined || result.session_token === '') {
        throw new Error(`${label} CLI bootstrap did not return a bearer`);
      }
      return result.session_token;
    }
    const browser = await request.newContext({ baseURL: BASE_URL });
    contexts.push(browser);
    const cli = await request.newContext({ baseURL: BASE_URL });
    contexts.push(cli);
    let signedIn = false;
    let csrf = '';
    async function logoutBrowser(): Promise<void> {
      if (!signedIn) return;
      signedIn = false;
      const logout = await browser.post('/api/v1/auth/logout', {
        headers: { 'X-Hikyo-CSRF': csrf }, maxRedirects: 0,
      });
      if (!logout.ok()) throw new Error(`${label} browser logout answered ${logout.status()}`);
    }
    try {
      let response = await browser.post('/api/v1/auth/local/login', {
        data: { username: ADMIN.username, password: ADMIN.password, artifact: 'browser' },
        maxRedirects: 0,
      });
      if (!response.ok()) throw new Error(`${label} could not sign in: ${response.status()}`);
      if (response.status() === 202) {
        const challenge = zLoginChallenge.parse(await response.json());
        if (!challenge.factors.includes('totp')) {
          throw new Error(`${label} requires its enrolled TOTP ledger`);
        }
        // Match the browser fixture's bounded skew/replay recovery. Each draw
        // advances the caller's ledger even when the server refuses that step.
        for (let attempt = 0; attempt < 4; attempt++) {
          response = await browser.post(`/api/v1/auth/login/challenge/${challenge.challenge_id}/totp`, {
            data: { code: await drawTotp() }, maxRedirects: 0,
          });
          if (response.ok()) break;
          if (response.status() !== 401 && response.status() !== 409) break;
        }
        if (!response.ok()) throw new Error(`${label} TOTP challenge answered ${response.status()}`);
      }
      const login = zLoginResult.parse(await response.json());
      if (login.session.artifact !== 'browser' || login.session_token !== undefined) {
        throw new Error(`${label} browser sign-in returned the wrong artifact`);
      }
      signedIn = true;
      const cookies = (await browser.storageState()).cookies;
      csrf = cookies.find((cookie) => cookie.name === '__Host-hikyo-csrf')?.value ?? '';
      if (csrf === '') throw new Error(`${label} browser sign-in did not set a CSRF cookie`);

      const state = randomBytes(32).toString('base64url');
      const verifier = randomBytes(32).toString('base64url');
      // API-driven setup consumes the approval response directly. No browser
      // navigation or callback listener is needed for this fixture handoff.
      const redirect = 'http://127.0.0.1:41234/callback';
      const start = await cli.post('/api/v1/auth/cli-login/start', {
        data: {
          transport: 'loopback', state,
          pkce_challenge: createHash('sha256').update(verifier).digest('base64url'),
          redirect_uri: redirect,
        }, maxRedirects: 0,
      });
      if (!start.ok()) throw new Error(`${label} CLI start answered ${start.status()}`);
      if (zCliLoginStart.parse(await start.json()).state !== state) {
        throw new Error(`${label} CLI start binding did not match`);
      }
      const approval = await browser.post('/api/v1/auth/cli-login/approve', {
        headers: { 'X-Hikyo-CSRF': csrf }, data: { reference: state }, maxRedirects: 0,
      });
      if (!approval.ok()) throw new Error(`${label} CLI approval answered ${approval.status()}`);
      const approved = zCliLoginApproved.parse(await approval.json());
      if (approved.state !== state || approved.redirect_uri !== redirect || approved.code === '') {
        throw new Error(`${label} CLI approval binding did not match`);
      }
      const redemption = await cli.post('/api/v1/auth/cli-login/redeem', {
        data: { code: approved.code, pkce_verifier: verifier, redirect_uri: redirect }, maxRedirects: 0,
      });
      if (!redemption.ok()) throw new Error(`${label} CLI redemption answered ${redemption.status()}`);
      const result = zLoginResult.parse(await redemption.json());
      if (result.session.artifact !== 'cli' || result.session_token === undefined || result.session_token === '') {
        throw new Error(`${label} CLI redemption did not return a bearer`);
      }
      await logoutBrowser();
      return result.session_token;
    } catch (error) {
      try {
        await logoutBrowser();
      } catch (cleanupError) {
        throw new AggregateError([error, cleanupError], `${label} failed and its browser logout also failed`);
      }
      throw error;
    }
  }
  // Capture the protocol result before cleanup so partial allocations are
  // covered and no disposal failure can replace the original failure.
  const [result] = await Promise.allSettled([mint()]);
  const disposed = await Promise.allSettled(contexts.map(async (context) => {
    await context.dispose();
  }));
  const cleanupErrors = disposed.flatMap((entry) => entry.status === 'rejected' ? [entry.reason] : []);
  if (result.status === 'rejected') {
    if (cleanupErrors.length > 0) {
      throw new AggregateError([result.reason, ...cleanupErrors], `${label} failed and context disposal also failed`);
    }
    throw result.reason;
  }
  if (cleanupErrors.length > 0) {
    throw new AggregateError(cleanupErrors, `${label} context disposal failed`);
  }
  return result.value;
}

/** Call the real API as a bearer and parse every successful response at the boundary. */
export async function fixtureApiCall<T>(
  token: string,
  method: string,
  path: string,
  schema: z.ZodType<T>,
  body?: Record<string, unknown>,
): Promise<T> {
  const response = await fetch(`${BASE_URL}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (!response.ok) {
    throw new Error(`${method} ${path} answered ${response.status}: ${await response.text()}`);
  }
  const raw: unknown = response.status === 204 ? {} : await response.json();
  const parsed = schema.safeParse(raw);
  if (!parsed.success) {
    throw new Error(
      `${method} ${path} answered a shape the fixture does not expect: ${parsed.error.message}`,
    );
  }
  return parsed.data;
}

export class BrowserApiError extends Error {
  constructor(
    method: string,
    path: string,
    readonly status: number,
    readonly body: string,
  ) {
    super(`${method} ${path} answered ${String(status)}: ${body}`);
    this.name = 'BrowserApiError';
  }
}

/** Drive a typed API call through the page's authenticated browser session. */
export async function browserApi<T>(
  page: Page,
  method: 'GET' | 'POST' | 'PUT' | 'DELETE',
  path: string,
  schema: z.ZodType<T>,
  body?: unknown,
): Promise<T> {
  // Scope the synchronizer-token lookup to this instance. The shared browser
  // jar also carries the second e2e instance's cookies (#71).
  const cookies = await page.context().cookies(BASE_URL);
  const csrf = cookies.find((cookie) => cookie.name === '__Host-hikyo-csrf')?.value;
  if (csrf === undefined || csrf === '') {
    throw new Error(`${method} ${path} cannot run: the page has no CSRF cookie for ${BASE_URL}`);
  }
  const response = await page.request.fetch(`${BASE_URL}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Hikyo-CSRF': csrf },
    ...(body === undefined ? {} : { data: body }),
  });
  if (!response.ok()) {
    throw new BrowserApiError(method, path, response.status(), await response.text());
  }
  const value: unknown = response.status() === 204 ? null : await response.json();
  return schema.parse(value);
}

import { z } from 'zod';
import type { Page } from '@playwright/test';

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

/** Mint a password bearer used only by API-driven fixture setup and repair. */
export async function fixtureBearer(label: string): Promise<string> {
  const response = await fetch(`${BASE_URL}/api/v1/auth/local/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: ADMIN.username, password: ADMIN.password }),
  });
  if (!response.ok) {
    throw new Error(`${label} could not sign in: ${response.status}`);
  }
  return z.object({ session_token: z.string() }).parse(await response.json()).session_token;
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

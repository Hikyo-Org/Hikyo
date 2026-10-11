// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest';
import { z } from 'zod';

import { installAppFetch } from './withApp.tsx';

const cleanups: (() => void)[] = [];
afterEach(() => {
  for (const cleanup of cleanups.splice(0).reverse()) cleanup();
});

async function install(app: Parameters<typeof installAppFetch>[0]['parameters']['app']) {
  const cleanup = await installAppFetch({ parameters: { app }, viewMode: 'story' });
  cleanups.push(cleanup);
  return cleanup;
}

describe('real-screen Storybook transport', () => {
  it('matches exact paths, verbs, and declared query values without hiding unexpected writes', async () => {
    await install({ responses: [
      { url: '/items', query: { page: '2' }, body: { page: 2 } },
      { url: '/items', method: 'POST', status: 201, body: { created: true } },
    ] });
    expect(await (await fetch('/items?page=2&filter=open')).json()).toEqual({ page: 2 });
    expect((await fetch('/items?page=1')).status).toBe(404);
    expect((await fetch('/items?page=2&page=1')).status).toBe(404);
    expect((await fetch('/items/other?page=2')).status).toBe(404);
    expect((await fetch('/items?page=2', { method: 'DELETE' })).status).toBe(404);
    expect((await fetch(new Request('http://localhost/items', { method: 'DELETE' }), { method: 'POST' })).status).toBe(201);
  });

  it('supports repeatable regexp matching and intentional null responses', async () => {
    await install({ responses: [{ url: /\/items\?page=2/g, status: 204 }] });
    for (let i = 0; i < 2; i++) {
      const response = await fetch('/items?page=2');
      expect(response.status).toBe(204);
      expect(await response.text()).toBe('');
    }
  });

  it('runs request-aware fixtures with a contract-valid body and signal', async () => {
    await install({ responses: [{ url: '/items', method: 'POST', handler: async (request) => {
      const body = z.object({ name: z.string().min(1) }).parse(await request.json());
      expect(request.method).toBe('POST');
      expect(request.signal.aborted).toBe(false);
      return Response.json({ name: body.name }, { status: 201 });
    } }] });
    const response = await fetch('/items', { method: 'POST', body: JSON.stringify({ name: 'billing' }) });
    expect(await response.json()).toEqual({ name: 'billing' });
    await expect(fetch('/items', { method: 'POST', body: '{}' })).rejects.toThrow();
  });

  it('keeps loading pending until the request aborts, including already-aborted requests', async () => {
    await install({ responses: [{ url: '/items', pending: true }] });
    const controller = new AbortController();
    const response = fetch('/items', { signal: controller.signal });
    const rejected = expect(response).rejects.toMatchObject({ name: 'AbortError' });
    controller.abort();
    await rejected;
    await expect(fetch('/items', { signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' });
    expect((await fetch(new Request('http://localhost/unexpected', { signal: controller.signal }), { signal: null })).status).toBe(404);
  });

  it('rejects a hanging request-aware handler when its story retires', async () => {
    const cleanup = await install({ responses: [{ url: '/items', handler: () => new Promise<Response>(() => {}) }] });
    const pending = fetch('/items');
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    await Promise.resolve();
    cleanup();
    await rejected;
  });

  it('retires pending requests and prevents late cleanup from replacing the next story fixtures', async () => {
    const original = globalThis.fetch;
    const first = await install({ responses: [{ url: '/items', pending: true }] });
    const pending = fetch('/items');
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    const second = await install({ responses: [{ url: '/items', body: { story: 2 } }] });
    await rejected;
    first();
    expect(await (await fetch('/items')).json()).toEqual({ story: 2 });
    second();
    expect(globalThis.fetch).toBe(original);
  });

  it('mounts auth fixtures with explicit refusals taking precedence over the default identity', async () => {
    await install({ auth: true, responses: [{ url: '/api/v1/auth/whoami', status: 401 }] });
    expect((await fetch('/api/v1/auth/whoami')).status).toBe(401);
  });

  it('refuses inline Docs before installing fixtures', async () => {
    const original = globalThis.fetch;
    await expect(installAppFetch({ parameters: { app: {} }, viewMode: 'docs' })).rejects.toThrow('topLayerDocs');
    expect(globalThis.fetch).toBe(original);
  });
});

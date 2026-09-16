import { createServer } from 'node:http';
import type { RequestOptions } from 'node:http';
import type { AddressInfo } from 'node:net';

import type { DiscoveryInfo } from '@open-pencil/mcp/discovery';
import { describe, expect, it, vi } from 'vitest';

import { createRpc, openInOpenPencil, openPencilMiddleware } from './openpencil-middleware.ts';

const info: DiscoveryInfo = {
  pid: 1,
  socketPath: null,
  httpPort: 7600,
  authRequired: true,
  authToken: 'secret',
  version: '0.14.0',
  startedAt: '',
};

/** `noUncheckedIndexedAccess` is on; narrow instead of asserting. */
function at<T>(items: T[], index: number): T {
  const item = items[index];
  if (item === undefined) throw new Error(`no element at index ${index}`);
  return item;
}

describe('openInOpenPencil', () => {
  it('opens the file, selects the node, zooms', async () => {
    const rpc = vi.fn(async (_i: DiscoveryInfo, command: string, args: Record<string, unknown>) => {
      if (command === 'tool' && args.name === 'find_nodes') return { nodes: [{ id: 'T3Um0', name: 'Button/Primary' }] };
      return {};
    });
    const r = await openInOpenPencil('Button/Primary', { discovery: async () => info, rpc, penPath: '/repo/web/design/hikyo.pen' });
    expect(r.status).toBe(200);
    expect(rpc.mock.calls.map((c) => c[1])).toEqual(['open_file', 'tool', 'tool', 'tool']);
    expect(at(rpc.mock.calls, 0)[2]).toEqual({ path: '/repo/web/design/hikyo.pen' });
    expect(at(rpc.mock.calls, 1)[2]).toEqual({ name: 'find_nodes', args: { name: 'Button/Primary' } });
    expect(at(rpc.mock.calls, 2)[2]).toEqual({ name: 'select_nodes', args: { ids: ['T3Um0'] } });
    expect(at(rpc.mock.calls, 3)[2]).toEqual({ name: 'viewport_zoom_to_fit', args: { ids: ['T3Um0'] } });
  });
  it('503 when the app is not running', async () => {
    const rpc = vi.fn(async () => ({}));
    const r = await openInOpenPencil('Button/Primary', { discovery: async () => null, rpc, penPath: '/x.pen' });
    expect(r.status).toBe(503);
    expect(r.message).toMatch(/OpenPencil is not running/);
    expect(rpc).not.toHaveBeenCalled();
  });
  it('404 when the node is missing, without leaking the token', async () => {
    const rpc = vi.fn(async () => ({ nodes: [] }));
    const r = await openInOpenPencil('Button/Nope', { discovery: async () => info, rpc, penPath: '/x.pen' });
    expect(r.status).toBe(404);
    expect(JSON.stringify(r)).not.toContain('secret');
  });
});

/**
 * Drives the handler through a real `http.Server`, so the `IncomingMessage` and
 * `ServerResponse` it sees are the real ones (no hand-built stand-ins, and no
 * casts to pretend a literal is one). Only the outbound transport is faked.
 */
async function withMiddleware(
  send: (options: RequestOptions, payload: string) => Promise<{ status: number; body: string }>,
  run: (url: string) => Promise<void>,
) {
  const handler = openPencilMiddleware({
    penPath: '/repo/web/design/hikyo.pen',
    allowedOrigin: 'http://localhost:6006',
    discovery: async () => info,
    rpc: createRpc(send),
  });
  const server = createServer((req, res) => void handler(req, res));
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('server did not bind a port');
  const bound: AddressInfo = address;
  try {
    await run(`http://127.0.0.1:${bound.port}/`);
  } finally {
    await new Promise<void>((resolve, reject) => server.close((e) => (e ? reject(e) : resolve())));
  }
}

/** `RequestOptions['headers']` widens to an array form that never occurs here; narrow, do not cast. */
function authHeader(options: RequestOptions): unknown {
  const headers = options.headers;
  if (headers === undefined || !('Authorization' in headers)) return undefined;
  return headers.Authorization;
}

const okResponse = { status: 200, body: JSON.stringify({ ok: true, result: { nodes: [{ id: 'T3Um0', name: 'Button/Primary' }] } }) };

describe('createRpc', () => {
  it('refuses to call a token-required server without a token', async () => {
    const send = vi.fn(async () => okResponse);
    const rpc = createRpc(send);
    await expect(rpc({ ...info, authToken: null }, 'open_file', {})).rejects.toThrow(/discovery file has no auth token/);
    expect(send).not.toHaveBeenCalled();
  });
});

describe('openPencilMiddleware', () => {
  it('sends the bearer token upstream and never back to the browser', async () => {
    const seen: RequestOptions[] = [];
    await withMiddleware(
      async (options) => {
        seen.push(options);
        return okResponse;
      },
      async (url) => {
        const res = await fetch(url, {
          method: 'POST',
          headers: { Origin: 'http://localhost:6006', 'Content-Type': 'application/json' },
          body: JSON.stringify({ node: 'Button/Primary' }),
        });
        const text = await res.text();
        expect(res.status).toBe(200);
        expect(text).toContain('Opened Button/Primary');
        expect(text).not.toContain('secret');
        expect(res.headers.get('authorization')).toBeNull();
      },
    );
    expect(seen).toHaveLength(4);
    expect(authHeader(at(seen, 0))).toBe('Bearer secret');
    expect(at(seen, 0).path).toBe('/rpc');
    expect(at(seen, 0).port).toBe(7600);
  });

  it('refuses a foreign origin without touching the transport', async () => {
    const seen: RequestOptions[] = [];
    await withMiddleware(
      async (options) => {
        seen.push(options);
        return okResponse;
      },
      async (url) => {
        const res = await fetch(url, {
          method: 'POST',
          headers: { Origin: 'http://evil', 'Content-Type': 'application/json' },
          body: JSON.stringify({ node: 'Button/Primary' }),
        });
        expect(res.status).toBe(403);
        expect(await res.text()).toContain('Origin not allowed');
      },
    );
    expect(seen).toHaveLength(0);
  });

  it('refuses a GET', async () => {
    await withMiddleware(
      async () => okResponse,
      async (url) => {
        const res = await fetch(url, { headers: { Origin: 'http://localhost:6006' } });
        expect(res.status).toBe(405);
        expect(await res.text()).toContain('POST only');
      },
    );
  });

  it('refuses a body over the cap', async () => {
    const seen: RequestOptions[] = [];
    await withMiddleware(
      async (options) => {
        seen.push(options);
        return okResponse;
      },
      async (url) => {
        const res = await fetch(url, {
          method: 'POST',
          headers: { Origin: 'http://localhost:6006', 'Content-Type': 'application/json' },
          body: JSON.stringify({ node: `Button/${'A'.repeat(8192)}` }),
        });
        expect(res.status).toBe(413);
      },
    );
    expect(seen).toHaveLength(0);
  });

  it('503s when the discovery file is stale and the app refuses the connection', async () => {
    await withMiddleware(
      async () => {
        throw Object.assign(new Error('connect ECONNREFUSED 127.0.0.1:7600'), { code: 'ECONNREFUSED' });
      },
      async (url) => {
        const res = await fetch(url, {
          method: 'POST',
          headers: { Origin: 'http://localhost:6006', 'Content-Type': 'application/json' },
          body: JSON.stringify({ node: 'Button/Primary' }),
        });
        expect(res.status).toBe(503);
        expect(await res.text()).toContain('OpenPencil is not running');
      },
    );
  });

  it('rejects a body that is not a node name', async () => {
    await withMiddleware(
      async () => okResponse,
      async (url) => {
        const res = await fetch(url, {
          method: 'POST',
          headers: { Origin: 'http://localhost:6006', 'Content-Type': 'application/json' },
          body: '{"node":"../../etc/passwd"}',
        });
        expect(res.status).toBe(400);
      },
    );
  });
});

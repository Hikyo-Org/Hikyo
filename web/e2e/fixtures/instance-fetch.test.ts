import { createServer } from 'node:http';
import { lookup, type LookupAddress, type LookupOptions } from 'node:dns';

import { describe, expect, it, vi } from 'vitest';
import { Response as UndiciResponse } from 'undici';

import { fetchPeerDirectory, instanceFetch, SERVING_HOST } from './instance-fetch.ts';

vi.mock('node:dns', async (importOriginal) => {
  const dns = await importOriginal<typeof import('node:dns')>();
  return {
    ...dns,
    lookup: vi.fn((hostname: string, options: LookupOptions, callback: (error: NodeJS.ErrnoException | null, address: string | LookupAddress[], family: number) => void) => {
      if (hostname === 'serving.localhost') {
        callback(Object.assign(new Error('fixture OS resolver has no .localhost record'), { code: 'ENOTFOUND' }), '', 0);
        return;
      }
      dns.lookup(hostname, options, callback);
    }),
  };
});

describe('fixture loopback transport', () => {
  it('uses installed Undici for ordinary hosts and leaves peer requests without a cookie jar', async () => {
    const server = createServer((request, response) => {
      response.setHeader('Set-Cookie', 'fixture=received; Path=/');
      response.end(`${request.headers.host}\n${request.headers.origin}\n${request.headers.cookie}\n${request.headers.authorization}`);
    });
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    try {
      const address = server.address();
      if (address === null || typeof address === 'string') throw new Error('missing TCP listener');
      const host = `127.0.0.1:${String(address.port)}`;
      const base = `http://${host}`;
      const response = await instanceFetch(base, {
        headers: { Origin: base, Cookie: 'fixture=explicit' },
      });
      expect(response).toBeInstanceOf(UndiciResponse);
      expect(response.headers.getSetCookie()).toEqual(['fixture=received; Path=/']);
      expect(await response.text()).toBe(`${host}\n${base}\nfixture=explicit\nundefined`);

      const peerResponse = await fetchPeerDirectory(base, 'fixture-credential');
      expect(peerResponse).toBeInstanceOf(UndiciResponse);
      expect(await peerResponse.text()).toBe(`${host}\nundefined\nundefined\nBearer fixture-credential`);
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    }
  });
  it('keeps the peer anonymous and bearer-only when the OS cannot resolve its hostname', async () => {
    await expect(new Promise<void>((resolve, reject) => {
      lookup(SERVING_HOST, {}, (error) => error === null ? resolve() : reject(error));
    })).rejects.toMatchObject({ code: 'ENOTFOUND' });
    vi.mocked(lookup).mockClear();

    const seen: { host: string | undefined; cookie: string | undefined; authorization: string | undefined }[] = [];
    let active = true;
    const server = createServer((request, response) => {
      seen.push({ host: request.headers.host, cookie: request.headers.cookie, authorization: request.headers.authorization });
      response.statusCode = active && request.headers.authorization === 'Bearer fixture-credential' ? 200 : 401;
      response.end();
    });
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    try {
      const address = server.address();
      if (address === null || typeof address === 'string') throw new Error('missing TCP listener');
      const host = `${SERVING_HOST}:${String(address.port)}`;
      const directory = `http://${host}/api/v1/instance/directory`;
      expect((await fetchPeerDirectory(directory)).status).toBe(401);
      expect((await fetchPeerDirectory(directory, 'fixture-credential')).status).toBe(200);
      active = false;
      expect((await fetchPeerDirectory(directory, 'fixture-credential')).status).toBe(401);
      expect(seen).toEqual([
        { host, cookie: undefined, authorization: undefined },
        { host, cookie: undefined, authorization: 'Bearer fixture-credential' },
        { host, cookie: undefined, authorization: 'Bearer fixture-credential' },
      ]);
      const lookups = vi.mocked(lookup).mock.calls.map(([hostname]) => hostname);
      expect(lookups).toContain('127.0.0.1');
      expect(lookups).not.toContain(SERVING_HOST);
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
      vi.mocked(lookup).mockClear();
    }
  });
  it('preserves the serving origin and headers while connecting to loopback', async () => {
    const server = createServer((request, response) => {
      response.end(`${request.headers.host}\n${request.headers.origin}\n${request.headers.cookie}`);
    });
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    try {
      const address = server.address();
      if (address === null || typeof address === 'string') throw new Error('missing TCP listener');
      const base = `http://${SERVING_HOST}:${String(address.port)}`;
      const response = await instanceFetch(base, {
        headers: { Origin: base, Cookie: 'fixture=serving' },
      });
      expect(await response.text()).toBe(`${SERVING_HOST}:${String(address.port)}\n${base}\nfixture=serving`);
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    }
  });
});

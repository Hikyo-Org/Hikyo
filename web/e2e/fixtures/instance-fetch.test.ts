import { createServer } from 'node:http';

import { describe, expect, it } from 'vitest';

import { instanceFetch, SERVING_HOST } from './instance-fetch.ts';

describe('fixture loopback transport', () => {
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

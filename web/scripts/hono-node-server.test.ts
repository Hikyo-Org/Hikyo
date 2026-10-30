import { createServer, type RequestListener } from 'node:http';
import { createRequire } from 'node:module';

import { expect, it } from 'vitest';

// Resolve from the consumer so this exercises OpenPencil's actual adapter,
// including the scoped compatibility override in pnpm-workspace.yaml.
const require = createRequire(import.meta.url);
const mcpRequire = createRequire(require.resolve('@open-pencil/mcp'));
type FetchHandler = (request: Request) => Response | Promise<Response>;
const getRequestListener: (handler: FetchHandler) => RequestListener =
  mcpRequire('@hono/node-server').getRequestListener;

async function withServer(handler: FetchHandler, check: (origin: string) => Promise<void>): Promise<void> {
  const server = createServer(getRequestListener(handler));
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  try {
    const address = server.address();
    if (address === null || typeof address === 'string') throw new Error('Fixture did not bind a TCP port');
    await check(`http://127.0.0.1:${address.port}`);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => {
      server.close((error) => error === undefined ? resolve() : reject(error));
    });
  }
}

it('preserves the OpenPencil request listener contract for MCP POST bodies and headers', async () => {
  await withServer(async (request) => {
    expect(new URL(request.url).pathname).toBe('/mcp');
    expect(request.method).toBe('POST');
    expect(request.headers.get('authorization')).toBe('Bearer synthetic-fixture');
    return new Response(await request.text(), { status: 202, headers: { 'mcp-session-id': 'synthetic-session' } });
  }, async (origin) => {
    const response = await fetch(`${origin}/mcp`, {
      method: 'POST',
      headers: { authorization: 'Bearer synthetic-fixture', 'content-type': 'application/json' },
      body: '{"jsonrpc":"2.0","id":1,"method":"ping"}',
    });
    expect(response.status).toBe(202);
    expect(response.headers.get('mcp-session-id')).toBe('synthetic-session');
    expect(await response.text()).toBe('{"jsonrpc":"2.0","id":1,"method":"ping"}');
  });
});

it('preserves streamed MCP response bytes through the pinned adapter', async () => {
  let producerClosed = false;
  let finishProducer = () => {};
  let releaseSecond: () => void = () => { throw new Error('Response stream has not started'); };
  await withServer(() => new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode('event: message\ndata: first\n\n'));
      finishProducer = () => {
        if (producerClosed) return;
        producerClosed = true;
        controller.close();
      };
      releaseSecond = () => {
        if (producerClosed) throw new Error('Response stream closed before the second event');
        controller.enqueue(new TextEncoder().encode('event: message\ndata: second\n\n'));
        finishProducer();
      };
    },
    cancel() { producerClosed = true; },
  }), { headers: { 'content-type': 'text/event-stream' } }), async (origin) => {
    const response = await fetch(`${origin}/mcp`, { signal: AbortSignal.timeout(3_000) });
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toBe('text/event-stream');
    const reader = response.body?.getReader();
    if (reader === undefined) throw new Error('Response stream is missing');
    try {
      const decoder = new TextDecoder();
      let first = '';
      while (!first.endsWith('\n\n')) {
        const chunk = await reader.read();
        if (chunk.done) throw new Error('Response stream ended before the first event');
        first += decoder.decode(chunk.value, { stream: true });
      }
      expect(first).toBe('event: message\ndata: first\n\n');
      expect(producerClosed).toBe(false);
      releaseSecond();
      let second = '';
      while (true) {
        const chunk = await reader.read();
        if (chunk.done) break;
        second += decoder.decode(chunk.value, { stream: true });
      }
      expect(second).toBe('event: message\ndata: second\n\n');
    } finally {
      finishProducer();
      await reader.cancel();
    }
  });
});

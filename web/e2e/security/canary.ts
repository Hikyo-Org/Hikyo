import { createServer, type Server } from 'node:http';

/** A real controlled origin; the app's requests are never intercepted. */
export async function startCanary(): Promise<{
  origin: string; hits: string[]; server: Server; close: () => Promise<void>;
}> {
  const hits: string[] = [];
  const server = createServer((request, response) => {
    hits.push(request.url ?? '/');
    response.setHeader('Content-Type', 'text/html; charset=utf-8');
    response.end('<!doctype html><title>Controlled hostile origin</title><body>Controlled hostile origin</body>');
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('canary did not bind a TCP port');
  return {
    origin: `http://127.0.0.1:${String(address.port)}`, hits, server,
    close: () => new Promise<void>((resolve, reject) => {
      server.close((error) => error === undefined ? resolve() : reject(error));
      // Stop accepting first, then release unfinished requests as well as idle sockets.
      server.closeAllConnections();
    }),
  };
}

// Dev-only bridge from the Storybook toolbar to the running OpenPencil app.
// The RPC bearer token is read from the discovery file here, in Node, and
// never sent to the browser: dev servers bind 0.0.0.0, so anything in the
// bundle is readable on the LAN. Interim until the openpencil:// scheme
// ships upstream (design-tooling ADR, point 5).
import { request as httpRequest } from 'node:http';
import type { IncomingMessage, RequestOptions, ServerResponse } from 'node:http';

import { readDiscoveryFile, type DiscoveryInfo } from '@open-pencil/mcp/discovery';
import type { Plugin } from 'vite';
import { z } from 'zod';

type Rpc = (info: DiscoveryInfo, command: string, args: Record<string, unknown>) => Promise<unknown>;

/** One round trip to the OpenPencil server. Injectable so tests can inspect the outgoing headers. */
type Send = (options: RequestOptions, payload: string) => Promise<{ status: number; body: string }>;

const RPC_TIMEOUT_MS = 10_000;
/** A node name is a few dozen bytes; anything larger is not a request this route serves. */
const MAX_BODY_BYTES = 4096;

const foundNodes = z.object({ nodes: z.array(z.object({ id: z.string(), name: z.string() })) });
const body = z.object({ node: z.string().regex(/^[A-Za-z0-9][A-Za-z0-9 ]*(\/[A-Za-z0-9][A-Za-z0-9 ]*)+$/) });

// `POST /rpc` answers with the browser's reply: `{ ok, result }`, or
// `{ ok: false, error }` on failure (@open-pencil/mcp's Hono app, and the CLI's
// doRPC, which returns `body.result`). The payload the callers below want is
// one level in, so the envelope is peeled here and the caller parses what was
// inside it. `result` is object-or-absent upstream too: the MCP tool layer does
// `const r = res.result; if (r && 'base64' in r)`, which only holds for objects.
const envelope = z.looseObject({
  ok: z.boolean().optional(),
  error: z.string().optional(),
  result: z.looseObject({}).nullish(),
});

export async function openInOpenPencil(
  node: string,
  deps: { discovery: () => Promise<DiscoveryInfo | null>; rpc: Rpc; penPath: string },
): Promise<{ status: 200 | 404 | 503; message: string }> {
  const info = await deps.discovery();
  if (!info) return { status: 503, message: 'OpenPencil is not running. Start the app, then click again.' };
  await deps.rpc(info, 'open_file', { path: deps.penPath });
  // find_nodes matches names case-insensitively as a substring, so the exact
  // node still has to be picked out of the matches.
  const found = foundNodes.parse(await deps.rpc(info, 'tool', { name: 'find_nodes', args: { name: node } }));
  const match = found.nodes.find((n) => n.name === node);
  if (!match) return { status: 404, message: `Node "${node}" not found in hikyo.pen.` };
  await deps.rpc(info, 'tool', { name: 'select_nodes', args: { ids: [match.id] } });
  await deps.rpc(info, 'tool', { name: 'viewport_zoom_to_fit', args: { ids: [match.id] } });
  return { status: 200, message: `Opened ${node}` };
}

const sendOverHttp: Send = (options, payload) =>
  new Promise((resolve, reject) => {
    const req = httpRequest(options, (res) => {
      let data = '';
      res.on('data', (c: Buffer) => {
        data += c;
      });
      res.on('end', () => resolve({ status: res.statusCode ?? 0, body: data }));
    });
    // Without this a wedged desktop app wedges the browser request behind it.
    req.setTimeout(RPC_TIMEOUT_MS, () => req.destroy(new Error('OpenPencil RPC timed out')));
    req.on('error', reject);
    req.end(payload);
  });

export function createRpc(send: Send = sendOverHttp): Rpc {
  return async (info, command, args) => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (info.authToken) headers.Authorization = `Bearer ${info.authToken}`;
    const target = info.socketPath ? { socketPath: info.socketPath } : { host: '127.0.0.1', port: info.httpPort };
    const { status, body: text } = await send({ ...target, path: '/rpc', method: 'POST', headers }, JSON.stringify({ command, args }));
    // The failure paths answer with the envelope too (`{ ok: false, error }`, or
    // `{ error }` on 401/400), so the body is read before the status is judged:
    // OpenPencil's own message is the useful one.
    const parsed = envelope.safeParse(tryParseJson(text));
    const detail = parsed.success ? parsed.data.error : undefined;
    if (status !== 200 || (parsed.success && parsed.data.ok === false)) {
      throw new Error(detail ?? `OpenPencil RPC ${command}: HTTP ${status}`);
    }
    if (!parsed.success) throw new Error(`OpenPencil RPC ${command}: unreadable response`);
    return parsed.data.result ?? {};
  };
}

function tryParseJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

/** Resolves to null when the body exceeds the cap; the request is cut off at that point. */
function readBody(req: IncomingMessage): Promise<string | null> {
  return new Promise((resolve, reject) => {
    let data = '';
    let oversized = false;
    req.on('data', (c: Buffer) => {
      if (oversized) return;
      data += c;
      if (Buffer.byteLength(data) > MAX_BODY_BYTES) {
        oversized = true;
        data = '';
        // Drain the rest rather than buffering it, and rather than destroying
        // the socket: the 413 still has to reach the client over it.
        req.resume();
        resolve(null);
      }
    });
    req.on('end', () => resolve(data));
    req.on('error', reject);
  });
}

export function openPencilMiddleware(opts: {
  penPath: string;
  allowedOrigin: string;
  discovery?: () => Promise<DiscoveryInfo | null>;
  rpc?: Rpc;
}): (req: IncomingMessage, res: ServerResponse) => Promise<void> {
  const discovery = opts.discovery ?? readDiscoveryFile;
  const rpc = opts.rpc ?? createRpc();
  return async (req, res) => {
    const send = (status: number, message: string) => {
      res.statusCode = status;
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ message }));
    };
    if (req.method !== 'POST') return send(405, 'POST only');
    if (req.headers.origin !== opts.allowedOrigin) return send(403, 'Origin not allowed');
    try {
      // Inside the try: reading the body must not hang the request on an
      // unhandled rejection, and a body that is not JSON is a 400, not a 502.
      const raw = await readBody(req);
      if (raw === null) return send(413, 'Body too large');
      const parsed = body.safeParse(tryParseJson(raw));
      if (!parsed.success) return send(400, 'Body must be { node: "Title/Variant" }');
      const result = await openInOpenPencil(parsed.data.node, { discovery, rpc, penPath: opts.penPath });
      send(result.status, result.message);
    } catch (error) {
      send(502, error instanceof Error ? error.message : 'OpenPencil RPC failed');
    }
  };
}

// `port` is Storybook's own port, not Vite's: the Vite builder runs in
// `middlewareMode`, so `server.httpServer` is null (it never emits 'listening')
// and `server.config.server.port` is Vite's unused 5173 default. Storybook
// passes the port it actually bound to `viteFinal`, which is where main.ts
// takes it from.
export function openPencilPlugin(penPath: string, port: number): Plugin {
  return {
    name: 'hikyo-openpencil-open',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use('/__openpencil/open', openPencilMiddleware({ penPath, allowedOrigin: `http://localhost:${port}` }));
    },
  };
}

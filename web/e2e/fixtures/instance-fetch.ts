import { lookup } from 'node:dns';

import { Agent, fetch } from 'undici';

export const SERVING_HOST = 'serving.localhost';

// Chromium resolves .localhost internally. Node's OS resolver does not do so
// in the pinned Linux image, so fixture requests use an explicit loopback
// lookup while retaining the actual origin, Host, cookies and request headers.
const servingAgent = new Agent({
  connect: {
    lookup(hostname, options, callback) {
      lookup(hostname === SERVING_HOST ? '127.0.0.1' : hostname, options, callback);
    },
  },
});

// Keep fetch and its custom Agent on the same Undici handler ABI. Node's
// bundled fetch can lag the installed dispatcher protocol.
export function instanceFetch(url: string, init?: Parameters<typeof fetch>[1]): ReturnType<typeof fetch> {
  if (new URL(url).hostname !== SERVING_HOST) return fetch(url, init);
  const options = { ...init, dispatcher: servingAgent };
  return fetch(url, options);
}

// Unlike Playwright's request context, native fetch has no saved browser cookie
// jar. A peer authenticates solely with the connection credential it supplies.
export function fetchPeerDirectory(url: string, credential?: string): ReturnType<typeof fetch> {
  return instanceFetch(url, credential === undefined ? undefined : {
    headers: { Authorization: `Bearer ${credential}` },
  });
}

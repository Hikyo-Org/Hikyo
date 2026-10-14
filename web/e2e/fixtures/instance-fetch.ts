import { lookup } from 'node:dns';

import { Agent } from 'undici';

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

export function instanceFetch(url: string, init?: RequestInit): Promise<Response> {
  if (new URL(url).hostname !== SERVING_HOST) return globalThis.fetch(url, init);
  const options = { ...init, dispatcher: servingAgent };
  return globalThis.fetch(url, options);
}

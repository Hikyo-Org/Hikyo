import { getMetaOp, logoutOp } from '@hikyo/operations';
import { createClient, createConfig } from '@hikyo/runtime-core';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  ApiError,
  ok,
  parsed,
  parsedPick,
  readCsrfToken,
  transportRefusalText,
} from './client.ts';

// `parsed`/`ok` run a REAL generated descriptor against a mocked `fetch`, so the
// whole chain under test is exercised - the sdk call, the response branch, and
// the generated Zod parser bound to the operation - not a hand-built stand-in.
// `getMetaOp` is a body operation (200 -> Meta); `logoutOp` is bodyless (204).
//
// The call is routed through an absolute-origin client passed as `options.client`
// - the transport seam the workspace tier already uses (#71) - because the
// same-origin singleton's empty baseUrl makes Node's `new Request` reject the
// relative URL. `fetch` itself is stubbed, so the origin is never really hit.
const transport = { client: createClient(createConfig({ baseUrl: 'http://hikyo.test' })) };
const META = { server_version: '1.4.0', api_revision: 7, protocol_capabilities: [] };

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function stubFetch(response: Response): void {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(response);
}

afterEach(() => {
  vi.restoreAllMocks();
  // restoreAllMocks does not undo stubGlobal; the navigator stubs below leak
  // across tests without this.
  vi.unstubAllGlobals();
});

// The synchronizer token is read out of a cookie string by hand, which is one
// of the few pieces of parsing the SPA does itself. A cookie header is a
// hostile little format, leading spaces, other cookies with overlapping
// prefixes, values containing '=', and getting it wrong means every mutation
// is refused with no obvious cause.
describe('readCsrfToken', () => {
  it('finds the token among other cookies', () => {
    expect(readCsrfToken('a=1; __Host-hikyo-csrf=hik_1_cs_abc; b=2')).toBe('hik_1_cs_abc');
  });

  it('is not fooled by a cookie whose name merely starts the same', () => {
    // `__Host-hikyo` is the session cookie and is HttpOnly, so it should never
    // appear here, but a prefix match would also pick up anything named
    // `__Host-hikyo-csrf-something`.
    expect(readCsrfToken('__Host-hikyo-csrf-other=nope; __Host-hikyo-csrf=yes')).toBe('yes');
  });

  it('keeps a value containing an equals sign intact', () => {
    expect(readCsrfToken('__Host-hikyo-csrf=aa=bb')).toBe('aa=bb');
  });

  it('answers empty when there is no token, rather than undefined', () => {
    // The caller sends no header on empty; `undefined` would be stringified
    // into the header as the word "undefined".
    expect(readCsrfToken('')).toBe('');
    expect(readCsrfToken('other=1')).toBe('');
  });
});

describe('parsed', () => {
  it('parses a 2xx body through the operation-bound schema', async () => {
    stubFetch(jsonResponse(META, 200));
    await expect(parsed(getMetaOp, transport)).resolves.toMatchObject({
      server_version: '1.4.0',
      api_revision: 7,
    });
  });

  it('refuses a body-bearing success status other than the operation-bound one', async () => {
    stubFetch(jsonResponse(META, 201));
    await expect(parsed(getMetaOp, transport)).rejects.toThrow(
      'expected a body-bearing 200, got 201',
    );
  });

  it('fails loudly when the SDK completes without an HTTP response', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
    await expect(parsed(getMetaOp, transport)).rejects.toThrow(
      'SDK call completed without an HTTP response',
    );
  });

  it('preserves only contract-validated safe refusal detail', async () => {
    stubFetch(
      jsonResponse(
        {
          error: {
            code: 'bad_request',
            message: 'The request was invalid.',
            detail: 'key "LOG_LEVEL" is invalid in environment env_prod',
          },
        },
        400,
      ),
    );

    await expect(parsed(getMetaOp, transport)).rejects.toMatchObject({
      name: 'ApiError',
      status: 400,
      detail: 'key "LOG_LEVEL" is invalid in environment env_prod',
    } satisfies Partial<ApiError>);
  });

  it('preserves a bounded numeric Retry-After for rate-limit recovery', async () => {
    stubFetch(
      new Response(JSON.stringify({ error: { code: 'rate_limited', message: 'slow down' } }), {
        status: 429,
        headers: { 'Content-Type': 'application/json', 'Retry-After': '5' },
      }),
    );

    await expect(parsed(getMetaOp, transport)).rejects.toMatchObject({
      name: 'ApiError',
      status: 429,
      retryAfterMs: 5_000,
    } satisfies Partial<ApiError>);
  });

  it('drops malformed error bodies instead of treating prose as safe detail', async () => {
    stubFetch(jsonResponse({ detail: 'not the contract error shape' }, 400));

    await expect(parsed(getMetaOp, transport)).rejects.toMatchObject({
      name: 'ApiError',
      status: 400,
      detail: undefined,
    } satisfies Partial<ApiError>);
  });
});

describe('parsedPick', () => {
  it('returns a bound narrow projection when unrelated response metadata drifts', async () => {
    stubFetch(
      jsonResponse(
        { server_version: '1.4.0', api_revision: 'future-shape', protocol_capabilities: [] },
        200,
      ),
    );

    await expect(
      parsedPick(getMetaOp, transport, { server_version: true }),
    ).resolves.toEqual({ server_version: '1.4.0' });
  });
});

describe('ok', () => {
  it('resolves on the operation-bound bodyless success status', async () => {
    stubFetch(new Response(null, { status: 204 }));
    await expect(ok(logoutOp, transport)).resolves.toBeUndefined();
  });

  it('refuses a success status other than the bodyless one, rather than discard a body', async () => {
    // A 200 where the contract says 204 means the contract grew a body this
    // caller ignores - a caller bug, refused as loudly as a failed request.
    stubFetch(jsonResponse({ unexpected: 'body' }, 200));
    await expect(ok(logoutOp, transport)).rejects.toThrow(/bodyless/);
  });

  it('turns a non-2xx into an ApiError carrying contract-validated detail', async () => {
    stubFetch(
      jsonResponse(
        { error: { code: 'conflict', message: 'busy', detail: 'session already revoked' } },
        409,
      ),
    );
    await expect(ok(logoutOp, transport)).rejects.toMatchObject({
      name: 'ApiError',
      status: 409,
      detail: 'session already revoked',
    } satisfies Partial<ApiError>);
  });
});

// The generic error tier: one canonical sentence per case, and, the reason
// this helper exists, "could not be reached" kept distinct from "answered
// something this client cannot understand". Where possible the error is
// produced by driving the REAL chain against a mocked fetch, matching this
// file's convention, so the classification is exercised end to end and not
// against a hand-built stand-in.
describe('transportRefusalText', () => {
  async function refusalFrom(response: Response): Promise<unknown> {
    stubFetch(response);
    return parsed(getMetaOp, transport).then(
      () => {
        throw new Error('expected the operation to reject');
      },
      (error: unknown) => error,
    );
  }

  it('gives one canonical sentence for 429, whatever the operation', async () => {
    const error = await refusalFrom(jsonResponse({ error: { code: 'x', message: 'y' } }, 429));
    expect(transportRefusalText(error)).toBe(
      'Too many requests right now. Wait a moment and try again.',
    );
  });

  it('returns null for every domain status so the feature keeps its own voice', () => {
    for (const status of [401, 403, 404, 409]) {
      expect(transportRefusalText(new ApiError(status, `request failed with ${status}`))).toBeNull();
    }
  });

  it('names the numeric status for an unknown server error', () => {
    expect(transportRefusalText(new ApiError(503, 'request failed with 503'))).toBe(
      'The request could not be completed (server error 503). Try again shortly.',
    );
  });

  it('reads a 2xx body that breaks the contract as a bug, not a network problem', async () => {
    const error = await refusalFrom(jsonResponse({ not: 'the meta shape' }, 200));
    expect(transportRefusalText(error)).toBe(
      'The server answered something this client cannot understand. This is likely a bug worth reporting.',
    );
  });

  it('reads an unbound success status as a contract violation, not transport', async () => {
    // The exact conflation #452 cites: a 201 where 200 was contracted throws a
    // plain Error. It must land in the contract tier, never "check your network".
    const error = await refusalFrom(jsonResponse(META, 201));
    expect(transportRefusalText(error)).toBe(
      'The server answered something this client cannot understand. This is likely a bug worth reporting.',
    );
  });

  it('tells offline apart from a reachable-but-broken server', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
    const error = await parsed(getMetaOp, transport).then(
      () => {
        throw new Error('expected the operation to reject');
      },
      (caught: unknown) => caught,
    );

    vi.stubGlobal('navigator', { onLine: false });
    expect(transportRefusalText(error)).toBe(
      'You appear to be offline. The server could not be reached: check your connection and try again.',
    );

    vi.stubGlobal('navigator', { onLine: true });
    expect(transportRefusalText(error)).toBe('The server could not be reached. Try again shortly.');
  });

  it('treats a dismissed passkey prompt as its own case', () => {
    const dismissed = Object.assign(new Error('The operation was aborted.'), {
      name: 'NotAllowedError',
    });
    expect(transportRefusalText(dismissed)).toBe(
      'The passkey prompt was dismissed or timed out. Nothing was sent.',
    );
  });
});

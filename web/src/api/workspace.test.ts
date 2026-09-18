import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  assertCompatible,
  forgetWorkspace,
  HANDOFF_REQUEST_TIMEOUT_MS,
  openPrepared,
  prepareWorkspace,
  probeWorkspace,
  rememberWorkspace,
  transitionWorkspaceOwner,
  workspaceBearer,
  WorkspaceError,
  type WorkspaceBearer,
} from './workspace.ts';

const bearer: WorkspaceBearer = {
  origin: 'https://peer.example',
  value: 'hik_ws_value',
  session: 'ses_1',
  idleExpiresAt: '2026-01-01T00:00:00Z',
  absoluteExpiresAt: '2026-02-01T00:00:00Z',
};

/** A well-formed answer from the session listing, which is what "alive" means. */
function sessionList(): Response {
  return new Response(
    JSON.stringify({
      sessions: [
        {
          id: 'ses_1',
          artifact: 'workspace',
          auth_method: 'workspace-handoff',
          created_at: '2026-01-01T00:00:00Z',
          last_seen_at: '2026-01-01T00:00:00Z',
          idle_expires_at: '2026-01-01T00:00:00Z',
          absolute_expires_at: '2026-02-01T00:00:00Z',
        },
      ],
    }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

function deferredResponse(): {
  readonly promise: Promise<Response>;
  readonly resolve: (response: Response) => void;
  readonly reject: (error: Error) => void;
} {
  let resolveResponse = (_response: Response): void => {
    throw new Error('deferred response was not initialized');
  };
  let rejectResponse = (_error: Error): void => {
    throw new Error('deferred response was not initialized');
  };
  const promise = new Promise<Response>((resolve, reject) => {
    resolveResponse = resolve;
    rejectResponse = reject;
  });
  return { promise, resolve: resolveResponse, reject: rejectResponse };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  forgetWorkspace(bearer.origin);
  transitionWorkspaceOwner(undefined);
});

/** A fetch that behaves like the real one on abort: it never settles otherwise. */
function hangingFetch() {
  return vi.fn<(input: string, init?: RequestInit) => Promise<Response>>(
    (_input, init) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true });
      }),
  );
}

// Every handoff request carries the caller's signal AND a deadline, and the
// deadline covers the body read too. A stalled remote must become a retryable
// refusal, never a launcher stuck on "Contacting…".
describe('remoteJSON deadlines', () => {
  it('aborts the request on the handoff deadline and refuses with a WorkspaceError', async () => {
    const deadline = new AbortController();
    const timeout = vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal);
    const fetchMock = hangingFetch();
    vi.stubGlobal('fetch', fetchMock);

    const checking = assertCompatible(bearer.origin, { signal: new AbortController().signal });
    const refused = expect(checking).rejects.toThrow(
      new WorkspaceError(`${bearer.origin} did not answer within 15 seconds. Try again.`),
    );
    deadline.abort();
    await refused;

    expect(timeout).toHaveBeenCalledWith(HANDOFF_REQUEST_TIMEOUT_MS);
    const init = fetchMock.mock.calls[0]?.[1];
    expect(init?.signal?.aborted).toBe(true);
  });

  it('applies the deadline to a body that never finishes arriving', async () => {
    const deadline = new AbortController();
    vi.spyOn(AbortSignal, 'timeout').mockReturnValue(deadline.signal);
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(new ReadableStream({ start() {} }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        ),
      ),
    );

    const checking = assertCompatible(bearer.origin, { signal: new AbortController().signal });
    const refused = expect(checking).rejects.toBeInstanceOf(WorkspaceError);
    deadline.abort();
    await refused;
  });

  it("rethrows the caller's own abort reason so disposal is not mistaken for a stalled remote", async () => {
    const caller = new AbortController();
    vi.stubGlobal('fetch', hangingFetch());

    const checking = assertCompatible(bearer.origin, { signal: caller.signal });
    const abandoned = expect(checking).rejects.toSatisfy(
      (error: unknown) => error === caller.signal.reason && !(error instanceof WorkspaceError),
    );
    caller.abort();
    await abandoned;
  });

  it('carries one caller signal through every phase of a prepare', async () => {
    const caller = new AbortController();
    const seen: Array<AbortSignal | null | undefined> = [];
    vi.stubGlobal('location', { origin: 'https://viewer.example' });
    vi.stubGlobal(
      'fetch',
      vi.fn((input: string, init?: RequestInit) => {
        seen.push(init?.signal);
        const body = String(input).endsWith('/api/v1/meta')
          ? { server_version: '1.0.0', api_revision: 1, protocol_capabilities: [] }
          : {
              handoff: 'ic_00000000-0000-4000-8000-000000000001',
              state: 'hik_1_hs_abc',
              expires_at: '2099-01-01T00:00:00Z',
            };
        return Promise.resolve(
          new Response(JSON.stringify(body), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }),
    );

    await prepareWorkspace(bearer.origin, { signal: caller.signal });
    expect(seen).toHaveLength(2);
    expect(seen.every((signal) => signal instanceof AbortSignal && !signal.aborted)).toBe(true);
    caller.abort();
    expect(seen.every((signal) => signal?.aborted)).toBe(true);
  });
});

// The popup wait keeps the ceremony's own five-minute deadline, but a launcher
// that has been disposed has nobody to hand the code to: its abort ends the
// wait and releases the channel instead of leaving a listener behind.
describe('openPrepared disposal', () => {
  it('ends the front-channel wait when the caller aborts', async () => {
    const closed = vi.fn();
    class IdleChannel {
      onmessage: ((event: MessageEvent) => void) | null = null;
      close = closed;
    }
    vi.stubGlobal('BroadcastChannel', IdleChannel);
    vi.stubGlobal('open', vi.fn());
    vi.stubGlobal('location', { origin: 'https://viewer.example' });
    vi.stubGlobal(
      'fetch',
      vi.fn((input: string) => {
        const body = String(input).endsWith('/api/v1/meta')
          ? { server_version: '1.0.0', api_revision: 1, protocol_capabilities: [] }
          : {
              handoff: 'ic_00000000-0000-4000-8000-000000000001',
              state: 'hik_1_hs_abc',
              expires_at: '2099-01-01T00:00:00Z',
            };
        return Promise.resolve(
          new Response(JSON.stringify(body), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }),
    );
    const prepared = await prepareWorkspace(bearer.origin, {
      signal: new AbortController().signal,
    });

    const caller = new AbortController();
    const opening = openPrepared(prepared, { signal: caller.signal });
    const abandoned = expect(opening).rejects.toSatisfy((error) => error === caller.signal.reason);
    caller.abort();
    await abandoned;

    expect(closed).toHaveBeenCalledOnce();
    expect(workspaceBearer(bearer.origin)).toBeUndefined();
  });
});

// A blip must not cost a ceremony, and a re-established workspace is a NEW
// session: it gets its own two strikes. Carrying the old count over kills the
// reconnected workspace on its first blip, which reads to the human as "the
// reconnect did not work".
describe('probeWorkspace strike counting', () => {
  it('gives a re-established workspace its full strike allowance again', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    );
    rememberWorkspace(bearer);

    // First unreachable probe: survived, because one failure is a blip.
    expect(await probeWorkspace(bearer)).toBe(true);
    // Second: the workspace dies and is forgotten.
    expect(await probeWorkspace(bearer)).toBe(false);
    // The human reconnects and the very next blip arrives. It must be strike
    // ONE again, not strike three.
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(true);
  });

  it('drops the workspace when the remote refuses the bearer, blips or not', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: 401 }))),
    );
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(false);
    expect(workspaceBearer(bearer.origin)).toBeUndefined();
  });

  it('removes bearer and health together when the workspace is closed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    );
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(true);

    forgetWorkspace(bearer.origin);
    expect(workspaceBearer(bearer.origin)).toBeUndefined();

    const reopened = { ...bearer, session: 'ses_2' };
    rememberWorkspace(reopened);
    expect(await probeWorkspace(reopened)).toBe(true);
    expect(workspaceBearer(bearer.origin)?.session).toBe('ses_2');
  });
});

// A probe is asynchronous and the human is not. Closing a workspace and opening
// a new one to the same origin while the old probe is still in flight used to
// let the old probe's verdict delete the NEW session.
describe('probeWorkspace session identity', () => {
  it('ignores a completion about a session that has been replaced', async () => {
    const response = deferredResponse();
    vi.stubGlobal('fetch', vi.fn(() => response.promise));
    rememberWorkspace(bearer);
    const inFlight = probeWorkspace(bearer);

    // The human closes S1 and establishes S2 to the same origin.
    const replacement: WorkspaceBearer = { ...bearer, session: 'ses_2', value: 'hik_ws_second' };
    rememberWorkspace(replacement);

    // S1's probe now fails. It must not touch S2.
    response.resolve(new Response(null, { status: 401 }));
    expect(await inFlight).toBe(false);
    expect(workspaceBearer(bearer.origin)?.session).toBe('ses_2');
  });

  // A step-up ELEVATES in place: same session id, a freshly rotated value. A
  // probe fired with the pre-elevation value must not, on its stale 401, take
  // down the live elevated bearer that shares its session id, the drop is keyed
  // by local epoch, exactly as the transport's kill path is.
  it('ignores a stale 401 for a value the same session has since rotated', async () => {
    const response = deferredResponse();
    vi.stubGlobal('fetch', vi.fn(() => response.promise));
    rememberWorkspace(bearer);
    const inFlight = probeWorkspace(bearer);

    // The step-up rotates the value under the SAME session id.
    const elevated: WorkspaceBearer = { ...bearer, value: 'hik_ws_elevated' };
    rememberWorkspace(elevated);

    response.resolve(new Response(null, { status: 401 }));
    expect(await inFlight).toBe(false);
    expect(workspaceBearer(bearer.origin)?.value).toBe('hik_ws_elevated');
    expect(workspaceBearer(bearer.origin)?.session).toBe('ses_1');
  });

  it('does not report a stale successful probe as health for the replacement session', async () => {
    const response = deferredResponse();
    vi.stubGlobal('fetch', vi.fn(() => response.promise));
    rememberWorkspace(bearer);
    const inFlight = probeWorkspace(bearer);

    rememberWorkspace({ ...bearer, session: 'ses_2', value: 'hik_ws_second' });

    response.resolve(sessionList());
    expect(await inFlight).toBe(false);
    expect(workspaceBearer(bearer.origin)?.session).toBe('ses_2');
  });

  it('does not spend a stale failed probe against a replacement epoch', async () => {
    const response = deferredResponse();
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => response.promise)
      .mockRejectedValue(new TypeError('Failed to fetch'));
    vi.stubGlobal('fetch', fetchMock);
    rememberWorkspace(bearer);
    const inFlight = probeWorkspace(bearer);

    // Epoch, not bearer text, owns the strike count. Reusing the same value in
    // this adversarial case proves old async work cannot mutate replacement.
    const replacement = { ...bearer, session: 'ses_2' };
    rememberWorkspace(replacement);
    response.reject(new TypeError('Failed to fetch'));

    expect(await inFlight).toBe(false);
    expect(await probeWorkspace(replacement)).toBe(true);
    expect(workspaceBearer(bearer.origin)?.session).toBe('ses_2');
  });
});

describe('root session ownership', () => {
  it('preserves workspace health while the root session epoch is unchanged', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    );
    transitionWorkspaceOwner('browser_1');
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(true);

    transitionWorkspaceOwner('browser_1');

    expect(await probeWorkspace(bearer)).toBe(false);
    expect(workspaceBearer(bearer.origin)).toBeUndefined();
  });

  it.each([
    ['logout or expiry', undefined],
    ['session replacement', 'browser_2'],
  ])('removes the whole workspace aggregate on %s', async (_transition, nextSession) => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    );
    transitionWorkspaceOwner('browser_1');
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(true);

    transitionWorkspaceOwner(nextSession);

    expect(workspaceBearer(bearer.origin)).toBeUndefined();
    const reopened = { ...bearer, session: 'ses_2' };
    rememberWorkspace(reopened);
    expect(await probeWorkspace(reopened)).toBe(true);
  });
});

// A response the shell cannot recognise is not evidence of life. Before this a
// 404, a 500 or an HTML error page from something in the path all cleared the
// strike counter and kept the card claiming "workspace open".
describe('probeWorkspace response validation', () => {
  it.each([
    ['a 404', new Response(null, { status: 404 })],
    ['a 500', new Response(null, { status: 500 })],
    ['a 200 carrying HTML', new Response('<html>captive portal</html>', { status: 200 })],
    ['a 200 carrying the wrong JSON', new Response('{"ok":true}', { status: 200 })],
  ])('counts %s as a strike rather than as life', async (_name, response) => {
    const responses = [response.clone(), response.clone()];
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(responses.shift() as Response)),
    );
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(true); // strike one
    expect(await probeWorkspace(bearer)).toBe(false); // strike two ends it
    expect(workspaceBearer(bearer.origin)).toBeUndefined();
  });

  it('treats a 403 as alive, never dropping a valid session on a spurious forbidden', async () => {
    // /me/sessions is self-scoped and cannot legitimately 403 a live session; a
    // 403 here is anomalous (a proxy/WAF), not death (that is 401) and not
    // unreachability. Two of them in a row must NOT kill the workspace, that
    // would be a false reconnect the human never earned.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: 403 }))),
    );
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(true);
    expect(await probeWorkspace(bearer)).toBe(true);
    expect(workspaceBearer(bearer.origin)?.session).toBe('ses_1');
  });

  it('accepts a well-formed session listing', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(sessionList())),
    );
    rememberWorkspace(bearer);
    expect(await probeWorkspace(bearer)).toBe(true);
    expect(workspaceBearer(bearer.origin)?.session).toBe('ses_1');
  });

  it('sets a deadline so a hung probe cannot stall the poll forever', async () => {
    const seen: RequestInit[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: string, init: RequestInit) => {
        seen.push(init);
        return Promise.resolve(sessionList());
      }),
    );
    rememberWorkspace(bearer);
    await probeWorkspace(bearer);
    expect(seen[0]?.signal).toBeInstanceOf(AbortSignal);
  });
});

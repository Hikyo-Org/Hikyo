import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { blockSessionEpoch, installSessionFence } from './sessionEpoch.ts';

import {
  forgetWorkspace,
  openPrepared,
  prepareWorkspace,
  rememberWorkspace,
  transitionWorkspaceOwner,
  workspaceBearer,
  type WorkspaceBearer,
} from './workspace.ts';

const origin = 'https://peer.example';
const state = 'hik_1_hs_abc';
const original: WorkspaceBearer = {
  origin,
  value: 'hik_ws_original',
  session: 'ses_00000000-0000-4000-8000-000000000001',
  idleExpiresAt: '2099-01-01T00:00:00Z',
  absoluteExpiresAt: '2099-01-01T00:00:00Z',
};
const replacement = { ...original, value: 'hik_ws_replacement', session: 'ses_00000000-0000-4000-8000-000000000002' };
const stepUp = {
  session: original.session,
  operation: 'reveal',
  environment: 'env_1',
  keySet: ['key_1'],
} satisfies NonNullable<Parameters<typeof prepareWorkspace>[1]>;

function deferred<T>() {
  let complete = (_value: T): void => {
    throw new Error('Deferred promise was not initialized');
  };
  const promise = new Promise<T>((resolve) => {
    complete = resolve;
  });
  return { promise, resolve: complete };
}

function json(body: object): Response {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  });
}

function meta(): Response {
  return json({ server_version: '1.0.0', api_revision: 1, protocol_capabilities: [] });
}

function started(): Response {
  return json({
    handoff: 'ic_00000000-0000-4000-8000-000000000001',
    state,
    expires_at: '2099-01-01T00:00:00Z',
  });
}

function redeemed(): Response {
  return json({
    value: original.value,
    origin: 'https://viewer.example',
    handoff: 'ic_00000000-0000-4000-8000-000000000001',
    session: original.session,
    idle_expires_at: original.idleExpiresAt,
    absolute_expires_at: original.absoluteExpiresAt,
  });
}

class CallbackChannel {
  static active: CallbackChannel | undefined;
  onmessage: ((event: MessageEvent) => void) | null = null;
  close = vi.fn();

  constructor() {
    CallbackChannel.active = this;
  }

  static deliver(): void {
    if (this.active?.onmessage === null || this.active?.onmessage === undefined) {
      throw new Error('No callback listener');
    }
    this.active.onmessage(new MessageEvent('message', { data: { code: 'code_1', state } }));
  }
}

beforeEach(() => {
  transitionWorkspaceOwner('browser_A');
  vi.stubGlobal('location', { origin: 'https://viewer.example' });
  vi.stubGlobal('open', vi.fn());
  vi.stubGlobal('BroadcastChannel', CallbackChannel);
});

afterEach(() => {
  transitionWorkspaceOwner(undefined);
  vi.unstubAllGlobals();
  CallbackChannel.active = undefined;
});

it('rejects a delayed compatibility response before starting the handoff', async () => {
  const response = deferred<Response>();
  const fetchMock = vi.fn(() => response.promise);
  vi.stubGlobal('fetch', fetchMock);
  const preparing = prepareWorkspace(origin);
  const rejected = expect(preparing).rejects.toThrow('session changed');

  transitionWorkspaceOwner(undefined);
  response.resolve(meta());

  await rejected;
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

it('rejects a delayed start response after A -> signed out -> A', async () => {
  const response = deferred<Response>();
  const startSent = deferred<void>();
  const fetchMock = vi.fn((url: string) => {
    if (url.endsWith('/meta')) return Promise.resolve(meta());
    startSent.resolve();
    return response.promise;
  });
  vi.stubGlobal('fetch', fetchMock);
  const preparing = prepareWorkspace(origin);
  const rejected = expect(preparing).rejects.toThrow('session changed');
  await startSent.promise;

  transitionWorkspaceOwner(undefined);
  transitionWorkspaceOwner('browser_A');
  response.resolve(started());

  await rejected;
  expect(workspaceBearer(origin)).toBeUndefined();
});

it('refuses to open a prepared handoff belonging to an earlier owner epoch', async () => {
  const fetchMock = vi.fn().mockResolvedValueOnce(meta()).mockResolvedValueOnce(started());
  vi.stubGlobal('fetch', fetchMock);
  const prepared = await prepareWorkspace(origin);

  transitionWorkspaceOwner(undefined);
  transitionWorkspaceOwner('browser_A');

  await expect(openPrepared(prepared)).rejects.toThrow('session changed');
  expect(globalThis.open).not.toHaveBeenCalled();
  expect(fetchMock).toHaveBeenCalledTimes(2);
});

it('rejects a late popup callback before redeeming its code', async () => {
  const fetchMock = vi.fn().mockResolvedValueOnce(meta()).mockResolvedValueOnce(started());
  vi.stubGlobal('fetch', fetchMock);
  const prepared = await prepareWorkspace(origin);
  const opening = openPrepared(prepared);
  const rejected = expect(opening).rejects.toThrow('session changed');

  transitionWorkspaceOwner('browser_B');
  rememberWorkspace(replacement);
  CallbackChannel.deliver();

  await rejected;
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(workspaceBearer(origin)).toEqual(replacement);
  expect(CallbackChannel.active?.close).toHaveBeenCalledOnce();
});

it.each(['logout', 'replacement', 'same owner returns'])(
  'discards and revokes a late redemption after %s',
  async (transition) => {
    const response = deferred<Response>();
    const redeemSent = deferred<void>();
    const fetchMock = vi.fn((url: string) => {
      if (url.endsWith('/meta')) return Promise.resolve(meta());
      if (url.endsWith('/start')) return Promise.resolve(started());
      if (url.endsWith('/redeem')) {
        redeemSent.resolve();
        return response.promise;
      }
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    vi.stubGlobal('fetch', fetchMock);
    const prepared = await prepareWorkspace(origin);
    const opening = openPrepared(prepared);
    const rejected = expect(opening).rejects.toThrow('session changed');
    CallbackChannel.deliver();
    await redeemSent.promise;

    transitionWorkspaceOwner(undefined);
    if (transition !== 'logout') {
      transitionWorkspaceOwner(transition === 'replacement' ? 'browser_B' : 'browser_A');
      rememberWorkspace(replacement);
    }
    response.resolve(redeemed());

    await rejected;
    expect(workspaceBearer(origin)).toEqual(transition === 'logout' ? undefined : replacement);
    expect(fetchMock).toHaveBeenLastCalledWith(
      `${origin}/api/v1/me/sessions/${original.session}`,
      expect.objectContaining({
        method: 'DELETE',
        mode: 'cors',
        credentials: 'omit',
        headers: { Authorization: `Bearer ${original.value}` },
        signal: expect.any(AbortSignal),
      }),
    );
  },
);

it.each(['hangs', 'rejects'])('keeps local rejection immediate when remote revocation %s', async (failure) => {
  const response = deferred<Response>();
  const redeemSent = deferred<void>();
  const never = deferred<Response>();
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url.endsWith('/meta')) return Promise.resolve(meta());
      if (url.endsWith('/start')) return Promise.resolve(started());
      if (url.endsWith('/redeem')) {
        redeemSent.resolve();
        return response.promise;
      }
      return failure === 'hangs' ? never.promise : Promise.reject(new TypeError('Failed to fetch'));
    }),
  );
  const prepared = await prepareWorkspace(origin);
  const opening = openPrepared(prepared);
  const rejected = expect(opening).rejects.toThrow('session changed');
  CallbackChannel.deliver();
  await redeemSent.promise;
  transitionWorkspaceOwner(undefined);
  response.resolve(redeemed());

  await rejected;
  expect(workspaceBearer(origin)).toBeUndefined();
});

it('preserves a live step-up and rotates its bearer within the same owner', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(meta())
    .mockResolvedValueOnce(started())
    .mockResolvedValueOnce(redeemed());
  vi.stubGlobal('fetch', fetchMock);
  rememberWorkspace({ ...original, value: 'hik_ws_before_stepup' });
  const prepared = await prepareWorkspace(origin, stepUp);
  transitionWorkspaceOwner('browser_A');
  const opening = openPrepared(prepared);
  CallbackChannel.deliver();

  await expect(opening).resolves.toEqual(original);
  expect(workspaceBearer(origin)).toEqual(original);
});

it('refuses a prepared step-up after its workspace is closed and reopened', async () => {
  const fetchMock = vi.fn().mockResolvedValueOnce(meta()).mockResolvedValueOnce(started());
  vi.stubGlobal('fetch', fetchMock);
  rememberWorkspace(original);
  const prepared = await prepareWorkspace(origin, stepUp);
  forgetWorkspace(origin);
  rememberWorkspace(original);

  await expect(openPrepared(prepared)).rejects.toThrow('session changed');
  expect(globalThis.open).not.toHaveBeenCalled();
  expect(workspaceBearer(origin)).toEqual(original);
});


it('rejects a late redemption when the shared cookie changes before notification', async () => {
  const documentState = { cookie: '__Host-hikyo-csrf=owner_A' };
  vi.stubGlobal('document', documentState);
  const removeFence = installSessionFence(() => {
    blockSessionEpoch();
    transitionWorkspaceOwner(undefined);
  }, () => {});
  try {
    const response = deferred<Response>();
    const redeemSent = deferred<void>();
    const fetchMock = vi.fn((url: string) => {
      if (url.endsWith('/meta')) return Promise.resolve(meta());
      if (url.endsWith('/start')) return Promise.resolve(started());
      if (url.endsWith('/redeem')) {
        redeemSent.resolve();
        return response.promise;
      }
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    vi.stubGlobal('fetch', fetchMock);
    const prepared = await prepareWorkspace(origin);
    const opening = openPrepared(prepared);
    const rejected = expect(opening).rejects.toThrow('browser session changed');
    CallbackChannel.deliver();
    await redeemSent.promise;
    documentState.cookie = '__Host-hikyo-csrf=owner_B';
    response.resolve(redeemed());

    await rejected;
    expect(workspaceBearer(origin)).toBeUndefined();
    expect(fetchMock).toHaveBeenCalledTimes(4);
  } finally {
    removeFence();
  }
});

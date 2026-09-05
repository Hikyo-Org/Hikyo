// @vitest-environment happy-dom
import { createClient } from '@hikyo/runtime-core';
import { QueryClient, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { act, useState, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  forgetWorkspace,
  rememberWorkspace,
  workspaceBearer,
  type WorkspaceBearer,
} from '../api/workspace.ts';
import { renderForm, settle, settleTask } from '../testkit/renderForm.tsx';
import { parsed, parsedPick } from '../api/client.ts';
import { listMyOrgsOp, localLoginOp, regenerateRecoveryCodesOp, stepUpTotpOp } from '@hikyo/operations';
import { SessionChangedError } from '../api/sessionEpoch.ts';
import { AuthProvider, useAuth, type WhoAmI } from './AuthProvider.tsx';

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T) => void;
};

function deferred<T>(): Deferred<T> {
  let resolve: (value: T) => void = (_value) => {
    throw new Error('deferred promise was resolved before construction');
  };
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

const id = (prefix: string, suffix: string) =>
  `${prefix}_123e4567-e89b-12d3-a456-4266141740${suffix}`;

const workspace: WorkspaceBearer = {
  origin: 'https://peer.example',
  value: 'hik_ws_value',
  session: 'ses_workspace',
  idleExpiresAt: '2099-08-22T10:30:00Z',
  absoluteExpiresAt: '2099-08-22T18:00:00Z',
};

function identity(
  sessionSuffix: string,
  principalSuffix: string,
  factors = ['password'],
): WhoAmI {
  return {
    session: {
      id: id('ses', sessionSuffix),
      artifact: 'browser',
      created_at: '2026-08-22T10:00:00Z',
      idle_expires_at: '2099-08-22T10:30:00Z',
      absolute_expires_at: '2099-08-22T18:00:00Z',
      assurance: {
        method: 'local-password',
        factors,
        authenticated_at: '2026-08-22T10:00:00Z',
      },
    },
    principal: {
      id: id('prn', principalSuffix),
      kind: 'human',
      display_name: `Person ${principalSuffix}`,
    },
    capabilities: { instance_operator: false },
  };
}

/** A login/step-up result: the same identity a mutation returns, minus the
 *  capabilities only whoami carries. */
function loginIdentity(sessionSuffix: string, principalSuffix: string): WhoAmI {
  const { capabilities: _omitted, ...rest } = identity(sessionSuffix, principalSuffix);
  return rest as WhoAmI;
}

function operatorWhoAmI(sessionSuffix: string, principalSuffix: string): WhoAmI {
  return { ...identity(sessionSuffix, principalSuffix), capabilities: { instance_operator: true } };
}

function json(body: object, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function Probe({
  oldResult,
  mutationResult,
}: {
  oldResult?: Deferred<string>;
  mutationResult?: Deferred<WhoAmI>;
}) {
  const auth = useAuth();
  const queries = useQueryClient();
  const [, renderMarker] = useState(0);
  const epoch = auth.state.status === 'authenticated' ? auth.state.sessionEpoch : '';
  const privateQuery = useQuery({
    queryKey: ['private'],
    queryFn: () =>
      epoch.endsWith('00') && oldResult !== undefined
        ? oldResult.promise
        : Promise.resolve(`fresh:${epoch}`),
    enabled: epoch !== '',
    retry: false,
  });

  return (
    <>
      <output data-testid="state">
        {auth.state.status}
        {epoch === '' ? '' : `:${epoch}`}
      </output>
      <output data-testid="private">
        {auth.state.status === 'authenticated' ? (privateQuery.data ?? '') : ''}
      </output>
      <output data-testid="operator">
        {auth.identity === null ? '' : String(auth.identity.capabilities.instance_operator)}
      </output>
      <output data-testid="failure">{auth.failure === null ? '' : 'failed'}</output>
      <output data-testid="degraded">{auth.degraded === null ? '' : 'degraded'}</output>
      <output data-testid="marker">{String(queries.getQueryData(['marker']) ?? '')}</output>
      <button type="button" onClick={() => void auth.revalidate()}>
        Revalidate
      </button>
      <button
        type="button"
        onClick={() => {
          queries.setQueryData(['marker'], 'owned');
          renderMarker((value) => value + 1);
        }}
      >
        Mark cache
      </button>
      <button
        type="button"
        onClick={() => {
          const guard = auth.captureTransition();
          void mutationResult?.promise.then((result) => auth.acceptSession(result, guard));
        }}
      >
        Start mutation
      </button>
    </>
  );
}

function text(container: HTMLElement, testId: string): string {
  return container.querySelector(`[data-testid="${testId}"]`)?.textContent ?? '';
}

async function expectTextEventually(
  container: HTMLElement,
  testId: string,
  expected: string,
): Promise<void> {
  await vi.waitFor(async () => {
    await settle();
    expect(text(container, testId)).toContain(expected);
  });
}

const unmounts: Array<() => Promise<void>> = [];

async function renderAuth(node: ReactNode) {
  const rendered = await renderForm(node);
  unmounts.push(rendered.unmount);
  return rendered;
}

afterEach(async () => {
  await Promise.all(unmounts.splice(0).map((unmount) => unmount()));
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  forgetWorkspace(workspace.origin);
});

describe('AuthProvider', () => {
  it('drops deferred results from the previous session before rendering a replacement', async () => {
    const oldResult = deferred<string>();
    const replacement = deferred<Response>();
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(replacement.promise);
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe oldResult={oldResult} />
      </AuthProvider>,
    );
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);
    rememberWorkspace(workspace);
    expect(workspaceBearer(workspace.origin)).toBe(workspace);

    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[1]?.click());
    expect(text(container, 'marker')).toBe('owned');
    await act(async () => buttons[0]?.click());
    expect(text(container, 'state')).toBe('transitioning');
    expect(text(container, 'private')).toBe('');

    await act(async () => replacement.resolve(json(identity('01', '11'))));
    await settleTask();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '01')}`);
    expect(text(container, 'marker')).toBe('');
    expect(workspaceBearer(workspace.origin)).toBeUndefined();

    oldResult.resolve('old-session-secret');
    await settle();
    expect(container.textContent).not.toContain('old-session-secret');
  });

  it('preserves the session cache for a same-epoch assurance refresh', async () => {
    const current = identity('00', '10');
    const elevated = identity('00', '10', ['password', 'totp']);
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(current))
      .mockResolvedValueOnce(json(elevated));
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await settle();

    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[1]?.click());
    rememberWorkspace(workspace);
    expect(text(container, 'marker')).toBe('owned');
    await act(async () => buttons[0]?.click());
    await settle();

    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);
    expect(text(container, 'marker')).toBe('owned');
    expect(workspaceBearer(workspace.origin)).toBe(workspace);
  });

  it('moves an expired session to anonymous and discards its cache', async () => {
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(identity('00', '10')))
      .mockResolvedValueOnce(json({ error: { code: 'unauthenticated', message: 'no session' } }, 401));
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await settle();

    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[1]?.click());
    rememberWorkspace(workspace);
    await act(async () => buttons[0]?.click());
    await settle();

    expect(text(container, 'state')).toBe('anonymous');
    expect(text(container, 'marker')).toBe('');
    expect(workspaceBearer(workspace.origin)).toBeUndefined();
  });

  it('keeps painting the current session while a focus check is in flight, then settles a replacement', async () => {
    const replacement = deferred<Response>();
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(replacement.promise);
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await expectTextEventually(container, 'private', id('ses', '00'));

    // A refocus must not blank the tree: the session we hold keeps painting
    // until the quiet check learns it has actually been replaced.
    await act(async () => globalThis.dispatchEvent(new Event('focus')));
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);
    expect(text(container, 'private')).toContain(id('ses', '00'));

    await act(async () => replacement.resolve(json(identity('01', '11'))));
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '01')}`);
  });

  it('leaves an unchanged session untouched on refocus', async () => {
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(identity('00', '10')))
      .mockResolvedValueOnce(json(identity('00', '10')));
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await settle();

    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[1]?.click());
    rememberWorkspace(workspace);
    expect(text(container, 'marker')).toBe('owned');

    await act(async () => globalThis.dispatchEvent(new Event('focus')));
    await settle();

    // No flash to a loading state, and no session-cache teardown: the refocus
    // re-read the same identity and changed nothing.
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);
    expect(text(container, 'marker')).toBe('owned');
    expect(workspaceBearer(workspace.origin)).toBe(workspace);
  });

  it('defaults capabilities on a login result and hydrates them from whoami', async () => {
    const mutationResult = deferred<WhoAmI>();
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(identity('00', '10')))
      .mockResolvedValueOnce(json(operatorWhoAmI('01', '11')));
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe mutationResult={mutationResult} />
      </AuthProvider>,
    );
    await settle();
    expect(text(container, 'operator')).toBe('false');

    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[2]?.click());
    // The login result carries no capabilities, so the session binds with the
    // fail-closed default and then a whoami hydrates the authoritative value —
    // without a second login.
    await act(async () => mutationResult.resolve(loginIdentity('01', '11')));
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '01')}`);
    expect(text(container, 'operator')).toBe('true');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('latches the reload wall when the very first check has no session to fall back on', async () => {
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockRejectedValueOnce(new Error('server unreachable'));
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await settle();

    // No identity was ever held, so the transport failure is the authoritative
    // answer and App's reload wall is correct.
    expect(text(container, 'failure')).toBe('failed');
    expect(text(container, 'degraded')).toBe('');
  });

  it('keeps a still-valid session painting through a background revalidation blip and recovers', async () => {
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(identity('00', '10')))
      .mockRejectedValueOnce(new Error('server briefly unreachable'))
      .mockResolvedValue(json(identity('00', '10')));
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);

    // A background revalidation rejects while the session is still valid.
    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[0]?.click());
    await settle();

    // The session keeps painting; the error surfaces only as the non-latching
    // degraded signal, never the global reload wall.
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);
    expect(text(container, 'failure')).toBe('');
    expect(text(container, 'degraded')).toBe('degraded');

    // The next successful revalidation clears the degraded signal.
    await act(async () => buttons[0]?.click());
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);
    expect(text(container, 'degraded')).toBe('');
  });

  it('recovers a degraded session on its own through the backoff retry', async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi
        .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
        .mockResolvedValueOnce(json(identity('00', '10')))
        .mockRejectedValueOnce(new Error('server briefly unreachable'))
        .mockResolvedValue(json(identity('00', '10')));
      vi.stubGlobal('fetch', fetchMock);

      const { container } = await renderAuth(
        <AuthProvider>
          <Probe />
        </AuthProvider>,
      );
      await settle();
      expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);

      // A background BLOCKING revalidate (a peer-tab broadcast or the expiry
      // timer) fails, degrading the still-valid session.
      const buttons = container.querySelectorAll('button');
      await act(async () => buttons[0]?.click());
      await settle();
      expect(text(container, 'degraded')).toBe('degraded');
      expect(text(container, 'failure')).toBe('');

      // No manual revalidation: only the backoff timer fires. Advancing past its
      // first interval must recover the session on its own — deleting the retry
      // effect leaves `degraded` set here forever.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1_100);
      });
      await settle();
      expect(text(container, 'degraded')).toBe('');
      expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);
    } finally {
      vi.useRealTimers();
    }
  });

  it('walls a session whose absolute deadline has passed even when the server is unreachable', async () => {
    const expired = identity('00', '10');
    const dead = {
      ...expired,
      session: { ...expired.session, absolute_expires_at: '2000-01-01T00:00:00Z' },
    };
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(dead))
      .mockRejectedValueOnce(new Error('server unreachable'));
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '00')}`);

    // The held identity is past its unextendable absolute deadline, so a failed
    // background revalidation must NOT keep it painting as a degraded-but-valid
    // session — it is dead regardless of the server, and the wall is correct.
    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[0]?.click());
    await settle();
    expect(text(container, 'failure')).toBe('failed');
    expect(text(container, 'degraded')).toBe('');
  });

  it('ignores a mutation result captured before a newer session replacement', async () => {
    const mutationResult = deferred<WhoAmI>();
    const authoritativeIdentity = deferred<Response>();
    const fetchMock = vi
      .fn<(...args: Parameters<typeof fetch>) => Promise<Response>>()
      .mockResolvedValueOnce(json(identity('00', '10')))
      .mockResolvedValueOnce(json(identity('01', '11')))
      .mockReturnValueOnce(authoritativeIdentity.promise);
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderAuth(
      <AuthProvider>
        <Probe mutationResult={mutationResult} />
      </AuthProvider>,
    );
    await settle();

    const buttons = container.querySelectorAll('button');
    await act(async () => buttons[2]?.click());
    await act(async () => buttons[0]?.click());
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '01')}`);

    await act(async () => mutationResult.resolve(identity('02', '12')));
    expect(text(container, 'state')).toBe('transitioning');
    expect(container.textContent).not.toContain(id('ses', '02'));

    await act(async () => authoritativeIdentity.resolve(json(identity('01', '11'))));
    await settle();
    expect(text(container, 'state')).toContain(`authenticated:${id('ses', '01')}`);
    expect(container.textContent).not.toContain(id('ses', '02'));
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });
});

class PeerChannel extends EventTarget {
  static peers = new Set<PeerChannel>();
  constructor(readonly name: string) { super(); PeerChannel.peers.add(this); }
  postMessage(data: { type: string; sender: string }) {
    for (const peer of PeerChannel.peers) {
      if (peer !== this && peer.name === this.name) {
        queueMicrotask(() => peer.dispatchEvent(new MessageEvent('message', { data })));
      }
    }
  }
  close() { PeerChannel.peers.delete(this); }
}

function DisclosureProbe({ clients }: { clients: QueryClient[] }) {
  const auth = useAuth();
  const client = useQueryClient();
  if (!clients.includes(client)) clients.push(client);
  const [secret, setSecret] = useState('');
  return <>
    <output data-testid="owner">{auth.identity?.principal.display_name ?? auth.state.status}</output>
    <output data-testid="disclosure">{secret}</output>
    <button onClick={() => {
      setSecret('A display-once secret');
      client.setQueryData(['private'], 'A cached secret');
      client.getMutationCache().build(client, { mutationKey: ['private-mutation'] });
    }}>Reveal</button>
    <button onClick={() => void auth.revalidate()}>Check identity</button>
  </>;
}

describe('cross-tab session ownership', () => {
  it('purges both caches and component disclosures before peer whoami completes', async () => {
    vi.stubGlobal('BroadcastChannel', PeerChannel);
    const replacement = deferred<Response>();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(replacement.promise));
    const clients: QueryClient[] = [];
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={clients} /></AuthProvider>);
    await settle();
    await act(async () => container.querySelector('button')?.click());
    const old = clients.at(-1);
    expect(old?.getQueryData(['private'])).toBe('A cached secret');
    rememberWorkspace(workspace);
    const peer = new PeerChannel('hikyo-root-auth');
    await act(async () => peer.postMessage({ type: 'session-changed', sender: 'peer-tab' }));
    expect(text(container, 'owner')).toBe('checking');
    expect(text(container, 'disclosure')).toBe('');
    expect(old?.getQueryCache().getAll()).toHaveLength(0);
    expect(old?.getMutationCache().getAll()).toHaveLength(0);
    expect(workspaceBearer(workspace.origin)).toBeUndefined();
    // A mutation callback retaining the old QueryClient cannot write to B's.
    old?.setQueryData(['private'], 'late A mutation');
    await act(async () => replacement.resolve(json(identity('01', '11'))));
    expect(text(container, 'owner')).toBe('Person 11');
    expect(clients.at(-1)).not.toBe(old);
    expect(clients.at(-1)?.getQueryData(['private'])).toBeUndefined();
    peer.close();
  });

  it('blocks a request under a replaced cookie before a broadcast is delivered', async () => {
    let cookie = '__Host-hikyo-csrf=A';
    vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
    const replacement = deferred<Response>();
    const fetchMock = vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(replacement.promise);
    vi.stubGlobal('fetch', fetchMock);
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    await settle();
    await act(async () => container.querySelector('button')?.click());
    cookie = '__Host-hikyo-csrf=B';
    await act(async () => {
      await expect(parsed(listMyOrgsOp, {})).rejects.toBeInstanceOf(SessionChangedError);
    });
    expect(text(container, 'disclosure')).toBe('');
    // Only identity requests reached fetch, never an org request as B under A.
    expect(fetchMock).toHaveBeenCalledTimes(2);
    await act(async () => replacement.resolve(json(identity('01', '11'))));
    expect(text(container, 'owner')).toBe('Person 11');
  });

  it('discards an old response after cookie replacement before peer notification', async () => {
    let cookie = '__Host-hikyo-csrf=A';
    vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
    const oldResponse = deferred<Response>();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(oldResponse.promise).mockResolvedValueOnce(json(identity('01', '11'))));
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    await settle();
    const result = parsed(listMyOrgsOp, {});
    const rejected = expect(result).rejects.toBeInstanceOf(SessionChangedError);
    await settle();
    cookie = '__Host-hikyo-csrf=B';
    await act(async () => oldResponse.resolve(json({ orgs: [] })));
    await rejected;
    await settle();
    expect(text(container, 'owner')).toBe('Person 11');
  });

  it('never binds an old whoami response to a newer cookie', async () => {
    let cookie = '__Host-hikyo-csrf=A';
    vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
    const oldIdentity = deferred<Response>();
    vi.stubGlobal('fetch', vi.fn().mockReturnValueOnce(oldIdentity.promise)
      .mockResolvedValueOnce(json(identity('01', '11'))));
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    cookie = '__Host-hikyo-csrf=B';
    await act(async () => oldIdentity.resolve(json(identity('00', '10'))));
    await settle();
    expect(text(container, 'owner')).toBe('Person 11');
  });

  it('does not restore A when peer replacement revalidation fails', async () => {
    vi.stubGlobal('BroadcastChannel', PeerChannel);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockRejectedValueOnce(new Error('offline')));
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    await settle();
    await act(async () => container.querySelector('button')?.click());
    const peer = new PeerChannel('hikyo-root-auth');
    await act(async () => peer.postMessage({ type: 'session-changed', sender: 'peer-tab' }));
    await settle();
    expect(text(container, 'owner')).toBe('anonymous');
    expect(text(container, 'disclosure')).toBe('');
    peer.close();
  });

  it.each([true, false])('only whoami confirms session loss after a proof 401 (live=%s)', async (live) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockResolvedValueOnce(json({}, 401))
      .mockResolvedValueOnce(live ? json(identity('00', '10')) : json({}, 401)));
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    await settle();
    await act(async () => container.querySelector('button')?.click());
    await act(async () => {
      await expect(parsed(stepUpTotpOp, { body: { code: '123456' } })).rejects.toMatchObject({ status: 401 });
    });
    await settle();
    expect(text(container, 'owner')).toBe(live ? 'Person 10' : 'anonymous');
    expect(text(container, 'disclosure')).toBe(live ? 'A display-once secret' : '');
  });
});

it.each([true, false])('validates cookie-rotating results before release (same owner=%s)', async (sameOwner) => {
  let cookie = '__Host-hikyo-csrf=A';
  vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
  const rotated = deferred<Response>();
  const verified = deferred<Response>();
  const fetchMock = vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
    .mockReturnValueOnce(rotated.promise).mockReturnValueOnce(verified.promise);
  vi.stubGlobal('fetch', fetchMock);
  const clients: QueryClient[] = [];
  const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={clients} /></AuthProvider>);
  await settle();
  await act(async () => container.querySelector('button')?.click());
  const result = parsed(stepUpTotpOp, { body: { code: '123456' } });
  const checked = sameOwner
    ? expect(result).resolves.toMatchObject({ principal: { display_name: 'Person 10' } })
    : expect(result).rejects.toBeInstanceOf(SessionChangedError);
  await settle();
  cookie = '__Host-hikyo-csrf=rotated';
  await act(async () => rotated.resolve(json(identity('00', '10'))));
  expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(true);
  // No request may use the changed cookie before identity is verified.
  await expect(parsed(listMyOrgsOp, {})).rejects.toBeInstanceOf(SessionChangedError);
  expect(fetchMock).toHaveBeenCalledTimes(3);
  await act(async () => verified.resolve(json(sameOwner ? identity('00', '10') : identity('01', '11'))));
  await checked;
  expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(false);
  expect(text(container, 'owner')).toBe(sameOwner ? 'Person 10' : 'Person 11');
  expect(text(container, 'disclosure')).toBe(sameOwner ? 'A display-once secret' : '');
});

it('keeps an anonymous login refusal local to its form', async () => {
  const fetchMock = vi.fn().mockResolvedValueOnce(json({}, 401)).mockResolvedValueOnce(json({}, 401));
  vi.stubGlobal('fetch', fetchMock);
  const clients: QueryClient[] = [];
  await renderAuth(<AuthProvider><DisclosureProbe clients={clients} /></AuthProvider>);
  await settle();
  const before = clients.at(-1);
  await act(async () => {
    await expect(parsed(localLoginOp, {
      body: { username: 'invalid', password: 'invalid', artifact: 'browser' },
    })).rejects.toMatchObject({ status: 401 });
  });
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(clients.at(-1)).toBe(before);
});

it('never restores unknown-owner disclosures when a superseding identity check fails', async () => {
  let cookie = '__Host-hikyo-csrf=A';
  vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
  const rotation = deferred<Response>();
  const verification = deferred<Response>();
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
    .mockReturnValueOnce(rotation.promise).mockReturnValueOnce(verification.promise)
    .mockRejectedValueOnce(new Error('offline')));
  const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
  await settle();
  await act(async () => container.querySelector('button')?.click());
  const result = parsed(stepUpTotpOp, { body: { code: '123456' } });
  const rejected = expect(result).rejects.toBeInstanceOf(SessionChangedError);
  await settle();
  cookie = '__Host-hikyo-csrf=B';
  await act(async () => rotation.resolve(json(identity('00', '10'))));
  expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(true);
  // An expiry/revalidation request supersedes the rotation check, then fails.
  await act(async () => container.querySelectorAll('button')[1]?.click());
  await settle();
  expect(text(container, 'owner')).toBe('anonymous');
  expect(text(container, 'disclosure')).toBe('');
  await act(async () => verification.resolve(json(identity('00', '10'))));
  await rejected;
  expect(text(container, 'owner')).toBe('anonymous');
});

function AccountRemintProbe({ completed }: { completed: (codes: readonly string[]) => void }) {
  const auth = useAuth();
  const client = useQueryClient();
  const [notice, setNotice] = useState('');
  const mutation = useMutation({
    mutationFn: () => parsed(regenerateRecoveryCodesOp, { body: { proof: 'proof' } }),
  });
  return <>
    <output data-testid="owner">{auth.identity?.principal.display_name ?? auth.state.status}</output>
    <output data-testid="notice">{notice}</output>
    <button onClick={() => {
      client.setQueryData(['account-before-remint'], 'old account answer');
      mutation.mutate(undefined, {
        onSuccess: (result) => {
          completed(result.recovery_codes);
          setNotice('Recovery codes shown once');
        },
      });
    }}>Rotate account session</button>
  </>;
}

it('keeps the initiating account-security callback through a verified session remint', async () => {
  let cookie = '__Host-hikyo-csrf=A';
  vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
  const mutationResponse = deferred<Response>();
  const verification = deferred<Response>();
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
    .mockReturnValueOnce(mutationResponse.promise).mockReturnValueOnce(verification.promise));
  const completed = vi.fn();
  const { container } = await renderAuth(<AuthProvider><AccountRemintProbe completed={completed} /></AuthProvider>);
  await settle();
  rememberWorkspace(workspace);
  await act(async () => container.querySelector('button')?.click());
  cookie = '__Host-hikyo-csrf=reminted';
  await act(async () => mutationResponse.resolve(json({
    login: identity('01', '10'), recovery_codes: ['display-once-code'],
  })));
  expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(true);
  expect(completed).not.toHaveBeenCalled();
  await act(async () => verification.resolve(json(identity('01', '10'))));
  await settleTask();
  expect(completed).toHaveBeenCalledExactlyOnceWith(['display-once-code']);
  expect(text(container, 'notice')).toBe('Recovery codes shown once');
  expect(text(container, 'owner')).toBe('Person 10');
  expect(workspaceBearer(workspace.origin)).toBeUndefined();
});

it.each([
  ['a different session for the same principal', '02', '10', '10'],
  ['the previous session for the same principal', '00', '10', '10'],
  ['a different principal', '01', '11', '10'],
  ['a matching returned identity belonging to another principal', '01', '11', '11'],
])('rejects account remint proof when whoami reports %s', async (_name, session, principal, returnedPrincipal) => {
  let cookie = '__Host-hikyo-csrf=A';
  vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
  const mutationResponse = deferred<Response>();
  const verification = deferred<Response>();
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
    .mockReturnValueOnce(mutationResponse.promise).mockReturnValueOnce(verification.promise));
  const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
  await settle();
  await act(async () => container.querySelector('button')?.click());
  const result = parsed(regenerateRecoveryCodesOp, { body: { proof: 'proof' } });
  const rejected = expect(result).rejects.toBeInstanceOf(SessionChangedError);
  await settle();
  cookie = '__Host-hikyo-csrf=reminted';
  await act(async () => mutationResponse.resolve(json({
    login: identity('01', returnedPrincipal), recovery_codes: ['display-once-code'],
  })));
  expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(true);
  await act(async () => verification.resolve(json(identity(session, principal))));
  await rejected;
  expect(text(container, 'disclosure')).toBe('');
});

it('does not treat an unrelated same-principal login as account-security continuity', async () => {
  let cookie = '__Host-hikyo-csrf=A';
  vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
  const login = deferred<Response>();
  const verification = deferred<Response>();
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
    .mockReturnValueOnce(login.promise).mockReturnValueOnce(verification.promise));
  const clients: QueryClient[] = [];
  const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={clients} /></AuthProvider>);
  await settle();
  await act(async () => container.querySelector('button')?.click());
  const previous = clients.at(-1);
  const result = parsed(localLoginOp, {
    body: { username: 'same-person', password: 'password', artifact: 'browser' },
  });
  const rejected = expect(result).rejects.toBeInstanceOf(SessionChangedError);
  await settle();
  cookie = '__Host-hikyo-csrf=new-login';
  await act(async () => login.resolve(json(identity('01', '10'))));
  await act(async () => verification.resolve(json(identity('01', '10'))));
  await rejected;
  expect(text(container, 'owner')).toBe('Person 10');
  expect(text(container, 'disclosure')).toBe('');
  expect(clients.at(-1)).not.toBe(previous);
});

it.each(['custom client', 'undocumented success status'])(
  'does not accept account-remint proof from a %s',
  async (source) => {
    let cookie = '__Host-hikyo-csrf=A';
    vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
    const mutationResponse = deferred<Response>();
    const verification = deferred<Response>();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(mutationResponse.promise).mockReturnValueOnce(verification.promise));
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    await settle();
    await act(async () => container.querySelector('button')?.click());
    const result = parsed(regenerateRecoveryCodesOp, {
      body: { proof: 'proof' },
      ...(source === 'custom client' ? {
        client: createClient({ baseUrl: 'https://peer.example', credentials: 'omit' }),
      } : {}),
    });
    const rejected = expect(result).rejects.toBeInstanceOf(SessionChangedError);
    await settle();
    cookie = '__Host-hikyo-csrf=reminted';
    await act(async () => mutationResponse.resolve(json({
      login: identity('01', '10'), recovery_codes: ['display-once-code'],
    }, source === 'custom client' ? 200 : 201)));
    await act(async () => verification.resolve(json(identity('01', '10'))));
    await rejected;
    expect(text(container, 'disclosure')).toBe('');
  },
);

it('validates account remint proof even when the companion cookie did not change', async () => {
  vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-hikyo-csrf=A');
  const mutationResponse = deferred<Response>();
  const verification = deferred<Response>();
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
    .mockReturnValueOnce(mutationResponse.promise).mockReturnValueOnce(verification.promise));
  const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
  await settle();
  await act(async () => container.querySelector('button')?.click());
  const result = parsed(regenerateRecoveryCodesOp, { body: { proof: 'proof' } });
  const rejected = expect(result).rejects.toBeInstanceOf(SessionChangedError);
  await settle();
  await act(async () => mutationResponse.resolve(json({
    login: identity('01', '10'), recovery_codes: ['display-once-code'],
  })));
  expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(true);
  await act(async () => verification.resolve(json(identity('00', '10'))));
  await rejected;
  expect(text(container, 'disclosure')).toBe('');
});

it('does not reuse another account response verification for a different remint proof', async () => {
  let cookie = '__Host-hikyo-csrf=A';
  vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
  const firstResponse = deferred<Response>();
  const secondResponse = deferred<Response>();
  const firstVerification = deferred<Response>();
  const secondVerification = deferred<Response>();
  const fetchMock = vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
    .mockReturnValueOnce(firstResponse.promise).mockReturnValueOnce(secondResponse.promise)
    .mockReturnValueOnce(firstVerification.promise).mockReturnValueOnce(secondVerification.promise);
  vi.stubGlobal('fetch', fetchMock);
  await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
  await settle();
  const first = parsed(regenerateRecoveryCodesOp, { body: { proof: 'proof' } });
  const second = parsed(regenerateRecoveryCodesOp, { body: { proof: 'proof' } });
  const accepted = expect(first).resolves.toMatchObject({ recovery_codes: ['first-code'] });
  const rejected = expect(second).rejects.toBeInstanceOf(SessionChangedError);
  await settle();
  cookie = '__Host-hikyo-csrf=reminted';
  await act(async () => firstResponse.resolve(json({
    login: identity('01', '10'), recovery_codes: ['first-code'],
  })));
  await act(async () => secondResponse.resolve(json({
    login: identity('02', '10'), recovery_codes: ['second-code'],
  })));
  await act(async () => firstVerification.resolve(json(identity('01', '10'))));
  await accepted;
  expect(fetchMock).toHaveBeenCalledTimes(5);
  await act(async () => secondVerification.resolve(json(identity('01', '10'))));
  await rejected;
});


it.each(['parsed', 'parsedPick'])(
  '%s reconciles a changed cookie before refusing malformed account rotation proof',
  async (parser) => {
    let cookie = '__Host-hikyo-csrf=A';
    vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
    const mutationResponse = deferred<Response>();
    const verification = deferred<Response>();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(mutationResponse.promise).mockReturnValueOnce(verification.promise));
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    await settle();
    await act(async () => container.querySelector('button')?.click());
    const options = { body: { proof: 'proof' } };
    const result = parser === 'parsed'
      ? parsed(regenerateRecoveryCodesOp, options)
      : parsedPick(regenerateRecoveryCodesOp, options, { login: true, recovery_codes: true });
    const rejected = expect(result).rejects.toBeInstanceOf(SessionChangedError);
    await settle();
    cookie = '__Host-hikyo-csrf=B';
    await act(async () => mutationResponse.resolve(json({ login: {}, recovery_codes: [] })));
    expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(true);
    await act(async () => verification.resolve(json(identity('01', '11'))));
    await rejected;
    expect(text(container, 'owner')).toBe('Person 11');
    expect(text(container, 'disclosure')).toBe('');
  },
);

it.each(['parsed', 'parsedPick'])(
  '%s still refuses malformed account data after proving the same cookie owner',
  async (parser) => {
    let cookie = '__Host-hikyo-csrf=A';
    vi.spyOn(document, 'cookie', 'get').mockImplementation(() => cookie);
    const mutationResponse = deferred<Response>();
    const verification = deferred<Response>();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(json(identity('00', '10')))
      .mockReturnValueOnce(mutationResponse.promise).mockReturnValueOnce(verification.promise));
    const { container } = await renderAuth(<AuthProvider><DisclosureProbe clients={[]} /></AuthProvider>);
    await settle();
    const options = { body: { proof: 'proof' } };
    const result = parser === 'parsed'
      ? parsed(regenerateRecoveryCodesOp, options)
      : parsedPick(regenerateRecoveryCodesOp, options, { login: true, recovery_codes: true });
    const rejected = expect(result).rejects.toMatchObject({ name: 'ZodError' });
    await settle();
    cookie = '__Host-hikyo-csrf=rotated';
    await act(async () => mutationResponse.resolve(json({ login: {}, recovery_codes: [] })));
    expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(true);
    await act(async () => verification.resolve(json(identity('00', '10'))));
    await rejected;
    expect(text(container, 'owner')).toBe('Person 10');
    expect(container.querySelector('.session-owner')?.hasAttribute('hidden')).toBe(false);
  },
);

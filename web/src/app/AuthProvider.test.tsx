// @vitest-environment happy-dom
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { act, useState, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  forgetWorkspace,
  rememberWorkspace,
  workspaceBearer,
  type WorkspaceBearer,
} from '../api/workspace.ts';
import { renderForm, settle, settleTask } from '../testkit/renderForm.tsx';
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

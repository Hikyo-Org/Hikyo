// @vitest-environment happy-dom
import type { WorkspaceHandoffTransaction } from '@hikyo/client';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { AuthProvider, useAuth, type WhoAmI } from '../app/AuthProvider.tsx';
import { WorkspaceApprove } from './WorkspaceApprove.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const fetcher = vi.fn<typeof fetch>();
let root: Root | undefined;
const id = (prefix: string, n: number) =>
  `${prefix}_00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
function identity(n: number): WhoAmI {
  return {
    session: {
      id: id('ses', n),
      artifact: 'browser',
      created_at: '2026-09-05T12:00:00Z',
      idle_expires_at: '2099-09-05T12:00:00Z',
      absolute_expires_at: '2099-09-05T12:00:00Z',
      assurance: {
        method: 'local-password',
        factors: ['password'],
        authenticated_at: '2026-09-05T12:00:00Z',
      },
    },
    principal: { id: id('prn', n), kind: 'human', display_name: `Person ${n}` },
    capabilities: { instance_operator: false },
  };
}
function SwitchSession() {
  const auth = useAuth();
  return (
    <button onClick={() => auth.acceptSession(identity(2), auth.captureTransition())}>
      Switch session
    </button>
  );
}
function response(body: object) {
  return new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
}
function summary(): WorkspaceHandoffTransaction {
  return {
    state: 'opaque-state',
    purpose: 'establishment',
    requesting_origin: 'https://trusted.example',
    expires_at: '2099-09-05T12:00:00Z',
    key_ids: [],
  };
}
function deferred<T>() {
  let resolve: (value: T) => void = () => {
    throw new Error('deferred not initialized');
  };
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}
function button(text: string) {
  const found = [...document.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === text,
  );
  if (found === undefined) throw new Error(`missing ${text}`);
  return found;
}
beforeEach(() => {
  fetcher.mockReset();
  vi.stubGlobal('fetch', fetcher);
  globalThis.history.replaceState({}, '', '/workspace/approve?state=opaque-state');
});
afterEach(async () => {
  if (root !== undefined) await act(async () => root?.unmount());
  root = undefined;
  document.body.replaceChildren();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('consent lifetime under the real AuthProvider', () => {
  it('allows a current completion and redirects only to the stored callback', async () => {
    fetcher.mockImplementation(async (input) => {
      if (!(input instanceof Request)) throw new Error('expected SDK request');
      if (input.url.includes('/whoami')) return response(identity(1));
      return response(
        input.method === 'POST'
          ? { redirect_uri: 'https://trusted.example/workspace/callback', code: 'server-code' }
          : summary(),
      );
    });
    const redirect = vi.spyOn(globalThis.location, 'assign').mockImplementation(() => {});
    const container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
    await act(async () =>
      root?.render(
        <AuthProvider>
          <WorkspaceApprove />
        </AuthProvider>,
      ),
    );
    await settle();
    await settle();
    await act(async () => button('Authorize').click());
    await settle();
    expect(redirect).toHaveBeenCalledExactlyOnceWith(
      'https://trusted.example/workspace/callback?code=server-code&state=opaque-state',
    );
  });
  it.each([
    ['summary', 'unmount'],
    ['summary', 'session'],
    ['approval', 'unmount'],
    ['approval', 'session'],
  ])('retires a deferred %s on %s before further side effects', async (stage, retirement) => {
    const pending = deferred<Response>();
    let reads = 0;
    let posts = 0;
    fetcher.mockImplementation(async (input) => {
      if (!(input instanceof Request)) throw new Error('expected SDK request');
      if (input.url.includes('/whoami')) return response(identity(1));
      if (input.method === 'POST') {
        posts++;
        return pending.promise;
      }
      reads++;
      if (stage === 'summary' && reads === 2) return pending.promise;
      return response(summary());
    });
    const redirect = vi.spyOn(globalThis.location, 'assign').mockImplementation(() => {});
    const container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
    await act(async () =>
      root?.render(
        <AuthProvider>
          <SwitchSession />
          <WorkspaceApprove />
        </AuthProvider>,
      ),
    );
    await settle();
    await settle();
    await act(async () => button('Authorize').click());
    await settle();
    expect(reads).toBe(2);
    expect(posts).toBe(stage === 'approval' ? 1 : 0);
    if (retirement === 'unmount') {
      await act(async () => root?.unmount());
      root = undefined;
    } else {
      await act(async () => button('Switch session').click());
      await settle();
    }
    await act(async () =>
      pending.resolve(
        response(
          stage === 'summary'
            ? summary()
            : { redirect_uri: 'https://trusted.example/workspace/callback', code: 'server-code' },
        ),
      ),
    );
    await settle();
    expect(posts).toBe(stage === 'approval' ? 1 : 0);
    expect(redirect).not.toHaveBeenCalled();
  });
});

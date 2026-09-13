// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { act, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle } from '../testkit/renderForm.tsx';
import { RuntimeMaintenanceBoundary } from './RuntimeMaintenanceBoundary.tsx';
import { AuthProvider, useAuth } from './AuthProvider.tsx';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';

const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
let cleanup: (() => Promise<void>) | undefined;

beforeEach(() => vi.useFakeTimers());
afterEach(async () => {
  await cleanup?.();
  cleanup = undefined;
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function mount(failure: Error | null = null) {
  const refresh = vi.fn(async () => {});
  const queries = new QueryClient();
  const result = await renderForm(<RuntimeMaintenanceBoundary failure={failure} refreshSession={refresh} queries={queries}>
    <input aria-label="Draft" defaultValue="Keep this draft" />
  </RuntimeMaintenanceBoundary>);
  cleanup = result.unmount;
  await settle();
  return { ...result, refresh };
}

async function advance(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
  await settle();
}

function SessionProbe() {
  const auth = useAuth();
  return <p>Session: {auth.failure === null ? auth.state.status : 'unavailable'}</p>;
}

function SecretProbe() {
  const [secret, setSecret] = useSensitiveState('');
  return <><button onClick={() => setSecret('temporary disclosure')}>Reveal</button><p>{secret}</p></>;
}

describe('runtime interruption', () => {
  it('keeps a transient identity outage fenced until runtime recovery revalidates the cache', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ state: 'ready', phase: null })));
    let changeFailure: (failure: Error | null) => void = () => { throw new Error('not mounted'); };
    let completeRecovery: () => void = () => { throw new Error('recovery not started'); };
    const refresh = vi.fn(() => new Promise<void>((resolve) => { completeRecovery = resolve; }));
    const queries = new QueryClient();
    function Probe() {
      const [failure, setFailure] = useState<Error | null>(null);
      changeFailure = setFailure;
      return <RuntimeMaintenanceBoundary failure={failure} refreshSession={refresh} queries={queries}>
        <input defaultValue="Preserved draft" />
      </RuntimeMaintenanceBoundary>;
    }
    const result = await renderForm(<Probe />);
    cleanup = result.unmount;
    await settle();
    const draft = result.container.querySelector('input');
    await act(async () => { changeFailure(new Error('Identity unavailable')); });
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    // The auth provider's own retry can succeed between runtime polls.
    await act(async () => { changeFailure(null); });
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await advance(15_000);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await act(async () => { completeRecovery(); });
    await settle();
    expect(result.container.querySelector('dialog')).toBeNull();
    expect(result.container.querySelector('input')).toBe(draft);
  });

  it('keeps editing fenced through pending and failed cache revalidation after whoami succeeds', async () => {
    let statusReads = 0;
    let identityReads = 0;
    let rejectQuery: (error: Error) => void = () => { throw new Error('no pending query'); };
    const read = vi.fn<() => Promise<string>>()
      .mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectQuery = reject; }))
      .mockResolvedValue('Fresh permission');
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request) => {
      const url = input instanceof Request ? input.url : String(input);
      if (url.endsWith('/runtime/status')) {
        statusReads += 1;
        return json(statusReads === 1 ? { state: 'maintenance', phase: 'migration' } : { state: 'ready', phase: null });
      }
      identityReads += 1;
      return json(authenticatedIdentity);
    }));
    function Probe() {
      const query = useQuery({ queryKey: ['permission'], queryFn: read, initialData: 'Cached permission', staleTime: Infinity });
      return <><SessionProbe /><input aria-label="Draft" defaultValue="Preserved draft" /><p>{query.data}</p></>;
    }
    const result = await renderForm(<AuthProvider monitorRuntime><Probe /></AuthProvider>);
    cleanup = result.unmount;
    await settle();
    const draft = result.container.querySelector('input');
    await advance(2_000);
    expect(identityReads).toBe(2);
    expect(read).toHaveBeenCalledTimes(1);
    expect(result.container.textContent).toContain('Session: authenticated');
    expect(result.container.textContent).toContain('Cached permission');
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await act(async () => { rejectQuery(new Error('Permission service unavailable')); });
    await settle();
    expect(result.container.textContent).toContain('Reconnecting');
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await advance(1_000);
    expect(read).toHaveBeenCalledTimes(2);
    await advance(1);
    expect(result.container.textContent).toContain('Fresh permission');
    expect(result.container.querySelector('dialog')).toBeNull();
    expect(result.container.querySelector('input')).toBe(draft);
    expect(draft?.value).toBe('Preserved draft');
  });

  it('cancels stalled cache recovery at the deadline and ignores its late answer', async () => {
    let statusReads = 0;
    let queryReads = 0;
    let querySignal: AbortSignal | undefined;
    let resolveLate: (value: string) => void = () => { throw new Error('no pending query'); };
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request) => {
      const url = input instanceof Request ? input.url : String(input);
      if (url.endsWith('/runtime/status')) {
        statusReads += 1;
        return json(statusReads === 1 ? { state: 'maintenance', phase: 'migration' } : { state: 'ready', phase: null });
      }
      return json(authenticatedIdentity);
    }));
    function Probe() {
      const query = useQuery({ queryKey: ['permission'], initialData: 'Cached permission', staleTime: Infinity,
        queryFn: ({ signal }) => {
          queryReads += 1;
          if (queryReads > 1) return Promise.resolve('Fresh permission');
          querySignal = signal;
          return new Promise<string>((resolve) => { resolveLate = resolve; });
        },
      });
      return <p>{query.data}</p>;
    }
    const result = await renderForm(<AuthProvider monitorRuntime><Probe /></AuthProvider>);
    cleanup = result.unmount;
    await settle();
    await advance(2_000);
    expect(querySignal?.aborted).toBe(false);
    await advance(8_000);
    expect(querySignal?.aborted).toBe(true);
    expect(result.container.textContent).toContain('Reconnecting');
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await act(async () => { resolveLate('Late permission'); });
    await settle();
    expect(result.container.textContent).not.toContain('Late permission');
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await advance(1_000);
    expect(queryReads).toBe(2);
    await advance(1);
    expect(result.container.textContent).toContain('Fresh permission');
    expect(result.container.querySelector('dialog')).toBeNull();
  });

  it('keeps editing fenced when another pending session check supersedes runtime recovery', async () => {
    let statusReads = 0;
    let identityReads = 0;
    const pending: Array<(response: Response) => void> = [];
    let supersede: () => Promise<void> = async () => { throw new Error('auth not mounted'); };
    vi.stubGlobal('fetch', vi.fn((input: string | URL | Request) => {
      const url = input instanceof Request ? input.url : String(input);
      if (url.endsWith('/runtime/status')) {
        statusReads += 1;
        return Promise.resolve(json(statusReads === 1 ? { state: 'maintenance', phase: 'health-check' } : { state: 'ready', phase: null }));
      }
      identityReads += 1;
      if (identityReads === 2 || identityReads === 3) return new Promise<Response>((resolve) => { pending.push(resolve); });
      return Promise.resolve(json({ error: 'unauthorized' }, 401));
    }));
    function Probe() {
      const auth = useAuth();
      supersede = auth.refreshSession;
      return <SessionProbe />;
    }
    const result = await renderForm(<AuthProvider monitorRuntime><Probe /></AuthProvider>);
    cleanup = result.unmount;
    await settle();
    await advance(2_000);
    await act(async () => { void supersede(); });
    await settle();
    expect(identityReads).toBe(3);
    const [runtimeResponse, currentResponse] = pending;
    if (runtimeResponse === undefined || currentResponse === undefined) throw new Error('expected competing session checks');
    await act(async () => { runtimeResponse(json(authenticatedIdentity)); });
    await settle();
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    expect(result.container.textContent).toContain('Reconnecting');
    expect(result.container.textContent).toContain('Session: anonymous');
    await act(async () => { currentResponse(json({ error: 'unauthorized' }, 401)); });
    await settle();
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await advance(1_000);
    expect(identityReads).toBe(4);
    expect(result.container.querySelector('dialog')).toBeNull();
  });

  it('aborts a hung whoami at the recovery deadline and continues polling before unblocking', async () => {
    let statusReads = 0;
    let identityReads = 0;
    let identitySignal: AbortSignal | null | undefined;
    let resolveLate: (response: Response) => void = () => { throw new Error('no pending identity request'); };
    vi.stubGlobal('fetch', vi.fn((input: string | URL | Request, init?: RequestInit) => {
      const url = input instanceof Request ? input.url : String(input);
      if (url.endsWith('/runtime/status')) {
        statusReads += 1;
        return Promise.resolve(json(statusReads === 1
          ? { state: 'maintenance', phase: 'migration' }
          : { state: 'ready', phase: null }));
      }
      identityReads += 1;
      if (identityReads === 1) return Promise.resolve(new Response('', { status: 503 }));
      if (identityReads !== 2) return Promise.resolve(json({ error: 'unauthorized' }, 401));
      identitySignal = input instanceof Request ? input.signal : init?.signal;
      const signal = identitySignal;
      if (signal == null) throw new Error('recovery whoami must be abortable');
      return new Promise<Response>((resolve, reject) => {
        resolveLate = resolve;
        signal.addEventListener('abort', () => reject(signal.reason), { once: true });
      });
    }));
    const result = await renderForm(<AuthProvider monitorRuntime><SessionProbe /></AuthProvider>);
    cleanup = result.unmount;
    await settle();
    await advance(2_000);
    expect(identityReads).toBe(2);
    expect(identitySignal?.aborted).toBe(false);
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await advance(8_000);
    expect(identitySignal?.aborted).toBe(true);
    expect(result.container.textContent).toContain('Reconnecting');
    expect(result.container.querySelector('[inert]')).not.toBeNull();
    await advance(1_000);
    expect(statusReads).toBe(3);
    expect(identityReads).toBe(3);
    expect(result.container.textContent).toContain('Session: anonymous');
    expect(result.container.querySelector('dialog')).toBeNull();
    await act(async () => { resolveLate(json(authenticatedIdentity)); });
    await settle();
    expect(result.container.textContent).toContain('Session: anonymous');
  });

  it('cannot settle identity from a response decoded after cancellation', async () => {
    const controller = new AbortController();
    let calls = 0;
    let resolveLate: (response: Response) => void = () => { throw new Error('no pending identity request'); };
    vi.stubGlobal('fetch', vi.fn(() => {
      calls += 1;
      if (calls === 1) return Promise.resolve(json({ error: 'unauthorized' }, 401));
      // Models a response already handed to the SDK when abort arrives.
      return new Promise<Response>((resolve) => { resolveLate = resolve; });
    }));
    const rejected = vi.fn();
    function Probe() {
      const auth = useAuth();
      return <><SessionProbe /><button onClick={() => { void auth.refreshSession(controller.signal).catch(rejected); }}>Refresh</button></>;
    }
    const result = await renderForm(<AuthProvider><Probe /></AuthProvider>);
    cleanup = result.unmount;
    await settle();
    await act(async () => { result.container.querySelector('button')?.click(); });
    await settle();
    controller.abort();
    await act(async () => { resolveLate(json(authenticatedIdentity)); });
    await settle();
    expect(rejected).toHaveBeenCalledTimes(1);
    expect(result.container.textContent).toContain('Session: anonymous');
  });

  it('recovers failed initial authentication in the monitored production composition', async () => {
    let ready = false;
    const fetcher = vi.fn(async (input: string | URL | Request) => {
      const url = input instanceof Request ? input.url : String(input);
      return url.endsWith('/runtime/status')
        ? json(ready ? { state: 'ready', phase: null } : { state: 'maintenance', phase: 'migration' }, ready ? 200 : 503)
        : ready ? json({ error: 'unauthorized' }, 401) : new Response('', { status: 503 });
    });
    vi.stubGlobal('fetch', fetcher);
    const result = await renderForm(<AuthProvider monitorRuntime><SessionProbe /></AuthProvider>);
    cleanup = result.unmount;
    await settle();
    expect(result.container.textContent).toContain('Hikyo is upgrading');
    ready = true;
    await advance(2_000);
    expect(result.container.textContent).toContain('Session: anonymous');
    expect(result.container.querySelector('dialog')).toBeNull();
  });

  it('retires secret disclosures while keeping the component mounted', async () => {
    let maintenance = false;
    vi.stubGlobal('fetch', vi.fn(async () => json(maintenance ? { state: 'maintenance', phase: 'preparing' } : { state: 'ready', phase: null })));
    const queries = new QueryClient();
    const result = await renderForm(<QueryClientProvider client={queries}>
      <RuntimeMaintenanceBoundary failure={null} refreshSession={async () => {}} queries={queries}><SecretProbe /></RuntimeMaintenanceBoundary>
    </QueryClientProvider>);
    cleanup = result.unmount;
    const button = result.container.querySelector('button');
    await act(async () => { button?.click(); });
    expect(result.container.textContent).toContain('temporary disclosure');
    maintenance = true;
    await advance(15_000);
    expect(result.container.textContent).not.toContain('temporary disclosure');
    expect(result.container.querySelector('button')).toBe(button);
  });
  it('blocks editing for confirmed maintenance and preserves drafts through automatic recovery', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(json({ state: 'maintenance', phase: 'backup' }, 503))
      .mockResolvedValue(json({ state: 'ready', phase: null }));
    vi.stubGlobal('fetch', fetcher);
    const { container, refresh } = await mount();
    const input = container.querySelector('input');
    expect(container.querySelector('[inert]')).not.toBeNull();
    expect(container.textContent).toContain('Creating a recovery backup');
    expect(container.querySelector('dialog')?.open).toBe(true);
    expect(document.activeElement?.id).toBe('runtime-maintenance-title');
    const cancel = new Event('cancel', { cancelable: true });
    container.querySelector('dialog')?.dispatchEvent(cancel);
    expect(cancel.defaultPrevented).toBe(true);
    await advance(2_000);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(container.querySelector('dialog')).toBeNull();
    expect(container.querySelector('[inert]')).toBeNull();
    expect(container.querySelector('input')).toBe(input);
    expect(input?.value).toBe('Keep this draft');
    expect(fetcher.mock.calls[0]).toEqual(['/api/v1/runtime/status', expect.objectContaining({ credentials: 'omit', cache: 'no-store', redirect: 'error' })]);
  });

  it('calls a proxy 503 reconnecting, backs off, and never invents an upgrade phase', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response('<html>Unavailable</html>', { status: 503 }));
    vi.stubGlobal('fetch', fetcher);
    const { container } = await mount();
    expect(container.textContent).toContain('Reconnecting to Hikyo');
    expect(container.textContent).not.toContain('Hikyo is upgrading');
    await advance(1_000);
    expect(fetcher).toHaveBeenCalledTimes(2);
    await advance(1_999);
    expect(fetcher).toHaveBeenCalledTimes(2);
    await advance(1);
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it('accepts an older server without the endpoint and retries failed initial authentication', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 404 })));
    const { container, refresh } = await mount(new Error('initial authentication unavailable'));
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(container.textContent).toContain('Reconnecting to Hikyo');
    await advance(2_000);
    expect(refresh).toHaveBeenCalledTimes(2);
  });

  it('leaves a healthy older server interactive', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 404 })));
    const { container, refresh } = await mount();
    expect(container.querySelector('dialog')).toBeNull();
    expect(refresh).not.toHaveBeenCalled();
  });

  it('explains required recovery without promising automatic repair', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ state: 'recovery-required', phase: null })));
    const { container } = await mount();
    expect(container.textContent).toContain('Recovery needs an operator');
    expect(container.textContent).toContain('An operator must recover it');
    expect(container.querySelector('[aria-live="polite"]')).not.toBeNull();
  });

  it('refuses unknown maintenance phases instead of displaying untrusted text', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ state: 'maintenance', phase: 'secret-path' })));
    const { container } = await mount();
    expect(container.textContent).toContain('Reconnecting');
    expect(container.textContent).not.toContain('secret-path');
  });

  it('pauses hidden-tab polling and wakes on visibility without overlapping requests', async () => {
    const fetcher = vi.fn().mockResolvedValue(json({ state: 'ready', phase: null }));
    vi.stubGlobal('fetch', fetcher);
    await mount();
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    document.dispatchEvent(new Event('visibilitychange'));
    await advance(30_000);
    expect(fetcher).toHaveBeenCalledTimes(1);
    visibility.mockReturnValue('visible');
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    await settle();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it('aborts an in-flight probe and removes polling on unmount', async () => {
    let signal: AbortSignal | null | undefined;
    const fetcher = vi.fn((_url: string, init: RequestInit) => {
      signal = init.signal;
      return new Promise<Response>(() => {});
    });
    vi.stubGlobal('fetch', fetcher);
    const result = await mount();
    await result.unmount();
    cleanup = undefined;
    expect(signal?.aborted).toBe(true);
    globalThis.dispatchEvent(new Event('online'));
    await advance(60_000);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});

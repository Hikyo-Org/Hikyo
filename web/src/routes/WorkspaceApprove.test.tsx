// @vitest-environment happy-dom
import type { WorkspaceHandoffTransaction } from '@hikyo/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { WorkspaceApprove } from './WorkspaceApprove.tsx';

const ceremonies = vi.hoisted(() => ({
  passkey: vi.fn<() => Promise<void>>(),
  totp: vi.fn<() => Promise<void>>(),
  revision: 0,
}));
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({
    state: { status: 'authenticated', sessionEpoch: 'session-1' },
    captureTransition: () => ({ revision: ceremonies.revision }),
    identity: { principal: { display_name: 'Alice', id: 'alice' } },
  }),
}));
vi.mock('../api/values.ts', async (original) => ({
  ...(await original<typeof import('../api/values.ts')>()),
  runPasskeyCeremony: ceremonies.passkey,
  runTOTPCeremony: ceremonies.totp,
}));
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const roots: Root[] = [];
const fetcher = vi.fn<typeof fetch>();
const id = (prefix: string, n: number) =>
  `${prefix}_00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;
const fresh = (origin = 'https://trusted.example'): WorkspaceHandoffTransaction => ({
  state: 'opaque-state',
  purpose: 'establishment',
  requesting_origin: origin,
  key_ids: [],
  expires_at: new Date(Date.now() + 60_000).toISOString(),
});
function response(body: object, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  fetcher.mockReset();
  ceremonies.passkey.mockReset();
  ceremonies.totp.mockReset();
  ceremonies.revision = 0;
  vi.stubGlobal('fetch', fetcher);
  globalThis.history.replaceState(
    {},
    '',
    '/workspace/approve?state=opaque-state&origin=https://attacker.example&redirect_uri=https://attacker.example/cb',
  );
});
afterEach(async () => {
  for (const root of roots.splice(0)) await act(async () => root.unmount());
  document.body.replaceChildren();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
async function render() {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  roots.push(root);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <WorkspaceApprove />
      </QueryClientProvider>,
    ),
  );
  await settle();
  return container;
}
async function settle() {
  await act(async () => {
    await Promise.resolve();
    if (vi.isFakeTimers()) {
      await vi.advanceTimersByTimeAsync(0);
    } else {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
  });
}
function button(container: HTMLElement, text: string) {
  const found = [...container.querySelectorAll('button')].find((b) => b.textContent === text);
  if (found === undefined) throw new Error(`missing button ${text}`);
  return found;
}
function methods() {
  return fetcher.mock.calls.map(([input]) => {
    if (!(input instanceof Request)) throw new Error('expected SDK Request');
    return input.method;
  });
}

describe('workspace consent summary', () => {
  it.each([
    'https://first.example:8443',
    'https://second.example',
    'https://xn--bcher-kva.example',
  ])('shows the exact server origin %s and ignores URL consent fields', async (origin) => {
    fetcher.mockResolvedValue(response(fresh(origin)));
    const container = await render();
    expect(container.querySelector('.workspace-consent__origin')?.textContent).toContain(origin);
    expect(container.textContent).not.toContain('attacker.example');
    expect(button(container, 'Authorize').disabled).toBe(false);
    expect(methods()).toEqual(['GET']);
  });
  it('keeps authorization unavailable while the summary is pending', async () => {
    fetcher.mockReturnValue(new Promise<Response>(() => {}));
    const container = await render();
    expect(container.textContent).toContain('Loading');
    expect(container.querySelector('button')).toBeNull();
  });
  it('never enables an expired summary', async () => {
    fetcher.mockResolvedValue(response({ ...fresh(), expires_at: '2020-01-01T00:00:00Z' }));
    const container = await render();
    expect(container.textContent).toContain('Authorization could not be completed');
    expect(container.querySelector('button')).toBeNull();
  });
  it('refuses a response for a different opaque state', async () => {
    fetcher.mockResolvedValue(response({ ...fresh(), state: 'other-state' }));
    const container = await render();
    expect(container.textContent).toContain('Authorization could not be completed');
    expect(container.querySelector('button')).toBeNull();
  });
  it('refuses a malformed summary', async () => {
    fetcher.mockResolvedValue(response({ purpose: 'establishment' }));
    const container = await render();
    expect(container.textContent).toContain('Authorization could not be completed');
    expect(container.querySelector('button')).toBeNull();
  });
  it('rechecks liveness and refuses a consumed summary without posting', async () => {
    fetcher.mockResolvedValueOnce(response(fresh())).mockResolvedValueOnce(response({}, 403));
    const container = await render();
    await act(async () => button(container, 'Authorize').click());
    await settle();
    expect(container.textContent).toContain('Authorization could not be completed');
    expect(methods()).toEqual(['GET', 'GET']);
  });
  it('refuses a changed origin without posting', async () => {
    fetcher
      .mockResolvedValueOnce(response(fresh()))
      .mockResolvedValueOnce(response(fresh('https://other.example')));
    const container = await render();
    await act(async () => button(container, 'Authorize').click());
    await settle();
    expect(container.textContent).toContain('Authorization could not be completed');
    expect(methods()).toEqual(['GET', 'GET']);
  });
  it('renders every bound key and environment in a keyboard-scrollable scope', async () => {
    const keys = Array.from({ length: 200 }, (_, n) => id('key', n));
    fetcher.mockResolvedValue(
      response({
        ...fresh(),
        purpose: 'step-up',
        operation: 'reveal',
        environment: id('env', 1),
        key_ids: keys,
      }),
    );
    const container = await render();
    const scope = container.querySelector('.workspace-consent__scope');
    expect(scope?.getAttribute('tabindex')).toBe('0');
    expect(scope?.textContent).toContain(id('env', 1));
    expect(scope?.querySelectorAll('li')).toHaveLength(200);
    expect(scope?.textContent).toContain(keys[199]);
  });
  it('expires visible consent and ignores a late reauthentication completion', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-05T12:00:00Z'));
    let complete: (() => void) | undefined;
    const pending = new Promise<void>((resolve) => {
      complete = resolve;
    });
    ceremonies.passkey.mockReturnValue(pending);
    fetcher.mockResolvedValue(
      response({
        ...fresh(),
        expires_at: new Date(Date.now() + 5_000).toISOString(),
        purpose: 'step-up',
        operation: 'reveal',
        environment: id('env', 1),
        key_ids: [id('key', 1)],
      }),
    );
    const container = await render();
    await act(async () => button(container, 'Use a passkey').click());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_001);
    });
    expect(container.textContent).toContain('Authorization could not be completed');
    if (complete === undefined) throw new Error('missing ceremony completion');
    await act(async () => complete?.());
    await settle();
    expect(methods()).toEqual(['GET']);
  });
  it.each(['reveal', 'copy', 'publish', 'approve', 'reject', 'bypass'])(
    'signs exactly the displayed %s operation',
    async (operation) => {
      ceremonies.passkey.mockRejectedValue(new Error('synthetic refusal'));
      fetcher.mockResolvedValue(
        response({
          ...fresh(),
          purpose: 'step-up',
          operation,
          environment: id('env', 1),
          key_ids: [id('key', 1)],
        }),
      );
      const container = await render();
      await act(async () => button(container, 'Use a passkey').click());
      await settle();
      expect(ceremonies.passkey).toHaveBeenCalledExactlyOnceWith({
        operation,
        environmentId: id('env', 1),
        keyIds: [id('key', 1)],
      });
      expect(methods()).toEqual(['GET']);
    },
  );
  it('ignores a late passkey completion after the session revision changes', async () => {
    let complete: (() => void) | undefined;
    ceremonies.passkey.mockReturnValue(
      new Promise<void>((resolve) => {
        complete = resolve;
      }),
    );
    fetcher.mockResolvedValue(
      response({
        ...fresh(),
        purpose: 'step-up',
        operation: 'approve',
        environment: id('env', 1),
        key_ids: [],
      }),
    );
    const container = await render();
    await act(async () => button(container, 'Use a passkey').click());
    ceremonies.revision++;
    await act(async () => complete?.());
    await settle();
    expect(methods()).toEqual(['GET']);
  });
  it.each(['success', 'refusal'])('clears a submitted authenticator code on %s', async (result) => {
    let finish: (() => void) | undefined;
    ceremonies.totp.mockReturnValue(
      new Promise<void>((resolve, reject) => {
        finish = () => (result === 'success' ? resolve() : reject(new Error('synthetic refusal')));
      }),
    );
    fetcher
      .mockResolvedValueOnce(
        response({
          ...fresh(),
          purpose: 'step-up',
          operation: 'reveal',
          environment: id('env', 1),
          key_ids: [],
        }),
      )
      .mockResolvedValue(response({}, 403));
    const container = await render();
    const input = container.querySelector('input');
    if (input === null) throw new Error('missing authenticator input');
    const nativeSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
    if (nativeSetter === undefined) throw new Error('missing input setter');
    await act(async () => {
      nativeSetter.call(input, '123456');
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await act(async () =>
      container
        .querySelector('form')
        ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    );
    expect(input.value).toBe('');
    expect(input.disabled).toBe(true);
    await act(async () => finish?.());
    await settle();
    expect(container.querySelector('input')?.value ?? '').toBe('');
    expect(ceremonies.totp).toHaveBeenCalledExactlyOnceWith(id('env', 1), '123456');
  });
});

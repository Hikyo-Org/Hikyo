// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { useEnrolTotpStart, useRegenerateRecoveryCodes, useRemoveTotp } from './account.ts';
import { useSetAdapterCredential } from './adapters.ts';
import { useImportValues, useStageMatrixValue } from './matrix.ts';
import { useAddRemote } from './remotes.ts';
import { useLogin } from './session.ts';
import { retireSensitiveOperations, transferSensitiveState, useSensitiveMutation, useSensitiveState, type SensitiveStateTransfer } from './sensitiveMutation.ts';
import { useStepUpTotp } from './stepup.ts';
import { useRevealAll, useRevealOne, useSetValue } from './values.ts';

const mocks = vi.hoisted(() => ({ request: vi.fn(), accept: vi.fn(), refresh: vi.fn() }));
vi.mock('./client.ts', async (importActual) => ({
  ...(await importActual<typeof import('./client.ts')>()), parsed: mocks.request, ok: mocks.request,
}));
vi.mock('../app/AuthProvider.tsx', () => ({ useAuth: () => ({
  captureTransition: () => ({ revision: 1 }), acceptSession: mocks.accept, refreshSession: mocks.refresh,
}) }));

beforeEach(() => { vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.clearAllMocks(); });
afterEach(() => { vi.unstubAllGlobals(); });
function deferred<Value>() {
  let resolve: (value: Value) => void = () => { throw new Error('Promise not initialized'); };
  let reject: (error: Error) => void = () => { throw new Error('Promise not initialized'); };
  const promise = new Promise<Value>((accept, refuse) => { resolve = accept; reject = refuse; });
  return { promise, resolve, reject };
}
const secret = 'SENTINEL-private-secret-material';
const env = { org: 'org', project: 'project', environment: 'environment' };

it.each(['success', 'reset', 'unmount', 'retirement', 'extra-retirement', 'wrong-client', 'wrong-session', 'wrong-principal'])
('bounds a single-use state-owner transfer at %s', async (boundary) => {
  const client = new QueryClient();
  const other = new QueryClient();
  const container = document.createElement('div');
  const root = createRoot(container);
  const target = { sessionId: 'session-new', principalId: 'principal-current' };
  let transfer: SensitiveStateTransfer | undefined;
  function Surface() {
    const [value, setValue, prepare] = useSensitiveState('');
    const [unrelated, setUnrelated] = useSensitiveState('');
    return <><button onClick={() => { setUnrelated('old-secret'); transfer = prepare(secret, target); }}>Prepare</button>
      <button onClick={() => setValue('')}>Dismiss</button><output>{value}</output><aside>{unrelated}</aside></>;
  }
  try {
    await act(async () => root.render(<QueryClientProvider client={client}><Surface /></QueryClientProvider>));
    await act(async () => container.querySelector('button')?.click());
    if (boundary === 'reset') await act(async () => container.querySelectorAll('button')[1]?.click());
    if (boundary === 'unmount') await act(async () => root.unmount());
    if (boundary === 'retirement') await act(async () => retireSensitiveOperations(client));
    const capability = transfer;
    if (capability === undefined) throw new Error('transfer was not prepared');
    const accept = vi.fn(() => {
      retireSensitiveOperations(client);
      if (boundary === 'extra-retirement') retireSensitiveOperations(client);
    });
    let accepted = false;
    await act(async () => {
      accepted = transferSensitiveState(capability, boundary === 'wrong-client' ? other : client, {
        sessionId: boundary === 'wrong-session' ? 'different-session' : target.sessionId,
        principalId: boundary === 'wrong-principal' ? 'different-principal' : target.principalId,
      }, accept);
    });
    expect(accepted).toBe(boundary === 'success');
    expect(container.querySelector('output')?.textContent ?? '').toBe(boundary === 'success' ? secret : '');
    if (boundary === 'success') {
      expect(container.querySelector('aside')?.textContent).toBe('');
      const again = vi.fn();
      expect(transferSensitiveState(capability, client, target, again)).toBe(false);
      expect(again).not.toHaveBeenCalled();
      await act(async () => retireSensitiveOperations(client));
      expect(container.textContent).not.toContain(secret);
    } else if (boundary !== 'extra-retirement') expect(accept).not.toHaveBeenCalled();
  } finally {
    if (boundary !== 'unmount') await act(async () => root.unmount());
    client.clear(); other.clear();
  }
});

async function assertUncached<Input>(hook: () => { mutate: (input: Input) => void }, input: Input, response: object) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: 5, gcTime: Infinity } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  mocks.request.mockResolvedValue(response);
  function Surface() {
    const operation = hook();
    return <button onClick={() => operation.mutate(input)}>Start</button>;
  }
  try {
    await act(async () => root.render(<QueryClientProvider client={client}><Surface /></QueryClientProvider>));
    await act(async () => container.querySelector('button')?.click());
    expect(mocks.request).toHaveBeenCalledOnce();
    expect(client.getMutationCache().getAll()).toEqual([]);
    await act(async () => root.unmount());
    expect(JSON.stringify(client.getMutationCache().getAll())).not.toContain(secret);
    expect(client.getMutationCache().getAll()).toEqual([]);
  } finally { client.clear(); container.remove(); }
}

it('does not register TOTP input or display-once result in MutationCache', () =>
  assertUncached(useEnrolTotpStart, { password: secret }, { otpauth_uri: secret }));
it('keeps password sign-in uncached while accepting the guarded session', async () => {
  await assertUncached(useLogin, { username: 'fixture', password: secret }, { session: { artifact: 'browser' } });
  expect(mocks.accept).toHaveBeenCalledOnce();
});
it('keeps authenticator step-up proof uncached', () =>
  assertUncached(useStepUpTotp, secret, { session: { artifact: 'browser' } }));
it('keeps factor-removal password proof uncached and refreshes account state', async () => {
  await assertUncached(useRemoveTotp, { password: secret }, {});
  expect(mocks.refresh).toHaveBeenCalledOnce();
});
it('keeps recovery proof and display-once codes uncached', () =>
  assertUncached(useRegenerateRecoveryCodes, { proof: secret }, { recovery_codes: [secret] }));
it('keeps single reveal results uncached', () =>
  assertUncached(() => useRevealOne(env), 'key', { value: secret }));
it('keeps bulk reveal results uncached', () =>
  assertUncached(() => useRevealAll(env), undefined, { items: [{ value: secret }] }));
it('keeps value writes uncached', () =>
  assertUncached(() => useSetValue(env), { key: 'key', value: secret }, {}));
it('keeps matrix staged values uncached', () =>
  assertUncached(() => useStageMatrixValue(env), { environment: env.environment, key: 'key', value: secret }, {}));
it('keeps imported values uncached', () =>
  assertUncached(() => useImportValues(env), {
    environment: env.environment, entries: [{ key: 'key', value: secret }], overwrite: [],
    precondition: { definitions_revision: 0, environment_ids: [env.environment], occurrences: [] },
  }, {}));
it('keeps remote connection credentials uncached', () =>
  assertUncached(useAddRemote, { name: 'remote', url: 'https://remote.example', spkiPin: 'pin', credential: secret }, {}));
it('keeps deployment-adapter credentials uncached', () =>
  assertUncached(() => useSetAdapterCredential(env), { adapter: 'adapter', credential: secret }, {}));

it.each(['reset', 'unmount', 'session'])('refuses late plaintext after %s, including StrictMode', async (boundary) => {
  const client = new QueryClient();
  const container = document.createElement('div');
  const root = createRoot(container);
  const response = deferred<string>();
  const delivered = vi.fn();
  const rejected = vi.fn();
  function Surface() {
    const [disclosed, setDisclosed] = useSensitiveState('');
    const operation = useSensitiveMutation({ mutationFn: () => response.promise });
    return <><button onClick={() => {
      void operation.mutateAsync().then((value) => { delivered(); setDisclosed(value); }, rejected);
    }}>Start</button><button onClick={operation.reset}>Close</button><output>{disclosed}</output></>;
  }
  try {
    await act(async () => root.render(<StrictMode><QueryClientProvider client={client}><Surface /></QueryClientProvider></StrictMode>));
    await act(async () => container.querySelector('button')?.click());
    await act(async () => {
      if (boundary === 'reset') container.querySelectorAll('button')[1]?.click();
      else if (boundary === 'session') retireSensitiveOperations(client);
      else root.unmount();
      response.resolve(secret);
    });
    expect(delivered).not.toHaveBeenCalled();
    expect(rejected).toHaveBeenCalledWith(expect.objectContaining({ name: 'AbortError' }));
    expect(container.textContent).not.toContain(secret);
    expect(client.getMutationCache().getAll()).toEqual([]);
  } finally { if (boundary !== 'unmount') await act(async () => root.unmount()); client.clear(); }
});

it('clears displayed state for only the retired session and rejects its captured setter', async () => {
  const left = new QueryClient();
  const right = new QueryClient();
  const container = document.createElement('div');
  const root = createRoot(container);
  const late = deferred<string>();
  function Surface() {
    const [value, setValue] = useSensitiveState('');
    return <><button onClick={() => { setValue(secret); void late.promise.then(setValue); }}>Display</button><output>{value}</output></>;
  }
  try {
    await act(async () => root.render(<><QueryClientProvider client={left}><Surface /></QueryClientProvider><QueryClientProvider client={right}><Surface /></QueryClientProvider></>));
    await act(async () => { for (const button of container.querySelectorAll('button')) button.click(); });
    expect([...container.querySelectorAll('output')].map((node) => node.textContent)).toEqual([secret, secret]);
    await act(async () => { retireSensitiveOperations(left); late.resolve(secret); });
    expect([...container.querySelectorAll('output')].map((node) => node.textContent)).toEqual(['', secret]);
  } finally { await act(async () => root.unmount()); left.clear(); right.clear(); }
});

it('preserves pending, refusal, manual retry and invalidation without automatic replay', async () => {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: 5 } } });
  const container = document.createElement('div');
  const root = createRoot(container);
  const first = deferred<string>();
  const request = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValueOnce(secret);
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  function Surface() {
    const operation = useSensitiveMutation({ mutationFn: (password: string): Promise<string> => request(password),
      onSuccess: () => client.invalidateQueries({ queryKey: ['metadata'] }) });
    return <><button disabled={operation.isPending} onClick={() => operation.mutate(secret)}>Submit</button><output>{operation.status}</output></>;
  }
  try {
    await act(async () => root.render(<QueryClientProvider client={client}><Surface /></QueryClientProvider>));
    await act(async () => container.querySelector('button')?.click());
    expect(container.querySelector('button')?.disabled).toBe(true);
    await act(async () => first.reject(new Error('Refused')));
    expect(container.querySelector('output')?.textContent).toBe('error');
    expect(container.querySelector('button')?.disabled).toBe(false);
    expect(request).toHaveBeenCalledOnce();
    expect(invalidate).not.toHaveBeenCalled();
    await act(async () => container.querySelector('button')?.click());
    expect(container.querySelector('output')?.textContent).toBe('success');
    expect(request).toHaveBeenCalledTimes(2);
    expect(invalidate).toHaveBeenCalledOnce();
    expect(client.getMutationCache().getAll()).toEqual([]);
  } finally { await act(async () => root.unmount()); client.clear(); }
});

it('settles a preparation refusal without issuing or replaying a request', async () => {
  const client = new QueryClient();
  const container = document.createElement('div');
  const root = createRoot(container);
  const request = vi.fn<(input: string) => Promise<string>>();
  function Surface() {
    const operation = useSensitiveMutation({ mutationFn: request, onMutate: () => { throw new Error('Preparation refused'); } });
    return <><button onClick={() => operation.mutate(secret)}>Submit</button><output>{operation.status}</output></>;
  }
  try {
    await act(async () => root.render(<QueryClientProvider client={client}><Surface /></QueryClientProvider>));
    await act(async () => container.querySelector('button')?.click());
    expect(container.querySelector('output')?.textContent).toBe('error');
    expect(request).not.toHaveBeenCalled();
    expect(client.getMutationCache().getAll()).toEqual([]);
  } finally { await act(async () => root.unmount()); client.clear(); }
});

it.each(['reset', 'unmount', 'session'])('refuses a successful result retired during finalization: %s', async (boundary) => {
  const client = new QueryClient();
  const container = document.createElement('div');
  const root = createRoot(container);
  const finalizer = deferred<void>();
  const finalizing = vi.fn(() => finalizer.promise);
  const delivered = vi.fn();
  const rejected = vi.fn();
  function Surface() {
    const operation = useSensitiveMutation({ mutationFn: () => Promise.resolve(secret), onSettled: finalizing });
    return <><button onClick={() => { void operation.mutateAsync().then(delivered, rejected); }}>Start</button><button onClick={operation.reset}>Close</button></>;
  }
  try {
    await act(async () => root.render(<QueryClientProvider client={client}><Surface /></QueryClientProvider>));
    await act(async () => container.querySelector('button')?.click());
    expect(finalizing).toHaveBeenCalledOnce();
    await act(async () => {
      if (boundary === 'reset') container.querySelectorAll('button')[1]?.click();
      else if (boundary === 'session') retireSensitiveOperations(client);
      else root.unmount();
      finalizer.resolve();
    });
    expect(delivered).not.toHaveBeenCalled();
    expect(rejected).toHaveBeenCalledWith(expect.objectContaining({ name: 'AbortError' }));
  } finally { if (boundary !== 'unmount') await act(async () => root.unmount()); client.clear(); }
});

it('settles a throwing finalizer as a refusal instead of remaining pending', async () => {
  const client = new QueryClient();
  const container = document.createElement('div');
  const root = createRoot(container);
  const request = vi.fn(() => Promise.resolve(secret));
  function Surface() {
    const operation = useSensitiveMutation({ mutationFn: request, onSettled: () => { throw new Error('Refresh refused'); } });
    return <><button onClick={() => operation.mutate()}>Start</button><output>{operation.status}</output></>;
  }
  try {
    await act(async () => root.render(<QueryClientProvider client={client}><Surface /></QueryClientProvider>));
    await act(async () => container.querySelector('button')?.click());
    expect(container.querySelector('output')?.textContent).toBe('error');
    expect(request).toHaveBeenCalledOnce();
  } finally { await act(async () => root.unmount()); client.clear(); }
});

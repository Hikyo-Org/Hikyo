// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, useNavigate } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { CLILogin } from './CLILogin.tsx';

const auth = vi.hoisted(() => ({ revision: 0 }));
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({
    state: { status: 'authenticated', sessionEpoch: 'browser-one' },
    identity: { principal: { display_name: 'Alice' } },
    captureTransition: () => ({ revision: auth.revision }),
  }),
}));
const fetcher = vi.fn<typeof fetch>();
const cleanups: Array<() => Promise<void>> = [];
const state = 'A'.repeat(43);
function response(body: object, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}
function transaction(reference = state) {
  return {
    reference,
    transport: reference === state ? 'loopback' : 'device',
    redirect_uri: reference === state ? 'http://127.0.0.1:12345/callback' : '',
    instance: 'https://instance.example',
    expires_at: new Date(Date.now() + 60_000).toISOString(),
  };
}
function NextRequest() {
  const navigate = useNavigate();
  return (
    <button onClick={() => void navigate('/login/cli?user_code=JKMNPQRS')}>Next request</button>
  );
}
async function mount(query: string) {
  const view = await renderForm(
    <MemoryRouter initialEntries={[`/login/cli?${query}`]}>
      <CLILogin />
      <NextRequest />
    </MemoryRouter>,
  );
  cleanups.push(view.unmount);
  await settleTask();
  return view;
}
function approve(container: HTMLElement) {
  const button = [...container.querySelectorAll('button')].find(
    (node) => node.textContent === 'Authorize CLI sign-in',
  );
  if (button === undefined) throw new Error('missing approval');
  return button;
}
beforeEach(() => {
  auth.revision = 0;
  fetcher.mockReset();
  vi.stubGlobal('fetch', fetcher);
});
afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) await cleanup();
  vi.unstubAllGlobals();
});

it('shows server consent metadata and ignores callback/instance URL fields', async () => {
  fetcher.mockResolvedValue(response(transaction()));
  const { container } = await mount(
    `state=${state}&instance=attacker&redirect_uri=https://attacker.example`,
  );
  expect(container.textContent).toContain('https://instance.example');
  expect(container.textContent).toContain('Separate CLI session');
  expect(container.textContent).not.toContain('attacker');
  expect(approve(container).disabled).toBe(false);
});
it('rejects malformed state before reading a transaction', async () => {
  const { container } = await mount('state=short');
  expect(container.textContent).toContain('Nothing to authorize');
  expect(fetcher).not.toHaveBeenCalled();
});
it('enters and names the human device code without showing a device secret', async () => {
  fetcher.mockResolvedValue(response(transaction('ABCDEFGH')));
  const { container } = await mount('device=1&provider=oidc:corp');
  const input = container.querySelector('input');
  if (input === null) throw new Error('missing code input');
  await act(async () => typeInto(input, 'abcd-efgh'));
  await act(async () =>
    container
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  );
  await settleTask();
  expect(container.textContent).toContain('ABCDEFGH');
  expect(container.textContent).not.toContain('device_code');
});
it('re-reads consent and refuses a changed callback before approval', async () => {
  const displayed = transaction();
  fetcher
    .mockResolvedValueOnce(response(displayed))
    .mockResolvedValueOnce(
      response({ ...displayed, redirect_uri: 'http://127.0.0.1:45678/other' }),
    );
  const { container } = await mount(`state=${state}`);
  await act(async () => approve(container).click());
  await settleTask();
  expect(container.textContent).toContain('could not be completed');
  expect(
    fetcher.mock.calls.every(([input]) => input instanceof Request && input.method === 'GET'),
  ).toBe(true);
});
it('finishes device approval without caching the approval response', async () => {
  const displayed = transaction('ABCDEFGH');
  fetcher
    .mockResolvedValueOnce(response(displayed))
    .mockResolvedValueOnce(response(displayed))
    .mockResolvedValueOnce(response({ code: '', state: 'ABCDEFGH', redirect_uri: '' }));
  const { container, client } = await mount('user_code=ABCDEFGH');
  await act(async () => approve(container).click());
  await settleTask();
  expect(container.textContent).toContain('CLI sign-in approved');
  expect(client.getMutationCache().getAll()).toHaveLength(0);
});
it('does not approve after the browser session changed during the metadata refresh', async () => {
  const displayed = transaction();
  fetcher.mockResolvedValueOnce(response(displayed)).mockImplementationOnce(async () => {
    auth.revision += 1;
    return response(displayed);
  });
  const { container } = await mount(`state=${state}`);
  await act(async () => approve(container).click());
  await settleTask();
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(container.textContent).toContain('could not be completed');
});

it('cancels in an ordinary browser tab without sending approval', async () => {
  fetcher.mockResolvedValue(response(transaction()));
  const { container } = await mount(`state=${state}`);
  const cancel = [...container.querySelectorAll('button')].find(
    (node) => node.textContent === 'Cancel',
  );
  if (cancel === undefined) throw new Error('missing cancel');
  await act(async () => cancel.click());
  expect(container.textContent).toContain('CLI sign-in cancelled');
  expect(
    fetcher.mock.calls.every(([input]) => input instanceof Request && input.method === 'GET'),
  ).toBe(true);
});
it('requires a separate approval after the route changes to another device request', async () => {
  const first = transaction('ABCDEFGH');
  fetcher
    .mockResolvedValueOnce(response(first))
    .mockResolvedValueOnce(response(first))
    .mockResolvedValueOnce(response({ code: '', state: 'ABCDEFGH', redirect_uri: '' }))
    .mockResolvedValue(response(transaction('JKMNPQRS')));
  const { container } = await mount('user_code=ABCDEFGH');
  await act(async () => approve(container).click());
  await settleTask();
  expect(container.textContent).toContain('CLI sign-in approved');
  const next = [...container.querySelectorAll('button')].find(
    (node) => node.textContent === 'Next request',
  );
  if (next === undefined) throw new Error('missing next request');
  await act(async () => next.click());
  await settleTask();
  expect(container.textContent).not.toContain('CLI sign-in approved');
  expect(container.textContent).toContain('JKMNPQRS');
  expect(approve(container).disabled).toBe(false);
});

it.each(['expired', 'wrong-reference'])('refuses %s transaction metadata without consent', async (failure) => {
  const metadata = transaction();
  fetcher.mockResolvedValue(response(failure === 'expired'
    ? { ...metadata, expires_at: new Date(Date.now() - 1_000).toISOString() }
    : { ...metadata, reference: 'B'.repeat(43) }));
  const { container } = await mount(`state=${state}`);
  expect(container.textContent).toContain('CLI sign-in could not be completed');
  expect([...container.querySelectorAll('button')].some((node) => node.textContent === 'Authorize CLI sign-in')).toBe(false);
});

it('does not carry a failed approval into the next request', async () => {
  const displayed = transaction();
  fetcher.mockResolvedValueOnce(response(displayed))
    .mockResolvedValueOnce(response({ ...displayed, redirect_uri: 'http://127.0.0.1:45678/other' }))
    .mockResolvedValue(response(transaction('JKMNPQRS')));
  const { container } = await mount(`state=${state}`);
  await act(async () => approve(container).click());
  await settleTask();
  expect(container.textContent).toContain('could not be completed');
  const next = [...container.querySelectorAll('button')].find((node) => node.textContent === 'Next request');
  if (next === undefined) throw new Error('missing next request');
  await act(async () => next.click());
  await settleTask();
  expect(container.textContent).toContain('JKMNPQRS');
  expect(approve(container).disabled).toBe(false);
});

it('retires an in-flight approval when the route changes to a new request', async () => {
  const displayed = transaction();
  let release: (value: Response) => void = () => { throw new Error('no pending request'); };
  const pending = new Promise<Response>((resolve) => { release = resolve; });
  fetcher.mockResolvedValueOnce(response(displayed)).mockReturnValueOnce(pending)
    .mockResolvedValue(response(transaction('JKMNPQRS')));
  const { container } = await mount(`state=${state}`);
  await act(async () => approve(container).click());
  const next = [...container.querySelectorAll('button')].find((node) => node.textContent === 'Next request');
  if (next === undefined) throw new Error('missing next request');
  await act(async () => next.click());
  await settleTask();
  expect(container.textContent).toContain('JKMNPQRS');
  expect(approve(container).disabled).toBe(false);
  await act(async () => release(response(displayed)));
  await settleTask();
  expect(container.textContent).not.toContain('could not be completed');
  expect(approve(container).disabled).toBe(false);
  expect(fetcher.mock.calls.every(([input]) => input instanceof Request && input.method === 'GET')).toBe(true);
});

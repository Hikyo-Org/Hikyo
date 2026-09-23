// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { AccountProfile } from './AccountProfile.tsx';

const refreshSession = vi.hoisted(() => vi.fn(async () => {}));
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: 'prn_alice' } }, refreshSession }),
}));

const profile = { username: 'alice', display_name: 'Alice Example', email: null, email_verified: false, managed: false, username_editable: true };
let unmount: (() => Promise<void>) | undefined;

afterEach(async () => {
  await unmount?.();
  unmount = undefined;
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function input(container: HTMLElement, name: string): HTMLInputElement {
  const result = container.querySelector(`input[name="${name}"]`);
  if (!(result instanceof HTMLInputElement)) throw new Error(`Missing ${name} input`);
  return result;
}

function json(body: object, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

async function mount() {
  const rendered = await renderForm(<AccountProfile />);
  unmount = rendered.unmount;
  await settleTask();
  return rendered;
}

async function submit(container: HTMLElement) {
  const form = container.querySelector('form');
  if (!(form instanceof HTMLFormElement)) throw new Error('Missing profile form');
  await act(async () => { form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
  await settleTask();
}

describe('account profile', () => {
  it('saves readable names through the API, clears proof, and refreshes the signed-in name', async () => {
    const saved = { ...profile, username: 'alice-new', display_name: 'Alice New' };
    const fetchMock = vi.fn((request: Request) => Promise.resolve(json(request.method === 'PATCH' ? saved : profile)));
    vi.stubGlobal('fetch', fetchMock);
    const { container, client } = await mount();
    expect(input(container, 'username').value).toBe('alice');
    expect(container.querySelector('button[type="submit"]')?.hasAttribute('disabled')).toBe(true);
    await act(async () => {
      typeInto(input(container, 'username'), saved.username);
      typeInto(input(container, 'display_name'), saved.display_name);
    });
    await act(async () => { typeInto(input(container, 'proof'), 'existing-password'); });
    await submit(container);
    const request = fetchMock.mock.calls.find(([candidate]) => candidate.method === 'PATCH')?.[0];
    expect(request).toBeDefined();
    expect(new URL(request?.url ?? '').pathname).toBe('/api/v1/me/profile');
    expect(await request?.json()).toEqual({ username: saved.username, display_name: saved.display_name, proof: 'existing-password' });
    expect(container.querySelector('input[name="proof"]')).toBeNull();
    expect(container.textContent).toContain('Profile saved.');
    expect(refreshSession).toHaveBeenCalledOnce();
    expect(client.getMutationCache().getAll()).toHaveLength(0);
  });

  it('keeps edits available after a duplicate username refusal without reporting success', async () => {
    vi.stubGlobal('fetch', vi.fn((request: Request) => Promise.resolve(request.method === 'PATCH'
      ? json({ error: { code: 'conflict', message: 'conflict' } }, 409) : json(profile))));
    const { container } = await mount();
    await act(async () => {
      typeInto(input(container, 'username'), 'taken');
    });
    await act(async () => { typeInto(input(container, 'proof'), 'existing-password'); });
    await submit(container);
    expect(container.textContent).toContain('That username is already in use.');
    expect(container.textContent).not.toContain('Profile saved.');
    expect(input(container, 'username').value).toBe('taken');
    expect(input(container, 'proof').value).toBe('');
  });

  it('hides provider-managed handles while keeping the display name read-only', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(json({ ...profile, managed: true }))));
    const { container } = await mount();
    expect(container.querySelector('input[name="username"]')).toBeNull();
    expect(input(container, 'display_name').readOnly).toBe(true);
    expect(container.textContent).toContain('Your identity provider manages your username and display name.');
  });

  it('saves an SSO display name with an empty start and no local password', async () => {
    const sso = { ...profile, username: 'scim_internal_handle', display_name: '', username_editable: false };
    const fetchMock = vi.fn((request: Request) => Promise.resolve(json(request.method === 'PATCH'
      ? { ...sso, display_name: 'Alice' } : sso)));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = await mount();
    expect(container.textContent).not.toContain('scim_internal_handle');
    expect(container.querySelector('input[name="proof"]')).toBeNull();
    expect(input(container, 'display_name').required).toBe(false);
    await act(async () => { typeInto(input(container, 'display_name'), 'Alice'); });
    await submit(container);
    const request = fetchMock.mock.calls.find(([candidate]) => candidate.method === 'PATCH')?.[0];
    expect(await request?.json()).toEqual({ username: sso.username, display_name: 'Alice' });
    expect(container.textContent).toContain('Profile saved.');
  });

  it('shows the sign-in email read-only with truthful copy and never sends it', async () => {
    const verified = { ...profile, email: 'alice@example.com', email_verified: true };
    const fetchMock = vi.fn((request: Request) => Promise.resolve(json(request.method === 'PATCH'
      ? { ...verified, display_name: 'Alice New' } : verified)));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = await mount();
    expect(input(container, 'email').value).toBe('alice@example.com');
    expect(input(container, 'email').readOnly).toBe(true);
    expect(container.querySelector('label[for$="-email"]')?.textContent).toBe('Sign-in email');
    expect(container.textContent).toContain('Used to sign in. Set when you sign up with email');
    expect(container.textContent).not.toContain('not used to sign in');
    await act(async () => { typeInto(input(container, 'display_name'), 'Alice New'); });
    await submit(container);
    const request = fetchMock.mock.calls.find(([candidate]) => candidate.method === 'PATCH')?.[0];
    expect(await request?.json()).toEqual({ username: verified.username, display_name: 'Alice New' });
  });

  it('shows an unverified legacy email as contact data that is not used to sign in', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(json({ ...profile, email: 'alice@example.com' }))));
    const { container } = await mount();
    expect(input(container, 'email').value).toBe('alice@example.com');
    expect(input(container, 'email').readOnly).toBe(true);
    expect(container.querySelector('label[for$="-email"]')?.textContent).toBe('Contact email');
    expect(container.textContent).toContain('A contact address from before sign-in email existed. It is not used to sign in');
    expect(container.textContent).not.toContain('Sign-in email');
    expect(container.textContent).not.toContain('Used to sign in.');
  });

  it('shows no email field when the account has no sign-in email', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(json(profile))));
    const { container } = await mount();
    expect(container.querySelector('input[name="email"]')).toBeNull();
  });

  it('does not display an editable empty profile when loading fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(json({ error: { code: 'internal', message: 'internal' } }, 500))));
    const { container } = await mount();
    expect(container.textContent).toContain('Your profile could not be loaded.');
    expect(container.querySelector('form')).toBeNull();
  });
});

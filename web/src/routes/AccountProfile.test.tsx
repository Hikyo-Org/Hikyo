// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { AccountProfile } from './AccountProfile.tsx';

const refreshSession = vi.hoisted(() => vi.fn(async () => {}));
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: 'prn_alice' } }, refreshSession }),
}));

const profile = { username: 'alice', display_name: 'Alice Example', email: '', managed: false, username_editable: true };
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
  it('saves readable names and email through the API, clears proof, and refreshes the signed-in name', async () => {
    const saved = { ...profile, username: 'alice-new', display_name: 'Alice New', email: 'alice@example.com' };
    const fetchMock = vi.fn((request: Request) => Promise.resolve(json(request.method === 'PATCH' ? saved : profile)));
    vi.stubGlobal('fetch', fetchMock);
    const { container, client } = await mount();
    expect(input(container, 'username').value).toBe('alice');
    expect(container.querySelector('button[type="submit"]')?.hasAttribute('disabled')).toBe(true);
    await act(async () => {
      typeInto(input(container, 'username'), saved.username);
      typeInto(input(container, 'display_name'), saved.display_name);
      typeInto(input(container, 'email'), saved.email);
    });
    await act(async () => { typeInto(input(container, 'proof'), 'existing-password'); });
    await submit(container);
    const request = fetchMock.mock.calls.find(([candidate]) => candidate.method === 'PATCH')?.[0];
    expect(request).toBeDefined();
    expect(new URL(request?.url ?? '').pathname).toBe('/api/v1/me/profile');
    expect(await request?.json()).toEqual({ username: saved.username, display_name: saved.display_name, email: saved.email, proof: 'existing-password' });
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
    expect(input(container, 'email').readOnly).toBe(false);
    expect(container.textContent).toContain('Your identity provider manages your username and display name.');
  });

  it.each([false, true])('saves an SSO contact email with an empty display name and no local password (managed: %s)', async (managed) => {
    const sso = { ...profile, username: 'scim_internal_handle', display_name: '', managed, username_editable: false };
    const fetchMock = vi.fn((request: Request) => Promise.resolve(json(request.method === 'PATCH'
      ? { ...sso, email: 'alice@example.com' } : sso)));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = await mount();
    expect(container.textContent).not.toContain('scim_internal_handle');
    expect(container.querySelector('input[name="proof"]')).toBeNull();
    expect(input(container, 'display_name').required).toBe(false);
    await act(async () => { typeInto(input(container, 'email'), 'alice@example.com'); });
    await submit(container);
    const request = fetchMock.mock.calls.find(([candidate]) => candidate.method === 'PATCH')?.[0];
    expect(await request?.json()).toEqual({ username: sso.username, display_name: sso.display_name, email: 'alice@example.com' });
    expect(container.textContent).toContain('Profile saved.');
  });

  it('does not display an editable empty profile when loading fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(json({ error: { code: 'internal', message: 'internal' } }, 500))));
    const { container } = await mount();
    expect(container.textContent).toContain('Your profile could not be loaded.');
    expect(container.querySelector('form')).toBeNull();
  });
});

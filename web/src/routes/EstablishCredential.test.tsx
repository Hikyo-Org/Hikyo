// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { AUTHORITY_REFUSAL } from '../api/session.ts';
import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { EstablishCredential } from './EstablishCredential.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const PASSWORD = 'a first password long enough';

type Provider = { kind: string; slug: string; display_name: string; brand?: string; profile?: 'github' };

const GOOGLE: Provider = { kind: 'oidc', slug: 'google', display_name: 'Google', brand: 'google' };
const GITHUB: Provider = { kind: 'oauth2', slug: 'github', display_name: 'GitHub', brand: 'github', profile: 'github' };
const SAML: Provider = { kind: 'saml', slug: 'corp-saml', display_name: 'Corp SAML' };

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function pathOf(input: Parameters<typeof fetch>[0], init?: RequestInit): Request {
  return input instanceof Request ? input : new Request(new URL(String(input), 'http://localhost'), init);
}

/**
 * Mounts the page against a fetch routed by path: the public methods list,
 * the establish POST, the claim starts and the recovery begin.
 */
function mount(
  options: {
    establish?: number;
    providers?: Provider[];
    start?: number;
    url?: string;
  } = {},
) {
  const requests: Request[] = [];
  const fetchMock = vi.fn((input: Parameters<typeof fetch>[0], init?: RequestInit) => {
    const request = pathOf(input, init);
    requests.push(request.clone());
    const path = new URL(request.url).pathname;
    if (path === '/api/v1/auth/methods') {
      return Promise.resolve(
        json({
          providers: options.providers ?? [],
          local_login_enabled: true,
          signup_open: false,
          signup_paused: false,
          signup_methods: [],
        }),
      );
    }
    if (path.endsWith('/start')) {
      return Promise.resolve(
        (options.start ?? 200) === 200
          ? json({ authorization_url: 'https://idp.example/authorize?state=hos_claim_state' })
          : new Response(null, { status: options.start }),
      );
    }
    if (path === '/api/v1/auth/recovery/begin') {
      return Promise.resolve(json({ authority: 'hik_cea_recovered_authority', expires_at: '2026-10-09T00:00:00Z' }));
    }
    return Promise.resolve(new Response(null, { status: options.establish ?? 204 }));
  });
  vi.stubGlobal('fetch', fetchMock);
  const stored = new Map<string, string>();
  vi.stubGlobal('sessionStorage', {
    getItem: (key: string) => stored.get(key) ?? null,
    setItem: (key: string, value: string) => void stored.set(key, value),
    removeItem: (key: string) => void stored.delete(key),
  });
  const assign = vi.spyOn(globalThis.location, 'assign').mockImplementation(() => {});
  const posted = (suffix: string) =>
    requests.filter((request) => request.method === 'POST' && new URL(request.url).pathname.endsWith(suffix));
  return {
    assign,
    posted,
    stored,
    rendered: renderForm(
      <MemoryRouter initialEntries={[options.url ?? '/establish']}>
        <EstablishCredential />
      </MemoryRouter>,
    ),
  };
}

function field(root: ParentNode, name: string): HTMLInputElement {
  const control = root.querySelector(`input[name="${name}"]`);
  if (!(control instanceof HTMLInputElement)) {
    throw new Error(`${name} input is missing`);
  }
  return control;
}

function button(root: ParentNode, text: string): HTMLButtonElement {
  const found = [...root.querySelectorAll('button')].find((candidate) => candidate.textContent?.includes(text));
  if (!(found instanceof HTMLButtonElement)) throw new Error(`no button reads ${text}`);
  return found;
}

/** Settles until the public methods list has rendered the named button. */
async function provided(root: ParentNode, text: string): Promise<HTMLButtonElement> {
  for (let round = 0; round < 50; round += 1) {
    const found = [...root.querySelectorAll('button')].find((candidate) => candidate.textContent?.includes(text));
    if (found instanceof HTMLButtonElement) return found;
    await settle();
  }
  return button(root, text);
}

async function submit(root: ParentNode) {
  const form = root.querySelector('form');
  if (!(form instanceof HTMLFormElement)) throw new Error('the establish form is missing');
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await settle();
}

async function fill(root: ParentNode, authority: string, password: string, repeat: string) {
  typeInto(field(root, 'authority'), authority);
  typeInto(field(root, 'password'), password);
  typeInto(field(root, 'repeat'), repeat);
  await submit(root);
}

async function click(target: HTMLElement) {
  await act(async () => {
    target.click();
  });
  await settle();
}

describe('EstablishCredential', () => {
  it('refuses mismatched passwords locally, before any request', async () => {
    const { posted, rendered } = mount();
    const { container, unmount } = await rendered;
    await fill(container, 'hik_cea_authority_value_1234', PASSWORD, `${PASSWORD} but different`);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('differ');
    expect(posted('/credential/establish')).toHaveLength(0);
    await unmount();
  });

  it('refuses a short password locally, before any request', async () => {
    const { posted, rendered } = mount();
    const { container, unmount } = await rendered;
    await fill(container, 'hik_cea_authority_value_1234', 'short', 'short');
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('at least 12');
    expect(posted('/credential/establish')).toHaveLength(0);
    await unmount();
  });

  it('establishes the credential and offers sign-in, with the authority cleared', async () => {
    const { posted, rendered } = mount();
    const { container, unmount } = await rendered;
    await fill(container, ' hik_cea_authority_value_1234 ', PASSWORD, PASSWORD);
    const [request] = posted('/credential/establish');
    expect(await request?.json()).toEqual({ authority: 'hik_cea_authority_value_1234', password: PASSWORD });
    expect(container.querySelector('h1')?.textContent).toBe('Credential established');
    expect(container.querySelector('a[href="/login"]')?.textContent).toBe('Sign in');
    expect(container.textContent).not.toContain('hik_cea_authority_value_1234');
    await unmount();
  });

  it('voices a refused authority uniformly', async () => {
    const { rendered } = mount({ establish: 401 });
    const { container, unmount } = await rendered;
    await fill(container, 'hik_cea_spent_authority_value', PASSWORD, PASSWORD);
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(AUTHORITY_REFUSAL);
    expect(container.querySelector('form')).not.toBeNull();
    await unmount();
  });

  it('offers one claim per OIDC or OAuth2 provider beside the password form, never SAML', async () => {
    const { rendered } = mount({ providers: [GOOGLE, SAML, GITHUB] });
    const { container, unmount } = await rendered;
    const google = await provided(container, 'Continue with Google');
    const github = button(container, 'Continue with GitHub');
    expect(container.textContent).not.toContain('Corp SAML');
    // Token first: nothing can claim before the authority is pasted.
    expect(google.disabled).toBe(true);
    expect(github.disabled).toBe(true);
    typeInto(field(container, 'authority'), 'hik_cea_authority_value_1234');
    await settle();
    expect(google.disabled).toBe(false);
    expect(github.disabled).toBe(false);
    await unmount();
  });

  it('claims through the provider kind with the authority as the proof, returning to account security', async () => {
    for (const provider of [GOOGLE, GITHUB]) {
      const { assign, posted, stored, rendered } = mount({ providers: [GOOGLE, GITHUB] });
      const { container, unmount } = await rendered;
      await provided(container, 'Continue with Google');
      typeInto(field(container, 'authority'), ' hik_cea_authority_value_1234 ');
      await settle();
      await click(button(container, `Continue with ${provider.display_name}`));
      const [request] = posted('/start');
      expect(new URL(request?.url ?? '').pathname).toBe(`/api/v1/auth/${provider.kind}/${provider.slug}/start`);
      expect(await request?.json()).toEqual({ purpose: 'claim', proof: 'hik_cea_authority_value_1234', browser: true });
      expect(stored.get('hikyo-oidc-return:hos_claim_state')).toBe('/settings');
      expect(assign).toHaveBeenCalledWith('https://idp.example/authorize?state=hos_claim_state');
      expect(posted('/credential/establish')).toHaveLength(0);
      // Wiped before leaving: Back from the provider restores no authority.
      expect(field(container, 'authority').value).toBe('');
      await unmount();
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    }
  });

  it('voices a refused claim start in the same one sentence and stays', async () => {
    const { assign, rendered } = mount({ providers: [GOOGLE], start: 401 });
    const { container, unmount } = await rendered;
    await provided(container, 'Continue with Google');
    typeInto(field(container, 'authority'), 'hik_cea_spent_authority_value');
    await settle();
    await click(button(container, 'Continue with Google'));
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(AUTHORITY_REFUSAL);
    expect(assign).not.toHaveBeenCalled();
    expect(button(container, 'Continue with Google').disabled).toBe(false);
    await unmount();
  });

  it('wipes every secret on a persisted pagehide (the back-forward cache)', async () => {
    const { rendered } = mount({ providers: [GOOGLE] });
    const { container, unmount } = await rendered;
    await provided(container, 'Continue with Google');
    typeInto(field(container, 'authority'), 'hik_cea_authority_value_1234');
    typeInto(field(container, 'password'), PASSWORD);
    typeInto(field(container, 'repeat'), PASSWORD);
    await act(async () => {
      const event = new Event('pagehide');
      Object.defineProperty(event, 'persisted', { value: true });
      globalThis.dispatchEvent(event);
    });
    await settle();
    expect(field(container, 'authority').value).toBe('');
    expect(field(container, 'password').value).toBe('');
    expect(field(container, 'repeat').value).toBe('');
    await unmount();
  });

  it('offers no provider after a recovery: a recovery-issued authority never claims', async () => {
    const { rendered } = mount({ providers: [GOOGLE, GITHUB], url: '/establish?mode=recover' });
    const { container, unmount } = await rendered;
    await settle();
    typeInto(field(container, 'username'), 'invitee');
    typeInto(field(container, 'code'), 'recovery-code');
    await submit(container);
    expect(container.querySelector('h1')?.textContent).toBe('Establish your credential');
    expect(container.textContent).not.toContain('Continue with');
    await unmount();
  });
});

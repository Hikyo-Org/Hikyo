// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { clearNotification, notifyFailure, ToastViewport } from '../app/notifications.tsx';
import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { AccountSecurity } from './AccountSecurity.tsx';

vi.mock('./AccountProfile.tsx', () => ({ AccountProfile: () => null }));

const authProviders = vi.hoisted(() => {
  const values: { kind: string; slug: string; display_name: string }[] = [];
  return { values };
});

type EstablishStatus = { kind: string; slug: string; display_name: string; expires_at: string };
const factor = vi.hoisted(() => {
  const value: { loaded: boolean; failed: boolean; confirmed: boolean; has_password: boolean; has_passkey: boolean; establish?: EstablishStatus } = { loaded: true, failed: false, confirmed: false, has_password: true, has_passkey: true };
  return value;
});
const security = vi.hoisted(() => {
  const identities: { id: string; kind: string; provider_slug: string; display_name: string; issuer: string; subject: string; created_at: string }[] = [];
  return { password: vi.fn(), link: vi.fn(), establish: vi.fn(), establishFailed: false, refetch: vi.fn(), identities };
});
const passkeyMutations = vi.hoisted(() => ({ enrol: vi.fn(), remove: vi.fn() }));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({
    identity: {
      principal: { id: 'usr_alice', display_name: 'Alice' },
      session: { assurance: { factors: ['password'] } },
    },
    refreshSession: vi.fn(),
  }),
}));

vi.mock('../api/account.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/account.ts')>();
  const mutation = () => ({ isPending: false, mutate: vi.fn() });
  return {
    ...actual,
    useAuthMethods: () => ({
      isPending: false,
      isError: false,
      isSuccess: true,
      data: { providers: authProviders.values },
    }),
    useConfirmTotp: mutation,
    useEnrolPasskey: () => ({ isPending: false, mutate: passkeyMutations.enrol }),
    useEnrolTotpStart: mutation,
    useIdentities: () => ({
      isError: false,
      isSuccess: true,
      data: { identities: security.identities },
    }),
    useLinkIdentity: () => ({ isPending: false, mutate: security.link }),
    useSetPassword: () => ({ isPending: false, isError: false, reset: vi.fn(), mutate: security.password }),
    useEstablishCredential: () => ({ isPending: false, isError: security.establishFailed, error: new Error('provider unavailable'), mutate: security.establish }),
    usePasskeys: () => ({
      isPending: false,
      isError: false,
      isSuccess: true,
      data: {
        passkeys: [
          { id: 'wacred_1', label: 'passkey', discoverable: true, disabled: false, created_at: '2026-08-24T12:00:00Z', last_used_at: null },
        ],
      },
    }),
    useRegenerateRecoveryCodes: () => ({ ...mutation(), codes: null, dismiss: vi.fn() }),
    useRemovePasskey: () => ({ isPending: false, mutate: passkeyMutations.remove }),
    useRemoveTotp: mutation,
    useTotpStatus: () => ({
      refetch: security.refetch,
      isPending: !factor.loaded && !factor.failed,
      isFetching: false,
      isError: factor.failed,
      isSuccess: factor.loaded && !factor.failed,
      data: !factor.loaded || factor.failed ? undefined : { confirmed: factor.confirmed, pending: false, has_password: factor.has_password, has_passkey: factor.has_passkey, can_reauth_oidc: false, establish: factor.establish },
    }),
    useUnlinkIdentity: mutation,
  };
});

vi.mock('../api/remotes.ts', () => ({
  useSessions: () => ({
    isError: false,
    isSuccess: true,
    data: {
      items: [
        {
          id: 'sess_1',
          artifact: 'browser',
          auth_method: 'password',
          last_seen_at: '2026-08-24T12:00:00Z',
        },
      ],
    },
  }),
  useRevokeSession: () => ({
    isPending: false,
    mutate: (
      _session: string,
      callbacks: { readonly onError: (error: Error) => void },
    ) => callbacks.onError(new Error('revoke failed')),
  }),
}));

let unmount: (() => Promise<void>) | undefined;

beforeEach(() => {
  clearNotification();
  authProviders.values.length = 0;
  factor.loaded = true; factor.failed = false;
  factor.confirmed = false;
  factor.has_password = true; factor.has_passkey = true; factor.establish = undefined;
  security.establishFailed = false; security.identities.length = 0; security.password.mockReset(); security.link.mockReset(); security.establish.mockReset(); security.refetch.mockReset();
  passkeyMutations.enrol.mockReset();
  passkeyMutations.remove.mockReset();
});

afterEach(async () => {
  await unmount?.();
  unmount = undefined;
  clearNotification();
  document.body.replaceChildren();
});

describe('AccountSecurity mutation feedback', () => {
  it('keeps a failed security mutation visible inline when a later toast replaces its toast', async () => {
    const rendered = await renderForm(
      <MemoryRouter>
        <AccountSecurity />
        <ToastViewport />
      </MemoryRouter>,
    );
    unmount = rendered.unmount;

    const revoke = rendered.container.querySelector(
      'button[aria-label="Revoke the browser session sess_1"]',
    );
    if (!(revoke instanceof HTMLButtonElement)) {
      throw new Error('the session has no revoke button');
    }

    await act(async () => revoke.click());
    await settle();

    const inlineFailure = rendered.container.querySelector('.page > .alert[role="alert"]');
    expect(inlineFailure?.textContent).toContain(
      'The account surface could not be reached, or it answered something this client does not understand.',
    );
    expect(rendered.container.querySelector('.toast')?.textContent).toContain(
      'The account surface could not be reached',
    );

    await act(async () => notifyFailure('A later security failure.'));

    expect(inlineFailure?.textContent).toContain('The account surface could not be reached');
    expect(rendered.container.querySelector('.toast')?.textContent).toContain(
      'A later security failure.',
    );
  });
});

it('preserves unknown provider kinds without starting an unsupported link flow', async () => {
  authProviders.values.push(
    { kind: 'future-kind', slug: 'future', display_name: 'Future provider' },
    { kind: 'oidc', slug: 'known', display_name: 'Known provider' },
  );
  const rendered = await renderForm(<MemoryRouter><AccountSecurity /></MemoryRouter>);
  unmount = rendered.unmount;
  await act(async () => [...rendered.container.querySelectorAll('button')].find((button) => button.textContent === 'Link another identity')?.click());
  const buttons = [...rendered.container.querySelectorAll('button')];
  const future = buttons.find((button) => button.textContent?.trim() === 'Future provider · future-kind');
  const known = buttons.find((button) => button.textContent?.trim() === 'Known provider · oidc');
  expect(future?.disabled).toBe(true);
  expect(known?.disabled).toBe(false);
  await act(async () => future?.click());
  expect(rendered.container.querySelector('dialog input')).toBeNull();
});

/**
 * The passkey proof dialog asks for the class the server will accept
 * (possession-first): the authenticator code where a confirmed factor stands,
 * the password otherwise, and sends that class and nothing else.
 */
describe('passkey proof selection', () => {
  it('sends the proof class displayed after inventory updates while the dialog stays open', async () => {
    const view = await renderForm(<MemoryRouter><AccountSecurity /></MemoryRouter>);
    unmount = view.unmount;
    await act(async () => button(view.container, 'add').click());
    const password = view.container.querySelector<HTMLInputElement>('dialog input');
    if (password === null) throw new Error('No password proof input');
    await act(async () => typeInto(password, 'password-that-must-not-become-a-code'));
    factor.confirmed = true;
    await view.rerender(<MemoryRouter><AccountSecurity /></MemoryRouter>);
    const input = view.container.querySelector<HTMLInputElement>('dialog input');
    if (input === null) throw new Error('No proof input');
    expect(input.autocomplete).toBe('one-time-code');
    expect(input.value).toBe('');
    await act(async () => typeInto(input, '123456'));
    await act(async () => button(view.container, 'Confirm').click());
    expect(passkeyMutations.enrol).toHaveBeenCalledWith({ proof: { kind: 'code', code: '123456' } }, expect.anything());
  });

  async function submitProof(open: string, value: string) {
    const rendered = await renderForm(<MemoryRouter><AccountSecurity /></MemoryRouter>);
    unmount = rendered.unmount;
    const trigger = rendered.container.querySelector(`button[aria-label="${open}"]`);
    if (!(trigger instanceof HTMLButtonElement)) {
      throw new Error(`no ${open} button`);
    }
    await act(async () => trigger.click());
    const input = rendered.container.querySelector('dialog form input');
    if (!(input instanceof HTMLInputElement)) {
      throw new Error('the proof dialog has no input');
    }
    const label = rendered.container.querySelector(`label[for="${input.id}"]`);
    const form = input.closest('form');
    if (form === null) {
      throw new Error('the proof input is outside its form');
    }
    await act(async () => typeInto(input, value));
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    await settle();
    return { input, label: label?.textContent ?? '' };
  }

  it('asks for the password and sends it when no factor is confirmed', async () => {
    const { input, label } = await submitProof('Add a passkey', 'existing-password');
    expect(label).toBe('Password');
    expect(input.type).toBe('password');
    expect(input.autocomplete).toBe('current-password');
    expect(passkeyMutations.enrol).toHaveBeenCalledWith(
      { proof: { kind: 'password', password: 'existing-password' } },
      expect.anything(),
    );
  });

  it('asks for the authenticator code and sends it when a factor is confirmed', async () => {
    factor.confirmed = true;
    const { input, label } = await submitProof('Add a passkey', '123456');
    expect(label).toBe('Authenticator code');
    expect(input.type).toBe('text');
    expect(input.inputMode).toBe('numeric');
    expect(input.autocomplete).toBe('one-time-code');
    expect(passkeyMutations.enrol).toHaveBeenCalledWith(
      { proof: { kind: 'code', code: '123456' } },
      expect.anything(),
    );
  });

  it('removes a passkey with the same selection', async () => {
    factor.confirmed = true;
    const { label } = await submitProof('Remove passkey passkey', '654321');
    expect(label).toBe('Authenticator code');
    expect(passkeyMutations.remove).toHaveBeenCalledWith(
      { id: 'wacred_1', proof: { kind: 'code', code: '654321' } },
      expect.anything(),
    );
  });
});

function button(container: HTMLElement, label: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find((candidate) => candidate.textContent?.trim() === label);
  if (found === undefined) throw new Error(`No ${label} button`);
  return found;
}

async function securityView() {
  const view = await renderForm(<MemoryRouter><AccountSecurity /></MemoryRouter>);
  unmount = view.unmount;
  return view;
}

function socialOnly() {
  factor.has_password = false;
  factor.has_passkey = false;
  security.identities.push({ id: 'identity-github', kind: 'oauth2', provider_slug: 'shared', display_name: 'GitHub', issuer: 'https://github.com', subject: '123', created_at: '2026-09-01T00:00:00Z' });
}

describe('social-only account security', () => {
  it('asks for fresh proof after a password write is refused instead of reusing the rejected proof', async () => {
    security.password.mockImplementation((_input: { password: string; proof?: string }, callbacks: { onError: (error: Error) => void }) => callbacks.onError(new Error('refused')));
    const view = await securityView();
    await act(async () => button(view.container, 'Change password').click());
    const proof = view.container.querySelector<HTMLInputElement>('dialog input');
    if (proof === null) throw new Error('No proof input');
    await act(async () => typeInto(proof, 'rejected-proof'));
    await act(async () => button(view.container, 'Confirm').click());
    for (const input of view.container.querySelectorAll<HTMLInputElement>('dialog input')) await act(async () => typeInto(input, 'long-new-password'));
    const form = view.container.querySelector('dialog form');
    if (form === null) throw new Error('No password form');
    await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    const retry = view.container.querySelector<HTMLInputElement>('dialog input');
    expect(retry?.autocomplete).toBe('current-password');
    expect(retry?.value).toBe('');
  });

  it('keeps password inventory unknown until its read succeeds', async () => {
    factor.loaded = false;
    const view = await securityView();
    expect(view.container.textContent).toContain('Loading password state');
    expect(view.container.textContent).not.toContain('No password is set');
    expect(button(view.container, 'Loading…').disabled).toBe(true);
    factor.failed = true;
    await view.rerender(<MemoryRouter><AccountSecurity /></MemoryRouter>);
    expect(view.container.textContent).toContain('Password state is unavailable');
    expect(view.container.textContent).not.toContain('No password is set');
    expect(button(view.container, 'Unavailable').disabled).toBe(true);
    factor.loaded = true; factor.failed = false;
    await view.rerender(<MemoryRouter><AccountSecurity /></MemoryRouter>);
    expect(button(view.container, 'Change password').disabled).toBe(false);
  });

  it('explains an incompatible passkey-only inventory instead of asking for a missing code', async () => {
    factor.has_password = false;
    const view = await securityView();
    await act(async () => button(view.container, 'Set password').click());
    expect(view.container.querySelector('dialog')?.textContent).toContain('A passkey cannot prove this operation');
    expect(view.container.querySelector('dialog input')).toBeNull();
  });

  it.each(['Set password', 'add', 'enrol', 'Replace recovery codes'])('offers establish for %s without inventing a local proof', async (action) => {
    socialOnly();
    const view = await securityView();
    await act(async () => button(view.container, action).click());
    expect(view.container.querySelector('dialog input')).toBeNull();
    expect(view.container.querySelector('dialog')?.textContent).toContain('Provider verification authorises one local credential change');
    expect(view.container.querySelector('dialog')?.textContent).not.toContain('fresh proof from your existing password');
    await act(async () => button(view.container, 'Verify with GitHub').click());
    expect(security.establish).toHaveBeenCalledWith({ kind: 'oauth2', slug: 'shared' }, expect.anything());
  });

  it('keeps separate linked identities for one provider without duplicate React keys', async () => {
    socialOnly();
    const first = security.identities[0];
    if (first === undefined) throw new Error('No linked identity');
    security.identities.push({ ...first, id: 'identity-github-second', subject: '456' });
    const errors = vi.spyOn(console, 'error');
    try {
      const view = await securityView();
      await act(async () => button(view.container, 'Set password').click());
      expect([...view.container.querySelectorAll('dialog button')].filter((candidate) => candidate.textContent === 'Verify with GitHub')).toHaveLength(2);
      expect(errors.mock.calls.flat().filter((value) => typeof value === 'string').join(' ')).not.toContain('same key');
    } finally { errors.mockRestore(); }
  });

  it('shows a refused provider start inside the open proof dialog', async () => {
    socialOnly(); security.establishFailed = true;
    const view = await securityView();
    await act(async () => button(view.container, 'Set password').click());
    expect(view.container.querySelector('dialog [role="alert"]')?.textContent).toContain('The account surface could not be reached');
  });

  it('refuses linking and unlinking with the local-proof remedy even with an establish stamp', async () => {
    socialOnly();
    factor.establish = { kind: 'oauth2', slug: 'shared', display_name: 'GitHub', expires_at: new Date(Date.now() + 300_000).toISOString() };
    authProviders.values.push({ kind: 'oidc', slug: 'corp', display_name: 'Corporate' });
    const view = await securityView();
    for (const label of ['Link another identity', 'Unlink']) {
      await act(async () => button(view.container, label).click());
      expect(view.container.querySelector('dialog')?.textContent).toContain('Set a password or enrol an authenticator first');
      expect(view.container.querySelector('dialog')?.textContent).not.toContain('Verify with');
      await act(async () => button(view.container, 'Close').click());
    }
    expect(security.link).not.toHaveBeenCalled();
  });

  it('passes empty proof for a verified passkey creation and removes consumed status after inventory refresh', async () => {
    socialOnly();
    factor.establish = { kind: 'oauth2', slug: 'shared', display_name: 'GitHub', expires_at: new Date(Date.now() + 300_000).toISOString() };
    const view = await securityView();
    expect(view.container.textContent).toContain('Verified via GitHub; complete one change within about 5 minutes');
    await act(async () => button(view.container, 'add').click());
    await act(async () => button(view.container, 'Continue').click());
    expect(passkeyMutations.enrol).toHaveBeenCalledWith({ proof: { kind: 'establish' } }, expect.anything());
    factor.establish = undefined;
    await view.rerender(<MemoryRouter><AccountSecurity /></MemoryRouter>);
    expect(view.container.textContent).not.toContain('Verified via GitHub');
  });

  it('does not trust an expired server stamp', async () => {
    socialOnly();
    factor.establish = { kind: 'oauth2', slug: 'shared', display_name: 'GitHub', expires_at: '2000-01-01T00:00:00Z' };
    const view = await securityView();
    expect(view.container.textContent).not.toContain('Verified via GitHub');
    await act(async () => button(view.container, 'Set password').click());
    expect(button(view.container, 'Verify with GitHub')).toBeDefined();
  });

  it('retires the visible verification when its server expiry passes while the dialog is open', async () => {
    vi.useFakeTimers();
    try {
      socialOnly();
      factor.establish = { kind: 'oauth2', slug: 'shared', display_name: 'GitHub', expires_at: new Date(Date.now() + 300_000).toISOString() };
      const view = await securityView();
      await act(async () => button(view.container, 'Set password').click());
      expect(button(view.container, 'Continue')).toBeDefined();
      await act(async () => vi.advanceTimersByTime(300_001));
      expect(view.container.textContent).not.toContain('Verified via GitHub');
      expect(button(view.container, 'Verify with GitHub')).toBeDefined();
      expect(security.refetch).toHaveBeenCalledOnce();
    } finally { vi.useRealTimers(); }
  });

  it('uses the selected kind with a duplicate slug and asks for TOTP when no password exists', async () => {
    factor.has_password = false;
    factor.confirmed = true;
    authProviders.values.push({ kind: 'oidc', slug: 'same', display_name: 'Corporate' }, { kind: 'oauth2', slug: 'same', display_name: 'GitHub' });
    const view = await securityView();
    await act(async () => button(view.container, 'Link another identity').click());
    await act(async () => button(view.container, 'GitHub · oauth2').click());
    const input = view.container.querySelector('dialog input');
    if (!(input instanceof HTMLInputElement)) throw new Error('No proof input');
    expect(input.autocomplete).toBe('one-time-code');
    await act(async () => typeInto(input, '123456'));
    await act(async () => button(view.container, 'Confirm').click());
    expect(security.link).toHaveBeenCalledWith({ provider: 'same', kind: 'oauth2', proof: '123456' }, expect.anything());
  });

  it('sets a new password with an empty proof slot only after live establish confirmation', async () => {
    socialOnly();
    factor.establish = { kind: 'oauth2', slug: 'shared', display_name: 'GitHub', expires_at: new Date(Date.now() + 300_000).toISOString() };
    const view = await securityView();
    await act(async () => button(view.container, 'Set password').click());
    await act(async () => button(view.container, 'Continue').click());
    const inputs = [...view.container.querySelectorAll<HTMLInputElement>('dialog input')];
    expect(inputs.map((input) => input.autocomplete)).toEqual(['new-password', 'new-password']);
    for (const input of inputs) await act(async () => typeInto(input, 'a-new-long-password'));
    const form = view.container.querySelector('dialog form');
    if (form === null) throw new Error('No password form');
    await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(security.password).toHaveBeenCalledWith({ password: 'a-new-long-password' }, expect.anything());
  });
});

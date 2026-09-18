// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { clearNotification, notifyFailure, ToastViewport } from '../app/notifications.tsx';
import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { AccountSecurity } from './AccountSecurity.tsx';

vi.mock('./AccountProfile.tsx', () => ({ AccountProfile: () => null }));

const authProviders = vi.hoisted(() => {
  const values: { kind: string; slug: string; display_name: string }[] = [];
  return { values };
});

const factor = vi.hoisted(() => ({ confirmed: false }));
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
      data: { identities: [] },
    }),
    useLinkIdentity: mutation,
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
      isPending: false,
      isFetching: false,
      isError: false,
      isSuccess: true,
      data: { confirmed: factor.confirmed, pending: false },
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
  factor.confirmed = false;
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
      <>
        <AccountSecurity />
        <ToastViewport />
      </>,
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
  const rendered = await renderForm(<AccountSecurity />);
  unmount = rendered.unmount;
  const buttons = [...rendered.container.querySelectorAll('button')];
  const future = buttons.find((button) => button.textContent?.trim() === 'Link Future provider');
  const known = buttons.find((button) => button.textContent?.trim() === 'Link Known provider');
  expect(future?.disabled).toBe(true);
  expect(known?.disabled).toBe(false);
  await act(async () => future?.click());
  expect(rendered.container.querySelector('[role="dialog"]')).toBeNull();
});

/**
 * The passkey proof dialog asks for the class the server will accept
 * (possession-first): the authenticator code where a confirmed factor stands,
 * the password otherwise, and sends that class and nothing else.
 */
describe('passkey proof selection', () => {
  async function submitProof(open: string, value: string) {
    const rendered = await renderForm(<AccountSecurity />);
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

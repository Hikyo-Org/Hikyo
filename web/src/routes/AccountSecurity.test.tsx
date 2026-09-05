// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { clearNotification, notifyFailure, ToastViewport } from '../app/notifications.tsx';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import { AccountSecurity } from './AccountSecurity.tsx';

const authProviders = vi.hoisted(() => {
  const values: { kind: string; slug: string; display_name: string }[] = [];
  return { values };
});

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
    useEnrolPasskey: mutation,
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
      data: { passkeys: [] },
    }),
    useRegenerateRecoveryCodes: () => ({ ...mutation(), codes: null, dismiss: vi.fn() }),
    useRemovePasskey: mutation,
    useRemoveTotp: mutation,
    useTotpStatus: () => ({
      isPending: false,
      isFetching: false,
      isError: false,
      isSuccess: true,
      data: { confirmed: false, pending: false },
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

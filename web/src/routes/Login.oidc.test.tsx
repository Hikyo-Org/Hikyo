// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { beforeEach, expect, it, vi } from 'vitest';

import { Login } from './Login.tsx';

const mocks = vi.hoisted(() => ({
  login: { mutate: vi.fn(), isPending: false, isError: false },
  oidc: { mutate: vi.fn(), isPending: false, isError: false },
  passkey: { mutate: vi.fn(), isPending: false, isError: false },
  methods: {
    data: {
      local_login_enabled: true,
      providers: [
        { kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' },
        { kind: 'saml', slug: 'sso', display_name: 'SAML SSO' },
      ],
    },
    isError: false,
    refetch: vi.fn(),
  },
  passkeysAvailable: false,
}));

vi.mock('../api/account.ts', () => ({
  useAuthMethods: () => mocks.methods,
}));

vi.mock('../api/session.ts', () => ({
  loginFailureText: () => 'Sign-in failed.',
  useLogin: () => mocks.login,
  useOIDCLogin: () => mocks.oidc,
}));

vi.mock('../api/stepup.ts', () => ({
  passkeysAvailable: () => mocks.passkeysAvailable,
  stepUpFailureText: () => 'Passkey failed.',
  usePasskeyLogin: () => mocks.passkey,
}));

beforeEach(() => {
  mocks.login.mutate.mockReset();
  mocks.oidc.mutate.mockReset();
  mocks.passkey.mutate.mockReset();
  mocks.methods.refetch.mockReset();
  mocks.login.isPending = false;
  mocks.oidc.isPending = false;
  mocks.passkey.isPending = false;
  mocks.methods.isError = false;
  mocks.passkeysAvailable = false;
});

it('offers each configured OIDC provider and starts the selected login', async () => {
  const container = document.createElement('div');
  const root = createRoot(container);
  await act(async () => root.render(<MemoryRouter><Login /></MemoryRouter>));
  const button = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Continue with Corporate IdP',
  );

  expect(button).toBeDefined();
  expect(container.textContent).not.toContain('Continue with SAML SSO');
  await act(async () => button?.click());
  expect(mocks.oidc.mutate).toHaveBeenCalledWith('strict');
  await act(async () => root.unmount());
});

it.each([
  ['passkey', () => (mocks.passkey.isPending = true)],
  ['OIDC', () => (mocks.oidc.isPending = true)],
])('disables every login control while a %s ceremony is pending', async (_method, setPending) => {
  mocks.passkeysAvailable = true;
  setPending();
  const container = document.createElement('div');
  const root = createRoot(container);

  await act(async () => root.render(<MemoryRouter><Login /></MemoryRouter>));

  const controls = container.querySelectorAll<HTMLInputElement | HTMLButtonElement>('input, button');
  expect(controls.length).toBe(5);
  for (const control of controls) {
    expect(control.disabled).toBe(true);
  }

  await act(async () => root.unmount());
});

it('shows and retries an identity-provider discovery failure', async () => {
  mocks.methods.isError = true;
  const container = document.createElement('div');
  const root = createRoot(container);

  await act(async () => root.render(<MemoryRouter><Login /></MemoryRouter>));

  expect(container.textContent).toContain('Identity provider options could not be loaded.');
  const retry = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Retry identity providers',
  );
  expect(retry).toBeDefined();
  await act(async () => retry?.click());
  expect(mocks.methods.refetch).toHaveBeenCalledOnce();

  await act(async () => root.unmount());
});

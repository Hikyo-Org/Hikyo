// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeEach, expect, it, vi } from 'vitest';

import { Login } from './Login.tsx';

const mocks = vi.hoisted(() => ({ oidc: vi.fn() }));

vi.mock('../api/account.ts', () => ({
  useAuthMethods: () => ({
    data: {
      local_login_enabled: true,
      providers: [
        { kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' },
        { kind: 'saml', slug: 'sso', display_name: 'SAML SSO' },
      ],
    },
  }),
}));

vi.mock('../api/session.ts', () => ({
  loginFailureText: () => 'Sign-in failed.',
  useLogin: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
  useOIDCLogin: () => ({ mutate: mocks.oidc, isPending: false, isError: false }),
}));

vi.mock('../api/stepup.ts', () => ({
  passkeysAvailable: () => false,
  stepUpFailureText: () => 'Passkey failed.',
  usePasskeyLogin: () => ({ mutate: vi.fn(), isPending: false, isError: false }),
}));

beforeEach(() => mocks.oidc.mockReset());

it('offers each configured OIDC provider and starts the selected login', async () => {
  const container = document.createElement('div');
  const root = createRoot(container);
  await act(async () => root.render(<Login />));
  const button = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Continue with Corporate IdP',
  );

  expect(button).toBeDefined();
  expect(container.textContent).not.toContain('Continue with SAML SSO');
  await act(async () => button?.click());
  expect(mocks.oidc).toHaveBeenCalledWith('strict');
  await act(async () => root.unmount());
});

// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { Login } from './Login.tsx';

function mount(container: HTMLElement) {
  const root = createRoot(container);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return {
    render: () =>
      act(async () =>
        root.render(
          <QueryClientProvider client={client}>
            <MemoryRouter>
              <Login />
            </MemoryRouter>
          </QueryClientProvider>,
        ),
      ),
    unmount: () => act(async () => root.unmount()),
  };
}

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
    isPending: false,
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
  mocks.methods.isPending = false;
  mocks.passkeysAvailable = false;
});

afterEach(() => vi.unstubAllGlobals());

it('offers each configured OIDC and SAML provider and starts the selected login', async () => {
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  const button = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Continue with Corporate IdP',
  );

  expect(button).toBeDefined();
  expect(container.textContent).toContain('Continue with SAML SSO');
  await act(async () => button?.click());
  expect(mocks.oidc.mutate).toHaveBeenCalledWith('strict');
  await unmount();
});

it('starts a SAML login through the SP-initiated redirect', async () => {
  const fetchMock = vi.fn((_request: RequestInfo | URL) =>
    Promise.resolve(Response.json({ redirect_url: 'https://idp.example/sso' })),
  );
  vi.stubGlobal('fetch', fetchMock);
  const assign = vi.fn();
  vi.stubGlobal('location', { ...globalThis.location, assign });
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  const button = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Continue with SAML SSO',
  );
  await act(async () => button?.click());
  for (let round = 0; round < 10; round += 1) await act(async () => Promise.resolve());

  const request = fetchMock.mock.calls[0]?.[0];
  expect(request).toBeInstanceOf(Request);
  if (request instanceof Request) {
    expect(new URL(request.url).pathname).toBe('/api/v1/auth/saml/sso/start');
    expect(await request.json()).toEqual({ purpose: 'login' });
  }
  expect(assign).toHaveBeenCalledWith('https://idp.example/sso');
  await unmount();
});

it.each([
  ['passkey', () => (mocks.passkey.isPending = true)],
  ['OIDC', () => (mocks.oidc.isPending = true)],
])('disables every control while a %s ceremony is pending, with the busy label as the reason', async (_method, setPending) => {
  mocks.passkeysAvailable = true;
  setPending();
  const container = document.createElement('div');
  const { render, unmount } = mount(container);

  await render();

  const buttons = container.querySelectorAll('button');
  expect(buttons.length).toBe(4);
  for (const button of buttons) {
    expect(button.disabled).toBe(true);
  }
  const inputs = container.querySelectorAll('input');
  expect(inputs.length).toBe(2);
  // A ceremony ends in a redirect or a session change; a second submission
  // racing it is the failure mode, and the busy button label says why.
  for (const input of inputs) {
    expect(input.disabled).toBe(true);
  }

  await unmount();
});

it('keys the busy label on the provider being contacted, not on every provider', async () => {
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  mocks.oidc.mutate.mockImplementation(() => {
    mocks.oidc.isPending = true;
  });
  const corporate = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Continue with Corporate IdP',
  );
  await act(async () => corporate?.click());
  await render();

  const labels = [...container.querySelectorAll('button')].map((button) => button.textContent);
  expect(labels).toContain('Contacting identity provider…');
  expect(labels).toContain('Continue with SAML SSO');
  expect(labels).not.toContain('Continue with Corporate IdP');
  await unmount();
});

it('shows a loading line while the sign-in methods are pending', async () => {
  mocks.methods.isPending = true;
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  expect(container.querySelector('[role="status"]')?.textContent).toBe('Loading sign-in methods…');
  await unmount();
});

it('demotes the setup and recovery links to quiet links', async () => {
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  const links = [...container.querySelectorAll('.login__links a')];
  expect(links.map((link) => link.textContent)).toEqual([
    'Have a setup authority? Establish your credential',
    'Lost your second factor? Recover with a code',
  ]);
  expect(links.every((link) => !link.classList.contains('btn'))).toBe(true);
  await unmount();
});

it('shows and retries an identity-provider discovery failure', async () => {
  mocks.methods.isError = true;
  const container = document.createElement('div');
  const { render, unmount } = mount(container);

  await render();

  expect(container.textContent).toContain('Identity provider options could not be loaded.');
  const retry = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Retry identity providers',
  );
  expect(retry).toBeDefined();
  await act(async () => retry?.click());
  expect(mocks.methods.refetch).toHaveBeenCalledOnce();

  await unmount();
});

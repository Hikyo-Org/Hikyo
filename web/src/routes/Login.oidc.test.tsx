// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, expect, it, vi, type Mock } from 'vitest';

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

/** One sign-in leg's hook surface, as the route consumes it. */
type LegMock = {
  mutate: Mock;
  reset: Mock;
  isPending: boolean;
  isError: boolean;
  error: Error | null;
};
type Mocks = {
  login: LegMock;
  oidc: LegMock;
  passkey: LegMock;
  challengeTotp: LegMock;
  challengePasskey: LegMock;
  methods: {
    data: {
      local_login_enabled: boolean;
      providers: { kind: string; slug: string; display_name: string }[];
      signup_paused: boolean;
    };
    isError: boolean;
    isPending: boolean;
    refetch: Mock;
  };
  passkeysAvailable: boolean;
};

const mocks = vi.hoisted((): Mocks => ({
  login: { mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null },
  oidc: { mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null },
  passkey: { mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null },
  challengeTotp: { mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null },
  challengePasskey: { mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false, error: null },
  methods: {
    data: {
      local_login_enabled: true,
      providers: [
        { kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' },
        { kind: 'saml', slug: 'sso', display_name: 'SAML SSO' },
      ],
      signup_paused: false,
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
  // Echoes the cause so a test can tell WHICH leg's refusal reached the slot.
  loginFailureText: (error?: Error | null) => error?.message ?? 'Sign-in failed.',
  useLogin: () => mocks.login,
  useLoginChallengeTotp: () => mocks.challengeTotp,
  useOIDCLogin: () => mocks.oidc,
}));

vi.mock('../api/stepup.ts', () => ({
  passkeysAvailable: () => mocks.passkeysAvailable,
  passkeyFailureText: () => 'Passkey failed.',
  stepUpFailureText: () => 'Code failed.',
  useLoginChallengePasskey: () => mocks.challengePasskey,
  usePasskeyLogin: () => mocks.passkey,
}));

beforeEach(() => {
  mocks.login.mutate.mockReset();
  mocks.oidc.mutate.mockReset();
  mocks.passkey.mutate.mockReset();
  mocks.challengeTotp.mutate.mockReset();
  mocks.challengePasskey.mutate.mockReset();
  mocks.challengeTotp.isError = false;
  mocks.challengePasskey.isError = false;
  mocks.challengePasskey.isPending = false;
  mocks.methods.refetch.mockReset();
  mocks.login.reset.mockReset();
  mocks.oidc.reset.mockReset();
  mocks.passkey.reset.mockReset();
  mocks.login.error = null;
  mocks.oidc.error = null;
  mocks.passkey.error = null;
  mocks.oidc.isError = false;
  mocks.login.isPending = false;
  mocks.oidc.isPending = false;
  mocks.passkey.isPending = false;
  mocks.methods.isError = false;
  mocks.methods.isPending = false;
  mocks.login.isError = false;
  mocks.passkey.isError = false;
  mocks.passkeysAvailable = false;
  mocks.methods.data.signup_paused = false;
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

it('says only "Sign-up is paused." while the registration policy is inactive', async () => {
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  expect(container.textContent).not.toContain('Sign-up is paused.');
  await unmount();

  mocks.methods.data.signup_paused = true;
  const paused = document.createElement('div');
  const second = mount(paused);
  await second.render();
  const line = paused.querySelector('.login__paused');
  expect(line?.textContent).toBe('Sign-up is paused.');
  // The cause never reaches the public page.
  for (const cause of ['authority-lost', 'mailer', 'precondition', 'inactive']) {
    expect(paused.textContent?.toLowerCase()).not.toContain(cause);
  }
  await second.unmount();
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

// The card has ONE refusal slot, so the route picks which failure speaks:
// the password leg first, then the passkey leg. Each keeps its own wording.
it.each([
  ['password', () => (mocks.login.isError = true), 'Sign-in failed.'],
  ['passkey', () => (mocks.passkey.isError = true), 'Passkey failed.'],
])('announces a %s refusal inside the card, in that leg’s own words', async (_leg, fail, text) => {
  mocks.passkeysAvailable = true;
  fail();
  const container = document.createElement('div');
  const { render, unmount } = mount(container);

  await render();

  const alert = container.querySelector('.login__card [role="alert"]');
  expect(alert?.textContent).toContain(text);
  await unmount();
});

// Ruled: the latest attempt owns the slot. A refusal that described an earlier
// attempt must not outlive it, nor mask the failure the person is waiting on.
it('replaces a stale password refusal with the provider refusal that followed it', async () => {
  mocks.login.isError = true;
  mocks.login.reset.mockImplementation(() => {
    mocks.login.isError = false;
  });
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  expect(container.textContent).toContain('Sign-in failed.');

  mocks.oidc.mutate.mockImplementation(() => {
    mocks.oidc.isError = true;
    mocks.oidc.error = new Error('The identity provider refused.');
  });
  const corporate = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === 'Continue with Corporate IdP',
  );
  await act(async () => corporate?.click());
  await render();

  expect(mocks.login.reset).toHaveBeenCalled();
  const alert = container.querySelector('.login__card [role="alert"]');
  expect(alert?.textContent).toContain('The identity provider refused.');
  expect(container.textContent).not.toContain('Sign-in failed.');
  await unmount();
});

// The two provider protocols are separate legs of the SAME slot: a SAML
// refusal must not sit in the card while an OIDC attempt runs. The SAML leg
// here is the REAL useSensitiveMutation the route owns, driven into failure by
// a rejecting transport, so this exercises the actual reset path.
it('clears a SAML refusal when an OIDC attempt starts', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.reject(new Error('The identity provider refused.'))),
  );
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();

  const named = (text: string) =>
    [...container.querySelectorAll('button')].find((button) => button.textContent === text);
  await act(async () => named('Continue with SAML SSO')?.click());
  for (let round = 0; round < 10; round += 1) await act(async () => Promise.resolve());
  await render();
  // The transport failure is worded by the SDK, not by this test; what matters
  // is that the SAML leg put SOMETHING in the card's one refusal slot.
  expect(container.querySelector('.login__card [role="alert"]')).not.toBeNull();

  // The OIDC mutate leaves its own leg idle: whatever is in the slot after the
  // click is what survived the attempt, and nothing should have.
  await act(async () => named('Continue with Corporate IdP')?.click());
  await render();

  expect(mocks.oidc.mutate).toHaveBeenCalledWith('strict');
  expect(container.querySelector('.login__card [role="alert"]')).toBeNull();
  await unmount();
});

/** Submit the password form and answer it with a #760 login challenge. */
async function answerWithChallenge(container: HTMLElement, factors: string[]) {
  mocks.login.mutate.mockImplementation(
    (_credentials: unknown, callbacks: { onSuccess: (outcome: unknown) => void }) =>
      callbacks.onSuccess({
        kind: 'challenge',
        challenge: { challenge_id: 'lch_1', expires_at: '2026-09-23T00:05:00Z', factors },
        username: 'alex',
      }),
  );
  const form = container.querySelector('form');
  await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
}

const buttonNamed = (container: HTMLElement, text: string) =>
  [...container.querySelectorAll('button')].find((button) => button.textContent === text);

it('presents a passkey as the second factor when one is enrolled and the platform can assert', async () => {
  mocks.passkeysAvailable = true;
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  await answerWithChallenge(container, ['totp', 'webauthn']);

  expect(container.textContent).toContain('Present your second factor');
  const passkey = buttonNamed(container, 'Use a passkey');
  expect(passkey).toBeDefined();
  await act(async () => passkey?.click());
  expect(mocks.challengeTotp.reset).toHaveBeenCalled();
  expect(mocks.challengePasskey.mutate).toHaveBeenCalled();
  await unmount();
});

it('offers no passkey button when the challenge does not accept one', async () => {
  mocks.passkeysAvailable = true;
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  await answerWithChallenge(container, ['totp']);

  expect(container.textContent).toContain('Present your second factor');
  expect(buttonNamed(container, 'Use a passkey')).toBeUndefined();
  await unmount();
});

it('shows the passkey leg refusal in the challenge slot', async () => {
  mocks.passkeysAvailable = true;
  mocks.challengePasskey.isError = true;
  mocks.challengePasskey.error = new Error('dismissed');
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  await answerWithChallenge(container, ['webauthn']);

  expect(container.querySelector('.login__card [role="alert"]')?.textContent).toContain('Passkey failed.');
  await unmount();
});

it('names an expired challenge instead of a server error', async () => {
  const { ApiError } = await import('../api/client.ts');
  mocks.challengeTotp.isError = true;
  mocks.challengeTotp.error = new ApiError(404, 'not found');
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  await answerWithChallenge(container, ['totp']);

  expect(container.querySelector('.login__card [role="alert"]')?.textContent).toContain(
    'This sign-in expired or was already completed.',
  );
  await unmount();
});

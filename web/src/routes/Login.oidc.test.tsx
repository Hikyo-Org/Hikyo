// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, expect, it, vi, type Mock } from 'vitest';

import { installMemoryStorage } from '../testkit/storage.ts';
import { Login } from './Login.tsx';

function mount(container: HTMLElement, page: { intent?: 'sign-in' | 'sign-up'; url?: string } = {}) {
  const root = createRoot(container);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return {
    render: () =>
      act(async () =>
        root.render(
          <QueryClientProvider client={client}>
            <MemoryRouter initialEntries={[page.url ?? '/login']}>
              <Login intent={page.intent} />
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
      providers: { kind: string; slug: string; display_name: string; brand?: 'google' | 'microsoft' }[];
      signup_open: boolean;
      signup_paused: boolean;
      signup_methods: ({ kind: string; slug: string } | 'local')[];
      signup_landing?: 'org-template' | 'none' | 'fresh-org';
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
      signup_open: false,
      signup_paused: false,
      signup_methods: [],
    },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  },
  passkeysAvailable: false,
}));

const methodsFor = vi.hoisted(() => vi.fn());
vi.mock('../api/account.ts', () => ({
  useAuthMethods: (org?: string) => {
    methodsFor(org);
    return mocks.methods;
  },
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
  // The remembered way in is per-browser state; a test that starts a leg
  // would leave it behind for the next one, so each gets a fresh store.
  installMemoryStorage();
  mocks.methods.data.signup_open = false;
  mocks.methods.data.signup_methods = [];
  mocks.methods.data.signup_landing = undefined;
  mocks.methods.data.providers = [
    { kind: 'oidc', slug: 'strict', display_name: 'Corporate IdP' },
    { kind: 'saml', slug: 'sso', display_name: 'SAML SSO' },
  ];
  methodsFor.mockReset();
});

/** The staged entry's step one names the password row; step two is its form. */
async function openPassword(container: HTMLElement) {
  const row = [...container.querySelectorAll('button')].find((button) => button.textContent?.startsWith('Password'));
  await act(async () => row?.click());
}

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
  // The sign-in door's rows start `sign-in`, which never creates an account.
  expect(mocks.oidc.mutate).toHaveBeenCalledWith({ provider: 'strict', intent: 'sign-in', signupOrg: undefined });
  await unmount();
});

it('opens the password form from its row, and comes back to the rows', async () => {
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  expect(container.querySelector('form')).toBeNull();
  expect(container.querySelector('h1')?.textContent).toBe('Sign in to Hikyo');
  await act(async () => buttonNamed(container, 'Passwordusername')?.click());
  expect(container.querySelectorAll('input').length).toBe(2);
  expect(container.querySelector('h1')?.textContent).toBe('Sign in with a password');
  await act(async () => buttonNamed(container, '‹ Other ways to sign in')?.click());
  expect(container.querySelector('form')).toBeNull();
  await unmount();
});

it('badges the row this browser used last time, and remembers the one chosen now', async () => {
  globalThis.localStorage.setItem('hikyo.last-sign-in', 'provider:saml:sso');
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  const badged = [...container.querySelectorAll('button')].filter((button) =>
    button.textContent?.includes('Last used'),
  );
  expect(badged.map((button) => button.textContent)).toEqual(['Continue with SAML SSOLast used']);

  await act(async () => buttonNamed(container, 'Continue with Corporate IdP')?.click());
  expect(globalThis.localStorage.getItem('hikyo.last-sign-in')).toBe('provider:oidc:strict');
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

  // Step one of the staged entry: the password row, the passkey row, and one
  // row per provider. A ceremony ends in a redirect or a session change; a
  // second start racing it is the failure mode, and the busy label says why.
  const buttons = container.querySelectorAll('button');
  expect(buttons.length).toBe(4);
  for (const button of buttons) {
    expect(button.disabled).toBe(true);
  }
  expect(container.querySelectorAll('input').length).toBe(0);

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
  const line = paused.querySelector('.login__card .login__paused');
  expect(line?.textContent).toBe('Sign-up is paused.');
  expect(line?.getAttribute('role')).toBe('status');
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

  expect(mocks.oidc.mutate).toHaveBeenCalledWith({ provider: 'strict', intent: 'sign-in', signupOrg: undefined });
  expect(container.querySelector('.login__card [role="alert"]')).toBeNull();
  await unmount();
});

const buttonNamed = (container: HTMLElement, text: string) =>
  [...container.querySelectorAll('button')].find((button) => button.textContent === text);

/** Open the password step, submit its form and answer it with a #760 login challenge. */
async function answerWithChallenge(container: HTMLElement, factors: string[]) {
  await act(async () => buttonNamed(container, 'Passwordusername')?.click());
  mocks.login.mutate.mockImplementation(
    (_credentials: unknown, callbacks: { onSuccess: (outcome: unknown) => void }) =>
      callbacks.onSuccess({
        kind: 'challenge',
        challenge: { challenge_id: 'lch_1', expires_at: '2026-09-23T00:05:00Z', factors },
        username: 'alex',
      }),
  );
  await openPassword(container);
  const form = container.querySelector('form');
  await act(async () => form?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
}

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

it('shows the password form only after the password row is chosen, and goes back', async () => {
  const container = document.createElement('div');
  // Attached, so focus can land in it.
  document.body.append(container);
  const { render, unmount } = mount(container);
  await render();
  expect(container.querySelector('input')).toBeNull();
  await openPassword(container);
  expect(container.querySelectorAll('input').length).toBe(2);
  expect(container.textContent).toContain('Sign in with a password');
  // Each step change moves focus to the new step's heading.
  expect(document.activeElement?.textContent).toBe('Sign in with a password');
  await act(async () => buttonNamed(container, '‹ Other ways to sign in')?.click());
  expect(container.querySelector('input')).toBeNull();
  expect(document.activeElement?.textContent).toBe('Sign in to Hikyo');
  // The back glyph is decorative: the control is named by its words.
  await openPassword(container);
  expect(container.querySelector('.login__back [aria-hidden="true"]')?.textContent).toBe('‹ ');
  await unmount();
  container.remove();
});

it('renders no sign-up door while registration is closed', async () => {
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();
  // The page rendered (the absence below is not vacuous).
  expect(container.querySelector('h1')?.textContent).toBe('Sign in to Hikyo');
  expect(buttonNamed(container, 'Continue with Corporate IdP')).toBeDefined();
  expect(container.textContent).not.toContain('Create an account');
  expect(container.textContent).not.toContain('New here?');
  await unmount();
});

it('opens the door, confirms, and starts a sign-up only from the confirmation step', async () => {
  mocks.methods.data.signup_open = true;
  mocks.methods.data.signup_methods = [{ kind: 'oidc', slug: 'strict' }];
  mocks.methods.data.signup_landing = 'fresh-org';
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();

  await act(async () => buttonNamed(container, 'Create an account')?.click());
  expect(container.querySelector('h1')?.textContent).toBe('Create an account');
  expect(container.textContent).toContain('You’ll get your own organisation, with you as its first administrator.');
  // Only the providers the policy admits: SAML never signs up.
  expect(container.textContent).not.toContain('SAML SSO');
  await act(async () => buttonNamed(container, 'Continue with Corporate IdP')?.click());
  expect(mocks.oidc.mutate).not.toHaveBeenCalled();

  expect(container.querySelector('h1')?.textContent).toBe('Create an account with Corporate IdP');
  expect(container.textContent).toContain(
    'This creates a new account. Already have one? Sign in with it first, then add Corporate IdP under Settings › Security.',
  );
  await act(async () => buttonNamed(container, 'Continue to Corporate IdP')?.click());
  expect(mocks.oidc.mutate).toHaveBeenCalledWith({ provider: 'strict', intent: 'sign-up', signupOrg: undefined });
  await unmount();
});

it('addresses the org door from /signup?org= and carries the org into the start', async () => {
  mocks.methods.data.signup_open = true;
  mocks.methods.data.signup_methods = [{ kind: 'oidc', slug: 'strict' }, 'local'];
  mocks.methods.data.signup_landing = 'org-template';
  const container = document.createElement('div');
  const { render, unmount } = mount(container, { intent: 'sign-up', url: '/signup?org=org_acme' });
  await render();

  expect(methodsFor).toHaveBeenCalledWith('org_acme');
  expect(container.querySelector('h1')?.textContent).toBe('Create an account');
  expect(container.textContent).toContain('You’ll join this organisation.');
  await act(async () => buttonNamed(container, 'Continue with Corporate IdP')?.click());
  await act(async () => buttonNamed(container, 'Continue to Corporate IdP')?.click());
  expect(mocks.oidc.mutate).toHaveBeenCalledWith({ provider: 'strict', intent: 'sign-up', signupOrg: 'org_acme' });
  await unmount();
});

it('starts an OIDC sign-up even when a SAML provider shares the slug', async () => {
  // Slugs are unique per kind only; the door admits the OIDC kind alone.
  mocks.methods.data.providers = [
    { kind: 'saml', slug: 'corp', display_name: 'Corp SAML' },
    { kind: 'oidc', slug: 'corp', display_name: 'Corp OIDC' },
  ];
  mocks.methods.data.signup_open = true;
  mocks.methods.data.signup_methods = [{ kind: 'oidc', slug: 'corp' }];
  mocks.methods.data.signup_landing = 'none';
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();

  await act(async () => buttonNamed(container, 'Create an account')?.click());
  expect(container.textContent).not.toContain('Corp SAML');
  await act(async () => buttonNamed(container, 'Continue with Corp OIDC')?.click());
  expect(container.querySelector('h1')?.textContent).toBe('Create an account with Corp OIDC');
  await act(async () => buttonNamed(container, 'Continue to Corp OIDC')?.click());
  expect(mocks.oidc.mutate).toHaveBeenCalledWith({ provider: 'corp', intent: 'sign-up', signupOrg: undefined });
  await unmount();
});

it('falls back to the sign-up door when the refreshed door drops the chosen provider', async () => {
  mocks.methods.data.providers = [
    { kind: 'oidc', slug: 'corp', display_name: 'Corp OIDC' },
    { kind: 'oidc', slug: 'other', display_name: 'Other IdP' },
  ];
  mocks.methods.data.signup_open = true;
  mocks.methods.data.signup_methods = [
    { kind: 'oidc', slug: 'corp' },
    { kind: 'oidc', slug: 'other' },
  ];
  mocks.methods.data.signup_landing = 'none';
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();

  await act(async () => buttonNamed(container, 'Create an account')?.click());
  await act(async () => buttonNamed(container, 'Continue with Corp OIDC')?.click());
  expect(container.querySelector('h1')?.textContent).toBe('Create an account with Corp OIDC');
  // Discovery refreshed: the policy still admits a provider, just not this
  // one. The card falls back to the door, not to a blank.
  mocks.methods.data = { ...mocks.methods.data, signup_methods: [{ kind: 'oidc', slug: 'other' }] };
  await render();
  expect(container.querySelector('h1')?.textContent).toBe('Create an account');
  expect(buttonNamed(container, 'Continue with Other IdP')).toBeDefined();
  expect(container.textContent).not.toContain('Corp OIDC');
  await unmount();
});

// Slugs are unique per kind only: the row pressed, not its slug, names the
// protocol, the busy label and nothing else.
const sharedSlug = [
  { kind: 'saml', slug: 'corp', display_name: 'Corp SAML' },
  { kind: 'oidc', slug: 'corp', display_name: 'Corp OIDC' },
];

it('starts an OIDC sign-in, and marks only its row, when a SAML provider shares the slug', async () => {
  mocks.methods.data.providers = sharedSlug;
  const fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  mocks.oidc.mutate.mockImplementation(() => {
    mocks.oidc.isPending = true;
  });
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();

  await act(async () => buttonNamed(container, 'Continue with Corp OIDC')?.click());
  await render();
  expect(mocks.oidc.mutate).toHaveBeenCalledWith({ provider: 'corp', intent: 'sign-in', signupOrg: undefined });
  expect(fetchMock).not.toHaveBeenCalled();
  const labels = [...container.querySelectorAll('button')].map((button) => button.textContent);
  expect(labels.filter((label) => label === 'Contacting identity provider…')).toHaveLength(1);
  expect(labels).toContain('Continue with Corp SAML');
  await unmount();
});

it('starts a SAML sign-in when an OIDC provider shares the slug', async () => {
  mocks.methods.data.providers = sharedSlug;
  const fetchMock = vi.fn((_request: RequestInfo | URL) =>
    Promise.resolve(Response.json({ redirect_url: 'https://idp.example/sso' })),
  );
  vi.stubGlobal('fetch', fetchMock);
  vi.stubGlobal('location', { ...globalThis.location, assign: vi.fn() });
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();

  await act(async () => buttonNamed(container, 'Continue with Corp SAML')?.click());
  for (let round = 0; round < 10; round += 1) await act(async () => Promise.resolve());
  const request = fetchMock.mock.calls[0]?.[0];
  expect(request).toBeInstanceOf(Request);
  if (request instanceof Request) expect(new URL(request.url).pathname).toBe('/api/v1/auth/saml/corp/start');
  expect(mocks.oidc.mutate).not.toHaveBeenCalled();
  await unmount();
});

it('opens /signup on sign-in when the addressed door is closed', async () => {
  const container = document.createElement('div');
  const { render, unmount } = mount(container, { intent: 'sign-up', url: '/signup?org=org_nope' });
  await render();
  expect(container.querySelector('h1')?.textContent).toBe('Sign in to Hikyo');
  await unmount();
});

it('follows the providers’ published button rules', async () => {
  mocks.methods.data.providers = [
    { kind: 'oidc', slug: 'google', display_name: 'Google', brand: 'google' },
    { kind: 'oidc', slug: 'contoso', display_name: 'Contoso', brand: 'microsoft' },
    { kind: 'oidc', slug: 'fabrikam', display_name: 'Fabrikam', brand: 'microsoft' },
    { kind: 'oidc', slug: 'corp', display_name: 'Corp SSO' },
  ];
  mocks.methods.data.signup_open = true;
  mocks.methods.data.signup_methods = [{ kind: 'oidc', slug: 'google' }, { kind: 'oidc', slug: 'contoso' }];
  const container = document.createElement('div');
  const { render, unmount } = mount(container);
  await render();

  // The tenant span is inline-block, so the accessible name reads "Microsoft ·
  // Contoso" while the raw text runs together; the rows are compared as read.
  const labels = () =>
    [...container.querySelectorAll('.login__methods .login__brand')].map((button) =>
      (button.textContent ?? '').replace(/\s*·\s*/, ' · '),
    );
  // One Microsoft row per Entra tenant row, the tenant named after the mark.
  expect(labels()).toEqual([
    'Continue with Google',
    'Sign in with Microsoft · Contoso',
    'Sign in with Microsoft · Fabrikam',
    'Continue with Corp SSO',
  ]);
  expect(container.querySelector('.login__brand--google svg')).not.toBeNull();
  expect(container.textContent).toContain('Microsoft: work or school account');

  await act(async () => buttonNamed(container, 'Create an account')?.click());
  // Google permits "Sign up with"; Microsoft keeps "Sign in with Microsoft".
  expect(labels()).toEqual([
    'Sign up with Google',
    'Sign in with Microsoft · Contoso',
  ]);
  await unmount();
});

import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zAccountProfile,
  zAuthMethods,
  zIdentityList,
  zPasskeyList,
  zSessionList,
  zTotpStatus,
} from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { AccountSecurity } from './AccountSecurity.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The page fans out into six reads under the real AuthProvider: the profile
// panel, passkeys, the authenticator state, linked identities, the configured
// providers and the session list. Every mutation is a POST/DELETE the play
// never fires (it would reach the WebAuthn prompt or redirect to a provider);
// the proof dialog opens purely client-side. Bodies are typed against the
// response schemas so contract drift fails in tsc before the browser run.
const profile = {
  username: 'alice',
  display_name: 'Alice Example',
  email: 'alice@example.com',
  email_verified: true,
  managed: false,
  username_editable: true,
} satisfies z.input<typeof zAccountProfile>;

const passkeys = {
  passkeys: [
    {
      id: 'pk_01989abc-def0-7123-8123-000000000001',
      label: 'YubiKey 5C',
      discoverable: true,
      disabled: false,
      created_at: '2026-08-22T10:00:00Z',
      last_used_at: '2026-09-01T09:00:00Z',
    },
    {
      id: 'pk_01989abc-def0-7123-8123-000000000002',
      label: 'MacBook Touch ID',
      discoverable: true,
      disabled: true,
      created_at: '2026-06-02T08:00:00Z',
      last_used_at: '2026-08-30T17:00:00Z',
    },
  ],
} satisfies z.input<typeof zPasskeyList>;

const identities = {
  identities: [
    {
      id: 'idn_01989abc-def0-7123-8123-000000000001',
      kind: 'oidc',
      issuer: 'https://idp.example.com',
      subject: 'alice@example.com',
      provider_id: 'corp',
      created_at: '2026-06-02T08:00:00Z',
    },
  ],
} satisfies z.input<typeof zIdentityList>;

const providers = {
  local_login_enabled: true,
  signup_open: false, signup_paused: false, signup_methods: [],
  providers: [
    { slug: 'corp', display_name: 'Corporate IdP', kind: 'oidc' },
    { slug: 'sso', display_name: 'SAML SSO', kind: 'saml' },
  ],
} satisfies z.input<typeof zAuthMethods>;

const sessions = {
  items: [
    {
      id: 'ses_123e4567-e89b-12d3-a456-426614174000',
      artifact: 'browser',
      auth_method: 'local-password',
      created_at: '2026-08-22T10:00:00Z',
      last_seen_at: '2026-09-01T09:00:00Z',
      idle_expires_at: '2099-08-22T10:30:00Z',
      absolute_expires_at: '2099-08-22T18:00:00Z',
      source_ip: '192.0.2.10',
      user_agent: 'Firefox 142 on macOS',
    },
    {
      id: 'ses_123e4567-e89b-12d3-a456-426614174001',
      artifact: 'cli',
      auth_method: 'local-password',
      created_at: '2026-08-30T07:00:00Z',
      last_seen_at: '2026-09-01T08:00:00Z',
      idle_expires_at: '2099-08-22T10:30:00Z',
      absolute_expires_at: '2099-08-22T18:00:00Z',
      source_ip: '192.0.2.11',
    },
    {
      id: 'ses_123e4567-e89b-12d3-a456-426614174002',
      artifact: 'workspace',
      auth_method: 'workspace',
      created_at: '2026-09-01T06:00:00Z',
      last_seen_at: '2026-09-01T09:00:00Z',
      idle_expires_at: '2099-08-22T10:30:00Z',
      absolute_expires_at: '2099-08-22T18:00:00Z',
      requesting_origin: 'https://hikyo.went.io',
    },
  ],
  count: 3,
} satisfies z.input<typeof zSessionList>;

const totp = (body: z.input<typeof zTotpStatus>): MockRoute => ({ url: '/api/v1/auth/totp', body });

const reads = {
  profile: '/api/v1/me/profile',
  passkeys: '/api/v1/auth/webauthn/credentials',
  identities: '/api/v1/auth/identities',
  methods: '/api/v1/auth/methods',
  sessions: '/api/v1/me/sessions',
} as const;

const readNames = ['profile', 'passkeys', 'identities', 'methods', 'sessions'] as const;

/** Every read answered from one table of bodies. */
const answered = (bodies: Record<keyof typeof reads, unknown>): MockRoute[] =>
  readNames.map((name) => ({ url: reads[name], body: bodies[name] }));

/** Every read given the same row (pending or a status), the totp one included. */
const everyRead = (rest: Partial<MockRoute>): MockRoute[] =>
  [...Object.values(reads), '/api/v1/auth/totp'].map((url) => ({ url, ...rest }));

const populated: MockRoute[] = [
  ...answered({ profile, passkeys, identities, methods: providers, sessions }),
  totp({ confirmed: true, pending: false }),
];

const meta = {
  component: AccountSecurity,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { auth: true, path: '/settings', responses: populated },
  },
} satisfies Meta<typeof AccountSecurity>;

export default meta;
type Story = StoryObj<typeof meta>;

// Two passkeys (one disabled after a clone signal), an enrolled authenticator,
// one linked identity, two providers to link, and every artifact class holding
// a session.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/YubiKey 5C/)).toBeVisible();
    await expect(canvas.getByText(/disabled after clone signal/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Remove the authenticator' })).toBeVisible();
    await expect(await canvas.findByRole('button', { name: 'Link Corporate IdP' })).toBeVisible();
    await expect(await canvas.findByRole('button', { name: /revoke the workspace session/i })).toBeVisible();
  },
};

// A fresh account: no passkey, no authenticator, nothing linked and no provider
// to link to. Every panel voices its own "none" sentence.
export const Empty: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/settings',
      responses: [
        ...answered({
          profile,
          passkeys: { passkeys: [] },
          identities: { identities: [] },
          methods: { local_login_enabled: true, providers: [], signup_open: false, signup_paused: false, signup_methods: [] },
          sessions: { items: [], count: 0 },
        }),
        totp({ confirmed: false, pending: false }),
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no passkey is enrolled/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'enrol' })).toBeVisible();
    await expect(await canvas.findByText(/no external identity is linked/i)).toBeVisible();
    await expect(await canvas.findByText(/this instance has none enabled/i)).toBeVisible();
    await expect(await canvas.findByText(/no active sessions/i)).toBeVisible();
  },
};

// Nothing has settled: every panel shows its loading status.
export const Loading: Story = {
  parameters: { app: { auth: true, path: '/settings', responses: everyRead({ pending: true }) } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading passkeys/i)).toBeVisible());
    await expect(canvas.getByText(/loading configured identity providers/i)).toBeVisible();
    await expect(canvas.getByText(/loading your profile/i)).toBeVisible();
  },
};

// Every read refused: each panel surfaces its own alert rather than a blank.
export const Failed: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/settings',
      responses: everyRead({ status: 500, body: { error: 'account read failed' } }),
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/your passkeys could not be listed/i)).toBeVisible();
    await expect(await canvas.findByText(/your authenticator state could not be read/i)).toBeVisible();
    await expect(await canvas.findByText(/your sessions could not be loaded/i)).toBeVisible();
    await expect(await canvas.findByText(/your linked identities could not be listed/i)).toBeVisible();
    await expect(await canvas.findByText(/configured identity providers could not be loaded/i)).toBeVisible();
  },
};

// "Confirm it is you": the proof dialog an account-security change opens first.
// Here a confirmed authenticator stands, so the passkey enrolment asks for its code.
export const ProofDialog: Story = {
  play: async ({ canvas }) => {
    // The "add" control renders before any read settles, but AuthProvider
    // remounts the page once whoami answers, and a dialog opened before that
    // is lost with it. The factor row is the read that settles last, so it is
    // the signal the page is stable enough to hold state.
    await expect(await canvas.findByRole('button', { name: 'Remove the authenticator' })).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Add a passkey' }));
    const dialog = await canvas.findByRole('dialog', { name: 'Confirm it is you' });
    await expect(dialog).toBeVisible();
    await expect(canvas.getByLabelText('Authenticator code')).toBeVisible();
  },
};

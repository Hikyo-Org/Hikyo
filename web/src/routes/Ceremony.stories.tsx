import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAuthMethods } from '@hikyo/zod';
import { expect, fn, userEvent } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import type { WhoAmI } from '../app/AuthProvider.tsx';
import { ceremonyRequest, revealWindow } from '../testkit/ceremony.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { Ceremony, type CeremonyRequest } from './Ceremony.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The purpose-bound ceremony modal. It reads the session (for the OIDC branch)
// and the configured providers, so it runs under the app harness; the passkey
// and OIDC factors are never driven here (a real WebAuthn prompt, a real
// provider redirect). The refused state is reached through the code form,
// whose POST the harness refuses, so no ceremony runs.
const protectedRequest = ceremonyRequest('production');

const production: CeremonyRequest = {
  ...protectedRequest,
  keys: [
    { id: 'key_01989abc-def0-7123-8123-000000000001', name: 'DATABASE_URL', classification: 'secret' },
    { id: 'key_01989abc-def0-7123-8123-000000000002', name: 'STRIPE_SECRET_KEY', classification: 'secret' },
    { id: 'key_01989abc-def0-7123-8123-000000000003', name: 'LOG_LEVEL', classification: 'config' },
  ],
};

// A non-protected environment with a 5-minute sliding window: a code is on the table.
const codeOffered: CeremonyRequest = {
  ...production,
  window: { ...revealWindow(false), protected: false, effective_window_seconds: 300, totp_offered: true },
};

const methods: MockRoute = {
  url: '/api/v1/auth/methods',
  body: {
    local_login_enabled: true,
    providers: [{ slug: 'corp', display_name: 'Corporate IdP', kind: 'oidc' }],
  } satisfies z.infer<typeof zAuthMethods>,
};

const oidcIdentity: WhoAmI = {
  ...authenticatedIdentity,
  session: {
    ...authenticatedIdentity.session,
    assurance: { ...authenticatedIdentity.session.assurance, method: 'oidc:corp', provider: 'corp' },
  },
};

const meta = {
  component: Ceremony,
  tags: ['ai-generated'],
  args: { request: production, onAuthorised: fn(), onCancel: fn() },
  parameters: { ...topLayerDocs, app: { auth: true, responses: [methods] } },
} satisfies Meta<typeof Ceremony>;

export default meta;
type Story = StoryObj<typeof meta>;

// A protected environment: the enumerated keys, the passkey, and the stated
// reason there is no code option rather than a disabled one.
export const Protected: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'reveal · production' })).toBeVisible();
    await expect(canvas.getByRole('list', { name: /keys this decision covers/i })).toBeVisible();
    await expect(canvas.getByText(/this environment is protected/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Use a passkey' })).toBeEnabled();
    await expect(canvas.queryByLabelText(/code from your authenticator/i)).toBeNull();
  },
};

// A sliding window: the passkey, the window sentence, and the code form.
export const CodeOffered: Story = {
  args: { request: codeOffered },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/success opens a sliding reveal window/i)).toBeVisible();
    await expect(canvas.getByLabelText(/or a code from your authenticator/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Authorise with a code' })).toBeDisabled();
  },
};

// The same window from an OIDC session: the identity provider is a third factor.
export const OIDC: Story = {
  args: { request: { ...codeOffered, purpose: 'publish' } },
  parameters: { app: { auth: true, identity: oidcIdentity, responses: [methods] } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'publish into · production' })).toBeVisible();
    await expect(
      await canvas.findByRole('button', { name: 'Re-authenticate with Corporate IdP' }),
    ).toBeVisible();
  },
};

// A code the server did not accept: the refusal sits above the factors and
// every factor is offered again.
export const Refused: Story = {
  args: { request: codeOffered },
  parameters: {
    app: {
      auth: true,
      identity: oidcIdentity,
      responses: [
        methods,
        { url: '/api/v1/auth/reauth/totp', method: 'POST', status: 401, body: { error: 'unauthenticated' } },
      ],
    },
  },
  play: async ({ canvas }) => {
    // AuthProvider remounts the modal once whoami answers, and a code typed
    // before that is lost with it. The provider button renders only from the
    // settled session, so it is the signal the modal now holds its state.
    await expect(
      await canvas.findByRole('button', { name: 'Re-authenticate with Corporate IdP' }),
    ).toBeVisible();
    await userEvent.type(canvas.getByLabelText(/or a code from your authenticator/i), '123456');
    await userEvent.click(canvas.getByRole('button', { name: 'Authorise with a code' }));
    await expect(await canvas.findByText(/that code did not match/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Use a passkey' })).toBeEnabled();
  },
};

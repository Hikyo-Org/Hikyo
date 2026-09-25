import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAuthMethods, zCliReauthTransaction, zTotpStatus } from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import type { WhoAmI } from '../app/AuthProvider.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { withSearchParams } from '../testkit/searchParams.ts';
import { CLIReauth } from './CLIReauth.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The page reads its transaction from `?transaction=` on the real location
// (a useState initialiser, so it has to be there before mount), then loads
// GET /api/v1/auth/cli-reauth/transactions/<state> plus the authenticator
// state and the configured providers. The approve is a mutation that runs a
// passkey/TOTP/OIDC ceremony; the play never fires it. Bodies use the wire
// (input) shape of the transaction schema: int64 fields travel as numbers.
const STATE = 'txn-195';

const sliding = {
  environment_id: 'env_01989abc-def0-7123-8123-000000000001',
  effective_window_seconds: 300,
  requires_webauthn: false,
};
const locked = {
  environment_id: 'env_01989abc-def0-7123-8123-000000000002',
  effective_window_seconds: 0,
  requires_webauthn: true,
};
const keys = ['key_01989abc-def0-7123-8123-000000000001', 'key_01989abc-def0-7123-8123-000000000002'];

type Transaction = z.input<typeof zCliReauthTransaction>;

const base = {
  state: STATE,
  redirect_uri: 'http://127.0.0.1:40126/callback',
  expires_at: '2099-08-23T12:00:00Z',
} satisfies Partial<Transaction>;

const disclosure = {
  ...base,
  purpose: 'reveal',
  operation: 'value.reveal',
  environments: [sliding, locked],
  key_ids: keys,
} satisfies Transaction;

const adapter = {
  ...base,
  purpose: 'adapter',
  operation: 'adapter.configure',
  environments: [sliding],
  key_ids: [],
} satisfies Transaction;

const selfConfig = {
  ...base,
  purpose: 'self-config',
  operation: 'self-config.apply',
  environments: [],
  key_ids: [],
  self_config: {
    action: 'apply',
    owner_instance_id: 'instance_local',
    revision: 3,
    expected_generation: 7,
    schema_version: 1,
    preview_token: '',
    to: '',
    confirm_restored_credentials: false,
    plan_digest: 'b'.repeat(64),
  },
} satisfies Transaction;

const transaction = (rest: Partial<MockRoute>): MockRoute => ({
  url: `/api/v1/auth/cli-reauth/transactions/${STATE}`,
  ...rest,
});
const totpConfirmed: MockRoute = {
  url: '/api/v1/auth/totp',
  body: { confirmed: true, pending: false } satisfies z.input<typeof zTotpStatus>,
};
const methods: MockRoute = {
  url: '/api/v1/auth/methods',
  body: {
    local_login_enabled: true,
    signup_open: false, signup_paused: false, signup_methods: [],
    providers: [{ slug: 'corp', display_name: 'Corporate IdP', kind: 'oidc' }],
  } satisfies z.input<typeof zAuthMethods>,
};

// A session signed in through the configured OIDC provider.
const oidcIdentity: WhoAmI = {
  ...authenticatedIdentity,
  session: {
    ...authenticatedIdentity.session,
    assurance: { ...authenticatedIdentity.session.assurance, method: 'oidc:corp', provider: 'corp' },
  },
};

const app = (identity: WhoAmI, ...rows: MockRoute[]) => ({
  auth: true,
  identity,
  path: '/reauth/cli',
  responses: [...rows, totpConfirmed, methods],
});

const meta = {
  component: CLIReauth,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: app(authenticatedIdentity, transaction({ body: disclosure })) },
} satisfies Meta<typeof CLIReauth>;

export default meta;
type Story = StoryObj<typeof meta>;

// A disclosure handoff over two keys: the locked environment takes a passkey,
// the sliding one also accepts a code from the enrolled authenticator.
export const Disclosure: Story = {
  beforeEach: withSearchParams({ transaction: STATE }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/the terminal asks to/i)).toBeVisible();
    await expect(canvas.getByText(/passkey required/)).toBeVisible();
    await expect(canvas.getByText(/TOTP required/)).toBeVisible();
    await expect(
      await canvas.findByLabelText(/authenticator code \(optional; leave empty to use a passkey\)/i),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Authorize CLI' })).toBeEnabled();
  },
};

// The same handoff from an OIDC session: the sliding environment can be
// re-authenticated at the identity provider instead.
export const DisclosureOIDC: Story = {
  beforeEach: withSearchParams({ transaction: STATE }),
  parameters: { app: app(oidcIdentity, transaction({ body: disclosure })) },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('button', { name: 'Re-authenticate with Corporate IdP' }),
    ).toBeVisible();
    await expect(canvas.getByText(/once per sliding-window environment/i)).toBeVisible();
  },
};

// An adapter operation over a sliding environment: the code is required, so
// the authorize button waits for it.
export const Adapter: Story = {
  beforeEach: withSearchParams({ transaction: STATE }),
  parameters: { app: app(authenticatedIdentity, transaction({ body: adapter })) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('adapter.configure')).toBeVisible();
    await expect(canvas.getByLabelText('Authenticator code')).toBeRequired();
    await expect(canvas.getByRole('button', { name: 'Authorize CLI' })).toBeDisabled();
  },
};

// A self-configuration decision: the revision, generation and the exact plan
// digest the authorization is bound to.
export const SelfConfig: Story = {
  beforeEach: withSearchParams({ transaction: STATE }),
  parameters: { app: app(authenticatedIdentity, transaction({ body: selfConfig })) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/revision r3, generation 7/)).toBeVisible();
    await expect(canvas.getByText('b'.repeat(64))).toBeVisible();
    await expect(canvas.getByText(/controlled rollout/i)).toBeVisible();
  },
};

// The transaction read never settles: the card holds its loading status.
export const Loading: Story = {
  beforeEach: withSearchParams({ transaction: STATE }),
  parameters: { app: app(authenticatedIdentity, transaction({ pending: true })) },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading authorization policy/i)).toBeVisible());
  },
};

// An expired, spent or unknown transaction: one sentence, back to the terminal.
export const Failed: Story = {
  beforeEach: withSearchParams({ transaction: STATE }),
  parameters: {
    app: app(authenticatedIdentity, transaction({ status: 410, body: { error: 'transaction spent' } })),
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByText(/this cli transaction is invalid, expired, or already used/i),
    ).toBeVisible();
  },
};

// Opened without a transaction on the URL: nothing to authorize.
export const NothingToAuthorize: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'Nothing to authorize' })).toBeVisible();
    await expect(canvas.getByText(/this page has no cli transaction/i)).toBeVisible();
  },
};

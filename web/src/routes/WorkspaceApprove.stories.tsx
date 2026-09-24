import type { WorkspaceHandoffEstablishment, WorkspaceHandoffStepUp } from '@hikyo/client';
import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAuthMethods } from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { withSearchParams } from '../testkit/searchParams.ts';
import { WorkspaceApprove } from './WorkspaceApprove.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The serving instance's consent page. It reads the opaque `state` from the
// real location (a useState initialiser, so it has to be there before mount),
// then loads GET /api/v1/auth/workspace/transactions/<state> once the session
// is known. Approval is a mutation behind a click (establishment) or a
// passkey/TOTP ceremony (step-up); the play never fires either. The fixture's
// `state` MUST equal the URL's, or the page refuses it as a wrong-state answer.
const STATE = 'state-195';

const establishment: WorkspaceHandoffEstablishment = {
  state: STATE,
  purpose: 'establishment',
  requesting_origin: 'https://viewer.hikyo.example',
  key_ids: [],
  expires_at: '2099-08-23T12:00:00Z',
};

const stepUp: WorkspaceHandoffStepUp = {
  state: STATE,
  purpose: 'step-up',
  requesting_origin: 'https://viewer.hikyo.example',
  operation: 'reveal',
  environment: 'env_01989abc-def0-7123-8123-000000000001',
  key_ids: [
    'key_01989abc-def0-7123-8123-000000000001',
    'key_01989abc-def0-7123-8123-000000000002',
    'key_01989abc-def0-7123-8123-000000000003',
  ],
  expires_at: '2099-08-23T12:00:00Z',
};

const transaction = (rest: Partial<MockRoute>): MockRoute => ({
  url: /\/api\/v1\/auth\/workspace\/transactions\//,
  ...rest,
});

// The anonymous branch renders Login in place, which reads the sign-in methods.
const methods: MockRoute = {
  url: '/api/v1/auth/methods',
  body: { local_login_enabled: true, providers: [], signup_open: false, signup_paused: false, signup_methods: [] } satisfies z.input<typeof zAuthMethods>,
};

const meta = {
  component: WorkspaceApprove,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { auth: true, responses: [] } },
} satisfies Meta<typeof WorkspaceApprove>;

export default meta;
type Story = StoryObj<typeof meta>;

// Opened without a state: nothing to authorize, and no transaction is read.
export const NothingToAuthorize: Story = {
  play: async ({ canvas }) => {
    // A fresh query per retry: the card re-mounts as AuthProvider settles.
    await waitFor(() =>
      expect(canvas.getByRole('heading', { name: /nothing to authorize/i })).toBeVisible(),
    );
  },
};

// The transaction read never settles: the loading status stands.
export const Loading: Story = {
  beforeEach: withSearchParams({ state: STATE }),
  parameters: { app: { auth: true, responses: [transaction({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('status')).toHaveTextContent(/loading/i));
  },
};

// A first workspace: the requesting origin and a plain Authorize / Cancel.
export const Establishment: Story = {
  beforeEach: withSearchParams({ state: STATE }),
  parameters: { app: { auth: true, responses: [transaction({ body: establishment })] } },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /authorize this workspace/i }),
    ).toBeVisible();
    await expect(canvas.getByText(establishment.requesting_origin)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Authorize' })).toBeEnabled();
  },
};

// An elevation of an open workspace: the bound scope and this instance's own
// reauthentication (passkey or authenticator code) before the approval.
export const StepUp: Story = {
  beforeEach: withSearchParams({ state: STATE }),
  parameters: { app: { auth: true, responses: [transaction({ body: stepUp })] } },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /authorize this disclosure/i }),
    ).toBeVisible();
    const scope = canvas.getByRole('region', { name: /requested scope/i });
    await expect(scope).toHaveTextContent(stepUp.environment);
    await expect(scope.querySelectorAll('li')).toHaveLength(stepUp.key_ids.length);
    await expect(canvas.getByRole('button', { name: /use a passkey/i })).toBeEnabled();
    await expect(canvas.getByRole('button', { name: /authorise with a code/i })).toBeDisabled();
  },
};

// The transaction could not be read (expired, consumed, or unknown): no button.
export const Failed: Story = {
  beforeEach: withSearchParams({ state: STATE }),
  parameters: {
    app: { auth: true, responses: [transaction({ status: 403, body: { error: 'expired' } })] },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /authorization could not be completed/i }),
    ).toBeVisible();
    await expect(canvas.queryByRole('button')).toBeNull();
  },
};

// No session on this instance: the sign-in form renders in place so the URL,
// and with it the state, survives.
export const SignIn: Story = {
  beforeEach: withSearchParams({ state: STATE }),
  parameters: {
    app: {
      auth: true,
      responses: [
        { url: '/api/v1/auth/whoami', status: 401, body: { error: 'unauthenticated' } },
        methods,
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /sign in to hikyo/i }),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Username')).toBeVisible();
  },
};

import type { Meta, StoryContext, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { ORG, PRJ } from '../testkit/ids.ts';
import { dynamicProvider as provider } from '../testkit/machineAccess.ts';
import { SetCredentialDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The write-only credential replace. Busy and failed live in the dialog's own
// state, so the plays type a credential and submit against the harness (a PUT
// on the provider's credential); the 400 re-probe refusal is asserted by its
// own sentence so a mis-routed request cannot pass as it.

const CREDENTIAL_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/dynamic-providers/${provider.id}/credential`;

const submit = async (canvas: StoryContext['canvas']) => {
  await userEvent.type(canvas.getByLabelText('Admin credential'), 'pg-admin-secret');
  await userEvent.click(canvas.getByRole('button', { name: 'Save credential' }));
};

const meta = {
  component: SetCredentialDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    provider,
    onClose: fn(),
    onSet: fn(),
  },
} satisfies Meta<typeof SetCredentialDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// A provider that already holds a credential: the title says Replace.
export const Replace: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: `Replace admin credential · ${provider.origin}` }),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Admin credential')).toHaveAttribute('type', 'password');
    await expect(canvas.getByRole('button', { name: 'Save credential' })).toBeEnabled();
  },
};

// A provider whose credential was revoked: the same form, titled Set.
export const FirstCredential: Story = {
  args: { provider: { ...provider, credential_present: false, credential_set_at: null } },
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: `Set admin credential · ${provider.origin}` }),
    ).toBeVisible();
  },
};

// Submitted blank: refused client-side, the stored credential untouched.
export const CredentialRequired: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Save credential' }));
    await expect(await canvas.findByText(/the credential is required/i)).toBeVisible();
  },
};

// In flight: the server is re-probing with the new credential.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: CREDENTIAL_URL, method: 'PUT', pending: true }] } },
  play: async ({ canvas }) => {
    await submit(canvas);
    await expect(await canvas.findByRole('button', { name: 'Saving…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    await expect(canvas.getByLabelText('Admin credential')).toBeDisabled();
  },
};

// The re-probe failed: the prior credential stays, the typed one is cleared.
export const Failed: Story = {
  parameters: {
    app: {
      responses: [{ url: CREDENTIAL_URL, method: 'PUT', status: 400, body: { error: 'probe' } }],
    },
  },
  play: async ({ canvas }) => {
    await submit(canvas);
    await expect(
      await canvas.findByText(/postgresql could not be reached and authenticated with it/i),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Admin credential')).toHaveValue('');
  },
};

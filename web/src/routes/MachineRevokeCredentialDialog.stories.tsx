import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { ORG, PRJ } from '../testkit/ids.ts';
import { dynamicProvider as provider } from '../testkit/machineAccess.ts';
import { RevokeCredentialDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// MachineAccess's own RevokeCredentialDialog (the Adapters one has its own
// story file): it clears a dynamic provider's admin credential. Busy and
// failed live in the dialog's mutation state, so the plays confirm against the
// harness (a DELETE on the provider's credential); the 403 is asserted by its
// own sentence so a mis-routed request cannot pass as it.

const CREDENTIAL_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/dynamic-providers/${provider.id}/credential`;

const meta = {
  component: RevokeCredentialDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    provider,
    onClose: fn(),
    onRevoked: fn(),
  },
} satisfies Meta<typeof RevokeCredentialDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The fail-closed consequence stated up front: leases stay minted, but the
// worker can no longer act on them until a replacement is set.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: `Revoke admin credential · ${provider.origin}` }),
    ).toBeVisible();
    await expect(canvas.getByText(/existing leases stay minted at the provider/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Revoke credential' })).toBeEnabled();
  },
};

// In flight: both actions held, the primary relabelled.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: CREDENTIAL_URL, method: 'DELETE', pending: true }] } },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Revoke credential' }));
    await expect(await canvas.findByRole('button', { name: 'Revoking…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
  },
};

// Refused for capability: the reason, and the action offered again.
export const Failed: Story = {
  parameters: {
    app: {
      responses: [
        { url: CREDENTIAL_URL, method: 'DELETE', status: 403, body: { error: 'forbidden' } },
      ],
    },
  },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Revoke credential' }));
    await expect(
      await canvas.findByText(/revoking the credential needs manage-identities/i),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Revoke credential' })).toBeEnabled();
  },
};

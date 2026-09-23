import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import type { DynamicProvider } from '../api/dynamic.ts';
import { RevokeCredentialDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// MachineAccess's own RevokeCredentialDialog (the Adapters one has its own
// story file): it clears a dynamic provider's admin credential. Busy and
// failed live in the dialog's mutation state, so the plays confirm against the
// harness (a DELETE on the provider's credential); the 403 is asserted by its
// own sentence so a mis-routed request cannot pass as it.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';

const provider: DynamicProvider = {
  id: 'dpv_123e4567-e89b-12d3-a456-426614174050',
  kind: 'postgres',
  origin: 'db.internal.example.com:5432',
  tls_mode: 'verify-full',
  grant_role: 'hikyo_leases',
  credential_present: true,
  credential_set_at: '2026-09-01T00:00:00Z',
  authority_principal_id: 'prn_123e4567-e89b-12d3-a456-426614174010',
  state: 'active',
  created_at: '2026-08-20T00:00:00Z',
};
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

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import type { Adapter } from '../api/adapters.ts';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { RevokeCredentialDialog } from './Adapters.tsx';

const adapter: Adapter = {
  id: 'apt_00000000-0000-0000-0000-000000000001',
  provider: 'forgejo',
  origin: 'https://forgejo.example.com',
  credential_present: true,
  authority_principal_id: 'usr_00000000-0000-0000-0000-000000000001',
  state: 'active',
  created_at: '2026-09-15T08:00:00Z',
  targets: [],
};

const meta = {
  component: RevokeCredentialDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    adapter,
    busy: false,
    onCancel: fn(),
    onConfirm: fn(),
  },
} satisfies Meta<typeof RevokeCredentialDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The modal names the origin whose custody it destroys.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: /revoke credential for/i }),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Revoke credential' })).toBeEnabled();
  },
};

// Clicking the danger button confirms the revoke.
export const Confirm: Story = {
  play: async ({ args, canvas }) => {
    const revoke = canvas.getByRole('button', { name: 'Revoke credential' });
    await expect(revoke).toBeEnabled();
    await userEvent.click(revoke);
    await waitFor(() => expect(args.onConfirm).toHaveBeenCalled());
  },
};

// The in-flight revoke disables both actions and relabels the danger button.
export const Busy: Story = {
  args: { busy: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Revoking…' })).toBeDisabled();
  },
};

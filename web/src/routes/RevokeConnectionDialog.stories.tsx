import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { RevokeConnectionDialog } from './Remotes.tsx';

type Props = Parameters<typeof RevokeConnectionDialog>[0];

const connection: Props['connection'] = {
  id: 'con_01989abc-def0-7123-8123-000000000001',
  principal: 'peer-directory-reader',
  label: 'staging cluster',
  kind: 'hikyo-token',
  prefix_hint: 'hk_9f3a',
  lifetime: 'finite',
  expires_at: '2026-12-01T00:00:00Z',
  created_at: '2026-08-01T09:00:00Z',
  created_by: 'operator@hikyo.example',
  last_used_at: '2026-08-22T14:30:00Z',
  live: true,
};

const meta = {
  component: RevokeConnectionDialog,
  tags: ['ai-generated'],
  // The dialog runs a real useRevokeConnection mutation; the harness supplies
  // the QueryClientProvider it mounts under, and topLayerDocs keeps the native
  // <dialog> inside the docs frame. No fetch fires until Revoke is clicked.
  parameters: { app: { responses: [] }, ...topLayerDocs },
  args: { connection, onClose: fn() },
} satisfies Meta<typeof RevokeConnectionDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The confirm dialog states the consequence before it commits.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /revoke staging cluster/i })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /revoke credential/i })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /cancel/i })).toBeVisible();
  },
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { ConnectionRow } from './Remotes.tsx';

type Props = Parameters<typeof ConnectionRow>[0];
type Connection = Props['connection'];

const base: Connection = {
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
  component: ConnectionRow,
  tags: ['ai-generated'],
  // A bare <li> outside a list fails the axe listitem rule; the section that
  // owns this row renders it inside <ul className="connections">, so the story
  // supplies the same context rather than the row inventing one.
  decorators: [
    (Story) => (
      <ul className="connections">
        <Story />
      </ul>
    ),
  ],
  args: { onRevoke: fn() },
} satisfies Meta<typeof ConnectionRow>;

export default meta;
type Story = StoryObj<typeof meta>;

// A live credential shows its state badge and a Revoke action.
export const Live: Story = {
  args: { connection: base },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'staging cluster' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /revoke staging cluster/i })).toBeVisible();
  },
};

// Expired: not revoked, but no longer live. Badge reads "expired", still revocable.
export const Expired: Story = {
  args: { connection: { ...base, label: 'retired laptop agent', live: false } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('expired')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /revoke retired laptop agent/i })).toBeVisible();
  },
};

// A revoked credential drops the Revoke action; there is nothing left to do.
export const Revoked: Story = {
  args: {
    connection: {
      ...base,
      label: 'compromised CI runner',
      live: false,
      revoked_at: '2026-08-20T11:00:00Z',
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: 'compromised CI runner' })).toBeVisible();
    await expect(canvas.getByText('revoked')).toBeVisible();
    await expect(canvas.queryByRole('button', { name: /revoke/i })).toBeNull();
  },
};

// An indefinite credential names its lifetime rather than an expiry date, and a
// long label proves the head wraps rather than clips.
export const Indefinite: Story = {
  args: {
    connection: {
      ...base,
      label: 'permanent replication link to the amsterdam datacentre mirror',
      lifetime: 'indefinite',
      expires_at: undefined,
    },
  },
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('heading', { name: /permanent replication link/i }),
    ).toBeVisible();
    await expect(canvas.getByText('indefinite')).toBeVisible();
  },
};

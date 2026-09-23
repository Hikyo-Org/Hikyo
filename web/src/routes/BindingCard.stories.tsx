import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import type { MachineCredential, ServiceAccount } from '../api/identities.ts';
import { BindingCard } from './MachineAccess.tsx';

// One federated binding as the Federation tab and a row expansion list it: the
// byte-exact (issuer, subject, audience) triple, every pinned claim, the expiry
// in words, and the quarantine notice a restore leaves behind. `now` is fixed
// so the expiry text is the same on every run.
const now = new Date('2026-09-23T00:00:00Z');

const account: ServiceAccount = {
  id: 'msa_123e4567-e89b-12d3-a456-426614174020',
  principal_id: 'prn_123e4567-e89b-12d3-a456-426614174020',
  name: 'api-gateway',
  kind: 'workload',
  created_at: '2026-08-01T00:00:00Z',
  created_by: 'prn_123e4567-e89b-12d3-a456-426614174010',
  live_credentials: 2,
};

const credential: MachineCredential = {
  id: 'mcr_123e4567-e89b-12d3-a456-426614174041',
  kind: 'oidc-federation',
  lifetime: 'finite',
  expires_at: '2026-12-12T00:00:00Z',
  issuer: 'https://token.actions.githubusercontent.com',
  subject: 'repo:acme/gateway:ref:refs/heads/main',
  audience: 'hikyo',
  required_claims: [
    { claim: 'repository_id', number_value: 987654321n },
    { claim: 'repository_owner_id', number_value: 4242n },
    { claim: 'event_name', string_value: 'push' },
    { claim: 'ref_protected', bool_value: true },
  ],
  created_at: '2026-08-12T00:00:00Z',
  created_by: 'prn_123e4567-e89b-12d3-a456-426614174010',
  expiring_soon: false,
};

const meta = {
  component: BindingCard,
  tags: ['ai-generated'],
  args: { account, credential, now, ready: true, onReplace: fn(), onRevoke: fn() },
} satisfies Meta<typeof BindingCard>;

export default meta;
type Story = StoryObj<typeof meta>;

// A live GitHub Actions binding: the triple, the numeric and boolean pins, both actions.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText('repo:acme/gateway:ref:refs/heads/main')).toBeVisible();
    await expect(canvas.getByText('987654321')).toBeVisible();
    await expect(canvas.getByText('expires in 80 days')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Replace binding on api-gateway' })).toBeEnabled();
  },
};

// Days from expiry: the tier turns to danger beside the words, never instead.
export const ExpiringSoon: Story = {
  args: { credential: { ...credential, expires_at: '2026-09-28T00:00:00Z', expiring_soon: true } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('expires in 5 days')).toBeVisible();
  },
};

// Restored from a backup: the binding refuses every token issued at or before
// the restore instant, and says so.
export const Quarantined: Story = {
  args: { credential: { ...credential, reactivated_at: '2026-09-20T06:30:00Z' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/quarantined since the restore on 2026-09-20/i)).toBeVisible();
  },
};

// The surface is half-read: replace (a mint) is held, revoke (a narrowing) is not.
export const NotReady: Story = {
  args: { ready: false },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Replace binding on api-gateway' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Revoke binding on api-gateway' })).toBeEnabled();
  },
};

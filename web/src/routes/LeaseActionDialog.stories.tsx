import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import type { DynamicLease } from '../api/dynamic.ts';
import { LeaseActionDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The queued lease act, one dialog for three verbs: renew (with an optional
// new ceiling), revoke and settle. Busy and failed live in the dialog's own
// mutation state, so the plays submit against the harness (a POST per verb);
// the 409 is asserted by its verb-specific sentence, so a mis-routed request
// (a harness 404 reads "no longer here") fails the play.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const PROD = 'env_123e4567-e89b-12d3-a456-426614174010';

const lease: DynamicLease = {
  id: 'dls_123e4567-e89b-12d3-a456-426614174060',
  provider_id: 'dpv_123e4567-e89b-12d3-a456-426614174050',
  environment_id: PROD,
  principal_id: 'prn_123e4567-e89b-12d3-a456-426614174020',
  principal_class: 'workload',
  provider_handle: 'hk_lease_0',
  state: 'active',
  issued_at: '2026-09-22T08:00:00Z',
  expires_at: '2026-09-23T08:00:00Z',
  max_ttl_seconds: 3600n,
  last_transition_at: '2026-09-22T08:00:00Z',
  created_at: '2026-09-22T08:00:00Z',
};
const LEASE_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/environments/${PROD}/leases/${lease.id}`;

const meta = {
  component: LeaseActionDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    action: { verb: 'renew', environmentId: PROD, environmentName: 'production', lease },
    onClose: fn(),
    onDone: fn(),
  },
} satisfies Meta<typeof LeaseActionDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Renew: the one verb with a field, the optional new ceiling.
export const Renew: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Renew lease · hk_lease_0' })).toBeVisible();
    await expect(
      canvas.getByLabelText('New maximum lifetime (seconds, optional)'),
    ).toHaveValue('');
    await expect(canvas.getByRole('button', { name: 'Queue renewal' })).toBeEnabled();
  },
};

// A ceiling that is not a whole number of seconds: refused client-side.
export const RenewCeilingRefused: Story = {
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('New maximum lifetime (seconds, optional)'), '1.5h');
    await userEvent.click(canvas.getByRole('button', { name: 'Queue renewal' }));
    await expect(
      await canvas.findByText(/enter the new ceiling as a whole number of seconds/i),
    ).toBeVisible();
  },
};

// Revoke: one tap, the fail-safe teardown.
export const Revoke: Story = {
  args: { action: { verb: 'revoke', environmentId: PROD, environmentName: 'production', lease } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Revoke lease · hk_lease_0' })).toBeVisible();
    await expect(canvas.queryByRole('textbox')).not.toBeInTheDocument();
    await expect(canvas.getByRole('button', { name: 'Queue revocation' })).toBeEnabled();
  },
};

// Settle: re-triggers reconcile on an unknown lease.
export const Settle: Story = {
  args: {
    action: {
      verb: 'settle',
      environmentId: PROD,
      environmentName: 'production',
      lease: { ...lease, state: 'unknown' },
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Settle lease · hk_lease_0' })).toBeVisible();
    await expect(canvas.getByText(/this lease is in an ambiguous state/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Queue reconcile' })).toBeEnabled();
  },
};

// In flight: the queue request has left; both actions held.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: `${LEASE_URL}/renew`, method: 'POST', pending: true }] } },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Queue renewal' }));
    await expect(await canvas.findByRole('button', { name: 'Queuing…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
  },
};

// The lease moved under the dialog: the 409 names the state that refused the verb.
export const Failed: Story = {
  args: { action: { verb: 'revoke', environmentId: PROD, environmentName: 'production', lease } },
  parameters: {
    app: {
      responses: [
        { url: `${LEASE_URL}/revoke`, method: 'POST', status: 409, body: { error: 'terminal' } },
      ],
    },
  },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Queue revocation' }));
    await expect(
      await canvas.findByText(/this lease is already terminal or being revoked/i),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Queue revocation' })).toBeEnabled();
  },
};

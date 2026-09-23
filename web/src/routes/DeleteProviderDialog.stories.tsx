import type { Meta, StoryContext, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { ORG, PRJ } from '../testkit/ids.ts';
import { dynamicProvider as provider } from '../testkit/machineAccess.ts';
import { DeleteProviderDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The provider delete behind a typed-origin confirmation, with the cascade
// checkbox whose presence and necessity follow the live-lease count. Busy,
// failed and the server-demanded cascade live in the dialog's own state, so
// those plays type the origin and submit against the harness (a DELETE on the
// provider; the `revoke_all` query is ignored by the path match).

const DELETE_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/dynamic-providers/${provider.id}`;
const CASCADE = 'Revoke every live lease of this provider as part of the delete.';

const arm = async (canvas: StoryContext['canvas']) => {
  await userEvent.type(canvas.getByLabelText('Confirm the provider origin to delete it'), provider.origin);
  await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeEnabled();
  await userEvent.click(canvas.getByRole('button', { name: 'Delete provider' }));
};

const meta = {
  component: DeleteProviderDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    provider,
    liveLeaseCount: 0,
    leasesKnown: true,
    onClose: fn(),
    onDeleted: fn(),
  },
} satisfies Meta<typeof DeleteProviderDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// No live leases: no cascade offered, the typed origin alone arms the delete.
export const NoLeases: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: `Delete provider · ${provider.origin}` }),
    ).toBeVisible();
    await expect(canvas.getByText(/this provider has no live leases/i)).toBeVisible();
    await expect(canvas.queryByRole('checkbox')).not.toBeInTheDocument();
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeDisabled();
  },
};

// Live leases: the cap counts them and the cascade tick is required, so the
// typed origin alone does not arm the delete.
export const LiveLeases: Story = {
  args: { liveLeaseCount: 3 },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/this provider has 3 live leases/i)).toBeVisible();
    await userEvent.type(canvas.getByLabelText('Confirm the provider origin to delete it'), provider.origin);
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeDisabled();
    await userEvent.click(canvas.getByRole('checkbox', { name: CASCADE }));
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeEnabled();
  },
};

// The lease listing could not be read: the count is unknown, the cascade is
// offered but not required.
export const LeasesUnknown: Story = {
  args: { leasesKnown: false },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/the number of live leases is unknown/i)).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: CASCADE })).not.toBeChecked();
    await userEvent.type(canvas.getByLabelText('Confirm the provider origin to delete it'), provider.origin);
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeEnabled();
  },
};

// In flight: the danger button and Cancel both held.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: DELETE_URL, method: 'DELETE', pending: true }] } },
  play: async ({ canvas }) => {
    await arm(canvas);
    await expect(await canvas.findByRole('button', { name: 'Cancel' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeDisabled();
  },
};

// The server's own live-leases guard fired (a lease appeared after the count
// was read): the refusal names it and the cascade tick appears for the retry.
export const Conflict: Story = {
  parameters: {
    app: { responses: [{ url: DELETE_URL, method: 'DELETE', status: 409, body: { error: 'leases' } }] },
  },
  play: async ({ canvas }) => {
    await arm(canvas);
    await expect(await canvas.findByText(/this provider still has live leases/i)).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: CASCADE })).not.toBeChecked();
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeDisabled();
  },
};

// Refused for capability: the reason, the delete offered again.
export const Failed: Story = {
  parameters: {
    app: { responses: [{ url: DELETE_URL, method: 'DELETE', status: 403, body: { error: 'forbidden' } }] },
  },
  play: async ({ canvas }) => {
    await arm(canvas);
    await expect(
      await canvas.findByText(/deleting a provider needs manage-identities/i),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Delete provider' })).toBeEnabled();
  },
};

import type { Meta, StoryContext, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { ORG, PRJ } from '../testkit/ids.ts';
import { serviceAccount as account } from '../testkit/machineAccess.ts';
import { DeleteAccountDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The cascade delete behind a typed-name confirmation. Busy and failed live in
// the dialog's own mutation state, so the plays type the name and submit
// against the harness; the refusal is asserted by its own sentence so a
// mis-routed request (a harness 404 reads "no longer here") fails the play.

const DELETE_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/service-accounts/${account.id}`;

const arm = async (canvas: StoryContext['canvas']) => {
  await userEvent.type(canvas.getByLabelText('Confirm the account name to delete it'), account.name);
  await expect(canvas.getByRole('button', { name: 'Delete service account' })).toBeEnabled();
  await userEvent.click(canvas.getByRole('button', { name: 'Delete service account' }));
};

const meta = {
  component: DeleteAccountDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    account,
    onClose: fn(),
    onDeleted: fn(),
  },
} satisfies Meta<typeof DeleteAccountDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The cap counts the live credentials the cascade revokes; the danger button
// stays held until the name is typed exactly.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: 'Delete service account · api-gateway' }),
    ).toBeVisible();
    await expect(canvas.getByText(/2 live credentials revoked/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Delete service account' })).toBeDisabled();
  },
};

// A single credential, singular copy.
export const OneCredential: Story = {
  args: { account: { ...account, live_credentials: 1 } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/1 live credential revoked/)).toBeVisible();
  },
};

// In flight: the danger button and Cancel both held.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: DELETE_URL, method: 'DELETE', pending: true }] } },
  play: async ({ canvas }) => {
    await arm(canvas);
    await expect(await canvas.findByRole('button', { name: 'Cancel' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Delete service account' })).toBeDisabled();
  },
};

// The delete was refused for capability: the plain sentence, no cascade ambiguity.
export const Failed: Story = {
  parameters: {
    app: { responses: [{ url: DELETE_URL, method: 'DELETE', status: 403, body: { error: 'forbidden' } }] },
  },
  play: async ({ canvas }) => {
    await arm(canvas);
    await expect(
      await canvas.findByText(/deleting a service account needs manage-identities/i),
    ).toBeVisible();
  },
};

// A 500 after the request left: the delete may still have landed, and the
// alert says so.
export const FailedMayHaveCommitted: Story = {
  parameters: {
    app: { responses: [{ url: DELETE_URL, method: 'DELETE', status: 500, body: { error: 'boom' } }] },
  },
  play: async ({ canvas }) => {
    await arm(canvas);
    await expect(await canvas.findByText(/the account may still have been deleted/i)).toBeVisible();
  },
};

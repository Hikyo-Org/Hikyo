import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { CreateAccountDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The create form. Busy and failed live in the dialog's own mutation state, so
// the plays fill the form and submit against the harness: a pending row holds
// the in-flight state, a refusal status renders the failure alert. The 409 is
// asserted by its own sentence, not "an alert", so a mis-routed request (which
// 404s into a different, plausible sentence) fails the play.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const CREATE_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/service-accounts`;

const meta = {
  component: CreateAccountDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    onClose: fn(),
    onCreated: fn(),
  },
} satisfies Meta<typeof CreateAccountDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Empty form: name blank, kind defaulting to workload.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Create service account' })).toBeVisible();
    await expect(canvas.getByLabelText('Kind (immutable)')).toHaveValue('workload');
    await expect(canvas.getByRole('button', { name: 'Create service account' })).toBeEnabled();
  },
};

// Submitted without a name: refused client-side, nothing sent.
export const NameRequired: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Create service account' }));
    await expect(await canvas.findByText(/a service account needs a name/i)).toBeVisible();
  },
};

// In flight: the form latched, both actions held, the primary relabelled.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: CREATE_URL, method: 'POST', pending: true }] } },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('Name'), 'api-gateway');
    await userEvent.click(canvas.getByRole('button', { name: 'Create service account' }));
    await expect(await canvas.findByRole('button', { name: 'Creating…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    await expect(canvas.getByLabelText('Name')).toBeDisabled();
  },
};

// The server refused the name as a duplicate: the form stays, with the reason.
export const Failed: Story = {
  parameters: {
    app: { responses: [{ url: CREATE_URL, method: 'POST', status: 409, body: { error: 'conflict' } }] },
  },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByLabelText('Name'), 'api-gateway');
    await userEvent.click(canvas.getByRole('button', { name: 'Create service account' }));
    await expect(
      await canvas.findByText(/already used by a live service account here/i),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Create service account' })).toBeEnabled();
  },
};

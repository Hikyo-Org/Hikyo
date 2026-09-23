import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { CreateProviderDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The provider form. Busy and failed live in the dialog's own state, so the
// plays fill all three fields and submit against the harness; the 400 is the
// load-bearing refusal (the server probes the origin before storing) and is
// asserted by its own sentence, so a mis-routed request cannot pass as it.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const CREATE_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/dynamic-providers`;

const fill = async (canvas: Parameters<NonNullable<Story['play']>>[0]['canvas']) => {
  await userEvent.type(canvas.getByLabelText('Origin (host:port/dbname)'), 'db.internal:5432/app');
  await userEvent.type(canvas.getByLabelText('Grant role'), 'hikyo_leases');
  await userEvent.type(canvas.getByLabelText('Admin credential (write-only)'), 'pg-admin-secret');
  await userEvent.click(canvas.getByRole('button', { name: 'Configure provider' }));
};

const meta = {
  component: CreateProviderDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    onClose: fn(),
    onCreated: fn(),
  },
} satisfies Meta<typeof CreateProviderDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Empty form: origin, grant role and a write-only credential.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: 'Configure dynamic-secret provider' }),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Admin credential (write-only)')).toHaveAttribute(
      'type',
      'password',
    );
    await expect(canvas.getByRole('button', { name: 'Configure provider' })).toBeEnabled();
  },
};

// Submitted incomplete: refused client-side, nothing sent.
export const FieldsRequired: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Configure provider' }));
    await expect(
      await canvas.findByText(/origin, grant role and the admin credential are all required/i),
    ).toBeVisible();
  },
};

// In flight: the server is dialling the origin; the form latched, actions held.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: CREATE_URL, method: 'POST', pending: true }] } },
  play: async ({ canvas }) => {
    await fill(canvas);
    await expect(await canvas.findByRole('button', { name: 'Configuring…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    await expect(canvas.getByLabelText('Grant role')).toBeDisabled();
  },
};

// The probe failed: the server could not reach or authenticate against
// PostgreSQL, nothing was stored, and the credential field is cleared.
export const Failed: Story = {
  parameters: {
    app: { responses: [{ url: CREATE_URL, method: 'POST', status: 400, body: { error: 'probe' } }] },
  },
  play: async ({ canvas }) => {
    await fill(canvas);
    await expect(
      await canvas.findByText(/postgresql could not be reached and authenticated/i),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Admin credential (write-only)')).toHaveValue('');
    await expect(canvas.getByLabelText('Origin (host:port/dbname)')).toHaveValue('db.internal:5432/app');
  },
};

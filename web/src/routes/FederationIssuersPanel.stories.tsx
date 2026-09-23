import type { Meta, StoryObj } from '@storybook/react-vite';
import { zFederationIssuerList } from '@hikyo/zod';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { FederationIssuersPanel } from './FederationIssuersPanel.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The panel's one read is the instance issuer list; the editor and the delete
// confirmation open from state, so the play functions click them open.
const LIST_URL = '/api/v1/instance/federation-issuers';
const OPERATOR = 'prn_123e4567-e89b-12d3-a456-426614174010';

const issuers = {
  count: 3,
  items: [
    {
      id: 'iss_123e4567-e89b-12d3-a456-426614174101',
      issuer: 'https://token.actions.githubusercontent.com',
      issuer_type: 'github-actions',
      jwks_mode: 'discovery',
      refused_audiences: ['https://github.com/acme'],
      created_at: '2026-08-01T00:00:00Z',
      created_by: OPERATOR,
      ca_bundle_configured: false,
      live_bindings: 0,
    },
    {
      id: 'iss_123e4567-e89b-12d3-a456-426614174102',
      issuer: 'https://kubernetes.default.svc.cluster.local',
      issuer_type: 'kubernetes',
      jwks_mode: 'static',
      refused_audiences: ['https://kubernetes.default.svc.cluster.local', 'kubernetes'],
      created_at: '2026-07-01T00:00:00Z',
      created_by: OPERATOR,
      updated_at: '2026-08-15T00:00:00Z',
      updated_by: OPERATOR,
      ca_bundle_configured: true,
      live_bindings: 3,
    },
    {
      id: 'iss_123e4567-e89b-12d3-a456-426614174103',
      issuer: 'https://git.acme.example',
      issuer_type: 'forgejo',
      jwks_mode: 'discovery',
      refused_audiences: ['https://git.acme.example'],
      created_at: '2026-09-01T00:00:00Z',
      created_by: OPERATOR,
      ca_bundle_configured: false,
      live_bindings: 1,
    },
  ],
} satisfies z.infer<typeof zFederationIssuerList>;

const list = (rest: Partial<MockRoute>): MockRoute => ({ url: LIST_URL, ...rest });

const meta = {
  component: FederationIssuersPanel,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { responses: [list({ body: issuers })] },
  },
} satisfies Meta<typeof FederationIssuersPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// Three issuers across the platform types: one never bound, one with several
// bindings naming it, one with exactly one (singular copy).
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('https://token.actions.githubusercontent.com')).toBeVisible();
    await expect(canvas.getByText(/no bindings name it/i)).toBeVisible();
    await expect(canvas.getByText(/3 bindings name it/i)).toBeVisible();
    await expect(canvas.getByText(/1 binding name it/i)).toBeVisible();
  },
};

// Nothing configured: the note that nothing external can present a token yet.
export const Empty: Story = {
  parameters: { app: { responses: [list({ body: { count: 0, items: [] } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no federation issuers configured/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Configure issuer' })).toBeVisible();
  },
};

// The list never settles: the loading status stands.
export const Loading: Story = {
  parameters: { app: { responses: [list({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading federation issuers/i)).toBeVisible());
  },
};

// A 403 on the MFA-mandatory read: the second-factor state, no list.
export const SecondFactorRequired: Story = {
  parameters: { app: { responses: [list({ status: 403, body: { code: 'forbidden' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/needs a second factor/i)).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'Configure issuer' })).toBeNull();
  },
};

// The read failed outright: none are shown, which is not the same as none.
export const Failed: Story = {
  parameters: { app: { responses: [list({ status: 500, body: { code: 'internal' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/could not be listed/i)).toBeVisible();
  },
};

// The create form in static JWKS mode: issuer, platform type, and the
// write-only key set document that every read omits.
export const Creating: Story = {
  parameters: { app: { responses: [list({ body: { count: 0, items: [] } })] } },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Configure issuer' }));
    await userEvent.selectOptions(canvas.getByLabelText('JWKS source'), 'static');
    await expect(canvas.getByLabelText('JWKS document')).toBeVisible();
    await expect(canvas.getByLabelText(/refused audiences/i)).toBeVisible();
  },
};

// Editing: the issuer string and platform type are read-only facts, only the
// JWKS source and refused audiences move.
export const Editing: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('https://git.acme.example')).toBeVisible();
    const [, , edit] = canvas.getAllByRole('button', { name: 'Edit' });
    if (edit === undefined) throw new Error('third Edit button missing');
    await userEvent.click(edit);
    await expect(canvas.getByText('Edit https://git.acme.example')).toBeVisible();
    await expect(canvas.getByText(/immutable: changing the issuer/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Save issuer' })).toBeVisible();
  },
};

// Delete asked of a bound issuer: the append-only refusal, with Close and no
// destructive button at all.
export const DeleteRefused: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('https://kubernetes.default.svc.cluster.local')).toBeVisible();
    const [, remove] = canvas.getAllByRole('button', { name: 'Delete' });
    if (remove === undefined) throw new Error('second Delete button missing');
    await userEvent.click(remove);
    await expect(canvas.getByRole('alert')).toHaveTextContent(/cannot be deleted/i);
    await expect(canvas.queryByRole('button', { name: 'Delete issuer' })).toBeNull();
    await expect(canvas.getByRole('button', { name: 'Close' })).toBeVisible();
  },
};

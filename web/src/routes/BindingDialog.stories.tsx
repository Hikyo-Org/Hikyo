import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import type { MachineCredential, ServiceAccount } from '../api/identities.ts';
import { BindingDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The federated-binding mint. Busy and failed live in the dialog's own state,
// so those plays fill the Kubernetes preset's two required fields and submit
// against the harness (a POST on the account's bindings); `reachFor` answers
// empty, so no passkey ceremony runs, exactly as today's contract. The 409 is
// asserted by its own sentence, so a mis-routed request (a harness 404 reads
// "no longer here") fails the play.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const OPERATOR = 'prn_123e4567-e89b-12d3-a456-426614174010';

const account = (id: string, name: string, live: number): ServiceAccount => ({
  id,
  principal_id: id.replace('msa_', 'prn_'),
  name,
  kind: 'workload',
  created_at: '2026-08-01T00:00:00Z',
  created_by: OPERATOR,
  live_credentials: live,
});
const gateway = account('msa_123e4567-e89b-12d3-a456-426614174020', 'api-gateway', 2);
const builder = account('msa_123e4567-e89b-12d3-a456-426614174022', 'image-builder', 0);
const BINDINGS_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/service-accounts/${gateway.id}/bindings`;

// A GitHub Actions predecessor with one pin beyond the preset's fields (`ref`),
// so the replace form shows it carried read-only.
const predecessor: MachineCredential = {
  id: 'mcr_123e4567-e89b-12d3-a456-426614174041',
  kind: 'oidc-federation',
  lifetime: 'finite',
  expires_at: '2026-12-01T00:00:00Z',
  issuer: 'https://token.actions.githubusercontent.com',
  subject: 'repo:acme/gateway:ref:refs/heads/main',
  audience: 'hikyo',
  required_claims: [
    { claim: 'repository_id', number_value: 987654321n },
    { claim: 'repository_owner_id', number_value: 4242n },
    { claim: 'event_name', string_value: 'push' },
    { claim: 'ref', string_value: 'refs/heads/main' },
  ],
  created_at: '2026-08-12T00:00:00Z',
  created_by: OPERATOR,
  expiring_soon: false,
};

const UID_LABEL = 'ServiceAccount UID (/kubernetes.io/serviceaccount/uid)';

const fill = async (canvas: Parameters<NonNullable<Story['play']>>[0]['canvas']) => {
  await userEvent.type(canvas.getByLabelText('Audience'), 'hikyo');
  await userEvent.type(canvas.getByLabelText(UID_LABEL), '5d3b2a1c-0f9e-4d8c-b7a6-543210fedcba');
  await userEvent.click(canvas.getByRole('button', { name: 'Bind this identity' }));
};

const meta = {
  component: BindingDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    accounts: [gateway, builder],
    initial: gateway,
    reachFor: fn(() => []),
    onClose: fn(),
    onCreated: fn(),
  },
} satisfies Meta<typeof BindingDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// A fresh binding on the Kubernetes preset: issuer and subject seeded, the
// audience empty because it may never be the issuer's default.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Add federated binding' })).toBeVisible();
    await expect(
      canvas.getByRole('button', { name: /Kubernetes ServiceAccount token/ }),
    ).toHaveAttribute('aria-pressed', 'true');
    await expect(canvas.getByLabelText('Service account')).toHaveValue(gateway.id);
    await expect(canvas.getByLabelText('Issuer')).toHaveValue('https://kubernetes.default.svc.cluster.local');
    await expect(canvas.getByLabelText('Audience')).toHaveValue('');
    await expect(canvas.getByRole('button', { name: 'Bind this identity' })).toBeEnabled();
  },
};

// The GitHub Actions preset: two immutable numeric ids and the event pin.
export const GitHubActions: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'GitHub Actions' }));
    await expect(canvas.getByLabelText('Issuer')).toHaveValue('https://token.actions.githubusercontent.com');
    await expect(canvas.getByLabelText('Repository id (repository_id)')).toHaveAttribute('inputmode', 'numeric');
    await expect(canvas.getByLabelText('Event name')).toHaveValue('push');
  },
};

// A pull-request event pinned: the refusal explains the reach and demands a
// deliberate acknowledgement before the bind is accepted.
export const PullRequestEvent: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'GitHub Actions' }));
    await userEvent.selectOptions(canvas.getByLabelText('Event name'), 'pull_request');
    await expect(
      canvas.getByRole('checkbox', { name: /deliberately binding a pull-request identity/i }),
    ).not.toBeChecked();
    await userEvent.click(canvas.getByRole('button', { name: 'Bind this identity' }));
    await expect(await canvas.findByText(/acknowledge deliberately below/i)).toBeVisible();
  },
};

// Submitted without an audience: refused client-side, nothing sent.
export const AudienceRequired: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Bind this identity' }));
    await expect(await canvas.findByText(/an audience is mandatory/i)).toBeVisible();
  },
};

// Replacing an immutable binding: seeded from the predecessor, the account
// locked, the platform picker gone, and the pin the form cannot render
// carried read-only.
export const Replace: Story = {
  args: { replaces: predecessor },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Replace federated binding' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'GitHub Actions' })).not.toBeInTheDocument();
    await expect(canvas.getByLabelText('Service account')).toBeDisabled();
    await expect(canvas.getByLabelText('Repository id (repository_id)')).toHaveValue('987654321');
    await expect(canvas.getByText('refs/heads/main')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Replace this binding' })).toBeEnabled();
  },
};

// In flight: the whole form latched, both actions held, the primary relabelled.
export const Busy: Story = {
  parameters: { app: { responses: [{ url: BINDINGS_URL, method: 'POST', pending: true }] } },
  play: async ({ canvas }) => {
    await fill(canvas);
    await expect(await canvas.findByRole('button', { name: 'Binding…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    await expect(canvas.getByLabelText('Audience')).toBeDisabled();
  },
};

// The server refused the issued request as a conflict: the reason, plus the
// issued-not-confirmed honesty that the binding may still exist.
export const Failed: Story = {
  parameters: {
    app: {
      responses: [{ url: BINDINGS_URL, method: 'POST', status: 409, body: { error: 'conflict' } }],
    },
  },
  play: async ({ canvas }) => {
    await fill(canvas);
    await expect(
      await canvas.findByText(/an identical binding that already exists/i),
    ).toBeVisible();
    await expect(canvas.getByText(/the binding may still have been created/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Bind this identity' })).toBeEnabled();
  },
};

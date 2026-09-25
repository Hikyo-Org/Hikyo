import type { Meta, StoryObj } from '@storybook/react-vite';
import { zKeyList } from '@hikyo/zod';
import { expect, fn, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MachineEnvScope } from '../api/identities.ts';
import { ORG, PRJ } from '../testkit/ids.ts';
import { serviceAccount as account } from '../testkit/machineAccess.ts';
import { GrantDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The widening ceremony. Its one load-time read is the key catalogue (names
// and classifications, never a value), which the harness answers; the grant
// itself has no story route, so the plays stop short of submitting.
const KEYS_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}/keys`;
// The org membership listing: readable only with org- or instance-scope
// manage-members, the one authority that may grant report-delivery-status.
// Unlisted, it 404s and the checkbox is not offered.
const ORG_GRANTS_URL = `/api/v1/orgs/${ORG}/grants`;

const key = (
  n: number,
  name: string,
  classification: 'secret' | 'config',
): z.input<typeof zKeyList>['items'][number] => ({
  id: `key_123e4567-e89b-12d3-a456-4266141740${String(70 + n)}`,
  org_id: ORG,
  project_id: PRJ,
  name,
  folder_path: '',
  classification,
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: true } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  created_at: '2026-09-15T08:00:00Z',
});
const catalogue = {
  count: 3,
  schema_revision: 7,
  items: [
    key(0, 'DATABASE_URL', 'secret'),
    key(1, 'STRIPE_SECRET_KEY', 'secret'),
    key(2, 'LOG_LEVEL', 'config'),
  ],
} satisfies z.input<typeof zKeyList>;

const env = (n: number, name: string, read: boolean, reveal: boolean): MachineEnvScope => ({
  id: `env_123e4567-e89b-12d3-a456-4266141740${String(10 + n)}`,
  name,
  read,
  reveal,
  report: false,
  origins: read ? [{ kind: 'direct', subject: 'dana@example.com' }] : [],
});
// Production read and revealed, staging read only, development unreached.
const scope: readonly MachineEnvScope[] = [
  env(0, 'production', true, true),
  env(1, 'staging', true, false),
  env(2, 'development', false, false),
];

const meta = {
  component: GrantDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { responses: [{ url: KEYS_URL, body: catalogue }] } },
  args: {
    project: { org: ORG, project: PRJ },
    account,
    scope,
    machineReveal: false,
    liveCredentials: 2,
    onClose: fn(),
    onGranted: fn(),
  },
} satisfies Meta<typeof GrantDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Opt-in off: read is the only capability, so no selector; the whole
// catalogue becomes reachable by name.
export const ReadGrant: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Add environment grant · api-gateway' })).toBeVisible();
    await expect(canvas.queryByLabelText('Capability')).not.toBeInTheDocument();
    await expect(canvas.getByLabelText('Environment (read)')).toHaveValue(scope[2]?.id);
    await expect(await canvas.findByText('LOG_LEVEL · config')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Grant read' })).toBeEnabled();
  },
};

// Opt-in on: the capability selector appears; reveal lists only the secrets
// and names the environment the passkey ceremony will cover.
export const RevealGrant: Story = {
  args: { machineReveal: true, scope: scope.slice(0, 2) },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Capability')).toHaveValue('reveal');
    await expect(canvas.getByLabelText('Environment (reveal)')).toHaveValue(scope[1]?.id);
    await expect(await canvas.findByText('DATABASE_URL · secret')).toBeVisible();
    await expect(canvas.queryByText('LOG_LEVEL · config')).not.toBeInTheDocument();
    await expect(canvas.getByText(/newly decrypts staging/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Grant reveal' })).toBeEnabled();
  },
};

// Every environment already read and the opt-in off: nothing to widen, so the
// body is the one sentence and Close is the only action.
export const NothingToWiden: Story = {
  args: { scope: scope.slice(0, 2) },
  play: async ({ canvas }) => {
    // The org membership read settles first: it 404s, so reporting is not offered.
    await expect(await canvas.findByText(/there is nothing to widen; reveal needs/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Close' })).toBeVisible();
  },
};

// The catalogue read failed: the blast radius cannot be named, so the grant
// button stays held.
export const CatalogueFailed: Story = {
  parameters: { app: { responses: [{ url: KEYS_URL, status: 500, body: { error: 'boom' } }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/the key catalogue could not be read/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Grant read' })).toBeDisabled();
  },
};

// The catalogue read never settles: fail closed, the grant button held.
export const CatalogueLoading: Story = {
  parameters: { app: { responses: [{ url: KEYS_URL, pending: true }] } },
  play: async ({ canvas }) => {
    await waitFor(() =>
      expect(canvas.getByText(/reading what this grant would make reachable/i)).toBeVisible(),
    );
    await expect(canvas.getByRole('button', { name: 'Grant read' })).toBeDisabled();
  },
};

// A workload account and an org member manager: report-delivery-status is one
// checkbox beside read, and ticking it names both grants on the button.
export const ReportingGrantable: Story = {
  parameters: {
    app: {
      responses: [
        { url: KEYS_URL, body: catalogue },
        { url: ORG_GRANTS_URL, body: { count: 0, items: [] } },
      ],
    },
  },
  play: async ({ canvas }) => {
    await userEvent.click(
      await canvas.findByRole('checkbox', { name: /also grant report-delivery-status/i }),
    );
    await expect(
      canvas.getByRole('button', { name: 'Grant read and report-delivery-status' }),
    ).toBeVisible();
  },
};

// A project-scope member manager cannot grant the atom (the org membership
// listing 404s), so the checkbox is not offered: read alone.
export const ReportingNotGrantable: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('LOG_LEVEL · config')).toBeVisible();
    await expect(
      canvas.queryByRole('checkbox', { name: /also grant report-delivery-status/i }),
    ).not.toBeInTheDocument();
    await expect(canvas.getByRole('button', { name: 'Grant read' })).toBeEnabled();
  },
};

// Every environment already read, and an org member manager: the report atom
// is still grantable on its own, and it makes no key reachable.
export const ReportAfterTheFact: Story = {
  args: { scope: scope.slice(0, 2) },
  parameters: {
    app: {
      responses: [
        { url: KEYS_URL, body: catalogue },
        { url: ORG_GRANTS_URL, body: { count: 0, items: [] } },
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByLabelText('Capability')).toHaveValue('report-delivery-status');
    await expect(canvas.getByText(/this grant makes nothing\s+reachable/i)).toBeVisible();
    await expect(
      canvas.getByRole('button', { name: 'Grant report-delivery-status' }),
    ).toBeEnabled();
  },
};

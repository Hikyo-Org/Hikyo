import type { InstanceConfigStatus, RemoteList } from '@hikyo/client';
import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { InstanceConfig } from './InstanceConfig.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The page reads its own configuration status (polled every 2s, the harness
// answers each poll the same way) and, outside a remote workspace, the
// connected-instance list. Wire bodies use plain numbers for the int64 fields:
// `z.coerce.bigint` parses them, and JSON.stringify refuses bigint literals.
// The route is `surfaceById('instance-config')`.
const CONFIG_URL = '/api/v1/instance/config';
const REMOTES_URL = '/api/v1/instance/remotes';

const binding = { org_id: 'org_system', project_id: 'prj_system', environment_id: 'env_system', schema_version: 1 };
const nodeA = { node_id: 'node-a', active_generation: 7, active_revision: 2, state: 'active', updated_at: '2026-09-06T12:00:00Z' } satisfies InstanceConfigStatus['nodes'][number];
const completedJob = { id: 'job-complete', state: 'completed', revision: 2, generation: 7, created_at: '2026-09-06T11:00:00Z', completed_at: '2026-09-06T11:01:00Z' } satisfies InstanceConfigStatus['job'];

const active: InstanceConfigStatus = {
  owner_instance_id: 'instance_local',
  managed: true,
  binding,
  generation: 7,
  desired_revision: 2,
  latest_revision: 3,
  state: 'active',
  nodes: [nodeA, { ...nodeA, node_id: 'node-b', updated_at: '2026-09-06T12:00:05Z' }],
  job: completedJob,
};

const unmanaged: InstanceConfigStatus = {
  owner_instance_id: 'instance_local',
  managed: false,
  binding: null,
  generation: 0,
  desired_revision: null,
  latest_revision: null,
  state: 'unmanaged',
  nodes: [],
  job: null,
};

const pending: InstanceConfigStatus = {
  ...active,
  state: 'pending',
  job: { id: 'job-prepared', state: 'preparing', revision: 3, generation: 8, created_at: '2026-09-06T12:30:00Z', prepared: true },
};

const partial: InstanceConfigStatus = {
  ...active,
  state: 'partial',
  nodes: [
    nodeA,
    { node_id: 'node-b', active_generation: 6, active_revision: 1, state: 'pending', updated_at: '2026-09-06T12:00:05Z', error: 'listener 0.0.0.0:8443 already in use' },
  ],
  job: {
    id: 'job-rollout',
    state: 'partial',
    revision: 3,
    generation: 8,
    created_at: '2026-09-06T12:30:00Z',
    plan_digest: '9c1e4b7a2f0d8e6c5b3a1f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c',
    error: 'node-b did not confirm its installed settings',
  },
};

const recovering: InstanceConfigStatus = { ...active, state: 'recovery_required' };

const remotes: RemoteList = {
  count: 1,
  items: [
    {
      id: 'rmt_123e4567-e89b-12d3-a456-426614174050',
      name: 'eu-west',
      url: 'https://hikyo.eu-west.example',
      spki_pin: 'sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
      created_at: '2026-08-01T00:00:00Z',
      created_by: 'prn_123e4567-e89b-12d3-a456-426614174010',
      state: 'ok',
      last_attempt_at: '2026-09-22T09:00:00Z',
      observed_at: '2026-09-22T09:00:00Z',
      stale: false,
    },
  ],
};

const config = (rest: Partial<MockRoute>): MockRoute => ({ url: CONFIG_URL, ...rest });
const app = (status: Partial<MockRoute>) => ({
  auth: true,
  path: '/instance/config',
  responses: [config(status), { url: REMOTES_URL, body: remotes }],
});

const meta = {
  component: InstanceConfig,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: app({ body: active }) },
} satisfies Meta<typeof InstanceConfig>;

export default meta;
type Story = StoryObj<typeof meta>;

// A managed owner with an applied revision: the project links, the revision
// controls, two converged nodes and one connected instance.
export const Active: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Applied configuration active')).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Edit configuration project' })).toBeVisible();
    await expect(canvas.getByLabelText('Published revision to apply or test')).toHaveValue('3');
    await expect(canvas.getByRole('button', { name: 'Apply selected revision' })).toBeEnabled();
    await expect(canvas.getByText('eu-west')).toBeVisible();
  },
};

// Not adopted yet: startup configuration, and the one-time adoption preview.
export const Unmanaged: Story = {
  parameters: { app: app({ body: unmanaged }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Using startup configuration')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Preview adoption' })).toBeEnabled();
    await expect(canvas.queryByRole('link', { name: 'Edit configuration project' })).toBeNull();
  },
};

// A prepared apply awaiting authorisation: Apply is held while the job is unsettled.
export const Pending: Story = {
  parameters: { app: app({ body: pending }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Apply pending')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Apply selected revision' })).toBeDisabled();
    await expect(canvas.getByText(/last apply: job-prepared · preparing/i)).toBeVisible();
  },
};

// A controlled rollout stuck half-way: the repair alert, the failed node's
// error, and the deployment-restore affordance.
export const Partial: Story = {
  parameters: { app: app({ body: partial }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Partially applied')).toBeVisible();
    await expect(canvas.getByText(/some nodes have not applied/i)).toBeVisible();
    await expect(canvas.getByText(/listener 0\.0\.0\.0:8443 already in use/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Restore deployment' })).toBeEnabled();
  },
};

// Fenced after a restore: Apply stays disabled until the credential review is confirmed.
export const RecoveryRequired: Story = {
  parameters: { app: app({ body: recovering }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Recovery required')).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: /reviewed the restored credentials/i })).not.toBeChecked();
    await expect(canvas.getByRole('button', { name: 'Apply selected revision' })).toBeDisabled();
  },
};

// The status never settles: the loading line stands.
export const Loading: Story = {
  parameters: { app: app({ pending: true }) },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading configuration/i)).toBeVisible());
  },
};

// Configuration not disclosed: the refusal sentence and a refresh, no guessed project.
export const NotDisclosed: Story = {
  parameters: { app: app({ status: 404 }) },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/not disclosed to this session/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Refresh status' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: 'Preview adoption' })).toBeNull();
  },
};

// The reauthentication ceremony for a mail test: the decision is named, the
// owner and generation are bound, and the code field waits. No network runs.
export const TestMailCeremony: Story = {
  play: async ({ canvas }) => {
    await userEvent.type(await canvas.findByLabelText('Test email recipient'), 'ops@acme.example');
    await userEvent.click(canvas.getByRole('button', { name: 'Send test email' }));
    const dialog = await canvas.findByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveTextContent(/test email with revision r3/i);
    await expect(dialog).toHaveTextContent(/send one message to ops@acme\.example/i);
    await expect(canvas.getByRole('button', { name: 'Authorize with code' })).toBeDisabled();
  },
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import type { AdapterTarget, ProjectKey } from '../api/adapters.ts';

import { TargetForm } from './Adapters.tsx';

type Props = Parameters<typeof TargetForm>[0];

const environments: Props['environments'] = [
  { id: 'env_00000000-0000-0000-0000-000000000001', name: 'development' },
  { id: 'env_00000000-0000-0000-0000-000000000002', name: 'staging' },
  { id: 'env_00000000-0000-0000-0000-000000000003', name: 'production' },
];

const key = (id: string, name: string): ProjectKey => ({
  id,
  org_id: 'org_00000000-0000-0000-0000-000000000001',
  project_id: 'prj_00000000-0000-0000-0000-000000000001',
  name,
  folder_path: '',
  classification: 'config',
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: true } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  created_at: '2026-09-15T08:00:00Z',
});

const keys: readonly ProjectKey[] = [
  key('key_00000000-0000-0000-0000-000000000001', 'DATABASE_URL'),
  key('key_00000000-0000-0000-0000-000000000002', 'STRIPE_SECRET_KEY'),
  key('key_00000000-0000-0000-0000-000000000003', 'SENTRY_DSN'),
  key('key_00000000-0000-0000-0000-000000000004', 'OPENAI_API_KEY'),
  key('key_00000000-0000-0000-0000-000000000005', 'REDIS_CONNECTION_STRING'),
  key('key_00000000-0000-0000-0000-000000000006', 'AWS_SECRET_ACCESS_KEY'),
];

const baseTarget: AdapterTarget = {
  id: 'tgt_00000000-0000-0000-0000-000000000001',
  adapter_id: 'apt_00000000-0000-0000-0000-000000000001',
  environment_id: 'env_00000000-0000-0000-0000-000000000002',
  destination_kind: 'repository',
  destination_owner: 'acme',
  destination_name: 'app',
  destination_environment: '',
  destination_id: 1n,
  repository_id: 0n,
  visibility: '',
  selected_repository_ids: [],
  name_prefix: 'PROD_',
  generation: 1n,
  state: 'active',
  sync_status: 'converged',
  converged_revision: 42n,
  last_attempted_revision: 42n,
  last_attempted_at: '2026-09-15T08:00:00Z',
  last_error_class: '',
  retry_at: null,
  paused_at: null,
  drift_attention: false,
  failure_names: [],
  warnings: [],
  keys: [
    { key_id: 'key_00000000-0000-0000-0000-000000000001', name: 'DATABASE_URL', classification: 'config' },
  ],
  conflicts: [],
};

const meta = {
  component: TargetForm,
  tags: ['ai-generated'],
  args: {
    title: 'Add target',
    environments,
    keys,
    busy: false,
    onCancel: fn(),
    onSubmit: fn(async () => undefined),
  },
} satisfies Meta<typeof TargetForm>;

export default meta;
type Story = StoryObj<typeof meta>;

// A fresh repository target: routing is editable and every project key is offered.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('form', { name: 'Add target' })).toBeVisible();
    await expect(canvas.getByRole('combobox', { name: /^environment$/i })).toBeEnabled();
    await expect(canvas.getByRole('checkbox', { name: 'DATABASE_URL' })).toBeInTheDocument();
  },
};

// Editing an existing target locks routing: the destination is moved via the CLI.
export const LockedRouting: Story = {
  args: { title: 'Edit target', initial: baseTarget, lockRouting: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('combobox', { name: /^environment$/i })).toBeDisabled();
  },
};

// An organization target with "selected" visibility reveals the repository-ids field.
export const OrganizationSelected: Story = {
  args: {
    title: 'Edit target',
    initial: {
      ...baseTarget,
      destination_kind: 'organization',
      destination_name: '',
      visibility: 'selected',
      selected_repository_ids: [123456n, 789012n],
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('textbox', { name: /repository ids/i })).toHaveValue(
      '123456, 789012',
    );
  },
};

// A project with no keys shows the empty note in place of the checkbox list.
export const EmptyKeys: Story = {
  args: { keys: [] },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('group', { name: 'Keys' })).toBeVisible();
    await expect(canvas.getByText('This project has no keys yet.')).toBeVisible();
  },
};

// While saving, the submit button reads "Saving…" and is disabled.
export const Busy: Story = {
  args: { busy: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Saving…' })).toBeDisabled();
  },
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import type { AdapterTarget } from '../api/adapters.ts';

import { HealthChip } from './Adapters.tsx';

// One healthy target; each story swaps only sync_status (and drift for the last),
// so the chip's label + modifier class is the whole subject.
const baseTarget: AdapterTarget = {
  id: 'tgt_00000000-0000-0000-0000-000000000001',
  adapter_id: 'apt_00000000-0000-0000-0000-000000000001',
  environment_id: 'env_00000000-0000-0000-0000-000000000001',
  destination_kind: 'repository',
  destination_owner: 'acme',
  destination_name: 'app',
  destination_environment: '',
  destination_id: 1n,
  repository_id: 0n,
  visibility: '',
  selected_repository_ids: [],
  name_prefix: '',
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
  keys: [],
  conflicts: [],
};

const meta = {
  component: HealthChip,
  tags: ['ai-generated'],
  args: { target: baseTarget },
} satisfies Meta<typeof HealthChip>;

export default meta;
type Story = StoryObj<typeof meta>;

// The chip is a role-less <span>, so the label text (from healthLabel) is the
// assertable subject rather than an ARIA role.
export const Never: Story = {
  args: { target: { ...baseTarget, sync_status: 'never' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Never synced')).toBeVisible();
  },
};

export const Pending: Story = {
  args: { target: { ...baseTarget, sync_status: 'pending' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Queued')).toBeVisible();
  },
};

export const Converging: Story = {
  args: { target: { ...baseTarget, sync_status: 'converging' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Converging')).toBeVisible();
  },
};

export const Converged: Story = {
  args: { target: { ...baseTarget, sync_status: 'converged' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Healthy')).toBeVisible();
  },
};

export const Degraded: Story = {
  args: { target: { ...baseTarget, sync_status: 'degraded' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Degraded')).toBeVisible();
  },
};

export const Failed: Story = {
  args: { target: { ...baseTarget, sync_status: 'failed' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Failed')).toBeVisible();
  },
};

export const Paused: Story = {
  args: { target: { ...baseTarget, sync_status: 'paused' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Paused')).toBeVisible();
  },
};

// drift_attention appends " · needs attention" to whatever the label is.
export const DriftAttention: Story = {
  args: { target: { ...baseTarget, sync_status: 'degraded', drift_attention: true } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Degraded · needs attention')).toBeVisible();
  },
};

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

// The target carries bigint revisions, which the docs page cannot serialise
// into an args table ("Do not know how to serialize a BigInt"), so the status
// is the arg and the fixture is built in render.
const meta = {
  title: 'Features/Adapters/HealthChip',
  id: 'routes-healthchip',
  component: HealthChip,
  tags: ['ai-generated'],
  args: { target: baseTarget },
  argTypes: { target: { control: false, table: { disable: true } } },
  parameters: { docs: { description: { component: 'Compact adapter-target synchronization status. A named health state and optional drift-attention text accompany the indicator so status is conveyed by text as well as colour.' }, source: { type: 'code' } }, controls: { disable: true } },
} satisfies Meta<typeof HealthChip>;

export default meta;
type Story = StoryObj<typeof meta>;

// The chip is a role-less <span>, so the label text (from healthLabel) is the
// assertable subject rather than an ARIA role. Each state is a tag: its
// tone class (none for neutral) and the icon before the word.
async function expectTag(word: HTMLElement, tone: 'neutral' | 'ok' | 'changed' | 'danger', glyph: string) {
  const tag = word.closest('.badge');
  await expect([...(tag?.classList ?? [])].filter((name) => /^badge--/.test(name))).toEqual(tone === 'neutral' ? [] : [`badge--${tone}`]);
  await expect(tag?.querySelector('svg.glyph')?.getAttribute('data-glyph')).toBe(glyph);
}
export const Never: Story = {
  args: { target: { ...baseTarget, sync_status: 'never' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Never synced')).toBeVisible();
    await expectTag(canvas.getByText('Never synced'), 'neutral', 'minus');
  },
};

export const Pending: Story = {
  args: { target: { ...baseTarget, sync_status: 'pending' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Queued')).toBeVisible();
    await expectTag(canvas.getByText('Queued'), 'changed', 'wait');
  },
};

export const Converging: Story = {
  args: { target: { ...baseTarget, sync_status: 'converging' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Converging')).toBeVisible();
    await expectTag(canvas.getByText('Converging'), 'changed', 'sync');
  },
};

export const Converged: Story = {
  args: { target: { ...baseTarget, sync_status: 'converged' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Healthy')).toBeVisible();
    await expectTag(canvas.getByText('Healthy'), 'ok', 'check');
  },
};

export const Degraded: Story = {
  args: { target: { ...baseTarget, sync_status: 'degraded' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Degraded')).toBeVisible();
    await expectTag(canvas.getByText('Degraded'), 'danger', 'warn');
  },
};

export const Failed: Story = {
  args: { target: { ...baseTarget, sync_status: 'failed' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Failed')).toBeVisible();
    await expectTag(canvas.getByText('Failed'), 'danger', 'alert');
  },
};

export const Paused: Story = {
  args: { target: { ...baseTarget, sync_status: 'paused' } },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Paused')).toBeVisible();
    await expectTag(canvas.getByText('Paused'), 'neutral', 'pause');
  },
};

// drift_attention appends " · needs attention" to whatever the label is.
export const DriftAttention: Story = {
  args: { target: { ...baseTarget, sync_status: 'degraded', drift_attention: true } },
  play: async ({ canvas }) => {
    // The dot is now the decorative <Sep/> glyph (aria-hidden, spaced by
    // CSS), so "Degraded" and "needs attention" are separate text nodes:
    // match by the phrase that survives the split, not the old literal
    // " · " text.
    const tag = canvas.getByText(/needs attention/);
    await expect(tag).toBeVisible();
    await expect(tag).toHaveTextContent(/Degraded.*needs attention/);
    await expectTag(tag, 'danger', 'warn');
  },
};

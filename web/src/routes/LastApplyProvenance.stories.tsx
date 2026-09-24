import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { LastApplyProvenance } from './DefinitionsBundlePanel.tsx';

// The last-applied labels a Git-managed project shows: whatever the applying
// CLI said about itself. Display only, never verified, and the line says so.
const applied = {
  plan_id: 'pln_123e4567-e89b-12d3-a456-426614174000',
  applied_at: '2026-09-01T10:00:00Z',
  applied_by: 'prn_123e4567-e89b-12d3-a456-426614174000',
  revision: 7n,
};

const meta = {
  component: LastApplyProvenance,
  tags: ['ai-generated'],
  args: { lastApply: applied },
} satisfies Meta<typeof LastApplyProvenance>;

export default meta;
type Story = StoryObj<typeof meta>;

// Commit, ref and actor all supplied: three mono labels and the verification note.
export const Labelled: Story = {
  args: {
    lastApply: { ...applied, commit: 'abc1234', ref: 'refs/heads/main', actor: 'ci@example.com' },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('abc1234')).toBeVisible();
    await expect(canvas.getByText('refs/heads/main')).toBeVisible();
    await expect(canvas.getByText(/display only, not verified/)).toBeVisible();
  },
};

// An apply that named nothing about itself: only the timestamp, no note.
export const Bare: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/last applied/i)).toBeVisible();
    await expect(canvas.queryByText(/display only/)).not.toBeInTheDocument();
  },
};

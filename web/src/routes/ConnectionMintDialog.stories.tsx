import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { ConnectionMintDialog } from './Remotes.tsx';

type Props = Parameters<typeof ConnectionMintDialog>[0];

const minted: Props['minted'] = {
  label: 'staging cluster',
  value: 'hk_9f3a1c7e5b2d84a06f1e93c7d0a5b8e2',
  clamped: false,
};

const meta = {
  component: ConnectionMintDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: { minted, onClose: fn() },
} satisfies Meta<typeof ConnectionMintDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The display-once ceremony: value shown, copy offered, stored-gate present.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('heading', { name: /shown exactly once/i }),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: /copy to clipboard/i })).toBeVisible();
    await expect(canvas.getByText(minted.value)).toBeVisible();
  },
};

// A clamped credential adds the ceiling notice: it expires earlier than asked.
export const Clamped: Story = {
  args: { minted: { ...minted, label: 'short-lived agent', clamped: true } },
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('heading', { name: /shown exactly once/i }),
    ).toBeVisible();
    await expect(canvas.getByText(/lifetime ceiling shortened/i)).toBeVisible();
  },
};

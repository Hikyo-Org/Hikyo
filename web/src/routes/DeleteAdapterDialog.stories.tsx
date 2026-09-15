import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import type { Adapter } from '../api/adapters.ts';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { DeleteAdapterDialog } from './Adapters.tsx';

const adapter: Adapter = {
  id: 'apt_00000000-0000-0000-0000-000000000001',
  provider: 'forgejo',
  origin: 'https://forgejo.example.com',
  credential_present: true,
  authority_principal_id: 'usr_00000000-0000-0000-0000-000000000001',
  state: 'active',
  created_at: '2026-09-15T08:00:00Z',
  targets: [],
};

const meta = {
  component: DeleteAdapterDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: {
    adapter,
    busy: false,
    onCancel: fn(),
    onDecide: fn(),
  },
} satisfies Meta<typeof DeleteAdapterDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Nothing preselected: the retain/prune decision is required, so Delete is off.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: /delete adapter/i })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Delete adapter' })).toBeDisabled();
  },
};

// Picking Prune enables Delete and reports that decision.
export const Prune: Story = {
  play: async ({ args, canvas }) => {
    await userEvent.click(canvas.getByRole('radio', { name: /prune/i }));
    const remove = canvas.getByRole('button', { name: 'Delete adapter' });
    await expect(remove).toBeEnabled();
    await userEvent.click(remove);
    await waitFor(() => expect(args.onDecide).toHaveBeenCalledWith('prune'));
  },
};

// The in-flight delete disables the danger button and relabels it.
export const Busy: Story = {
  args: { busy: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Deleting…' })).toBeDisabled();
  },
};

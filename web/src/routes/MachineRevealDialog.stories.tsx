import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import { MachineRevealDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The per-project machine-reveal opt-in ceremony, both directions. Enabling
// states what a standing decryption capability on a machine principal means and
// gates its button on an acknowledgement; withdrawing states the other half.
const meta = {
  component: MachineRevealDialog,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
  args: { enable: true, busy: false, failure: null, onConfirm: fn(), onClose: fn() },
} satisfies Meta<typeof MachineRevealDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Enable: the primary is held until the acknowledgement is ticked.
export const Enable: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Enable machine secret delivery' })).toBeVisible();
    const confirm = canvas.getByRole('button', { name: 'Enable the opt-in' });
    await expect(confirm).toBeDisabled();
    await userEvent.click(canvas.getByRole('checkbox', { name: /i understand this admits/i }));
    await expect(confirm).toBeEnabled();
  },
};

// Withdraw: reversible by the same act, so no acknowledgement to tick.
export const Withdraw: Story = {
  args: { enable: false },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Withdraw machine secret delivery' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Withdraw the opt-in' })).toBeEnabled();
  },
};

// The write is in flight: every control held.
export const Busy: Story = {
  args: { enable: false, busy: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Withdraw the opt-in' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
  },
};

// The server refused the write (a session short of the second factor): the
// strip repeats what it said.
export const Failed: Story = {
  args: {
    enable: false,
    failure: 'This act needs a second factor on the current session. Present one and try again.',
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/needs a second factor/i)).toBeVisible();
  },
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent } from 'storybook/test';

import type { RetentionPolicy } from '../api/settings.ts';
import { CompactOrgRetention } from './OrgSettings.tsx';

// The one-field org retention row: a number that saves on blur, refuses out
// loud below one, and locks while the save is in flight.
const policy: RetentionPolicy = { mode: 'keep-if-either', max_age_seconds: 7_776_000, last_revisions: 10 };

const meta = {
  component: CompactOrgRetention,
  tags: ['ai-generated'],
  args: { policy, busy: false, onSave: fn() },
} satisfies Meta<typeof CompactOrgRetention>;

export default meta;
type Story = StoryObj<typeof meta>;

// The field seeded from the policy, ready to edit.
export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Org default revisions kept')).toHaveValue(10);
    await expect(canvas.getByLabelText('Org default revisions kept')).toBeEnabled();
  },
};

// A save in flight: the field is locked until the server answers.
export const Busy: Story = {
  args: { busy: true },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Org default revisions kept')).toBeDisabled();
  },
};

// Zero typed and committed: the refusal is said, the field resets, nothing saves.
export const Refused: Story = {
  play: async ({ canvas, args }) => {
    const input = canvas.getByLabelText('Org default revisions kept');
    await userEvent.clear(input);
    await userEvent.type(input, '0');
    await userEvent.tab();
    await expect(canvas.getByRole('alert')).toHaveTextContent('Refused: keep at least 1 revision.');
    await expect(input).toHaveValue(10);
    await expect(args.onSave).not.toHaveBeenCalled();
  },
};

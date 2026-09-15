import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { RetentionBoundsFields } from './RetentionBoundsFields.tsx';

const meta = {
  component: RetentionBoundsFields,
  tags: ['ai-generated'],
  args: {
    age: { kind: 'days', days: '30' },
    count: '10',
    onAgeChange: fn(),
    onCountChange: fn(),
  },
} satisfies Meta<typeof RetentionBoundsFields>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Days: Story = {};

export const Exact: Story = {
  args: { age: { kind: 'exact', seconds: 90_000 } },
  play: async ({ canvas }) => {
    // An exact (non-whole-day) age disables the day input rather than hiding it.
    await expect(canvas.getByRole('alert')).toBeVisible();
    await expect(canvas.getByLabelText(/maximum age, in days/i)).toBeDisabled();
  },
};

export const Absent: Story = {
  args: { age: { kind: 'absent' } },
};

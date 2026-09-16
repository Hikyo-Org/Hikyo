import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Textarea } from './Textarea.tsx';

const meta = {
  component: Textarea,
  tags: ['ai-generated'],
  args: { label: 'Description', placeholder: 'What this project is for' },
} satisfies Meta<typeof Textarea>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Filled: Story = { args: { defaultValue: 'Customer-facing API and its workers.' } };
export const Disabled: Story = { args: { disabled: true, defaultValue: 'locked' } };

export const LabelIsWired: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Description')).toBeVisible();
  },
};

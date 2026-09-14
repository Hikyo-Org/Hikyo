import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { NotFound } from './Placeholder.tsx';

const meta = {
  component: NotFound,
  tags: ['ai-generated'],
} satisfies Meta<typeof NotFound>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /not found/i })).toBeVisible();
  },
};

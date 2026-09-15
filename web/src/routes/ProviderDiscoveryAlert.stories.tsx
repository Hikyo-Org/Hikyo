import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { ProviderDiscoveryAlert } from './ProviderDiscoveryAlert.tsx';

const meta = {
  component: ProviderDiscoveryAlert,
  tags: ['ai-generated'],
  args: { onRetry: fn() },
} satisfies Meta<typeof ProviderDiscoveryAlert>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas, userEvent, args }) => {
    // Proves the retry button is wired to the callback — the render alone doesn't.
    await expect(canvas.getByRole('alert')).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: /retry identity providers/i }));
    await expect(args.onRetry).toHaveBeenCalledOnce();
  },
};

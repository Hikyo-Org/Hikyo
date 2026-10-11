import { interactionOnce } from '../../.storybook/interactionCoverage.ts';
import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { ProviderDiscoveryAlert } from './ProviderDiscoveryAlert.tsx';

const meta = {
  parameters: { docs: { description: { component: "Identity-provider discovery feedback with an explicit retry affordance. Failed discovery remains a visible explanation rather than silently removing the configured sign-in choices." } } },
  title: 'Features/Identity/ProviderDiscoveryAlert',
  id: 'routes-providerdiscoveryalert',
  component: ProviderDiscoveryAlert,
  tags: ['ai-generated'],
  args: { onRetry: fn() },
} satisfies Meta<typeof ProviderDiscoveryAlert>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  tags: ['interaction-once'],
  play: interactionOnce('routes-providerdiscoveryalert--default', async ({ canvas, userEvent, args }) => {
    // Proves the retry button is wired to the callback — the render alone doesn't.
    await expect(canvas.getByRole('alert')).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: /retry identity providers/i }));
    await expect(args.onRetry).toHaveBeenCalledOnce();
  }, async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toBeVisible();
    await expect(canvas.getByRole('button', { name: /retry identity providers/i })).toBeVisible();
  }),
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { ThemeIcon } from './ThemeIcon.tsx';

const meta = {
  component: ThemeIcon,
  tags: ['ai-generated'],
  args: { dark: false },
} satisfies Meta<typeof ThemeIcon>;

export default meta;
type Story = StoryObj<typeof meta>;

// The light-theme glyph: a sun with its rays drawn in. Decorative, so it is
// hidden from assistive tech; the owning button carries the accessible name.
export const Sun: Story = {
  play: async ({ canvasElement }) => {
    const svg = canvasElement.querySelector('svg.theme-icon');
    await expect(svg).not.toBeNull();
    await expect(svg).toHaveAttribute('aria-hidden', 'true');
    await expect(svg).not.toHaveClass('theme-icon--dark');
  },
};

// The dark-theme glyph: the same paths morphed into a crescent moon.
export const Moon: Story = {
  args: { dark: true },
  play: async ({ canvasElement }) => {
    await expect(canvasElement.querySelector('svg.theme-icon')).toHaveClass('theme-icon--dark');
  },
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor } from 'storybook/test';

import { ThemeToggle } from './Shell.tsx';

// The toggle writes the chosen theme to localStorage, which would leak into the
// next story's initial state; clear it before each so story order never matters.
function clearThemeChoice(): void {
  globalThis.localStorage?.removeItem('hikyo.theme');
}

const meta = {
  component: ThemeToggle,
  tags: ['ai-generated'],
  beforeEach: () => {
    clearThemeChoice();
    return clearThemeChoice;
  },
} satisfies Meta<typeof ThemeToggle>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  play: async ({ canvas }) => {
    // The label names the ACTION and depends on the painted theme, which follows
    // the OS preference until a choice is made — so match either direction.
    await expect(canvas.getByRole('button', { name: /switch to (dark|light) theme/i })).toBeVisible();
  },
};

// Clicking flips the theme, so the action the label offers flips with it.
export const Toggles: Story = {
  play: async ({ canvas }) => {
    const button = canvas.getByRole('button');
    const before = button.getAttribute('aria-label');
    await userEvent.click(button);
    await waitFor(() => expect(button.getAttribute('aria-label')).not.toBe(before));
  },
};

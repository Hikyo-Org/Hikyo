import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { SidebarVersion } from './Shell.tsx';

type Props = Parameters<typeof SidebarVersion>[0];

const meta = {
  component: SidebarVersion,
  tags: ['ai-generated'],
} satisfies Meta<typeof SidebarVersion>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { version: '1.4.2' satisfies Props['version'] },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Version')).toBeVisible();
    await expect(canvas.getByText('1.4.2')).toBeVisible();
  },
};

// While the version query is unresolved the footer renders nothing rather than a
// placeholder that would flash (see the component's own note).
export const Absent: Story = {
  args: { version: undefined satisfies Props['version'] },
  play: async ({ canvas }) => {
    await expect(canvas.queryByText('Version')).toBeNull();
  },
};

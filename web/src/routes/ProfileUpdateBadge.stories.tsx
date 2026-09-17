import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { ProfileUpdateBadge } from './Shell.tsx';

type Props = Parameters<typeof ProfileUpdateBadge>[0];

const meta = {
  component: ProfileUpdateBadge,
  // The badge is a 10px dot pinned to the corner of the account avatar; alone
  // it has no corner to pin to, so the story mounts it on one.
  decorators: [
    (Story) => (
      <span className="avatar" style={{ position: 'relative' }} aria-hidden="true">
        AL
        <Story />
      </span>
    ),
  ],
  tags: ['ai-generated'],
} satisfies Meta<typeof ProfileUpdateBadge>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { version: '1.4.2' satisfies Props['version'] },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText(/update 1\.4\.2 available/i)).toBeInTheDocument();
  },
};

// The badge concatenates every available version, so it can carry more than one.
export const MultipleVersions: Story = {
  args: { version: '1.4.2, 2.0.0' satisfies Props['version'] },
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText(/update 1\.4\.2, 2\.0\.0 available/i)).toBeInTheDocument();
  },
};

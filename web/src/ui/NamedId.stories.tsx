import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, within } from 'storybook/test';

import { NamedId } from './NamedId.tsx';

const ID = 'prn_01989abc-def0-7123-8123-000000000001';

const meta = {
  title: 'Design system/NamedId',
  id: 'ui-named-id',
  component: NamedId,
  tags: ['ai-generated'],
  args: { id: ID, name: 'Ada Lovelace' },
} satisfies Meta<typeof NamedId>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Named: Story = {
  play: async ({ canvasElement }) => {
    await expect(within(canvasElement).getByText('Ada Lovelace')).toHaveAttribute('title', ID);
  },
};

export const FallsBackToTheId: Story = {
  args: { name: undefined },
  play: async ({ canvasElement }) => {
    await expect(within(canvasElement).getByText(ID)).toHaveClass('mono');
  },
};

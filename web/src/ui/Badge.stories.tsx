import type { Meta, StoryObj } from '@storybook/react-vite';

import { Badge } from './Badge.tsx';

const meta = {
  component: Badge,
  tags: ['ai-generated'],
  args: { children: 'Active', tone: 'neutral' },
  argTypes: { tone: { control: 'select', options: ['neutral', 'danger', 'warn'] } },
} satisfies Meta<typeof Badge>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Neutral: Story = {};
export const Danger: Story = { args: { children: 'Unreachable', tone: 'danger' } };
export const Warn: Story = { args: { children: 'Pending', tone: 'warn' } };

export const AllTones: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
      <Badge>Active</Badge>
      <Badge tone="warn">Pending</Badge>
      <Badge tone="danger">Unreachable</Badge>
    </div>
  ),
};

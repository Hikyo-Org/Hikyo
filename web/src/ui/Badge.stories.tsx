import type { Meta, StoryObj } from '@storybook/react-vite';

import { Badge } from './Badge.tsx';

const meta = {
  component: Badge,
  tags: ['ai-generated'],
  args: { children: 'Active', tone: 'neutral' },
  argTypes: { tone: { control: 'select', options: ['neutral', 'danger', 'warn', 'changed', 'ok'] } },
} satisfies Meta<typeof Badge>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Neutral: Story = {};
export const Danger: Story = { args: { children: 'Unreachable', tone: 'danger' } };
export const Changed: Story = { args: { children: 'Draft', tone: 'changed' } };
export const Ok: Story = { args: { children: 'Verified', tone: 'ok' } };
export const Mono: Story = { args: { children: 'rev 12', mono: true } };

const tones = (
  <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
    <Badge>Active</Badge>
    <Badge tone="changed">Pending</Badge>
    <Badge tone="danger">Unreachable</Badge>
    <Badge tone="ok">Verified</Badge>
    <Badge mono>rev 12</Badge>
  </div>
);

export const AllTones: Story = { render: () => tones };


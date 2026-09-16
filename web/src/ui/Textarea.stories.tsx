import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Textarea } from './Textarea.tsx';

const meta = {
  component: Textarea,
  tags: ['ai-generated'],
  args: { label: 'Description', placeholder: 'What this project is for' },
} satisfies Meta<typeof Textarea>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Filled: Story = { args: { defaultValue: 'Customer-facing API and its workers.' } };
export const Disabled: Story = { args: { disabled: true, defaultValue: 'locked' } };

export const LabelIsWired: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Description')).toBeVisible();
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 360 }}>
      <Textarea label="Description" placeholder="What this project is for" />
      <Textarea label="Filled" defaultValue="Customer-facing API and its workers." hint="Shown on the projects list." />
      <Textarea label="Refused" defaultValue="x" error="At least three characters." />
      <Textarea label="Disabled" defaultValue="locked" disabled />
    </div>
  ),
};

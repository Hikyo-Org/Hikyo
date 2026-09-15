import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Select } from './Select.tsx';

const meta = {
  component: Select,
  tags: ['ai-generated'],
  args: {
    label: 'Environment',
    children: (
      <>
        <option value="prod">Production</option>
        <option value="staging">Staging</option>
        <option value="dev">Development</option>
      </>
    ),
  },
} satisfies Meta<typeof Select>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Disabled: Story = { args: { disabled: true } };

export const LabelIsWired: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByLabelText('Environment')).toBeVisible();
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 320 }}>
      <Select label="Environment" defaultValue="staging">
        <option value="prod">Production</option>
        <option value="staging">Staging</option>
        <option value="dev">Development</option>
      </Select>
      <Select label="Disabled" disabled>
        <option>Production</option>
      </Select>
    </div>
  ),
};

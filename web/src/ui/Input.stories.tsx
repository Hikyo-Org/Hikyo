import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Input } from './Input.tsx';

const meta = {
  component: Input,
  tags: ['ai-generated'],
  args: { label: 'Username', placeholder: 'you@example.com' },
} satisfies Meta<typeof Input>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Password: Story = { args: { label: 'Password', type: 'password' } };
export const Disabled: Story = { args: { disabled: true, defaultValue: 'locked' } };

export const LabelIsWired: Story = {
  play: async ({ canvas }) => {
    // The label must resolve the control, or the field is a swatch with no name.
    await expect(canvas.getByLabelText('Username')).toBeVisible();
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 320 }}>
      <Input label="Username" placeholder="you@example.com" />
      <Input label="Password" type="password" />
      <Input label="Disabled" defaultValue="locked" disabled />
    </div>
  ),
};


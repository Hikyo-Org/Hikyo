import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent } from 'storybook/test';

import { Radio } from './Radio.tsx';

const meta = {
  component: Radio,
  tags: ['ai-generated'],
  args: { label: 'Every environment', name: 'scope' },
} satisfies Meta<typeof Radio>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Checked: Story = { args: { defaultChecked: true } };
export const Disabled: Story = { args: { disabled: true } };

export const Selects: Story = {
  play: async ({ canvas }) => {
    const radio = canvas.getByRole('radio');
    await expect(radio).not.toBeChecked();
    await userEvent.click(radio);
    await expect(radio).toBeChecked();
  },
};

// Each row is its own group, so the states do not steal each other's check.
const states = (
  <>
    <Radio name="a" label="Unchecked" />
    <Radio name="b" label="Checked" defaultChecked />
    <Radio name="c" label="Disabled" disabled />
    <Radio name="d" label="Disabled checked" defaultChecked disabled />
  </>
);

export const AllStates: Story = {
  render: () => <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>{states}</div>,
};


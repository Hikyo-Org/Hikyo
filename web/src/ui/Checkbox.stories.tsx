import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent } from 'storybook/test';

import { Checkbox } from './Checkbox.tsx';

const meta = {
  component: Checkbox,
  tags: ['ai-generated'],
  args: { label: 'Allow credentials with no expiry' },
} satisfies Meta<typeof Checkbox>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Checked: Story = { args: { defaultChecked: true } };
export const Disabled: Story = { args: { disabled: true } };

export const Toggles: Story = {
  play: async ({ canvas }) => {
    const box = canvas.getByRole('checkbox');
    await expect(box).not.toBeChecked();
    await userEvent.click(box);
    await expect(box).toBeChecked();
  },
};

// Clicking the label must toggle the box: the label is the larger part of the
// hit target on a fine pointer, where the box itself is 18px.
export const LabelToggles: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByText('Allow credentials with no expiry'));
    await expect(canvas.getByRole('checkbox')).toBeChecked();
  },
};

// The hit target stays the input's own box: 18px on a fine pointer, the 44px
// touch floor on a coarse one. The drawn square never exceeds 20px.
export const HitBoxIsTheInput: Story = {
  play: async ({ canvas }) => {
    const box = canvas.getByRole('checkbox');
    const rect = box.getBoundingClientRect();
    const coarse = matchMedia('(pointer: coarse)').matches;
    const want = coarse ? 44 : 18;
    await expect(Math.round(rect.width)).toBe(want);
    await expect(Math.round(rect.height)).toBe(want);
    const drawn = Number.parseFloat(getComputedStyle(box, '::before').width);
    await expect(drawn).toBeLessThanOrEqual(20);
  },
};

const states = (
  <>
    <Checkbox label="Unchecked" />
    <Checkbox label="Checked" defaultChecked />
    <Checkbox label="Disabled" disabled />
    <Checkbox label="Disabled checked" defaultChecked disabled />
    <Checkbox
      label="A longer label that wraps onto a second line so the box's vertical alignment against multi-line text is visible"
    />
  </>
);

export const AllStates: Story = {
  render: () => <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 360 }}>{states}</div>,
};


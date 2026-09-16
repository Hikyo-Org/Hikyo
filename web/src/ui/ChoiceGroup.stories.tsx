import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Checkbox } from './Checkbox.tsx';
import { ChoiceGroup } from './ChoiceGroup.tsx';
import { Radio } from './Radio.tsx';

const meta = {
  component: ChoiceGroup,
  tags: ['ai-generated'],
  args: {
    legend: 'Outcomes',
    children: (
      <>
        <Checkbox label="Allowed" defaultChecked />
        <Checkbox label="Refused" defaultChecked />
        <Checkbox label="Failed" />
      </>
    ),
  },
} satisfies Meta<typeof ChoiceGroup>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Checkboxes: Story = {};

export const Inline: Story = { args: { inline: true } };

export const WithHint: Story = {
  args: { hint: 'Events outside the chosen outcomes are hidden from the list, not deleted.' },
};

export const Radios: Story = {
  args: {
    legend: 'Presence',
    hint: 'How the key must be set before an environment can publish.',
    children: (
      <>
        <Radio name="presence" label="Optional" defaultChecked />
        <Radio name="presence" label="Required in every environment" />
        <Radio name="presence" label="Required in protected environments only" />
      </>
    ),
  },
};

export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <ChoiceGroup legend="Outcomes">
        <Checkbox label="Allowed" defaultChecked />
        <Checkbox label="Refused" />
      </ChoiceGroup>
      <ChoiceGroup legend="Outcomes, inline, with a hint" hint="Events outside the chosen outcomes are hidden." inline>
        <Checkbox label="Allowed" defaultChecked />
        <Checkbox label="Refused" />
        <Checkbox label="Failed" disabled />
      </ChoiceGroup>
      <ChoiceGroup legend="Presence">
        <Radio name="p" label="Optional" defaultChecked />
        <Radio name="p" label="Required" />
      </ChoiceGroup>
    </div>
  ),
};

// The legend names the whole set: a screen reader announces "Outcomes, group"
// before the first row, which is what a fieldset is for.
export const LegendNamesTheGroup: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('group', { name: 'Outcomes' })).toBeVisible();
    await expect(canvas.getAllByRole('checkbox')).toHaveLength(3);
  },
};

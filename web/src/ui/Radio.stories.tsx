import type { Meta, StoryObj } from '@storybook/react-vite';

import { Radio } from './Radio.tsx';

// Every example carries its OWN group name: the Docs page renders all of them
// inline in one document with no form owner between them, so a shared `name`
// would make one native radio group of the lot and selecting Default would
// uncheck Checked.
const meta = {
  parameters: { docs: { description: { component: "A labelled native radio with shared focus, pointer-target and disabled styling. The name identifies its native selection group; each Docs example uses an independent group so one example cannot uncheck another." } } },
  title: 'Design system/Radio',
  id: 'ui-radio',
  component: Radio,
  tags: ['ai-generated'],
  args: { label: 'Every environment' },
} satisfies Meta<typeof Radio>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = { args: { name: 'scope-default' } };
export const Checked: Story = { args: { name: 'scope-checked', defaultChecked: true } };
export const Disabled: Story = { args: { name: 'scope-disabled', disabled: true } };

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
  args: { name: 'scope-all' },
  render: () => <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>{states}</div>,
};


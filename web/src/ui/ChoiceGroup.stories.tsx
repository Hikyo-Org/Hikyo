import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Checkbox } from './Checkbox.tsx';
import { ChoiceGroup } from './ChoiceGroup.tsx';
import { Radio } from './Radio.tsx';

const outcomes = (
  <>
    <Checkbox label="Allowed" defaultChecked />
    <Checkbox label="Refused" defaultChecked />
    <Checkbox label="Failed" />
    <Checkbox label="Pending approval" />
    <Checkbox label="Expired" disabled />
  </>
);

const keyNames = [
  'DATABASE_URL', 'REDIS_URL', 'AUTH_SECRET', 'LOG_LEVEL', 'FEATURE_CHECKOUT', 'PUBLIC_APP_URL',
  'SMTP_HOST', 'SMTP_PASSWORD', 'SENTRY_DSN', 'STRIPE_KEY', 'S3_BUCKET', 'S3_REGION',
];
const keys = (
  <>
    {keyNames.map((name, index) => (
      <Checkbox key={name} label={name} mono defaultChecked={index < 3} />
    ))}
  </>
);

const meta = {
  component: ChoiceGroup,
  tags: ['ai-generated'],
  args: { legend: 'Outcomes', children: outcomes },
  argTypes: {
    layout: { control: 'select', options: ['stack', 'wrap', 'nowrap'] },
    columns: { control: { type: 'number', min: 1, max: 6 } },
    variant: { control: 'select', options: ['rows', 'chips'] },
  },
} satisfies Meta<typeof ChoiceGroup>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Stack: Story = {};
export const Wrap: Story = { args: { layout: 'wrap' } };
export const Nowrap: Story = {
  args: { legend: 'Keys to include', layout: 'nowrap', children: keys },
  decorators: [(Story) => <div style={{ maxWidth: 520 }}><Story /></div>],
};
export const TwoColumns: Story = { args: { columns: 2 } };
export const ThreeColumns: Story = { args: { legend: 'Keys to include', columns: 3, children: keys } };
export const Chips: Story = {
  args: { legend: 'Keys to include', variant: 'chips', layout: 'wrap', children: keys, hint: 'Applied to every name at the provider.' },
};
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
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24, maxWidth: 640 }}>
      <ChoiceGroup legend="Stack (default)">{outcomes}</ChoiceGroup>
      <ChoiceGroup legend="Wrap" layout="wrap">{outcomes}</ChoiceGroup>
      <ChoiceGroup legend="Nowrap, scrolls sideways" layout="nowrap">{keys}</ChoiceGroup>
      <ChoiceGroup legend="Two columns" columns={2}>{outcomes}</ChoiceGroup>
      <ChoiceGroup legend="Chips, wrapping" variant="chips" layout="wrap">{keys}</ChoiceGroup>
      <ChoiceGroup legend="Chips, three columns" variant="chips" columns={3}>{keys}</ChoiceGroup>
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
    await expect(canvas.getAllByRole('checkbox')).toHaveLength(5);
  },
};

// Columns are a grid with that many tracks at this width; the options stay
// in reading order, so "one row of N" and "N columns" are the same thing.
export const ColumnsAreAGrid: Story = {
  args: { columns: 3 },
  play: async ({ canvasElement }) => {
    const options = canvasElement.querySelector('.choice-group__options');
    await expect(options).not.toBeNull();
    const tracks = getComputedStyle(options!).gridTemplateColumns.split(' ').length;
    await expect(tracks).toBe(3);
  },
};

// Nowrap keeps one line and scrolls; every option is still a keyboard stop.
export const NowrapScrolls: Story = {
  ...Nowrap,
  play: async ({ canvas, canvasElement }) => {
    const options = canvasElement.querySelector('.choice-group__options');
    await expect(options).not.toBeNull();
    await expect(options!.scrollWidth).toBeGreaterThan(options!.clientWidth);
    const boxes = canvas.getAllByRole('checkbox');
    const tops = new Set(boxes.map((box) => Math.round(box.getBoundingClientRect().top)));
    await expect(tops.size).toBe(1);
  },
};

// A chip names its state twice: the box and the border. It stands at the
// control height, so a chip beside a button lines up.
export const ChipsMarkChecked: Story = {
  ...Chips,
  play: async ({ canvas }) => {
    const checked = canvas.getByRole('checkbox', { name: 'DATABASE_URL' });
    const chip = checked.closest('.chk');
    await expect(chip).not.toBeNull();
    const control = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--control'));
    await expect(Math.round(chip!.getBoundingClientRect().height)).toBe(Math.round(control));
    const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();
    await expect(getComputedStyle(chip!).borderTopColor.replace(/\s+/g, '')).toBe(accent.replace(/\s+/g, ''));
  },
};

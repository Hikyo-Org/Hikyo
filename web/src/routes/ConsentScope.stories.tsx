import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, within } from 'storybook/test';

import { ConsentKeys, type ConsentKey } from './ConsentScope.tsx';

const LONG_NAME = 'STRIPE_WEBHOOK_SIGNING_SECRET_PRODUCTION_EU_WEST_PRIMARY_REGION';
const keys: ConsentKey[] = [
  { id: 'key_01989abc-def0-7123-8123-000000000001', name: LONG_NAME, classification: 'secret' },
  { id: 'key_01989abc-def0-7123-8123-000000000002', name: 'LOG_LEVEL', classification: 'config' },
];

const meta = {
  title: 'Pages/ConsentKeys',
  id: 'routes-consentkeys',
  component: ConsentKeys,
  tags: ['ai-generated'],
  args: { keys },
  decorators: [(Story) => <div style={{ width: 280 }}><Story /></div>],
} satisfies Meta<typeof ConsentKeys>;

export default meta;
type Story = StoryObj<typeof meta>;

// A name with no spaces or hyphens still has to be readable in full: this is
// what the person is authorizing, and hovering shows the ID, not the name.
export const LongNameWraps: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByText('2 keys')).toBeVisible();
    const list = canvas.getByRole('list', { name: 'Keys this decision covers' });
    await expect(list.scrollWidth).toBeLessThanOrEqual(list.clientWidth);
    await expect(within(list).getByText(LONG_NAME).closest('li')).toHaveAttribute('title', keys[0]?.id);
  },
};

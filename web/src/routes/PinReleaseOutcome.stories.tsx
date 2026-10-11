import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { PinReleaseOutcome } from './HistoryDrawer.tsx';

// The sentence the history drawer's "done" alert carries after a pin release:
// the server's retention consequence decides the second half. One story per
// consequence, the locked taxonomy.
const meta = {
  parameters: { docs: { description: { component: "The history drawer confirmation after an environment revision pin is released. The server reports whether values remain retained, become eligible for collection, or had already been collected before the locked release completed." } } },
  title: 'Features/Values/PinReleaseOutcome',
  id: 'routes-pinreleaseoutcome',
  component: PinReleaseOutcome,
  tags: ['ai-generated'],
  args: { revision: 9n },
} satisfies Meta<typeof PinReleaseOutcome>;

export default meta;
type Story = StoryObj<typeof meta>;

// The values survive: policy or another live pin still keeps them.
export const Retained: Story = {
  args: { consequence: 'retained' },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/r9's values remain retained by current policy or another live pin\.$/)).toBeVisible();
  },
};

// The released pin was the sole keeper: a sweep may collect them at once.
export const CollectionEligible: Story = {
  args: { consequence: 'collection_eligible' },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/r9's values became eligible for collection/)).toBeVisible();
  },
};

// The values were gone before the release completed; lineage stays.
export const AlreadyCollected: Story = {
  args: { consequence: 'already_collected' },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/r9's values were already collected before release completed/)).toBeVisible();
  },
};

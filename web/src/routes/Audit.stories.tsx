import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, waitFor } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { Audit } from './Audit.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// One denied grant event; the trail page wraps it in the cursor envelope the
// infinite query reads (a single exhausted page, no ceiling to chase).
const event = {
  seq: 1,
  id: 'evt_1',
  type: 'grant.created',
  schema_version: 1,
  occurred_at: '2026-09-01T00:00:00Z',
  occurred_asserted: false,
  recorded_at: '2026-09-01T00:00:00Z',
  actor_class: 'user',
  actor_id: 'prn_dana',
  actor_name: 'Dana Jacobs',
  scope_class: 'org',
  outcome: 'denied',
  origin: 'api',
  payload: {},
};

const page = (items: readonly (typeof event)[]) => ({
  items,
  count: items.length,
  next_after_seq: 0,
  upper_seq: 0,
  exhausted: true,
});

const trail = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/audit(\?|$)/, ...rest });

const meta = {
  component: Audit,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: { auth: true, path: '/orgs/acme/audit', routePath: '/orgs/:org/audit', responses: [] },
  },
} satisfies Meta<typeof Audit>;

export default meta;
type Story = StoryObj<typeof meta>;

// The trail resolved to events: rows carry the actor name and the outcome glyph.
export const Populated: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/orgs/acme/audit',
      routePath: '/orgs/:org/audit',
      responses: [trail({ body: page([event]) })],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: /Dana Jacobs/ })).toBeVisible();
  },
};

// The trail resolved empty: the filter's own "no events" status and its Clear.
export const Empty: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/orgs/acme/audit',
      routePath: '/orgs/:org/audit',
      responses: [trail({ body: page([]) })],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/no events match this filter/i)).toBeVisible();
  },
};

// The trail never settles: the loading status stands.
export const Loading: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/orgs/acme/audit',
      routePath: '/orgs/:org/audit',
      responses: [trail({ pending: true })],
    },
  },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading events/i)).toBeVisible());
  },
};

// The trail read failed: the page surfaces its error path rather than a blank.
export const Failed: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/orgs/acme/audit',
      routePath: '/orgs/:org/audit',
      responses: [trail({ status: 500, body: { error: 'audit read failed' } })],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/could not|failed|unavailable/i)).toBeVisible();
  },
};

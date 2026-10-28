import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { Audit, InstanceAudit } from './Audit.tsx';

import { expectLeftAlignedPage } from '../../.storybook/layout.ts';
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
  title: 'Pages/Audit',
  id: 'routes-audit',
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
    // One button size (DESIGN.md): the quiet "Clear" and "Self" match "Apply
    // filter" in height, padding and type, and the row keeps the one gap.
    const metrics = (el: HTMLElement) => [el.getBoundingClientRect().height, getComputedStyle(el).paddingLeft, getComputedStyle(el).fontSize];
    const apply = canvas.getByRole('button', { name: 'Apply filter' });
    for (const name of ['Clear', 'Self']) await expect(metrics(canvas.getByRole('button', { name }))).toEqual(metrics(apply));
    const row = apply.parentElement!;
    await expect(row).toHaveClass('actions');
    const boxes = [...row.children].map((child) => child.getBoundingClientRect());
    for (let i = 1; i < boxes.length; i++) await expect(boxes[i]!.left - boxes[i - 1]!.right).toBe(8);
    // The "From" datetime-local input takes the same field box as an
    // ordinary text input, not the browser-default look.
    const from = canvas.getByLabelText('From');
    const principal = canvas.getByLabelText('Principal');
    for (const prop of ['height', 'borderTopWidth', 'borderTopLeftRadius'] as const) {
      const value = (el: HTMLElement) => (prop === 'height' ? el.getBoundingClientRect().height : Number.parseFloat(getComputedStyle(el)[prop]));
      await expect(value(from)).toBe(value(principal));
    }
  },
};

// Filter and Events stay together on one page, no tabs.
export const NoSectionTabs: Story = {
  parameters: Populated.parameters,
  play: async ({ canvas }) => {
    // The tree is replaced once the session binds; wait on the live one.
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Filter' })).toBeVisible());
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Events' })).toBeVisible());
    await expect(canvas.queryByRole('tablist')).toBeNull();
    await expectLeftAlignedPage(canvas.getByRole('heading', { name: 'Audit', level: 1 }));
  },
};

// The project trail is the same one page: Filter and Events, no tabs.
export const ProjectNoSectionTabs: Story = {
  parameters: {
    app: {
      auth: true,
      path: '/orgs/acme/projects/app/audit',
      routePath: '/orgs/:org/projects/:project/audit',
      responses: [
        { url: /\/environments(\?|$)/, body: { items: [], count: 0 } },
        trail({ body: page([{ ...event, scope_class: 'project' }]) }),
      ],
    },
  },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Project audit', level: 1 })).toBeVisible());
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Filter' })).toBeVisible());
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Events' })).toBeVisible());
    await expect(canvas.queryByRole('tablist')).toBeNull();
  },
};

const instanceEvent = { ...event, scope_class: 'instance', actor_class: 'human', source_ip: '192.0.2.7', user_agent: 'Instance audit fixture', actor_credential_id: 'cred_fixture' };

export const InstanceSecurity: Story = {
  render: () => <InstanceAudit />,
  parameters: {
    app: {
      auth: true, path: '/instance/audit', routePath: '/instance/audit',
      responses: [trail({ body: page([instanceEvent]) })],
    },
  },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'Instance audit', level: 1 })).toBeVisible());
    await userEvent.click(await canvas.findByRole('button', { name: /Dana Jacobs/ }));
    const panel = canvas.getByRole('complementary', { name: 'Event detail' });
    await expect(within(panel).getByText('192.0.2.7')).toBeVisible();
    await expect(within(panel).getByText('Instance audit fixture')).toBeVisible();
    await expect(within(panel).getByText('cred_fixture')).toBeVisible();
  },
};

export const InstancePhone: Story = {
  ...InstanceSecurity,
  globals: { viewport: { value: 'phone', isRotated: false } },
};

export const InstanceExportFailure: Story = {
  render: InstanceSecurity.render,
  parameters: {
    app: {
      auth: true, path: '/instance/audit', routePath: '/instance/audit',
      responses: [
        trail({ body: page([instanceEvent]) }),
        { url: /\/instance\/audit\/export/, status: 500, body: { error: { code: 'internal', message: 'Synthetic export failure' } } },
      ],
    },
  },
  play: async ({ canvas }) => {
    await canvas.findByRole('button', { name: /Dana Jacobs/ });
    await userEvent.click(canvas.getByRole('button', { name: 'Export JSONL' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent('No download was saved.');
  },
};

// An event opens its detail in the side panel over the page.
export const EventDetailPanel: Story = {
  parameters: Populated.parameters,
  play: async ({ canvas }) => {
    const row = await canvas.findByRole('button', { name: /Dana Jacobs/ });
    await expect(canvas.queryByRole('complementary', { name: 'Event detail' })).toBeNull();
    await userEvent.click(row);
    const panel = canvas.getByRole('complementary', { name: 'Event detail' });
    await expect(within(panel).getByRole('heading', { name: 'grant.created' })).toBeVisible();
    await expect(within(panel).getByText('prn_dana')).toBeVisible();
  },
};

// The row's four columns keep apart: operation, outcome, principal and time
// never touch.
export const RowColumns: Story = {
  parameters: Populated.parameters,
  play: async ({ canvas }) => {
    const row = await canvas.findByRole('button', { name: /Dana Jacobs/ });
    const cells = [...row.children].map((cell) => cell.getBoundingClientRect());
    for (let index = 1; index < cells.length; index++) {
      const [before, after] = [cells[index - 1], cells[index]];
      if (before === undefined || after === undefined) throw new Error('row cell missing');
      // Side by side, or (phone) on the next line.
      await expect(after.left - before.right >= 8 || after.top >= before.bottom).toBe(true);
    }
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

// Each Outcomes option is the tag that outcome wears in the events
// list (same tone, same icon), and the six share one row in the 860px page
// column at desktop width.
export const OutcomeFilterTags: Story = {
  parameters: {
    ...Populated.parameters,
    viewport: { options: { desktop1440: { name: 'Desktop 1440', styles: { width: '1440px', height: '900px' } } } },
  },
  globals: { viewport: { value: 'desktop1440', isRotated: false } },
  decorators: [(Story) => <div style={{ maxWidth: 860 }}><Story /></div>],
  play: async ({ canvas }) => {
    const row = await canvas.findByRole('button', { name: /Dana Jacobs/ });
    const outcomes = canvas.getByRole('group', { name: 'Outcomes' });
    const options = [...outcomes.querySelectorAll('.chk')];
    await expect(options).toHaveLength(6);
    for (const option of options) {
      const tag = option.querySelector('label .badge');
      await expect(tag).not.toBeNull();
      await expect(tag?.querySelector('svg.glyph[aria-hidden="true"]')).not.toBeNull();
      // The checkbox keeps the plain word as its name.
      const box = option.querySelector('input');
      await expect(box).toHaveAccessibleName(tag?.textContent ?? '');
    }
    const listed = row.querySelector('.audit__outcome');
    const filtered = within(outcomes).getByText('denied').closest('.badge');
    await expect(filtered?.className).toBe(listed?.className);
    await expect(filtered?.querySelector('svg')?.getAttribute('data-glyph')).toBe(listed?.querySelector('svg')?.getAttribute('data-glyph'));
    await expect(getComputedStyle(filtered!).backgroundColor).toBe(getComputedStyle(listed!).backgroundColor);
    const tops = new Set(options.map((option) => Math.round(option.getBoundingClientRect().top)));
    await expect(tops.size).toBe(1);
  },
};

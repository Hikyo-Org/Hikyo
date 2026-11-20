import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { dynamicProvider } from '../testkit/machineAccess.ts';
import {
  deletedOwedRotation,
  GONE_ROTATION,
  haltedRotation,
  HALTED_ROTATION,
  HELD_ROTATION,
  LONG_PASSWORD_NAME,
  longKeyNameRotation,
  owedRuns,
  ROTATION,
  rotationKeys,
  rotationList,
  settledRuns,
} from '../testkit/rotations.ts';
import { RotationsPanel } from './machineAccess/Rotations.tsx';

// The Rotations tab of machine access (#148 S3): status only. Each story runs
// the real panel against the harness's fetch table, so the data code, the
// paging and the refusal paths are the production ones. The list and runs rows
// are the wire shapes (`testkit/rotations.ts`), parsed by the generated schema.
const BASE = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const LIST = `${BASE}/environments/${PROD}/rotations`;
const runs = (rotation: string, body: unknown): MockRoute => ({ url: `${LIST}/${rotation}/runs`, body });
const keys: MockRoute = { url: `${BASE}/keys`, body: rotationKeys };
const list = (items: unknown[]): MockRoute => ({ url: LIST, body: { items } });

const meta = {
  title: 'Features/MachineAccess/RotationsPanel',
  component: RotationsPanel,
  tags: ['ai-generated'],
  args: {
    org: ORG,
    project: PRJ,
    environments: [
      { id: PROD, name: 'production' },
      { id: STAGING, name: 'staging' },
    ],
    environmentsRead: 'ready',
    providers: [dynamicProvider],
  },
  parameters: {
    ...topLayerDocs,
    app: { responses: [keys, list([...rotationList().items, haltedRotation(), deletedOwedRotation()]), runs(ROTATION, owedRuns), runs(HELD_ROTATION, { items: [] })] },
    docs: {
      ...topLayerDocs.docs,
      description: {
        component:
          'Scheduled credential rotation status for one environment: the schedule, the state in words, a loud restore-hold line that names the release command, and a run timeline whose owed-drop runs carry the CLI safety wording. No mutation: every change is a hikyo rotation command.',
      },
    },
  },
} satisfies Meta<typeof RotationsPanel>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Populated: Story = {
  play: async ({ canvas }) => {
    const table = await canvas.findByRole('table', { name: /credential rotations/ });
    await expect(within(table).getAllByRole('row')).toHaveLength(5);
    // Status is a word, never only a colour, and the table has no act to take.
    await expect(within(table).getByText('halted')).toBeVisible();
    await expect(within(table).getAllByRole('button').map((button) => button.textContent)).toEqual(['Show runs', 'Show runs', 'Show runs', 'Show runs']);
    await expect(within(table).getByRole('button', { name: 'Show runs of ANALYTICS_PASSWORD' })).toBeVisible();
    // The id is labelled (the CLI takes it) and the record fields are on the row.
    await expect(within(table).getAllByText('Rotation', { selector: 'dt' })).toHaveLength(4);
    await expect(within(table).getByText(ROTATION)).toBeVisible();
    for (const label of ['Authority principal', 'Created by', 'Generation', 'Created', 'Updated']) {
      await expect(within(table).getAllByText(label, { selector: 'dt' })).toHaveLength(4);
    }
  },
};

// S4: a halted rotation says why, in the CLI's words, on the notice and on its row.
export const HaltedWithReason: Story = {
  play: async ({ canvas, canvasElement }) => {
    const table = await canvas.findByRole('table', { name: /credential rotations/ });
    const row = within(table).getByText(HALTED_ROTATION).closest('tr')!;
    await expect(within(row).getByText('Halt reason', { selector: 'dt' })).toBeVisible();
    // The reason's code is its own word (it breaks only after an underscore), so the sentence spans elements.
    await expect(row).toHaveTextContent('retries_exhausted: the retry window ran out');
    await expect(canvasElement).toHaveTextContent('is halted (retries_exhausted): the retry window ran out');
  },
};

// S4: a rotation that owes a drop says so in words. When a restore refused one of them the
// badge and line say not to drop it, and no row anywhere reads "drop it" for that case.
export const OwedDropSummary: Story = {
  play: async ({ canvas }) => {
    const table = await canvas.findByRole('table', { name: /credential rotations/ });
    const row = within(table).getByText(ROTATION).closest('tr')!;
    await expect(within(row).getByText('Do not drop: restore')).toBeVisible();
    await expect(within(row).queryByText('Owes a drop')).toBeNull();
    await expect(within(row).getByText(/do NOT drop it at the provider until the keys have moved off it/)).toBeVisible();
    await expect(within(row).getByText('Last run', { selector: 'dt' })).toBeVisible();
    // The other line is the deleted rotation that still owes a drop: the default list keeps it, marked deleted.
    await expect(within(table).queryAllByText(/^ATTENTION: rotation /)).toHaveLength(2);
    const gone = within(table).getByText(GONE_ROTATION).closest('tr')!;
    await expect(within(gone).getByText('deleted')).toBeVisible();
    await expect(within(gone).getByText(/\(deleted\) has 1 run\(s\) that owe a provider role a drop/)).toBeVisible();
  },
};

export const RestoreHold: Story = {
  play: async ({ canvas }) => {
    const alert = await canvas.findByText(/^RESTORE HOLD: rotation /);
    await expect(alert).toBeVisible();
    await expect(alert).toHaveTextContent(`hikyo rotation release-hold ${HELD_ROTATION}`);
    await expect(alert.closest('[role="alert"]')).not.toBeNull();
    await expect(canvas.queryByRole('button', { name: /release/i })).toBeNull();
  },
};

export const RunTimelineWithOwedDrops: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Show runs of DATABASE_PASSWORD' }));
    const notes = await canvas.findAllByText(/^ATTENTION: run /);
    await expect(notes).toHaveLength(3);
    await expect(notes[0]).toHaveTextContent('do NOT drop it at the provider until the keys have moved off it');
    await expect(notes[1]).toHaveTextContent('stays valid at least until 2026-10-29T12:30:45Z, and possibly longer');
    await expect(notes[2]).toHaveTextContent('no recorded expiry');
    const timeline = canvas.getByRole('table', { name: /Runs of this rotation/ });
    await expect(within(timeline).getAllByText('Owes a drop')).toHaveLength(2);
    await expect(within(timeline).getAllByText('Do not drop: restore')).toHaveLength(1);
  },
};

export const SettledRuns: Story = {
  parameters: {
    app: {
      responses: [
        keys,
        list(rotationList().items.map((r) => ({ ...r, owed_drop_runs: 0, owed_drop_runs_versions_changed: 0 }))),
        runs(ROTATION, settledRuns),
      ],
    },
  },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Show runs of DATABASE_PASSWORD' }));
    await expect(await canvas.findByRole('table', { name: /Runs of this rotation/ })).toBeVisible();
    await expect(canvas.queryByText(/^ATTENTION/)).toBeNull();
  },
};

export const NoRunsYet: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Show runs of REPORTING_PASSWORD' }));
    await expect(await canvas.findByText('This rotation has not run yet.')).toBeVisible();
  },
};

export const Empty: Story = {
  parameters: { app: { responses: [keys, list([])] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/No rotations in this environment yet/)).toBeVisible();
  },
};

export const Loading: Story = {
  parameters: { app: { responses: [keys, { url: LIST, pending: true }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Loading rotations…')).toBeVisible();
  },
};

// A caller who may not read the environment gets the same not-found as an
// absent one, so the panel says what listing needs and nothing about existence.
export const NotReadable: Story = {
  parameters: { app: { responses: [keys, { url: LIST, status: 404, body: { error: 'not found' } }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('alert')).toHaveTextContent('Listing them needs read on this environment.');
    await expect(canvas.queryByRole('table')).toBeNull();
  },
};

export const NoEnvironments: Story = {
  args: { environments: [] },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('This project has no environments, so it has no rotations.')).toBeVisible();
  },
};

// At a phone width the table is one card per rotation. Every field of every row
// stays on the page (no column is hidden), and neither the page nor the table
// scrolls sideways, so the restore hold, the next run and the expiry are all
// readable without a gesture.
function expectNoSideScroll(canvasElement: HTMLElement) {
  const scrollers = canvasElement.querySelectorAll('.table-scroll');
  expect(scrollers.length).toBeGreaterThan(0);
  for (const scroller of scrollers) expect(scroller.scrollWidth).toBeLessThanOrEqual(scroller.clientWidth);
  expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
}

export const PhoneCompactTable: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    const table = await canvas.findByRole('table', { name: /credential rotations/ });
    for (const text of ['Rotation', 'Password key', 'Role name key', 'Provider', 'Schedule', 'Next run', 'Delivered credential expires', 'Authority principal', 'Created by', 'Generation', 'Updated', 'Held after a restore: no run starts', 'Restore hold']) {
      await expect(within(table).getAllByText(new RegExp(text)).filter((node) => node.checkVisibility())[0]).toBeDefined();
    }
    expectNoSideScroll(canvasElement);
    await userEvent.click(await canvas.findByRole('button', { name: 'Show runs of DATABASE_PASSWORD' }));
    const runs = await canvas.findByRole('table', { name: /Runs of this rotation/ });
    for (const text of ['Started', 'Last change', 'Published revision', 'Overlap', 'Adapter targets not converged', 'Owed role valid until', 'At least']) {
      await expect(within(runs).getAllByText(new RegExp(text)).filter((node) => node.checkVisibility())[0]).toBeDefined();
    }
    expectNoSideScroll(canvasElement);
  },
};

// At desktop width the same tables hold every field without a sideways scroll too.
export const DesktopNoSideScroll: Story = {
  play: async ({ canvas, canvasElement }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Show runs of DATABASE_PASSWORD' }));
    await canvas.findByRole('table', { name: /Runs of this rotation/ });
    expectNoSideScroll(canvasElement);
    expectKeyNamesFit(canvasElement);
    await expect(canvas.getAllByText(/Owed role valid until/)[0]).toBeVisible();
  },
};

/**
 * A key name breaks only after an underscore, and its right edge stays inside its own
 * cell: a name that runs over the next cell is caught here, not by one line of text.
 */
function expectKeyNamesFit(root: HTMLElement) {
  const names = root.querySelectorAll('.rotations__table .kv bdi');
  expect(names.length).toBeGreaterThan(0);
  for (const name of names) {
    for (const node of name.childNodes) {
      if (node.nodeType !== Node.TEXT_NODE) continue;
      const text = node.textContent ?? '';
      const range = document.createRange();
      for (let i = 1; i < text.length; i += 1) {
        range.setStart(node, i - 1);
        range.setEnd(node, i);
        const before = range.getBoundingClientRect().top;
        range.setStart(node, i);
        range.setEnd(node, i + 1);
        // A line break inside a word, at anything but an underscore.
        if (range.getBoundingClientRect().top > before + 1) expect(text[i - 1]).toBe('_');
      }
    }
    const cell = name.closest('th, td');
    expect(cell).not.toBeNull();
    expect(name.getBoundingClientRect().right).toBeLessThanOrEqual((cell?.getBoundingClientRect().right ?? 0) + 0.5);
  }
}

// A key name about 45 characters long wraps after an underscore and stays in its cell, at a
// phone's width and at a laptop's.
export const LongKeyNamePhone: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  parameters: { app: { responses: [keys, list([longKeyNameRotation()]), runs(ROTATION, owedRuns)] } },
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByRole('table', { name: /credential rotations/ });
    await expect(canvas.getAllByText(LONG_PASSWORD_NAME, { selector: 'bdi' })[0]).toBeVisible();
    expectNoSideScroll(canvasElement);
    expectKeyNamesFit(canvasElement);
  },
};

export const LongKeyNameDesktop: Story = {
  parameters: { app: { responses: [keys, list([longKeyNameRotation()]), runs(ROTATION, owedRuns)] } },
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByRole('table', { name: /credential rotations/ });
    await expect(canvas.getAllByText(LONG_PASSWORD_NAME, { selector: 'bdi' })[0]).toBeVisible();
    expectNoSideScroll(canvasElement);
    expectKeyNamesFit(canvasElement);
  },
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { dynamicProvider } from '../testkit/machineAccess.ts';
import {
  haltedRotation,
  HELD_ROTATION,
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
    providers: [dynamicProvider],
  },
  parameters: {
    ...topLayerDocs,
    app: { responses: [keys, list([...rotationList().items, haltedRotation()]), runs(ROTATION, owedRuns), runs(HELD_ROTATION, { items: [] })] },
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
    await expect(within(table).getAllByRole('row')).toHaveLength(4);
    // Status is a word, never only a colour, and the table has no act to take.
    await expect(within(table).getByText('halted')).toBeVisible();
    await expect(within(table).getAllByRole('button').map((button) => button.textContent)).toEqual(['Show runs', 'Show runs', 'Show runs']);
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
    await userEvent.click((await canvas.findAllByRole('button', { name: 'Runs of rotation DATABASE_PASSWORD' }))[0]!);
    const notes = await canvas.findAllByText(/^ATTENTION: run /);
    await expect(notes).toHaveLength(3);
    await expect(notes[0]).toHaveTextContent('do NOT drop it at the provider until the keys have moved off it');
    await expect(notes[1]).toHaveTextContent('stays valid at least until 2026-10-29T12:30:45Z, and possibly longer');
    await expect(notes[2]).toHaveTextContent('no recorded expiry');
    const timeline = canvas.getByRole('table', { name: /Runs of this rotation/ });
    await expect(within(timeline).getAllByText('Owes a drop')).toHaveLength(3);
  },
};

export const SettledRuns: Story = {
  parameters: { app: { responses: [keys, list(rotationList().items), runs(ROTATION, settledRuns)] } },
  play: async ({ canvas }) => {
    await userEvent.click((await canvas.findAllByRole('button', { name: 'Runs of rotation DATABASE_PASSWORD' }))[0]!);
    await expect(await canvas.findByRole('table', { name: /Runs of this rotation/ })).toBeVisible();
    await expect(canvas.queryByText(/^ATTENTION/)).toBeNull();
  },
};

export const NoRunsYet: Story = {
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Runs of rotation REPORTING_PASSWORD' }));
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

// At a phone width the table drops to the compact type size and scrolls inside
// its own container while the page itself never scrolls sideways.
export const PhoneCompactTable: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const table = await canvas.findByRole('table', { name: /credential rotations/ });
    const root = getComputedStyle(document.documentElement);
    await expect(getComputedStyle(table).fontSize).toBe(root.getPropertyValue('--fs-xs').trim());
    await expect(getComputedStyle(table.parentElement!).overflowX).toBe('auto');
    await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
  },
};

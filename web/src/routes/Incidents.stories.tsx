import type { Meta, StoryObj } from '@storybook/react-vite';
import { zIncident, zIncidentItemList } from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { expectLeftAlignedPage } from '../../.storybook/layout.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { Incidents } from './Incidents.tsx';

const incidentId = 'inc_00000000-0000-0000-0000-000000000001';

const incident = {
  id: incidentId,
  scope_kind: 'account',
  scope_id: 'usr_00000000-0000-0000-0000-00000000000b',
  org: 'org_00000000-0000-0000-0000-000000000001',
  status: 'open',
  opened_by: 'usr_00000000-0000-0000-0000-00000000000a',
  opened_at: '2026-11-23T09:00:00Z',
  counts: { pending: 3, done: 2, unknown: 1, attested: 0, blocked: 0, out_of_scope: 0 },
} satisfies z.input<typeof zIncident>;

// One item per state the page must tell apart: a refused containment (pending
// with owner_failed), one that has not run yet, a rotation whose live state is
// halted and one the server could no longer find, and a confirmed revoke.
const items = {
  items: [
    {
      id: 'itm_00000000-0000-0000-0000-000000000001', owner_function: 'account.contain', object_kind: 'account',
      object_id: 'usr_00000000-0000-0000-0000-00000000000b', outcome: 'pending', reason: 'owner_failed', refused: true, attestable: false,
      updated_at: '2026-11-23T09:01:00Z',
    },
    {
      id: 'itm_00000000-0000-0000-0000-000000000002', owner_function: 'session.revoke', object_kind: 'session',
      object_id: 'ses_00000000-0000-0000-0000-000000000001', outcome: 'pending', refused: false, attestable: false,
      updated_at: '2026-11-23T09:00:00Z',
    },
    {
      id: 'itm_00000000-0000-0000-0000-000000000003', owner_function: 'rotation.halt', object_kind: 'rotation',
      object_id: 'rot_00000000-0000-0000-0000-000000000001', outcome: 'done', refused: false, attestable: false, object_state: 'halted',
      updated_at: '2026-11-23T09:02:00Z',
    },
    {
      id: 'itm_00000000-0000-0000-0000-000000000004', owner_function: 'rotation.halt', object_kind: 'rotation',
      object_id: 'rot_00000000-0000-0000-0000-000000000002', outcome: 'unknown', reason: 'owner_unconfirmed', refused: false, attestable: false,
      object_state: 'gone', updated_at: '2026-11-23T09:02:00Z',
    },
    {
      id: 'itm_00000000-0000-0000-0000-000000000005', owner_function: 'developer_credential.revoke', object_kind: 'developer_credential',
      object_id: 'devc_00000000-0000-0000-0000-000000000001', outcome: 'done', refused: false, attestable: false,
      updated_at: '2026-11-23T09:02:00Z',
    },
    {
      id: 'itm_00000000-0000-0000-0000-000000000006', owner_function: 'dynamic_provider.upstream_revoke', object_kind: 'dynamic_lease',
      object_id: 'dyl_00000000-0000-0000-0000-000000000001', outcome: 'pending', reason: 'upstream_not_observable', refused: false, attestable: true,
      updated_at: '2026-11-23T09:02:00Z',
    },
  ],
} satisfies z.input<typeof zIncidentItemList>;

const incidentRoute = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/api\/v1\/incidents\/[^/]+(\?|$)/, ...rest });
const itemsRoute = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/incidents\/[^/]+\/items(\?|$)/, ...rest });

const path = '/orgs/acme/incidents';
const routePath = '/orgs/:org/incidents';

const meta = {
  title: 'Pages/Incidents',
  id: 'routes-incidents',
  component: Incidents,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { auth: true, path, routePath, responses: [] } },
} satisfies Meta<typeof Incidents>;

export default meta;
type Story = StoryObj<typeof meta>;

// No incident chosen yet: the open form is the whole page.
export const OpenForm: Story = {
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Open incident' })).toBeVisible());
    await expectLeftAlignedPage(canvas.getByRole('heading', { level: 1 }));
    await userEvent.selectOptions(canvas.getByRole('combobox', { name: 'Scope' }), 'project');
    await expect(canvas.getByRole('textbox', { name: 'Project' })).toBeVisible();
  },
};

// An incident with a refused item, a not-yet-run item and two rotation items
// showing the rotation's own live state.
export const Ledger: Story = {
  parameters: {
    app: {
      auth: true,
      path: `${path}?incident=${incidentId}`,
      routePath,
      responses: [itemsRoute({ body: items }), incidentRoute({ body: incident })],
    },
  },
  play: async ({ canvas }) => {
    const table = await canvas.findByRole('table');
    // A refused item and a not-yet-run item read differently.
    await expect(within(table).getByText('Refused by its owner')).toBeVisible();
    await expect(within(table).getByText('The owner refused this item.')).toBeVisible();
    await expect(within(table).getAllByText('Not yet run')).toHaveLength(2);
    // The rotation's own state, as the server read it.
    await expect(within(table).getByText('halted')).toBeVisible();
    await expect(within(table).getByText('gone')).toBeVisible();
    // Attest is offered only where the server says it would be accepted.
    await expect(within(table).getAllByRole('button', { name: /^Attest/ })).toHaveLength(1);
    await expect(canvas.getByRole('button', { name: 'Resume open items' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Close with residuals' })).toBeVisible();
  },
};

// Attesting names who states it and is never offered as a confirmation.
export const AttestDialog: Story = {
  parameters: Ledger.parameters,
  play: async ({ canvas }) => {
    await canvas.findByRole('table');
    await userEvent.click(canvas.getAllByRole('button', { name: /^Attest/ })[0] ?? canvas.getByRole('table'));
    await waitFor(() => expect(canvas.getByText(/An attestation is your statement/)).toBeVisible());
  },
};

// The ledger read is refused: one sentence, never the raw status.
export const NotFound: Story = {
  parameters: {
    app: {
      auth: true,
      path: `${path}?incident=${incidentId}`,
      routePath,
      responses: [itemsRoute({ status: 404, body: { error: 'not found' } }), incidentRoute({ status: 404, body: { error: 'not found' } })],
    },
  },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/No such incident, or you may not work it/)).toBeVisible());
  },
};

// The ledger read never settles: the loading status stands.
export const Loading: Story = {
  parameters: {
    app: {
      auth: true,
      path: `${path}?incident=${incidentId}`,
      routePath,
      responses: [itemsRoute({ pending: true }), incidentRoute({ pending: true })],
    },
  },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading the incident/i)).toBeVisible());
  },
};

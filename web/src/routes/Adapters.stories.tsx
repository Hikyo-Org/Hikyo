import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAdapter, zAdapterList, zAdapterTarget, zAdapterTargetDetail, zEnvironmentList, zKeyList } from '@hikyo/zod';
import { expect, userEvent, within } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { ORG, PRJ, STAGING } from '../testkit/ids.ts';
import { Adapters } from './Adapters.tsx';

const base = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const url = `${base}/adapters`;
const responses = [
  { url: `${base}/file-targets`, body: { items: [] } },
  { url: `${base}/service-accounts`, body: { items: [], count: 0 } },
  { url: `${base}/environments`, body: { items: [], count: 0 } satisfies z.input<typeof zEnvironmentList> },
  { url: `${base}/keys`, body: { items: [], count: 0, schema_revision: 1 } satisfies z.input<typeof zKeyList> },
];
const meta = {
  title: 'Pages/Adapters',
  component: Adapters,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {
    auth: true, identity: { ...authenticatedIdentity, capabilities: { ...authenticatedIdentity.capabilities, instance_operator: false } },
    path: `/orgs/${ORG}/projects/${PRJ}/adapters`, routePath: '/orgs/:org/projects/:project/adapters',
    responses: [{ url, body: { items: [] } satisfies z.input<typeof zAdapterList> }, ...responses],
  }, docs: { ...topLayerDocs.docs, description: { component: 'Real deployment adapters page through its production system-scope gate. Browser passkey ceremonies remain human-only; isolated adapter state catalogues cover existing targets.' } } },
} satisfies Meta<typeof Adapters>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Empty: Story = { play: async ({ canvas }) => { await expect(await canvas.findByText(/No adapters yet/)).toBeVisible(); } };
export const Failed: Story = { parameters: { app: { responses: [{ url, status: 500, body: { error: { code: 'internal', message: 'Internal' } } }, ...responses] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent(/500|could not|failed/i); } };
// "Add adapter" opens the form in the side panel.
export const Adding: Story = { play: async ({ canvas }) => {
  await canvas.findByText(/No adapters yet/);
  await userEvent.click(canvas.getByRole('button', { name: 'Add adapter' }));
  const panel = canvas.getByRole('complementary', { name: 'New adapter' });
  await expect(within(panel).getByRole('textbox', { name: 'Origin' })).toBeVisible();
} };

const TARGET = 'adt_123e4567-e89b-12d3-a456-426614174020';
const ADAPTER = 'adp_123e4567-e89b-12d3-a456-426614174021';
const KEY = 'key_123e4567-e89b-12d3-a456-426614174022';
const target = {
  id: TARGET, adapter_id: ADAPTER, environment_id: STAGING, destination_kind: 'environment',
  destination_owner: 'acme', destination_name: 'payments', destination_environment: 'staging', destination_id: 42, repository_id: 42,
  visibility: '', selected_repository_ids: [], name_prefix: 'PAYMENTS_', generation: 1, state: 'active', sync_status: 'converged',
  converged_revision: 7, last_attempted_revision: 7, last_attempted_at: '2026-10-12T08:00:00Z', last_error_class: '', retry_at: null,
  paused_at: null, drift_attention: false, failure_names: [], warnings: [], conflicts: [],
  keys: [{ key_id: KEY, name: 'STRIPE_SECRET', classification: 'secret' }],
} satisfies z.input<typeof zAdapterTarget>;
const adapter = {
  id: ADAPTER, provider: 'github-actions', origin: 'https://api.github.com', credential_present: true, credential_set_at: '2026-10-01T08:00:00Z',
  credential_expires_at: null, authority_principal_id: 'prn_123e4567-e89b-12d3-a456-426614174023', state: 'active', created_at: '2026-10-01T08:00:00Z', targets: [target],
} satisfies z.input<typeof zAdapter>;
const withTarget = [
  { url, body: { items: [adapter] } satisfies z.input<typeof zAdapterList> },
  {
    url: `${base}/adapter-targets/${TARGET}`,
    body: {
      target, conflicts: [],
      mapping: [{ key_id: KEY, canonical_name: 'STRIPE_SECRET', surface: 'secret', effective_name: 'PAYMENTS_STRIPE_SECRET' }],
    } satisfies z.input<typeof zAdapterTargetDetail>,
  },
  {
    url: `${base}/environments`,
    body: {
      items: [{ id: STAGING, org_id: ORG, project_id: PRJ, name: 'staging', display_order: 0, created_at: '2026-10-01T08:00:00Z' }],
      count: 1,
    } satisfies z.input<typeof zEnvironmentList>,
  },
  ...responses.filter((response) => response.url !== `${base}/environments`),
];

// A target opens its detail in the side panel, not beside the adapter card.
export const TargetDetailPanel: Story = {
  parameters: { app: { responses: withTarget } },
  play: async ({ canvas }) => {
    const row = await canvas.findByRole('button', { name: /staging/ });
    await userEvent.click(row);
    const panel = await canvas.findByRole('complementary', { name: 'Target detail' });
    await expect(await within(panel).findByRole('list', { name: 'Member keys' })).toHaveTextContent('STRIPE_SECRET');
    await expect(within(panel).getByText(/secrets\.PAYMENTS_STRIPE_SECRET/)).toBeVisible();
  },
};

// One side panel at a time: a link carrying both `?target=` and `?move=`
// opens only the followed move, never the target detail beside it.
export const MoveWinsOverTarget: Story = {
  parameters: { app: {
    path: `/orgs/${ORG}/projects/${PRJ}/adapters?target=${TARGET}&move=mov_123e4567-e89b-12d3-a456-426614174024`,
    responses: [{ url: /\/adapter-moves\//, pending: true }, ...withTarget],
  } },
  play: async ({ canvas }) => {
    const panel = await canvas.findByRole('complementary', { name: 'Route move' });
    await expect(within(panel).getByText('Loading…')).toBeVisible();
    await expect(canvas.queryByRole('complementary', { name: 'Target detail' })).toBeNull();
    await expect(canvas.getAllByRole('complementary')).toHaveLength(1);
  },
};

// "Replace credential" opens its form in the side panel, naming the adapter.
export const ReplaceCredentialPanel: Story = {
  parameters: { app: { responses: withTarget } },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Replace credential' }));
    const panel = canvas.getByRole('complementary', { name: 'Replace credential' });
    await expect(within(panel).getByText('https://api.github.com')).toBeVisible();
    await expect(within(panel).getByRole('form', { name: 'Replace credential' })).toBeVisible();
  },
};

import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zProjectRetentionPolicy,
  zRevisionDetail,
  zRevisionList,
  zRevisionPinList,
  zServiceAccountList,
} from '@hikyo/zod';
import { createRef } from 'react';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { HistoryDrawer } from './HistoryDrawer.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The revision-history drawer, mounted as the matrix mounts it: over the grid,
// addressed by the `history` route with `?env=`, `?rev=` and `?key=` in the
// URL. Its props are the matrix's already-loaded catalogue; on mount it reads
// the environment's revisions and pins, the project's retention policy and
// service accounts (to name pinned workloads), and the selected revision's
// delivered key set. The restore / pin / release sheets are `<dialog>`s that
// open from the detail pane; two stories open one without submitting it.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const ROUTE = '/orgs/:org/projects/:project/matrix/history';
const BASE = `/orgs/${ORG}/projects/${PRJ}/matrix/history`;

const DEV = 'env_123e4567-e89b-12d3-a456-426614174010';
const STG = 'env_123e4567-e89b-12d3-a456-426614174011';
const PRD = 'env_123e4567-e89b-12d3-a456-426614174012';

type Props = Parameters<typeof HistoryDrawer>[0];
type Key = Props['keys'][number];
type Account = z.input<typeof zServiceAccountList>['items'][number];
type Revision = z.input<typeof zRevisionList>['items'][number];
type Pin = z.input<typeof zRevisionPinList>['items'][number];

const environments: Props['environments'] = [
  { id: DEV, name: 'development', display_order: 0 },
  { id: STG, name: 'staging', display_order: 1 },
  { id: PRD, name: 'production', display_order: 2 },
].map((item) => ({ ...item, org_id: ORG, project_id: PRJ, created_at: '2026-01-01T00:00:00Z' }));

const keyId = (n: number) => `key_123e4567-e89b-12d3-a456-4266141740${String(n).padStart(2, '0')}`;
const key = (n: number, name: string, classification: 'secret' | 'config'): Key => ({
  id: keyId(n),
  org_id: ORG,
  project_id: PRJ,
  name,
  folder_path: '',
  classification,
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: false } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  created_at: '2026-01-01T00:00:00Z',
});
const keys: Props['keys'] = [
  key(1, 'LOG_LEVEL', 'config'),
  key(2, 'PUBLIC_BASE_URL', 'config'),
  key(3, 'DATABASE_URL', 'secret'),
  key(4, 'STRIPE_SECRET_KEY', 'secret'),
  key(5, 'FEATURE_FLAGS', 'config'),
  key(6, 'OTEL_EXPORTER_OTLP_ENDPOINT', 'config'),
];

const ADMIN = 'prn_123e4567-e89b-12d3-a456-426614174010';
const DEPLOYER = 'prn_123e4567-e89b-12d3-a456-426614174011';
const WORKER = 'prn_123e4567-e89b-12d3-a456-426614174020';
const CRON = 'prn_123e4567-e89b-12d3-a456-426614174021';
const LEGACY = 'prn_123e4567-e89b-12d3-a456-426614174022';
const UNKNOWN_WORKLOAD = 'prn_123e4567-e89b-12d3-a456-426614174029';

const hoursAgo = (hours: number) => new Date(Date.now() - hours * 3_600_000).toISOString();
const inDays = (days: number) => new Date(Date.now() + days * 86_400_000).toISOString();

const changed = (n: number, change: 'added' | 'edited' | 'removed') => {
  const record = keys[n - 1];
  if (record === undefined) throw new Error(`no key ${String(n)}`);
  return { key_id: record.id, name: record.name, change };
};

// Twelve publishes, newest first: the current one, one collected by retention
// (lineage only), one renamed key that no longer exists, and every change kind.
const revisions: readonly Revision[] = [
  { revision: 12, schema_revision: 9, published_by: ADMIN, published_by_name: 'Dana Jacobs', published_at: hoursAgo(0.5), changed_keys: [changed(1, 'edited'), changed(5, 'edited')], payload_present: true },
  { revision: 11, schema_revision: 9, published_by: DEPLOYER, published_by_name: 'deploy-bot', published_at: hoursAgo(3), changed_keys: [changed(3, 'edited'), changed(4, 'added'), changed(6, 'removed')], payload_present: true },
  { revision: 10, schema_revision: 8, published_by: ADMIN, published_by_name: 'Dana Jacobs', published_at: hoursAgo(26), changed_keys: [changed(2, 'edited')], payload_present: true },
  { revision: 9, schema_revision: 8, published_by: DEPLOYER, published_by_name: 'deploy-bot', published_at: hoursAgo(50), changed_keys: [changed(3, 'edited'), changed(5, 'added')], payload_present: true },
  { revision: 8, schema_revision: 8, published_by: ADMIN, published_at: hoursAgo(72), changed_keys: [changed(6, 'added')], payload_present: true },
  { revision: 7, schema_revision: 7, published_by: ADMIN, published_by_name: 'Dana Jacobs', published_at: hoursAgo(96), changed_keys: [{ key_id: 'key_123e4567-e89b-12d3-a456-426614174099', name: 'LEGACY_API_TOKEN', change: 'removed' }], payload_present: true },
  { revision: 6, schema_revision: 7, published_by: DEPLOYER, published_by_name: 'deploy-bot', published_at: hoursAgo(120), changed_keys: [changed(1, 'edited')], payload_present: true },
  { revision: 5, schema_revision: 6, published_by: ADMIN, published_by_name: 'Dana Jacobs', published_at: hoursAgo(400), changed_keys: [changed(3, 'edited'), changed(2, 'edited')], payload_present: false, collected_policy: 'keep-if-either' },
  { revision: 4, schema_revision: 6, published_by: ADMIN, published_by_name: 'Dana Jacobs', published_at: hoursAgo(420), changed_keys: [changed(4, 'edited')], payload_present: false, collected_policy: 'keep-if-either' },
  { revision: 3, schema_revision: 5, published_by: DEPLOYER, published_by_name: 'deploy-bot', published_at: hoursAgo(600), changed_keys: [changed(2, 'added'), changed(1, 'added')], payload_present: false, collected_policy: 'keep-if-either' },
  { revision: 2, schema_revision: 5, published_by: ADMIN, published_by_name: 'Dana Jacobs', published_at: hoursAgo(700), changed_keys: [changed(3, 'added')], payload_present: false, collected_policy: 'keep-if-either' },
  { revision: 1, schema_revision: 1, published_by: ADMIN, published_by_name: 'Dana Jacobs', published_at: hoursAgo(800), changed_keys: [changed(5, 'added'), changed(6, 'added')], payload_present: false, collected_policy: 'keep-if-either' },
];

const pin = (n: number, workload: string, revision: number, expiresAt: string, extra: Partial<Pin> = {}): Pin => ({
  id: `pin_123e4567-e89b-12d3-a456-4266141741${String(n).padStart(2, '0')}`,
  workload_principal_id: workload,
  revision,
  authority_principal_id: ADMIN,
  expires_at: expiresAt,
  created_at: hoursAgo(48),
  authorized_at: hoursAgo(48),
  history_authorized: true,
  schema_override: false,
  expired: false,
  release_retention_consequence: 'retained',
  ...extra,
});

// One pin per expiry tier, one sole keeper with a schema override, one whose
// workload no longer has a service account (the id shows), one expired.
const pins = {
  count: 5,
  items: [
    pin(1, WORKER, 11, inDays(45)),
    pin(2, CRON, 9, inDays(5), { schema_override: true, release_retention_consequence: 'collection_eligible' }),
    pin(3, UNKNOWN_WORKLOAD, 12, inDays(0.5)),
    pin(4, LEGACY, 6, inDays(-3), { expired: true, release_retention_consequence: 'already_collected' }),
    pin(5, DEPLOYER, 10, inDays(20)),
  ],
} satisfies z.input<typeof zRevisionPinList>;

const account = (n: number, principal: string, name: string, kind: Account['kind'], liveCredentials: number): Account => ({
  id: `sva_123e4567-e89b-12d3-a456-4266141740${String(n)}`,
  principal_id: principal,
  name,
  kind,
  created_at: '2026-01-01T00:00:00Z',
  created_by: ADMIN,
  live_credentials: liveCredentials,
});
const accounts = {
  count: 4,
  items: [
    account(30, WORKER, 'worker-api', 'workload', 2),
    account(31, CRON, 'cron-nightly', 'workload', 1),
    account(32, LEGACY, 'legacy-batch', 'workload', 0),
    account(33, DEPLOYER, 'deploy-bot', 'automation', 1),
  ],
} satisfies z.input<typeof zServiceAccountList>;

const retention = {
  inherited: true,
  mode: 'keep-if-either',
  max_age_seconds: 7_776_000,
  last_revisions: 10,
} satisfies z.input<typeof zProjectRetentionPolicy>;

// The delivered key set of any readable revision: the drawer only reads `keys`.
const detail = {
  revision: 11,
  schema_revision: 9,
  published_by: DEPLOYER,
  published_at: hoursAgo(3),
  changed_keys: [changed(3, 'edited')],
  change_token: 'tok_0123456789abcdef',
  keys: keys.map((record) => ({ key_id: record.id, name: record.name, classification: record.classification })),
} satisfies z.input<typeof zRevisionDetail>;

const REVISIONS_URL = `${PROJECT_URL}/environments/${PRD}/revisions`;

const rows = (history: Omit<MockRoute, 'url'> = { body: { items: revisions, count: revisions.length } }) =>
  [
    { url: REVISIONS_URL, ...history },
    { url: /\/revisions\/\d+$/, body: detail },
    { url: `${PROJECT_URL}/environments/${PRD}/pins`, body: pins },
    { url: `${PROJECT_URL}/retention`, body: retention },
    { url: `${PROJECT_URL}/service-accounts`, body: accounts },
  ] satisfies readonly MockRoute[];

const app = (search: string, responses: readonly MockRoute[] = rows()) => ({
  app: { path: `${BASE}?${search}`, routePath: ROUTE, responses },
});

const meta = {
  component: HistoryDrawer,
  tags: ['ai-generated'],
  args: {
    refData: { org: ORG, project: PRJ },
    environments,
    keys,
    currentRevisions: new Map([[DEV, 31n], [STG, 18n], [PRD, 12n]]),
    protectedEnvironmentIds: [PRD],
    cellsByEnvironment: new Map([[PRD, keys.map((record) => ({ keyId: record.id, classification: record.classification, set: true }))]]),
    pendingByEnvironment: new Map([[PRD, 2]]),
    pendingByOthersByEnvironment: new Map([[PRD, 1]]),
    currentValuesByEnvironment: new Map(),
    openerRef: createRef<HTMLAnchorElement>(),
  },
  parameters: { ...topLayerDocs, ...app(`env=${PRD}&rev=11`) },
} satisfies Meta<typeof HistoryDrawer>;

export default meta;
type Story = StoryObj<typeof meta>;

// Desktop: the list beside the detail of a historical revision, with the
// current tag, pinned tags, collected rows, every pin tier and both actions live.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: /^r11/ })).toBeVisible();
    // Both actions wait on the revision's delivered key set.
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore r11…' })).toBeEnabled());
    await expect(canvas.getByRole('button', { name: 'Pin r11…' })).toBeEnabled();
    await expect(canvas.getByRole('button', { name: /^r5 payload collected/ })).toBeVisible();
    await expect(canvas.getByText('sole keeper')).toBeVisible();
    await expect(canvas.getByText('2 staged by you (unpublished) · 1 change pending by others')).toBeVisible();
  },
};

// A per-key filter: the same lineage projected onto one key, with the status
// line naming it and the way back to every revision.
export const KeyFilter: Story = {
  parameters: app(`env=${PRD}&key=${keyId(3)}`),
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByText(/filter active: history of DATABASE_URL \(current name\), showing 4 of 12 revisions/),
    ).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'show every revision' })).toBeVisible();
  },
};

// The pin sheet, opened from the detail pane and left unsubmitted.
export const PinSheetOpen: Story = {
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Pin r11…' })).toBeEnabled());
    await userEvent.click(canvas.getByRole('button', { name: 'Pin r11…' }));
    await expect(await canvas.findByRole('heading', { name: 'Pin r11 · production' })).toBeVisible();
  },
};

// The restore sheet before staging: what a restore does, not yet what it did.
export const RestoreSheetOpen: Story = {
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore r11…' })).toBeEnabled());
    await userEvent.click(canvas.getByRole('button', { name: 'Restore r11…' }));
    await expect(await canvas.findByRole('heading', { name: 'Restore r11 · production' })).toBeVisible();
  },
};

// No publish yet in this environment.
export const Empty: Story = {
  args: { currentRevisions: new Map([[DEV, 31n], [STG, 18n], [PRD, 0n]]), pendingByEnvironment: new Map(), pendingByOthersByEnvironment: new Map() },
  parameters: app(`env=${PRD}`, rows({ body: { items: [], count: 0 } })),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('No revisions published in this environment yet.')).toBeVisible();
  },
};

// The lineage never settles.
export const Loading: Story = {
  parameters: app(`env=${PRD}`, rows({ pending: true })),
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText('Loading revision history…')).toBeVisible());
  },
};

// The lineage read failed: the drawer's head stands, the body says so.
export const Failed: Story = {
  parameters: app(`env=${PRD}`, rows({ status: 500, body: { error: 'history read failed' } })),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('The revision history could not be read. Reload to try again.')).toBeVisible();
  },
};

// Below 800px the drawer is one pane: the list first, the detail a drill-in.
export const Phone: Story = {
  globals: { viewport: { value: 'mobile1', isRotated: false } },
  parameters: app(`env=${PRD}`),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: /^r12 current/ })).toBeVisible();
    await expect(canvas.getByRole('button', { name: '← All revisions', hidden: true })).not.toBeVisible();
  },
};

// A phone deep link with `rev`: the detail pane with its back action, no list.
export const PhoneDetail: Story = {
  globals: { viewport: { value: 'mobile1', isRotated: false } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: '← All revisions' })).toBeVisible();
    await expect(canvas.getByRole('heading', { name: /^r11/ })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^r12 current/, hidden: true })).not.toBeVisible();
  },
};

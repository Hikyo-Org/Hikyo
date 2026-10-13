import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zProjectRetentionPolicy,
  zRevisionDetail,
  zRevisionList,
  zRevisionPinList,
  zRollbackResult,
  zServiceAccountList,
} from '@hikyo/zod';
import { createRef } from 'react';
import { useLocation } from 'react-router';
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ } from '../testkit/ids.ts';
import { HistoryDrawer } from './HistoryDrawer.tsx';

import { tokenColor } from '../../.storybook/tokens.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The revision-history drawer, mounted as the matrix mounts it: over the grid,
// addressed by the `history` route with `?env=`, `?rev=` and `?key=` in the
// URL. Its props are the matrix's already-loaded catalogue; on mount it reads
// the environment's revisions and pins, the project's retention policy and
// service accounts (to name pinned workloads), and the selected revision's
// delivered key set. The restore / pin / release sheets are `<dialog>`s that
// open from the detail pane; two stories open one without submitting it.
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
  keys: keys.map((record) => ({
    key_id: record.id,
    name: record.name,
    classification: record.classification,
    sensitive: record.classification === 'secret',
  })),
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
  title: 'Features/Values/HistoryDrawer',
  id: 'routes-historydrawer',
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

// The detail of a historical revision at the full panel width, headed by
// the revision selector, with the current and pinned marks, collected
// revisions, every pin tier and the actions live.
export const Populated: Story = {
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByRole('combobox', { name: 'Revision' })).toHaveDisplayValue(/^r11/);
    // Both actions wait on the revision's delivered key set.
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore to r11' })).toBeEnabled());
    await expect(canvas.getByRole('button', { name: 'Pin workload to r11' })).toBeEnabled();
    const selector = canvas.getByRole('combobox', { name: 'Revision' });
    await expect(selector).toHaveValue('11');
    await expect(canvas.getByRole('option', { name: /^r12 · current · / })).toBeInTheDocument();
    await expect(canvas.getByRole('option', { name: /^r5 · payload collected · / })).toBeInTheDocument();
    // The selector heads the detail section; its heading keeps the outline for
    // screen readers, and the select is a keyboard stop of its own.
    const section = canvas.getByRole('region', { name: 'Selected revision' });
    await expect(section).toContainElement(selector);
    selector.focus();
    await expect(selector).toHaveFocus();
    // Option labels use the plain middle dot, never the old bullet.
    for (const option of canvas.getAllByRole('option')) await expect(option.textContent).not.toContain('•');
    await expect(canvas.getByText('sole keeper')).toBeVisible();
    await expect(canvasElement.querySelector('.history__pending')).toHaveTextContent(
      /^2 staged by you \(unpublished\)\s*·\s*1 change pending by others$/,
    );
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
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Pin workload to r11' })).toBeEnabled());
    await userEvent.click(canvas.getByRole('button', { name: 'Pin workload to r11' }));
    await expect(await canvas.findByRole('heading', { name: 'Pin r11 · production' })).toBeVisible();
  },
};

// The restore sheet before staging: what a restore does, not yet what it did.
export const RestoreSheetOpen: Story = {
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore to r11' })).toBeEnabled());
    await userEvent.click(canvas.getByRole('button', { name: 'Restore to r11' }));
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

/** Every changed-key row is laid out the same way: one height, the restore action at one edge. */
const expectEvenChangedKeys = async () => {
  const rows = [...document.querySelectorAll<HTMLElement>('.history__changes li')];
  await expect(rows.length).toBe(revisions[1]?.changed_keys.length);
  const heights = new Set(rows.map((row) => Math.round(row.getBoundingClientRect().height)));
  const restoreEdges = new Set(rows.map((row) =>
    Math.round(row.querySelector('.history__change-restore')?.getBoundingClientRect().right ?? -1),
  ));
  await expect(heights.size).toBe(1);
  await expect(restoreEdges.size).toBe(1);
};

// The changed-key list of r11: two secrets with the write-presence badge and
// a long config name, each row the same shape with
// the action at its end. The action keeps the key in its accessible name.
export const ChangedKeys: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('combobox', { name: 'Revision' })).toHaveDisplayValue(/^r11/);
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore OTEL_EXPORTER_OTLP_ENDPOINT to r11' })).toBeEnabled());
    await expectEvenChangedKeys();
    // The change kind is a changed-tone tag with its own icon, the
    // write-presence tag carries the lock, and both read in capitals while the
    // DOM keeps the word; "current r12" keeps the revision's case.
    const kinds = [...document.querySelectorAll('.history__changes .history__kind')];
    await expect(kinds.length).toBeGreaterThan(0);
    for (const kind of kinds) {
      await expect(kind).toHaveClass('badge', 'badge--changed');
      const icon = kind.querySelector('svg')?.getAttribute('data-glyph');
      await expect({ added: 'plus', edited: 'edit', removed: 'minus' }[kind.textContent ?? '']).toBe(icon);
      await expect(getComputedStyle(kind).textTransform).toBe('uppercase');
    }
    const presence = [...document.querySelectorAll('.history__changes .history__presence')];
    await expect(presence.length).toBeGreaterThan(0);
    for (const tag of presence) await expect(tag.querySelector('svg')?.getAttribute('data-glyph')).toBe('lock');
    // Each row's action reads just "Restore"; the key and target ride in its name.
    for (const restore of document.querySelectorAll('.history__changes .history__change-restore')) {
      await expect(restore.textContent?.trim()).toBe('Restore');
      await expect(restore).toHaveAccessibleName(/^Restore \S+ to r11$/);
    }
    const current = canvas.getByText(/^r\d+$/, { selector: '.badge .mono' });
    await expect(getComputedStyle(current).textTransform).toBe('none');
  },
};

// The same list on a phone: two lines per row, the key with the action at
// the end, then its badges, identical on every row.
export const ChangedKeysPhone: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('combobox', { name: 'Revision' })).toHaveDisplayValue(/^r11/);
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore DATABASE_URL to r11' })).toBeEnabled());
    await expectEvenChangedKeys();
  },
};

// Every revision is an option of the heading's selector; choosing one moves
// `rev` and the detail follows.
export const SelectRevision: Story = {
  play: async ({ canvas }) => {
    const selector = await canvas.findByRole('combobox', { name: 'Revision' });
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore to r11' })).toBeEnabled());
    await userEvent.selectOptions(selector, '12');
    await expect(await canvas.findByRole('combobox', { name: 'Revision' })).toHaveDisplayValue(/^r12 · current/);
    await expect(canvas.getByRole('combobox', { name: 'Revision' })).toHaveValue('12');
    // The detail follows the `rev` it wrote: r12's actions, not r11's.
    await expect(await canvas.findByRole('button', { name: 'Pin workload to r12' })).toBeInTheDocument();
    await expect(canvas.queryByRole('button', { name: 'Restore to r11' })).toBeNull();
  },
};

// The four revision actions sit on one row, each with its glyph, and say what
// they do without a trailing ellipsis.
export const ActionsRow: Story = {
  play: async ({ canvas }) => {
    const restore = await canvas.findByRole('button', { name: 'Restore to r11' });
    const row = restore.closest('.actions');
    const names = ['Restore to r11', 'Pin workload to r11', 'Diff vs previous', 'Diff vs current'];
    const buttons = names.map((name) => canvas.getByRole('button', { name }));
    for (const button of buttons) {
      await expect(button.closest('.actions')).toBe(row);
      await expect(button.querySelector('svg.glyph')).not.toBeNull();
      await expect(button.textContent).not.toContain('…');
    }
    await expect(row?.querySelectorAll('button')).toHaveLength(4);
    await expect(new Set(buttons.map((button) => Math.round(button.getBoundingClientRect().top))).size).toBe(1);
  },
};

// The environment switcher is the shared tabs: one tab stop, arrows move.
export const EnvironmentTabs: Story = {
  parameters: app(`env=${PRD}&rev=11`, [
    ...rows(),
    { url: `${PROJECT_URL}/environments/${STG}/revisions`, body: { items: [], count: 0 } },
    { url: `${PROJECT_URL}/environments/${STG}/pins`, body: { items: [], count: 0 } },
  ]),
  play: async ({ canvas }) => {
    const production = await canvas.findByRole('tab', { name: 'production' });
    await expect(production).toHaveAttribute('aria-selected', 'true');
    await expect(canvas.getByRole('tabpanel', { name: 'production' })).toBeVisible();
    const tabs = canvas.getAllByRole('tab');
    await expect(tabs.map((tab) => tab.getAttribute('tabindex'))).toEqual(['-1', '-1', '0']);
    production.focus();
    await userEvent.keyboard('{ArrowLeft}');
    const staging = canvas.getByRole('tab', { name: 'staging' });
    await waitFor(() => expect(staging).toHaveAttribute('aria-selected', 'true'));
    await expect(staging).toHaveFocus();
    await expect(await canvas.findByRole('tabpanel', { name: 'staging' })).toBeVisible();
    // Switching environment drops `rev`: staging opens on its own lineage.
    await expect(await canvas.findByText('No revisions published in this environment yet.')).toBeVisible();
  },
};

/** A disabled button is visibly unavailable, never a blank box: label shown in faint ink, flat, not-allowed. */
const expectVisiblyDisabled = async (button: HTMLElement) => {
  await expect(button).toBeDisabled();
  await expect(button.textContent?.trim()).not.toBe('');
  await expect(button.querySelector('svg.glyph')?.getBoundingClientRect().width).toBeGreaterThan(0);
  const style = getComputedStyle(button);
  await expect(style.cursor).toBe('not-allowed');
  await expect(style.opacity).toBe('1');
  await expect(style.color).toBe(tokenColor('--tx-faint'));
};

// A revision retention collected: lineage only, so restore, pin and both diffs
// are refused, each still labelled and visibly unavailable, and restore and pin
// say why in their title.
export const CollectedRevision: Story = {
  parameters: app(`env=${PRD}&rev=5`),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('combobox', { name: 'Revision' })).toHaveDisplayValue(/^r5 · payload collected · /);
    const restore = await canvas.findByRole('button', { name: 'Restore to r5' });
    for (const name of ['Restore to r5', 'Pin workload to r5', 'Diff vs previous', 'Diff vs current']) {
      await expectVisiblyDisabled(canvas.getByRole('button', { name }));
    }
    await expect(restore).toHaveAttribute('title', expect.stringMatching(/^r5's payload was collected by retention policy/));
    await expect(canvas.getByRole('button', { name: 'Pin workload to r5' })).toHaveAttribute('title', expect.stringMatching(/restore and pin are refused/));
    await expect(canvas.getByRole('button', { name: 'Restore DATABASE_URL to r5' })).toBeDisabled();
  },
};

// The current revision: nothing to restore or diff against itself, pinning
// still offered.
export const CurrentRevision: Story = {
  parameters: app(`env=${PRD}&rev=12`),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('combobox', { name: 'Revision' })).toHaveDisplayValue(/^r12 · current/);
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Pin workload to r12' })).toBeEnabled());
    const restore = canvas.getByRole('button', { name: 'Restore to r12' });
    await expectVisiblyDisabled(restore);
    await expect(restore).toHaveAttribute('title', 'r12 is already the current revision, so a restore would stage nothing.');
    await expectVisiblyDisabled(canvas.getByRole('button', { name: 'Diff vs current' }));
    await expect(canvas.getByRole('button', { name: 'Diff vs previous' })).toBeEnabled();
  },
};

// The retention line wraps inside the header rather than running out of it.
export const RetentionWraps: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const line = await canvas.findByText(/^values kept: /);
    const box = line.closest('.history__retention');
    await expect(line.textContent).toMatch(/^values kept: \d+ days? or the last/);
    await expect(box?.scrollWidth).toBeLessThanOrEqual(box?.clientWidth ?? 0);
    // It really wraps onto more than one line at this width, nothing clipped.
    const fontSize = Number.parseFloat(getComputedStyle(line).fontSize);
    await expect(line.getBoundingClientRect().height).toBeGreaterThan(fontSize * 2);
    await expect(line.scrollWidth).toBeLessThanOrEqual(line.clientWidth);
  },
};

// A phone gets the same single detail and selector, nothing hidden.
export const Phone: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  parameters: app(`env=${PRD}`),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('combobox', { name: 'Revision' })).toHaveDisplayValue(/^r12 · current/);
    await expect(canvas.getByRole('combobox', { name: 'Revision' })).toBeVisible();
    // The header Close collapses to icon-only here, square, "Close revision
    // history" unchanged as its accessible name.
    const close = canvas.getByRole('link', { name: 'Close revision history' });
    const label = close.querySelector('.btn__label');
    if (!(label instanceof HTMLElement)) throw new Error('close label missing');
    await expect(label.getBoundingClientRect().width).toBeLessThanOrEqual(1);
    const box = close.getBoundingClientRect();
    await expect(Math.round(box.width)).toBe(Math.round(box.height));
  },
};

/** The router's current search string, for plays that assert what the URL says. */
function LocationProbe() {
  return <output hidden data-story-location>{useLocation().search}</output>;
}
const storyLocation = (root: HTMLElement) => root.querySelector('[data-story-location]')?.textContent ?? '';

// S5/S6: choosing a revision that is neither the first option nor the one the
// URL opened on moves `rev` to it, keeps focus on the selector, and the detail
// follows (r9 is pinned, so its option says so).
export const SelectOlderRevision: Story = {
  decorators: [(Story) => <><Story /><LocationProbe /></>],
  play: async ({ canvas, canvasElement }) => {
    const selector = await canvas.findByRole('combobox', { name: 'Revision' });
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Restore to r11' })).toBeEnabled());
    await expect(new URLSearchParams(storyLocation(canvasElement)).get('rev')).toBe('11');
    await userEvent.selectOptions(selector, '9');
    await waitFor(() => expect(new URLSearchParams(storyLocation(canvasElement)).get('rev')).toBe('9'));
    const after = canvas.getByRole('combobox', { name: 'Revision' });
    await expect(after).toHaveValue('9');
    await expect(after).toHaveDisplayValue(/^r9 · pinned · /);
    await expect(after).toHaveFocus();
    await expect(await canvas.findByRole('button', { name: 'Pin workload to r9' })).toBeInTheDocument();
    await expect(canvas.queryByRole('button', { name: 'Restore to r11' })).toBeNull();
  },
};

// Pin expiry spells its unit out on every tier (today, days), never "5 d".
export const PinExpiryWording: Story = {
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByText('!! expires in 5 days')).toBeVisible();
    await expect(canvas.getByText('! expires in 20 days')).toBeVisible();
    await expect(canvas.getByText('expires in 45 days')).toBeVisible();
    await expect(canvas.getByText('!!! expires today')).toBeVisible();
    for (const expiry of canvasElement.querySelectorAll('.history__expiry')) {
      await expect(expiry.textContent).not.toMatch(/\d d\b/);
    }
    const retention = canvas.getByText(/^values kept: /);
    await expect(retention).toHaveTextContent('values kept: 90 days or the last 10 revisions, whichever is longer, plus pinned');
    // The retention knob is the project settings Policy tab.
    await expect(canvas.getByRole('link', { name: /change it in project settings › Policy/ }).getAttribute('href')).toMatch(/\?tab=policy$/);
  },
};

const VERSION = 'ver_123e4567-e89b-12d3-a456-426614174060';
const restored = {
  revision: 10,
  changes: [{
    version_id: VERSION,
    key_id: keyId(2),
    name: 'PUBLIC_BASE_URL',
    classification: 'config',
    operation: 'set',
    staged_from_revision: 12,
    created_at: hoursAgo(0),
  }],
  preview: {
    token: 'tok_0123456789abcdef',
    environments: [{
      environment_id: PRD,
      base_revision: 12,
      schema_revision: 9,
      protected: true,
      changes: [{
        version_id: VERSION,
        key_id: keyId(2),
        name: 'PUBLIC_BASE_URL',
        classification: 'config',
        operation: 'set',
        status: 'edited',
        before: 'https://app.example.com',
        after: 'https://old.example.com',
      }],
    }],
  },
} satisfies z.input<typeof zRollbackResult>;

// A staged per-key restore: the impact names its environment, then the step
// "r12 → r13" after the shared separator, with the protected tag carrying the
// lock; the kind tag has its own icon.
export const RestoreStaged: Story = {
  parameters: app(`env=${PRD}&rev=10`, [
    { url: `${REVISIONS_URL}/10/rollback`, method: 'POST', body: restored },
    ...rows(),
  ]),
  play: async ({ canvas }) => {
    const restore = await canvas.findByRole('button', { name: 'Restore PUBLIC_BASE_URL to r10' });
    await waitFor(() => expect(restore).toBeEnabled());
    await userEvent.click(restore);
    await userEvent.click(await canvas.findByRole('button', { name: 'Stage the restore from r10' }));
    const heading = await canvas.findByRole('heading', { name: /^production\s*·\s*r12 → r13/ });
    await expect(heading.querySelector('.sep')).not.toBeNull();
    await expect(heading.querySelector('.badge--danger')).toHaveTextContent('protected');
    await expect(heading.querySelector('.badge--danger svg')).toHaveAttribute('data-glyph', 'lock');
    const kind = heading.parentElement?.querySelector('.history__impact .history__kind');
    await expect(kind).toHaveTextContent('set');
    await expect(kind?.querySelector('svg')).toHaveAttribute('data-glyph', 'edit');
    await expect(canvas.getByText('Staged 1 draft from r10. Nothing is published yet.')).toBeVisible();
  },
};

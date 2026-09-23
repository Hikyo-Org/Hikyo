import type { Meta, StoryObj } from '@storybook/react-vite';
import { zDefinitionsSettings, zKey, zKeyGroupList } from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { KEY_GONE_REFUSAL } from '../api/matrix.ts';
import { ORG, PRJ } from '../testkit/ids.ts';
import { KeyDeclarationDetail } from './KeyDeclarationDetail.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The catalogue declaration detail: one key's declaration, presence and
// organisation facts, plus the editors when the project's definitions source is
// confirmed `db`. It reads the key, the definitions source, the instance config
// (to spot the system project; a non-operator's 403 keeps it quiet and stops
// the poll) and, in db mode, the key groups for the linked-keys editor.
const KEY = 'key_123e4567-e89b-12d3-a456-426614174004';
const GROUP = 'kgr_123e4567-e89b-12d3-a456-426614174050';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const PATH = `/orgs/${ORG}/projects/${PRJ}/matrix/keys/${KEY}`;
const ROUTE = '/orgs/:org/projects/:project/matrix/keys/:key';

const DEV = 'env_123e4567-e89b-12d3-a456-426614174010';
const PRD = 'env_123e4567-e89b-12d3-a456-426614174012';

const stamp = { org_id: ORG, project_id: PRJ, created_at: '2026-01-01T00:00:00Z' };

const environments = [
  { id: DEV, name: 'development', display_order: 0, ...stamp },
  { id: PRD, name: 'production', display_order: 1, ...stamp },
];

const record = {
  id: KEY,
  name: 'DATABASE_URL',
  folder_path: 'database',
  classification: 'secret',
  description: 'Primary datastore connection string',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'url', schemes: ['postgres', 'postgresql'] } },
  presence: {
    required_in: { mode: 'explicit', environment_ids: [PRD] },
    forbidden_in: { mode: 'none' },
  },
  group_id: GROUP,
  ...stamp,
} satisfies z.input<typeof zKey>;

// The deprecated variant: an any_of declaration, a note, and recorded findings.
const deprecated = {
  ...record,
  name: 'LEGACY_WAREHOUSE_DSN',
  classification: 'config',
  deprecated: true,
  deprecation_note: 'Replaced by the warehouse adapter.',
  declaration: {
    any_of: [
      { type: 'url', schemes: ['postgres'] },
      { type: 'string', min_length: 12, allow_empty: false, pattern: '^dsn:' },
    ],
  },
  presence: { required_in: { mode: 'all' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  findings: [
    { rule_id: 'generic-high-entropy', surface: 'edit', locator: 'description:1' },
    { rule_id: 'postgres-uri', surface: 'value_write', locator: `${DEV}:1` },
  ],
} satisfies z.input<typeof zKey>;

const groups = {
  count: 1,
  items: [
    { id: GROUP, name: 'database', members: ['DATABASE_URL', 'DATABASE_POOL_SIZE'], inert: false, ...stamp },
  ],
} satisfies z.input<typeof zKeyGroupList>;

const db = { definitions_source: 'db' } satisfies z.input<typeof zDefinitionsSettings>;
const git = {
  definitions_source: 'git',
  last_apply: {
    plan_id: 'pln_123e4567-e89b-12d3-a456-426614174000',
    applied_at: '2026-09-01T10:00:00Z',
    applied_by: 'prn_123e4567-e89b-12d3-a456-426614174000',
    commit: 'abc1234',
    ref: 'refs/heads/main',
    actor: 'ci@example.com',
    revision: 7,
  },
} satisfies z.input<typeof zDefinitionsSettings>;

type Answer = Omit<MockRoute, 'url'>;

const app = (rest: { key?: Answer; definitions?: Answer } = {}) => ({
  app: {
    path: PATH,
    routePath: ROUTE,
    responses: [
      { url: `${PROJECT_URL}/keys/${KEY}`, body: record, ...rest.key },
      { url: `${PROJECT_URL}/definitions/settings`, body: db, ...rest.definitions },
      { url: `${PROJECT_URL}/key-groups`, body: groups },
      { url: '/api/v1/instance/config', status: 403, body: { error: { code: 'forbidden', message: 'forbidden' } } },
    ],
  },
});

const noImpact = { setEnvironmentIds: [], pendingEnvironmentIds: [] };

const meta = {
  component: KeyDeclarationDetail,
  tags: ['ai-generated'],
  args: {
    refData: { org: ORG, project: PRJ },
    keyId: KEY,
    environments,
    impact: noImpact,
    impactReady: true,
    openerRef: { current: null },
  },
  parameters: { ...topLayerDocs, ...app() },
} satisfies Meta<typeof KeyDeclarationDetail>;

export default meta;
type Story = StoryObj<typeof meta>;

// A database-managed project: every fact, and the full editor stack below them.
export const Editable: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Edit declaration' })).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'Rename key' })).toBeVisible();
    await expect(canvas.getByText('postgres, postgresql', { exact: false })).toBeVisible();
  },
};

// A deprecated key still delivering values: the warning names the environments,
// the any_of rules list, and the recorded scanning findings.
export const Deprecated: Story = {
  args: { impact: { setEnvironmentIds: [DEV, PRD], pendingEnvironmentIds: [] } },
  parameters: app({ key: { body: deprecated } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/Deprecated with 2 live values across development, production/)).toBeVisible();
    await expect(canvas.getByText('Valid if it matches any one of:')).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'Scanning findings' })).toBeVisible();
  },
};

// The reclassification confirm open over the panel, with its impact preview.
export const ReclassifyConfirm: Story = {
  args: { impact: { setEnvironmentIds: [DEV, PRD], pendingEnvironmentIds: [DEV] } },
  play: async ({ canvas }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Reclassify as config…' }));
    // The delete section previews the same impact; scope to the confirm dialog.
    const dialog = within(await canvas.findByRole('dialog', { name: 'Reclassify this secret as config?' }));
    await expect(dialog.getByText(/Delivers a value in 2 environments: development, production/)).toBeVisible();
    await expect(dialog.getByText(/Unpublished drafts touch 1 environment: development/)).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Reclassify as config' })).toBeEnabled();
  },
};

// A Git-managed project: declarations read-only behind the notice and the
// last-applied provenance labels.
export const GitReadOnly: Story = {
  parameters: app({ definitions: { body: git } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Declarations are read-only' })).toBeVisible();
    await expect(canvas.getByText(/Last applied by/)).toBeVisible();
    await expect(canvas.queryByRole('heading', { name: 'Rename key' })).not.toBeInTheDocument();
  },
};

// The definitions source could not be read: editing fails closed and says why.
export const SourceFailed: Story = {
  parameters: app({ definitions: { status: 500 } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Editing unavailable' })).toBeVisible();
    await expect(canvas.getByText(/definitions source could not be read/)).toBeVisible();
  },
};

// The key read never settles.
export const Loading: Story = {
  parameters: app({ key: { pending: true } }),
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText('Loading the key declaration…')).toBeVisible());
  },
};

// A stale link to a deleted or renamed-away key: the one canonical sentence and the way back.
export const Gone: Story = {
  parameters: app({ key: { status: 404, body: { error: { code: 'not_found', message: 'not found' } } } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(KEY_GONE_REFUSAL)).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Back to the matrix' })).toBeVisible();
  },
};

// Any other read failure is reported with its status.
export const Failed: Story = {
  parameters: app({ key: { status: 500, body: { error: { code: 'internal', message: 'boom' } } } }),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('The key could not be loaded (error 500).')).toBeVisible();
  },
};

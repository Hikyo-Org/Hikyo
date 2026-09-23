import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zDefinitionsSettings,
  zEnvironmentList,
  zEnvironmentSettings,
  zEnvironmentSignals,
  zKey,
  zKeyGroupList,
  zKeyList,
  zPendingDraftList,
  zValueList,
} from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { Matrix } from './Matrix.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The whole-project environment matrix. On mount it reads the project catalogue
// (environments, keys, key groups), then four things per environment (values,
// signals, settings, pending drafts), plus the definitions source, the folders,
// the instance config (to spot the system project) and the advisory event
// stream. A missing catalogue read blanks the grid; a missing per-environment
// read degrades that column in place (#451). Wire bodies here are `z.input`
// shapes: int64 fields ride as plain numbers and are coerced to bigint by the
// generated schemas when the screen parses them.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const PATH = `/orgs/${ORG}/projects/${PRJ}/matrix`;
const ROUTE = '/orgs/:org/projects/:project/matrix';
const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;

const DEV = 'env_123e4567-e89b-12d3-a456-426614174010';
const STG = 'env_123e4567-e89b-12d3-a456-426614174011';
const PRD = 'env_123e4567-e89b-12d3-a456-426614174012';

type Key = z.input<typeof zKey>;
type Signals = z.input<typeof zEnvironmentSignals>;
type Drafts = z.input<typeof zPendingDraftList>;
type Values = z.input<typeof zValueList>;

const environments = {
  count: 3,
  items: [
    { id: DEV, name: 'development', display_order: 0 },
    { id: STG, name: 'staging', display_order: 1 },
    { id: PRD, name: 'production', display_order: 2 },
  ].map((item) => ({ ...item, org_id: ORG, project_id: PRJ, created_at: '2026-01-01T00:00:00Z' })),
} satisfies z.input<typeof zEnvironmentList>;

const keyId = (n: number) => `key_123e4567-e89b-12d3-a456-4266141740${String(n).padStart(2, '0')}`;
const STRIPE_GROUP = 'kgr_123e4567-e89b-12d3-a456-426614174050';

const key = (
  n: number,
  name: string,
  folder: string,
  classification: 'secret' | 'config',
  extra: Partial<Key> = {},
): Key => ({
  id: keyId(n),
  org_id: ORG,
  project_id: PRJ,
  name,
  folder_path: folder,
  classification,
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: false } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '',
  created_at: '2026-01-01T00:00:00Z',
  ...extra,
});

// Four folders (one of them "No folder"), a linked-key group, secrets and
// config, a key required everywhere and one required only in production, and
// a name near the 128-character ceiling for the sticky key column.
const keys: readonly Key[] = [
  key(1, 'LOG_LEVEL', '', 'config'),
  key(2, 'PUBLIC_BASE_URL', '', 'config', { presence: { required_in: { mode: 'all' }, forbidden_in: { mode: 'none' } } }),
  key(3, 'FEATURE_FLAGS', '', 'config'),
  key(4, 'DATABASE_URL', 'database', 'secret', { presence: { required_in: { mode: 'all' }, forbidden_in: { mode: 'none' } } }),
  key(5, 'DATABASE_POOL_SIZE', 'database', 'config', { declaration: { rule: { type: 'integer', min: 1, max: 64 } } }),
  key(6, 'DATABASE_READ_REPLICA_URL', 'database', 'secret'),
  key(7, 'STRIPE_SECRET_KEY', 'payments', 'secret', { group_id: STRIPE_GROUP }),
  key(8, 'STRIPE_WEBHOOK_SECRET', 'payments', 'secret', { group_id: STRIPE_GROUP }),
  key(9, 'STRIPE_PUBLISHABLE_KEY', 'payments', 'config'),
  key(10, 'PAYMENT_PROVIDER', 'payments', 'config', { declaration: { rule: { type: 'enum', members: ['stripe', 'adyen', 'mollie'] } } }),
  key(11, 'OTEL_EXPORTER_OTLP_ENDPOINT', 'observability', 'config', { declaration: { rule: { type: 'url', schemes: ['https'] } } }),
  key(12, 'OTEL_EXPORTER_OTLP_HEADERS', 'observability', 'secret'),
  key(13, 'SENTRY_DSN', 'observability', 'secret', {
    presence: { required_in: { mode: 'explicit', environment_ids: [PRD] }, forbidden_in: { mode: 'none' } },
  }),
  key(14, 'ANALYTICS_PIPELINE_WAREHOUSE_CONNECTION_STRING_FOR_THE_NIGHTLY_EXPORT_JOB_IN_EUROPE_WEST', 'observability', 'config', {
    deprecated: true,
    deprecation_note: 'Replaced by the warehouse adapter.',
  }),
];

const keyList = { items: [...keys], count: keys.length, schema_revision: 12 } satisfies z.input<typeof zKeyList>;

const groups = {
  count: 1,
  items: [
    {
      id: STRIPE_GROUP,
      org_id: ORG,
      project_id: PRJ,
      name: 'stripe',
      members: ['STRIPE_SECRET_KEY', 'STRIPE_WEBHOOK_SECRET'],
      inert: false,
      created_at: '2026-01-01T00:00:00Z',
    },
  ],
} satisfies z.input<typeof zKeyGroupList>;

/** A value row for `n`; `value` undefined = absent, a secret carries only presence. */
const cell = (n: number, value?: string): Values['items'][number] => {
  const record = keys[n - 1];
  if (record === undefined) throw new Error(`no key ${String(n)}`);
  const config = record.classification === 'config';
  return {
    key_id: record.id,
    name: record.name,
    classification: record.classification,
    set: value !== undefined,
    revealed: config && value !== undefined,
    ...(config && value !== undefined ? { value } : {}),
  };
};

const values = (items: readonly Values['items'][number][]): Values => ({ items: [...items], count: items.length });

const signal = (
  n: number,
  extra: Partial<Omit<Signals['cells'][number], 'key_id' | 'name' | 'classification'>> = {},
): Signals['cells'][number] => {
  const record = keys[n - 1];
  if (record === undefined) throw new Error(`no key ${String(n)}`);
  return { key_id: record.id, name: record.name, classification: record.classification, pending_by_others: false, ...extra };
};

const VER_OWN = 'ver_123e4567-e89b-12d3-a456-426614174060';
const VER_CLEAR = 'ver_123e4567-e89b-12d3-a456-426614174061';
const VER_INVALID = 'ver_123e4567-e89b-12d3-a456-426614174062';
const OWNER = 'prn_123e4567-e89b-12d3-a456-426614174010';

const draft = (
  n: number,
  versionId: string,
  operation: 'set' | 'unset',
  extra: Partial<Drafts['items'][number]> = {},
): Drafts['items'][number] => {
  const record = keys[n - 1];
  if (record === undefined) throw new Error(`no key ${String(n)}`);
  return {
    version_id: versionId,
    key_id: record.id,
    name: record.name,
    classification: record.classification,
    operation,
    staged_from_revision: 7,
    created_at: '2026-09-20T09:00:00Z',
    revealed: false,
    ...extra,
  };
};

const noDrafts: Drafts = { items: [], count: 0 };

// development: fully set, one config draft (set) and one draft another editor holds.
const devValues = values([
  cell(1, 'debug'), cell(2, 'https://dev.example.com'), cell(3, 'checkout-v2,search-rerank'),
  cell(4, 'x'), cell(5, '8'), cell(6, 'x'), cell(7, 'x'), cell(8, 'x'), cell(9, 'pk_test_51N'),
  cell(10, 'stripe'), cell(11, 'https://otel.dev.example.com'), cell(12, 'x'), cell(13),
  cell(14, 'warehouse://eu-west-4/nightly'),
]);
const devSignals: Signals = {
  environment_id: DEV,
  revision: 7,
  cells: [
    signal(1, { pending_version_id: VER_OWN, pending_operation: 'set' }),
    signal(3, { changed_in_revision: 7 }),
    signal(9, { pending_by_others: true }),
  ],
};
const devDrafts: Drafts = {
  count: 1,
  items: [draft(1, VER_OWN, 'set', { revealed: true, value: 'trace', advisory: { owner_id: OWNER, valid: true } })],
};

// staging: a required key absent (a problem), an invalid draft and a clear draft.
const stgValues = values([
  cell(1, 'info'), cell(2), cell(3, 'checkout-v2'), cell(4, 'x'), cell(5, 'sixteen'), cell(6),
  cell(7, 'x'), cell(8), cell(9, 'pk_test_51N'), cell(10, 'adyen'), cell(11, 'https://otel.stg.example.com'),
  cell(12, 'x'), cell(13), cell(14),
]);
const stgSignals: Signals = {
  environment_id: STG,
  revision: 3,
  cells: [
    signal(5, { pending_version_id: VER_INVALID, pending_operation: 'set', changed_in_revision: 3 }),
    signal(11, { pending_version_id: VER_CLEAR, pending_operation: 'unset' }),
    signal(4, { changed_in_revision: 2 }),
  ],
};
const stgDrafts: Drafts = {
  count: 2,
  items: [
    draft(5, VER_INVALID, 'set', { revealed: true, value: 'sixteen', advisory: { owner_id: OWNER, valid: false } }),
    draft(11, VER_CLEAR, 'unset'),
  ],
};

// production: PROTECTED, fully set, nothing pending, one change since publish.
const prdValues = values([
  cell(1, 'warn'), cell(2, 'https://app.example.com'), cell(3, 'checkout-v2,search-rerank,beta-dashboard'), cell(4, 'x'), cell(5, '32'), cell(6, 'x'),
  cell(7, 'x'), cell(8, 'x'), cell(9, 'pk_live_51N'), cell(10, 'stripe'), cell(11, 'https://otel.example.com'),
  cell(12, 'x'), cell(13, 'x'), cell(14, 'warehouse://eu-west-4/nightly'),
]);
const prdSignals: Signals = { environment_id: PRD, revision: 41, cells: [signal(2, { changed_in_revision: 41 })] };

const settings = (isProtected: boolean) =>
  ({ protected: isProtected, reauth_window_seconds: isProtected ? 0 : 600 }) satisfies z.input<typeof zEnvironmentSettings>;

/** The four per-environment reads, each answered from `rest`. */
type Answer = Omit<MockRoute, 'url'>;

const environmentRows = (
  env: string,
  rest: { values: Answer; signals: Answer; settings: Answer; pending: Answer },
): readonly MockRoute[] => [
  { url: `${PROJECT_URL}/environments/${env}/values`, ...rest.values },
  { url: `${PROJECT_URL}/environments/${env}/signals`, ...rest.signals },
  { url: `${PROJECT_URL}/environments/${env}/settings`, ...rest.settings },
  { url: `${PROJECT_URL}/environments/${env}/pending`, ...rest.pending },
];

const readyRows = (env: string, body: { values: Values; signals: Signals; pending: Drafts; protected: boolean }) =>
  environmentRows(env, {
    values: { body: body.values },
    signals: { body: body.signals },
    settings: { body: settings(body.protected) },
    pending: { body: body.pending },
  });

const failedRows = (env: string, status: number) => {
  const failure: Answer = { status, body: { error: status === 403 ? 'forbidden' : 'read failed' } };
  return environmentRows(env, { values: failure, signals: failure, settings: failure, pending: failure });
};

const definitions = (source: 'db' | 'git') => ({
  url: `${PROJECT_URL}/definitions/settings`,
  body: { definitions_source: source } satisfies z.input<typeof zDefinitionsSettings>,
});

// The reads every matrix story shares: no system-project binding (a non-operator
// gets 403 on the instance config and the notice stays hidden), no declared
// folders beyond the keys' own, and an advisory stream that never connects (a
// pending row holds it in "connecting" rather than a reconnect loop).
const shared: readonly MockRoute[] = [
  { url: '/api/v1/instance/config', status: 403, body: { error: 'forbidden' } },
  { url: `${PROJECT_URL}/folders`, body: { items: [], count: 0 } },
  { url: `${PROJECT_URL}/events`, pending: true },
];

const catalogue = (rest: {
  environments?: Answer;
  keys?: Answer;
  groups?: Answer;
  definitions?: MockRoute;
}): readonly MockRoute[] => [
  { url: `${PROJECT_URL}/environments`, body: environments, ...rest.environments },
  { url: `${PROJECT_URL}/keys`, body: keyList, ...rest.keys },
  { url: `${PROJECT_URL}/key-groups`, body: groups, ...rest.groups },
  rest.definitions ?? definitions('db'),
];

const allReady: readonly MockRoute[] = [
  ...readyRows(DEV, { values: devValues, signals: devSignals, pending: devDrafts, protected: false }),
  ...readyRows(STG, { values: stgValues, signals: stgSignals, pending: stgDrafts, protected: false }),
  ...readyRows(PRD, { values: prdValues, signals: prdSignals, pending: noDrafts, protected: true }),
];

const app = (responses: readonly MockRoute[]) => ({
  app: { path: PATH, routePath: ROUTE, responses: [...responses, ...shared] },
});

const meta = {
  component: Matrix,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, ...app([...catalogue({}), ...allReady]) },
} satisfies Meta<typeof Matrix>;

export default meta;
type Story = StoryObj<typeof meta>;

// Every column readable: folder groups, a linked-key glyph, secrets masked,
// draft dots and deltas, one required-absent problem, a PROTECTED column and
// the drafts pill counting the unpublished edits.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Environment matrix' })).toBeVisible();
    // A row deep in the list: the virtualiser is rendering the whole catalogue.
    await expect(canvas.getByRole('link', { name: 'Declaration of SENTRY_DSN, secret' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^PUBLIC_BASE_URL in staging: ! required · absent/ })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^LOG_LEVEL in development: debug, unpublished draft set/ })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /3 unpublished edits · Publish drafts/ })).toBeVisible();
    await expect(canvas.getAllByText('PROTECTED').length).toBeGreaterThan(0);
  },
};

// Two columns degraded in place (#451): production's reads denied, staging's
// failed. The grid stays up; those cells read `· unreadable`.
export const Degraded: Story = {
  parameters: app([
    ...catalogue({}),
    ...readyRows(DEV, { values: devValues, signals: devSignals, pending: devDrafts, protected: false }),
    ...failedRows(STG, 500),
    ...failedRows(PRD, 403),
  ]),
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('No access to this environment')).toBeVisible();
    await expect(canvas.getByText('Values could not be loaded')).toBeVisible();
    await expect(canvas.getAllByLabelText('production unreadable: no access')).toHaveLength(keys.length);
    await expect(canvas.getAllByLabelText('staging unreadable: values could not be loaded')).toHaveLength(keys.length);
  },
};

// Declarations come from Git (#492): the header loses `+ New key`, the notice
// explains why, and every value action stays.
export const GitManaged: Story = {
  parameters: app([...catalogue({ definitions: definitions('git') }), ...allReady]),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Environment matrix' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: '+ New key' })).not.toBeInTheDocument();
    await expect(canvas.getByRole('button', { name: 'Import' })).toBeVisible();
  },
};

// Environments exist but no key is declared: the empty state offers the first
// declaration, the manage dialog and the CLI alternative.
export const Empty: Story = {
  parameters: app([
    ...catalogue({ keys: { body: { items: [], count: 0, schema_revision: 0 } satisfies z.input<typeof zKeyList> } }),
    ...readyRows(DEV, { values: values([]), signals: { environment_id: DEV, revision: 0, cells: [] }, pending: noDrafts, protected: false }),
    ...readyRows(STG, { values: values([]), signals: { environment_id: STG, revision: 0, cells: [] }, pending: noDrafts, protected: false }),
    ...readyRows(PRD, { values: values([]), signals: { environment_id: PRD, revision: 0, cells: [] }, pending: noDrafts, protected: true }),
  ]),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'No keys yet' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Declare first key' })).toBeVisible();
  },
};

// A project with no environment at all: nothing can hold a value yet, so the
// empty state points at project settings.
export const NoEnvironments: Story = {
  parameters: app(catalogue({ environments: { body: { items: [], count: 0 } satisfies z.input<typeof zEnvironmentList> } })),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'No environments yet' })).toBeVisible();
    await expect(canvas.getByRole('link', { name: /Project settings › New environment/ })).toBeVisible();
  },
};

// The catalogue never settles: the one-line loading status.
export const Loading: Story = {
  parameters: app([...catalogue({ keys: { pending: true } }), ...allReady]),
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText('Loading environment matrix…')).toBeVisible());
  },
};

// The catalogue is denied: a permission page, not a reload prompt (#451).
export const Forbidden: Story = {
  parameters: app([...catalogue({ keys: { status: 403, body: { error: 'forbidden' } } }), ...allReady]),
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByText("You do not have permission to view this project's environment matrix."),
    ).toBeVisible();
  },
};

// The catalogue read failed: the reload prompt.
export const Failed: Story = {
  parameters: app([...catalogue({ keys: { status: 500, body: { error: 'keys read failed' } } }), ...allReady]),
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByText('The environment matrix could not be loaded. Reload to try again.'),
    ).toBeVisible();
  },
};

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
import { expect, userEvent, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { ORG, PRJ } from '../testkit/ids.ts';
import { Matrix } from './Matrix.tsx';

import { tokenColor } from '../../.storybook/tokens.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The whole-project environment matrix. On mount it reads the project catalogue
// (environments, keys, key groups), then four things per environment (values,
// signals, settings, pending drafts), plus the definitions source, the folders,
// the instance config (to spot the system project) and the advisory event
// stream. A missing catalogue read blanks the grid; a missing per-environment
// read degrades that column in place (#451). Wire bodies here are `z.input`
// shapes: int64 fields ride as plain numbers and are coerced to bigint by the
// generated schemas when the screen parses them.
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
    // First published in r1: it exists, it has not changed, so no mark.
    signal(1, { changed_in_revision: 1 }),
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
  body: { definitions_source: source, can_declare_keys: true, can_edit_definitions: true } satisfies z.input<typeof zDefinitionsSettings>,
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

const app = (responses: readonly MockRoute[], search = '') => ({
  app: { path: `${PATH}${search}`, routePath: ROUTE, responses: [...responses, ...shared] },
});

const px = (value: string) => Number.parseFloat(value);

/** Sections are ruled, rows are not; key glyphs are 40% of the row. */
const expectSectionsAndGlyphs = async (root: HTMLElement) => {
  const keyRow = root.querySelector<HTMLElement>('.matrix__key-row > th');
  const groupRow = root.querySelector<HTMLElement>('.matrix__group-row th');
  const lock = root.querySelector<SVGElement>('.matrix__key .glyph[data-glyph="lock"]');
  if (keyRow === null || groupRow === null || lock === null) throw new Error('matrix rows missing');
  await expect(px(getComputedStyle(keyRow).borderBottomWidth)).toBe(0);
  await expect(px(getComputedStyle(keyRow).borderTopWidth)).toBe(0);
  // The first group row sits under the header's own rule; a later one carries its own.
  const laterGroup = root.querySelectorAll<HTMLElement>('.matrix__group-row th')[1];
  if (laterGroup === undefined) throw new Error('second group missing');
  await expect(getComputedStyle(laterGroup).borderTopStyle).toBe('solid');
  await expect(px(getComputedStyle(laterGroup).borderTopWidth)).toBe(1);
  // The row keeps its height; the lock is 40% of it.
  const rowHeight = keyRow.getBoundingClientRect().height;
  await expect(lock.getBoundingClientRect().height).toBeCloseTo(rowHeight * 0.4, 0);
  // A config key's braces icon shows on desktop too, same size as the
  // lock, so config and secret keys both
  // carry an icon their names align against.
  const config = root.querySelector<SVGElement>('.matrix__key .glyph[data-glyph="config"]');
  if (config === null) throw new Error('config glyph missing');
  await expect(getComputedStyle(config).display).not.toBe('none');
  await expect(config.getBoundingClientRect().width).toBeCloseTo(lock.getBoundingClientRect().width, 0);
};

// The table spans its scroll area and the trailing aria-hidden filler column
// takes whatever the environment columns leave.
const expectColumnLayout = async (root: HTMLElement) => {
  // Column widths are sized for IBM Plex Mono's advance. Measuring while the
  // swap-period fallback face still paints the values reads a different font's
  // width, so wait for both weights the cells use before reading any box.
  await Promise.all(['400', '500'].map((weight) => document.fonts.load(`${weight} 13px "IBM Plex Mono"`)));
  await document.fonts.ready;
  await expect(document.fonts.check('13px "IBM Plex Mono"')).toBe(true);
  const scroller = root.querySelector('.matrix__scroll');
  const table = root.querySelector<HTMLElement>('.matrix__table');
  const filler = root.querySelector('.matrix__table thead .matrix__filler');
  if (scroller === null || table === null || filler === null) throw new Error('matrix table missing');
  await expect(filler).toHaveAttribute('aria-hidden', 'true');
  await expect(table.getBoundingClientRect().width).toBeGreaterThanOrEqual(scroller.clientWidth - 1);
  const heads = [...root.querySelectorAll<HTMLElement>('.matrix__table thead th')].slice(1, -1);
  const chars = heads.map((head) => Number(head.style.getPropertyValue('--chars')));
  const values = [...root.querySelectorAll<HTMLElement>('.matrix-cell__value')];
  const truncated = values.filter((value) => value.scrollWidth > value.clientWidth + 1);
  const overflows = table.getBoundingClientRect().width > scroller.clientWidth + 1;
  return { chars, values, truncated, overflows, fillerWidth: filler.getBoundingClientRect().width };
};

// Column header titles (environment names, "Key") read as eyebrows: caps and
// bold, on both phone and desktop; the source text itself stays unchanged.
const expectEyebrowCaps = async (element: Element | null) => {
  if (element === null) throw new Error('column heading text missing');
  const style = getComputedStyle(element);
  await expect(style.textTransform).toBe('uppercase');
  await expect(Number.parseFloat(style.fontWeight)).toBeGreaterThanOrEqual(600);
  // `--tracking-eyebrow` is an em value: resolve it against this element's
  // own font size the way the browser resolves the computed letter-spacing,
  // rather than comparing an unresolved token string to a resolved px one.
  const trackingEm = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--tracking-eyebrow'));
  const expectedPx = trackingEm * Number.parseFloat(style.fontSize);
  await expect(Math.abs(Number.parseFloat(style.letterSpacing) - expectedPx)).toBeLessThan(0.1);
};

/** sRGB alpha of any CSS colour the browser understands, via a 1px canvas. */
function alphaOf(css: string): number {
  const canvas = document.createElement('canvas');
  canvas.width = 1;
  canvas.height = 1;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (context === null) throw new Error('no 2d context');
  context.fillStyle = css;
  context.fillRect(0, 0, 1, 1);
  return (context.getImageData(0, 0, 1, 1).data[3] ?? 0) / 255;
}

// Sticky key column (desktop and phone alike):
// opaque behind every scrolled environment column, for the header, a group
// row and an ordinary key row; and for the group row specifically, which has
// no cells of its own to protect, the label itself (chevron, folder name,
// count, problem badge, "+ Key") stays over the left edge instead of
// scrolling away with the rest of the row. The tinted right border marks a
// KEY row's own edge, and only at the phone breakpoint: the header and group
// rows still carry the full-width key column there, not an edge to mark.
const expectStickyKeyColumn = async (root: HTMLElement) => {
  const scroller = root.querySelector<HTMLElement>('.matrix__scroll');
  const headerCell = root.querySelector<HTMLElement>('.matrix__table thead th:first-child');
  const groupCell = root.querySelector<HTMLElement>('.matrix__table tbody > .matrix__group-row th');
  const keyCell = root.querySelector<HTMLElement>('.matrix__table tbody > .matrix__key-row > th');
  const groupLabel = root.querySelector<HTMLElement>('.matrix__group-row-inner');
  if (scroller === null || headerCell === null || groupCell === null || keyCell === null || groupLabel === null) {
    throw new Error('sticky key column cells missing');
  }
  scroller.scrollLeft = 200;
  for (const cell of [headerCell, groupCell, keyCell]) {
    await expect(alphaOf(getComputedStyle(cell).backgroundColor)).toBe(1);
  }
  await expect(px(getComputedStyle(keyCell).borderRightWidth)).toBeGreaterThan(0);
  await expect(getComputedStyle(keyCell).borderRightStyle).toBe('solid');
  await expect(px(getComputedStyle(headerCell).borderRightWidth)).toBe(0);
  await expect(px(getComputedStyle(groupCell).borderRightWidth)).toBe(0);
  // Nothing scrolled is visible left of or behind the sticky cells: each
  // one's own left edge sits flush with the scroll container's and covers
  // from 0, in the header, a group row and a key row alike.
  const scrollerLeft = scroller.getBoundingClientRect().left;
  for (const cell of [headerCell, groupCell, keyCell, groupLabel]) {
    await expect(Math.abs(cell.getBoundingClientRect().left - scrollerLeft)).toBeLessThanOrEqual(1);
  }
  scroller.scrollLeft = 0;
};

const meta = {
  title: 'Pages/Matrix',
  id: 'routes-matrix',
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
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByRole('heading', { name: 'Environment matrix' })).toBeVisible();
    // A row deep in the list: the virtualiser is rendering the whole catalogue.
    await expect(canvas.getByRole('link', { name: 'Declaration of SENTRY_DSN, secret' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^PUBLIC_BASE_URL in staging: problem, required · absent/ })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /^LOG_LEVEL in development: debug, unpublished draft set/ })).toBeVisible();
    await expect(canvas.getByRole('button', { name: /3 unpublished edits · Publish drafts/ })).toBeVisible();
    await expect(canvas.getAllByText('PROTECTED').length).toBeGreaterThan(0);
    await expectSectionsAndGlyphs(canvasElement);
    // The tinted right border on the sticky key column is a phone-only,
    // key-row-only mark; desktop keeps the column borderless, and the
    // header's key icon shows beside the word rather than replacing it.
    const stickyKeyCell = canvasElement.querySelector<HTMLElement>('.matrix__table tbody > .matrix__key-row > th');
    if (stickyKeyCell === null) throw new Error('sticky key cell missing');
    await expect(px(getComputedStyle(stickyKeyCell).borderRightWidth)).toBe(0);
    const headingIcon = canvasElement.querySelector<SVGElement>('.matrix__key-heading .glyph[data-glyph="key"]');
    await expect(headingIcon?.getBoundingClientRect().width).toBeGreaterThan(0);
    // r1 is a first publish, not a change: neither mark nor words; r2 keeps both.
    const firstPublish = canvas.getByRole('button', { name: /^LOG_LEVEL in staging:/ });
    await expect(firstPublish).not.toHaveAccessibleName(/changed in r/);
    await expect(firstPublish.querySelector('.matrix-cell__delta')).toBeNull();
    const changed = canvas.getByRole('button', { name: /in staging: .*changed in r2/ });
    await expect(changed.querySelector('.matrix-cell__delta')).not.toBeNull();
    // Default history opener: a compact quiet button, never underlined, clear of the name.
    const history = canvas.getByRole('link', { name: 'rev 41 · history' });
    await expect(history).toHaveClass('btn');
    await expect(getComputedStyle(history).textDecorationLine).toBe('none');
    await expect(px(getComputedStyle(history).marginLeft)).toBeGreaterThan(0);
    // The envs picker is sized like the history button beside it.
    const picker = canvasElement.querySelector<HTMLElement>('.matrix__environment-picker > summary');
    await expect(picker?.getBoundingClientRect().height).toBe(history.getBoundingClientRect().height);
    // Column header titles read as eyebrows, the environment name and "Key"
    // alike; the underlying text is unchanged (e.g. the key heading's
    // accessible name is still "Key").
    await expectEyebrowCaps(canvasElement.querySelector('.matrix__table thead th:not(:first-child, .matrix__filler) span:first-child'));
    await expectEyebrowCaps(canvasElement.querySelector('.matrix__key-heading > span'));
    // The envs picker label takes the same eyebrow treatment as the column
    // titles beside it; the source string the
    // picker builds (`envs 3/3`) is unchanged, only its rendered case.
    await expectEyebrowCaps(picker);
    await expect(picker?.textContent?.trim().startsWith('envs')).toBe(true);
    // The table spans the page; each environment column follows its longest
    // content (`--chars`, counted from the data since rows are virtualised),
    // and every pencil ends at its column's end.
    const { chars } = await expectColumnLayout(canvasElement);
    const keyRows = [...canvasElement.querySelectorAll('.matrix__key-row')];
    // One shared inset: every pencil, in every column, sits the same distance
    // from its cell's end.
    const insets = new Set(
      keyRows.flatMap((row) =>
        [...row.querySelectorAll('td')].flatMap((cell) => {
          const edit = cell.querySelector('.matrix-cell__edit');
          return edit === null ? [] : [Math.round(cell.getBoundingClientRect().right - edit.getBoundingClientRect().right)];
        }),
      ),
    );
    await expect(insets.size).toBe(1);
    for (const column of [0, 1, 2]) {
      const values = keyRows.map((row) => row.querySelectorAll('td')[column]?.querySelector('.matrix-cell__value')?.textContent ?? '');
      for (const value of values) await expect(chars[column]).toBeGreaterThanOrEqual(value.trim().length);
    }
    // The pencil is muted ink at rest, not the value's colour. Desktop
    // keeps it visible (the phone breakpoint drops it; see
    // PhoneCompactKeyColumn), at the column's own end, asserted above.
    const pencil = canvasElement.querySelector('.matrix-cell__edit');
    const value = canvasElement.querySelector('.matrix-cell__value');
    if (pencil === null || value === null) throw new Error('matrix cell missing');
    await expect(pencil).toBeVisible();
    await expect(getComputedStyle(pencil).color).not.toBe(getComputedStyle(value).color);
    await expect(getComputedStyle(pencil).color).toBe(tokenColor('color-mix(in oklch, var(--tx-faint), var(--bg) 30%)'));
    // Keyboard focus on the cell lifts the pencil to secondary ink.
    const focusCell = pencil.closest<HTMLElement>('.matrix-cell');
    if (focusCell === null) throw new Error('pencil cell missing');
    focusCell.focus();
    await expect(focusCell).toHaveFocus();
    await expect(focusCell.matches(':focus-visible')).toBe(true);
    await expect(getComputedStyle(pencil).color).toBe(tokenColor('--tx-dim'));
    focusCell.blur();
    // A problem cell carries its own mark instead of a pencil.
    const problem = canvas.getByRole('button', { name: /^PUBLIC_BASE_URL in staging: problem/ });
    await expect(problem.querySelector('.matrix-cell__edit')).toBeNull();
  },
};

// The sticky key column at a laptop width (760px, still the desktop layout):
// three environments leave it less than its full 236px, so it compacts to its
// 160px floor and a long name ellipsises, while the row title and the link's
// accessible name keep the full name. The environment columns give up their
// spare width (the filler goes to 0) and the table scrolls in its own well.
export const CompactKeyColumn: Story = {
  decorators: [(Story) => <div style={{ width: 760 }}><Story /></div>],
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByRole('heading', { name: 'Environment matrix' });
    const name = keys[13]?.name ?? '';
    const link = canvas.getByRole('link', { name: `Declaration of ${name}` });
    const keyCell = link.closest('th');
    const heading = canvasElement.querySelector<HTMLElement>('.matrix__table thead th:first-child');
    if (keyCell === null || heading === null) throw new Error('key column missing');
    await expect(Math.abs(heading.getBoundingClientRect().width - 160)).toBeLessThanOrEqual(1);
    await expect(Math.abs(keyCell.getBoundingClientRect().width - 160)).toBeLessThanOrEqual(1);
    await expect(getComputedStyle(link).textOverflow).toBe('ellipsis');
    await expect(link.scrollWidth).toBeGreaterThan(link.clientWidth);
    await expect(keyCell).toHaveAttribute('title', name);
    // The envs picker still fits beside "Key" at the floor.
    await expect(heading.querySelector('.matrix__environment-picker')).not.toBeNull();
    // Short of room: the table scrolls, the filler has nothing left, and a
    // value ellipsises rather than widening its column.
    const { overflows, truncated, fillerWidth } = await expectColumnLayout(canvasElement);
    await expect(overflows).toBe(true);
    await expect(fillerWidth).toBeLessThanOrEqual(1);
    await expect(truncated.length).toBeGreaterThan(0);
    // Desktop still: pencils stay.
    await expect(canvasElement.querySelector('.matrix-cell__edit')).toBeVisible();
  },
};

// A collapsed group keeps its row and lists its keys, comma-separated, in place
// of the rows it hides; opening it brings them back.
export const CollapsedGroup: Story = {
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByRole('link', { name: 'Declaration of PAYMENT_PROVIDER' });
    const toggle = [...canvasElement.querySelectorAll<HTMLElement>('.matrix__group-toggle')]
      .find((candidate) => candidate.textContent?.includes('payments') === true);
    if (toggle === undefined) throw new Error('payments group missing');
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(toggle.querySelector('.matrix__group-summary')).toBeNull();
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    const summary = toggle.querySelector('.matrix__group-summary');
    if (summary === null) throw new Error('collapsed summary missing');
    await expect(summary).toBeVisible();
    const members = keys.filter((record) => record.folder_path === 'payments').map((record) => record.name);
    await expect((summary.textContent ?? '').split(', ').sort()).toEqual([...members].sort());
    await expect(canvas.queryByRole('link', { name: 'Declaration of PAYMENT_PROVIDER' })).toBeNull();
    // Collapse is instant: the chevron turns, nothing animates a box.
    const chevron = toggle.querySelector('.matrix__group-chevron');
    if (chevron === null) throw new Error('group chevron missing');
    await expect(getComputedStyle(chevron).transform).not.toBe('none');
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(await canvas.findByRole('link', { name: 'Declaration of PAYMENT_PROVIDER' })).toBeVisible();
  },
};

// A wide page (1920px): every environment column is exactly its content width
// and the filler column takes the spare width; rows still stripe across it.
export const WideFillerColumn: Story = {
  decorators: [(Story) => <div style={{ width: 1920 }}><Story /></div>],
  play: async ({ canvas, canvasElement }) => {
    await canvas.findByRole('heading', { name: 'Environment matrix' });
    // With room, no value is cut, the table does not scroll, and the filler
    // takes the spare width rather than the environment columns.
    const { values, truncated, overflows, fillerWidth } = await expectColumnLayout(canvasElement);
    await expect(values.length).toBeGreaterThan(0);
    await expect(truncated).toEqual([]);
    await expect(overflows).toBe(false);
    await expect(fillerWidth).toBeGreaterThan(0);
    // With room to spare the key column keeps its full 236px.
    const keyHeading = canvasElement.querySelector<HTMLElement>('.matrix__table thead th:first-child');
    await expect(Math.abs((keyHeading?.getBoundingClientRect().width ?? 0) - 236)).toBeLessThanOrEqual(1);
    // The filler is not a grid column for assistive tech.
    const filler = canvasElement.querySelector('.matrix__table thead .matrix__filler');
    await expect(canvas.getAllByRole('columnheader')).not.toContain(filler);
    const altRow = canvasElement.querySelector('.matrix__key-row--alt');
    const altFiller = altRow?.querySelector('.matrix__filler');
    if (altRow === null || altFiller == null) throw new Error('striped row missing');
    await expect(getComputedStyle(altFiller).backgroundColor).toBe(getComputedStyle(altRow.querySelectorAll('td')[0] as Element).backgroundColor);
  },
};

// Group rows (N17): the add action keeps its group in the accessible name, a
// problem count carries no leading "!", and a linked key stays one line with
// its link glyph after the name. Collapsed (CollapsedGroup), the add action
// goes with the rows.
export const GroupRows: Story = {
  play: async ({ canvas, canvasElement }) => {
    const add = await canvas.findByRole('button', { name: 'Add key to payments' });
    await expect(add.querySelector('.glyph[data-glyph="plus"]')).not.toBeNull();
    const badges = [...canvasElement.querySelectorAll('.matrix__group-row .badge')];
    await expect(badges.length).toBeGreaterThan(0);
    for (const badge of badges) await expect(badge.textContent?.trim()).toMatch(/^\d+ problems?$/);

    const linkedRow = canvas.getByRole('link', { name: 'Declaration of STRIPE_SECRET_KEY, secret' }).closest('tr');
    const plainRow = canvas.getByRole('link', { name: 'Declaration of STRIPE_PUBLISHABLE_KEY' }).closest('tr');
    if (linkedRow === null || plainRow === null) throw new Error('payments rows missing');
    await expect(linkedRow.querySelector('.matrix__linked-keys .glyph[data-glyph="link"]')).not.toBeNull();
    await expect(linkedRow.querySelector('.matrix__linked-keys')).toHaveTextContent('Linked keys: stripe');
    await expect(Math.round(linkedRow.getBoundingClientRect().height)).toBe(Math.round(plainRow.getBoundingClientRect().height));

    const toggle = add.closest('tr')?.querySelector<HTMLElement>('.matrix__group-toggle');
    if (toggle == null) throw new Error('payments toggle missing');
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(canvas.queryByRole('button', { name: 'Add key to payments' })).toBeNull();
    await userEvent.click(toggle);
    await expect(await canvas.findByRole('button', { name: 'Add key to payments' })).toBeVisible();
  },
};

// The cell marks are glyphs, not characters: changed is the history glyph,
// an own draft the own-draft glyph, an invalid draft the alert glyph. A
// problem cell has no pencil (its own mark says what to do). The legend
// names the same glyphs.
export const CellMarks: Story = {
  play: async ({ canvas, canvasElement }) => {
    const changed = await canvas.findByRole('button', { name: /^FEATURE_FLAGS in development:.*changed in r7/ });
    await expect(changed.querySelector('.matrix-cell__delta .glyph')).toHaveAttribute('data-glyph', 'history');
    const ownDraft = canvas.getByRole('button', { name: /^LOG_LEVEL in development:.*unpublished draft set/ });
    await expect(ownDraft.querySelector('.matrix-cell__draft .glyph')).toHaveAttribute('data-glyph', 'own-draft');
    const otherDraft = canvas.getByRole('button', { name: /^STRIPE_PUBLISHABLE_KEY in development:/ });
    await expect(otherDraft.querySelector('.matrix-cell__other .glyph')).toHaveAttribute('data-glyph', 'draft');
    const invalid = canvas.getByRole('button', { name: /^DATABASE_POOL_SIZE in staging:/ });
    await expect(invalid.querySelector('.matrix-cell__error .glyph')).toHaveAttribute('data-glyph', 'alert');
    // An invalid DRAFT still has a pencil: it is your own unpublished edit and
    // the fix is to open and correct it. Only a true problem cell (below) has
    // none, because there the mark alone says what to do.
    await expect(invalid.querySelector('.matrix-cell__edit')).not.toBeNull();
    const required = canvas.getByRole('button', { name: /^PUBLIC_BASE_URL in staging: problem, required · absent/ });
    await expect(required.textContent).not.toContain('!');
    await expect(required.querySelector('.matrix-cell__edit')).toBeNull();

    const legend = canvasElement.querySelector<HTMLElement>('.matrix__legend-toggle');
    if (legend === null) throw new Error('legend toggle missing');
    await expect(legend).toHaveAttribute('aria-label', 'what the cells mean');
    await expect(legend.querySelector('.glyph')).toHaveAttribute('data-glyph', 'question');
    await userEvent.click(legend);
    for (const name of ['alert', 'changed', 'draft dot', "another editor's draft"]) {
      await expect(canvas.getByRole('img', { name })).toBeVisible();
    }
  },
};

// A phone: the sticky key column drops to the touch target, one
// glyph per key; the name moves into a tap/hover popover anchored to the icon,
// and the envs picker moves out of the now icon-only header into the head row.
// The full name stays the link's accessible name throughout.
export const PhoneCompactKeyColumn: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    // The headless browser running this story reports `(hover: hover)` as
    // matched regardless of the phone viewport (it is not a real touch
    // device), which would let MatrixKeyLink's hover-preview fire on the
    // click's own focus step before the tap logic below ever sees the
    // event, which it would not on an actual phone, where that query reports
    // no hover capability. Make it answer like the phone this story names,
    // for this interaction only.
    const realMatchMedia = window.matchMedia.bind(window);
    window.matchMedia = (query: string) =>
      query.includes('hover') || query.includes('pointer: fine')
        ? ({ matches: false, media: query, onchange: null, addListener: () => undefined, removeListener: () => undefined, addEventListener: () => undefined, removeEventListener: () => undefined, dispatchEvent: () => false } as MediaQueryList)
        : realMatchMedia(query);

    try {
      const name = 'Declaration of DATABASE_READ_REPLICA_URL, secret';
      const link = await canvas.findByRole('link', { name });
      const heading = canvasElement.querySelector<HTMLElement>('.matrix__table thead th:first-child');
      if (heading === null) throw new Error('key heading missing');
      await expect(heading.getBoundingClientRect().width).toBeLessThanOrEqual(48);
      // The envs picker left the icon-only header for the head row above the table.
      await expect(heading.querySelector('.matrix__environment-picker')).toBeNull();
      await expect(canvasElement.querySelector('.matrix__head .matrix__environment-picker')).not.toBeNull();
      // The lock icon is visible; the name is not, until tapped.
      const lock = link.querySelector<SVGElement>('.glyph');
      await expect(lock?.getBoundingClientRect().width).toBeGreaterThan(0);
      const nameSpan = link.querySelector('.matrix__key-name');
      if (nameSpan === null) throw new Error('key name span missing');
      await expect(nameSpan.getBoundingClientRect().width).toBeLessThanOrEqual(1);
      const tip = link.parentElement?.querySelector('.matrix__key-tip');
      if (tip === null || tip === undefined) throw new Error('key tip missing');
      await expect(tip).not.toBeVisible();
      // A tap shows the name, in the popover, not by growing the column.
      await userEvent.click(link);
      await waitFor(() => expect(tip).toBeVisible());
      await expect(tip).toHaveTextContent('DATABASE_READ_REPLICA_URL');
      await expect(heading.getBoundingClientRect().width).toBeLessThanOrEqual(48);
      // Unaffected by the tap: the accessible name a screen reader or keyboard
      // user acts on to open the declaration detail, same as at desktop.
      await expect(link).toHaveAccessibleName(name);
      await expectSectionsAndGlyphs(canvasElement);
      await expectStickyKeyColumn(canvasElement);
      // The key-column header stays non-blank: its Glyph is visible even
      // though the word beside it is clipped to the accessible name only.
      const headingIcon = heading.querySelector<SVGElement>('.glyph[data-glyph="key"]');
      if (headingIcon === null) throw new Error('key heading icon missing');
      await expect(headingIcon.getBoundingClientRect().width).toBeGreaterThan(0);
      await expect(heading).toHaveAccessibleName('Key');
      // Caps and bold, same as desktop, even while visually clipped to the
      // accessible name only.
      await expectEyebrowCaps(heading.querySelector('.matrix__key-heading > span'));
      // One line of icons: the header's key glyph is the same size as a key
      // row's own (both `--matrix-icon`) and shares its x-centre, and that
      // centre is also the cell's own (the column's left edge is not left
      // dangling off to one side).
      const rowGlyph = canvasElement.querySelector<SVGElement>('.matrix__key .glyph');
      if (rowGlyph === null) throw new Error('key row glyph missing');
      const headingIconBox = headingIcon.getBoundingClientRect();
      const rowGlyphBox = rowGlyph.getBoundingClientRect();
      await expect(headingIconBox.width).toBeCloseTo(rowGlyphBox.width, 0);
      await expect(Math.abs(headingIconBox.left + headingIconBox.width / 2 - (rowGlyphBox.left + rowGlyphBox.width / 2))).toBeLessThanOrEqual(1);
      const cellCenter = heading.getBoundingClientRect().left + heading.getBoundingClientRect().width / 2;
      await expect(Math.abs(headingIconBox.left + headingIconBox.width / 2 - cellCenter)).toBeLessThanOrEqual(1);
      // The column header's `rev N · history` opener keeps clear space above
      // and below it inside the header cell: at this width the button holds
      // the full touch control height, so the row has to be taller than it.
      const historyLink = canvasElement.querySelector<HTMLElement>('.matrix__history-link');
      const envHeaderCell = historyLink?.closest('th') ?? null;
      if (historyLink === null || envHeaderCell === null) throw new Error('env header/history link missing');
      const cellRect = envHeaderCell.getBoundingClientRect();
      const linkRect = historyLink.getBoundingClientRect();
      await expect(linkRect.top - cellRect.top).toBeGreaterThanOrEqual(4);
      await expect(cellRect.bottom - linkRect.bottom).toBeGreaterThanOrEqual(4);
      await expectEyebrowCaps(envHeaderCell.querySelector('span:first-child'));
      // Icon-only at this width: a square button showing only the history
      // glyph, its full "rev N · history" meaning still the accessible name
      // (the label is clipped out of view, not removed from the a11y tree)
      // and reachable via `title`.
      const historyText = historyLink.textContent?.trim() ?? '';
      await expect(historyText).toMatch(/^rev \d+ · history$/);
      const historyGlyph = historyLink.querySelector<SVGElement>('.glyph');
      await expect(historyGlyph?.getBoundingClientRect().width).toBeGreaterThan(0);
      const historyLabel = historyLink.querySelector('.btn__label');
      await expect(historyLabel?.getBoundingClientRect().width).toBeLessThanOrEqual(1);
      await expect(Math.round(linkRect.width)).toBe(Math.round(linkRect.height));
      await expect(historyLink).toHaveAccessibleName(historyText);
      await expect(historyLink).toHaveAttribute('title', historyText);
      // The pencil in every value cell costs a tap target's worth of column
      // width; at this breakpoint app.css drops it so columns run narrower,
      // but the cell stays the whole button (#pencil-phone), so tapping it
      // still opens the row editor and the history/draft marks stay put.
      const valueCell = canvas.getByRole('button', { name: /^DATABASE_READ_REPLICA_URL in development:/ });
      const cellPencil = valueCell.querySelector('.matrix-cell__edit');
      if (cellPencil === null) throw new Error('matrix cell pencil missing');
      await expect(cellPencil).not.toBeVisible();
      // No pencils on a phone, in any rendered cell.
      const pencils = [...canvasElement.querySelectorAll('.matrix-cell__edit')];
      await expect(pencils.length).toBeGreaterThan(0);
      for (const pencil of pencils) await expect(pencil).not.toBeVisible();
      // The table scrolls in its own well; the page itself never scrolls sideways.
      await expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(document.documentElement.clientWidth);
      await userEvent.click(valueCell);
      await expect(await canvas.findByRole('heading', { name: 'DATABASE_READ_REPLICA_URL' })).toBeVisible();
      await userEvent.click(canvas.getByRole('button', { name: 'Close' }));
      await waitFor(() => expect(canvas.queryByRole('heading', { name: 'DATABASE_READ_REPLICA_URL' })).toBeNull());
      // Escape closes the name; a tap shows it again.
      await userEvent.keyboard('{Escape}');
      await waitFor(() => expect(tip).not.toBeVisible());
      await userEvent.click(link);
      await waitFor(() => expect(tip).toBeVisible());
      // The second tap, with the name shown, opens the declaration detail: the
      // matrix route unmounts (this story mounts only the matrix route). The
      // real-touch proof (an auto popover light-dismissing between the tap's
      // pointer events and its click) is in e2e/matrix.spec.ts.
      await userEvent.click(link);
      await waitFor(() => expect(link).not.toBeInTheDocument());
    } finally {
      window.matchMedia = realMatchMedia;
    }
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
    await expect(canvas.queryByRole('button', { name: 'New key' })).not.toBeInTheDocument();
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

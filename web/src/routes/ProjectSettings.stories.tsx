import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zDefinitionsSettings,
  zEnvironmentList,
  zProject,
  zProjectCapacity,
  zProjectRetentionPolicy,
  zRetentionPolicy,
} from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import { DemoSurface } from '../demo/DemoSurface.tsx';
import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { ProjectSettings } from './ProjectSettings.tsx';

import { expectLeftAlignedPage, expectStackedRow, phone } from '../../.storybook/layout.ts';
import { openTab } from '../../.storybook/openTab.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The project-settings surface reads `useParams({ org, project })` and fires
// five load-time GETs on mount, each independent of the granted
// capabilities (the Policy/Danger panels are gated on the project body, but the
// retention and definitions queries run regardless): the project itself,
// its environments, the org retention cap, the project retention policy, and
// the definitions source. Authorized policy holders additionally read capacity.
// Each needs a `responses` row or the fetch stub 404s.
// Its mutations (rename, delete, DEK rotation, definitions/environment writes)
// have no story route and would 404 or navigate away, so the plays only assert
// read-only state. The screen calls `useAuth` through those mutation hooks, so
// the harness runs it with `auth: true`; the granted capabilities come from the
// project body, not the identity, so the default operator whoami is left as is.
// The route pattern is `surfaceById('project-settings').path` in
// `src/app/navigation.ts`. See .storybook/withApp.tsx.
const PATH = `/orgs/${ORG}/projects/${PRJ}/settings`;
const ROUTE = '/orgs/:org/projects/:project/settings';

const PROJECT_URL = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const ENVIRONMENTS_URL = `${PROJECT_URL}/environments`;
const PROJECT_RETENTION_URL = `${PROJECT_URL}/retention`;
const DEFINITIONS_URL = `${PROJECT_URL}/definitions/settings`;
const ORG_RETENTION_URL = `/api/v1/orgs/${ORG}/retention`;

const orgRetention = {
  mode: 'keep-if-either',
  max_age_seconds: 7_776_000,
  last_revisions: 10,
} satisfies z.input<typeof zRetentionPolicy>;
const projectRetention = {
  inherited: true,
  mode: 'keep-if-either',
  max_age_seconds: 7_776_000,
  last_revisions: 10,
} satisfies z.input<typeof zProjectRetentionPolicy>;
const definitions = { definitions_source: 'db' } satisfies z.input<typeof zDefinitionsSettings>;
const capacity = { key_limit_override: null, instance_default: 1000,
  instance_maximum: 10000, effective_limit: 1000, key_count: 0, environment_count: 0,
  revision: 0, inherited: true, capped: false } satisfies z.input<typeof zProjectCapacity>;

const noEnvironments = { count: 0, items: [] } satisfies z.input<typeof zEnvironmentList>;
const environment: z.input<typeof zEnvironmentList>['items'][number] = {
  id: PROD,
  org_id: ORG,
  project_id: PRJ,
  name: 'production',
  display_order: 0,
  created_at: '2026-01-01T00:00:00Z',
};
const twoEnvironments = {
  count: 2,
  items: [
    environment,
    { ...environment, id: STAGING, name: 'staging', display_order: 1 },
  ],
} satisfies z.input<typeof zEnvironmentList>;

const administrableProject = {
  id: PRJ,
  org_id: ORG,
  name: 'billing',
  created_at: '2026-01-01T00:00:00Z',
  can_manage_policy: true,
  can_delete: true,
} satisfies z.input<typeof zProject>;
const memberProject = {
  ...administrableProject,
  can_manage_policy: false,
  can_delete: false,
} satisfies z.input<typeof zProject>;

// Shared responses, including the capacity read made only for policy holders,
// over the empty environment list unless a story replaces it.
const commonResponses = [
  { url: ENVIRONMENTS_URL, body: noEnvironments },
  { url: ORG_RETENTION_URL, body: orgRetention },
  { url: PROJECT_RETENTION_URL, body: projectRetention },
  { url: DEFINITIONS_URL, body: definitions },
  { url: `${PROJECT_URL}/capacity`, body: capacity },
];

const meta = {
  title: 'Pages/ProjectSettings',
  id: 'routes-projectsettings',
  component: ProjectSettings,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    app: {
      auth: true,
      path: PATH,
      routePath: ROUTE,
      // Full capability, no environments yet: the honest state of a fresh
      // project whose operator may still manage policy and delete it.
      responses: [{ url: PROJECT_URL, body: administrableProject }, ...commonResponses],
    },
  },
} satisfies Meta<typeof ProjectSettings>;

export default meta;
type Story = StoryObj<typeof meta>;


// A project that grants both `can_manage_policy` and `can_delete` renders every
// panel: Identity, Metadata, Environments, Policy (definitions + retention) and
// the Danger zone.
export const Administrable: Story = {
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /project settings\s*·?\s*billing/i, level: 1 }),
    ).toBeVisible();
    await expect(canvas.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
      'Identity', 'Metadata', 'Environments', 'Policy', 'Access', 'Developer credentials',
      'Keys & crypto', 'Danger zone',
    ]);
    await openTab(canvas, 'Metadata');
    await expect(canvas.getByLabelText('Name')).toBeVisible();
    // The Policy tab exists only because the project grants can_manage_policy.
    await openTab(canvas, 'Policy');
    await expect(canvas.getByRole('heading', { name: 'Policy', level: 2 })).toBeVisible();
    await expect(canvas.getByLabelText('Definitions source')).toBeVisible();
    await expect(canvas.getByLabelText('Retention mode')).toBeVisible();
    // The Danger zone exists only because the project grants can_delete.
    await openTab(canvas, 'Danger zone');
    await expect(canvas.getByRole('heading', { name: 'Danger zone', level: 2 })).toBeVisible();
    // The page column starts at the well's left edge.
    await expectLeftAlignedPage(canvas.getByRole('heading', { level: 1 }));
    // The sections are tabs with exactly one selected.
    await expect(canvas.getAllByRole('tab', { selected: true })).toHaveLength(1);
  },
};

// A settings row with two or more buttons puts them on one row under
// its copy. The definitions bundle's three buttons, the protected-environment
// toggles and each environment's Move up / Move down.
export const ActionRows: Story = {
  parameters: { app: { auth: true, path: `${PATH}?tab=policy`, routePath: ROUTE, responses: [
    { url: PROJECT_URL, body: administrableProject },
    { url: ENVIRONMENTS_URL, body: twoEnvironments },
    { url: ORG_RETENTION_URL, body: orgRetention },
    { url: PROJECT_RETENTION_URL, body: projectRetention },
    { url: DEFINITIONS_URL, body: definitions },
    { url: `${PROJECT_URL}/capacity`, body: capacity },
  ] } },
  play: async ({ canvas }) => {
    const bundle = (await canvas.findByText('Definitions bundle')).closest<HTMLElement>('.settings-row');
    if (bundle === null) throw new Error('bundle row missing');
    await expectStackedRow(bundle, 3);
    const protectedRow = canvas.getByText('Protected environments').closest<HTMLElement>('.settings-row');
    if (protectedRow === null) throw new Error('protected row missing');
    await expectStackedRow(protectedRow, 2);
    await openTab(canvas, 'Environments');
    const manage = (await canvas.findByText('Manage staging')).closest('summary');
    if (manage === null) throw new Error('summary missing');
    await userEvent.click(manage);
    const order = within(manage.closest('details') as HTMLElement).getByText('Order').closest<HTMLElement>('.settings-row');
    if (order === null || order === undefined) throw new Error('order row missing');
    await expectStackedRow(order, 2);
  },
};

/** Capacity uses the same title/detail/action-row composition as sibling policy settings. */
export const CapacityPolicy: Story = {
  parameters: { app: { auth: true, path: `${PATH}?tab=policy`, routePath: ROUTE,
    responses: [{ url: PROJECT_URL, body: administrableProject }, ...commonResponses] } },
  play: async ({ canvas, canvasElement }) => {
    await expect(await canvas.findByText('0 keys used / 1,000 key limit')).toBeVisible();
    const row = canvas.getByText('Key capacity').closest<HTMLElement>('.settings-row');
    if (row === null) throw new Error('capacity settings row missing');
    await expectStackedRow(row, 2);
    const editBox = canvas.getByRole('button', { name: 'Edit key capacity' }).getBoundingClientRect();
    const refreshBox = canvas.getByRole('button', { name: 'Refresh key capacity' }).getBoundingClientRect();
    await expect(editBox.height).toBe(refreshBox.height);
    await expect(editBox.top).toBe(refreshBox.top);
    await expect(canvas.getByRole('heading', { name: 'Policy', level: 2 })).toBeVisible();
    await expect(canvas.getByLabelText('Definitions source')).toBeVisible();
    await expect(canvas.getByText('Revision retention')).toBeVisible();
    await expect(canvasElement.scrollWidth).toBeLessThanOrEqual(canvasElement.clientWidth);
    await expectLeftAlignedPage(canvas.getByRole('heading', { level: 1 }));
  },
};
export const CapacityPolicyPhone: Story = {
  ...CapacityPolicy,
  globals: { viewport: { value: 'phone', isRotated: false } },
};

// At a phone width a row's 2+ buttons become an equal-width two-column grid.
export const ActionRowsPhone: Story = {
  parameters: ActionRows.parameters,
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    const bundle = (await canvas.findByText('Definitions bundle')).closest<HTMLElement>('.settings-row');
    if (bundle === null) throw new Error('bundle row missing');
    await expectStackedRow(bundle, 3);
    if (phone()) {
      await expect(getComputedStyle(bundle.querySelector(':scope > .actions')!).gridTemplateColumns.split(' ')).toHaveLength(2);
    }
    await expect(canvasElement.scrollWidth).toBeLessThanOrEqual(canvasElement.clientWidth);
  },
};

// An old `#project-retention` link opens the Policy tab that now holds it.
export const LegacyAnchorOpensTab: Story = {
  parameters: { app: { auth: true, path: `${PATH}#project-retention`, routePath: ROUTE, responses: [
    { url: PROJECT_URL, body: administrableProject },
    ...commonResponses,
  ] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('tab', { name: 'Policy' })).toHaveAttribute('aria-selected', 'true');
    await expect(canvas.getByRole('heading', { name: 'Policy', level: 2 })).toBeVisible();
  },
};

// The environments read failed: the Environments tab says so even to a
// member without the policy capability.
export const EnvironmentsReadFailed: Story = {
  parameters: { app: { auth: true, path: `${PATH}?tab=environments`, routePath: ROUTE, responses: [
    { url: PROJECT_URL, body: memberProject },
    { url: ENVIRONMENTS_URL, status: 500, body: { error: 'boom' } },
    ...commonResponses.slice(1),
  ] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('tab', { name: 'Environments' })).toHaveAttribute('aria-selected', 'true');
    await expect(await canvas.findByText("This project's environments could not be read.")).toBeVisible();
    await expect(canvas.queryByRole('tab', { name: 'Policy' })).toBeNull();
  },
};

// The same failure for a project that grants can_manage_policy: the
// Environments tab says so too, not only the Policy tab.
export const EnvironmentsReadFailedWithPolicy: Story = {
  parameters: { app: { auth: true, path: `${PATH}?tab=environments`, routePath: ROUTE, responses: [
    { url: PROJECT_URL, body: administrableProject },
    { url: ENVIRONMENTS_URL, status: 500, body: { error: 'boom' } },
    ...commonResponses.slice(1),
  ] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('tab', { name: 'Environments' })).toHaveAttribute('aria-selected', 'true');
    const environments = canvas.getByRole('heading', { name: 'Environments', level: 2 }).closest('section');
    if (environments === null) throw new Error('Environments panel missing');
    await expect(await within(environments).findByText("This project's environments could not be read.")).toBeVisible();
    await openTab(canvas, 'Policy');
    const policy = canvas.getByRole('heading', { name: 'Policy', level: 2 }).closest('section');
    if (policy === null) throw new Error('Policy panel missing');
    await expect(await within(policy).findByText("This project's environments could not be read.")).toBeVisible();
  },
};

// The Danger zone's typed-name gate: Delete project arms only on the exact name.
export const DangerZoneTypedName: Story = {
  parameters: { app: { auth: true, path: `${PATH}?tab=danger`, routePath: ROUTE, responses: [
    { url: PROJECT_URL, body: administrableProject },
    ...commonResponses,
  ] } },
  play: async ({ canvas }) => {
    const input = await canvas.findByLabelText('Delete this project');
    const button = canvas.getByRole('button', { name: 'Delete project' });
    await waitFor(() => expect(input).toBeEnabled());
    await userEvent.type(input, 'bill');
    await expect(button).toBeDisabled();
    await userEvent.type(input, 'ing');
    await expect(button).toBeEnabled();
    await expect(canvas.getByText('The name matches. Delete project is now possible.')).toBeVisible();
  },
};

// The public demo mounts this page read-only via `ReadOnlyContext` ('actions'
// mode, owner decision M7, 2026-10-13), not a disabling `<fieldset>`: its tabs
// stay native buttons, so switching sections works, while the Name field that
// would rename the project is individually disabled.
export const DemoReadOnly: Story = {
  parameters: { app: { auth: true, path: PATH, routePath: ROUTE, responses: [
    { url: PROJECT_URL, body: administrableProject },
    ...commonResponses,
  ] } },
  decorators: [(Story) => <DemoSurface id="project-settings"><Story /></DemoSurface>],
  play: async ({ canvas, canvasElement }) => {
    await waitFor(() => expect(canvas.getByText(/^Read-only sample data\./)).toBeVisible());
    await expect(canvasElement.querySelector('fieldset[disabled]')).toBeNull();
    await expect(canvas.getByLabelText('Name')).toBeDisabled();
    await openTab(canvas, 'Policy');
    await expect(await canvas.findByRole('tab', { name: 'Policy' })).toHaveAttribute('aria-selected', 'true');
  },
};

export const UnlimitedRetentionOverride: Story = {
  parameters: { app: { auth: true, path: PATH, routePath: ROUTE, responses: [
    { url: PROJECT_URL, body: administrableProject },
    { url: ORG_RETENTION_URL, body: { mode: 'unlimited' } satisfies z.input<typeof zRetentionPolicy> },
    { url: PROJECT_RETENTION_URL, body: { inherited: true, mode: 'unlimited' } satisfies z.input<typeof zProjectRetentionPolicy> },
    ...commonResponses,
  ] } },
  play: async ({ canvas }) => {
    await openTab(canvas, 'Policy');
    await expect(await canvas.findByText('inherits the org default: unlimited retention; follows org changes')).toBeVisible();
    await userEvent.selectOptions(canvas.getByLabelText('Retention mode'), 'custom');
    await expect(canvas.getByLabelText('Maximum retention age (seconds)')).toBeVisible();
    await expect(canvas.getByLabelText('Revisions kept per environment')).toBeVisible();
    await expect(canvas.getByRole('alert')).toHaveTextContent('Choose a positive maximum age in seconds and revision count');
  },
};

// A member who holds neither capability: the environment list is populated, but
// the Policy and Danger panels are absent — the screen shows only what the
// project body permits.
export const MemberWithEnvironments: Story = {
  parameters: {
    app: {
      auth: true,
      path: PATH,
      routePath: ROUTE,
      responses: [
        { url: PROJECT_URL, body: memberProject },
        { url: ENVIRONMENTS_URL, body: twoEnvironments },
        { url: ORG_RETENTION_URL, body: orgRetention },
        { url: PROJECT_RETENTION_URL, body: projectRetention },
        { url: DEFINITIONS_URL, body: definitions },
      ],
    },
  },
  play: async ({ canvas }) => {
    await openTab(canvas, 'Environments');
    await expect(await canvas.findByText('Manage production')).toBeVisible();
    await expect(canvas.getByText('Manage staging')).toBeVisible();
    // Each "Manage …" disclosure wears the secondary button skin and opens in place.
    const summary = canvas.getByText('Manage staging').closest('summary');
    if (summary === null) throw new Error('summary missing');
    await expect(summary).toHaveClass('btn');
    await userEvent.click(summary);
    await expect(summary.closest('details')).toHaveAttribute('open');
    // The chevron, not a native marker, carries the open state.
    const chevron = summary.querySelector('.glyph[data-glyph="chevron"]');
    if (chevron === null) throw new Error('chevron missing');
    await expect(getComputedStyle(chevron).transform).not.toBe('none');
    await expect(canvas.queryByRole('tab', { name: 'Policy' })).toBeNull();
    await expect(canvas.queryByRole('tab', { name: 'Danger zone' })).toBeNull();
  },
};

// Load failure on the project read: the page degrades to the deliberately
// ambiguous "could not be read" alert (missing and forbidden are the same
// answer), the state a real screen shows when that call is down.
export const LoadError: Story = {
  parameters: {
    app: {
      auth: true,
      path: PATH,
      routePath: ROUTE,
      responses: [{ url: PROJECT_URL, status: 500, body: { error: 'boom' } }, ...commonResponses],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/this project could not be read/i)).toBeVisible();
  },
};

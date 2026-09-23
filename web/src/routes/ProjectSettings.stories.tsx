import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  zDefinitionsSettings,
  zEnvironmentList,
  zProject,
  zProjectRetentionPolicy,
  zRetentionPolicy,
} from '@hikyo/zod';
import { expect } from 'storybook/test';
import type { z } from 'zod';

import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { ProjectSettings } from './ProjectSettings.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The project-settings surface reads `useParams({ org, project })` and fires
// five load-time GETs on mount, every one independent of the granted
// capabilities (the Policy/Danger panels are gated on the project body, but the
// retention and definitions queries run regardless): the project itself,
// its environments, the org retention cap, the project retention policy, and
// the definitions source. Each needs a `responses` row or the fetch stub 404s.
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

// The four capability-independent rows every story needs, over the empty
// environment list unless a story replaces it.
const commonResponses = [
  { url: ENVIRONMENTS_URL, body: noEnvironments },
  { url: ORG_RETENTION_URL, body: orgRetention },
  { url: PROJECT_RETENTION_URL, body: projectRetention },
  { url: DEFINITIONS_URL, body: definitions },
];

const meta = {
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
      await canvas.findByRole('heading', { name: /project settings · billing/i, level: 1 }),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Name')).toBeVisible();
    // The Policy panel exists only because the project grants can_manage_policy.
    await expect(canvas.getByRole('heading', { name: 'Policy', level: 2 })).toBeVisible();
    await expect(canvas.getByLabelText('Definitions source')).toBeVisible();
    await expect(canvas.getByLabelText('Retention mode')).toBeVisible();
    // The Danger zone exists only because the project grants can_delete.
    await expect(canvas.getByRole('heading', { name: 'Danger zone', level: 2 })).toBeVisible();
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
    await expect(await canvas.findByText('Manage production')).toBeVisible();
    await expect(canvas.getByText('Manage staging')).toBeVisible();
    await expect(canvas.queryByRole('heading', { name: 'Policy', level: 2 })).toBeNull();
    await expect(canvas.queryByRole('heading', { name: 'Danger zone', level: 2 })).toBeNull();
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

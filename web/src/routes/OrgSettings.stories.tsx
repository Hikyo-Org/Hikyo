import type { Meta, StoryObj } from '@storybook/react-vite';
import { zGrantList, zOrg, zProjectList, zProjectRetentionPolicy, zRetentionPolicy } from '@hikyo/zod';
import { expect, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { OrgSettings } from './OrgSettings.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The organisation settings page reads `useParams({ org })` and fires four
// load-time GETs (the org, its retention policy, its grants, its projects), then
// one retention read per project. Its mutations (rename, delete, retention
// save) have no story route, so the plays assert read-only state. The route is
// `surfaceById('org-settings')`.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PATH = `/orgs/${ORG}/settings`;
const ROUTE = '/orgs/:org/settings';

const ORG_URL = `/api/v1/orgs/${ORG}`;
const RETENTION_URL = `${ORG_URL}/retention`;
const GRANTS_URL = `${ORG_URL}/grants`;
const PROJECTS_URL = `${ORG_URL}/projects`;
const projectRetentionUrl = (project: string) => `${PROJECTS_URL}/${project}/retention`;

const org = {
  id: ORG,
  name: 'Acme',
  active: true,
  created_at: '2026-01-01T00:00:00Z',
} satisfies z.infer<typeof zOrg>;

const retention = {
  mode: 'keep-if-either',
  max_age_seconds: 7_776_000,
  last_revisions: 10,
} satisfies z.infer<typeof zRetentionPolicy>;

const grants = {
  count: 2,
  items: [
    {
      id: 'grt_123e4567-e89b-12d3-a456-426614174101',
      principal_id: 'prn_123e4567-e89b-12d3-a456-426614174010',
      principal_name: 'Dana Jacobs',
      capability: 'manage-members',
      scope: { org_id: ORG },
      origins: [{ kind: 'direct', subject: 'prn_123e4567-e89b-12d3-a456-426614174010' }],
      created_at: '2026-02-01T00:00:00Z',
    },
    {
      id: 'grt_123e4567-e89b-12d3-a456-426614174102',
      principal_id: 'prn_123e4567-e89b-12d3-a456-426614174011',
      principal_name: 'Sam Okafor',
      capability: 'read-values',
      scope: { org_id: ORG },
      origins: [{ kind: 'direct', subject: 'prn_123e4567-e89b-12d3-a456-426614174011' }],
      created_at: '2026-03-01T00:00:00Z',
    },
  ],
} satisfies z.infer<typeof zGrantList>;

// One project per branch the retention list renders: inheriting the org
// default, a custom value under the cap, a custom value the org cap clamps,
// and one whose policy read fails.
const INHERITING = 'prj_123e4567-e89b-12d3-a456-426614174201';
const CUSTOM = 'prj_123e4567-e89b-12d3-a456-426614174202';
const CAPPED = 'prj_123e4567-e89b-12d3-a456-426614174203';
const UNREADABLE = 'prj_123e4567-e89b-12d3-a456-426614174204';

const project = (id: string, name: string) => ({ id, org_id: ORG, name, created_at: '2026-01-15T00:00:00Z' });
const projects = {
  count: 4,
  items: [
    project(INHERITING, 'billing'),
    project(CUSTOM, 'checkout'),
    project(CAPPED, 'warehouse-telemetry'),
    project(UNREADABLE, 'legacy-erp'),
  ],
} satisfies z.infer<typeof zProjectList>;

const projectPolicy = (inherited: boolean, revisions: number) =>
  ({ inherited, mode: 'keep-if-either', max_age_seconds: 7_776_000, last_revisions: revisions }) satisfies z.infer<typeof zProjectRetentionPolicy>;

const populated: readonly MockRoute[] = [
  { url: ORG_URL, body: org },
  { url: RETENTION_URL, body: retention },
  { url: GRANTS_URL, body: grants },
  { url: PROJECTS_URL, body: projects },
  { url: projectRetentionUrl(INHERITING), body: projectPolicy(true, 10) },
  { url: projectRetentionUrl(CUSTOM), body: projectPolicy(false, 4) },
  { url: projectRetentionUrl(CAPPED), body: projectPolicy(false, 25) },
  { url: projectRetentionUrl(UNREADABLE), status: 500, body: { code: 'internal' } },
];

const app = (responses: readonly MockRoute[]) => ({ auth: true, path: PATH, routePath: ROUTE, responses });

const meta = {
  component: OrgSettings,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: app(populated) },
} satisfies Meta<typeof OrgSettings>;

export default meta;
type Story = StoryObj<typeof meta>;

// Every read resolved: the name, the grant count, the org default and each
// project's effective bound, including the clamped one and the unreadable one.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /organisation settings · acme/i, level: 1 }),
    ).toBeVisible();
    await expect(canvas.getByLabelText('Name')).toHaveValue('Acme');
    await expect(canvas.getByText('2 org-scoped grant lines')).toBeVisible();
    await expect(canvas.getByLabelText('Org default revisions kept')).toHaveValue(10);
    // Each project's bound arrives with its own retention read, after the list.
    await waitFor(() => expect(canvas.getByRole('link', { name: 'Settings for billing' })).toHaveTextContent('inherits → 10'));
    await waitFor(() => expect(canvas.getByRole('link', { name: 'Settings for checkout' })).toHaveTextContent('custom 4'));
    await waitFor(() => expect(canvas.getByRole('link', { name: 'Settings for warehouse-telemetry' })).toHaveTextContent(/capped to 10 by org/));
    await expect(await canvas.findByText(/this project's retention policy could not be read/i)).toBeVisible();
  },
};

// No projects yet: the org default stands alone with the honest empty line.
export const Empty: Story = {
  parameters: {
    app: app([
      { url: ORG_URL, body: org },
      { url: RETENTION_URL, body: retention },
      { url: GRANTS_URL, body: { count: 0, items: [] } },
      { url: PROJECTS_URL, body: { count: 0, items: [] } },
    ]),
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('This organisation has no projects.')).toBeVisible();
    await expect(canvas.getByText('0 org-scoped grant lines')).toBeVisible();
  },
};

// Nothing settles: the name field is disabled, the panels hold their loading lines.
export const Loading: Story = {
  parameters: {
    app: app([ORG_URL, RETENTION_URL, GRANTS_URL, PROJECTS_URL].map((url) => ({ url, pending: true }))),
  },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByText(/loading the retention policy/i)).toBeVisible());
    await expect(canvas.getByLabelText('Name')).toBeDisabled();
    await expect(canvas.getByText(/loading project retention policies/i)).toBeVisible();
  },
};

// The org read failed: the deliberately ambiguous refusal (missing and
// forbidden are the same answer), and the dependent panels degrade honestly.
export const Failed: Story = {
  parameters: {
    app: app([ORG_URL, RETENTION_URL, GRANTS_URL, PROJECTS_URL].map((url) => ({ url, status: 500, body: { code: 'internal' } }))),
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/this organisation could not be read/i)).toBeVisible();
    await expect(canvas.getByText(/retention policy could not be read/i)).toBeVisible();
    await expect(canvas.getByText('membership listing unavailable')).toBeVisible();
  },
};

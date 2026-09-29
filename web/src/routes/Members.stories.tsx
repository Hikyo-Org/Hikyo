import type { Meta, StoryObj } from '@storybook/react-vite';
import { zEnvironmentList, zEnvironmentSettings, zGrantList, zKeyList, zOrg, zProjectList, zRuleList } from '@hikyo/zod';
import { expect, userEvent, within } from 'storybook/test';
import type { z } from 'zod';

import { authenticatedIdentity } from '../testkit/identity.ts';
import { ORG } from '../testkit/ids.ts';
import { grantRows, IDS, ruleRows, TOPOLOGY } from './accessRules/fixture.ts';
import { Members } from './Members.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The org-scope members surface reads `useParams().org`, `useSearchParams()` and
// `useAuth` (no outlet context, no workspace context), so the harness runs it
// with `auth: true` at `/orgs/:org/members`. Its load-time reads are three GETs:
// the org (`useOrg`), the org's grant list (`useOrgGrants`), and the org's
// projects (`useOrgTopology` → `useProjects`). An EMPTY project list keeps the
// topology `ready` while firing no per-environment/per-setting fan-out, so the
// list, the inspector and the "New grant" affordance all render from one page.
// `useInstanceGrants(false)` is disabled at org scope and needs no row. See
// .storybook/withApp.tsx. Revoke/Reset/New grant/Invite are mutations or dialogs
// the read-only play never fires.
const ORG_URL = `/api/v1/orgs/${ORG}`;
const GRANTS_URL = `/api/v1/orgs/${ORG}/grants`;
const PROJECTS_URL = `/api/v1/orgs/${ORG}/projects`;
const RULES_URL = `/api/v1/orgs/${ORG}/rules`;

const org = {
  id: ORG,
  name: 'Acme',
  active: true,
  created_at: '2026-01-01T00:00:00Z',
  origin: 'manual',
} satisfies z.input<typeof zOrg>;

const noProjects = { count: 0, items: [] } satisfies z.input<typeof zProjectList>;
const noRules = { count: 0, items: [] } satisfies z.input<typeof zRuleList>;

// Two members at organisation scope. Principal ids differ from the operator's
// (prn_…174010), so the "you" badge and the self-reset guard stay out of the way.
// No grant carries `reveal`, the inspector's default capability, so the answer
// reads "Nobody" and the only "Dana"/"Ravi" text on the page is the member cell.
const DANA = 'prn_123e4567-e89b-12d3-a456-426614174020';
const RAVI = 'prn_123e4567-e89b-12d3-a456-426614174021';
const origins = [{ kind: 'manual', subject: 'operator' }];
const grants = {
  count: 3,
  items: [
    {
      id: 'grn_123e4567-e89b-12d3-a456-426614174030',
      principal_id: DANA,
      principal_name: 'Dana',
      capability: 'manage-members',
      scope: { org_id: ORG },
      origins,
      created_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'grn_123e4567-e89b-12d3-a456-426614174031',
      principal_id: DANA,
      principal_name: 'Dana',
      capability: 'read',
      scope: { org_id: ORG },
      origins,
      created_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'grn_123e4567-e89b-12d3-a456-426614174032',
      principal_id: RAVI,
      principal_name: 'Ravi',
      capability: 'read',
      scope: { org_id: ORG },
      origins,
      created_at: '2026-01-01T00:00:00Z',
    },
  ],
} satisfies z.input<typeof zGrantList>;

const meta = {
  component: Members,
  tags: ['ai-generated'],
  args: { scope: { kind: 'org' } },
  parameters: {
    ...topLayerDocs,
    app: {
      auth: true,
      identity: authenticatedIdentity,
      path: `/orgs/${ORG}/members`,
      routePath: '/orgs/:org/members',
      responses: [
        { url: ORG_URL, body: org },
        { url: GRANTS_URL, body: grants },
        { url: PROJECTS_URL, body: noProjects },
        { url: RULES_URL, body: noRules },
      ],
    },
  },
} satisfies Meta<typeof Members>;

export default meta;
type Story = StoryObj<typeof meta>;

// Populated organisation: the membership table lists each principal per scope,
// one revocable line per capability, under the "Who can…?" inspector.
export const Populated: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { name: /members/i, level: 1 })).toBeVisible();
    // The table mounts only once the grants query resolves, so await the first
    // header; its siblings and rows are in the DOM together by then.
    await expect(await canvas.findByRole('columnheader', { name: 'Member' })).toBeVisible();
    await expect(canvas.getByRole('columnheader', { name: 'Scope' })).toBeVisible();
    await expect(canvas.getByRole('columnheader', { name: 'Capabilities' })).toBeVisible();
    await expect(await canvas.findByText('Dana')).toBeVisible();
    await expect(canvas.getByText('Ravi')).toBeVisible();
    await expect(
      canvas.getByRole('button', { name: /revoke manage-members on .* for Dana/i }),
    ).toBeVisible();
  },
};

// Load failure: the grant listing 500s, so the page blanks the derived rows and
// shows the degraded alert instead — the state a real screen shows when the
// memberships read is down. The org and projects reads still fire.
export const LoadError: Story = {
  parameters: {
    app: {
      auth: true,
      identity: authenticatedIdentity,
      path: `/orgs/${ORG}/members`,
      routePath: '/orgs/:org/members',
      responses: [
        { url: ORG_URL, body: org },
        { url: GRANTS_URL, status: 500, body: { error: 'boom' } },
        { url: PROJECTS_URL, body: noProjects },
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByText(/server failed while reading memberships/i),
    ).toBeVisible();
  },
};

// The organisation with access rules (fixture: accessRules/fixture.ts): two
// projects with their environments, protection and key catalogues, grants
// for Sam and two machines, rules for four people. Every read the page makes
// is answered, so the Rules panel, Who can reach one key? and the rule editor
// all run over the real listings' shapes.
const created = '2026-01-01T00:00:00Z';
const projectUrl = (project: string) => `${PROJECTS_URL}/${project}`;
const topologyRows = TOPOLOGY.flatMap((project) => [
  {
    url: `${projectUrl(project.id)}/environments`,
    body: {
      count: project.envs.length,
      items: project.envs.map((env, index) => ({ id: env.id, org_id: ORG, project_id: project.id, name: env.name, display_order: index, created_at: created })),
    } satisfies z.input<typeof zEnvironmentList>,
  },
  ...project.envs.map((env) => ({
    url: `${projectUrl(project.id)}/environments/${env.id}/settings`,
    body: { protected: env.protected, reauth_window_seconds: 600 } satisfies z.input<typeof zEnvironmentSettings>,
  })),
  {
    url: `${projectUrl(project.id)}/keys`,
    body: {
      count: project.keys.length,
      schema_revision: 1,
      items: project.keys.map((key) => ({
        id: key.id,
        org_id: ORG,
        project_id: project.id,
        name: key.name,
        folder_path: key.folder,
        classification: key.secret ? 'secret' : 'config',
        description: '',
        deprecated: false,
        deprecation_note: '',
        declaration: { rule: { type: 'string', allow_empty: false } },
        presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
        group_id: '',
        created_at: created,
      })),
    } satisfies z.input<typeof zKeyList>,
  },
]);
const projects = {
  count: TOPOLOGY.length,
  items: TOPOLOGY.map((project) => ({ id: project.id, org_id: ORG, name: project.name, created_at: created })),
} satisfies z.input<typeof zProjectList>;
const rules = ruleRows();

const ruleResponses = [
  { url: ORG_URL, body: org },
  { url: GRANTS_URL, body: { count: grantRows().length, items: grantRows() } satisfies z.input<typeof zGrantList> },
  { url: PROJECTS_URL, body: projects },
  { url: RULES_URL, body: { count: rules.length, items: rules } satisfies z.input<typeof zRuleList> },
  ...topologyRows,
];

export const WithAccessRules: Story = {
  parameters: {
    app: {
      auth: true,
      identity: authenticatedIdentity,
      path: `/orgs/${ORG}/members`,
      routePath: '/orgs/:org/members',
      responses: ruleResponses,
    },
  },
  play: async ({ canvas }) => {
    const section = (await canvas.findByRole('heading', { level: 2, name: 'Access rules' })).closest('section');
    if (section === null) throw new Error('no Access rules panel');
    const rulesPanel = within(section);
    await expect(await rulesPanel.findByRole('button', { name: 'Edit rule 2 of Alice Novak' })).toBeVisible();
    // Machines keep their grants: they are not offered rules.
    await expect(rulesPanel.queryByText('ci-deploy')).toBeNull();

    // Who can reach one key? answers over grants and rules.
    const key = await canvas.findByRole('combobox', { name: 'Key' });
    await userEvent.selectOptions(key, IDS.dbPassword);
    await expect(canvas.getByRole('table', { name: 'Yes: 3' })).toBeVisible();

    // Search narrows the grant lines and the rules by member.
    await userEvent.type(canvas.getByRole('searchbox', { name: 'Find a member' }), 'alice');
    await expect(rulesPanel.queryByText('Bob Tran')).toBeNull();
    await expect(rulesPanel.getByText('Alice Novak')).toBeVisible();
    await userEvent.clear(canvas.getByRole('searchbox', { name: 'Find a member' }));

    await userEvent.click(rulesPanel.getByRole('button', { name: '+ Add rule for Chen Li' }));
    const dialog = within(await canvas.findByRole('dialog', { name: 'New rule · Chen Li' }));
    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }));
    await expect(canvas.queryByRole('dialog')).toBeNull();
  },
};

// A project's Members page reads that project's part of the rules; a rule that
// also names another project is read-only there.
const projectRules = rules
  .filter((rule) => rule.where.projects.includes(IDS.payments))
  .map((rule) => ({
    ...rule,
    other_projects: rule.where.projects.length > 1,
    where: {
      projects: [IDS.payments],
      environments: { ...rule.where.environments, items: rule.where.environments.items.filter((item) => item.project === IDS.payments) },
      keys: { ...rule.where.keys, items: rule.where.keys.items.filter((item) => item.project === IDS.payments) },
    },
  }));

export const ProjectWithAccessRules: Story = {
  parameters: {
    app: {
      auth: true,
      identity: authenticatedIdentity,
      path: `/orgs/${ORG}/members?project=${IDS.payments}`,
      routePath: '/orgs/:org/members',
      responses: [
        { url: `${projectUrl(IDS.payments)}/rules`, body: { count: projectRules.length, items: projectRules } satisfies z.input<typeof zRuleList> },
        ...ruleResponses,
      ],
    },
  },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { level: 1, name: /payments/ })).toBeVisible();
    await expect(await canvas.findAllByText("Also applies to other projects: change it on the organisation's Members page.")).toHaveLength(3);
    await expect(canvas.queryByRole('button', { name: /^Edit rule \d of Bob Tran/ })).toBeNull();
    await expect(canvas.getByRole('button', { name: 'Edit rule 1 of Alice Novak' })).toBeVisible();
    // The key question asks about this project only.
    const project = await canvas.findByRole('combobox', { name: 'Project' });
    await expect(within(project).getAllByRole('option')).toHaveLength(1);
  },
};

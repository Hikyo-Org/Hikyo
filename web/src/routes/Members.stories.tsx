import type { Meta, StoryObj } from '@storybook/react-vite';
import { zEnvironmentList, zEnvironmentSettings, zGrantList, zKeyList, zOrg, zProjectList, zRuleList } from '@hikyo/zod';
import { expect, userEvent, waitFor, within } from 'storybook/test';
import type { z } from 'zod';

import { DemoSurface } from '../demo/DemoSurface.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { ORG } from '../testkit/ids.ts';
import { grantRows, IDS, ruleRows, TOPOLOGY } from './accessRules/fixture.ts';
import { Members } from './Members.tsx';

import { openTab } from '../../.storybook/openTab.ts';
import { phone } from '../../.storybook/layout.ts';
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
  title: 'Pages/Members',
  id: 'routes-members',
  component: Members,
  tags: ['ai-generated'],
  args: { scope: { kind: 'org' } },
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "Organisation or instance membership and access administration. The same route-owned screen displays invitations, principals, grants and registration policy with the scope supplied by its real providers and route parameters." } },

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
    const edits = await canvas.findAllByRole('button', { name: /Edit access on/ });
    const edit = edits[0];
    if (edit === undefined) throw new Error('No access card editor.');
    await expect(edit).toBeVisible();
    await expect(await canvas.findByText('Dana')).toBeVisible();
    await expect(canvas.getByText('Ravi')).toBeVisible();
    await userEvent.click(edit);
    const dialog = within(document.body).getByRole('dialog');
    await expect(within(dialog).getByRole('button', { name: /revoke manage-members on .* for Dana/i })).toBeVisible();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
  },
};

// The sections are tabs: the list first, "Who can…?" a tab away, its
// panel hidden until chosen and the selection kept in `?tab=`.
export const SectionTabs: Story = {
  play: async ({ canvas }) => {
    const members = await canvas.findByRole('tab', { name: 'Members' });
    await expect(members).toHaveAttribute('aria-selected', 'true');
    await expect(canvas.queryByRole('heading', { name: /Who can…\? Answer by inspection/ })).toBeNull();
    await openTab(canvas, 'Who can…?');
    await expect(await canvas.findByRole('heading', { name: /Who can…\? Answer by inspection/ })).toBeVisible();
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
    const section = (await canvas.findByRole('button', { name: 'Edit rule 2 of Alice Novak' })).closest('section');
    if (section === null) throw new Error('no Access rules panel');
    const rulesPanel = within(section);
    await expect(await rulesPanel.findByRole('button', { name: 'Edit rule 2 of Alice Novak' })).toBeVisible();
    // One button size (DESIGN.md): a rule's quiet Edit and "Add rule" match
    // every other button in height, padding and type.
    const metrics = (el: HTMLElement) => [el.getBoundingClientRect().height, getComputedStyle(el).paddingLeft, getComputedStyle(el).fontSize];
    const reference = metrics(canvas.getByRole('button', { name: 'Glossary' }));
    await expect(metrics(rulesPanel.getByRole('button', { name: 'Edit rule 2 of Alice Novak' }))).toEqual(reference);
    await expect(metrics(rulesPanel.getByRole('button', { name: 'Add rule for Chen Li' }))).toEqual(reference);
    // Machines keep their grants: they are not offered rules.
    await expect(rulesPanel.getByText('ci-deploy')).toBeVisible();

    // Who can reach one key? opens in the side panel and answers over grants
    // and rules; Escape puts the page back.
    await expect(canvas.queryByRole('combobox', { name: 'Key' })).toBeNull();
    await userEvent.click(canvas.getByRole('button', { name: 'Who can reach one key?' }));
    const ask = within(canvas.getByRole('complementary', { name: 'Who can reach one key?' }));
    const key = await ask.findByRole('combobox', { name: 'Key' });
    await userEvent.selectOptions(key, IDS.dbPassword);
    await expect(ask.getByRole('table', { name: 'Yes: 3' })).toBeVisible();
    await userEvent.keyboard('{Escape}');
    await expect(canvas.queryByRole('complementary', { name: 'Who can reach one key?' })).toBeNull();
    // Reopened (say after editing a rule), it keeps the question.
    await userEvent.click(canvas.getByRole('button', { name: 'Who can reach one key?' }));
    const again = within(canvas.getByRole('complementary', { name: 'Who can reach one key?' }));
    await expect(await again.findByRole('combobox', { name: 'Key' })).toHaveValue(IDS.dbPassword);
    await expect(again.getByRole('table', { name: 'Yes: 3' })).toBeVisible();
    await userEvent.keyboard('{Escape}');

    // The glossary is the other reference panel.
    await userEvent.click(canvas.getByRole('button', { name: 'Glossary' }));
    await expect(canvas.getByRole('complementary', { name: 'Glossary' })).toBeVisible();
    await userEvent.keyboard('{Escape}');

    // Search narrows the grant lines and the rules by member.
    await userEvent.type(canvas.getByRole('searchbox', { name: 'Find a member' }), 'alice');
    await expect(rulesPanel.queryByText('Bob Tran')).toBeNull();
    await expect(rulesPanel.getByText('Alice Novak')).toBeVisible();
    await userEvent.clear(canvas.getByRole('searchbox', { name: 'Find a member' }));

    await userEvent.click(rulesPanel.getByRole('button', { name: 'Add rule for Chen Li' }));
    const dialog = within(await canvas.findByRole('dialog', { name: 'New rule · Chen Li' }));
    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }));
    await expect(canvas.queryByRole('dialog')).toBeNull();
  },
};

// At a phone width the Who-can answer table drops its desktop `min-width`
// (so a sparser answer fills the side panel instead of forcing a scroll it
// does not need) and the member name cell drops out of its
// `.access-table .grants th[scope='row']` override (compact tables on
// mobile). This fixture's rows carry enough permission and
// scope badges that the table still scrolls sideways at a phone width, by
// design (DESIGN.md, `.table-scroll`); the `min-width: 0` and compact font
// are what's deterministic regardless of row content, so that is what this
// checks.
export const WhoCanPhoneCompact: Story = {
  parameters: WithAccessRules.parameters,
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    await canvas.findByRole('button', { name: 'Edit rule 2 of Alice Novak' });
    await userEvent.click(canvas.getByRole('button', { name: 'Who can reach one key?' }));
    const ask = within(canvas.getByRole('complementary', { name: 'Who can reach one key?' }));
    const key = await ask.findByRole('combobox', { name: 'Key' });
    await userEvent.selectOptions(key, IDS.dbPassword);
    const table = await ask.findByRole('table', { name: 'Yes: 3' });
    await expect(getComputedStyle(table).minWidth).toBe('0px');
    const compact = getComputedStyle(document.documentElement).getPropertyValue('--fs-xs').trim();
    const name = table.querySelector("th[scope='row']");
    await expect(name).not.toBeNull();
    await expect(getComputedStyle(name!).fontSize).toBe(compact);
  },
};

// Every rule card's Where is three fixed columns, so each label sits at
// the same x in every card. A person's actions are the grant first, the
// credential reset under it.
export const RuleScopeColumns: Story = {
  parameters: WithAccessRules.parameters,
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getAllByText('Keys', { selector: '.access-where__label' }).length).toBeGreaterThan(1));
    for (const label of ['Projects', 'Environments', 'Keys']) {
      const lefts = canvas.getAllByText(label, { selector: '.access-where__label' }).map((node) => Math.round(node.getBoundingClientRect().left));
      await expect(new Set(lefts).size).toBe(1);
    }
    // The note under Where keeps the card's gap from it, the same in every card.
    const notes = canvas.getAllByText(/^Includes .*created later\.$/);
    const gaps = notes.map((note) => Math.round(note.getBoundingClientRect().top - (note.previousElementSibling?.getBoundingClientRect().bottom ?? 0)));
    await expect(new Set(gaps).size).toBe(1);
    await expect(gaps[0]).toBeGreaterThanOrEqual(8);
    const add = canvas.getAllByRole('button', { name: /^Add scope-wide access for / });
    const reset = canvas.queryAllByRole('button', { name: /^Reset credential for / });
    for (const button of reset) {
      const sibling = add.find((candidate) => candidate.parentElement === button.parentElement);
      if (sibling === undefined) continue;
      await expect(sibling.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      await expect(button.getBoundingClientRect().top).toBeGreaterThanOrEqual(sibling.getBoundingClientRect().bottom);
    }
  },
};

// At a phone width each card's Where becomes label | value rows; the label
// column and the value column still line up across every card, and nothing
// pushes the page sideways.
export const RuleScopeColumnsPhone: Story = {
  parameters: WithAccessRules.parameters,
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    await waitFor(() => expect(canvas.getAllByText('Keys', { selector: '.access-where__label' }).length).toBeGreaterThan(1));
    const labels = canvas.getAllByText(/^(Projects|Environments|Keys)$/, { selector: '.access-where__label' });
    await expect(new Set(labels.map((node) => Math.round(node.getBoundingClientRect().left))).size).toBe(1);
    const values = labels.map((label) => label.nextElementSibling as HTMLElement);
    await expect(new Set(values.map((node) => Math.round(node.getBoundingClientRect().left))).size).toBe(1);
    if (phone()) {
      // Each value shares its label's row, to the right of it.
      for (const [index, label] of labels.entries()) {
        const value = values[index]?.getBoundingClientRect();
        const box = label.getBoundingClientRect();
        await expect((value?.left ?? 0) > box.right).toBe(true);
        await expect((value?.top ?? Infinity) < box.bottom && box.top < (value?.bottom ?? -Infinity)).toBe(true);
      }
    }
    await expect(canvasElement.scrollWidth).toBeLessThanOrEqual(canvasElement.clientWidth);
  },
};

// Editing a rule from a Who can reach one key? answer keeps the question: the
// selection lives in the page, so saving (which refetches the rules) neither
// resets the key nor drops the answer.
export const WhoCanKeepsQuestionAcrossEdit: Story = {
  parameters: WithAccessRules.parameters,
  play: async ({ canvas }) => {
    await canvas.findByRole('button', { name: 'Edit rule 2 of Alice Novak' });
    const opener = canvas.getByRole('button', { name: 'Who can reach one key?' });
    await userEvent.click(opener);
    const ask = within(canvas.getByRole('complementary', { name: 'Who can reach one key?' }));
    await userEvent.selectOptions(await ask.findByRole('combobox', { name: 'Key' }), IDS.dbPassword);
    await expect(ask.getByRole('table', { name: 'Yes: 3' })).toBeVisible();

    await userEvent.click(ask.getByRole('button', { name: 'Edit rule of Alice Novak' }));
    const dialog = within(await canvas.findByRole('dialog', { name: /^Edit rule\s*·\s*Alice Novak$/ }));
    await userEvent.click(dialog.getByRole('button', { name: 'Save' }));
    await expect(await canvas.findByText(/^Saved the rule of Alice Novak\./)).toBeInTheDocument();
    await waitFor(() => expect(canvas.queryByRole('dialog')).toBeNull());

    // The panel may have stayed open or been reopened: either way the question stands.
    if (canvas.queryByRole('complementary', { name: 'Who can reach one key?' }) === null) await userEvent.click(opener);
    const after = within(canvas.getByRole('complementary', { name: 'Who can reach one key?' }));
    await expect(await after.findByRole('combobox', { name: 'Key' })).toHaveValue(IDS.dbPassword);
    await expect(after.getByRole('table', { name: 'Yes: 3' })).toBeVisible();
  },
};

// The public demo mounts Members read-only ('actions' mode, no disabled
// fieldset): changing access is off, but everything that only reveals
// information stays usable: the tabs, Who can reach one key? (its form
// included), the Glossary and each card's Permission origins disclosure.
export const DemoReadOnly: Story = {
  parameters: WithAccessRules.parameters,
  decorators: [(Story) => <DemoSurface id="members"><Story /></DemoSurface>],
  play: async ({ canvas, canvasElement }) => {
    await expect(canvas.getByText(/^Read-only sample data\./)).toBeVisible();
    await expect(canvasElement.querySelector('fieldset[disabled]')).toBeNull();
    // Changes are off.
    await expect(await canvas.findByRole('button', { name: 'Edit rule 2 of Alice Novak' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Add rule for Chen Li' })).toBeDisabled();

    // Permission origins opens in place.
    const [origins] = canvas.getAllByRole('button', { name: 'Permission origins' });
    if (origins === undefined) throw new Error('no Permission origins disclosure');
    await expect(origins).toBeEnabled();
    await userEvent.click(origins);
    await expect(origins).toHaveAttribute('aria-expanded', 'true');

    // A machine wears the same tag in the rules list and in a Who can answer.
    const machineInList = [...canvasElement.querySelectorAll('.access-person__who .badge')]
      .find((badge) => badge.textContent?.trim() === 'machine');
    if (machineInList === undefined) throw new Error('no machine tag in the rules list');

    const ask = canvas.getByRole('button', { name: 'Who can reach one key?' });
    await expect(ask).toBeEnabled();
    await userEvent.click(ask);
    const panel = within(canvas.getByRole('complementary', { name: 'Who can reach one key?' }));
    const key = await panel.findByRole('combobox', { name: 'Key' });
    await expect(key).toBeEnabled();
    await userEvent.selectOptions(key, IDS.dbPassword);
    const yes = panel.getByRole('table', { name: 'Yes: 3' });
    await expect(yes).toBeVisible();
    const machineInAnswer = within(yes).getByText('machine').closest('.badge');
    await expect(machineInAnswer?.className).toBe(machineInList.className);
    await expect(machineInAnswer?.querySelector('svg')?.getAttribute('data-glyph')).toBe(
      machineInList.querySelector('svg')?.getAttribute('data-glyph'),
    );
    // Its answers' Edit rule is a change, so it is off here too.
    await expect(panel.getByRole('button', { name: 'Edit rule of Alice Novak' })).toBeDisabled();
    await userEvent.keyboard('{Escape}');

    const glossary = canvas.getByRole('button', { name: 'Glossary' });
    await expect(glossary).toBeEnabled();
    await userEvent.click(glossary);
    await expect(canvas.getByRole('complementary', { name: 'Glossary' })).toBeVisible();
    await userEvent.keyboard('{Escape}');

    // The tabs switch.
    await openTab(canvas, 'Who can…?');
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
    // The key question (in its side panel) asks about this project only.
    await userEvent.click(canvas.getByRole('button', { name: 'Who can reach one key?' }));
    const ask = within(canvas.getByRole('complementary', { name: 'Who can reach one key?' }));
    const project = await ask.findByRole('combobox', { name: 'Project' });
    await expect(within(project).getAllByRole('option')).toHaveLength(1);
  },
};

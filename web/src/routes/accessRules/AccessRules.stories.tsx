import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../../.storybook/topLayerDocs.ts';
import { AccessGlossary } from './AccessGlossary.tsx';
import { AccessRulesMembers } from './AccessRulesMembers.tsx';
import { makeWorld } from './fixture.ts';
import { KeyMoveConfirmDialog } from './KeyMoveConfirmDialog.tsx';
import { moveKey, type World } from './model.ts';
import { newRule, RuleEditorDialog } from './RuleEditorDialog.tsx';
import { WhoCanPage } from './WhoCan.tsx';

// The member-access rules prototype (iteration 6 of
// docs/site/public/prototypes/member-access) as real components over fixture
// data: no API, no router, no query client. Every story builds its own world
// from makeWorld(), so an edit in one never leaks into another. The editor
// and the key-move confirmation mount a native <dialog>, so every story
// renders framed on the Docs page.
const meta = {
  title: 'Prototypes/Member access',
  tags: ['ai-generated'],
  parameters: topLayerDocs,
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

function ruleOf(world: World, id: number) {
  const rule = world.rules.find((r) => r.id === id);
  if (rule === undefined) throw new Error(`fixture has no rule ${id}`);
  return rule;
}

/** A Panel by its h2: panels are sections with an id, not named regions. */
function panel(canvas: ReturnType<typeof within>, name: string) {
  const section = canvas.getByRole('heading', { level: 2, name }).closest('section');
  if (section === null) throw new Error(`no panel ${name}`);
  return within(section);
}

/** True when `a` comes before `b` in document order. */
const before = (a: Element, b: Element) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;

// Members: people only (machines and their rules live in Machine access),
// found by name, handle or note, one card per rule (permissions, then Where,
// then reach).
export const Members: Story = {
  render: () => <AccessRulesMembers initialWorld={makeWorld()} />,
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { level: 1, name: 'Members · acme' })).toBeVisible();
    const members = panel(canvas, 'Members');
    await expect(members.getByText('5 people, 9 rules. Service accounts are managed in Machine access.')).toBeVisible();
    await expect(members.getByText('Alice Novak')).toBeVisible();
    await expect(canvas.queryByText('ci-deploy')).toBeNull();
    await expect(canvas.queryByText('deploy-prod')).toBeNull();

    // Search matches the note as well as the name.
    const search = canvas.getByRole('searchbox', { name: 'Find a person' });
    await userEvent.type(search, 'sre');
    await expect(members.getByText('Dana Ruiz')).toBeVisible();
    await expect(members.queryByText('Alice Novak')).toBeNull();
    await userEvent.clear(search);
    await userEvent.type(search, 'nobody here');
    await expect(members.getByText('· no one matches “nobody here”')).toBeVisible();
    await userEvent.clear(search);

    await userEvent.click(canvas.getByRole('button', { name: 'Edit rule 2 of Alice Novak' }));
    const dialog = await canvas.findByRole('dialog', { name: 'Edit rule · Alice Novak' });
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await expect(canvas.queryByRole('dialog')).toBeNull();
  },
};

// The editor on Alice's folder-scoped rule (payments › staging, prod › only
// db/). Where comes first. The Admin preset ticks every non-secret box, so
// Reveal clears, and what a folder rule cannot carry stays disabled with its
// condition on the row. Widening Keys to "All, except..." re-enables Pin
// without changing the height of the permission list.
export const EditorFolderScopedAdmin: Story = {
  render: () => {
    const world = makeWorld();
    return <RuleEditorDialog world={world} rule={ruleOf(world, 3)} onSave={fn()} onRemove={fn()} onCancel={fn()} />;
  },
  play: async ({ canvas }) => {
    const dialogEl = await canvas.findByRole('dialog', { name: 'Edit rule · Alice Novak' });
    const dialog = within(dialogEl);
    const [where, permissions] = [dialog.getByRole('group', { name: 'Where' }), dialog.getByRole('group', { name: 'Permissions' })];
    await expect(before(where, permissions)).toBe(true);

    await expect(dialog.getByRole('checkbox', { name: 'Reveal' })).toBeChecked();
    await userEvent.click(dialog.getByRole('button', { name: 'Admin' }));
    await expect(dialog.getByRole('checkbox', { name: 'Reveal' })).not.toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Define keys' })).toBeChecked();
    const pin = dialog.getByRole('checkbox', { name: 'Pin' });
    await expect(pin).toBeDisabled();
    await expect(pin).toHaveAccessibleDescription(/Not available here: needs all keys of an environment\..*Only on rules that cover all keys of an environment\./);
    const machines = dialog.getByRole('checkbox', { name: 'Manage machines' });
    await expect(machines).toBeDisabled();
    await expect(machines).toHaveAccessibleDescription(/needs a whole project/);
    await expect(dialog.getByText(/Left out because this rule is narrower than they need: Pin, Manage machines/)).toBeVisible();

    const height = permissions.getBoundingClientRect().height;
    await userEvent.click(within(dialog.getByRole('group', { name: 'Keys' })).getByRole('radio', { name: 'All, except…' }));
    await expect(pin).toBeEnabled();
    await expect(permissions.getBoundingClientRect().height).toBe(height);
  },
};

// A new rule starts with See ticked and no Where: nothing to save yet.
export const EditorNewRule: Story = {
  render: () => <RuleEditorDialog world={makeWorld()} rule={newRule('bob')} onSave={fn()} onCancel={fn()} />,
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'New rule · Bob Tran' }));
    await expect(before(dialog.getByRole('group', { name: 'Projects' }), dialog.getByRole('group', { name: 'Permissions' }))).toBe(true);
    await expect(dialog.getByRole('checkbox', { name: 'See' })).toBeChecked();
    await expect(dialog.getByText('Pick where this rule applies.')).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled();
    await expect(dialog.queryByRole('button', { name: 'Remove rule' })).toBeNull();
    // Environments and keys wait for a project.
    await expect(dialog.queryByRole('group', { name: 'Environments' })).toBeNull();
  },
};

// The editor opened for the deploy-prod workload machine, as Machine access
// will open it: the permission ADR's machine allowlists disable Pin and every
// management permission, and Reveal names the per-project opt-in.
export const MachineRuleEditor: Story = {
  name: 'Machine rule editor',
  render: () => {
    const world = makeWorld();
    return <RuleEditorDialog world={world} rule={ruleOf(world, 11)} onSave={fn()} onRemove={fn()} onCancel={fn()} />;
  },
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'Edit rule · deploy-prod' }));
    for (const name of ['Pin', 'Manage access', 'Manage machines', 'Manage deploys', 'Change settings', 'Manage projects']) {
      const box = dialog.getByRole('checkbox', { name });
      await expect(box).toBeDisabled();
      await expect(box).toHaveAccessibleDescription(/Machines cannot hold this/);
    }
    await expect(dialog.getByRole('checkbox', { name: 'See' })).toBeChecked();
    const reveal = dialog.getByRole('checkbox', { name: 'Reveal' });
    await expect(reveal).toBeChecked();
    await expect(reveal).toHaveAccessibleDescription(/Needs the project's machine reveal opt-in\./);
    const sentence = dialog.getByText((_, el) => el?.classList.contains('access-summary__sentence') === true);
    await expect(sentence).toHaveTextContent('deploy-prod can See, Reveal in payments › prod › all keys.');
  },
};

// Who can...? as a form: permission (with the editor's explanations), then
// project, environment and key. Default question: Reveal on DB_PASSWORD in
// payments prod. Each answer shows the deciding rule as the Members card does.
export const WhoCan: Story = {
  render: () => <WhoCanPage world={makeWorld()} />,
  play: async ({ canvas }) => {
    const reveal = canvas.getByRole('radio', { name: 'Reveal' });
    await expect(reveal).toBeChecked();
    await expect(reveal).toHaveAccessibleDescription(/Show current secret values/);
    await expect(canvas.queryByRole('radio', { name: 'Pin' })).toBeNull();
    const [project, env, key] = ['Project', 'Environment', 'Key'].map((name) => canvas.getByRole('combobox', { name }));
    if (project === undefined || env === undefined || key === undefined) throw new Error('form incomplete');
    await expect(before(reveal, project) && before(project, env) && before(env, key)).toBe(true);

    const yes = within(canvas.getByRole('list', { name: 'Yes: 3' }));
    await expect(yes.getByText('Marc Went')).toBeVisible();
    const machine = yes.getByText('deploy-prod').closest('li');
    if (machine === null) throw new Error('no answer row for deploy-prod');
    await expect(within(machine).getByText('Machine')).toBeVisible();
    // Alice's deciding rule, as badges and a Where line.
    const alice = yes.getByText('Alice Novak').closest('li');
    if (alice === null) throw new Error('no answer row for Alice');
    await expect(within(alice).getByText('Define keys')).toBeVisible();
    await expect(within(alice).getByText(/only db\//)).toBeVisible();
    const excepted = within(canvas.getByRole('list', { name: 'No, left out by an except: 2' }));
    await expect(excepted.getByText('Bob Tran')).toBeVisible();
    await expect(excepted.getByText('Dana Ruiz')).toBeVisible();

    await userEvent.click(canvas.getByRole('radio', { name: 'See' }));
    await expect(within(canvas.getByRole('list', { name: 'Yes: 5' })).getByText('Bob Tran')).toBeVisible();
  },
};

// The confirmation the Definitions move flow shows when a key move widens
// access: DB_PASSWORD leaves db/, so Dana's "except db/" no longer covers it.
export const KeyMoveConfirmation: Story = {
  name: 'Key move confirmation',
  render: () => {
    const world = makeWorld();
    return (
      <KeyMoveConfirmDialog
        before={world}
        after={moveKey(world, 'payments_k3', 'app')}
        keyId="payments_k3"
        change="Move DB_PASSWORD from db/ to app/"
        onCancel={fn()}
        onConfirm={fn()}
      />
    );
  },
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'This move gives people new access' }));
    const gains = within(dialog.getByRole('region', { name: 'Gains access' }));
    await expect(gains.getByText('Dana Ruiz: Reveal (prod), Reveal history (prod)')).toBeVisible();
    await expect(gains.queryByText(/deploy-prod/)).toBeNull();
    const loses = within(dialog.getByRole('region', { name: 'Loses access' }));
    await expect(loses.getByText('Alice Novak: Reveal (staging, prod), Define keys (staging, prod), Manage access (staging, prod)')).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Move and give access' })).toBeVisible();
  },
};

export const Glossary: Story = {
  render: () => <AccessGlossary />,
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { level: 1, name: 'Glossary' })).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'Permissions: secrets' })).toBeVisible();
    const machines = canvas.getByText('Manage machines', { selector: 'dt' });
    await expect(machines.nextElementSibling).toHaveTextContent('Only on rules that cover a whole project.');
    await expect(canvas.getByText('Machine', { selector: 'dt' }).nextElementSibling).toHaveTextContent(/never holds Pin/);
    // The Admin preset lists no Secrets permission.
    const admin = canvas.getByText('Admin', { selector: 'dt' });
    await expect(admin.nextElementSibling).not.toHaveTextContent('Reveal');
  },
};

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
    await expect(dialog.getByText(/Left out until Where is wider: Pin, Manage machines/)).toBeVisible();
    // "Saves as" is the same rule summary the Members card shows.
    const saves = within(dialog.getByRole('region', { name: 'Saves as' }));
    await expect(saves.getByText('Define keys')).toBeVisible();
    await expect(saves.getByText(/only db\//)).toBeVisible();
    // The action row stays in the dialog's box: Save is reachable without scrolling.
    await expect(dialog.getByRole('button', { name: 'Save' }).getBoundingClientRect().bottom).toBeLessThanOrEqual(dialogEl.getBoundingClientRect().bottom);

    const toggle = dialog.getByRole('button', { name: 'Pick single keys (8)' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(dialog.queryByRole('button', { name: /^db\/DB_PASSWORD/ })).toBeNull();
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(dialog.getByRole('button', { name: /^db\/DB_PASSWORD/ })).toBeVisible();

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
// will open it: what the permission ADR's machine allowlists forbid (Pin and
// every management permission) is not offered, one line names it, and
// Reveal names the per-project opt-in.
export const MachineRuleEditor: Story = {
  name: 'Machine rule editor',
  render: () => {
    const world = makeWorld();
    return <RuleEditorDialog world={world} rule={ruleOf(world, 11)} onSave={fn()} onRemove={fn()} onCancel={fn()} />;
  },
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'Edit rule · deploy-prod' }));
    // What a machine can never hold is not offered at all, and named once.
    for (const name of ['Pin', 'Manage access', 'Manage machines', 'Manage deploys', 'Change settings', 'Manage projects']) {
      await expect(dialog.queryByRole('checkbox', { name })).toBeNull();
    }
    await expect(
      dialog.getByText('Machines cannot hold Pin, Manage access, Manage machines, Manage deploys, Change settings or Manage projects.'),
    ).toBeVisible();
    await expect(dialog.getByRole('checkbox', { name: 'See' })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Define keys' })).toBeEnabled();
    const reveal = dialog.getByRole('checkbox', { name: 'Reveal' });
    await expect(reveal).toBeChecked();
    await expect(reveal).toHaveAccessibleDescription(/Needs the project's machine reveal opt-in\./);
    const saves = within(dialog.getByRole('region', { name: 'Saves as' }));
    await expect(saves.getByText('Reveal')).toBeVisible();
    await expect(saves.getByText(/payments/)).toBeVisible();
  },
};

// Who can...? as a compact form: permission (its explanation as the hint),
// project, environment, key. Default question: Reveal on DB_PASSWORD in
// payments prod. Answers are tables in the Members page's anatomy, each row
// with an Edit rule action that opens the deciding rule in the editor.
export const WhoCan: Story = {
  render: () => <WhoCanPage initialWorld={makeWorld()} />,
  play: async ({ canvas }) => {
    const [permission, project, env, key] = ['Permission', 'Project', 'Environment', 'Key'].map((name) => canvas.getByRole('combobox', { name }));
    if (permission === undefined || project === undefined || env === undefined || key === undefined) throw new Error('form incomplete');
    await expect(before(permission, project) && before(project, env) && before(env, key)).toBe(true);
    await expect(permission).toHaveValue('reveal');
    await expect(permission).toHaveAccessibleDescription(/Show current secret values/);

    const yes = within(canvas.getByRole('table', { name: 'Yes: 3' }));
    await expect(yes.getByRole('rowheader', { name: 'Sam Ortiz' })).toBeVisible();
    await expect(yes.getByRole('rowheader', { name: /deploy-prod/ })).toHaveTextContent('Machine');
    // Alice's deciding rule: badges in Permissions, the Where line beside them.
    const alice = within(yes.getByRole('row', { name: /Alice Novak/ }));
    await expect(alice.getByText('Define keys')).toBeVisible();
    await expect(alice.getByText(/only db\//)).toBeVisible();
    const excepted = within(canvas.getByRole('table', { name: 'No, left out by an except: 2' }));
    await expect(within(excepted.getByRole('row', { name: /Bob Tran/ })).getByText('Left out: this rule has except prod.')).toBeVisible();
    await expect(excepted.getByRole('rowheader', { name: 'Dana Ruiz' })).toBeVisible();

    // Only who CAN, or nearly could: members no rule reaches are not listed.
    await expect(canvas.queryByText('Chen Li')).toBeNull();
    await expect(canvas.queryByRole('table', { name: /No rule reaches/ })).toBeNull();

    // See: an except narrows only its own rule, so Bob is a yes, with the why as a second line.
    await userEvent.selectOptions(permission, 'read');
    await expect(permission).toHaveAccessibleDescription(/Key names, descriptions/);
    const see = within(canvas.getByRole('table', { name: 'Yes: 5' }));
    await expect(within(see.getByRole('row', { name: /Bob Tran/ })).getByText(/an except only narrows its own rule/)).toBeVisible();

    // Edit the deciding rule from its answer: the same editor, and saving recomputes the answer.
    await userEvent.selectOptions(permission, 'reveal');
    await userEvent.click(canvas.getByRole('button', { name: 'Edit rule of Alice Novak' }));
    const dialog = within(await canvas.findByRole('dialog', { name: 'Edit rule · Alice Novak' }));
    await expect(dialog.getByRole('checkbox', { name: 'Reveal' })).toBeChecked();
    await userEvent.click(dialog.getByRole('checkbox', { name: 'Reveal' }));
    await userEvent.click(dialog.getByRole('button', { name: 'Save' }));
    await expect(canvas.queryByRole('dialog')).toBeNull();
    const after = within(canvas.getByRole('table', { name: 'Yes: 2' }));
    await expect(after.queryByRole('rowheader', { name: 'Alice Novak' })).toBeNull();
    // Machine rows open the same editor (it is the Machine access editor too).
    await userEvent.click(canvas.getByRole('button', { name: 'Edit rule of deploy-prod' }));
    await expect(await canvas.findByRole('dialog', { name: 'Edit rule · deploy-prod' })).toBeVisible();
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

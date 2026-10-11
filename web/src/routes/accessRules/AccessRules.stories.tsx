import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../../.storybook/topLayerDocs.ts';
import { Panel } from '../Sections.tsx';
import { AccessGlossary } from './AccessGlossary.tsx';
import { IDS, makeWorld } from './fixture.ts';
import { KeyMoveConfirmDialog } from './KeyMoveConfirmDialog.tsx';
import { envName, newRule, type Rule, type World } from './model.ts';
import { RuleEditorDialog } from './RuleEditorDialog.tsx';
import { RulesPanel } from './RulesPanel.tsx';
import { WhoCan as WhoCanForm } from './WhoCan.tsx';

// The pieces the Members page mounts for member access rules, over the
// fixture organisation in the listings' own shapes (fixture.ts), turned into
// the page's model by the same mapping. The Members route story shows them in
// place; these show each piece's states. The editor and the key-move
// confirmation mount a native <dialog>, so every story renders framed on the
// Docs page.
const meta = {
  title: 'Features/Members/Access rules',
  id: 'members-access-rules',
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, docs: { ...topLayerDocs.docs, description: { component: "The member-access rule composites: rule lists and editors, concrete permission explanations, the access glossary and key-move confirmation. Each composite retains its own meaningful branch within the shared module; native dialogs render in isolated Docs frames." } } },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const PROJECTS = [IDS.payments, IDS.web];

function rulesOf(world: World, member: string): Rule[] {
  return world.rules.filter((r) => r.member === member && r.source.kind === 'rule');
}

function ruleAt(world: World, member: string, index: number): Rule {
  const rule = rulesOf(world, member)[index];
  if (rule === undefined) throw new Error(`fixture has no rule ${index} for ${member}`);
  return rule;
}

/** True when `a` comes before `b` in document order. */
const before = (a: Element, b: Element) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;

const people = (world: World) => world.people.filter((p) => p.kind === 'person');

// The rules half of Members: people only (machines keep their grants), one
// card per rule (permissions, then Where, then reach).
export const Rules: Story = {
  render: () => {
    const world = makeWorld();
    return (
      <Panel id="members-rules" title="Access rules">
        <RulesPanel world={world} people={people(world)} you={IDS.sam} canEdit onEdit={fn()} onAdd={fn()} />
      </Panel>
    );
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText('Alice Novak')).toBeVisible();
    await expect(canvas.queryByText('ci-deploy')).toBeNull();
    // Sam holds grants only.
    const sam = within(canvas.getByText('Sam Ortiz').closest('li') ?? document.body);
    await expect(sam.getByText('· no rules')).toBeVisible();
    await expect(canvas.getByText('you')).toBeVisible();
    // Alice's folder rule: its Where reads as labelled parts, its reach counts keys.
    await expect(canvas.getByRole('button', { name: 'Edit rule 2 of Alice Novak' })).toBeVisible();
    await expect(canvas.getAllByText(/only db\//).length).toBeGreaterThan(0);
    await expect(canvas.getByText(/2 environments · 3 keys · can reveal 2 secret values · 1 protected/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: '+ Add rule for Chen Li' })).toBeVisible();
  },
};

// On a project's Members page a rule that also names another project shows
// only this project's part, so it cannot be edited from there.
export const RulesOnAProject: Story = {
  render: () => {
    const base = makeWorld();
    const world: World = {
      ...base,
      rules: base.rules.map((rule) =>
        rule.member === IDS.bob && rule.source.kind === 'rule' ? { ...rule, source: { ...rule.source, otherProjects: true } } : rule,
      ),
    };
    return (
      <Panel id="members-rules" title="Access rules">
        <RulesPanel world={world} people={people(world).filter((p) => p.id === IDS.bob)} you={IDS.sam} canEdit onEdit={fn()} onAdd={fn()} />
      </Panel>
    );
  },
  play: async ({ canvas }) => {
    await expect(canvas.getAllByText("Also applies to other projects: change it on the organisation's Members page.")).toHaveLength(2);
    await expect(canvas.queryByRole('button', { name: /^Edit rule/ })).toBeNull();
    await expect(canvas.getByRole('button', { name: '+ Add rule for Bob Tran' })).toBeVisible();
  },
};

// The editor on Alice's folder rule (payments › staging, prod › only db/).
// Where comes first. See remains environment-wide alongside key permissions.
// Pin needs all keys; widening Keys enables it without changing row heights.
export const EditorFolderRule: Story = {
  render: () => {
    const world = makeWorld();
    return <RuleEditorDialog world={world} rule={ruleAt(world, IDS.alice, 1)} projects={PROJECTS} onSave={fn()} onRemove={fn()} onCancel={fn()} />;
  },
  play: async ({ canvas }) => {
    const dialogEl = await canvas.findByRole('dialog', { name: 'Edit rule · Alice Novak' });
    const dialog = within(dialogEl);
    const [where, permissions] = [dialog.getByRole('group', { name: 'Where' }), dialog.getByRole('group', { name: 'Permissions' })];
    await expect(before(where, permissions)).toBe(true);
    // No "all projects": a rule names its projects.
    await expect(dialog.queryByRole('button', { name: 'all projects' })).toBeNull();
    await expect(dialog.getByText(/A rule names its projects/)).toBeVisible();

    await expect(dialog.getByRole('checkbox', { name: 'Reveal' })).toBeChecked();
    await userEvent.click(dialog.getByRole('button', { name: 'Admin' }));
    await expect(dialog.getByRole('checkbox', { name: 'Reveal' })).not.toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Define keys' })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Define keys' })).toHaveAccessibleDescription(/Key definitions are shared.*every project environment/);
    // Mixed permission changes save as one atomic replacement.
    await userEvent.click(dialog.getByRole('checkbox', { name: 'Reveal history' }));
    await expect(dialog.getByRole('button', { name: 'Save' })).toBeEnabled();
    const see = dialog.getByRole('checkbox', { name: 'See' });
    await expect(see).toBeEnabled();
    await expect(see).toBeChecked();
    await expect(see).toHaveAccessibleDescription(/Always covers the whole environment/);
    await expect(dialog.getByRole('checkbox', { name: 'Publish' })).toBeEnabled();
    await expect(dialog.getByRole('checkbox', { name: 'Publish' })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Pin' })).toBeDisabled();
    await expect(dialog.getByText(/See always covers the whole environment: key names/)).toBeVisible();
    await expect(dialog.getByText('Assigning See requires Manage access across every key of the selected environments.')).toBeVisible();
    await expect(dialog.getByRole('checkbox', { name: 'Manage machines' })).toBeDisabled();
    // What no rule can carry is not listed, and named once.
    await expect(dialog.queryByRole('checkbox', { name: 'Manage projects' })).toBeNull();
    await expect(dialog.getAllByText(/Manage projects needs all projects/).length).toBeGreaterThan(0);
    // Manage access is available with its delegation boundary explained.
    await expect(dialog.getByRole('checkbox', { name: 'Manage access' })).toHaveAccessibleDescription(/Delegate only inside this rule/);
    await expect(dialog.getByText(/Left out until Where is wider: Pin, Manage machines/)).toBeVisible();
    const saves = within(dialog.getByRole('region', { name: 'Saves as' }));
    await expect(saves.getByText('Define keys')).toBeVisible();
    await expect(saves.getByText(/only db\//)).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Save' }).getBoundingClientRect().bottom).toBeLessThanOrEqual(dialogEl.getBoundingClientRect().bottom);

    const toggle = dialog.getByRole('button', { name: 'Pick single keys (8)' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(toggle);
    await expect(dialog.getByRole('button', { name: /^db\/DB_PASSWORD/ })).toBeVisible();

    const height = permissions.getBoundingClientRect().height;
    await userEvent.click(within(dialog.getByRole('group', { name: 'Keys' })).getByRole('radio', { name: 'All, except…' }));
    await expect(dialog.getByRole('checkbox', { name: 'Pin' })).toBeEnabled();
    await expect(permissions.getBoundingClientRect().height).toBe(height);
  },
};

// A new rule starts with See ticked and no Where: nothing to save yet. A
// refused save shows its reason inside the editor, and the draft stays.
export const EditorNewRule: Story = {
  render: () => (
    <RuleEditorDialog
      world={makeWorld()}
      rule={newRule(IDS.chen)}
      projects={PROJECTS}
      failure="Nothing changed: You do not manage access on every project this rule names, or something it names no longer exists. The two are deliberately the same answer."
      onSave={fn()}
      onCancel={fn()}
    />
  ),
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'New rule · Chen Li' }));
    await expect(dialog.getByRole('alert')).toHaveTextContent(/Nothing changed/);
    await expect(dialog.getByRole('checkbox', { name: 'See' })).toBeChecked();
    await expect(dialog.getByText('Pick where this rule applies.')).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled();
    await expect(dialog.queryByRole('button', { name: 'Remove rule' })).toBeNull();
    await expect(dialog.queryByRole('group', { name: 'Environments' })).toBeNull();
    await userEvent.click(dialog.getByRole('button', { name: /^payments/ }));
    await expect(dialog.getByRole('group', { name: 'Environments' })).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Save' })).toBeEnabled();
  },
};

const saveSingleKey = fn<(rule: Rule) => void>();

// A Publisher on one key keeps See environment-wide and leaves Pin out.
export const EditorSingleKeyRule: Story = {
  render: () => <RuleEditorDialog world={makeWorld()} rule={newRule(IDS.chen)} projects={PROJECTS} onSave={saveSingleKey} onCancel={fn()} />,
  play: async ({ canvas }) => {
    saveSingleKey.mockClear();
    const dialog = within(await canvas.findByRole('dialog', { name: 'New rule · Chen Li' }));
    await userEvent.click(dialog.getByRole('button', { name: /^payments/ }));
    await userEvent.click(within(dialog.getByRole('group', { name: 'Keys' })).getByRole('radio', { name: 'Only…' }));
    await userEvent.click(dialog.getByRole('button', { name: 'Pick single keys (8)' }));
    await userEvent.click(dialog.getByRole('button', { name: /^db\/DB_PASSWORD/ }));
    await userEvent.click(dialog.getByRole('button', { name: 'Publisher' }));
    await expect(dialog.getByRole('checkbox', { name: 'See' })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Edit' })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Publish' })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Pin' })).toBeDisabled();
    await expect(dialog.getByText(/See always covers the whole environment: key names/)).toBeVisible();
    await userEvent.click(dialog.getByRole('button', { name: 'Save' }));
    await expect(saveSingleKey).toHaveBeenCalledWith(expect.objectContaining({
      projects: [IDS.payments],
      keys: { mode: 'only', items: [{ project: IDS.payments, key: IDS.dbPassword }] },
    }));
  },
};

// Without See on a project its key catalogue is unreadable: folders and keys
// there cannot be picked, and the editor says so.
export const EditorWithoutKeyNames: Story = {
  render: () => <RuleEditorDialog world={makeWorld({ readable: false })} rule={{ ...newRule(IDS.chen), projects: [IDS.payments] }} projects={PROJECTS} onSave={fn()} onCancel={fn()} />,
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'New rule · Chen Li' }));
    await expect(dialog.getByText(/Key names in payments need See/)).toBeVisible();
    await expect(dialog.queryByRole('button', { name: /Pick single keys/ })).toBeNull();
    await expect(dialog.getByText(/keys not readable/)).toBeVisible();
  },
};

// Who can...? over grants and rules: permission, project, environment, key.
// Default question: Reveal on the first key in payments prod. Answers are
// tables; a rule row opens its rule, a grant row names the grant list.
export const WhoCan: Story = {
  render: () => <WhoCanForm world={makeWorld()} projects={PROJECTS} onEditRule={fn()} />,
  play: async ({ canvas }) => {
    const [permission, project, env, key] = ['Permission', 'Project', 'Environment', 'Key'].map((name) => canvas.getByRole('combobox', { name }));
    if (permission === undefined || project === undefined || env === undefined || key === undefined) throw new Error('form incomplete');
    await expect(before(permission, project) && before(project, env) && before(env, key)).toBe(true);
    await expect(permission).toHaveValue('reveal');
    await expect(permission).toHaveAccessibleDescription(/Show current secret values/);
    await expect(env).toHaveValue(IDS.payProd);
    await userEvent.selectOptions(key, IDS.dbPassword);

    const yes = within(canvas.getByRole('table', { name: 'Yes: 3' }));
    await expect(yes.getByRole('rowheader', { name: 'Sam Ortiz' })).toBeVisible();
    await expect(yes.getByRole('rowheader', { name: /deploy-prod/ })).toHaveTextContent('Machine');
    await expect(yes.getAllByText('Scope-wide access: edit its card in Members.')).toHaveLength(2);
    const alice = within(yes.getByRole('row', { name: /Alice Novak/ }));
    await expect(alice.getByText(/only db\//)).toBeVisible();
    const excepted = within(canvas.getByRole('table', { name: 'No, left out by an except: 2' }));
    await expect(within(excepted.getByRole('row', { name: /Bob Tran/ })).getByText('Left out: this rule has except prod.')).toBeVisible();
    await expect(within(excepted.getByRole('row', { name: /Dana Ruiz/ })).getByText('Left out: this rule has except db/.')).toBeVisible();
    const noRule = canvas.getByRole('button', { name: 'No, no rule reaches: 2' });
    await expect(noRule).toHaveAttribute('aria-expanded', 'false');
    await expect(canvas.getByText('Chen Li')).not.toBeVisible();
    await userEvent.click(noRule);
    await expect(noRule).toHaveAttribute('aria-expanded', 'true');
    await expect(canvas.getByText('Chen Li')).toBeVisible();
    await expect(canvas.getAllByText('None of their rules or scope-wide access gives Reveal here.')).toHaveLength(2);
    await expect(within(excepted.getByRole('row', { name: /Dana Ruiz/ })).getByText('Left out: this rule has except db/.')).toBeVisible();

    // See is never narrowed by keys: an except narrows only its own rule.
    await userEvent.selectOptions(permission, 'read');
    await expect(within(canvas.getByRole('table', { name: 'Yes: 5' })).getByRole('rowheader', { name: 'Bob Tran' })).toBeVisible();

    await userEvent.selectOptions(permission, 'reveal');
    await userEvent.click(canvas.getByRole('button', { name: 'Edit rule of Alice Novak' }));
  },
};

// Without See on the project the question is asked for the whole
// environment, and says so; key-narrowed rules then do not count.
export const WhoCanWithoutKeyNames: Story = {
  render: () => <WhoCanForm world={makeWorld({ readable: false })} projects={PROJECTS} />,
  play: async ({ canvas }) => {
    const key = canvas.getByRole('combobox', { name: 'Key' });
    await expect(key).toBeDisabled();
    await expect(key).toHaveAccessibleDescription(/Key names need See in this project/);
    await expect(canvas.getByText('every key')).toBeVisible();
    await expect(canvas.getByRole('table', { name: 'Yes: 2' })).toBeVisible();
    await expect(canvas.queryByRole('button', { name: /^Edit rule/ })).toBeNull();
  },
};

const widening = {
  count: 1,
  gainers: [
    { principal_id: IDS.dana, principal_name: 'Dana Ruiz', capability: 'reveal' as const, environments: [IDS.payProd] },
    { principal_id: IDS.dana, principal_name: 'Dana Ruiz', capability: 'reveal-history' as const, environments: [IDS.payProd] },
  ],
};

// The confirmation a key move needs when it widens access: DB_PASSWORD leaves
// db/, so Dana's "except db/" no longer covers it. The server names who gains
// to a member manager; confirming resends the move naming exactly them.
const onMoveConfirm = fn();

export const KeyMoveConfirmation: Story = {
  name: 'Key move confirmation',
  render: () => {
    const world = makeWorld();
    return (
      <KeyMoveConfirmDialog
        widening={widening}
        change="Move DB_PASSWORD from db/ to app/"
        envName={(id) => envName(world, id)}
        onCancel={fn()}
        onConfirm={onMoveConfirm}
      />
    );
  },
  play: async ({ canvas }) => {
    onMoveConfirm.mockClear();
    const dialog = within(await canvas.findByRole('dialog', { name: 'This move gives people new access' }));
    const gains = within(dialog.getByRole('region', { name: 'Gains access' }));
    await expect(gains.getByText('Dana Ruiz: Reveal (prod), Reveal history (prod)')).toBeVisible();
    await userEvent.click(dialog.getByRole('button', { name: 'Move and give access' }));
    await expect(onMoveConfirm).toHaveBeenCalledWith([IDS.dana]);
  },
};

// Someone who does not manage access on the project learns only how many
// gain, and cannot confirm: there is no confirm action, and the dialog says why.
export const KeyMoveCountOnly: Story = {
  name: 'Key move, count only',
  render: () => <KeyMoveConfirmDialog widening={{ count: 3 }} change="Move DB_PASSWORD from db/ to app/" envName={(id) => id} onCancel={fn()} onConfirm={fn()} />,
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'This move gives people new access' }));
    await expect(dialog.getByText('Gains access: 3 people')).toBeVisible();
    await expect(dialog.getByText(/ask one of them to make this move/)).toBeVisible();
    await expect(dialog.queryByRole('button', { name: 'Move and give access' })).toBeNull();
  },
};

export const Glossary: Story = {
  render: () => (
    <Panel id="members-glossary" title="Glossary">
      <AccessGlossary />
    </Panel>
  ),
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Permissions: administration' }));
    const machines = canvas.getByText('Manage machines', { selector: 'dt' });
    await expect(machines.nextElementSibling).toHaveTextContent('Only on rules that cover a whole project.');
    await expect(canvas.getByText(/Manage projects needs all projects/)).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Presets' }));
    const admin = canvas.getByText('Admin', { selector: 'dt' });
    await expect(admin.nextElementSibling).not.toHaveTextContent('Reveal');
  },
};

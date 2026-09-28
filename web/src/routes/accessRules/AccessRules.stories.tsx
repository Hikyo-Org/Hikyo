import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../../.storybook/topLayerDocs.ts';
import { AccessGlossary } from './AccessGlossary.tsx';
import { AccessRulesMembers } from './AccessRulesMembers.tsx';
import { makeWorld } from './fixture.ts';
import type { World } from './model.ts';
import { newRule, RuleEditorDialog } from './RuleEditorDialog.tsx';
import { WhoCanPage } from './WhoCan.tsx';

// The member-access rules prototype (iteration 6 of
// docs/site/public/prototypes/member-access) as real components over fixture
// data: no API, no router, no query client. Every story builds its own world
// from makeWorld(), so an edit in one never leaks into another. The editor
// and the simulator mount a native <dialog>, so every story renders framed on
// the Docs page.
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

// Members: one card per rule, permissions then Where then reach. Excepts carry
// a cross and the word; secret permissions the lock and the danger tone.
export const Members: Story = {
  render: () => <AccessRulesMembers initialWorld={makeWorld()} />,
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { level: 1, name: 'Members · acme' })).toBeVisible();
    await expect(canvas.getByText('6 members, 10 rules')).toBeVisible();
    await expect(canvas.getByText('Alice Novak')).toBeVisible();
    // Alice's first rule matches the Publisher preset exactly.
    await expect(canvas.getAllByText('Publisher:').length).toBeGreaterThan(0);
    await userEvent.click(canvas.getByRole('button', { name: 'Edit rule 2 of Alice Novak' }));
    const dialog = await canvas.findByRole('dialog', { name: 'Edit rule · Alice Novak' });
    await expect(dialog).toBeVisible();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await expect(canvas.queryByRole('dialog')).toBeNull();
  },
};

// The editor on Alice's folder-scoped rule (payments › staging, prod › only
// db/). The play presses the Admin preset: it ticks every non-secret box, so
// Reveal clears, and the permissions a folder rule cannot carry stay disabled
// with the reason inline.
export const EditorFolderScopedAdmin: Story = {
  render: () => {
    const world = makeWorld();
    return <RuleEditorDialog world={world} rule={ruleOf(world, 3)} onSave={fn()} onRemove={fn()} onCancel={fn()} />;
  },
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'Edit rule · Alice Novak' }));
    await expect(dialog.getByRole('checkbox', { name: 'Reveal' })).toBeChecked();
    await userEvent.click(dialog.getByRole('button', { name: 'Admin' }));
    await expect(dialog.getByRole('checkbox', { name: 'Reveal' })).not.toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Define keys' })).toBeChecked();
    const pin = dialog.getByRole('checkbox', { name: 'Pin' });
    await expect(pin).toBeDisabled();
    await expect(pin).toHaveAccessibleDescription(/Not available here: needs all keys of an environment\./);
    const machines = dialog.getByRole('checkbox', { name: 'Manage machines' });
    await expect(machines).toBeDisabled();
    await expect(machines).toHaveAccessibleDescription(/needs a whole project/);
    await expect(dialog.getByText(/Left out because this rule is narrower than they need: Pin, Manage machines/)).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Remove rule' })).toBeVisible();
  },
};

// A new rule starts with See ticked and no Where: nothing to save yet.
export const EditorNewRule: Story = {
  render: () => <RuleEditorDialog world={makeWorld()} rule={newRule('bob')} onSave={fn()} onCancel={fn()} />,
  play: async ({ canvas }) => {
    const dialog = within(await canvas.findByRole('dialog', { name: 'New rule · Bob Tran' }));
    await expect(dialog.getByRole('checkbox', { name: 'See' })).toBeChecked();
    await expect(dialog.getByText('Pick where this rule applies.')).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled();
    await expect(dialog.queryByRole('button', { name: 'Remove rule' })).toBeNull();
    // Environments and keys wait for a project.
    await expect(dialog.queryByRole('group', { name: 'Environments' })).toBeNull();
  },
};

// Who can...? on the default question: Reveal on DB_PASSWORD in payments/prod.
// Bob and Dana are left out by an except in one of their rules; switching to
// See shows an except narrows only its own rule.
export const WhoCan: Story = {
  render: () => <WhoCanPage initialWorld={makeWorld()} />,
  play: async ({ canvas }) => {
    const yes = within(canvas.getByRole('list', { name: 'Yes: 2' }));
    await expect(yes.getByText('Marc Went')).toBeVisible();
    await expect(yes.getByText('Alice Novak')).toBeVisible();
    const excepted = within(canvas.getByRole('list', { name: 'No, left out by an except: 2' }));
    await expect(excepted.getByText('Bob Tran')).toBeVisible();
    await expect(excepted.getByText('Dana Ruiz')).toBeVisible();
    await userEvent.selectOptions(canvas.getByRole('combobox', { name: 'Permission' }), 'read');
    const see = within(canvas.getByRole('list', { name: 'Yes: 4' }));
    await expect(see.getByText('Bob Tran')).toBeVisible();
  },
};

export const Glossary: Story = {
  render: () => <AccessGlossary />,
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('heading', { level: 1, name: 'Glossary' })).toBeVisible();
    await expect(canvas.getByRole('heading', { name: 'Permissions: secrets' })).toBeVisible();
    const machines = canvas.getByText('Manage machines', { selector: 'dt' });
    await expect(machines.nextElementSibling).toHaveTextContent('Only on rules that cover a whole project.');
    // The Admin preset lists no Secrets permission.
    const admin = canvas.getByText('Admin', { selector: 'dt' });
    await expect(admin.nextElementSibling).not.toHaveTextContent('Reveal');
  },
};

// The definitions simulator: a rename saves straight away and changes nobody's
// access; moving DB_PASSWORD out of db/ widens Dana (her except named db/), so
// it is held for confirmation naming who gains and who loses.
export const SimulatorWideningMove: Story = {
  render: () => <WhoCanPage initialWorld={makeWorld()} />,
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Rename STRIPE_SECRET_KEY to STRIPE_API_KEY' }));
    await expect(within(canvas.getByRole('list', { name: 'Changes made' })).getByText('Nobody gains or loses access.')).toBeVisible();
    await userEvent.click(canvas.getByRole('button', { name: 'Move DB_PASSWORD from db/ to app/' }));
    const dialog = within(await canvas.findByRole('dialog', { name: 'This move gives people new access' }));
    await expect(dialog.getByText('Dana Ruiz: Reveal (prod), Reveal history (prod)')).toBeVisible();
    await expect(dialog.getByText('Alice Novak: Reveal (staging, prod), Define keys (staging, prod), Manage access (staging, prod)')).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Move and give access' })).toBeVisible();
  },
};

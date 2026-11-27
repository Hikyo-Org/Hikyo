// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm } from '../../testkit/renderForm.tsx';
import { IDS, makeWorld } from './fixture.ts';
import { MACHINE_RULE_HINT, type Person, type World } from './model.ts';
import { RulesPanel } from './RulesPanel.tsx';
import { WhoCan } from './WhoCan.tsx';

let unmount: (() => Promise<void>) | null = null;
afterEach(async () => {
  await unmount?.();
  unmount = null;
});

/** The fixture's machines hold grants only: give deploy-prod a copy of Alice's first rule. */
function worldWithMachineRule(): { world: World; machine: Person } {
  const world = makeWorld();
  const rule = world.rules.find((r) => r.member === IDS.alice && r.source.kind === 'rule');
  const machine = world.people.find((p) => p.id === IDS.deploy);
  if (rule === undefined || machine === undefined) throw new Error('fixture lacks a rule or machine');
  return { world: { ...world, rules: [...world.rules, { ...rule, id: 'machine-rule', member: IDS.deploy }] }, machine };
}

it('shows the machine hint in place of an Edit button in the rules panel', async () => {
  const { world, machine } = worldWithMachineRule();
  const view = await renderForm(<RulesPanel world={world} people={[machine]} you={IDS.sam} canEdit onEdit={vi.fn()} onAdd={vi.fn()} />);
  unmount = view.unmount;
  expect(view.container.textContent).toContain(MACHINE_RULE_HINT);
  expect([...view.container.querySelectorAll('button')]).toHaveLength(0);
});

it('shows the machine hint and no Edit rule action in who can', async () => {
  const { world } = worldWithMachineRule();
  const view = await renderForm(<WhoCan world={world} projects={[IDS.payments]} onEditRule={vi.fn()} asked={{ perm: 'read', project: IDS.payments, env: IDS.payStaging }} />);
  unmount = view.unmount;
  expect(view.container.textContent).toContain(MACHINE_RULE_HINT);
  const row = [...view.container.querySelectorAll('tr')].find((tr) => tr.textContent?.includes(MACHINE_RULE_HINT));
  expect(row?.querySelector('button')).toBeNull();
});

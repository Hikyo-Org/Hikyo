// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm } from '../../testkit/renderForm.tsx';
import { IDS, makeWorld } from './fixture.ts';
import { newRule, type MachineClass, type Rule } from './model.ts';
import { RuleEditorDialog } from './RuleEditorDialog.tsx';

vi.mock('../../ui/useModalDialog.ts', () => ({ useModalDialog: () => ({ current: null }) }));

let unmount: (() => Promise<void>) | null = null;
afterEach(async () => {
  await unmount?.();
  unmount = null;
});

async function editor(machine: MachineClass, rule: Rule, onSave = vi.fn()) {
  const view = await renderForm(
    <RuleEditorDialog world={makeWorld()} rule={rule} projects={[IDS.payments]} machine={machine} showReach={false} onSave={onSave} onCancel={vi.fn()} />,
  );
  unmount = view.unmount;
  return view.container;
}

const box = (root: HTMLElement, label: string) => {
  const found = [...root.querySelectorAll('label')].find((l) => l.textContent === label);
  const input = found === undefined ? null : root.querySelector<HTMLInputElement>(`#${CSS.escape(found.htmlFor)}`);
  if (input === null) throw new Error(`no ${label} control`);
  return input;
};
const row = (root: HTMLElement, label: string) => box(root, label).closest('.access-perm');
const saveButton = (root: HTMLElement) => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Save');

it('disables what a workload may not hold and shows the reason on the row', async () => {
  const root = await editor('workload', { ...newRule(IDS.ci), projects: [IDS.payments], envs: { mode: 'only', items: [] } });
  expect(box(root, 'See').disabled).toBe(false);
  for (const label of ['Edit', 'Publish', 'Reveal', 'Manage access']) expect(box(root, label).disabled).toBe(true);
  expect(row(root, 'Edit')?.textContent).toContain('Not for a workload account: it holds See only.');
  expect(row(root, 'Reveal')?.textContent).toContain('environment grant');
});

it('offers no key limits and only an Only... environment pick for a workload', async () => {
  const root = await editor('workload', { ...newRule(IDS.ci), projects: [IDS.payments], envs: { mode: 'only', items: [] } });
  expect(root.textContent).toContain('A machine rule covers every key of its environments');
  expect([...root.querySelectorAll('label')].map((l) => l.textContent)).not.toContain('All, except…');
  expect(saveButton(root)?.disabled).toBe(true);
});

it('saves only what an automation account may hold', async () => {
  const onSave = vi.fn();
  const root = await editor('automation', { ...newRule(IDS.ci), perms: ['read', 'edit', 'reveal', 'manage-members'], projects: [IDS.payments] }, onSave);
  expect(box(root, 'Edit').disabled).toBe(false);
  expect(box(root, 'Reveal').disabled).toBe(true);
  expect(row(root, 'Manage access')?.textContent).toContain('Not for an automation account');
  await act(async () => saveButton(root)?.click());
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ perms: ['read', 'edit'], projects: [IDS.payments] }));
});

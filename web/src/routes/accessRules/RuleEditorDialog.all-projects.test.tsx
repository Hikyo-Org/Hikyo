// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm } from '../../testkit/renderForm.tsx';
import { IDS, makeWorld } from './fixture.ts';
import { newRule, type Rule } from './model.ts';
import { RuleEditorDialog } from './RuleEditorDialog.tsx';

vi.mock('../../ui/useModalDialog.ts', () => ({ useModalDialog: () => ({ current: null }) }));

let unmount: (() => Promise<void>) | null = null;
afterEach(async () => {
  await unmount?.();
  unmount = null;
});

const PROJECTS = [IDS.payments, IDS.web];
const button = (root: HTMLElement, text: string) => [...root.querySelectorAll('button')].find((b) => (b.querySelector('.toggle-chip__label')?.textContent ?? b.textContent) === text);

async function editor(allProjects: boolean, onSave = vi.fn(), rule: Rule = newRule(IDS.chen), projects = PROJECTS) {
  const view = await renderForm(
    <RuleEditorDialog world={makeWorld()} rule={rule} projects={projects} allProjects={allProjects} showReach={false} onSave={onSave} onCancel={vi.fn()} />,
  );
  unmount = view.unmount;
  return view.container;
}

it('offers All projects only where the page lists the whole organisation (#1446)', async () => {
  expect(button(await editor(false), 'All projects')).toBeUndefined();
  await unmount?.();
  unmount = null;
  expect(button(await editor(true), 'All projects')).toBeDefined();
});

it('an All projects rule covers every environment and saves as all projects', async () => {
  const onSave = vi.fn();
  const root = await editor(true, onSave);
  await act(async () => button(root, 'All projects')?.click());
  expect(root.textContent).toContain('All projects covers every environment');
  expect(root.textContent).not.toContain('Pick single keys');
  await act(async () => button(root, 'Save')?.click());
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ projects: '*', envs: { mode: 'all', items: [] } }));
});

it('outside the org-wide page an all-projects rule can be narrowed but not saved as is', async () => {
  const onSave = vi.fn();
  const rule: Rule = { ...newRule(IDS.chen), projects: '*' };
  const root = await editor(false, onSave, rule, [IDS.payments]);
  expect(root.textContent).toContain('This rule covers all projects.');
  expect(button(root, 'Save')?.disabled).toBe(true);
  await act(async () => button(root, 'payments')?.click());
  expect(button(root, 'Save')?.disabled).toBe(false);
});

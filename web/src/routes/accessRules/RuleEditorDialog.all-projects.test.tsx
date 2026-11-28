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

it('an All projects rule picks environments by name and saves as all projects (#1447)', async () => {
  const onSave = vi.fn();
  const root = await editor(true, onSave);
  await act(async () => button(root, 'All projects')?.click());
  expect(root.textContent).toContain('Environments match by name in every project');
  expect(root.textContent).not.toContain('Pick single keys');
  // Leaving dev out picks it in every project, payments and web alike.
  await act(async () => button(root, 'dev')?.click());
  await act(async () => button(root, 'Save')?.click());
  expect(onSave).toHaveBeenCalledWith(
    expect.objectContaining({
      projects: '*',
      envs: {
        mode: 'all',
        names: ['dev'],
        items: [
          { project: IDS.payments, environment: IDS.payDev },
          { project: IDS.web, environment: IDS.webDev },
        ],
      },
    }),
  );
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

it('All projects stays unavailable while the organisation has no project', async () => {
  const root = await editor(true, vi.fn(), newRule(IDS.chen), []);
  expect(button(root, 'All projects')?.disabled).toBe(true);
  expect(root.textContent).toContain('All projects needs a project first');
  // A seeded all-projects draft cannot be saved without a project list either.
  await unmount?.();
  unmount = null;
  const seeded = await editor(true, vi.fn(), { ...newRule(IDS.chen), projects: '*' }, []);
  expect(button(seeded, 'Save')?.disabled).toBe(true);
});

it('an open all-projects draft follows a project added to the list', async () => {
  const onSave = vi.fn();
  const rule: Rule = { ...newRule(IDS.chen), projects: '*', keys: { mode: 'all', items: [{ project: IDS.payments, folder: 'secret' }] } };
  const view = await renderForm(
    <RuleEditorDialog world={makeWorld()} rule={rule} projects={[IDS.payments]} allProjects showReach={false} onSave={onSave} onCancel={vi.fn()} />,
  );
  unmount = view.unmount;
  await view.rerender(<RuleEditorDialog world={makeWorld()} rule={rule} projects={PROJECTS} allProjects showReach={false} onSave={onSave} onCancel={vi.fn()} />);
  await act(async () => button(view.container, 'Save')?.click());
  expect(onSave).toHaveBeenCalledWith(
    expect.objectContaining({ keys: { mode: 'all', items: [{ project: IDS.payments, folder: 'secret' }, { project: IDS.web, folder: 'secret' }] } }),
  );
});

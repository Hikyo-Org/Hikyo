// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm } from '../../testkit/renderForm.tsx';
import { IDS, makeWorld } from './fixture.ts';
import { newRule, type Rule } from './model.ts';
import { RuleWhere } from './parts.tsx';
import { RuleEditorDialog } from './RuleEditorDialog.tsx';

vi.mock('../../ui/useModalDialog.ts', () => ({ useModalDialog: () => ({ current: null }) }));

let unmount: (() => Promise<void>) | null = null;
afterEach(async () => {
  await unmount?.();
  unmount = null;
});

const button = (root: HTMLElement, text: string) => [...root.querySelectorAll('button')].find((b) => b.querySelector('.toggle-chip__label')?.textContent === text);

it('lists a named selector by its names, not by the projects that carry them (#1447)', async () => {
  // staging exists only in payments, yet selects across both projects.
  const rule: Rule = {
    ...newRule(IDS.chen),
    projects: [IDS.payments, IDS.web],
    envs: {
      mode: 'only',
      names: ['prod', 'staging'],
      items: [
        { project: IDS.payments, environment: IDS.payStaging },
        { project: IDS.payments, environment: IDS.payProd },
        { project: IDS.web, environment: IDS.webProd },
      ],
    },
  };
  const view = await renderForm(<RuleWhere world={makeWorld()} rule={rule} />);
  unmount = view.unmount;
  const envs = [...view.container.querySelectorAll('.access-where__value')][1];
  expect(envs?.getAttribute('title')).toBe('prod (protected), staging');
  expect(view.container.textContent).not.toContain('payments/');
});

it('removes one unmatched name and keeps the others (#1447)', async () => {
  const onSave = vi.fn();
  const rule: Rule = { ...newRule(IDS.chen), projects: [IDS.payments], envs: { mode: 'all', names: ['qa', 'retired'], items: [] } };
  const view = await renderForm(
    <RuleEditorDialog world={makeWorld()} rule={rule} projects={[IDS.payments, IDS.web]} allProjects={false} showReach={false} onSave={onSave} onCancel={vi.fn()} />,
  );
  unmount = view.unmount;
  const root = view.container;
  await act(async () => button(root, 'retired')?.click());
  expect(button(root, 'retired')).toBeUndefined();
  expect(button(root, 'qa')).toBeDefined();
  await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Save')?.click());
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ envs: expect.objectContaining({ mode: 'all', names: ['qa'] }) }));
});

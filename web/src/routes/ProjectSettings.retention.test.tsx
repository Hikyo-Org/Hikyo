// @vitest-environment happy-dom
import { act } from 'react';
import { beforeEach, expect, it, vi } from 'vitest';

import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { CompactProjectRetention } from './ProjectSettings.tsx';

const mutate = vi.hoisted(() => vi.fn());
vi.mock('../api/settings.ts', async (original) => ({
  ...(await original<typeof import('../api/settings.ts')>()),
  useSetProjectRetention: () => ({ isPending: false, mutate }),
}));
beforeEach(() => { mutate.mockReset(); });

const org = { mode: 'keep-if-either' as const, max_age_seconds: 1000, last_revisions: 30 };
const policy = (count: number) => ({ inherited: false, mode: 'keep-if-either' as const, max_age_seconds: 1000, last_revisions: count });
const props = { org: 'org_a', project: 'prj_a', orgPolicy: org, onDone: vi.fn(), onError: vi.fn() };
function input(container: HTMLElement, label: string): HTMLInputElement {
  const field = container.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`);
  if (field === null) throw new Error(`Missing ${label}`);
  return field;
}
async function blur(field: HTMLInputElement) {
  await act(async () => field.dispatchEvent(new FocusEvent('focusout', { bubbles: true })));
}

it('refreshes untouched retention from policy data and never writes on an untouched blur', async () => {
  const view = await renderForm(<CompactProjectRetention {...props} policy={policy(5)} />);
  await view.rerender(<CompactProjectRetention {...props} policy={policy(20)} />);
  const field = input(view.container, 'Revisions kept per environment');
  expect(field.value).toBe('20');
  await blur(field);
  expect(mutate).not.toHaveBeenCalled();
  await view.unmount();
});

it('refuses an edited draft when the visible policy changed during editing', async () => {
  const view = await renderForm(<CompactProjectRetention {...props} policy={policy(5)} />);
  const field = input(view.container, 'Revisions kept per environment');
  await act(async () => typeInto(field, '4'));
  await view.rerender(<CompactProjectRetention {...props} policy={policy(20)} />);
  await blur(field);
  expect(mutate).not.toHaveBeenCalled();
  expect(view.container.textContent).toContain('policy changed while you were editing');
  expect(field.value).toBe('20');
  await view.unmount();
});

it('renders unlimited inheritance and collects both positive bounds before creating an override', async () => {
  const view = await renderForm(<CompactProjectRetention {...props}
    policy={{ inherited: true, mode: 'unlimited' }} orgPolicy={{ mode: 'unlimited' }} />);
  expect(view.container.textContent).toContain('unlimited retention');
  expect(view.container.textContent).not.toContain('last 6 revisions');
  const mode = view.container.querySelector('select');
  if (mode === null) throw new Error('No retention mode');
  await act(async () => { mode.value = 'custom'; mode.dispatchEvent(new Event('change', { bubbles: true })); });
  expect(mutate).not.toHaveBeenCalled();
  const age = input(view.container, 'Maximum retention age (seconds)');
  const count = input(view.container, 'Revisions kept per environment');
  await act(async () => typeInto(count, '5'));
  await blur(count);
  expect(mutate).not.toHaveBeenCalled();
  await act(async () => typeInto(age, '86400'));
  await blur(age);
  expect(mutate).toHaveBeenCalledWith({ inherited: false, lastRevisions: 5, maxAgeSeconds: 86400 }, expect.any(Object));
  await view.unmount();
});

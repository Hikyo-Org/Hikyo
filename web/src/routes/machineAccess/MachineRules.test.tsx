// @vitest-environment happy-dom
import { replaceRulesOp } from '@hikyo/operations';
import { act } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { ApiError } from '../../api/client.ts';
import type { ServiceAccount } from '../../api/identities.ts';
import { RuleReauthFailure, RuleSaveFailure } from '../../api/rules.ts';
import { renderForm } from '../../testkit/renderForm.tsx';
import { IDS, makeWorld } from '../accessRules/fixture.ts';
import { MachineRules } from './MachineRules.tsx';

type SaveOptions = { onError: (error: unknown) => void };
const mutations = vi.hoisted(() => ({ save: vi.fn(), remove: vi.fn() }));
vi.mock('../../api/rules.ts', async (original) => ({
  ...(await original<typeof import('../../api/rules.ts')>()),
  useRuleMutations: () => ({ save: { mutate: mutations.save, isPending: false }, remove: { mutate: mutations.remove, isPending: false } }),
}));
vi.mock('../../ui/useModalDialog.ts', () => ({ useModalDialog: () => ({ current: null }) }));

const account = (kind: ServiceAccount['kind']): ServiceAccount => ({
  id: 'svc_a', principal_id: IDS.ci, name: 'ci-deploy', kind, created_at: '2026-08-01T00:00:00Z', created_by: 'usr_admin', live_credentials: 1,
});

let unmount: (() => Promise<void>) | null = null;
beforeEach(() => {
  mutations.save.mockReset();
  mutations.remove.mockReset();
});
afterEach(async () => {
  await unmount?.();
  unmount = null;
});

async function mount(kind: ServiceAccount['kind'], { ready = true, unavailable = null }: { ready?: boolean; unavailable?: string | null } = {}) {
  // The fixture's machines hold grants only: give ci-deploy a copy of Alice's first rule.
  const base = makeWorld();
  const rule = base.rules.find((r) => r.member === IDS.alice && r.source.kind === 'rule');
  if (rule === undefined) throw new Error('fixture lacks a rule');
  const world = { ...base, rules: [...base.rules, { ...rule, id: 'machine-rule', member: IDS.ci }] };
  const view = await renderForm(<MachineRules org={IDS.org} project={IDS.payments} account={account(kind)} world={world} unavailable={unavailable} ready={ready} />);
  unmount = view.unmount;
  return view.container;
}
const button = (root: HTMLElement, text: string) => {
  const found = [...root.querySelectorAll('button')].find((b) => b.textContent?.includes(text));
  if (found === undefined) throw new Error(`no ${text} button`);
  return found;
};
const label = (root: HTMLElement, text: string) => [...root.querySelectorAll('label')].some((l) => l.textContent === text);

/** Open the account's rule editor, save, and have the save fail with `error`. */
async function saveFailing(root: HTMLElement, error: unknown) {
  await act(async () => button(root, 'Edit').click());
  mutations.save.mockImplementation((_input: unknown, options: SaveOptions) => options.onError(error));
  await act(async () => button(root, 'Save').click());
}

it('says the reauthentication did not complete, with the ceremony cause, in the dialog', async () => {
  const root = await mount('automation');
  await saveFailing(root, new RuleReauthFailure(IDS.payProd, new DOMException('cancelled', 'NotAllowedError')));
  const text = root.querySelector('[role="alert"]')?.textContent ?? '';
  expect(text).toContain('newly reaches plaintext in prod');
  expect(text).toContain('The passkey prompt was dismissed or timed out.');
  expect(text).not.toContain('disclosed');
  expect(text).toContain('Nothing was saved.');
});

it('does not speak of codes when the passkey ceremony request is refused', async () => {
  const root = await mount('automation');
  await saveFailing(root, new RuleReauthFailure(IDS.payProd, new ApiError(401, 'request failed with 401', 'unauthorized')));
  const text = root.querySelector('[role="alert"]')?.textContent ?? '';
  expect(text).toContain('The passkey reauthentication was refused (server error 401).');
  expect(text).not.toContain('code');
});

it('says the server refused again when the reauthentication completed', async () => {
  const root = await mount('automation');
  const refusal = new ApiError(409, 'request failed with 409',
    `reauthenticate over the environments this operation makes reachable, then retry (${IDS.payProd})`, undefined, [], undefined, replaceRulesOp);
  await saveFailing(root, refusal);
  const text = root.querySelector('[role="alert"]')?.textContent ?? '';
  expect(text).toContain('over prod completed, but the server still refused');
  expect(text).not.toContain('did not complete');
});

it('closes the editor and reports an uncertain outcome on a RuleSaveFailure', async () => {
  const root = await mount('automation');
  await saveFailing(root, new RuleSaveFailure('atomic', new TypeError('lost response')));
  expect(root.querySelector('dialog, form')).toBeNull();
  expect(root.querySelector('[role="alert"]')?.textContent).toContain('could not be confirmed');
});

it('starts a workload rule with no environments and an automation rule with all of them', async () => {
  const workload = await mount('workload');
  await act(async () => button(workload, 'Add rule').click());
  expect(label(workload, 'All, except…')).toBe(false);
  expect(button(workload, 'Save').disabled).toBe(true);
  await unmount?.();
  const automation = await mount('automation');
  await act(async () => button(automation, 'Add rule').click());
  const all = [...automation.querySelectorAll('label')].find((l) => l.textContent === 'All, except…');
  expect(automation.querySelector<HTMLInputElement>(`#${CSS.escape(all?.htmlFor ?? '')}`)?.checked).toBe(true);
});

it('disables adding and editing until the rules and environments are read', async () => {
  const root = await mount('automation', { ready: false });
  expect(button(root, 'Add rule').disabled).toBe(true);
});

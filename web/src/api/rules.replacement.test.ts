import { beforeEach, expect, it, vi } from 'vitest';

import { ALL, replacementSaveRefusal, type Rule } from '../routes/accessRules/model.ts';
import { ApiError } from './client.ts';
import { removeRule, RuleSaveFailure, ruleFailureText, saveRule } from './rules.ts';

const writes = vi.hoisted(() => ({ parsed: vi.fn(), ok: vi.fn() }));
vi.mock('./client.ts', async (original) => ({
  ...(await original<typeof import('./client.ts')>()), parsed: writes.parsed, ok: writes.ok,
}));

const before: Rule = { id: 'grouped-rule', member: 'usr_other', perms: ['reveal'], projects: ['prj_a'], envs: ALL, keys: ALL,
  source: { kind: 'rule', parts: [{ perm: 'reveal', id: 'rul_old' }], otherProjects: false } };
beforeEach(() => {
  writes.parsed.mockReset().mockResolvedValue({ id: 'rul_new' });
  writes.ok.mockReset().mockResolvedValue(undefined);
});

it('refuses mixed non-self permission replacement before any create or revoke can expose their union', async () => {
  const draft: Rule = { ...before, perms: ['read'] };
  expect(replacementSaveRefusal(before, draft)).toContain('atomic rule replacement');
  const error = await saveRule('org_a', before, draft).catch((cause: unknown) => cause);
  expect(ruleFailureText(error)).toContain('No changes were sent');
  expect(writes.parsed).not.toHaveBeenCalled();
  expect(writes.ok).not.toHaveBeenCalled();
});

it('refuses mixed scope replacement before any writes', async () => {
  await expect(saveRule('org_a', before, { ...before, keys: { mode: 'only', items: [{ project: 'prj_a', folder: 'db' }] } })).rejects.toThrow('atomic rule replacement');
  expect(writes.parsed).not.toHaveBeenCalled();
  expect(writes.ok).not.toHaveBeenCalled();
});

it('still permits add-only and removal-only plans', async () => {
  await saveRule('org_a', before, { ...before, perms: ['reveal', 'read'] });
  expect(writes.parsed).toHaveBeenCalledOnce();
  expect(writes.ok).not.toHaveBeenCalled();
  writes.parsed.mockClear();
  await saveRule('org_a', before, { ...before, perms: [] });
  expect(writes.parsed).not.toHaveBeenCalled();
  expect(writes.ok).toHaveBeenCalledOnce();
});

it('does not claim complete rollback when a create result has no confirmed ID', async () => {
  writes.parsed.mockResolvedValueOnce({ id: 'rul_confirmed' }).mockRejectedValueOnce(new Error('Malformed committed response.'));
  const error = await saveRule('org_a', null, { ...before, perms: ['read', 'reveal'] }).catch((cause: unknown) => cause);
  expect(error).toBeInstanceOf(RuleSaveFailure);
  expect(error).toMatchObject({ stage: 'create', confirmedRevoked: ['rul_confirmed'] });
  expect(writes.ok).toHaveBeenCalledExactlyOnceWith(expect.anything(), { path: { org: 'org_a', rule: 'rul_confirmed' } });
  expect(ruleFailureText(error)).toContain('A new rule may still apply');
  expect(ruleFailureText(error)).not.toContain('Nothing changed');
});

it('does not mistake a session reconciliation ApiError for proof that create never committed', async () => {
  writes.parsed.mockRejectedValueOnce(new ApiError(401, 'session_reconciliation_failed'));
  const error = await saveRule('org_a', null, before).catch((cause: unknown) => cause);
  expect(ruleFailureText(error)).toContain('could not be confirmed');
  expect(ruleFailureText(error)).not.toContain('Nothing changed');
});

const grouped: Rule = { ...before, perms: ['read', 'reveal', 'edit'], source: {
  kind: 'rule', otherProjects: false,
  parts: [{ id: 'rul_first', perm: 'read' }, { id: 'rul_second', perm: 'reveal' }, { id: 'rul_third', perm: 'edit' }],
} };

it('reports confirmed grouped deletions without treating a masked 404 as success or continuing past it', async () => {
  writes.ok.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new ApiError(404, 'not_found'));
  const error = await removeRule('org_a', grouped).catch((cause: unknown) => cause);
  expect(error).toMatchObject({ stage: 'remove', confirmedRevoked: ['rul_first'] });
  expect(writes.ok).toHaveBeenCalledTimes(2);
  expect(writes.ok).not.toHaveBeenCalledWith(expect.anything(), { path: { org: 'org_a', rule: 'rul_third' } });
  expect(ruleFailureText(error)).toContain('1 rule part was confirmed removed');
  expect(ruleFailureText(error)).toContain('The two are deliberately the same answer');
});

it('reports pure removal save progress without claiming a new rule was saved', async () => {
  writes.ok.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new ApiError(503, 'unavailable'));
  const error = await saveRule('org_a', grouped, { ...grouped, perms: [] }).catch((cause: unknown) => cause);
  expect(error).toMatchObject({ stage: 'revoke', confirmedRevoked: ['rul_first'] });
  expect(writes.parsed).not.toHaveBeenCalled();
  expect(ruleFailureText(error)).toContain('1 rule part was confirmed removed');
  expect(ruleFailureText(error)).not.toContain('new rule is saved');
});

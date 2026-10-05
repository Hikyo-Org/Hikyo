import { beforeEach, expect, it, vi } from 'vitest';

import { ALL, type Rule } from '../routes/accessRules/model.ts';
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

it('dispatches a mixed permission replacement as one atomic request', async () => {
  await saveRule('org_a', before, { ...before, perms: ['read'] });
  expect(writes.parsed).toHaveBeenCalledExactlyOnceWith(expect.anything(), { path: { org: 'org_a' }, body: {
    principal: 'usr_other', revoke: ['rul_old'], create: [expect.objectContaining({ capability: 'read' })],
  } });
  expect(writes.ok).not.toHaveBeenCalled();
});

it('dispatches selector changes together with all old removals', async () => {
  await saveRule('org_a', before, { ...before, keys: { mode: 'only', items: [{ project: 'prj_a', folder: 'db' }] } });
  expect(writes.parsed).toHaveBeenCalledExactlyOnceWith(expect.anything(), expect.objectContaining({ body: {
    principal: 'usr_other', revoke: ['rul_old'], create: [expect.objectContaining({ capability: 'reveal', where: expect.objectContaining({ keys: { mode: 'only', items: [{ project: 'prj_a', folder: 'db' }] } }) })],
  } }));
});

it('removes all grouped parts in one request', async () => {
  const grouped: Rule = { ...before, source: { kind: 'rule', otherProjects: false, parts: [{ id: 'rul_first', perm: 'read' }, { id: 'rul_second', perm: 'reveal' }] } };
  await removeRule('org_a', grouped);
  expect(writes.parsed).toHaveBeenCalledExactlyOnceWith(expect.anything(), { path: { org: 'org_a' }, body: { principal: 'usr_other', revoke: ['rul_first', 'rul_second'], create: [] } });
  expect(writes.ok).not.toHaveBeenCalled();
});

it('keeps committed-response uncertainty without sending rollback requests', async () => {
  writes.parsed.mockRejectedValueOnce(new ApiError(401, 'session_reconciliation_failed'));
  const error = await saveRule('org_a', null, before).catch((cause: unknown) => cause);
  expect(error).toBeInstanceOf(RuleSaveFailure);
  expect(ruleFailureText(error)).toContain('could not be confirmed');
  expect(ruleFailureText(error)).toContain('lost response');
  expect(writes.ok).not.toHaveBeenCalled();
});

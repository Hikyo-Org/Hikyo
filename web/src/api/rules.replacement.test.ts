import { beforeEach, expect, it, vi } from 'vitest';

import { ALL, replacementSaveRefusal, type Rule } from '../routes/accessRules/model.ts';
import { ruleFailureText, saveRule } from './rules.ts';

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

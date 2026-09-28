import { describe, expect, it } from 'vitest';

import { makeWorld } from './fixture.ts';
import { accessDiff, allowed, availability, effective, MACHINE_FORBIDDEN_WHY, moveKey, perm, PRESETS, presetOf, reachOf, renameKey, requirement, resolve, type PermId, type World } from './model.ts';

const DB_PASSWORD = 'payments_k3';
const STRIPE_SECRET_KEY = 'payments_k5';

function ruleOf(world: World, id: number) {
  const rule = world.rules.find((r) => r.id === id);
  if (rule === undefined) throw new Error(`fixture has no rule ${id}`);
  return rule;
}

describe('access rules model', () => {
  it('an except narrows only its own rule', () => {
    const world = makeWorld();
    // Bob: Publisher except prod, plus See on prod.
    expect(resolve(world, 'bob', 'read', 'payments', 'prod', DB_PASSWORD).state).toBe('yes');
    const edit = resolve(world, 'bob', 'edit', 'payments', 'prod', DB_PASSWORD);
    expect(edit).toMatchObject({ state: 'excepted', why: 'except prod' });
    expect(resolve(world, 'bob', 'edit', 'payments', 'staging', DB_PASSWORD).state).toBe('yes');
  });

  it('a folder pick follows the folder: a key moved out leaves the rule', () => {
    const world = makeWorld();
    expect(resolve(world, 'alice', 'reveal', 'payments', 'prod', DB_PASSWORD).state).toBe('yes');
    const moved = moveKey(world, DB_PASSWORD, 'app');
    expect(resolve(moved, 'alice', 'reveal', 'payments', 'prod', DB_PASSWORD).state).toBe('no');
  });

  it('a single-key pick follows the key id through a rename and a move', () => {
    const world = makeWorld();
    expect(resolve(world, 'chen', 'reveal', 'payments', 'staging', STRIPE_SECRET_KEY).state).toBe('yes');
    const changed = moveKey(renameKey(world, STRIPE_SECRET_KEY, 'STRIPE_API_KEY'), STRIPE_SECRET_KEY, 'app');
    expect(resolve(changed, 'chen', 'reveal', 'payments', 'staging', STRIPE_SECRET_KEY).state).toBe('yes');
    // Edit was a folder pick on stripe/, so it stays with the folder.
    expect(resolve(changed, 'chen', 'edit', 'payments', 'staging', STRIPE_SECRET_KEY).state).toBe('no');
  });

  it('a key leaving an excepted folder widens access', () => {
    const world = makeWorld();
    expect(resolve(world, 'dana', 'reveal', 'payments', 'prod', DB_PASSWORD)).toMatchObject({ state: 'excepted', why: 'except db/' });
    const moved = moveKey(world, DB_PASSWORD, 'app');
    expect(resolve(moved, 'dana', 'reveal', 'payments', 'prod', DB_PASSWORD).state).toBe('yes');
  });

  it('the DB_PASSWORD move diff names who gains and who loses', () => {
    const world = makeWorld();
    const { gained, lost } = accessDiff(world, moveKey(world, DB_PASSWORD, 'app'), DB_PASSWORD);
    expect(gained).toEqual([
      { member: 'dana', perms: [{ perm: 'reveal', envs: ['prod'] }, { perm: 'reveal-history', envs: ['prod'] }] },
    ]);
    expect(lost).toEqual([
      {
        member: 'alice',
        perms: [
          { perm: 'reveal', envs: ['staging', 'prod'] },
          { perm: 'definitions-edit', envs: ['staging', 'prod'] },
          { perm: 'manage-members', envs: ['staging', 'prod'] },
        ],
      },
    ]);
  });

  it("a rename changes nobody's access", () => {
    const world = makeWorld();
    expect(accessDiff(world, renameKey(world, STRIPE_SECRET_KEY, 'STRIPE_API_KEY'), STRIPE_SECRET_KEY)).toEqual({ gained: [], lost: [] });
  });

  it('machines cannot hold management permissions or Pin, whatever the rule shape', () => {
    const wide = ruleOf(makeWorld(), 1);
    const forbidden: PermId[] = ['pin', 'manage-members', 'manage-identities', 'manage-adapters', 'project-settings', 'manage-projects'];
    const held: PermId[] = ['read', 'edit', 'publish', 'definitions-edit', 'reveal', 'reveal-history'];
    for (const id of forbidden) {
      expect(availability(id, wide, 'machine')).toEqual({ ok: false, why: MACHINE_FORBIDDEN_WHY });
      expect(allowed(id, wide, 'person')).toBe(true);
    }
    for (const id of held) {
      expect(allowed(id, wide, 'machine')).toBe(true);
    }
  });

  it('a permission row note depends on the member kind only, never on Where', () => {
    expect(requirement('pin')).toBe('Only on rules that cover all keys of an environment.');
    expect(requirement('edit')).toBeUndefined();
    expect(requirement('reveal', 'machine')).toBe("Needs the project's machine reveal opt-in.");
    expect(requirement('reveal')).toBeUndefined();
  });

  it('a machine rule drops what machines cannot hold, and resolves without it', () => {
    const world: World = {
      ...makeWorld(),
      rules: [{ id: 1, member: 'ci', perms: ['read', 'publish', 'pin', 'manage-members'], projects: ['payments'], envs: { mode: 'all', exc: [] }, keys: { mode: 'all', exc: [] } }],
    };
    const [rule] = world.rules;
    if (rule === undefined) throw new Error('no rule');
    expect(effective(rule, 'machine')).toEqual(['read', 'publish']);
    expect(resolve(world, 'ci', 'manage-members', 'payments', 'prod', DB_PASSWORD).state).toBe('no');
    expect(resolve(world, 'ci', 'publish', 'payments', 'prod', DB_PASSWORD).state).toBe('yes');
  });

  it('Reveal without See resolves to the needs-See state', () => {
    const world: World = {
      ...makeWorld(),
      rules: [{ id: 1, member: 'chen', perms: ['reveal'], projects: ['payments'], envs: { mode: 'only', list: ['prod'] }, keys: { mode: 'all', exc: [] } }],
    };
    expect(resolve(world, 'chen', 'reveal', 'payments', 'prod', DB_PASSWORD).state).toBe('needsSee');
    expect(resolve(world, 'chen', 'reveal-history', 'payments', 'prod', DB_PASSWORD).state).toBe('no');
  });

  it('allowed rejects permissions a narrow rule cannot carry', () => {
    const world = makeWorld();
    const folderRule = ruleOf(world, 3);
    expect(allowed('pin', folderRule)).toBe(false);
    expect(allowed('edit', folderRule)).toBe(true);
    const envRule = ruleOf(world, 4);
    expect(allowed('pin', envRule)).toBe(true);
    expect(allowed('manage-identities', envRule)).toBe(false);
    expect(allowed('manage-projects', ruleOf(world, 1))).toBe(true);
    expect(allowed('manage-projects', ruleOf(world, 2))).toBe(false);
  });

  it('the Admin preset carries no Secrets permission', () => {
    const admin = PRESETS.find((p) => p.name === 'Admin');
    expect(admin?.perms.length).toBeGreaterThan(0);
    expect(admin?.perms.some((id) => perm(id).group === 'Secrets')).toBe(false);
  });

  it('names a rule by its preset and counts its reach', () => {
    const world = makeWorld();
    expect(presetOf(ruleOf(world, 2))).toBe('Publisher');
    expect(presetOf(ruleOf(world, 3))).toBeNull();
    expect(effective(ruleOf(world, 3))).not.toContain('pin');
    expect(reachOf(world, ruleOf(world, 3))).toEqual({ environments: 2, keys: 3, secrets: 2, protectedEnvs: 1, grows: null });
  });
});

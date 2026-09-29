import { zRuleCapability } from '@hikyo/zod';
import { describe, expect, it } from 'vitest';

import { grantRows, IDS, makeWorld, PAYMENTS_KEYS } from './fixture.ts';
import {
  ALL,
  allowed,
  availability,
  createBody,
  effective,
  newRule,
  perm,
  PERMS,
  PRESETS,
  presetOf,
  reachOf,
  resolve,
  rulesFromGrants,
  savePlan,
  type Key,
  type PermId,
  type Rule,
  type World,
} from './model.ts';

const key = (id: string): Key => {
  const found = PAYMENTS_KEYS.find((k) => k.id === id);
  if (found === undefined) throw new Error(`no key ${id}`);
  return found;
};
const DB_PASSWORD = key(IDS.dbPassword);
const STRIPE_SECRET = key(IDS.stripeSecret);
const LOG_LEVEL = key(IDS.logLevel);
const { payments: PAY, payProd: PROD, payStaging: STAGING, payDev: DEV } = IDS;

const rulesOf = (world: World, member: string) => world.rules.filter((r) => r.member === member && r.source.kind === 'rule');

/** A world with exactly these rules for Chen, and nothing else. */
function only(rules: Omit<Rule, 'id' | 'member' | 'source'>[]): World {
  return {
    ...makeWorld(),
    rules: rules.map((r, i) => ({ ...r, id: `r${i}`, member: IDS.chen, source: { kind: 'rule', parts: [], otherProjects: false } })),
  };
}

const state = (world: World, member: string, id: PermId, env: string, k?: Key) => resolve(world, member, id, PAY, env, k).state;

describe('the vocabulary', () => {
  it('names exactly the capabilities the server accepts on a rule', () => {
    expect(PERMS.map((p) => p.id).sort()).toEqual([...zRuleCapability.options].sort());
  });

  it('the Admin preset carries no Secrets permission', () => {
    const admin = PRESETS.find((p) => p.name === 'Admin');
    expect(admin?.perms.length).toBeGreaterThan(0);
    expect(admin?.perms.some((id) => perm(id).group === 'Secrets')).toBe(false);
  });

  it('holds exactly the shapes the server refuses: See and Pin need all keys, three need a whole project', () => {
    const keyNarrowed = { envs: ALL, keys: { mode: 'only' as const, items: [{ project: PAY, folder: 'db' }] } };
    const envNarrowed = { envs: { mode: 'only' as const, items: [{ project: PAY, environment: PROD }] }, keys: ALL };
    for (const p of PERMS) {
      expect(allowed(p.id, keyNarrowed)).toBe(p.shape === 'key');
      expect(allowed(p.id, envNarrowed)).toBe(p.shape !== 'project');
      expect(allowed(p.id, { envs: ALL, keys: ALL })).toBe(true);
    }
    // An except narrows too: "all keys except db/" is key-narrowed.
    expect(availability('read', { envs: ALL, keys: { mode: 'all', items: [{ project: PAY, folder: 'db' }] } })).toEqual({
      ok: false,
      why: 'Not available here: needs all keys of an environment',
    });
  });
});

describe('from the listings', () => {
  it('groups server rules by person and Where, one permission each', () => {
    const world = makeWorld();
    const alice = rulesOf(world, IDS.alice);
    expect(alice.map((r) => r.perms)).toEqual([
      ['read', 'edit', 'publish', 'pin'],
      ['edit', 'publish', 'reveal', 'definitions-edit', 'manage-members'],
    ]);
    const [first] = alice;
    expect(first?.source.kind === 'rule' ? first.source.parts.length : 0).toBe(4);
    expect(presetOf(first ?? newRule(''))).toBe('Publisher');
  });

  it('maps grants to rules with no key limits: org to all projects, project to all environments, environment to itself', () => {
    const rules = rulesFromGrants([
      ...grantRows(),
      { id: 'g1', principal_id: 'prn_x', capability: 'edit', scope: { org_id: IDS.org, project_id: PAY }, origins: [], created_at: '2026-09-29T08:00:00Z' },
      { id: 'g2', principal_id: 'prn_x', capability: 'instance-config', scope: {}, origins: [], created_at: '2026-09-29T08:00:00Z' },
      { id: 'g3', principal_id: 'prn_x', capability: 'audit-read', scope: { org_id: IDS.org }, origins: [], created_at: '2026-09-29T08:00:00Z' },
    ]);
    const sam = rules.find((r) => r.member === IDS.sam);
    expect(sam).toMatchObject({ projects: '*', envs: ALL, keys: ALL });
    // manage-projects is not a rule permission, so it takes no part.
    expect(sam?.perms).not.toContain('manage-projects');
    expect(rules.find((r) => r.member === IDS.ci)).toMatchObject({ projects: [PAY], envs: { mode: 'only', items: [{ project: PAY, environment: STAGING }] }, keys: ALL });
    expect(rules.filter((r) => r.member === 'prn_x')).toEqual([expect.objectContaining({ projects: [PAY], envs: ALL, perms: ['edit'] })]);

    const world = { ...makeWorld(), rules };
    expect(state(world, IDS.sam, 'reveal', PROD, DB_PASSWORD)).toBe('yes');
    expect(resolve(world, IDS.sam, 'read', IDS.web, IDS.webProd, undefined).state).toBe('yes');
    expect(state(world, IDS.ci, 'publish', STAGING, DB_PASSWORD)).toBe('yes');
    expect(state(world, IDS.ci, 'publish', PROD, DB_PASSWORD)).toBe('no');
    expect(state(world, 'prn_x', 'edit', DEV, LOG_LEVEL)).toBe('yes');
  });
});

describe('evaluation, as the server decides it', () => {
  it('an except narrows only its own rule', () => {
    const world = makeWorld();
    // Bob: Publisher and Reveal except prod, plus See on prod.
    expect(state(world, IDS.bob, 'read', PROD, DB_PASSWORD)).toBe('yes');
    expect(resolve(world, IDS.bob, 'edit', PAY, PROD, DB_PASSWORD)).toMatchObject({ state: 'excepted', why: 'except prod' });
    expect(state(world, IDS.bob, 'edit', STAGING, DB_PASSWORD)).toBe('yes');
  });

  it('an only-environments rule reaches nothing in a project it names no environment of', () => {
    const world = only([{ perms: ['edit'], projects: [PAY, IDS.web], envs: { mode: 'only', items: [{ project: PAY, environment: PROD }] }, keys: ALL }]);
    expect(state(world, IDS.chen, 'edit', PROD, DB_PASSWORD)).toBe('yes');
    expect(resolve(world, IDS.chen, 'edit', IDS.web, IDS.webProd, undefined).state).toBe('no');
  });

  it('a folder except covers its subfolders; an only-pick matches the folder exactly', () => {
    const nested: Key = { id: 'key_nested', name: 'REPLICA_PASSWORD', folder: 'db/replica', secret: true };
    const except = only([{ perms: ['read', 'reveal'], projects: [PAY], envs: ALL, keys: ALL }, { perms: ['edit'], projects: [PAY], envs: ALL, keys: { mode: 'all', items: [{ project: PAY, folder: 'db' }] } }]);
    expect(resolve(except, IDS.chen, 'edit', PAY, PROD, nested)).toMatchObject({ state: 'excepted', why: 'except db/' });
    expect(state(except, IDS.chen, 'edit', PROD, DB_PASSWORD)).toBe('excepted');
    expect(state(except, IDS.chen, 'edit', PROD, LOG_LEVEL)).toBe('yes');

    const onlyPick = only([{ perms: ['edit'], projects: [PAY], envs: ALL, keys: { mode: 'only', items: [{ project: PAY, folder: 'db' }] } }]);
    expect(state(onlyPick, IDS.chen, 'edit', PROD, DB_PASSWORD)).toBe('yes');
    expect(state(onlyPick, IDS.chen, 'edit', PROD, nested)).toBe('no');

    // The root folder has no subfolders: excepting it leaves out only root keys.
    const root: Key = { id: 'key_root', name: 'ROOT', folder: '', secret: false };
    const rootExcept = only([{ perms: ['edit'], projects: [PAY], envs: ALL, keys: { mode: 'all', items: [{ project: PAY, folder: '' }] } }]);
    expect(state(rootExcept, IDS.chen, 'edit', PROD, root)).toBe('excepted');
    expect(state(rootExcept, IDS.chen, 'edit', PROD, DB_PASSWORD)).toBe('yes');
  });

  it('a single-key pick follows the key id through a rename and a move', () => {
    const world = makeWorld();
    expect(state(world, IDS.chen, 'reveal', STAGING, STRIPE_SECRET)).toBe('yes');
    const moved: Key = { ...STRIPE_SECRET, name: 'STRIPE_API_KEY', folder: 'app' };
    expect(state(world, IDS.chen, 'reveal', STAGING, moved)).toBe('yes');
    // Edit was a folder pick on stripe/, so it stays with the folder.
    expect(state(world, IDS.chen, 'edit', STAGING, STRIPE_SECRET)).toBe('yes');
    expect(state(world, IDS.chen, 'edit', STAGING, moved)).toBe('no');
  });

  it('a key leaving an excepted folder widens access', () => {
    const world = makeWorld();
    expect(resolve(world, IDS.dana, 'reveal', PAY, PROD, DB_PASSWORD)).toMatchObject({ state: 'excepted', why: 'except db/' });
    expect(state(world, IDS.dana, 'reveal', PROD, { ...DB_PASSWORD, folder: 'app' })).toBe('yes');
  });

  it('a key-narrowed rule counts only for a permission checked against one key', () => {
    const world = makeWorld();
    // Dana's Reveal history excepts db/; no operation checks history per key, so it never counts.
    expect(resolve(world, IDS.dana, 'reveal-history', PAY, PROD, LOG_LEVEL)).toMatchObject({ state: 'excepted' });
    // Asked for the whole environment, Chen's single-key Reveal does not count.
    expect(state(world, IDS.chen, 'reveal', STAGING, undefined)).toBe('excepted');
    expect(state(world, IDS.chen, 'reveal', STAGING, STRIPE_SECRET)).toBe('yes');
  });

  it('Manage access on a rule is inert; on a grant it counts', () => {
    const world = makeWorld();
    const aliceFolder = rulesOf(world, IDS.alice)[1];
    expect(aliceFolder?.perms).toContain('manage-members');
    expect(state(world, IDS.alice, 'manage-members', PROD, DB_PASSWORD)).toBe('no');
    expect(state(world, IDS.sam, 'manage-members', PROD, DB_PASSWORD)).toBe('yes');
  });

  it('Define keys needs every environment of the project', () => {
    const world = makeWorld();
    // Alice's folder rule names only staging and prod, so it never defines keys.
    expect(state(world, IDS.alice, 'definitions-edit', PROD, DB_PASSWORD)).toBe('no');
    const folder = only([{ perms: ['definitions-edit'], projects: [PAY], envs: ALL, keys: { mode: 'only', items: [{ project: PAY, folder: 'db' }] } }]);
    expect(state(folder, IDS.chen, 'definitions-edit', DEV, DB_PASSWORD)).toBe('yes');
    expect(state(folder, IDS.chen, 'definitions-edit', DEV, LOG_LEVEL)).toBe('no');
    const excepted = only([{ perms: ['definitions-edit'], projects: [PAY], envs: { mode: 'all', items: [{ project: PAY, environment: PROD }] }, keys: ALL }]);
    expect(resolve(excepted, IDS.chen, 'definitions-edit', PAY, DEV, LOG_LEVEL)).toMatchObject({ state: 'excepted', why: 'except prod' });
  });

  it('showing a secret needs See in the same environment, from a rule or a grant', () => {
    const world = makeWorld();
    // Alice's folder rule gives Reveal on db/ and her Publisher rule gives See.
    expect(state(world, IDS.alice, 'reveal', PROD, DB_PASSWORD)).toBe('yes');
    const blind = only([{ perms: ['reveal'], projects: [PAY], envs: ALL, keys: ALL }]);
    expect(state(blind, IDS.chen, 'reveal', PROD, DB_PASSWORD)).toBe('needsSee');
    const see = { principal_id: IDS.chen, capability: 'read', scope: { org_id: IDS.org, project_id: PAY } };
    const withGrant: World = { ...blind, rules: [...blind.rules, ...rulesFromGrants([see])] };
    expect(state(withGrant, IDS.chen, 'reveal', PROD, DB_PASSWORD)).toBe('yes');
  });

  it('a rule drops what its shape cannot carry', () => {
    const narrowed = { perms: ['read', 'pin', 'edit'] satisfies PermId[], envs: ALL, keys: { mode: 'only' as const, items: [{ project: PAY, folder: 'db' }] } };
    expect(effective(narrowed)).toEqual(['edit']);
  });
});

describe('reach and saving', () => {
  it('counts what a rule reaches, and says when keys cannot be counted', () => {
    const world = makeWorld();
    const aliceFolder = rulesOf(world, IDS.alice)[1];
    if (aliceFolder === undefined) throw new Error('no rule');
    expect(reachOf(world, aliceFolder)).toEqual({ environments: 2, keys: 3, secrets: 2, protectedEnvs: 1, grows: null });
    expect(reachOf(makeWorld({ readable: false }), aliceFolder)).toMatchObject({ environments: 2, keys: null, secrets: null });
  });

  it('with the Where unchanged, a save moves only the permissions that changed', () => {
    const world = makeWorld();
    const [publisher] = rulesOf(world, IDS.alice);
    if (publisher === undefined || publisher.source.kind !== 'rule') throw new Error('no rule');
    const pinId = publisher.source.parts.find((p) => p.perm === 'pin')?.id;
    expect(savePlan(publisher, { ...publisher, perms: ['read', 'edit', 'publish', 'reveal'] })).toEqual({ create: ['reveal'], revoke: [pinId] });
  });

  it('with a new Where, a save creates the whole rule and revokes the old one', () => {
    const world = makeWorld();
    const [publisher] = rulesOf(world, IDS.alice);
    if (publisher === undefined || publisher.source.kind !== 'rule') throw new Error('no rule');
    const draft: Rule = { ...publisher, envs: { mode: 'all', items: [{ project: PAY, environment: PROD }] } };
    expect(savePlan(publisher, draft)).toEqual({ create: ['read', 'edit', 'publish', 'pin'], revoke: publisher.source.parts.map((p) => p.id) });
    expect(savePlan(null, { ...newRule(IDS.chen), projects: [PAY] })).toEqual({ create: ['read'], revoke: [] });
  });

  it('spells the create body the way the API reads it', () => {
    const draft: Rule = {
      ...newRule(IDS.chen),
      perms: ['edit'],
      projects: [PAY],
      envs: { mode: 'only', items: [{ project: PAY, environment: STAGING }] },
      keys: { mode: 'all', items: [{ project: PAY, folder: 'db' }, { project: PAY, key: IDS.stripeSecret }] },
    };
    expect(createBody(draft, 'edit')).toEqual({
      principal: IDS.chen,
      capability: 'edit',
      where: {
        projects: [PAY],
        environments: { mode: 'only', items: [{ project: PAY, environment: STAGING }] },
        keys: { mode: 'all', items: [{ project: PAY, folder: 'db' }, { project: PAY, key: IDS.stripeSecret }] },
      },
    });
  });
});

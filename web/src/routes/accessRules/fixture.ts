import { addKey, ALL, keyPick, moveKey, PERMS, renameKey, type Axis, type Key, type PermId, type Rule, type World } from './model.ts';

/**
 * The organisation from prototype iteration 6: six members, two projects,
 * eleven keys, ten rules. A function, so every story and test gets its own
 * copy. Key ids are stable (`payments_k3` is DB_PASSWORD, `payments_k5`
 * STRIPE_SECRET_KEY); renames and moves keep them.
 */
export function makeWorld(): World {
  const only = (list: string[]): Axis => ({ mode: 'only', list });
  const except = (exc: string[]): Axis => ({ mode: 'all', exc });
  const keys = (project: string, list: [name: string, folder: string, secret: boolean][]): Key[] =>
    list.map(([name, folder, secret], i) => ({ id: `${project}_k${i + 1}`, project, name, folder, secret }));
  const rules: Omit<Rule, 'id'>[] = [
    { member: 'marc', perms: PERMS.map((p) => p.id), projects: '*', envs: ALL, keys: ALL },
    { member: 'alice', perms: ['read', 'edit', 'publish', 'pin'], projects: ['payments'], envs: ALL, keys: ALL },
    { member: 'alice', perms: ['read', 'edit', 'publish', 'reveal', 'definitions-edit', 'manage-members'], projects: ['payments'], envs: only(['staging', 'prod']), keys: only(['db']) },
    { member: 'bob', perms: ['read', 'edit', 'publish', 'pin', 'reveal'], projects: ['payments', 'web'], envs: except(['prod']), keys: ALL },
    { member: 'bob', perms: ['read'], projects: ['payments', 'web'], envs: only(['prod']), keys: ALL },
    { member: 'chen', perms: ['read', 'edit'], projects: ['payments'], envs: only(['staging']), keys: only(['stripe']) },
    { member: 'chen', perms: ['reveal'], projects: ['payments'], envs: only(['staging']), keys: only([keyPick('payments_k5')]) },
    { member: 'dana', perms: ['read'], projects: '*', envs: ALL, keys: ALL },
    { member: 'dana', perms: ['read', 'reveal', 'reveal-history'], projects: ['payments'], envs: only(['prod']), keys: except(['db']) },
    { member: 'ci', perms: ['read', 'edit', 'publish'], projects: ['payments'], envs: only(['staging']), keys: ALL },
  ];
  return {
    people: [
      { id: 'marc', name: 'Marc Went', handle: 'usr_01marc', note: 'owner' },
      { id: 'alice', name: 'Alice Novak', handle: 'usr_01alice', note: 'DBA' },
      { id: 'bob', name: 'Bob Tran', handle: 'usr_01bob', note: 'app developer' },
      { id: 'chen', name: 'Chen Li', handle: 'usr_01chen', note: 'contractor, Stripe integration' },
      { id: 'dana', name: 'Dana Ruiz', handle: 'usr_01dana', note: 'SRE' },
      { id: 'ci', name: 'ci-deploy', handle: 'mch_01cideploy', note: 'automation credential' },
    ],
    projects: [
      { id: 'payments', envs: [{ id: 'dev', protected: false }, { id: 'staging', protected: false }, { id: 'prod', protected: true }] },
      { id: 'web', envs: [{ id: 'dev', protected: false }, { id: 'prod', protected: true }] },
    ],
    keys: [
      ...keys('payments', [
        ['DB_HOST', 'db', false],
        ['DB_USER', 'db', false],
        ['DB_PASSWORD', 'db', true],
        ['STRIPE_PUBLISHABLE_KEY', 'stripe', false],
        ['STRIPE_SECRET_KEY', 'stripe', true],
        ['STRIPE_WEBHOOK_SECRET', 'stripe', true],
        ['LOG_LEVEL', 'app', false],
        ['SESSION_SECRET', 'app', true],
      ]),
      ...keys('web', [
        ['API_URL', 'app', false],
        ['SENTRY_DSN', 'app', true],
        ['COOKIE_SECRET', 'app', true],
      ]),
    ],
    rules: rules.map((rule, i) => ({ ...rule, id: i + 1 })),
  };
}

/** A definitions change the simulator offers. Adds never ask: a new key only joins a folder. */
export type DefinitionsChange = {
  readonly id: string;
  readonly label: string;
  readonly keyId: string;
  readonly isAdd: boolean;
  readonly apply: (world: World) => World;
};

export const CHANGES: readonly DefinitionsChange[] = [
  {
    id: 'add-replica',
    label: 'Alice adds DB_REPLICA_URL (secret) to db/',
    keyId: 'payments_k9',
    isAdd: true,
    apply: (w) => addKey(w, { id: 'payments_k9', project: 'payments', name: 'DB_REPLICA_URL', folder: 'db', secret: true }),
  },
  { id: 'move-db-password', label: 'Move DB_PASSWORD from db/ to app/', keyId: 'payments_k3', isAdd: false, apply: (w) => moveKey(w, 'payments_k3', 'app') },
  { id: 'rename-stripe', label: 'Rename STRIPE_SECRET_KEY to STRIPE_API_KEY', keyId: 'payments_k5', isAdd: false, apply: (w) => renameKey(w, 'payments_k5', 'STRIPE_API_KEY') },
  { id: 'move-stripe', label: 'Move STRIPE_SECRET_KEY from stripe/ to app/', keyId: 'payments_k5', isAdd: false, apply: (w) => moveKey(w, 'payments_k5', 'app') },
];

/** The question Who can...? opens on. */
export const DEFAULT_QUESTION: { perm: PermId; project: string; env: string; key: string } = {
  perm: 'reveal',
  project: 'payments',
  env: 'prod',
  key: 'payments_k3',
};

import { ALL, keyPick, PERMS, type Axis, type Key, type PermId, type Rule, type World } from './model.ts';

/**
 * The organisation from prototype iteration 6: five people and two
 * machines, two projects, eleven keys, eleven rules. A function, so every story and test gets its own
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
    { member: 'deploy-prod', perms: ['read', 'reveal'], projects: ['payments'], envs: only(['prod']), keys: ALL },
  ];
  return {
    people: [
      { id: 'marc', kind: 'person', name: 'Marc Went', handle: 'usr_01marc', note: 'owner' },
      { id: 'alice', kind: 'person', name: 'Alice Novak', handle: 'usr_01alice', note: 'DBA' },
      { id: 'bob', kind: 'person', name: 'Bob Tran', handle: 'usr_01bob', note: 'app developer' },
      { id: 'chen', kind: 'person', name: 'Chen Li', handle: 'usr_01chen', note: 'contractor, Stripe integration' },
      { id: 'dana', kind: 'person', name: 'Dana Ruiz', handle: 'usr_01dana', note: 'SRE' },
      { id: 'ci', kind: 'machine', name: 'ci-deploy', handle: 'mch_01cideploy', note: 'automation credential, CI apply' },
      { id: 'deploy-prod', kind: 'machine', name: 'deploy-prod', handle: 'mch_01deployprod', note: 'workload credential, payments prod deploys' },
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

/** The question Who can...? opens on. */
export const DEFAULT_QUESTION: { perm: PermId; project: string; env: string; key: string } = {
  perm: 'reveal',
  project: 'payments',
  env: 'prod',
  key: 'payments_k3',
};

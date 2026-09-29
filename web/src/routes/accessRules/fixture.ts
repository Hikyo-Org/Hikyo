import type { zGrantList, zRuleList } from '@hikyo/zod';
import type { z } from 'zod';

import { ORG } from '../../testkit/ids.ts';
import { rulesFromGrants, rulesFromServer, type Key, type World } from './model.ts';

/**
 * A small organisation in the listings' own shapes (the rule and grant
 * listings, the topology, the key catalogues), turned into a World by the same
 * mapping the Members page uses. Stories and tests share it; every call builds
 * a fresh copy.
 *
 * Two projects: payments (dev, staging, prod, protected) and web (dev, prod,
 * protected). Sam holds organisation grants; Alice, Bob, Chen and Dana hold
 * rules; two machines hold environment grants.
 */

/** A contract-valid id: prefix, then a uuid ending in `n`. */
const id = (prefix: string, n: number) => `${prefix}_123e4567-e89b-12d3-a456-426614174${String(n).padStart(3, '0')}`;

export const IDS = {
  org: ORG,
  payments: id('prj', 501),
  web: id('prj', 502),
  payDev: id('env', 511),
  payStaging: id('env', 512),
  payProd: id('env', 513),
  webDev: id('env', 521),
  webProd: id('env', 522),
  sam: id('prn', 531),
  alice: id('prn', 532),
  bob: id('prn', 533),
  chen: id('prn', 534),
  dana: id('prn', 535),
  ci: id('mch', 541),
  deploy: id('mch', 542),
  /** payments db/DB_PASSWORD, secret. */
  dbPassword: id('key', 553),
  /** payments stripe/STRIPE_SECRET_KEY, secret. */
  stripeSecret: id('key', 555),
  /** payments app/LOG_LEVEL, config. */
  logLevel: id('key', 557),
} as const;

const keys = (base: number, list: [name: string, folder: string, secret: boolean][]): Key[] =>
  list.map(([name, folder, secret], i) => ({ id: id('key', base + i + 1), name, folder, secret }));

export const PAYMENTS_KEYS = keys(550, [
  ['DB_HOST', 'db', false],
  ['DB_USER', 'db', false],
  ['DB_PASSWORD', 'db', true],
  ['STRIPE_PUBLISHABLE_KEY', 'stripe', false],
  ['STRIPE_SECRET_KEY', 'stripe', true],
  ['STRIPE_WEBHOOK_SECRET', 'stripe', true],
  ['LOG_LEVEL', 'app', false],
  ['SESSION_SECRET', 'app', true],
]);

export const WEB_KEYS = keys(560, [
  ['API_URL', 'app', false],
  ['SENTRY_DSN', 'app', true],
  ['COOKIE_SECRET', 'app', true],
]);

type RuleRow = z.input<typeof zRuleList>['items'][number];
type GrantRow = z.input<typeof zGrantList>['items'][number];

const NAMES: Record<string, string> = {
  [IDS.sam]: 'Sam Ortiz',
  [IDS.alice]: 'Alice Novak',
  [IDS.bob]: 'Bob Tran',
  [IDS.chen]: 'Chen Li',
  [IDS.dana]: 'Dana Ruiz',
  [IDS.ci]: 'ci-deploy',
  [IDS.deploy]: 'deploy-prod',
};

const env = (project: string, environment: string) => ({ project, environment });

let seq = 0;
function rule(principal: string, capabilities: RuleRow['capability'][], where: RuleRow['where']): RuleRow[] {
  return capabilities.map((capability) => {
    seq += 1;
    return {
      id: id('rul', 100 + seq),
      principal_id: principal,
      principal_name: NAMES[principal],
      capability,
      where,
      other_projects: false,
      created_by: IDS.sam,
      created_at: '2026-09-29T08:00:00Z',
    };
  });
}

function grant(principal: string, capability: string, scope: GrantRow['scope']): GrantRow {
  seq += 1;
  return {
    id: id('grn', 200 + seq),
    principal_id: principal,
    principal_name: NAMES[principal],
    capability,
    scope,
    origins: [{ kind: 'manual', subject: IDS.sam }],
    created_at: '2026-09-29T08:00:00Z',
  };
}

/** The rule listing as `GET /orgs/{org}/rules` answers it. */
export function ruleRows(): RuleRow[] {
  seq = 0;
  const pay = IDS.payments;
  const all = () => ({ mode: 'all' as const, items: [] });
  return [
    ...rule(IDS.alice, ['read', 'edit', 'publish', 'pin'], { projects: [pay], environments: all(), keys: all() }),
    ...rule(IDS.alice, ['edit', 'reveal', 'definitions-edit', 'manage-members'], {
      projects: [pay],
      environments: { mode: 'only', items: [env(pay, IDS.payStaging), env(pay, IDS.payProd)] },
      keys: { mode: 'only', items: [{ project: pay, folder: 'db' }] },
    }),
    ...rule(IDS.bob, ['read', 'edit', 'publish', 'pin', 'reveal'], {
      projects: [pay, IDS.web],
      environments: { mode: 'all', items: [env(pay, IDS.payProd), env(IDS.web, IDS.webProd)] },
      keys: all(),
    }),
    ...rule(IDS.bob, ['read'], {
      projects: [pay, IDS.web],
      environments: { mode: 'only', items: [env(pay, IDS.payProd), env(IDS.web, IDS.webProd)] },
      keys: all(),
    }),
    ...rule(IDS.chen, ['read'], { projects: [pay], environments: { mode: 'only', items: [env(pay, IDS.payStaging)] }, keys: all() }),
    ...rule(IDS.chen, ['edit'], {
      projects: [pay],
      environments: { mode: 'only', items: [env(pay, IDS.payStaging)] },
      keys: { mode: 'only', items: [{ project: pay, folder: 'stripe' }] },
    }),
    ...rule(IDS.chen, ['reveal'], {
      projects: [pay],
      environments: { mode: 'only', items: [env(pay, IDS.payStaging)] },
      keys: { mode: 'only', items: [{ project: pay, key: IDS.stripeSecret }] },
    }),
    ...rule(IDS.dana, ['read'], { projects: [pay, IDS.web], environments: all(), keys: all() }),
    ...rule(IDS.dana, ['reveal', 'reveal-history'], {
      projects: [pay],
      environments: { mode: 'only', items: [env(pay, IDS.payProd)] },
      keys: { mode: 'all', items: [{ project: pay, folder: 'db' }] },
    }),
  ];
}

/** The grant listing as `GET /orgs/{org}/grants` answers it. */
export function grantRows(): GrantRow[] {
  seq = 0;
  const org = { org_id: IDS.org };
  const staging = { org_id: IDS.org, project_id: IDS.payments, environment_id: IDS.payStaging };
  const prod = { org_id: IDS.org, project_id: IDS.payments, environment_id: IDS.payProd };
  return [
    ...(['read', 'edit', 'publish', 'pin', 'reveal', 'reveal-history', 'definitions-edit', 'manage-members', 'manage-projects'] as const).map((c) => grant(IDS.sam, c, org)),
    ...(['read', 'edit', 'publish'] as const).map((c) => grant(IDS.ci, c, staging)),
    ...(['read', 'reveal'] as const).map((c) => grant(IDS.deploy, c, prod)),
  ];
}

/** The topology: two projects, prod protected in both. */
export const TOPOLOGY = [
  {
    id: IDS.payments,
    name: 'payments',
    envs: [
      { id: IDS.payDev, name: 'dev', protected: false },
      { id: IDS.payStaging, name: 'staging', protected: false },
      { id: IDS.payProd, name: 'prod', protected: true },
    ],
    keys: PAYMENTS_KEYS,
  },
  {
    id: IDS.web,
    name: 'web',
    envs: [
      { id: IDS.webDev, name: 'dev', protected: false },
      { id: IDS.webProd, name: 'prod', protected: true },
    ],
    keys: WEB_KEYS,
  },
] as const;

/** The org, as the Members page builds it from the listings. `readable: false` leaves the catalogues unread. */
export function makeWorld({ readable = true }: { readable?: boolean } = {}): World {
  const rules = ruleRows();
  const grants = grantRows();
  const principals = [...new Set([...grants.map((g) => g.principal_id), ...rules.map((r) => r.principal_id)])];
  return {
    people: principals.map((id) => ({ id, kind: id.startsWith('mch_') ? 'machine' : 'person', name: NAMES[id] ?? id })),
    projects: TOPOLOGY.map((p) => ({ ...p, keys: readable ? p.keys : null })),
    rules: [...rulesFromServer(rules), ...rulesFromGrants(grants)],
  };
}

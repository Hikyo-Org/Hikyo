import type { Environment, Grant, Key } from '@hikyo/client';
import { DEMO_ACCOUNTS, DEMO_IDS, type DemoRole } from './config.ts';

export const fixtureTime = '2026-10-10T10:00:00Z';
export const principalIds: Record<DemoRole, string> = {
  admin: 'prn_11111111-1111-4111-8111-111111111111',
  developer: 'prn_44444444-4444-4444-8444-444444444444',
  viewer: 'prn_55555555-5555-4555-8555-555555555555',
};
export const org = { id: DEMO_IDS.org, name: 'Acme', active: true, metadata: null, created_at: fixtureTime, origin: 'manual' };
export const project = { id: DEMO_IDS.project, org_id: DEMO_IDS.org, name: 'Payments', created_at: fixtureTime, can_manage_policy: false, can_delete: false };
export const environments: Environment[] = [
  { id: DEMO_IDS.development, name: 'development' },
  { id: DEMO_IDS.staging, name: 'staging' },
  { id: DEMO_IDS.production, name: 'production' },
].map((environment, index) => ({ ...environment, org_id: org.id, project_id: project.id, display_order: index, created_at: fixtureTime }));

// The same six sample keys as the visual prototype, with deliberately fake material.
const seeds: { name: string; classification: 'config' | 'secret'; folder: string; description: string }[] = [
  { name: 'LOG_LEVEL', classification: 'config', folder: 'app', description: 'Application log verbosity.' },
  { name: 'FEATURE_CHECKOUT', classification: 'config', folder: 'app', description: 'Enable the new checkout flow.' },
  { name: 'PUBLIC_APP_URL', classification: 'config', folder: 'app', description: 'Canonical browser origin.' },
  { name: 'DATABASE_URL', classification: 'secret', folder: 'db', description: 'Connection string used by the API. Demo data only.' },
  { name: 'REDIS_URL', classification: 'secret', folder: 'db', description: 'Shared cache endpoint. Demo data only.' },
  { name: 'AUTH_SECRET', classification: 'secret', folder: 'auth', description: 'Session signing secret. Demo data only.' },
];
export const keys: Key[] = seeds.map((seed, index) => ({
  id: `key_11111111-1111-4111-8111-${String(index + 1).padStart(12, '0')}`,
  org_id: org.id, project_id: project.id, name: seed.name, classification: seed.classification,
  folder_path: seed.folder, description: seed.description, deprecated: false, deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: false } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '', created_at: fixtureTime,
}));
export const folders = ['app', 'db', 'auth'].map((path, index) => ({
  id: `fld_11111111-1111-4111-8111-${String(index + 1).padStart(12, '0')}`,
  org_id: org.id, project_id: project.id, path, created_at: fixtureTime,
}));
export const retention = { mode: 'keep-if-either', max_age_seconds: 2_592_000, last_revisions: 10 };
export const grants: Grant[] = DEMO_ACCOUNTS.flatMap((account) => {
  const scope = { org_id: org.id, project_id: project.id };
  const capabilities: Grant['capability'][] = account.id === 'admin' ? ['read', 'edit', 'publish', 'reveal'] : ['read'];
  const rows = capabilities.map((capability) => ({ capability, scope }));
  if (account.id === 'developer') {
    rows.push(...[DEMO_IDS.development, DEMO_IDS.staging].map((environment_id) => ({ capability: 'edit', scope: { ...scope, environment_id } })));
  }
  return rows.map((row, index) => ({
    id: `grn_${account.id === 'admin' ? '11111111' : account.id === 'developer' ? '44444444' : '55555555'}-1111-4111-8111-${String(index + 1).padStart(12, '0')}`,
    principal_id: principalIds[account.id], principal_name: account.name, ...row,
    origins: [{ kind: 'manual', subject: principalIds[account.id] }], created_at: fixtureTime,
  }));
});

export function initialValue(environmentId: string, keyName: string): string | undefined {
  const environment = environments.find((item) => item.id === environmentId)?.name;
  if (environment === undefined) return undefined;
  switch (keyName) {
    case 'LOG_LEVEL': return environment === 'development' ? 'debug' : environment === 'staging' ? 'info' : 'warn';
    case 'FEATURE_CHECKOUT': return environment === 'production' ? 'false' : 'true';
    case 'PUBLIC_APP_URL': return `https://${environment}.example.test`;
    case 'DATABASE_URL': return `postgresql://demo:demo-only@database.example.test/${environment}`;
    case 'REDIS_URL': return `redis://cache.example.test/${environment === 'production' ? '0' : '1'}`;
    case 'AUTH_SECRET': return environment === 'production' ? undefined : `DEMO_ONLY_${environment}_not_a_real_secret`;
    default: return undefined;
  }
}

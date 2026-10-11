/** Build-time boundary. Demo fixtures and startup are imported only in demo mode. */
export const isDemo = import.meta.env.MODE === 'demo';

export type DemoRole = 'viewer' | 'developer' | 'admin';

export const DEMO_ACCOUNTS: readonly {
  readonly id: DemoRole;
  readonly label: string;
  readonly name: string;
  readonly description: string;
}[] = [
  { id: 'viewer', label: 'Viewer', name: 'Sam Rivera', description: 'Browse config values. Secrets stay masked; editing and publishing are disabled.' },
  { id: 'developer', label: 'Developer', name: 'Dana Chen', description: 'Edit and stage development and staging values. Publishing and secret reveal are disabled.' },
  { id: 'admin', label: 'Demo admin', name: 'Alex Lee', description: 'Edit, publish and reveal sample values in every environment. Settings remain read-only.' },
];

export const DEMO_IDS = {
  org: 'org_11111111-1111-4111-8111-111111111111',
  project: 'prj_11111111-1111-4111-8111-111111111111',
  development: 'env_11111111-1111-4111-8111-111111111111',
  staging: 'env_22222222-2222-4222-8222-222222222222',
  production: 'env_33333333-3333-4333-8333-333333333333',
};

export function demoRole(search: string = globalThis.location?.search ?? ''): DemoRole {
  const account = new URLSearchParams(search).get('account');
  return account === 'viewer' || account === 'admin' ? account : 'developer';
}

export function demoCanEdit(role: DemoRole, environment: string): boolean {
  return role === 'admin' || (role === 'developer' && environment !== DEMO_IDS.production);
}

export const DEMO_MATRIX_PATH = `/orgs/${DEMO_IDS.org}/projects/${DEMO_IDS.project}/matrix`;

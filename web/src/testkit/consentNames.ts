/** Fixtures for the consent screens' name lookups: org, project, environments and the key catalogue. */
const at = '2026-01-01T00:00:00Z';
const id = (prefix: string, n: number) => `${prefix}_00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}`;

export const consentIds = {
  org: id('org', 1),
  project: id('prj', 1),
  environment: id('env', 1),
  secretKey: id('key', 1),
  configKey: id('key', 2),
  /** Bound by the transaction but absent from the catalogue. */
  missingKey: id('key', 3),
};

const keyRow = (keyId: string, name: string, classification: 'secret' | 'config') => ({
  id: keyId, org_id: consentIds.org, project_id: consentIds.project, name, folder_path: '', classification,
  description: '', deprecated: false, deprecation_note: '', declaration: { rule: { type: 'string' } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } }, group_id: '', created_at: at,
});

export type ConsentLookupFailures = { keys?: boolean; environments?: boolean; org?: boolean; project?: boolean };

/**
 * The response for one lookup path, or undefined when the path is not a lookup.
 * `keys` may be a promise to hold the catalogue open; a flagged failure answers 403.
 */
export function consentLookup(
  path: string,
  failures: ConsentLookupFailures = {},
  keys: () => Promise<void> = () => Promise.resolve(),
): Promise<Response> | undefined {
  const denied = () => Response.json({ error: { code: 'forbidden', message: 'Denied' } }, { status: 403 });
  if (path.endsWith('/keys')) {
    return keys().then(() => failures.keys === true ? denied() : Response.json({
      count: 2, schema_revision: 1,
      items: [keyRow(consentIds.secretKey, 'DATABASE_URL', 'secret'), keyRow(consentIds.configKey, 'LOG_LEVEL', 'config')],
    }));
  }
  if (path.endsWith('/environments')) {
    return Promise.resolve(failures.environments === true ? denied() : Response.json({
      count: 1, items: [{ id: consentIds.environment, org_id: consentIds.org, project_id: consentIds.project, name: 'production', display_order: 0, created_at: at }],
    }));
  }
  if (path.endsWith(`/projects/${consentIds.project}`)) {
    return Promise.resolve(failures.project === true ? denied() : Response.json({ id: consentIds.project, org_id: consentIds.org, name: 'Billing', created_at: at }));
  }
  if (path.endsWith(`/orgs/${consentIds.org}`)) {
    return Promise.resolve(failures.org === true ? denied() : Response.json({ id: consentIds.org, name: 'Acme', active: true, created_at: at, origin: 'manual' }));
  }
  return undefined;
}

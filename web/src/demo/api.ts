import { client } from '@hikyo/runtime';
import type { Key, Revision } from '@hikyo/client';
import { zPublishRequest, zSetValueRequest } from '@hikyo/zod';
import { ZodError } from 'zod';
import { createReadOnlyFixtures } from './readOnlyFixtures.ts';
import { DEMO_ACCOUNTS, DEMO_IDS, demoCanEdit, type DemoRole } from './config.ts';
import { environments, fixtureTime, folders, grants, initialValue, keys, org, principalIds, project, retention } from './fixtures.ts';

type Draft = { version_id: string; environment_id: string; key: Key; operation: 'set' | 'unset'; value?: string; staged_from_revision: number; created_at: string };
type Options = { origin?: string; assetFetch?: typeof fetch };
const orgRoot = `/api/v1/orgs/${DEMO_IDS.org}`;
const projectRoot = `${orgRoot}/projects/${DEMO_IDS.project}`;
const sessionExpiry = '9999-12-31T23:59:59Z';
const revealWindowSeconds = 900;

function json(body: object, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' } });
}
function refusal(status: number, detail: string): Response {
  return json({ error: { code: status === 400 ? 'bad_request' : status === 404 ? 'not_found' : status === 409 ? 'conflict' : 'forbidden', message: detail, detail } }, status);
}
function list<T>(items: T[]) { return { items, count: items.length }; }
function pending(draft: Draft) {
  return { version_id: draft.version_id, key_id: draft.key.id, name: draft.key.name, classification: draft.key.classification, operation: draft.operation, staged_from_revision: draft.staged_from_revision, created_at: draft.created_at };
}

/** Each call owns one tab's disposable state. Every API request terminates here. */
export function createDemoFetch(role: DemoRole, options: Options = {}): typeof fetch {
  const origin = new URL(options.origin ?? globalThis.location?.origin ?? 'https://demo.example.test').origin;
  const account = DEMO_ACCOUNTS.find((candidate) => candidate.id === role);
  if (account === undefined) throw new Error('Unknown demo account');
  const sessionTime = new Date().toISOString();
  const readOnlyFixture = createReadOnlyFixtures(role, sessionTime);
  const drafts = new Map<string, Draft>();
  const published = new Map<string, string | undefined>();
  const revisions = new Map(environments.map((environment) => [environment.id, 1]));
  const histories = new Map<string, Revision[]>(environments.map((environment) => [environment.id, [{
    revision: 1, schema_revision: 1, published_by: principalIds.admin, published_by_name: 'Alex Lee', published_at: fixtureTime,
    changed_keys: keys.filter((key) => initialValue(environment.id, key.name) !== undefined).map((key) => ({ key_id: key.id, name: key.name, change: 'added' })), payload_present: true,
  }]]));
  const snapshots = new Map(environments.map((environment) => [
    `${environment.id}:1`, keys.filter((key) => initialValue(environment.id, key.name) !== undefined),
  ]));
  let sequence = 0;
  const value = (environmentId: string, key: Key) => {
    const address = `${environmentId}:${key.id}`;
    return published.has(address) ? published.get(address) : initialValue(environmentId, key.name);
  };
  const cell = (environmentId: string, key: Key, reveal = false) => {
    const material = value(environmentId, key);
    const revealed = material !== undefined && (key.classification === 'config' || reveal);
    return { key_id: key.id, name: key.name, classification: key.classification, set: material !== undefined, revealed,
      ...(revealed ? { value: material } : {}), ...(material !== undefined ? { updated_at: fixtureTime, updated_by: 'Alex Lee' } : {}) };
  };
  const whoami = {
    session: { id: `ses_${principalIds[role].slice(4)}`, artifact: 'browser', created_at: sessionTime, idle_expires_at: sessionExpiry, absolute_expires_at: sessionExpiry,
      assurance: { method: 'local-password+totp', factors: ['password', 'totp'], authenticated_at: sessionTime } },
    principal: { id: principalIds[role], kind: 'human', display_name: account.name },
    capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } },
  };
  return async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : input.toString(), origin);
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    const path = url.pathname;
    const api = path === '/api' || path.startsWith('/api/');
    if (!api) {
      if (url.origin === origin && method === 'GET' && options.assetFetch !== undefined) return options.assetFetch(input, init);
      return refusal(403, 'External requests are disabled in the browser demo.');
    }
    // No fallback to native fetch for an API, even when the endpoint is unsupported.
    if (url.origin !== origin) return refusal(403, 'Remote instances are disabled in the browser demo.');
    const request = input instanceof Request ? new Request(input, init) : new Request(url, init);
    request.signal.throwIfAborted();
    try {
      if (method === 'GET') {
        const fixture = readOnlyFixture(url);
        if (fixture !== undefined) return json(fixture);
        if (path === '/api/v1/auth/whoami') return json(whoami);
        if (path === '/api/v1/runtime/status') return json({ state: 'ready', phase: null });
        if (path === '/api/v1/meta') return json({ server_version: 'browser-demo', instance_identity: 'hikyo-browser-demo', api_revision: 1, protocol_capabilities: ['delivery-target-report/1'] });
        if (path === '/api/v1/me/orgs') return json(list([{ id: org.id, name: org.name }]));
        if (path === '/api/v1/orgs') return json(list([org]));
        if (path === orgRoot) return json(org);
        if (path === `${orgRoot}/projects`) return json(list([project]));
        if (path === projectRoot) return json({ ...project, can_manage_policy: role === 'admin' });
        if (path === `${orgRoot}/retention`) return json(retention);
        if (path === `${projectRoot}/retention`) return json({ ...retention, inherited: true });
        if (path === `${projectRoot}/definitions/settings`) return json({ definitions_source: 'db', can_declare_keys: false, can_edit_definitions: false });
        if (path === `${projectRoot}/machine-reveal`) return json({ enabled: false });
        if (path === `${projectRoot}/environments`) return json(list(environments));
        if (path === `${projectRoot}/folders`) return json(list(folders));
        if (path === `${projectRoot}/keys`) return json({ ...list(keys), schema_revision: 1 });
        if (path === `${projectRoot}/key-groups`) return json(list([]));
        if (path === `${projectRoot}/grants` || path === `${orgRoot}/grants`) return json(list(grants));
        if (path === `${projectRoot}/rules` || path === `${orgRoot}/rules`) return json(list([]));
        if (path === '/api/v1/auth/totp') return json({ confirmed: true, pending: false, has_password: true, has_passkey: true, can_reauth_oidc: true });
        if (path === '/api/v1/auth/methods') return json({ local_login_enabled: false, providers: [], signup_open: false, signup_paused: true, signup_methods: [] });
        const key = keys.find((candidate) => path === `${projectRoot}/keys/${candidate.id}` || path === `${projectRoot}/keys/${candidate.name}`);
        if (key !== undefined) return json(key);
      }
      const environment = environments.find((candidate) => path.startsWith(`${projectRoot}/environments/${candidate.id}/`));
      if (environment === undefined) return refusal(404, 'This feature is not included in the browser demo.');
      const resource = path.slice(`${projectRoot}/environments/${environment.id}/`.length);
      const revision = revisions.get(environment.id) ?? 1;
      const ownDrafts = [...drafts.values()].filter((draft) => draft.environment_id === environment.id);
      if (method === 'GET') {
        if (resource === 'values') return json(list(keys.map((key) => cell(environment.id, key))));
        if (resource === 'settings') return json({ protected: false, reauth_window_seconds: 900, expected_protected: environment.id === DEMO_IDS.production });
        if (resource === 'reveal-window') return json({ effective_window_seconds: revealWindowSeconds, protected: false, totp_offered: false, live: role === 'admin', single_decision: false, can_reveal: role === 'admin', ...(role === 'admin' ? { expires_at: new Date(Date.now() + revealWindowSeconds * 1000).toISOString() } : {}) });
        if (resource === 'signals') {
          // Like the real service, signal only cells changed in this environment's latest publish.
          const changedKeys = new Set(histories.get(environment.id)?.[0]?.changed_keys.map((key) => key.key_id));
          return json({ environment_id: environment.id, revision, cells: keys.map((key) => {
            const draft = drafts.get(`${environment.id}:${key.id}`);
            return { key_id: key.id, name: key.name, classification: key.classification, pending_by_others: false,
              ...(changedKeys.has(key.id) ? { changed_in_revision: revision } : {}),
              ...(draft === undefined ? {} : { pending_version_id: draft.version_id, pending_operation: draft.operation }) };
          }) });
        }
        if (resource === 'pending') return json(list(ownDrafts.map((draft) => ({ ...pending(draft), revealed: draft.key.classification === 'config' && draft.operation === 'set',
          ...(draft.key.classification === 'config' && draft.operation === 'set' ? { value: draft.value } : {}) }))));
        if (resource === 'revisions') return json(list(histories.get(environment.id) ?? []));
        if (resource === 'pins' || resource === 'grants') return json(list([]));
        if (resource.startsWith('revisions/')) {
          const history = histories.get(environment.id)?.find((item) => String(item.revision) === resource.slice('revisions/'.length));
          if (history !== undefined) return json({ ...history, change_token: `demo-${environment.id}-${history.revision}`, keys: (snapshots.get(`${environment.id}:${history.revision}`) ?? []).map((key) => ({ key_id: key.id, name: key.name, classification: key.classification, sensitive: key.classification === 'secret' })) });
        }
      }
      if (resource === 'values/reveal' && method === 'POST') {
        return role === 'admin' ? json(list(keys.map((key) => cell(environment.id, key, true)))) : refusal(403, 'Only the demo admin can reveal sample secrets.');
      }
      const valueMatch = /^values\/([^/]+)(\/reveal|\/initialize)?$/.exec(resource);
      if (valueMatch !== null) {
        const key = keys.find((candidate) => candidate.id === valueMatch[1] || candidate.name === valueMatch[1]);
        if (key === undefined) return refusal(404, 'Unknown sample key.');
        if (method === 'POST' && valueMatch[2] === '/reveal') return role === 'admin' ? json(cell(environment.id, key, true)) : refusal(403, 'Only the demo admin can reveal sample secrets.');
        if (((method === 'PUT' || method === 'DELETE') && valueMatch[2] === undefined) || (method === 'POST' && valueMatch[2] === '/initialize')) {
          if (!demoCanEdit(role, environment.id)) return refusal(403, 'This demo account cannot edit values in this environment.');
          if (valueMatch[2] === '/initialize' && value(environment.id, key) !== undefined) return refusal(409, 'This sample value is already set. Use edit to change it.');
          const inputValue = method === 'DELETE' ? undefined : zSetValueRequest.parse(await request.json()).value;
          request.signal.throwIfAborted();
          const draft: Draft = { version_id: `ver_11111111-1111-4111-8111-${String(++sequence).padStart(12, '0')}`, environment_id: environment.id, key,
            operation: method === 'DELETE' ? 'unset' : 'set', value: inputValue, staged_from_revision: revision, created_at: new Date().toISOString() };
          drafts.set(`${environment.id}:${key.id}`, draft);
          return json(pending(draft));
        }
      }
      if (method === 'POST' && resource === 'publish') {
        if (role !== 'admin') return refusal(403, 'Only the demo admin can publish sample values.');
        const body = zPublishRequest.parse(await request.json());
        request.signal.throwIfAborted();
        const selected = ownDrafts.filter((draft) => body.version_ids === undefined || body.version_ids.includes(draft.version_id));
        if (selected.length === 0 || body.version_ids?.some((id) => !selected.some((draft) => draft.version_id === id))) return refusal(409, 'The selected drafts have changed. Reload the pending changes.');
        const changed_keys: Revision['changed_keys'] = selected.map((draft) => ({ key_id: draft.key.id, name: draft.key.name, change: draft.operation === 'unset' ? 'removed' : value(environment.id, draft.key) === undefined ? 'added' : 'edited' }));
        const history: Revision = { revision: revision + 1, schema_revision: 1, published_by: principalIds[role], published_by_name: account.name, published_at: new Date().toISOString(),
          changed_keys, payload_present: true };
        for (const draft of selected) {
          published.set(`${environment.id}:${draft.key.id}`, draft.operation === 'unset' ? undefined : draft.value);
          drafts.delete(`${environment.id}:${draft.key.id}`);
        }
        snapshots.set(`${environment.id}:${revision + 1}`, keys.filter((key) => value(environment.id, key) !== undefined));
        revisions.set(environment.id, revision + 1);
        histories.get(environment.id)?.unshift(history);
        return json({ published: selected.map((draft) => draft.version_id), closed_in: [], environments: [{ environment_id: environment.id, revision: revision + 1, schema_revision: 1, change_token: `demo-${environment.id}-${revision + 1}`, changed_keys }] });
      }
      return refusal(404, 'This feature is not included in the browser demo.');
    } catch (error) {
      if (error instanceof ZodError || error instanceof SyntaxError) return refusal(400, 'The demo request does not match the expected format.');
      throw error;
    }
  };
}

/** Run before React: catches both generated SDK and direct runtime-status reads. */
export function installDemo(role: DemoRole): void {
  const demoFetch = createDemoFetch(role, { assetFetch: globalThis.fetch.bind(globalThis) });
  globalThis.fetch = demoFetch;
  client.setConfig({ fetch: demoFetch });
}

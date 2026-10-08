import { zEnvironmentList, zEnvironmentSignals, zKey, zKeyList, zPendingChange, zPendingDraftList, zSetValueRequest, zValueList } from '@hikyo/zod';
import type { z } from 'zod';
import { ORG, PRJ } from '../../src/testkit/ids.ts';
import { authenticatedIdentity } from '../../src/testkit/identity.ts';

export const matrixPath = `/orgs/${ORG}/projects/${PRJ}/matrix`;
const projectUrl = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const timestamp = '2026-01-01T00:00:00Z';
const id = (kind: string, index: number) => `${kind}_123e4567-e89b-12d3-a456-${String(index).padStart(12, '0')}`;
export const environmentId = (index: number) => id('env', index);
const keyId = (index: number) => id('key', index);
const keyName = (index: number) => `SETTING_${String(index).padStart(4, '0')}`;

// Transport-only fixtures: the real Shell, Matrix, query hooks, Zod boundary,
// validation, virtualizer and editor run in the production-mode bundle.
export function installMatrixFixture() {
  const keys = Array.from({ length: 1000 }, (_, index) => ({
    id: keyId(index), org_id: ORG, project_id: PRJ, name: keyName(index),
    folder_path: `group-${String(Math.floor(index / 100))}`, classification: index % 5 === 0 ? 'secret' : 'config',
    description: '', deprecated: false, deprecation_note: '', group_id: '',
    declaration: { rule: { type: 'string', allow_empty: false } },
    presence: { required_in: { mode: 'all' }, forbidden_in: { mode: 'none' } }, created_at: timestamp,
  } satisfies z.input<typeof zKey>));
  const catalogue = { count: keys.length, items: keys, schema_revision: 1 } satisfies z.input<typeof zKeyList>;
  const environments = Array.from({ length: 20 }, (_, index) => ({
    id: environmentId(index), org_id: ORG, project_id: PRJ, name: `environment-${String(index).padStart(2, '0')}`, display_order: index, created_at: timestamp,
  }));
  const environmentList = { count: environments.length, items: environments } satisfies z.input<typeof zEnvironmentList>;
  const drafts = new Map<string, z.input<typeof zPendingDraftList>['items']>();
  const liveValues = new Map<string, string>();
  let revision = 1;
  let stream: ReadableStreamDefaultController<Uint8Array> | undefined;
  const failures: string[] = [];
  const json = (body: object, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
  const identity = { ...authenticatedIdentity, capabilities: { ...authenticatedIdentity.capabilities, instance_operator: false } };
  globalThis.fetch = async (input, init) => {
    const request = new Request(new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.origin), input instanceof Request ? input : init);
    const path = new URL(request.url).pathname;
    if (path === '/api/v1/auth/whoami') return json(identity);
    if (path === '/api/v1/auth/totp') return json({ confirmed: true, pending: false });
    if (path === '/api/v1/auth/webauthn/credentials') return json({ passkeys: [] });
    if (path === '/api/v1/me/orgs') return json({ items: [{ id: ORG, name: 'Performance fixture' }], count: 1 });
    if (path === `/api/v1/orgs/${ORG}/projects`) return json({ items: [{ id: PRJ, org_id: ORG, name: 'Large project', created_at: timestamp }], count: 1 });
    if (path === '/api/v1/meta') return json({ server_version: 'dev', api_revision: 1, protocol_capabilities: [] });
    if (path === '/api/v1/instance/config') return json({ error: 'forbidden' }, 403);
    if (path === `${projectUrl}/environments`) return json(environmentList);
    if (path === `${projectUrl}/keys`) return json(catalogue);
    if (path === `${projectUrl}/key-groups` || path === `${projectUrl}/folders`) return json({ items: [], count: 0 });
    if (path === `${projectUrl}/definitions/settings`) return json({ definitions_source: 'db', can_declare_keys: true, can_edit_definitions: true });
    if (path === `${projectUrl}/events`) return new Response(new ReadableStream<Uint8Array>({ start(controller) { stream = controller; controller.enqueue(new TextEncoder().encode(': connected\n\n')); } }), { headers: { 'Content-Type': 'text/event-stream' } });
    const match = path.match(/\/environments\/([^/]+)\/(values|signals|settings|pending)(?:\/([^/]+))?$/);
    if (match !== null) {
      const [, env, resource, key] = match;
      if (env === undefined || !environments.some((item) => item.id === env)) throw new Error(`Unknown environment: ${path}`);
      if (resource === 'settings') return json({ protected: false, reauth_window_seconds: 600 });
      if (resource === 'pending') return json({ count: drafts.get(env)?.length ?? 0, items: drafts.get(env) ?? [] });
      if (resource === 'signals') {
        const result = { environment_id: env, revision, cells: keys.map((item) => {
          const draft = drafts.get(env)?.find((entry) => entry.key_id === item.id);
          return { key_id: item.id, name: item.name, classification: item.classification, pending_by_others: false, ...(draft === undefined ? {} : { pending_version_id: draft.version_id, pending_operation: 'set' }) };
        }) } satisfies z.input<typeof zEnvironmentSignals>;
        return json(result);
      }
      if (resource === 'values' && request.method === 'GET') {
        const result = { count: keys.length, items: keys.map((item, index) => {
          // 100 keys are absent in one environment, making Problems meaningful.
          const set = !(env === environmentId(0) && index % 10 === 0);
          const revealed = set && item.classification === 'config';
          return { key_id: item.id, name: item.name, classification: item.classification, set, revealed, ...(revealed ? { value: liveValues.get(`${env}:${item.id}`) ?? 'published-value' } : {}) };
        }) } satisfies z.input<typeof zValueList>;
        return json(result);
      }
      if (resource === 'values' && request.method === 'PUT') {
        const record = keys.find((item) => item.id === key || item.name === key);
        if (record === undefined) throw new Error(`Unknown key: ${path}`);
        const body = zSetValueRequest.parse(await request.json());
        const draft = { version_id: id('pcv', 1), key_id: record.id, name: record.name, classification: record.classification, operation: 'set', staged_from_revision: revision, created_at: timestamp, revealed: true, value: body.value, advisory: { owner_id: identity.principal.id, valid: true } } satisfies z.input<typeof zPendingDraftList>['items'][number];
        drafts.set(env, [draft]);
        return json({ version_id: draft.version_id, key_id: draft.key_id, name: draft.name, classification: draft.classification, operation: draft.operation, staged_from_revision: draft.staged_from_revision, created_at: draft.created_at, findings: [] } satisfies z.input<typeof zPendingChange>);
      }
    }
    failures.push(`${request.method} ${path}`);
    return json({ error: `Unmatched performance fixture: ${request.method} ${path}` }, 404);
  };
  return {
    failures,
    identity,
    refresh(value: string) {
      if (stream === undefined) throw new Error('Advisory stream is not connected');
      liveValues.set(`${environmentId(0)}:${keyId(2)}`, value);
      revision++;
      stream.enqueue(new TextEncoder().encode(`event: advisory\ndata: ${JSON.stringify({ type: 'revision.published', environment_id: environmentId(0), revision })}\n\n`));
    },
  };
}

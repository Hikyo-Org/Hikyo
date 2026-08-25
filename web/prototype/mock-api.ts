// PROTOTYPE — in-memory API for viewing the bundled UI without the Go application.

import type { IncomingMessage, ServerResponse } from 'node:http';

import {
  zCreateProjectRequest,
  zPublishRequest,
  zSetValueRequest,
} from '../../clients/ts/src/generated/zod.gen.ts';
import type { Plugin } from 'vite';

type Scenario = 'populated' | 'attention' | 'empty';

const fixtureTime = '2026-08-24T10:00:00Z';
const prototypeIdleExpiry = '9999-12-30T23:59:59.999Z';
const prototypeAbsoluteExpiry = '9999-12-31T23:59:59.999Z';

export function createPrototypeSessionTimes(requestedAt: number) {
  return {
    created_at: new Date(requestedAt).toISOString(),
    idle_expires_at: prototypeIdleExpiry,
    absolute_expires_at: prototypeAbsoluteExpiry,
  } as const;
}

// One mock-server process represents one browser session. Keeping this identity
// stable matters: AuthProvider intentionally treats a changed assurance time as
// a security transition and invalidates every query below it.
export const prototypeSessionTimes = createPrototypeSessionTimes(Date.now());

const ids = {
  session: 'ses_11111111-1111-4111-8111-111111111111',
  principal: 'prn_11111111-1111-4111-8111-111111111111',
  org: 'org_11111111-1111-4111-8111-111111111111',
  sandboxOrg: 'org_22222222-2222-4222-8222-222222222222',
  project: 'prj_11111111-1111-4111-8111-111111111111',
  webProject: 'prj_22222222-2222-4222-8222-222222222222',
  mobileProject: 'prj_33333333-3333-4333-8333-333333333333',
  environments: {
    development: 'env_11111111-1111-4111-8111-111111111111',
    staging: 'env_22222222-2222-4222-8222-222222222222',
    production: 'env_33333333-3333-4333-8333-333333333333',
  },
} as const;

const keySeeds = [
  ['key_11111111-1111-4111-8111-111111111111', 'LOG_LEVEL', 'config', 'app', 'Application log verbosity.'],
  ['key_22222222-2222-4222-8222-222222222222', 'FEATURE_CHECKOUT', 'config', 'app', 'Enable the new checkout flow.'],
  ['key_33333333-3333-4333-8333-333333333333', 'PUBLIC_APP_URL', 'config', 'app', 'Canonical browser origin.'],
  ['key_44444444-4444-4444-8444-444444444444', 'DATABASE_URL', 'secret', 'db', 'Connection string used by the API.'],
  ['key_55555555-5555-4555-8555-555555555555', 'REDIS_URL', 'secret', 'db', 'Shared cache endpoint.'],
  ['key_66666666-6666-4666-8666-666666666666', 'AUTH_SECRET', 'secret', 'auth', 'Session signing secret.'],
] as const;

const environmentItems = Object.entries(ids.environments).map(([name, id], displayOrder) => ({
  id,
  org_id: ids.org,
  project_id: ids.project,
  name,
  display_order: displayOrder,
  created_at: fixtureTime,
}));

const keys = keySeeds.map(([id, name, classification, groupId, description]) => ({
  id,
  org_id: ids.org,
  project_id: ids.project,
  name,
  folder_path: groupId,
  classification,
  description,
  deprecated: false,
  deprecation_note: '',
  declaration: {
    rule: { type: 'string', allow_empty: false },
  },
  presence: {
    required_in: name === 'DATABASE_URL' || name === 'AUTH_SECRET'
      ? { mode: 'all' }
      : { mode: 'none' },
    forbidden_in: { mode: 'none' },
  },
  group_id: groupId,
  created_at: fixtureTime,
}));

const groups = [
  ['grp_11111111-1111-4111-8111-111111111111', 'app', 'app'],
  ['grp_22222222-2222-4222-8222-222222222222', 'db', 'db'],
  ['grp_33333333-3333-4333-8333-333333333333', 'auth', 'auth'],
].map(([id, name, groupId]) => ({
  id,
  org_id: ids.org,
  project_id: ids.project,
  name,
  members: keys.filter((key) => key.group_id === groupId).map((key) => key.name),
  inert: keys.filter((key) => key.group_id === groupId).length < 2,
  created_at: fixtureTime,
}));

type Draft = {
  versionId: string;
  environmentId: string;
  keyId: string;
  operation: 'set' | 'unset';
  value?: string;
};

const drafts = new Map<string, Draft>();
const published = new Map<string, string | null>();
let projectSequence = 1;
let draftSequence = 1;
let revision = 12;

function reset(): void {
  drafts.clear();
  published.clear();
  projectSequence = 1;
  draftSequence = 1;
  revision = 12;
}

function scenarioFrom(request: IncomingMessage): Scenario {
  const cookie = request.headers.cookie ?? '';
  const match = /(?:^|;\s*)hikyo-prototype-scenario=(populated|attention|empty)(?:;|$)/.exec(cookie);
  return match?.[1] === 'attention' || match?.[1] === 'empty' ? match[1] : 'populated';
}

function send(response: ServerResponse, status: number, body?: object): void {
  response.statusCode = status;
  response.setHeader('Cache-Control', 'no-store');
  if (body === undefined) {
    response.end();
    return;
  }
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify(body));
}

async function body(request: IncomingMessage): Promise<string> {
  let value = '';
  for await (const chunk of request) {
    value += chunk.toString();
  }
  return value;
}

function keyByReference(reference: string) {
  return keys.find((key) => key.id === reference || key.name === reference);
}

function baseValue(environmentId: string, keyName: string): string | undefined {
  const address = `${environmentId}:${keyName}`;
  if (published.has(address)) return published.get(address) ?? undefined;
  const values: Record<string, Record<string, string>> = {
    [ids.environments.development]: {
      DATABASE_URL: 'set', REDIS_URL: 'set', LOG_LEVEL: 'debug', FEATURE_CHECKOUT: 'true',
      PUBLIC_APP_URL: 'http://localhost:8080', AUTH_SECRET: 'set',
    },
    [ids.environments.staging]: {
      DATABASE_URL: 'set', REDIS_URL: 'set', LOG_LEVEL: 'info', FEATURE_CHECKOUT: 'true',
      PUBLIC_APP_URL: 'https://staging.example.test', AUTH_SECRET: 'set',
    },
    [ids.environments.production]: {
      DATABASE_URL: 'set', REDIS_URL: 'set', LOG_LEVEL: 'warn', FEATURE_CHECKOUT: 'false',
      PUBLIC_APP_URL: 'https://app.example.test',
    },
  };
  return values[environmentId]?.[keyName];
}

function valuesFor(environmentId: string, scenario: Scenario) {
  return keys.map((key) => {
    const intentionallyAbsent = scenario === 'attention'
      && environmentId === ids.environments.production
      && key.name === 'DATABASE_URL';
    const value = intentionallyAbsent ? undefined : baseValue(environmentId, key.name);
    const set = value !== undefined;
    return {
      key_id: key.id,
      name: key.name,
      classification: key.classification,
      set,
      revealed: key.classification === 'config' && set,
      ...(key.classification === 'config' && value !== undefined ? { value } : {}),
      ...(set ? { updated_at: fixtureTime, updated_by: 'Alex Lee' } : {}),
    };
  });
}

function mockApi(request: IncomingMessage, response: ServerResponse): boolean | Promise<boolean> {
  const method = request.method ?? 'GET';
  const url = new URL(request.url ?? '/', 'http://prototype.local');
  const path = url.pathname;
  const scenario = scenarioFrom(request);

  if (path === '/__prototype/reset' && method === 'POST') {
    reset();
    send(response, 204);
    return true;
  }
  if (!path.startsWith('/api/v1/')) return false;

  if (path === '/api/v1/auth/whoami' && method === 'GET') {
    send(response, 200, {
      session: {
        id: ids.session,
        artifact: 'browser',
        ...prototypeSessionTimes,
        assurance: {
          method: 'local-password+totp',
          factors: ['password', 'totp'],
          authenticated_at: prototypeSessionTimes.created_at,
        },
      },
      principal: { id: ids.principal, kind: 'human', display_name: 'Alex Lee' },
    });
    return true;
  }
  if (path === '/api/v1/auth/totp' && method === 'GET') {
    send(response, 200, { confirmed: true, pending: false });
    return true;
  }
  if (path === '/api/v1/auth/webauthn/credentials' && method === 'GET') {
    send(response, 200, { passkeys: [] });
    return true;
  }
  if (path === '/api/v1/auth/logout' && method === 'POST') {
    send(response, 204);
    return true;
  }
  if (path === '/api/v1/me/orgs' && method === 'GET') {
    const items = scenario === 'empty' ? [] : [
      { id: ids.org, name: 'acme' },
      { id: ids.sandboxOrg, name: 'sandbox' },
    ];
    send(response, 200, { items, count: items.length });
    return true;
  }
  if (path === '/api/v1/instance/retention-health' && method === 'GET') {
    send(response, 200, {
      last_prune_success: scenario === 'attention' ? '2026-08-20T08:15:00Z' : fixtureTime,
      stale: scenario === 'attention',
      stale_after_seconds: 86400,
      peak_project_bytes: scenario === 'attention' ? 1_610_612_736 : 92_274_688,
      storage_warn: scenario === 'attention',
    });
    return true;
  }
  if (path === '/api/v1/instance/update-status' && method === 'GET') {
    send(response, 200, scenario === 'attention'
      ? {
          channel: 'stable', current_version: '0.9.4', latest_version: '0.9.5',
          release_url: 'https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.9.5',
          available: true, prerelease: false, published_at: '2026-08-23T14:00:00Z',
          apply_supported: false,
        }
      : { channel: 'stable', current_version: '0.9.5', available: false, prerelease: false, apply_supported: false });
    return true;
  }

  if (path === `/api/v1/orgs/${ids.org}/grants` && method === 'GET') {
    const items = [{
      id: 'grn_77777777-7777-4777-8777-777777777777',
      principal_id: ids.principal,
      capability: 'manage-members',
      scope: { org_id: ids.org },
      origins: [{ kind: 'manual', subject: ids.principal }],
      created_at: fixtureTime,
    }];
    send(response, 200, { items, count: items.length });
    return true;
  }

  const projectCollection = new RegExp(`^/api/v1/orgs/${ids.org}/projects$`);
  if (projectCollection.test(path) && method === 'GET') {
    const items = scenario === 'empty' ? [] : [
      { id: ids.project, org_id: ids.org, name: 'demo', created_at: fixtureTime },
      { id: ids.webProject, org_id: ids.org, name: 'web', created_at: fixtureTime },
      { id: ids.mobileProject, org_id: ids.org, name: 'mobile', created_at: fixtureTime },
    ];
    send(response, 200, { items, count: items.length });
    return true;
  }
  if (projectCollection.test(path) && method === 'POST') {
    return body(request).then((raw) => {
      const input = zCreateProjectRequest.parse(JSON.parse(raw));
      const project = {
        id: `prj_99999999-9999-4999-8999-${String(projectSequence).padStart(12, '0')}`,
        org_id: ids.org,
        name: input.name,
        created_at: fixtureTime,
      };
      projectSequence += 1;
      send(response, 201, project);
      return true;
    });
  }

  const projectRoot = `/api/v1/orgs/${ids.org}/projects/${ids.project}`;
  if (path === `${projectRoot}/environments` && method === 'GET') {
    const items = scenario === 'empty' ? [] : environmentItems;
    send(response, 200, { items, count: items.length });
    return true;
  }
  if (path === `${projectRoot}/keys` && method === 'GET') {
    const items = scenario === 'empty' ? [] : keys;
    send(response, 200, { items, count: items.length, schema_revision: 4 });
    return true;
  }
  if (path === `${projectRoot}/key-groups` && method === 'GET') {
    const items = scenario === 'empty' ? [] : groups;
    send(response, 200, { items, count: items.length });
    return true;
  }

  const environmentMatch = new RegExp(`^${projectRoot}/environments/([^/]+)/(values|signals|settings|pending)$`).exec(path);
  if (environmentMatch !== null && method === 'GET') {
    const environmentId = environmentMatch[1];
    const resource = environmentMatch[2];
    if (environmentId === undefined || resource === undefined) {
      send(response, 500, { error: { code: 'prototype_error', detail: 'Malformed prototype route.' } });
      return true;
    }
    if (resource === 'values') {
      const items = scenario === 'empty' ? [] : valuesFor(environmentId, scenario);
      send(response, 200, { items, count: items.length });
      return true;
    }
    if (resource === 'settings') {
      send(response, 200, {
        protected: environmentId === ids.environments.production,
        reauth_window_seconds: environmentId === ids.environments.production ? 0 : 900,
      });
      return true;
    }
    const environmentDrafts = [...drafts.values()].filter((draft) => draft.environmentId === environmentId);
    if (resource === 'signals') {
      send(response, 200, {
        environment_id: environmentId,
        revision,
        cells: keys.map((key) => {
          const draft = environmentDrafts.find((candidate) => candidate.keyId === key.id);
          return {
            key_id: key.id,
            name: key.name,
            classification: key.classification,
            ...(draft === undefined ? {} : {
              pending_version_id: draft.versionId,
              pending_operation: draft.operation,
            }),
            pending_by_others: scenario === 'attention'
              && environmentId === ids.environments.staging
              && key.name === 'AUTH_SECRET',
            changed_in_revision: revision,
          };
        }),
      });
      return true;
    }
    send(response, 200, {
      items: environmentDrafts.map((draft) => {
        const key = keyByReference(draft.keyId);
        if (key === undefined) throw new Error(`prototype draft names unknown key ${draft.keyId}`);
        return {
          version_id: draft.versionId,
          key_id: key.id,
          name: key.name,
          classification: key.classification,
          operation: draft.operation,
          staged_from_revision: revision,
          created_at: fixtureTime,
          revealed: key.classification === 'config' && draft.operation === 'set',
          ...(key.classification === 'config' && draft.value !== undefined ? { value: draft.value } : {}),
        };
      }),
      count: environmentDrafts.length,
    });
    return true;
  }

  const valueMatch = new RegExp(`^${projectRoot}/environments/([^/]+)/values/([^/]+)$`).exec(path);
  if (valueMatch !== null && (method === 'PUT' || method === 'DELETE')) {
    const environmentId = valueMatch[1];
    const keyReference = valueMatch[2];
    const key = keyReference === undefined ? undefined : keyByReference(keyReference);
    if (environmentId === undefined || key === undefined) {
      send(response, 404, { error: { code: 'not_found' } });
      return true;
    }
    const versionId = `ver_aaaaaaaa-aaaa-4aaa-8aaa-${String(draftSequence).padStart(12, '0')}`;
    draftSequence += 1;
    if (method === 'PUT') {
      return body(request).then((raw) => {
        const input = zSetValueRequest.parse(JSON.parse(raw));
        drafts.set(`${environmentId}:${key.id}`, {
          versionId,
          environmentId,
          keyId: key.id,
          operation: 'set',
          value: input.value,
        });
        send(response, 201, {
          version_id: versionId, key_id: key.id, name: key.name, classification: key.classification,
          operation: 'set', staged_from_revision: revision, created_at: fixtureTime,
        });
        return true;
      });
    }
    drafts.set(`${environmentId}:${key.id}`, {
      versionId,
      environmentId,
      keyId: key.id,
      operation: 'unset',
    });
    send(response, 201, {
      version_id: versionId, key_id: key.id, name: key.name, classification: key.classification,
      operation: 'unset', staged_from_revision: revision, created_at: fixtureTime,
    });
    return true;
  }

  const publishMatch = new RegExp(`^${projectRoot}/environments/([^/]+)/publish$`).exec(path);
  if (publishMatch !== null && method === 'POST') {
    return body(request).then((raw) => {
      const input = zPublishRequest.parse(JSON.parse(raw));
      const selected = [...drafts.values()].filter((draft) => input.version_ids.includes(draft.versionId));
      for (const draft of selected) {
        const key = keyByReference(draft.keyId);
        if (key === undefined) throw new Error(`prototype publish names unknown key ${draft.keyId}`);
        if (draft.operation === 'set' && draft.value !== undefined) {
          published.set(`${draft.environmentId}:${key.name}`, draft.value);
        } else {
          published.set(`${draft.environmentId}:${key.name}`, null);
        }
        drafts.delete(`${draft.environmentId}:${draft.keyId}`);
      }
      revision += 1;
      const environmentIds = [...new Set(selected.map((draft) => draft.environmentId))];
      send(response, 201, {
        published: selected.map((draft) => draft.versionId),
        closed_in: [],
        environments: environmentIds.map((environmentId) => ({
          environment_id: environmentId,
          revision,
          schema_revision: 4,
          change_token: `prototype-r${String(revision)}`,
          changed_keys: selected
            .filter((draft) => draft.environmentId === environmentId)
            .map((draft) => {
              const key = keyByReference(draft.keyId);
              if (key === undefined) throw new Error(`prototype result names unknown key ${draft.keyId}`);
              return { key_id: key.id, name: key.name, change: 'edited' };
            }),
        })),
      });
      return true;
    });
  }

  send(response, 501, {
    error: {
      code: 'prototype_not_implemented',
      detail: `${method} ${path} is outside this throwaway prototype.`,
    },
  });
  return true;
}

export function prototypeMockApi(): Plugin {
  return {
    name: 'hikyo-prototype-mock-api',
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        Promise.resolve(mockApi(request, response)).then((handled) => {
          if (!handled) next();
        }, next);
      });
    },
  };
}

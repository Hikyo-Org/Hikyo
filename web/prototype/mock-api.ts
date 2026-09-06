// PROTOTYPE, in-memory API for viewing the bundled UI without the Go application.

import type { IncomingMessage, ServerResponse } from 'node:http';

import {
  zApplyTemplateRequest,
  zCreateOrgRequest,
  zCreateGrantRequest,
  zCreateProjectRequest,
  zEnvironmentSettings,
  zEstablishCredentialRequest,
  zInviteMemberRequest,
  zPublishRequest,
  zRenameRequest,
  zRetentionPolicy,
  zRotateRootKeyRequest,
  zSetCredentialPolicyRequest,
  zSetDefinitionsSettingsRequest,
  zSetProjectRetentionRequest,
  zSetValueRequest,
  zReclassifyKeyRequest,
  zRenameKeyRequest,
  zUpdateKeyMetadataRequest,
} from '../../clients/ts/src/generated/zod.gen.ts';
import type { Key } from '../../clients/ts/src/generated/types.gen.ts';
import { expandTemplate, type Level } from '../src/api/access-templates.ts';
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

function prototypeLoginResult() {
  return {
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
    capabilities: { instance_operator: true },
  };
}

const ids = {
  session: 'ses_11111111-1111-4111-8111-111111111111',
  principal: 'prn_11111111-1111-4111-8111-111111111111',
  org: 'org_11111111-1111-4111-8111-111111111111',
  sandboxOrg: 'org_22222222-2222-4222-8222-222222222222',
  project: 'prj_11111111-1111-4111-8111-111111111111',
  webProject: 'prj_22222222-2222-4222-8222-222222222222',
  mobileProject: 'prj_33333333-3333-4333-8333-333333333333',
  dana: 'prn_44444444-4444-4444-8444-444444444444',
  sam: 'prn_55555555-5555-4555-8555-555555555555',
  priya: 'prn_66666666-6666-4666-8666-666666666666',
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

const keys: Key[] = keySeeds.map(([id, name, classification, groupId, description]) => ({
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

type PrototypeGrant = {
  readonly id: string;
  readonly principal_id: string;
  readonly capability: string;
  readonly scope: {
    readonly org_id?: string;
    readonly project_id?: string;
    readonly environment_id?: string;
  };
  readonly origins: readonly { readonly kind: 'manual'; readonly subject: string }[];
  readonly created_at: string;
};

type PrototypeGrantOutcome = {
  readonly grant_id: string;
  readonly capability: string;
  readonly outcome: 'created' | 'unchanged';
};

function prototypeGrant(
  id: string,
  principalId: string,
  capability: string,
  scope: PrototypeGrant['scope'],
): PrototypeGrant {
  return {
    id,
    principal_id: principalId,
    capability,
    scope,
    origins: [{ kind: 'manual', subject: principalId }],
    created_at: fixtureTime,
  };
}

const prototypeGrantSeeds: readonly PrototypeGrant[] = [
  prototypeGrant(
    'grn_11111111-1111-4111-8111-111111111111',
    ids.principal,
    'manage-members',
    { org_id: ids.org },
  ),
  prototypeGrant(
    'grn_22222222-2222-4222-8222-222222222222',
    ids.principal,
    'reveal',
    { org_id: ids.org },
  ),
  prototypeGrant(
    'grn_aaaaaaaa-1111-4111-8111-111111111111',
    ids.principal,
    'audit-read',
    { org_id: ids.org },
  ),
  prototypeGrant(
    'grn_bbbbbbbb-1111-4111-8111-111111111111',
    ids.principal,
    'reveal-history',
    { org_id: ids.org },
  ),
  prototypeGrant(
    'grn_33333333-3333-4333-8333-333333333333',
    ids.dana,
    'read',
    { org_id: ids.org, project_id: ids.project },
  ),
  prototypeGrant(
    'grn_44444444-4444-4444-8444-444444444444',
    ids.dana,
    'edit',
    { org_id: ids.org, project_id: ids.project },
  ),
  prototypeGrant(
    'grn_55555555-5555-4555-8555-555555555555',
    ids.dana,
    'publish',
    {
      org_id: ids.org,
      project_id: ids.project,
      environment_id: ids.environments.staging,
    },
  ),
  prototypeGrant(
    'grn_66666666-6666-4666-8666-666666666666',
    ids.dana,
    'reveal',
    {
      org_id: ids.org,
      project_id: ids.project,
      environment_id: ids.environments.staging,
    },
  ),
  prototypeGrant(
    'grn_77777777-7777-4777-8777-777777777777',
    ids.sam,
    'read',
    { org_id: ids.org, project_id: ids.project },
  ),
  prototypeGrant(
    'grn_88888888-8888-4888-8888-888888888888',
    ids.priya,
    'read',
    { org_id: ids.org, project_id: ids.project },
  ),
  prototypeGrant(
    'grn_99999999-9999-4999-8999-999999999999',
    ids.priya,
    'reveal',
    {
      org_id: ids.org,
      project_id: ids.project,
      environment_id: ids.environments.production,
    },
  ),
];

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
let grantSequence = 1;
let revision = 12;
let prototypeGrants = [...prototypeGrantSeeds];
let prototypeOrgName = 'acme';
let prototypeOrgDeleted = false;
let prototypeProjectNames = new Map<string, string>([
  [ids.project, 'demo'],
  [ids.webProject, 'website'],
  [ids.mobileProject, 'mobile-app'],
]);
let prototypeOrgRetention: {
  mode: 'keep-if-either' | 'unlimited';
  max_age_seconds: number | null;
  last_revisions: number | null;
} = {
  mode: 'keep-if-either',
  max_age_seconds: 2_592_000,
  last_revisions: 6,
};
let prototypeProjectRetentions = new Map<string, {
  inherited: boolean;
  mode: 'keep-if-either';
  max_age_seconds: number | null;
  last_revisions: number | null;
}>([
  [ids.project, { inherited: true, mode: 'keep-if-either', max_age_seconds: 2_592_000, last_revisions: 6 }],
  [ids.webProject, { inherited: false, mode: 'keep-if-either', max_age_seconds: 2_592_000, last_revisions: 4 }],
  [ids.mobileProject, { inherited: false, mode: 'keep-if-either', max_age_seconds: 2_592_000, last_revisions: 10 }],
]);
let prototypeDefinitionsSource: 'db' | 'git' = 'db';
let prototypeEnvironmentSettings = new Map<string, {
  protected: boolean;
  reauth_window_seconds: number | null;
}>(Object.values(ids.environments).map((environmentId) => [
  environmentId,
  {
    protected: environmentId === ids.environments.production,
    reauth_window_seconds: environmentId === ids.environments.production ? 0 : 900,
  },
]));
let prototypePasskeys = new Set([
  'psk_11111111-1111-4111-8111-111111111111',
  'psk_22222222-2222-4222-8222-222222222222',
]);
let prototypeTotpConfirmed = true;
let prototypeTotpPending = false;
let prototypeRevokedSessions = new Set<string>();
let prototypeIdentityLinked = true;
let prototypeCredentialPolicy = {
  max_finite_lifetime_seconds: 7_776_000,
  allow_indefinite: false,
  max_live_credentials: 5,
  updated_at: fixtureTime,
  updated_by: ids.principal,
};
type PrototypeOrgRow = {
  readonly id: string;
  readonly name: string;
  readonly active: boolean;
  readonly metadata: object | null;
  readonly created_at: string;
};
type PrototypeProjectRow = {
  readonly id: string;
  readonly org_id: string;
  readonly name: string;
  readonly created_at: string;
};
let prototypeExtraOrgs: PrototypeOrgRow[] = [];
let prototypeExtraProjects: PrototypeProjectRow[] = [];

function reset(): void {
  drafts.clear();
  published.clear();
  projectSequence = 1;
  draftSequence = 1;
  grantSequence = 1;
  revision = 12;
  prototypeGrants = [...prototypeGrantSeeds];
  prototypeOrgName = 'acme';
  prototypeOrgDeleted = false;
  prototypeProjectNames = new Map([
    [ids.project, 'demo'],
    [ids.webProject, 'website'],
    [ids.mobileProject, 'mobile-app'],
  ]);
  prototypeOrgRetention = {
    mode: 'keep-if-either',
    max_age_seconds: 2_592_000,
    last_revisions: 6,
  };
  prototypeProjectRetentions = new Map([
    [ids.project, { inherited: true, mode: 'keep-if-either', max_age_seconds: 2_592_000, last_revisions: 6 }],
    [ids.webProject, { inherited: false, mode: 'keep-if-either', max_age_seconds: 2_592_000, last_revisions: 4 }],
    [ids.mobileProject, { inherited: false, mode: 'keep-if-either', max_age_seconds: 2_592_000, last_revisions: 10 }],
  ]);
  prototypeDefinitionsSource = 'db';
  prototypeEnvironmentSettings = new Map(Object.values(ids.environments).map((environmentId) => [
    environmentId,
    {
      protected: environmentId === ids.environments.production,
      reauth_window_seconds: environmentId === ids.environments.production ? 0 : 900,
    },
  ]));
  prototypePasskeys = new Set([
    'psk_11111111-1111-4111-8111-111111111111',
    'psk_22222222-2222-4222-8222-222222222222',
  ]);
  prototypeTotpConfirmed = true;
  prototypeTotpPending = false;
  prototypeRevokedSessions = new Set();
  prototypeIdentityLinked = true;
  prototypeCredentialPolicy = {
    max_finite_lifetime_seconds: 7_776_000,
    allow_indefinite: false,
    max_live_credentials: 5,
    updated_at: fixtureTime,
    updated_by: ids.principal,
  };
  prototypeExtraOrgs = [];
  prototypeExtraProjects = [];
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

type PrototypeReadFixture = {
  readonly status: number;
  readonly body: object;
};

function canonicalPrototypePath(path: string): string {
  return path
    .replace('/orgs/acme', `/orgs/${ids.org}`)
    .replace('/projects/demo', `/projects/${ids.project}`)
    .replace('/projects/website', `/projects/${ids.webProject}`)
    .replace('/projects/mobile-app', `/projects/${ids.mobileProject}`);
}

/** Static reads required to exercise every finalized app-chrome surface locally. */
export function prototypeReadFixture(
  path: string,
  scenario: Scenario = 'populated',
): PrototypeReadFixture | undefined {
  path = canonicalPrototypePath(path);
  if (path === '/api/v1/auth/methods') {
    return {
      status: 200,
      body: {
        local_login_enabled: true,
        providers: [{ slug: 'git', display_name: 'git.example.com', kind: 'oidc' }],
      },
    };
  }
  if (path === '/api/v1/auth/identities') {
    return {
      status: 200,
      body: {
        identities: scenario === 'empty' || !prototypeIdentityLinked ? [] : [{
          id: 'idn_11111111-1111-4111-8111-111111111111',
          kind: 'oidc',
          issuer: 'https://git.example.com',
          subject: 'alex',
          provider_id: 'git',
          created_at: '2026-06-02T10:00:00Z',
        }],
      },
    };
  }
  if (path === '/api/v1/me/sessions') {
    const items = (scenario === 'empty' ? [] : [
      {
        id: ids.session,
        artifact: 'browser',
        auth_method: 'local-password+totp',
        created_at: fixtureTime,
        last_seen_at: fixtureTime,
        idle_expires_at: prototypeIdleExpiry,
        absolute_expires_at: prototypeAbsoluteExpiry,
        source_ip: '193.28.x.x',
        user_agent: 'Safari · macOS',
      },
      {
        id: 'ses_22222222-2222-4222-8222-222222222222',
        artifact: 'browser',
        auth_method: 'local-password',
        created_at: '2026-08-22T10:00:00Z',
        last_seen_at: '2026-08-22T10:00:00Z',
        idle_expires_at: prototypeIdleExpiry,
        absolute_expires_at: prototypeAbsoluteExpiry,
        source_ip: '193.28.x.x',
        user_agent: 'Firefox · Fedora',
      },
      {
        id: 'ses_33333333-3333-4333-8333-333333333333',
        artifact: 'cli',
        auth_method: 'device-authorization',
        created_at: '2026-08-24T09:35:00Z',
        last_seen_at: '2026-08-24T09:35:00Z',
        idle_expires_at: prototypeIdleExpiry,
        absolute_expires_at: prototypeAbsoluteExpiry,
        user_agent: 'hikyo CLI · laptop.example',
      },
      {
        id: 'ses_44444444-4444-4333-8333-444444444444',
        artifact: 'cli',
        auth_method: 'device-authorization',
        created_at: '2026-08-18T09:35:00Z',
        last_seen_at: '2026-08-18T09:35:00Z',
        idle_expires_at: prototypeIdleExpiry,
        absolute_expires_at: prototypeAbsoluteExpiry,
        user_agent: 'hikyo CLI · example-cluster-0',
      },
    ]).filter((session) => !prototypeRevokedSessions.has(session.id));
    return { status: 200, body: { items, count: items.length } };
  }
  if (path === '/api/v1/orgs') {
    const items = scenario === 'empty' ? [] : [
      ...(prototypeOrgDeleted ? [] : [
      {
        id: ids.org,
        name: prototypeOrgName,
        active: true,
        metadata: null,
        created_at: fixtureTime,
      },
      ]),
      {
        id: ids.sandboxOrg,
        name: 'sample-org',
        active: true,
        metadata: null,
        created_at: fixtureTime,
      },
      ...prototypeExtraOrgs,
    ];
    return { status: 200, body: { items, count: items.length } };
  }
  const extraOrgRead = /^\/api\/v1\/orgs\/([^/]+)$/.exec(path);
  if (extraOrgRead !== null) {
    const orgId = extraOrgRead[1];
    const org = orgId === undefined
      ? undefined
      : prototypeExtraOrgs.find((candidate) => candidate.id === orgId);
    if (org !== undefined) return { status: 200, body: org };
  }
  const extraOrgChildren = /^\/api\/v1\/orgs\/([^/]+)\/(projects|grants|retention)$/.exec(path);
  if (extraOrgChildren !== null) {
    const orgId = extraOrgChildren[1];
    const resource = extraOrgChildren[2];
    if (prototypeExtraOrgs.some((candidate) => candidate.id === orgId)) {
      if (resource === 'retention') return { status: 200, body: prototypeOrgRetention };
      return { status: 200, body: { items: [], count: 0 } };
    }
  }
  if (path === '/api/v1/instance/grants') {
    const items = scenario === 'empty' ? [] : [
      prototypeGrant(
        'grn_cccccccc-1111-4111-8111-111111111111',
        ids.principal,
        'instance-config',
        {},
      ),
    ];
    return { status: 200, body: { items, count: items.length } };
  }
  if (path === '/api/v1/instance/credential-policy') {
    return { status: 200, body: prototypeCredentialPolicy };
  }
  // The provider, federation and SP-key panels render unguarded in prototype
  // mode (#567); the prototype has none configured, and says so.
  if (path === '/api/v1/instance/oidc-providers') return { status: 200, body: { providers: [] } };
  if (path === '/api/v1/instance/saml-providers') return { status: 200, body: { providers: [] } };
  if (path === '/api/v1/instance/saml-sp-keys') return { status: 200, body: { keys: [] } };
  if (path === '/api/v1/instance/federation-issuers') {
    return { status: 200, body: { items: [], count: 0 } };
  }

  const extraProjectRead = new RegExp(
    `^/api/v1/orgs/${ids.org}/projects/([^/]+)$`,
  ).exec(path);
  if (extraProjectRead !== null) {
    const projectId = extraProjectRead[1];
    const project = projectId === undefined
      ? undefined
      : prototypeExtraProjects.find((candidate) => candidate.id === projectId);
    if (project !== undefined) return { status: 200, body: project };
  }

  const projectRead = new RegExp(
    `^/api/v1/orgs/${ids.org}/projects/(${ids.project}|${ids.webProject}|${ids.mobileProject})$`,
  ).exec(path);
  if (projectRead !== null) {
    const projectId = projectRead[1];
    if (projectId === undefined) {
      throw new Error('prototype project read matched without a project id');
    }
    const name = prototypeProjectNames.get(projectId);
    if (name === undefined) return undefined;
    return {
      status: 200,
      body: {
        id: projectId,
        org_id: ids.org,
        name,
        created_at: fixtureTime,
      },
    };
  }
  if (path === `/api/v1/orgs/${ids.org}/retention`) {
    return { status: 200, body: prototypeOrgRetention };
  }
  const projectRetention = new RegExp(
    `^/api/v1/orgs/${ids.org}/projects/(${ids.project}|${ids.webProject}|${ids.mobileProject})/retention$`,
  ).exec(path);
  if (projectRetention !== null) {
    const projectId = projectRetention[1];
    const policy = projectId === undefined ? undefined : prototypeProjectRetentions.get(projectId);
    return policy === undefined ? undefined : { status: 200, body: policy };
  }
  if (path === `/api/v1/orgs/${ids.org}/projects/${ids.project}/definitions/settings`) {
    return { status: 200, body: { definitions_source: prototypeDefinitionsSource } };
  }
  if (path === `/api/v1/orgs/${ids.org}/projects/${ids.project}/grants`) {
    const items = prototypeGrants.filter((grant) => grant.scope.project_id === ids.project);
    return { status: 200, body: { items, count: items.length } };
  }
  return undefined;
}

export const prototypeMeta = {
  server_version: '0.9.5',
  api_revision: 1,
  protocol_capabilities: [],
};

/** Shaped by the generated contract (`zRetentionHealth`); the mock test pins it. */
export function prototypeRetentionHealth(scenario: Scenario) {
  const attention = scenario === 'attention';
  return {
    last_prune_success: attention ? '2026-08-20T08:15:00Z' : fixtureTime,
    stale: attention,
    stale_after_seconds: 86400,
    peak_project_bytes: attention ? 1_610_612_736 : 92_274_688,
    storage_warn: attention,
    backup: {
      scheduled: true,
      last_success_at: attention ? '2026-08-19T02:00:00Z' : fixtureTime,
      artifact_age_seconds: attention ? 432_000 : 3_600,
      rpo_seconds: 86_400,
      rpo_exceeded: attention,
      last_failure_at: attention ? '2026-08-20T02:00:00Z' : null,
      last_failure_reason: attention ? 'destination refused the archive' : '',
      last_prune_at: fixtureTime,
      last_drill_at: attention ? null : fixtureTime,
      last_drill_ok: !attention,
      drill_stale: attention,
    },
    adapter_targets_failed: attention ? 1 : 0,
    adapter_targets_paused: 0,
    adapter_targets_attention: attention ? 1 : 0,
    adapter_jobs_queued: 0,
  };
}

function mockApi(request: IncomingMessage, response: ServerResponse): boolean | Promise<boolean> {
  const method = request.method ?? 'GET';
  const url = new URL(request.url ?? '/', 'http://prototype.local');
  const path = canonicalPrototypePath(url.pathname);
  const scenario = scenarioFrom(request);

  if (path === '/__prototype/reset' && method === 'POST') {
    reset();
    send(response, 204);
    return true;
  }
  if (!path.startsWith('/api/v1/')) return false;

  if (method === 'GET') {
    const fixture = prototypeReadFixture(path, scenario);
    if (fixture !== undefined) {
      send(response, fixture.status, fixture.body);
      return true;
    }
  }

  // The advisory stream (#510). The prototype has no event source to subscribe
  // to, but the matrix asks for one and, without an answer, reconnects at
  // its backoff forever while the fallback poll runs. Answer with a healthy,
  // never-ending stream of heartbeats instead: the stream goes healthy, the
  // fallback poll never starts, and the prototype exercises the same
  // one-connection shape the real server serves. No events are emitted: the
  // prototype's mutations invalidate their own caches through the ordinary
  // mutation responses.
  if (method === 'GET' && path.endsWith('/events')) {
    response.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
    });
    response.write('retry: 5000\n\n');
    const heartbeat = setInterval(() => response.write(': heartbeat\n\n'), 15_000);
    response.on('close', () => {
      clearInterval(heartbeat);
    });
    return true;
  }

  if (path === '/api/v1/orgs' && method === 'POST') {
    return body(request).then((raw) => {
      const input = zCreateOrgRequest.parse(JSON.parse(raw));
      const org: PrototypeOrgRow = {
        id: 'org_99999999-9999-4999-8999-999999999999',
        name: input.name,
        active: input.active,
        metadata: input.metadata ?? null,
        created_at: fixtureTime,
      };
      prototypeExtraOrgs = [...prototypeExtraOrgs, org];
      send(response, 201, org);
      return true;
    });
  }

  if (path === `/api/v1/orgs/${ids.org}` && method === 'PATCH') {
    return body(request).then((raw) => {
      const input = zRenameRequest.parse(JSON.parse(raw));
      prototypeOrgName = input.name;
      send(response, 200, {
        id: ids.org,
        name: prototypeOrgName,
        active: true,
        metadata: null,
        created_at: fixtureTime,
      });
      return true;
    });
  }

  if (path === `/api/v1/orgs/${ids.org}` && method === 'DELETE') {
    prototypeOrgDeleted = true;
    prototypeProjectNames.clear();
    send(response, 204);
    return true;
  }

  const extraOrgMutation = /^\/api\/v1\/orgs\/([^/]+)$/.exec(path);
  if (extraOrgMutation !== null && method === 'PATCH') {
    return body(request).then((raw) => {
      const orgId = extraOrgMutation[1];
      const input = zRenameRequest.parse(JSON.parse(raw));
      const current = prototypeExtraOrgs.find((candidate) => candidate.id === orgId);
      if (current === undefined) return false;
      const updated = { ...current, name: input.name };
      prototypeExtraOrgs = prototypeExtraOrgs.map((candidate) =>
        candidate.id === orgId ? updated : candidate,
      );
      send(response, 200, updated);
      return true;
    });
  }

  const extraOrgRetentionMutation = /^\/api\/v1\/orgs\/([^/]+)\/retention$/.exec(path);
  if (extraOrgRetentionMutation !== null && method === 'PUT') {
    return body(request).then((raw) => {
      const orgId = extraOrgRetentionMutation[1];
      if (!prototypeExtraOrgs.some((candidate) => candidate.id === orgId)) return false;
      const input = zRetentionPolicy.parse(JSON.parse(raw));
      prototypeOrgRetention = {
        mode: input.mode,
        max_age_seconds: input.max_age_seconds ?? null,
        last_revisions: input.last_revisions ?? null,
      };
      send(response, 200, prototypeOrgRetention);
      return true;
    });
  }

  if (extraOrgMutation !== null && method === 'DELETE') {
    const orgId = extraOrgMutation[1];
    const exists = prototypeExtraOrgs.some((candidate) => candidate.id === orgId);
    if (!exists) return false;
    prototypeExtraOrgs = prototypeExtraOrgs.filter((candidate) => candidate.id !== orgId);
    send(response, 204);
    return true;
  }

  const projectMutation = new RegExp(
    `^/api/v1/orgs/${ids.org}/projects/(${ids.project}|${ids.webProject}|${ids.mobileProject})$`,
  ).exec(path);
  if (projectMutation !== null && method === 'PATCH') {
    return body(request).then((raw) => {
      const projectId = projectMutation[1];
      if (projectId === undefined) throw new Error('prototype project mutation has no id');
      const input = zRenameRequest.parse(JSON.parse(raw));
      prototypeProjectNames.set(projectId, input.name);
      send(response, 200, {
        id: projectId,
        org_id: ids.org,
        name: input.name,
        created_at: fixtureTime,
      });
      return true;
    });
  }
  if (projectMutation !== null && method === 'DELETE') {
    const projectId = projectMutation[1];
    if (projectId !== undefined) prototypeProjectNames.delete(projectId);
    send(response, 204);
    return true;
  }

  if (path === `/api/v1/orgs/${ids.org}/retention` && method === 'PUT') {
    return body(request).then((raw) => {
      const input = zRetentionPolicy.parse(JSON.parse(raw));
      prototypeOrgRetention = {
        mode: input.mode,
        max_age_seconds: input.max_age_seconds ?? null,
        last_revisions: input.last_revisions ?? null,
      };
      send(response, 200, prototypeOrgRetention);
      return true;
    });
  }

  const projectRetentionMutation = new RegExp(
    `^/api/v1/orgs/${ids.org}/projects/(${ids.project}|${ids.webProject}|${ids.mobileProject})/retention$`,
  ).exec(path);
  if (projectRetentionMutation !== null && method === 'PUT') {
    return body(request).then((raw) => {
      const projectId = projectRetentionMutation[1];
      if (projectId === undefined) throw new Error('prototype retention mutation has no project id');
      const input = zSetProjectRetentionRequest.parse(JSON.parse(raw));
      const policy: {
        inherited: boolean;
        mode: 'keep-if-either';
        max_age_seconds: number | null;
        last_revisions: number | null;
      } = {
        inherited: input.inherited,
        mode: 'keep-if-either',
        max_age_seconds: input.max_age_seconds ?? null,
        last_revisions: input.last_revisions ?? null,
      };
      prototypeProjectRetentions.set(projectId, policy);
      send(response, 200, policy);
      return true;
    });
  }

  if (
    path === `/api/v1/orgs/${ids.org}/projects/${ids.project}/definitions/settings`
    && method === 'PUT'
  ) {
    return body(request).then((raw) => {
      const input = zSetDefinitionsSettingsRequest.parse(JSON.parse(raw));
      prototypeDefinitionsSource = input.definitions_source;
      send(response, 200, { definitions_source: prototypeDefinitionsSource });
      return true;
    });
  }

  const environmentSettingsMutation = new RegExp(
    `^/api/v1/orgs/${ids.org}/projects/${ids.project}/environments/([^/]+)/settings$`,
  ).exec(path);
  if (environmentSettingsMutation !== null && method === 'PUT') {
    return body(request).then((raw) => {
      const environmentId = environmentSettingsMutation[1];
      if (environmentId === undefined) throw new Error('prototype settings mutation has no environment id');
      const input = zEnvironmentSettings.parse(JSON.parse(raw));
      const settings = {
        protected: input.protected,
        reauth_window_seconds: input.reauth_window_seconds ?? null,
      };
      prototypeEnvironmentSettings.set(environmentId, settings);
      send(response, 200, settings);
      return true;
    });
  }

  if (path === '/api/v1/instance/credential-policy' && method === 'PUT') {
    return body(request).then((raw) => {
      const input = zSetCredentialPolicyRequest.parse(JSON.parse(raw));
      prototypeCredentialPolicy = {
        max_finite_lifetime_seconds: input.max_finite_lifetime_seconds,
        allow_indefinite: input.allow_indefinite,
        max_live_credentials: input.max_live_credentials,
        updated_at: fixtureTime,
        updated_by: ids.principal,
      };
      send(response, 200, {
        applied: true,
        policy: prototypeCredentialPolicy,
        affected: [],
        clamped_count: 0,
      });
      return true;
    });
  }

  if (path === '/api/v1/instance/rotate-token-key' && method === 'POST') {
    send(response, 200, { token_key_version: 2 });
    return true;
  }
  // The remaining crypto jobs (#503), stubbed the way rotate-token-key is:
  // the prototype has no ciphertext to move, so every rotation reports the
  // next version and every re-encryption moves nothing.
  if (path === '/api/v1/instance/rotate-scanning-key' && method === 'POST') {
    send(response, 200, { scanning_key_version: 2, dismissals_dropped: 0 });
    return true;
  }
  if (path === '/api/v1/instance/rotate-master-key' && method === 'POST') {
    send(response, 200, { key_version: 2 });
    return true;
  }
  if (path === '/api/v1/instance/rotate-dek' && method === 'POST') {
    send(response, 200, { scope: 'instance', key_version: 2 });
    return true;
  }
  if (path === '/api/v1/instance/reencrypt' && method === 'POST') {
    send(response, 200, { scope: 'instance', rows_moved: 0 });
    return true;
  }
  if (path === '/api/v1/instance/rotate-root-key' && method === 'POST') {
    return body(request).then((raw) => {
      const input = zRotateRootKeyRequest.parse(JSON.parse(raw));
      send(response, 200, { phase: input.phase, root_key_epoch: 2 });
      return true;
    });
  }

  const removePasskey = /^\/api\/v1\/auth\/webauthn\/credentials\/([^/]+)$/.exec(path);
  if (removePasskey !== null && method === 'DELETE') {
    const passkeyId = removePasskey[1];
    if (passkeyId !== undefined) prototypePasskeys.delete(passkeyId);
    send(response, 200, prototypeLoginResult());
    return true;
  }

  if (path === '/api/v1/auth/totp' && method === 'DELETE') {
    prototypeTotpConfirmed = false;
    prototypeTotpPending = false;
    send(response, 200, prototypeLoginResult());
    return true;
  }

  if (path === '/api/v1/auth/totp/enrol/start' && method === 'POST') {
    prototypeTotpPending = true;
    send(response, 200, {
      otpauth_uri: 'otpauth://totp/Hikyo:alex?secret=JBSWY3DPEHPK3PXP&issuer=Hikyo',
    });
    return true;
  }

  if (path === '/api/v1/auth/totp/enrol/confirm' && method === 'POST') {
    prototypeTotpConfirmed = true;
    prototypeTotpPending = false;
    send(response, 200, prototypeLoginResult());
    return true;
  }

  if (path === '/api/v1/auth/webauthn/enrol/start' && method === 'POST') {
    send(response, 200, {
      challenge: 'AQIDBA',
      rp: { name: 'Hikyo', id: 'localhost' },
      user: { id: 'AQIDBA', name: 'alex', displayName: 'Alex' },
      pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
      authenticatorSelection: { residentKey: 'required', userVerification: 'preferred' },
      timeout: 60_000,
    });
    return true;
  }

  if (path === '/api/v1/auth/webauthn/enrol/finish' && method === 'POST') {
    prototypePasskeys.add('psk_33333333-3333-4333-8333-333333333333');
    send(response, 200, prototypeLoginResult());
    return true;
  }

  if (path === '/api/v1/auth/recovery-codes/regenerate' && method === 'POST') {
    send(response, 200, {
      recovery_codes: ['alpha-bravo', 'charlie-delta', 'echo-foxtrot'],
      login: prototypeLoginResult(),
    });
    return true;
  }

  if (path === '/api/v1/auth/identities/link' && method === 'POST') {
    prototypeIdentityLinked = true;
    send(response, 200, { authorization_url: '/settings' });
    return true;
  }

  if (/^\/api\/v1\/auth\/identities\/[^/]+$/.test(path) && method === 'DELETE') {
    prototypeIdentityLinked = false;
    send(response, 200, prototypeLoginResult());
    return true;
  }

  const revokeSession = /^\/api\/v1\/me\/sessions\/([^/]+)$/.exec(path);
  if (revokeSession !== null && method === 'DELETE') {
    const sessionId = revokeSession[1];
    if (sessionId !== undefined) prototypeRevokedSessions.add(sessionId);
    send(response, 204);
    return true;
  }

  if (path === '/api/v1/auth/whoami' && method === 'GET') {
    send(response, 200, prototypeLoginResult());
    return true;
  }
  if (path === '/api/v1/auth/totp' && method === 'GET') {
    send(response, 200, { confirmed: prototypeTotpConfirmed, pending: prototypeTotpPending });
    return true;
  }
  if (path === '/api/v1/auth/webauthn/credentials' && method === 'GET') {
    send(response, 200, {
      passkeys: scenario === 'empty' ? [] : [
        {
          id: 'psk_11111111-1111-4111-8111-111111111111',
          label: 'MacBook Touch ID',
          discoverable: true,
          disabled: false,
          created_at: '2026-05-12T10:00:00Z',
          last_used_at: fixtureTime,
        },
        {
          id: 'psk_22222222-2222-4222-8222-222222222222',
          label: 'YubiKey 5C',
          discoverable: true,
          disabled: false,
          created_at: '2026-05-12T10:00:00Z',
          last_used_at: fixtureTime,
        },
        {
          id: 'psk_33333333-3333-4333-8333-333333333333',
          label: 'New passkey',
          discoverable: true,
          disabled: false,
          created_at: fixtureTime,
          last_used_at: fixtureTime,
        },
      ].filter((passkey) => prototypePasskeys.has(passkey.id)),
    });
    return true;
  }
  if (path === '/api/v1/auth/logout' && method === 'POST') {
    send(response, 204);
    return true;
  }
  if (path === '/api/v1/me/orgs' && method === 'GET') {
    const items = scenario === 'empty' ? [] : [
      ...(prototypeOrgDeleted ? [] : [{ id: ids.org, name: prototypeOrgName }]),
      { id: ids.sandboxOrg, name: 'sandbox' },
      ...prototypeExtraOrgs.map((org) => ({ id: org.id, name: org.name })),
    ];
    send(response, 200, { items, count: items.length });
    return true;
  }
  if (path === `/api/v1/orgs/${ids.org}` && method === 'GET') {
    if (prototypeOrgDeleted) {
      send(response, 404, { code: 'not_found' });
      return true;
    }
    send(response, 200, {
      id: ids.org,
      name: prototypeOrgName,
      active: true,
      metadata: null,
      created_at: fixtureTime,
    });
    return true;
  }
  if (path === '/api/v1/meta' && method === 'GET') {
    send(response, 200, prototypeMeta);
    return true;
  }
  if (path === '/api/v1/instance/retention-health' && method === 'GET') {
    send(response, 200, prototypeRetentionHealth(scenario));
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
    const items = scenario === 'empty' ? [] : prototypeGrants;
    send(response, 200, { items, count: items.length });
    return true;
  }

  // Member invitation and credential establishment (#568). The prototype
  // mints a fixed authority; nothing is ever verified against it.
  if (
    (path === `/api/v1/orgs/${ids.org}/invitations` || path === '/api/v1/instance/invitations') &&
    method === 'POST'
  ) {
    return body(request).then((raw) => {
      const input = zInviteMemberRequest.parse(JSON.parse(raw));
      if (input.username === 'taken') {
        send(response, 409, { error: { code: 'conflict', detail: 'that username is already in use' } });
        return true;
      }
      send(response, 201, {
        principal_id: 'prn_77777777-7777-4777-8777-777777777777',
        account_id: 'acc_77777777-7777-4777-8777-777777777777',
        authority: 'hik_cea_prototype_invitation_authority_value',
        expires_at: '2026-08-25T10:00:00Z',
      });
      return true;
    });
  }
  const resetMatch = /^\/api\/v1\/accounts\/([^/]+)\/credential-reset$/.exec(path);
  if (resetMatch !== null && method === 'POST') {
    send(response, 200, {
      authority: 'hik_cea_prototype_reset_authority_value',
      expires_at: '2026-08-25T10:00:00Z',
    });
    return true;
  }
  if (path === '/api/v1/auth/credential/establish' && method === 'POST') {
    return body(request).then((raw) => {
      zEstablishCredentialRequest.parse(JSON.parse(raw));
      send(response, 204);
      return true;
    });
  }

  const grantPathMatch = new RegExp(
    `^/api/v1/orgs/${ids.org}(?:/projects/([^/]+)(?:/environments/([^/]+))?)?/grants$`,
  ).exec(path);
  if (grantPathMatch !== null && (method === 'POST' || method === 'DELETE')) {
    const projectId = grantPathMatch[1];
    const environmentId = grantPathMatch[2];
    const scope = {
      org_id: ids.org,
      ...(projectId === undefined ? {} : { project_id: projectId }),
      ...(environmentId === undefined ? {} : { environment_id: environmentId }),
    };
    const sameScope = (grant: PrototypeGrant) =>
      grant.scope.org_id === scope.org_id &&
      grant.scope.project_id === scope.project_id &&
      grant.scope.environment_id === scope.environment_id;

    if (method === 'DELETE') {
      const principal = url.searchParams.get('principal');
      const capability = url.searchParams.get('capability');
      prototypeGrants = prototypeGrants.filter(
        (grant) =>
          grant.principal_id !== principal ||
          grant.capability !== capability ||
          !sameScope(grant),
      );
      send(response, 204);
      return true;
    }

    return body(request).then((raw) => {
      const createOutcome = (principal: string, capability: string): PrototypeGrantOutcome => {
        const existing = prototypeGrants.find(
          (grant) =>
            grant.principal_id === principal &&
            grant.capability === capability &&
            sameScope(grant),
        );
        if (existing !== undefined) {
          return {
            grant_id: existing.id,
            capability: existing.capability,
            outcome: 'unchanged',
          };
        }
        const id = `grn_aaaaaaaa-aaaa-4aaa-8aaa-${String(grantSequence).padStart(12, '0')}`;
        grantSequence += 1;
        prototypeGrants = [
          ...prototypeGrants,
          prototypeGrant(id, principal, capability, scope),
        ];
        return { grant_id: id, capability, outcome: 'created' };
      };

      const input = zCreateGrantRequest.or(zApplyTemplateRequest).parse(JSON.parse(raw));
      if ('template' in input) {
        const level: Level = environmentId !== undefined
          ? 'environment'
          : projectId !== undefined ? 'project' : 'org';
        const capabilities = expandTemplate(input.template, level);
        const items = capabilities.map((capability) =>
          createOutcome(input.principal, capability),
        );
        send(response, 200, { items, count: items.length });
        return true;
      }

      send(response, 201, createOutcome(input.principal, input.capability));
      return true;
    });
  }

  const projectCollection = new RegExp(`^/api/v1/orgs/${ids.org}/projects$`);
  if (projectCollection.test(path) && method === 'GET') {
    const items = scenario === 'empty' ? [] : [
      ...[ids.project, ids.webProject, ids.mobileProject].flatMap((projectId) => {
        const name = prototypeProjectNames.get(projectId);
        return name === undefined ? [] : [{ id: projectId, org_id: ids.org, name, created_at: fixtureTime }];
      }),
      ...prototypeExtraProjects,
    ];
    send(response, 200, { items, count: items.length });
    return true;
  }
  if (projectCollection.test(path) && method === 'POST') {
    return body(request).then((raw) => {
      const input = zCreateProjectRequest.parse(JSON.parse(raw));
      const project: PrototypeProjectRow = {
        id: `prj_99999999-9999-4999-8999-${String(projectSequence).padStart(12, '0')}`,
        org_id: ids.org,
        name: input.name,
        created_at: fixtureTime,
      };
      projectSequence += 1;
      prototypeExtraProjects = [...prototypeExtraProjects, project];
      send(response, 201, project);
      return true;
    });
  }

  const projectRoot = `/api/v1/orgs/${ids.org}/projects/${ids.project}`;
  const projectEnvironmentMatch = new RegExp(
    `^/api/v1/orgs/${ids.org}/projects/(${ids.project}|${ids.webProject}|${ids.mobileProject})/environments$`,
  ).exec(path);
  if (projectEnvironmentMatch !== null && method === 'GET') {
    const items = scenario === 'empty' || projectEnvironmentMatch[1] !== ids.project
      ? []
      : environmentItems;
    send(response, 200, { items, count: items.length });
    return true;
  }
  if (path === `${projectRoot}/keys` && method === 'GET') {
    const items = scenario === 'empty' ? [] : keys;
    send(response, 200, { items, count: items.length, schema_revision: 4 });
    return true;
  }
  if (path === `${projectRoot}/keys` && method === 'POST') {
    return body(request).then((raw) => {
      const input = JSON.parse(raw) as {
        name: string;
        classification: 'config' | 'secret';
        declaration?: unknown;
        folder_path?: string;
        description?: string;
        presence?: unknown;
      };
      const folder = typeof input.folder_path === 'string' ? input.folder_path : '';
      const key = {
        id: `key_${globalThis.crypto.randomUUID()}`,
        org_id: ids.org,
        project_id: ids.project,
        name: input.name,
        folder_path: folder,
        classification: input.classification,
        description: typeof input.description === 'string' ? input.description : '',
        deprecated: false,
        deprecation_note: '',
        declaration: input.declaration ?? { rule: { type: 'string', allow_empty: false } },
        presence: input.presence ?? {
          required_in: { mode: 'none' },
          forbidden_in: { mode: 'none' },
        },
        group_id: folder,
        created_at: fixtureTime,
      };
      keys.push(key as (typeof keys)[number]);
      send(response, 201, key);
      return true;
    });
  }
  if (path === `${projectRoot}/key-groups` && method === 'GET') {
    const items = scenario === 'empty' ? [] : groups;
    send(response, 200, { items, count: items.length });
    return true;
  }

  // The catalogue declaration detail (#491): one key's full declaration, and
  // its metadata edit. The store is the same `keys` array the list serves, so
  // an edit here shows through on the matrix and survives a prototype reload , 
  // the same in-memory persistence the create handler above relies on.
  const keyDetailMatch = new RegExp(`^${projectRoot}/keys/([^/]+)$`).exec(path);
  if (keyDetailMatch !== null && (method === 'GET' || method === 'PATCH' || method === 'DELETE')) {
    const reference = keyDetailMatch[1];
    const key = reference === undefined ? undefined : keyByReference(reference);
    if (key === undefined) {
      send(response, 404, { error: { code: 'not_found' } });
      return true;
    }
    if (method === 'GET') {
      send(response, 200, key);
      return true;
    }
    if (method === 'DELETE') {
      // #494: remove the key from the same in-memory catalogue the list serves,
      // so the matrix reflects the deletion (and a re-open 404s) after nav back.
      const index = keys.indexOf(key);
      if (index !== -1) keys.splice(index, 1);
      send(response, 204);
      return true;
    }
    return body(request).then((raw) => {
      const input = zUpdateKeyMetadataRequest.parse(JSON.parse(raw));
      if (input.folder_path !== undefined) {
        key.folder_path = input.folder_path;
        key.group_id = input.folder_path;
      }
      if (input.description !== undefined) key.description = input.description;
      if (input.deprecated !== undefined) key.deprecated = input.deprecated;
      if (input.deprecation_note !== undefined) key.deprecation_note = input.deprecation_note;
      if (input.classification !== undefined) key.classification = input.classification;
      send(response, 200, key);
      return true;
    });
  }

  // #494: rename and reclassify are dedicated sub-resources, mutating the same
  // in-memory catalogue. Reclassify carries no reveal ceremony in the prototype
  // (there is no session assurance to check here); a real declassification's
  // Surface-1 warnings are exercised by the component tests, not the mock.
  const keyNameMatch = new RegExp(`^${projectRoot}/keys/([^/]+)/name$`).exec(path);
  if (keyNameMatch !== null && method === 'PUT') {
    const key = keyNameMatch[1] === undefined ? undefined : keyByReference(keyNameMatch[1]);
    if (key === undefined) {
      send(response, 404, { error: { code: 'not_found' } });
      return true;
    }
    return body(request).then((raw) => {
      const input = zRenameKeyRequest.parse(JSON.parse(raw));
      key.name = input.name;
      send(response, 200, key);
      return true;
    });
  }
  const keyClassificationMatch = new RegExp(`^${projectRoot}/keys/([^/]+)/classification$`).exec(path);
  if (keyClassificationMatch !== null && method === 'PUT') {
    const key =
      keyClassificationMatch[1] === undefined ? undefined : keyByReference(keyClassificationMatch[1]);
    if (key === undefined) {
      send(response, 404, { error: { code: 'not_found' } });
      return true;
    }
    return body(request).then((raw) => {
      const input = zReclassifyKeyRequest.parse(JSON.parse(raw));
      key.classification = input.classification;
      send(response, 200, key);
      return true;
    });
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
      const settings = prototypeEnvironmentSettings.get(environmentId);
      if (settings === undefined) {
        send(response, 404, { error: { code: 'not_found' } });
        return true;
      }
      send(response, 200, settings);
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
      const selected = [...drafts.values()].filter((draft) => (input.version_ids ?? []).includes(draft.versionId));
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

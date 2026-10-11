import type {
  Adapter, AdapterTarget, ApprovalPolicy, ApprovalRequest, AccessPolicy, AccessRequest,
  AuditEvent, Grant, MachineCredential, ScimBinding, ScimMapping, ServiceAccount,
} from '@hikyo/client';
import { DEMO_ACCOUNTS, DEMO_IDS, type DemoRole } from './config.ts';
import { environments, grants, keys, org, principalIds, project } from './fixtures.ts';

const orgRoot = `/api/v1/orgs/${DEMO_IDS.org}`;
const projectRoot = `${orgRoot}/projects/${DEMO_IDS.project}`;
const id = (prefix: string, number: number) => `${prefix}_aaaaaaaa-aaaa-4aaa-8aaa-${String(number).padStart(12, '0')}`;
const list = <T>(items: T[]) => ({ items, count: items.length });

/** Static, explicitly fictional examples. This catalogue has no mutation handlers. */
export function createReadOnlyFixtures(role: DemoRole, sessionTime: string) {
  const account = DEMO_ACCOUNTS.find((item) => item.id === role);
  if (account === undefined) throw new Error('Unknown demo account');
  const later = (seconds: number) => new Date(Date.parse(sessionTime) + seconds * 1000).toISOString();
  const responses = new Map<string, object>();
  const names = Object.fromEntries(DEMO_ACCOUNTS.map((item) => [principalIds[item.id], item.name]));

  const serviceAccounts: ServiceAccount[] = [
    { id: id('svc', 1), principal_id: id('prn', 1), name: 'payments-api', kind: 'workload', created_at: sessionTime, created_by: principalIds.admin, live_credentials: 1 },
    { id: id('svc', 2), principal_id: id('prn', 2), name: 'release-pipeline', kind: 'automation', created_at: sessionTime, created_by: principalIds.admin, live_credentials: 1 },
  ];
  responses.set(`${projectRoot}/service-accounts`, list(serviceAccounts));
  serviceAccounts.forEach((serviceAccount, index) => {
    const credential: MachineCredential = index === 0 ? {
      id: id('cre', 1), kind: 'hikyo-token', prefix_hint: 'hik_demo_only', lifetime: 'finite', expires_at: later(86_400 * 30),
      created_at: sessionTime, created_by: principalIds.admin, last_used_at: sessionTime, expiring_soon: false,
    } : {
      id: id('cre', 2), kind: 'oidc-federation', issuer: 'https://token.actions.githubusercontent.com',
      subject: 'repo:acme/payments:environment:staging', audience: 'hikyo-demo', lifetime: 'indefinite',
      created_at: sessionTime, created_by: principalIds.admin, last_used_at: sessionTime, expiring_soon: false,
    };
    responses.set(`${projectRoot}/service-accounts/${serviceAccount.id}/credentials`, list([credential]));
  });
  const machineGrants: Grant[] = serviceAccounts.map((serviceAccount, index) => ({
    id: id('grn', index + 1), principal_id: serviceAccount.principal_id, principal_name: serviceAccount.name, capability: 'read',
    scope: { org_id: org.id, project_id: project.id, environment_id: DEMO_IDS.staging },
    origins: [{ kind: 'manual', subject: principalIds.admin }], created_at: sessionTime,
  }));
  responses.set(`${projectRoot}/grants`, list([...grants, ...machineGrants]));
  responses.set(`${projectRoot}/dynamic-providers`, list([]));
  responses.set('/api/v1/instance/credential-policy', { max_finite_lifetime_seconds: 7_776_000, allow_indefinite: false, max_live_credentials: 5, updated_at: sessionTime, updated_by: principalIds.admin });

  const approvalPolicy: ApprovalPolicy = {
    id: id('pol', 1), environment_id: '', min_approvals: 1, allow_self_approval: false, request_ttl_seconds: 3600,
    enabled: true, version: 1, approvers: [{ kind: 'principal', subject_id: principalIds.admin }], bypassers: [],
    principal_names: names, created_at: sessionTime, updated_at: sessionTime,
  };
  const accessPolicy: AccessPolicy = {
    ...approvalPolicy, id: id('pol', 2), capabilities: ['read', 'reveal'], max_duration_seconds: 1800,
  };
  responses.set(`${projectRoot}/approval-policies`, { items: [approvalPolicy] });
  responses.set(`${projectRoot}/access-policies`, { items: [accessPolicy] });
  environments.forEach((environment, index) => {
    const root = `${projectRoot}/environments/${environment.id}`;
    const approvalRequest: ApprovalRequest = {
      id: id('apr', index + 1), environment_id: environment.id, policy_id: approvalPolicy.id, policy_version: 1,
      requester: principalIds.developer, requester_name: names[principalIds.developer], change_count: 1, base_revision: 1,
      purpose: 'Sample review: rotate the payment provider credential.', state: 'open', invalidated_cause: '', min_approvals: 1,
      approvals: 0, key_ids: keys.filter((key) => key.name === 'AUTH_SECRET').map((key) => key.id), votes: [],
      created_at: sessionTime, expires_at: later(3600),
    };
    const accessRequest: AccessRequest = {
      id: id('arq', index + 1), environment_id: environment.id, policy_id: accessPolicy.id, policy_version: 1,
      requester: principalIds.developer, requester_name: names[principalIds.developer], capabilities: ['read', 'reveal'], duration_seconds: 900,
      reason: 'Sample request: investigate a checkout error.', bypassed: false, state: 'open', invalidated_cause: '',
      min_approvals: 1, approvals: 0, votes: [], can_approve: false, created_at: sessionTime, review_expires_at: later(3600),
    };
    responses.set(`${root}/approval-requests`, { items: [approvalRequest] });
    responses.set(`${root}/access-requests`, { offer: { policy_id: accessPolicy.id, policy_version: 1, capabilities: accessPolicy.capabilities, max_duration_seconds: 1800, min_approvals: 1, enabled: true, caller_may_bypass: false }, items: [accessRequest] });
    for (const resource of ['leases', 'ssh-cas', 'ssh-profiles', 'ssh-certificates']) responses.set(`${root}/${resource}`, { items: [] });
    responses.set(`${root}/certificates`, { certificates: [] });
    responses.set(`${root}/delivery-targets`, { principals: [], targets: [] });
  });

  const adapterId = id('adp', 1);
  const target: AdapterTarget = {
    id: id('adt', 1), adapter_id: adapterId, environment_id: DEMO_IDS.staging, destination_kind: 'environment',
    destination_owner: 'acme', destination_name: 'payments', destination_environment: 'staging', destination_id: 42, repository_id: 42,
    visibility: '', selected_repository_ids: [], name_prefix: 'PAYMENTS_', generation: 1, state: 'active', sync_status: 'converged',
    converged_revision: 1, last_attempted_revision: 1, last_attempted_at: sessionTime, last_error_class: '', retry_at: null,
    paused_at: null, drift_attention: false, failure_names: [], warnings: [], conflicts: [],
    keys: keys.map((key) => ({ key_id: key.id, name: key.name, classification: key.classification })),
  };
  const adapter: Adapter = {
    id: adapterId, provider: 'github-actions', origin: 'https://api.github.com', credential_present: true, credential_set_at: sessionTime,
    credential_expires_at: later(86_400 * 30), authority_principal_id: id('prn', 1), state: 'active', created_at: sessionTime, targets: [target],
  };
  responses.set(`${projectRoot}/adapters`, { items: [adapter] });
  responses.set(`${projectRoot}/adapters/${adapterId}`, adapter);
  responses.set(`${projectRoot}/adapters/${adapterId}/targets`, { items: [target] });
  responses.set(`${projectRoot}/adapter-targets/${target.id}`, {
    target, conflicts: [], mapping: keys.map((key) => ({ key_id: key.id, canonical_name: key.name, surface: key.classification === 'secret' ? 'secret' : 'variable', effective_name: `PAYMENTS_${key.name}` })),
  });

  const binding: ScimBinding = {
    id: id('bnd', 1), org_id: org.id, provider_kind: 'oidc', provider_slug: 'acme-sso', provider_issuer: 'https://identity.example.test',
    subject_source: 'externalId', connection_principal_id: id('prn', 3), last_contact_at: sessionTime, created_at: sessionTime, attention: [],
  };
  const groupId = id('grp', 1);
  const mapping: ScimMapping = {
    id: id('map', 1), binding_id: binding.id, group_id: groupId, template: 'viewer', project_id: project.id,
    inert: false, created_at: sessionTime, capabilities: ['read'],
  };
  const bindingRoot = `${orgRoot}/scim-bindings/${binding.id}`;
  responses.set(`${orgRoot}/scim-bindings`, list([binding]));
  responses.set(bindingRoot, binding);
  responses.set(`${bindingRoot}/mappings`, list([mapping]));
  responses.set(`${bindingRoot}/credentials`, list([{ id: id('scr', 1), binding_id: binding.id, created_at: sessionTime, expires_at: later(86_400 * 30), last_used_at: sessionTime, live: true }]));
  responses.set(`${bindingRoot}/directory/groups`, list([{ id: groupId, display_name: 'Payments engineering', external_id: 'payments-engineering', member_count: 2, created_at: sessionTime, updated_at: sessionTime }]));
  responses.set(`${bindingRoot}/directory/users`, list(DEMO_ACCOUNTS.filter((item) => item.id !== 'admin').map((item, index) => ({
    id: id('usr', index + 1), user_name: item.name, external_id: `demo-${item.id}`, account_id: id('acc', index + 1), active: true,
    groups: [groupId], created_at: sessionTime, updated_at: sessionTime, attention: [],
  }))));

  responses.set('/api/v1/instance/directory', { identity: 'hikyo-browser-demo', version: 'browser-demo', orgs: [{ name: org.name, projects: [project.name] }], org_count: 1, project_count: 1 });
  responses.set('/api/v1/instance/remotes', list([{
    id: id('rem', 1), name: 'European production (sample)', url: 'https://eu.hikyo.example.test', spki_pin: 'sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
    created_at: sessionTime, created_by: principalIds.admin, state: 'ok', last_attempt_at: sessionTime, observed_at: sessionTime,
    stale: false, identity: 'sample-europe-instance', version: 'browser-demo', org_count: 1, project_count: 2,
    orgs: [{ name: 'Acme Europe', projects: ['Payments', 'Subscriptions'] }],
  }]));
  responses.set('/api/v1/instance/connections', list([{
    id: id('con', 1), principal: principalIds.admin, label: 'Demo directory connection', kind: 'hikyo-token', prefix_hint: 'hik_demo_only',
    lifetime: 'finite', expires_at: later(86_400 * 30), created_at: sessionTime, created_by: principalIds.admin, last_used_at: sessionTime, live: true,
  }]));
  responses.set('/api/v1/instance/workspace-origins', list([{ origin: 'https://workspace.example.test', created_at: sessionTime, created_by: principalIds.admin }]));
  responses.set('/api/v1/me/profile', { username: role, display_name: account.name, email: `${role}@example.test`, email_verified: true, managed: false, username_editable: false });
  responses.set('/api/v1/auth/identities', { identities: [{ id: id('idn', 1), kind: 'oidc', issuer: 'https://identity.example.test', subject: `demo-${role}`, provider_id: 'acme-sso', created_at: sessionTime }] });
  responses.set('/api/v1/auth/webauthn/credentials', { passkeys: [{ id: id('psk', 1), label: 'Sample security key', discoverable: true, disabled: false, created_at: sessionTime, last_used_at: sessionTime }] });
  responses.set('/api/v1/me/sessions', list([{
    id: `ses_${principalIds[role].slice(4)}`, artifact: 'browser', auth_method: 'local-password+totp', created_at: sessionTime,
    last_seen_at: sessionTime, idle_expires_at: later(3600), absolute_expires_at: later(86_400), user_agent: 'Current browser demo session',
  }]));

  const auditEvents: AuditEvent[] = [
    { type: 'revision.published', outcome: 'success', object_type: 'revision', object_id: '1', payload: { revision: 1, changed_keys: ['LOG_LEVEL', 'FEATURE_CHECKOUT'] } },
    { type: 'grant.created', outcome: 'success', object_type: 'grant', object_id: id('grn', 1), payload: { capability: 'read', principal_name: 'payments-api' } },
    { type: 'reveal.denied', outcome: 'denied', object_type: 'key', object_id: keys.find((key) => key.name === 'AUTH_SECRET')?.id, payload: { reason: 'Sample event: reveal permission required.' } },
  ].map((event, index) => ({ ...event, seq: index + 1, id: id('aud', index + 1), schema_version: 1,
    occurred_at: sessionTime, occurred_asserted: false, recorded_at: sessionTime, actor_id: index === 2 ? principalIds.viewer : principalIds.admin, actor_name: names[index === 2 ? principalIds.viewer : principalIds.admin],
    actor_class: 'human', scope_class: 'environment', org_id: org.id, project_id: project.id, env_id: DEMO_IDS.staging, origin: 'browser-demo',
  }));
  const auditPaths = new Set([`${orgRoot}/audit`, `${projectRoot}/audit`, ...environments.map((environment) => `${projectRoot}/environments/${environment.id}/audit`)]);
  return (url: URL): object | undefined => {
    if (auditPaths.has(url.pathname)) {
      const environment = environments.find((item) => url.pathname.includes(`/environments/${item.id}/`));
      const items = auditEvents.filter((event) => {
        if (environment !== undefined && event.env_id !== environment.id) return false;
        const filters = [['actor', event.actor_id], ['actor_name', event.actor_name], ['operation', event.type], ['object_type', event.object_type], ['object_id', event.object_id], ['correlation_id', event.correlation_id]];
        for (const [name, value] of filters) {
          if (name === undefined) continue;
          const pattern = url.searchParams.get(name);
          if (pattern === null) continue;
          const matched = ['actor_name', 'operation', 'object_type', 'object_id'].includes(name)
            ? matchesGlob(pattern, value ?? '')
            : pattern === value;
          if (!matched) return false;
        }
        const outcomes = url.searchParams.getAll('outcomes');
        if (outcomes.length > 0 && !outcomes.includes(event.outcome)) return false;
        const from = url.searchParams.get('from');
        const to = url.searchParams.get('to');
        return (from === null || event.occurred_at >= from) && (to === null || event.occurred_at <= to);
      });
      return { ...list(items), next_after_seq: 3, upper_seq: 3, exhausted: true };
    }
    return responses.get(url.pathname);
  };
}

function matchesGlob(pattern: string, value: string): boolean {
  const expression = pattern.split('*').map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('.*');
  return new RegExp(`^${expression}$`).test(value);
}

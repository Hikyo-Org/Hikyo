import { describe, expect, it, vi } from 'vitest';
import { createClient, createConfig } from '@hikyo/runtime-core';
import { listValuesOp, publishPendingChangesOp, setValueOp } from '@hikyo/operations';
import {
  zDefinitionsSettings, zEnvironmentList, zEnvironmentSettings, zEnvironmentSignals, zError,
  zFolderList, zGrantList, zKeyList, zMeta, zMyOrgList, zOrg, zPendingChange, zPendingDraftList,
  zProject, zProjectCapacity, zProjectList, zProjectRetentionPolicy, zPublishResult, zRevealWindow, zRevisionDetail,
  zRevisionList, zRuleList, zValueCell, zValueList, zWhoAmI,
} from '@hikyo/zod';
import {
  zAccessPolicyList, zAccessQueue, zAccountProfile, zAdapterList, zAdapterTargetDetail, zApprovalPolicyList,
  zApprovalRequestList, zAuditPage, zCertificateList, zDeliveryTargetList, zDeveloperCredentialList,
  zDirectoryListing, zDynamicLeaseList, zDynamicProviderList, zFileTarget, zFileTargetList, zIdentityList,
  zInstanceConnectionList, zMachineCredentialList, zPasskeyList, zRemoteList, zRemotePairingInviteList, zScimBindingList,
  zScimCredentialList, zScimDirectoryGroupList, zScimDirectoryUserList, zScimMappingList, zServiceAccountList,
  zSessionList, zSshcaList, zSshCertificateList, zSshProfileList, zWorkspaceOriginList,
} from '@hikyo/zod';
import { parsed } from '../api/client.ts';
import { createDemoFetch } from './api.ts';
import { DEMO_ACCOUNTS, DEMO_IDS } from './config.ts';

const origin = 'https://demo.example.test';
const org = `/api/v1/orgs/${DEMO_IDS.org}`;
const project = `${org}/projects/${DEMO_IDS.project}`;
const env = (id = DEMO_IDS.development) => `${project}/environments/${id}`;
const mutation = (value: string): RequestInit => ({ method: 'PUT', body: JSON.stringify({ value }), headers: { 'Content-Type': 'application/json' } });

async function stage(fetch: typeof globalThis.fetch, name: string, value: string, environment = DEMO_IDS.development) {
  const response = await fetch(`${env(environment)}/values/${name}`, mutation(value));
  expect(response.status).toBe(200);
  return zPendingChange.parse(await response.json());
}

describe('browser demo API', () => {
  it('serves schema-valid read-only navigation, settings, members and matrix data', async () => {
    const fetch = createDemoFetch('developer', { origin });
    const fixtures = [
      ['/api/v1/auth/whoami', zWhoAmI], ['/api/v1/meta', zMeta], ['/api/v1/me/orgs', zMyOrgList],
      [org, zOrg], [`${org}/projects`, zProjectList], [project, zProject],
      [`${project}/environments`, zEnvironmentList], [`${project}/keys`, zKeyList],
      [`${project}/folders`, zFolderList], [`${org}/grants`, zGrantList], [`${org}/rules`, zRuleList],
      [`${project}/definitions/settings`, zDefinitionsSettings], [`${project}/retention`, zProjectRetentionPolicy],
      [`${project}/capacity`, zProjectCapacity],
      [`${env()}/values`, zValueList], [`${env()}/signals`, zEnvironmentSignals],
      [`${env()}/settings`, zEnvironmentSettings], [`${env()}/pending`, zPendingDraftList],
      [`${env()}/reveal-window`, zRevealWindow], [`${env()}/revisions`, zRevisionList],
      [`${env()}/revisions/1`, zRevisionDetail],
    ].map(([path, schema]) => ({ path, schema }));
    for (const fixture of fixtures) {
      if (typeof fixture.path !== 'string' || typeof fixture.schema === 'string' || fixture.schema === undefined) throw new Error('Invalid fixture');
      const response = await fetch(fixture.path);
      expect(response.status, fixture.path).toBe(200);
      expect(fixture.schema.safeParse(await response.json()).success, fixture.path).toBe(true);
    }
    const first = zWhoAmI.parse(await (await fetch('/api/v1/auth/whoami')).json());
    const second = zWhoAmI.parse(await (await fetch('/api/v1/auth/whoami')).json());
    expect(first.session).toEqual(second.session);
  });

  it.each(DEMO_ACCOUNTS)('keeps $label project policy read-only and exposes its panel only for admin', async ({ id }) => {
    const fetch = createDemoFetch(id, { origin });
    const detail = zProject.parse(await (await fetch(project)).json());
    expect(detail.can_manage_policy).toBe(id === 'admin');
    expect(detail.can_delete).toBe(false);
    const definitionsPath = `${project}/definitions/settings`;
    const definitions = zDefinitionsSettings.parse(await (await fetch(definitionsPath)).json());
    expect(definitions.can_declare_keys).toBe(false);
    expect(definitions.can_edit_definitions).toBe(false);
    const capacityPath = `${project}/capacity`;
    const capacity = zProjectCapacity.parse(await (await fetch(capacityPath)).json());
    expect(capacity).toMatchObject({ key_count: 6, environment_count: 3, effective_limit: 1000, inherited: true });
    const writes = [
      { path: project, method: 'PATCH', body: { name: 'Renamed project' } },
      { path: definitionsPath, method: 'PUT', body: { definitions_source: 'git' } },
      { path: capacityPath, method: 'PUT', body: { key_limit_override: 5000, expected_revision: 0, expected_instance_default: 1000, expected_instance_maximum: 10000 } },
      { path: `${project}/retention`, method: 'PUT', body: { mode: 'keep-if-either', max_age_seconds: 86400, last_revisions: 5 } },
      { path: `${env()}/settings`, method: 'PUT', body: { protected: true, reauth_window_seconds: 600 } },
    ];
    for (const write of writes) {
      const response = await fetch(write.path, { method: write.method, body: JSON.stringify(write.body), headers: { 'Content-Type': 'application/json' } });
      expect(response.status, write.path).toBe(404);
    }
    expect(zDefinitionsSettings.parse(await (await fetch(definitionsPath)).json())).toEqual(definitions);
    expect(zProjectCapacity.parse(await (await fetch(capacityPath)).json())).toEqual(capacity);
  });

  it('serves useful schema-valid read-only examples for every expanded screen and nested details', async () => {
    const fetch = createDemoFetch('viewer', { origin });
    const binding = `${org}/scim-bindings/bnd_aaaaaaaa-aaaa-4aaa-8aaa-000000000001`;
    const fixtures = [
      { path: `${project}/service-accounts`, schema: zServiceAccountList },
      { path: `${project}/service-accounts/svc_aaaaaaaa-aaaa-4aaa-8aaa-000000000001/credentials`, schema: zMachineCredentialList },
      { path: `${project}/service-accounts/svc_aaaaaaaa-aaaa-4aaa-8aaa-000000000002/credentials`, schema: zMachineCredentialList },
      { path: `${project}/approval-policies`, schema: zApprovalPolicyList },
      { path: `${env()}/approval-requests`, schema: zApprovalRequestList },
      { path: `${project}/access-policies`, schema: zAccessPolicyList },
      { path: `${env()}/access-requests`, schema: zAccessQueue },
      { path: `${project}/adapters`, schema: zAdapterList },
      { path: `${project}/adapter-targets/adt_aaaaaaaa-aaaa-4aaa-8aaa-000000000001`, schema: zAdapterTargetDetail },
      { path: `${project}/file-targets`, schema: zFileTargetList },
      { path: `${project}/file-targets/ftg_aaaaaaaa-aaaa-4aaa-8aaa-000000000001`, schema: zFileTarget },
      { path: `${project}/developer-credentials`, schema: zDeveloperCredentialList },
      { path: `${org}/scim-bindings`, schema: zScimBindingList },
      { path: `${binding}/mappings`, schema: zScimMappingList },
      { path: `${binding}/credentials`, schema: zScimCredentialList },
      { path: `${binding}/directory/users`, schema: zScimDirectoryUserList },
      { path: `${binding}/directory/groups`, schema: zScimDirectoryGroupList },
      { path: `${project}/dynamic-providers`, schema: zDynamicProviderList },
      { path: `${env()}/leases`, schema: zDynamicLeaseList },
      { path: `${env()}/certificates`, schema: zCertificateList },
      { path: `${env()}/delivery-targets`, schema: zDeliveryTargetList },
      { path: `${env()}/ssh-cas`, schema: zSshcaList },
      { path: `${env()}/ssh-profiles`, schema: zSshProfileList },
      { path: `${env()}/ssh-certificates`, schema: zSshCertificateList },
      { path: `${org}/audit`, schema: zAuditPage },
      { path: `${project}/audit`, schema: zAuditPage },
      { path: '/api/v1/instance/remotes', schema: zRemoteList },
      { path: '/api/v1/instance/pairing-invites', schema: zRemotePairingInviteList },
      { path: '/api/v1/instance/directory', schema: zDirectoryListing },
      { path: '/api/v1/instance/connections', schema: zInstanceConnectionList },
      { path: '/api/v1/instance/workspace-origins', schema: zWorkspaceOriginList },
      { path: '/api/v1/me/profile', schema: zAccountProfile },
      { path: '/api/v1/me/sessions', schema: zSessionList },
      { path: '/api/v1/auth/identities', schema: zIdentityList },
      { path: '/api/v1/auth/webauthn/credentials', schema: zPasskeyList },
    ];
    for (const { path, schema } of fixtures) {
      const response = await fetch(path);
      expect(response.status, path).toBe(200);
      const result = schema.safeParse(await response.json());
      expect(result.success, `${path}: ${result.error?.message ?? ''}`).toBe(true);
    }
    const meta = zMeta.parse(await (await fetch('/api/v1/meta')).json());
    expect(meta.protocol_capabilities).toContain('delivery-target-report/1');
    for (const environment of [DEMO_IDS.development, DEMO_IDS.staging, DEMO_IDS.production]) {
      const reports = zDeliveryTargetList.parse(await (await fetch(`${env(environment)}/delivery-targets`)).json());
      expect(reports).toEqual({ principals: [], targets: [] });
    }
    const passkeys = zPasskeyList.parse(await (await fetch('/api/v1/auth/webauthn/credentials')).json());
    expect(passkeys.passkeys.map((passkey) => passkey.label)).toEqual(['Sample security key']);
    const accounts = zServiceAccountList.parse(await (await fetch(`${project}/service-accounts`)).json());
    expect(accounts.items.map((item) => item.name)).toEqual(['payments-api', 'release-pipeline']);
    const adapters = zAdapterList.parse(await (await fetch(`${project}/adapters`)).json());
    expect(adapters.items[0]?.targets[0]?.keys.length).toBe(6);
    const fileTargets = zFileTargetList.parse(await (await fetch(`${project}/file-targets`)).json());
    expect(fileTargets.items.map((item) => item.name)).toEqual(['payments-api-env', 'release-pipeline-env']);
    expect(fileTargets.items.map((item) => item.report === null)).toEqual([false, true]);
    const credentials = zDeveloperCredentialList.parse(await (await fetch(`${project}/developer-credentials`)).json());
    expect(credentials.items.map((item) => item.environment_id)).toEqual([DEMO_IDS.staging, DEMO_IDS.development]);
    expect(credentials.items.filter((item) => item.revoked_at !== undefined)).toHaveLength(1);
    const requests = zAccessQueue.parse(await (await fetch(`${env()}/access-requests`)).json());
    expect(requests.items[0]?.reason).toContain('Sample request');
  });

  it.each(DEMO_ACCOUNTS)('shows pairing metadata for $label without enabling pairing', async ({ id }) => {
    const fetch = createDemoFetch(id, { origin });
    const path = '/api/v1/instance/pairing-invites';
    const response = await fetch(path);
    expect(response.status).toBe(200);
    const invitations = zRemotePairingInviteList.parse(await response.json());
    expect(invitations).toEqual({ items: [], count: 0, origin: 'https://demo.hikyo.example.test' });
    const mutations = [
      { path: '/api/v1/instance/pairing-requests', method: 'POST' },
      { path, method: 'POST' },
      { path: `${path}/sample-invitation`, method: 'DELETE' },
      { path: '/api/v1/instance/remotes/pair', method: 'POST' },
    ];
    for (const mutation of mutations) {
      const refused = await fetch(mutation.path, { method: mutation.method, body: '{}' });
      expect(refused.status, `${mutation.method} ${mutation.path}`).toBe(404);
      expect(zError.safeParse(await refused.json()).success).toBe(true);
    }
    expect(zRemotePairingInviteList.parse(await (await fetch(path)).json())).toEqual(invitations);
  });

  it('filters sample audit rows and never accepts mutations on the expanded read-only surfaces', async () => {
    const fetch = createDemoFetch('admin', { origin });
    const audit = zAuditPage.parse(await (await fetch(`${project}/audit?outcomes=denied`)).json());
    expect(audit.items).toHaveLength(1);
    expect(audit.items[0]?.outcome).toBe('denied');
    expect(audit.exhausted).toBe(true);
    expect(zAuditPage.parse(await (await fetch(`${project}/audit?operation=revision.*&actor_name=Alex*`)).json()).items[0]?.type).toBe('revision.published');
    expect(zAuditPage.parse(await (await fetch(`${project}/audit?operation=revision.+`)).json()).count).toBe(0);
    expect(zAuditPage.parse(await (await fetch(`${project}/audit?actor_name=nobody`)).json()).count).toBe(0);
    const paths = [
      `${project}/service-accounts`, `${project}/approval-policies`, `${env()}/approval-requests`,
      `${project}/access-policies`, `${env()}/access-requests`, `${project}/adapters`, `${env()}/delivery-targets`,
      `${project}/file-targets`, `${project}/file-targets/ftg_aaaaaaaa-aaaa-4aaa-8aaa-000000000001`,
      `${project}/developer-credentials`, `${org}/scim-bindings`, '/api/v1/instance/remotes',
      '/api/v1/instance/connections', '/api/v1/me/profile',
    ];
    for (const path of paths) {
      for (const method of ['POST', 'PUT', 'PATCH', 'DELETE']) {
        const response = await fetch(path, { method, body: '{}' });
        expect(response.ok, `${method} ${path}`).toBe(false);
        expect(zError.safeParse(await response.json()).success).toBe(true);
      }
    }
  });

  it('reports a bounded renewable demo reveal window independently of the session lifetime', async () => {
    const fetch = createDemoFetch('admin', { origin });
    const startedAt = Date.now();
    const window = zRevealWindow.parse(await (await fetch(`${env()}/reveal-window`)).json());
    const finishedAt = Date.now();
    expect(window).toMatchObject({ live: true, can_reveal: true, effective_window_seconds: 900 });
    if (window.expires_at === undefined) throw new Error('The live demo window needs an expiry');
    const expiresAt = Date.parse(window.expires_at);
    expect(expiresAt).toBeGreaterThanOrEqual(startedAt + 900_000);
    expect(expiresAt).toBeLessThanOrEqual(finishedAt + 900_000);
    const session = zWhoAmI.parse(await (await fetch('/api/v1/auth/whoami')).json()).session;
    expect(Date.parse(session.absolute_expires_at)).toBeGreaterThan(expiresAt);
  });

  it('stages privately, publishes selectively, then exposes the value and new history through real SDK operations', async () => {
    const fetch = createDemoFetch('admin', { origin });
    const client = createClient(createConfig({ baseUrl: origin, fetch }));
    const path = { org: DEMO_IDS.org, project: DEMO_IDS.project, environment: DEMO_IDS.development };
    const draft = await parsed(setValueOp, { client, path: { ...path, key: 'LOG_LEVEL' }, body: { value: 'trace' } });
    const untouchedDraft = await stage(fetch, 'FEATURE_CHECKOUT', 'false');
    const before = await parsed(listValuesOp, { client, path });
    expect(before.items.find((cell) => cell.name === 'LOG_LEVEL')?.value).toBe('debug');
    const pending = zPendingDraftList.parse(await (await fetch(`${env()}/pending`)).json());
    expect(pending.items.find((cell) => cell.name === 'LOG_LEVEL')?.value).toBe('trace');
    const result = await parsed(publishPendingChangesOp, { client, path, body: { version_ids: [draft.version_id] } });
    expect(result).toMatchObject({ published: [draft.version_id], environments: [{ revision: 2n }] });
    const after = await parsed(listValuesOp, { client, path });
    expect(after.items.find((cell) => cell.name === 'LOG_LEVEL')?.value).toBe('trace');
    expect(after.items.find((cell) => cell.name === 'FEATURE_CHECKOUT')?.value).toBe('true');
    const remaining = zPendingDraftList.parse(await (await fetch(`${env()}/pending`)).json());
    expect(remaining.items.map((item) => item.version_id)).toEqual([untouchedDraft.version_id]);
    const history = zRevisionList.parse(await (await fetch(`${env()}/revisions`)).json());
    expect(history.items[0]).toMatchObject({ revision: 2n, changed_keys: [{ name: 'LOG_LEVEL', change: 'edited' }] });
    const signals = zEnvironmentSignals.parse(await (await fetch(`${env()}/signals`)).json());
    expect(signals.cells.find((cell) => cell.name === 'LOG_LEVEL')?.changed_in_revision).toBe(2n);
    expect(signals.cells.find((cell) => cell.name === 'FEATURE_CHECKOUT')).toMatchObject({ pending_version_id: untouchedDraft.version_id });
    expect(signals.cells.find((cell) => cell.name === 'FEATURE_CHECKOUT')).not.toHaveProperty('changed_in_revision');
    expect(signals.cells.find((cell) => cell.name === 'PUBLIC_APP_URL')).not.toHaveProperty('changed_in_revision');
    const otherEnvironment = zEnvironmentSignals.parse(await (await fetch(`${env(DEMO_IDS.staging)}/signals`)).json());
    expect(otherEnvironment.cells.find((cell) => cell.name === 'LOG_LEVEL')?.changed_in_revision).toBe(1n);
  });

  it('masks secrets in ordinary and draft reads; only admin can reveal fake material', async () => {
    const admin = createDemoFetch('admin', { origin });
    await stage(admin, 'AUTH_SECRET', 'demo-edited-secret');
    const pending = zPendingDraftList.parse(await (await admin(`${env()}/pending`)).json());
    expect(pending.items[0]).toMatchObject({ revealed: false });
    expect(pending.items[0]).not.toHaveProperty('value');
    const values = zValueList.parse(await (await admin(`${env()}/values`)).json());
    expect(values.items.find((item) => item.name === 'AUTH_SECRET')).not.toHaveProperty('value');
    const revealed = zValueCell.parse(await (await admin(`${env()}/values/AUTH_SECRET/reveal`, { method: 'POST' })).json());
    expect(revealed.value).toBe('DEMO_ONLY_development_not_a_real_secret');
    for (const role of ['viewer', 'developer']) {
      const fetch = createDemoFetch(role === 'viewer' ? 'viewer' : 'developer', { origin });
      expect((await fetch(`${env()}/values/AUTH_SECRET/reveal`, { method: 'POST' })).status).toBe(403);
      expect((await fetch(`${env()}/values/reveal`, { method: 'POST' })).status).toBe(403);
    }
  });

  it('enforces viewer and developer permissions even when UI controls are bypassed', async () => {
    const viewer = createDemoFetch('viewer', { origin });
    expect((await viewer(`${env()}/values/LOG_LEVEL`, mutation('trace'))).status).toBe(403);
    const developer = createDemoFetch('developer', { origin });
    await stage(developer, 'LOG_LEVEL', 'trace');
    await stage(developer, 'LOG_LEVEL', 'debug', DEMO_IDS.staging);
    expect((await developer(`${env(DEMO_IDS.production)}/values/LOG_LEVEL`, mutation('trace'))).status).toBe(403);
    expect((await developer(`${env()}/publish`, { method: 'POST', body: '{}' })).status).toBe(403);
    expect((await developer(`${project}/definitions/settings`, { method: 'PUT', body: '{"definitions_source":"git"}' })).status).toBe(404);
  });

  it('isolates tabs and reloads and rejects stale or cross-environment publishes without partial writes', async () => {
    const first = createDemoFetch('admin', { origin });
    const second = createDemoFetch('admin', { origin });
    const draft = await stage(first, 'LOG_LEVEL', 'trace');
    expect(zPendingDraftList.parse(await (await second(`${env()}/pending`)).json()).count).toBe(0);
    const invalid = await first(`${env(DEMO_IDS.staging)}/publish`, { method: 'POST', body: JSON.stringify({ version_ids: [draft.version_id] }) });
    expect(invalid.status).toBe(409);
    await stage(first, 'LOG_LEVEL', 'warning');
    expect((await first(`${env()}/publish`, { method: 'POST', body: JSON.stringify({ version_ids: [draft.version_id] }) })).status).toBe(409);
    expect(zPendingDraftList.parse(await (await first(`${env()}/pending`)).json()).count).toBe(1);
    const fresh = createDemoFetch('admin', { origin });
    expect(zValueList.parse(await (await fresh(`${env()}/values`)).json()).items.find((item) => item.name === 'LOG_LEVEL')?.value).toBe('debug');
  });

  it('can publish unsets and keeps the request environment revisions independent', async () => {
    const fetch = createDemoFetch('admin', { origin });
    const draft = zPendingChange.parse(await (await fetch(`${env()}/values/LOG_LEVEL`, { method: 'DELETE' })).json());
    const response = await fetch(`${env()}/publish`, { method: 'POST', body: JSON.stringify({ version_ids: [draft.version_id] }) });
    expect(zPublishResult.parse(await response.json()).environments[0]?.revision).toBe(2n);
    const values = zValueList.parse(await (await fetch(`${env()}/values`)).json());
    expect(values.items.find((item) => item.name === 'LOG_LEVEL')).toMatchObject({ set: false, revealed: false });
    const signals = zEnvironmentSignals.parse(await (await fetch(`${env()}/signals`)).json());
    expect(signals.cells.find((cell) => cell.name === 'LOG_LEVEL')?.changed_in_revision).toBe(2n);
    expect(signals.cells.find((cell) => cell.name === 'FEATURE_CHECKOUT')).not.toHaveProperty('changed_in_revision');
    const seededProduction = zEnvironmentSignals.parse(await (await fetch(`${env(DEMO_IDS.production)}/signals`)).json());
    expect(seededProduction.cells.find((cell) => cell.name === 'LOG_LEVEL')?.changed_in_revision).toBe(1n);
    expect(seededProduction.cells.find((cell) => cell.name === 'AUTH_SECRET')).not.toHaveProperty('changed_in_revision');
    expect(zEnvironmentSignals.parse(await (await fetch(`${env(DEMO_IDS.staging)}/signals`)).json()).revision).toBe(1n);
    const oldSnapshot = zRevisionDetail.parse(await (await fetch(`${env()}/revisions/1`)).json());
    const newSnapshot = zRevisionDetail.parse(await (await fetch(`${env()}/revisions/2`)).json());
    expect(oldSnapshot.keys.some((key) => key.name === 'LOG_LEVEL')).toBe(true);
    expect(newSnapshot.keys.some((key) => key.name === 'LOG_LEVEL')).toBe(false);
    const stagingSnapshot = zRevisionDetail.parse(await (await fetch(`${env(DEMO_IDS.staging)}/revisions/1`)).json());
    const productionSnapshot = zRevisionDetail.parse(await (await fetch(`${env(DEMO_IDS.production)}/revisions/1`)).json());
    expect(stagingSnapshot.keys.some((key) => key.name === 'LOG_LEVEL')).toBe(true);
    expect(productionSnapshot.keys.some((key) => key.name === 'AUTH_SECRET')).toBe(false);
    expect((await fetch(`${env(DEMO_IDS.staging)}/revisions/2`)).status).toBe(404);
    const nextDraft = await stage(fetch, 'FEATURE_CHECKOUT', 'false');
    await fetch(`${env()}/publish`, { method: 'POST', body: JSON.stringify({ version_ids: [nextDraft.version_id] }) });
    const nextSignals = zEnvironmentSignals.parse(await (await fetch(`${env()}/signals`)).json());
    expect(nextSignals.cells.find((cell) => cell.name === 'LOG_LEVEL')).not.toHaveProperty('changed_in_revision');
    expect(nextSignals.cells.find((cell) => cell.name === 'FEATURE_CHECKOUT')?.changed_in_revision).toBe(3n);
  });

  it('never forwards unsupported API calls or remote traffic, but can fetch same-origin static assets', async () => {
    const assetFetch = vi.fn<typeof fetch>().mockResolvedValue(new Response('asset'));
    const fetch = createDemoFetch('admin', { origin, assetFetch });
    for (const url of ['/api/v1/unknown', `${origin}/api/v1/unknown`, 'https://remote.example.test/api/v1/auth/whoami', 'https://remote.example.test/payload']) {
      const response = await fetch(url);
      expect(response.ok).toBe(false);
      expect(zError.safeParse(await response.json()).success).toBe(true);
    }
    expect(assetFetch).not.toHaveBeenCalled();
    expect(await (await fetch(`${origin}/assets/app.js`)).text()).toBe('asset');
    expect(assetFetch).toHaveBeenCalledTimes(1);
    expect((await fetch(`${env()}/values/LOG_LEVEL`, { method: 'PUT', body: '{bad-json' })).status).toBe(400);
  });
});

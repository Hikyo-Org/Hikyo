// @vitest-environment happy-dom
import { act } from 'react';
import { Link, MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { deferred } from '../testkit/ceremony.ts';
import { makeQueryClient } from '../app/queryClient.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { dynamicProvider, serviceAccount } from '../testkit/machineAccess.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { MachineAccessPage } from './MachineAccess.tsx';

const ready = { isSuccess: true, isPending: false, isError: false, error: null };
vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: authenticatedIdentity, refreshSession: vi.fn() }),
}));
vi.mock('../api/identities.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/identities.ts')>()),
  useServiceAccounts: () => ({ ...ready, data: { items: [], count: 0 } }),
  useProjectGrants: () => ({ ...ready, data: { items: [], count: 0 } }),
  useCredentials: () => ({ byAccount: new Map(), isPending: false, isError: false }),
}));
vi.mock('../api/machineReveal.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/machineReveal.ts')>()),
  useMachineReveal: () => ({ ...ready, data: { enabled: false } }),
}));
vi.mock('../api/dynamic.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/dynamic.ts')>()),
  useDynamicProviders: () => ({ ...ready, data: { items: [dynamicProvider], count: 1 } }),
}));
vi.mock('../api/deliveryTargets.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/deliveryTargets.ts')>()),
  useDeliveryTargets: () => ({ reports: [], failures: [], isPending: false, support: 'unsupported' }),
  useReportingSupport: () => 'unsupported',
}));

// Environment, lease and certificate reads remain the real generated operations,
// including the shared null marker and the real empty-fan-out aggregation.
const org = 'org_00000000-0000-0000-0000-000000000001';
const project = 'prj_00000000-0000-0000-0000-000000000001';
const environment = 'env_00000000-0000-0000-0000-000000000001';
const at = '2026-10-16T10:00:00Z';
const environmentKey = ['environments', org, project];
const lease = {
  id: 'lease_00000000-0000-0000-0000-000000000001', provider_id: dynamicProvider.id,
  environment_id: environment, principal_id: serviceAccount.principal_id,
  principal_class: 'workload', provider_handle: 'existing-role', state: 'active',
  issued_at: at, expires_at: '2026-10-17T10:00:00Z', max_ttl_seconds: 3600,
  last_transition_at: at, created_at: at,
};
const certificate = {
  id: 'cert_00000000-0000-0000-0000-000000000001', environment_id: environment,
  profile: 'web', issuer_id: 'pkii_00000000-0000-0000-0000-000000000001',
  issuer_name: 'issuer', issuer_version: 1, serial: 'abc123', state: 'issued',
  key_source: 'csr', key_algorithm: 'ecdsa-p256', key_fingerprint: 'sha256:public',
  dns_names: ['service.example.com'], ip_addresses: [], uris: [],
  not_before: at, not_after: '2026-10-17T10:00:00Z', principal_id: serviceAccount.principal_id,
  principal_class: 'workload', created_at: at, updated_at: at,
};

function RouteUnderTest() {
  return <MemoryRouter initialEntries={[`/orgs/${org}/projects/${project}/machine-access?tab=leases`]}>
    <Link to={`/orgs/${org}/projects/new-project/machine-access?tab=leases`}>New project</Link>
    <Routes><Route path="/orgs/:org/projects/:project/machine-access" element={<MachineAccessPage />} /></Routes>
  </MemoryRouter>;
}

function countedTab(container: HTMLElement, name: string, count: number | 'unknown') {
  const tab = container.querySelector(`[role="tab"][aria-label="${name}, ${String(count)}"]`);
  if (!(tab instanceof HTMLButtonElement)) throw new Error(`Missing ${name}, ${String(count)} tab`);
  return tab;
}

async function click(container: HTMLElement, label: string) {
  const button = [...container.querySelectorAll('button')].find((candidate) => candidate.textContent?.trim() === label);
  if (button === undefined) throw new Error(`Missing ${label} button`);
  expect(button.disabled).toBe(false);
  await act(async () => button.click());
  await settleTask();
}

afterEach(() => vi.unstubAllGlobals());

it.each([403, 404])('retired %i environment metadata leaves credential inventories and provider removal counts unknown until a successful read', async (status) => {
  let phase: 'loaded' | 'denied' | 'offline' | 'server-error' | 'empty' = 'loaded';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith('/environments')) {
      if (phase === 'denied') return Response.json({ error: { code: status === 404 ? 'not_found' : 'forbidden', message: 'Denied' } }, { status });
      if (phase === 'offline') throw new TypeError('Failed to fetch');
      if (phase === 'server-error') return Response.json({ error: { code: 'internal', message: 'Unavailable' } }, { status: 500 });
      return Response.json(phase === 'empty' ? { count: 0, items: [] } : { count: 1, items: [{
        id: environment, org_id: org, project_id: project, name: 'Production', display_order: 0, created_at: at,
      }] });
    }
    if (path.endsWith('/leases')) return Response.json({ items: [lease] });
    if (path.endsWith('/certificates')) return Response.json({ certificates: [certificate] });
    throw new Error(`Unexpected ${request.method} ${path}`);
  }));
  const view = await renderForm(<RouteUnderTest />, { client: makeQueryClient() });
  try {
    await vi.waitFor(async () => {
      await settleTask();
      countedTab(view.container, 'Leases', 1);
      countedTab(view.container, 'Certificates', 1);
    });
    expect(view.container.textContent).toContain('existing-role');
    for (const failure of ['denied', 'offline', 'server-error'] satisfies readonly ('denied' | 'offline' | 'server-error')[]) {
      phase = failure;
      await act(async () => { await view.client.invalidateQueries({ queryKey: environmentKey, exact: true }); });
      await settleTask();
      expect(view.client.getQueryData(environmentKey)).toBeNull();
      await act(async () => countedTab(view.container, 'Leases', 'unknown').click());
      expect(view.container.textContent).not.toContain('No dynamic-secret leases on this project yet');
      expect(view.container.textContent).toContain('The leases could not be read');
      await act(async () => countedTab(view.container, 'Certificates', 'unknown').click());
      expect(view.container.textContent).not.toContain('No certificates in this project yet');
      expect(view.container.textContent).toContain('the table is incomplete');
      await act(async () => countedTab(view.container, 'Providers', 1).click());
      await click(view.container, 'Delete');
      expect(view.container.textContent).toContain('the number of live leases is unknown');
      expect(view.container.textContent).not.toContain('This provider has no live leases');
      await click(view.container, 'Cancel');
    }
    phase = 'empty';
    await act(async () => { await view.client.invalidateQueries({ queryKey: environmentKey, exact: true }); });
    await settleTask();
    await act(async () => countedTab(view.container, 'Leases', 0).click());
    expect(view.container.textContent).toContain('No dynamic-secret leases on this project yet');
    await act(async () => countedTab(view.container, 'Certificates', 0).click());
    expect(view.container.textContent).toContain('No certificates in this project yet');
    await act(async () => countedTab(view.container, 'Providers', 1).click());
    await click(view.container, 'Delete');
    expect(view.container.textContent).toContain('This provider has no live leases');
  } finally { await view.unmount(); }
});


it('keeps a new project inventory unknown while its hierarchy loads, then accepts authoritative empty inventory', async () => {
  const nextEnvironment = deferred<Response>();
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path.includes('/projects/new-project/environments')) return nextEnvironment.promise;
    if (path.endsWith('/environments')) return Response.json({ count: 1, items: [{
      id: environment, org_id: org, project_id: project, name: 'Production', display_order: 0, created_at: at,
    }] });
    if (path.endsWith('/leases')) return Response.json({ items: [lease] });
    if (path.endsWith('/certificates')) return Response.json({ certificates: [certificate] });
    throw new Error(`Unexpected ${request.method} ${path}`);
  }));
  const view = await renderForm(<RouteUnderTest />, { client: makeQueryClient() });
  try {
    await vi.waitFor(async () => {
      await settleTask();
      countedTab(view.container, 'Leases', 1);
      countedTab(view.container, 'Certificates', 1);
    });
    const link = view.container.querySelector('a');
    if (!(link instanceof HTMLAnchorElement)) throw new Error('scope navigation missing');
    await act(async () => link.click());
    await settleTask();
    countedTab(view.container, 'Leases', 'unknown');
    countedTab(view.container, 'Certificates', 'unknown');
    expect(view.container.textContent).toContain('Loading leases');
    expect(view.container.textContent).not.toContain('existing-role');
    expect(view.container.textContent).not.toContain('No dynamic-secret leases on this project yet');
    await act(async () => countedTab(view.container, 'Certificates', 'unknown').click());
    expect(view.container.textContent).toContain('Loading certificates');
    expect(view.container.textContent).not.toContain('No certificates in this project yet');
    await act(async () => nextEnvironment.resolve(Response.json({ items: [], count: 0 })));
    await settleTask();
    countedTab(view.container, 'Leases', 0);
    countedTab(view.container, 'Certificates', 0);
    expect(view.container.textContent).toContain('No certificates in this project yet');
    await act(async () => countedTab(view.container, 'Leases', 0).click());
    expect(view.container.textContent).toContain('No dynamic-secret leases on this project yet');
  } finally { await view.unmount(); }
});

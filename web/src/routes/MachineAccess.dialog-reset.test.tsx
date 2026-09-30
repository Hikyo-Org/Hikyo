// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { DynamicLease } from '../api/dynamic.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { dynamicProvider, serviceAccount } from '../testkit/machineAccess.ts';
import { deferred } from '../testkit/ceremony.ts';
import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { MachineAccessPage } from './MachineAccess.tsx';

const mocks = vi.hoisted(() => ({ session: 'session-first', mintCredential: vi.fn(), mintLease: vi.fn() }));
const accounts = { items: [serviceAccount], count: 1 };
const environments = { items: [{ id: 'env-one', name: 'production' }], count: 1 };
const providers = { items: [dynamicProvider], count: 1 };
const ready = { isSuccess: true, isPending: false, isError: false, error: null };
const lease: DynamicLease = {
  id: 'lease-one', provider_id: dynamicProvider.id, environment_id: 'env-one',
  principal_id: serviceAccount.principal_id, principal_class: 'workload', provider_handle: 'lease-role',
  state: 'active', issued_at: '2026-09-22T08:00:00Z', expires_at: '2026-09-23T08:00:00Z',
  max_ttl_seconds: 3600n, last_transition_at: '2026-09-22T08:00:00Z', created_at: '2026-09-22T08:00:00Z',
};
const leases = { rows: [{ environmentName: 'production', lease }], isPending: false, isError: false };

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: {
    ...authenticatedIdentity, session: { ...authenticatedIdentity.session, id: mocks.session },
  }, refreshSession: vi.fn() }),
}));
vi.mock('../api/identities.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/identities.ts')>()),
  useServiceAccounts: () => ({ ...ready, data: accounts }),
  useProjectGrants: () => ({ ...ready, data: { items: [], count: 0 } }),
  useCredentials: () => ({ byAccount: new Map(), isPending: false, isError: false }),
  useKeyCatalogue: () => ({ ...ready, data: { items: [], count: 0 } }),
  useRefreshAccount: () => vi.fn(),
  mintCredential: mocks.mintCredential,
}));
vi.mock('../api/values.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/values.ts')>()),
  useEnvironments: () => ({ ...ready, data: environments }),
  runPasskeyCeremony: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../api/machineReveal.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/machineReveal.ts')>()),
  useMachineReveal: () => ({ ...ready, data: { enabled: false } }),
}));
vi.mock('../api/dynamic.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/dynamic.ts')>()),
  useDynamicProviders: () => ({ ...ready, data: providers }),
  useLeases: () => leases,
  useRefreshLeases: () => vi.fn(),
  mintLease: mocks.mintLease,
}));
vi.mock('../api/deliveryTargets.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/deliveryTargets.ts')>()),
  useDeliveryTargets: () => ({ reports: [], failures: [], isPending: false, support: 'unsupported' }),
  useReportingSupport: () => 'unsupported',
}));
vi.mock('../api/pki.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/pki.ts')>()),
  useCertificates: () => ({ rows: [], isPending: false, isError: false }),
}));

function ProjectNavigation() {
  const navigate = useNavigate();
  return <>
    <button onClick={() => navigate('/orgs/org-next/projects/project-first/machine-access')}>Change org</button>
    <button onClick={() => navigate('/orgs/org-first/projects/project-first/machine-access')}>Return org</button>
    <button onClick={() => navigate('/orgs/org-first/projects/project-next/machine-access')}>Change project</button>
    <button onClick={() => navigate('/orgs/org-first/projects/project-first/machine-access')}>Return project</button>
  </>;
}
function RouteUnderTest() {
  return (
    <MemoryRouter initialEntries={['/orgs/org-first/projects/project-first/machine-access']}>
      <ProjectNavigation />
      <Routes><Route path="/orgs/:org/projects/:project/machine-access" element={<MachineAccessPage />} /></Routes>
    </MemoryRouter>
  );
}
let view: Awaited<ReturnType<typeof renderForm>> | undefined;
beforeEach(() => {
  mocks.session = 'session-first';
  mocks.mintCredential.mockReset().mockResolvedValue({ value: 'credential-SENTINEL', clamped: false, expires_at: null });
  mocks.mintLease.mockReset().mockResolvedValue({ username: 'lease-user', password: 'lease-SENTINEL', expires_at: null });
});
afterEach(async () => { await view?.unmount(); view = undefined; vi.unstubAllGlobals(); });

async function click(name: string) {
  const buttons = Array.from(view?.container.querySelectorAll('button') ?? []);
  const button = buttons.find((node) => node.textContent?.trim() === name || node.getAttribute('aria-label') === name);
  if (button === undefined) throw new Error(`No button ${name}`);
  expect(button.disabled, `${name} is enabled`).toBe(false);
  await act(async () => button.click());
  await settle();
}
async function tab(name: string) {
  const node = Array.from(view?.container.querySelectorAll('button[role="tab"]') ?? [])
    .find((candidate) => candidate.textContent?.startsWith(name));
  if (!(node instanceof HTMLButtonElement)) throw new Error(`No tab ${name}`);
  await act(async () => node.click());
}
async function boundary(kind: 'org' | 'project' | 'session') {
  if (kind === 'org') await click('Change org');
  else if (kind === 'project') await click('Change project');
  else { mocks.session = 'session-next'; await view?.rerender(<RouteUnderTest />); }
  await settle();
}
function dialogs() { return view?.container.querySelectorAll('dialog[open]').length ?? 0; }
function assertSecretAbsent(secret: string) {
  expect(view?.container.textContent).not.toContain(secret);
  expect(view?.container.innerHTML).not.toContain(secret);
  expect(Array.from(view?.container.querySelectorAll('input') ?? []).map((input) => input.value)).not.toContain(secret);
  expect(JSON.stringify(view?.client.getQueryCache().getAll())).not.toContain(secret);
  expect(JSON.stringify(view?.client.getMutationCache().getAll())).not.toContain(secret);
}
async function openCredential() {
  await click(`▸ ${serviceAccount.name}`);
  await click(`Mint credential for ${serviceAccount.name}`);
}

const dialogCases = [
  { name: 'create account', open: () => click('Create service account') },
  { name: 'binding', open: async () => { await click(`▸ ${serviceAccount.name}`); await click(`Add federated binding to ${serviceAccount.name}`); } },
  { name: 'grant', open: async () => { await click(`▸ ${serviceAccount.name}`); await click(`Add environment grant to ${serviceAccount.name}`); } },
  { name: 'delete account', open: async () => { await click(`▸ ${serviceAccount.name}`); await click(`Delete ${serviceAccount.name}`); } },
  { name: 'create provider', open: async () => { await tab('Providers'); await click('Configure provider'); } },
  { name: 'replace provider credential', open: async () => { await tab('Providers'); await click('Replace credential'); } },
  { name: 'revoke provider credential', open: async () => { await tab('Providers'); await click('Revoke credential'); } },
  { name: 'delete provider', open: async () => { await tab('Providers'); await click('Delete'); } },
  { name: 'lease mint form', open: async () => { await tab('Leases'); await click('Mint lease'); } },
  { name: 'lease renewal', open: async () => { await tab('Leases'); await click('Renew'); } },
  { name: 'lease revocation', open: async () => { await tab('Leases'); await click('Revoke'); } },
];

// The real route and real useMintLifecycle stay mounted across boundaries.
// Same-owner rerenders below prove closure is not an accidental harness remount.
describe('MachineAccess route owner boundaries', () => {
  for (const kind of ['org', 'project', 'session'] satisfies readonly ('org' | 'project' | 'session')[]) {
    for (const dialog of dialogCases) {
      it(`closes ${dialog.name} on ${kind} replacement`, async () => {
        view = await renderForm(<RouteUnderTest />);
        await dialog.open();
        expect(dialogs()).toBe(1);
        await view.rerender(<RouteUnderTest />);
        expect(dialogs()).toBe(1);
        await boundary(kind);
        expect(dialogs()).toBe(0);
      });
    }
    it(`closes the real policy confirmation on ${kind} replacement`, async () => {
      view = await renderForm(<RouteUnderTest />);
      await click('Enable the opt-in…');
      expect(dialogs()).toBe(1);
      await view.rerender(<RouteUnderTest />);
      expect(dialogs()).toBe(1);
      await boundary(kind);
      expect(dialogs()).toBe(0);
    });

    it(`clears the provider password input on ${kind} replacement`, async () => {
      view = await renderForm(<RouteUnderTest />);
      await tab('Providers');
      await click('Replace credential');
      const input = view.container.querySelector('input[type="password"]');
      if (!(input instanceof HTMLInputElement)) throw new Error('Missing provider credential field');
      await act(async () => typeInto(input, 'provider-SENTINEL'));
      expect(input.value).toBe('provider-SENTINEL');
      await view.rerender(<RouteUnderTest />);
      expect(view.container.querySelector('input[type="password"]')?.getAttribute('value')).toBe('provider-SENTINEL');
      await boundary(kind);
      expect(dialogs()).toBe(0);
      assertSecretAbsent('provider-SENTINEL');
    });

    for (const family of ['credential', 'lease']) {
      const sentinel = `${family}-SENTINEL`;
      const open = family === 'credential' ? openCredential : async () => { await tab('Leases'); await click('Mint lease'); };
      const submit = family === 'credential' ? 'Mint credential' : 'Use a passkey and mint';
      it(`clears disclosed ${family} on ${kind} replacement without caching its value`, async () => {
        view = await renderForm(<RouteUnderTest />);
        await open();
        await click(submit);
        expect(view.container.textContent).toContain(sentinel);
        expect(JSON.stringify(view.client.getQueryCache().getAll())).not.toContain(sentinel);
        expect(JSON.stringify(view.client.getMutationCache().getAll())).not.toContain(sentinel);
        await view.rerender(<RouteUnderTest />);
        expect(view.container.textContent).toContain(sentinel);
        await boundary(kind);
        expect(dialogs()).toBe(0);
        assertSecretAbsent(sentinel);
      });

      it(`ignores late ${family} completion after ${kind} replacement and returning to the old owner`, async () => {
        const pending = deferred<{ value: string; password: string; username: string; expires_at: null; clamped: boolean }>();
        const mint = family === 'credential' ? mocks.mintCredential : mocks.mintLease;
        mint.mockReturnValueOnce(pending.promise);
        view = await renderForm(<RouteUnderTest />);
        await open();
        await click(submit);
        expect(mint).toHaveBeenCalledTimes(1);
        expect(dialogs()).toBe(1);
        await boundary(kind);
        expect(dialogs()).toBe(0);
        if (kind === 'org') await click('Return org');
        else if (kind === 'project') await click('Return project');
        else { mocks.session = 'session-first'; await view.rerender(<RouteUnderTest />); }
        await act(async () => pending.resolve({ value: sentinel, password: sentinel, username: 'lease-user', expires_at: null, clamped: false }));
        await settle();
        expect(dialogs()).toBe(0);
        assertSecretAbsent(sentinel);
      });
    }
  }
});

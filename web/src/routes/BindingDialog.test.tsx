// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { MachineCredential, ServiceAccount } from '../api/identities.ts';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import { BindingDialog } from './machineAccess/FederationBindings.tsx';

const mocks = vi.hoisted(() => ({ ceremony: vi.fn() }));
vi.mock('../api/values.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/values.ts')>()),
  runPasskeyCeremony: mocks.ceremony,
}));
beforeEach(() => mocks.ceremony.mockReset().mockResolvedValue(undefined));

const account: ServiceAccount = {
  id: 'msa_123e4567-e89b-12d3-a456-426614174020',
  principal_id: 'prn_123e4567-e89b-12d3-a456-426614174020',
  name: 'worker', kind: 'workload', created_at: '2026-08-01T00:00:00Z',
  created_by: 'prn_123e4567-e89b-12d3-a456-426614174010', live_credentials: 1,
};
const predecessor = (value: bigint): MachineCredential => ({
  id: 'mcr_123e4567-e89b-12d3-a456-426614174041', kind: 'oidc-federation',
  lifetime: 'finite', created_at: '2026-08-01T00:00:00Z',
  created_by: account.created_by, expiring_soon: false,
  issuer: 'https://token.actions.githubusercontent.com',
  subject: 'repo:owner/repo:ref:refs/heads/main', audience: 'hikyo',
  required_claims: [
    { claim: 'repository_id', string_value: '42' },
    { claim: 'repository_owner_id', string_value: '7' },
    { claim: 'event_name', string_value: 'push' },
    { claim: 'ref_protected', string_value: 'true' },
    { claim: 'runner_environment', string_value: 'github-hosted' },
    { claim: 'job_workflow_ref', string_value: 'owner/repo/.github/workflows/x.yml@refs/heads/main' },
    { claim: 'custom_id', number_value: value },
  ],
});
afterEach(() => vi.unstubAllGlobals());

it('runs a mint-purpose ceremony before replacing a historical-only binding', async () => {
  const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) => Promise.resolve(
    new Response(JSON.stringify({ code: 'refused', message: 'fixture refusal' }), {
      status: 422, headers: { 'Content-Type': 'application/json' },
    }),
  ));
  vi.stubGlobal('fetch', fetchMock);
  const view = await renderForm(
    <BindingDialog project={{ org: 'acme', project: 'payments' }} accounts={[account]} initial={account}
      replaces={predecessor(7n)} reachFor={() => [{ id: 'env-prod', name: 'production', current: false, historical: true }]}
      onClose={() => {}} onCreated={() => {}} />,
  );
  try {
    await submitReplacement(view.container);
    expect(mocks.ceremony).toHaveBeenCalledExactlyOnceWith({ operation: 'mint', environmentId: 'env-prod', keyIds: [] });
    const ceremonyOrder = mocks.ceremony.mock.invocationCallOrder[0];
    const requestOrder = fetchMock.mock.invocationCallOrder[0];
    if (ceremonyOrder === undefined || requestOrder === undefined) throw new Error('ceremony or request missing');
    expect(ceremonyOrder).toBeLessThan(requestOrder);
  } finally { await view.unmount(); }
});

it.each([
  { pin: { claim: 'ref_protected', string_value: 'true' }, expected: '"claim":"ref_protected","string_value":"true"' },
  { pin: { claim: 'ref_protected', bool_value: true }, expected: '"claim":"ref_protected","string_value":"true"' },
])('re-pins a required claim as the string GitHub emits ($expected)', async ({ pin, expected }) => {
  const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) => Promise.resolve(
    new Response(JSON.stringify({ code: 'refused', message: 'fixture refusal' }), {
      status: 422, headers: { 'Content-Type': 'application/json' },
    }),
  ));
  vi.stubGlobal('fetch', fetchMock);
  const base = predecessor(7n);
  const existing = { ...base, required_claims: (base.required_claims ?? []).map((original) => original.claim === pin.claim ? pin : original) };
  const view = await renderForm(
    <BindingDialog project={{ org: 'acme', project: 'payments' }} accounts={[account]} initial={account}
      replaces={existing} reachFor={() => []} onClose={() => {}} onCreated={() => {}} />,
  );
  try {
    await submitReplacement(view.container);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const request = fetchMock.mock.calls[0]?.[0];
    if (!(request instanceof Request)) throw new Error('binding request missing');
    expect(await request.clone().text()).toContain(expected);
  } finally { await view.unmount(); }
});

it('re-pins a legacy numeric GitHub id as the string GitHub emits', async () => {
  const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) => Promise.resolve(
    new Response(JSON.stringify({ code: 'refused', message: 'fixture refusal' }), {
      status: 422, headers: { 'Content-Type': 'application/json' },
    }),
  ));
  vi.stubGlobal('fetch', fetchMock);
  const base = predecessor(7n);
  const legacy = {
    ...base,
    required_claims: (base.required_claims ?? []).map((pin) =>
      pin.claim === 'repository_id' ? { claim: 'repository_id', number_value: 42n } : pin),
  };
  const view = await renderForm(
    <BindingDialog project={{ org: 'acme', project: 'payments' }} accounts={[account]} initial={account}
      replaces={legacy} reachFor={() => []} onClose={() => {}} onCreated={() => {}} />,
  );
  try {
    await submitReplacement(view.container);
    const request = fetchMock.mock.calls[0]?.[0];
    if (!(request instanceof Request)) throw new Error('binding request missing');
    expect(await request.clone().text()).toContain('"claim":"repository_id","string_value":"42"');
  } finally { await view.unmount(); }
});

it('replaces a legacy id past 2^53 exactly, because preset fields pin strings', async () => {
  const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) => Promise.resolve(
    new Response(JSON.stringify({ code: 'refused', message: 'fixture refusal' }), {
      status: 422, headers: { 'Content-Type': 'application/json' },
    }),
  ));
  vi.stubGlobal('fetch', fetchMock);
  const base = predecessor(7n);
  const legacy = {
    ...base,
    required_claims: (base.required_claims ?? []).map((pin) =>
      pin.claim === 'repository_id' ? { claim: 'repository_id', number_value: 9007199254740993n } : pin),
  };
  const view = await renderForm(
    <BindingDialog project={{ org: 'acme', project: 'payments' }} accounts={[account]} initial={account}
      replaces={legacy} reachFor={() => []} onClose={() => {}} onCreated={() => {}} />,
  );
  try {
    await submitReplacement(view.container);
    const request = fetchMock.mock.calls[0]?.[0];
    if (!(request instanceof Request)) throw new Error('binding request missing');
    expect(await request.clone().text()).toContain('"claim":"repository_id","string_value":"9007199254740993"');
  } finally { await view.unmount(); }
});

it('disables Forgejo Actions and explains the immutable-identity refusal', async () => {
  const view = await renderForm(
    <BindingDialog project={{ org: 'acme', project: 'payments' }} accounts={[account]} initial={account}
      reachFor={() => []} onClose={() => {}} onCreated={() => {}} />,
  );
  try {
    const forgejo = Array.from(view.container.querySelectorAll('button')).find((button) => button.textContent === 'Forgejo Actions');
    expect(forgejo?.disabled).toBe(true);
    expect(view.container.textContent).toContain('immutable repository identity');
  } finally { await view.unmount(); }
});

async function submitReplacement(container: HTMLElement) {
  const submit = Array.from(container.querySelectorAll('button')).find(
    (button) => button.textContent === 'Replace this binding',
  );
  if (submit === undefined) throw new Error('replacement submit missing');
  await act(async () => submit.click());
  await settle();
}

it.each([9007199254740993n, -9007199254740993n])(
  'preserves %s in the form and refuses lossy replacement before any request',
  async (value) => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const view = await renderForm(
      <BindingDialog project={{ org: 'acme', project: 'payments' }}
        accounts={[account]} initial={account} replaces={predecessor(value)}
        reachFor={() => []} onClose={() => {}} onCreated={() => {}} />,
    );
    try {
      expect(view.container.textContent).toContain(String(value));
      await submitReplacement(view.container);
      expect(view.container.textContent).toContain('A preserved numeric pin cannot be carried exactly');
      expect(fetchMock).not.toHaveBeenCalled();
    } finally { await view.unmount(); }
  },
);

it('carries a safe numeric pin exactly into the replacement request', async () => {
  const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) => Promise.resolve(
    new Response(JSON.stringify({ code: 'refused', message: 'fixture refusal' }), {
      status: 422, headers: { 'Content-Type': 'application/json' },
    }),
  ));
  vi.stubGlobal('fetch', fetchMock);
  const view = await renderForm(
    <BindingDialog project={{ org: 'acme', project: 'payments' }}
      accounts={[account]} initial={account} replaces={predecessor(9007199254740991n)}
      reachFor={() => []} onClose={() => {}} onCreated={() => {}} />,
  );
  try {
    await submitReplacement(view.container);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const request = fetchMock.mock.calls[0]?.[0];
    if (!(request instanceof Request)) throw new Error('binding request missing');
    const body = await request.clone().text();
    expect(body).toEqual(expect.stringContaining('"claim":"custom_id","number_value":9007199254740991'));
  } finally { await view.unmount(); }
});

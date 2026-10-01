// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import type { MachineCredential, ServiceAccount } from '../api/identities.ts';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import { BindingDialog } from './machineAccess/FederationBindings.tsx';

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
    { claim: 'repository_id', number_value: 42n },
    { claim: 'repository_owner_id', number_value: 7n },
    { claim: 'event_name', string_value: 'push' },
    { claim: 'custom_id', number_value: value },
  ],
});
afterEach(() => vi.unstubAllGlobals());

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

// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { TransitKeys } from './TransitKeys.tsx';

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ state: { status: 'authenticated' }, identity: { session: { id: 'ses_owner' } } }),
}));
const org = 'org_00000000-0000-7000-8000-000000000001';
const project = 'prj_00000000-0000-7000-8000-000000000001';
const environment = 'env_00000000-0000-7000-8000-000000000001';
const base = `/api/v1/orgs/${org}/projects/${project}/environments`;
const at = '2026-10-01T00:00:00Z';
const key = {
  id: 'trk_00000000-0000-7000-8000-000000000001', environment_id: environment,
  name: 'app-key', algorithm: 'xchacha20-poly1305', custody: 'software', allowed_operations: ['decrypt'], exportable: false,
  state: 'active', latest_version: 1, min_encrypt_version: 1, min_decrypt_version: 1, compromised_through_version: 0,
  rotation_period_seconds: 0, rotation_due: false, created_by: 'owner', created_at: at, updated_at: at,
  versions: [{ version: 1, material: 'sealed', created_at: at }], callers: [],
};
function Harness({ env = environment }: { env?: string }) {
  return <MemoryRouter initialEntries={[`/orgs/${org}/projects/${project}/transit-keys?env=${env}&key=app-key`]}><Routes><Route path="/orgs/:org/projects/:project/transit-keys" element={<TransitKeys />} /></Routes></MemoryRouter>;
}
function installFetch() {
  const state = { refused: '', removed: false };
  const calls: string[] = [];
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
    const request = input instanceof Request ? input : new Request(input);
    const path = new URL(request.url).pathname;
    calls.push(path);
    if (request.method === 'POST') return Promise.resolve(Response.json({ plaintext: 'cHVibGljLXRlc3QtZml4dHVyZQ==', key_version: 1 }));
    const kind = path === base ? 'environment' : path.endsWith('/transit-keys') ? 'list' : 'detail';
    if (state.refused === kind) return Promise.resolve(Response.json({ error: { code: 'forbidden', message: 'NO_RAW_DETAIL' } }, { status: 403 }));
    if (kind === 'environment') return Promise.resolve(Response.json({ items: state.removed ? [] : [{ id: environment, org_id: org, project_id: project, name: 'production', display_order: 0, created_at: at }], count: state.removed ? 0 : 1 }));
    return Promise.resolve(Response.json(kind === 'list' ? { items: [key] } : key));
  }));
  return { state, calls };
}
function plaintextVisible(container: HTMLElement) {
  return [...container.querySelectorAll('label')].some((label) => label.textContent === 'Plaintext output (base64)');
}
async function disclose(container: HTMLElement) {
  const input = [...container.querySelectorAll('textarea')].find((control) => control.id === [...container.querySelectorAll('label')].find((label) => label.textContent === 'Ciphertext')?.htmlFor);
  if (input === undefined) throw new Error('Missing ciphertext input');
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set?.call(input, 'fixture-ciphertext');
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () => { container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
  await settleTask();
}
afterEach(() => vi.unstubAllGlobals());
it.each(['environment', 'list', 'detail'])('retires cached metadata and plaintext on authoritative %s refusal', async (kind) => {
  const { state } = installFetch();
  const view = await renderForm(<Harness />);
  try {
    await settleTask(); await settleTask();
    await disclose(view.container);
    expect(plaintextVisible(view.container)).toBe(true);
    state.refused = kind;
    await act(async () => { await view.client.invalidateQueries(); });
    await settleTask();
    expect(plaintextVisible(view.container)).toBe(false);
    expect(view.container.querySelector('[aria-label="Cryptographic operations"]')).toBeNull();
    expect(view.container.textContent).not.toContain('NO_RAW_DETAIL');
  } finally { await view.unmount(); }
});
it('removes a deleted environment and its pending sensitive surface', async () => {
  const { state } = installFetch();
  const view = await renderForm(<Harness />);
  try {
    await settleTask(); await settleTask(); await disclose(view.container);
    expect(plaintextVisible(view.container)).toBe(true);
    state.removed = true;
    await act(async () => { await view.client.invalidateQueries({ queryKey: ['environments'] }); });
    await settleTask();
    expect(plaintextVisible(view.container)).toBe(false);
    expect(view.container.textContent).toContain('The selected environment is no longer available.');
  } finally { await view.unmount(); }
});
it('does not request keys for a missing selected environment', async () => {
  const { calls } = installFetch();
  const view = await renderForm(<Harness env="missing" />);
  try {
    await settleTask(); await settleTask();
    expect(calls.filter((path) => path.includes('/transit-keys'))).toHaveLength(0);
    expect(view.container.textContent).toContain('The selected environment is no longer available.');
  } finally { await view.unmount(); }
});

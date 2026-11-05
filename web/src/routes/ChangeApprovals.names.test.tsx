// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { consentIds, consentLookup, type ConsentLookupFailures } from '../testkit/consentNames.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { ChangeApprovals } from './ChangeApprovals.tsx';

const guard = vi.hoisted(() => ({ run: vi.fn() }));
vi.mock('./useProtectedPublishCeremony.ts', () => ({
  useProtectedPublishCeremony: () => ({ request: null, requestKey: 0, error: null, run: guard.run, onAuthorised: vi.fn(), onCancel: vi.fn() }),
}));

const at = '2026-09-01T00:00:00Z';
const request = (overrides: object = {}) => ({
  id: 'apr_00000000-0000-4000-8000-0000000000a1', environment_id: consentIds.environment,
  policy_id: 'pol_00000000-0000-4000-8000-0000000000b1', policy_version: 1,
  requester: 'prn_00000000-0000-4000-8000-0000000000c1', requester_name: 'Dana Jacobs', change_count: 3, base_revision: 1,
  purpose: '', state: 'open', invalidated_cause: '', min_approvals: 1, approvals: 0,
  key_ids: [consentIds.secretKey, consentIds.configKey, consentIds.missingKey], votes: [], created_at: at, expires_at: '2099-01-01T00:00:00Z',
  ...overrides,
});

function route(requests: object[], failures: ConsentLookupFailures = {}, catalogueGate?: Promise<void>) {
  vi.stubGlobal('fetch', async (input: Request) => {
    const path = new URL(input.url).pathname;
    if (catalogueGate !== undefined && path.endsWith('/keys')) await catalogueGate;
    if (path.endsWith('/environments')) {
      return Response.json({ count: 1, items: [{ id: consentIds.environment, org_id: consentIds.org, project_id: consentIds.project, name: 'production', display_order: 0, created_at: at }] });
    }
    if (path.endsWith('/approval-policies')) return Response.json({ items: [] });
    if (path.endsWith('/approval-requests')) return Response.json({ items: requests });
    const lookup = consentLookup(path, failures);
    if (lookup === undefined) throw new Error(`unexpected ${path}`);
    return lookup;
  });
}

async function openQueue() {
  const view = await renderForm(
    <MemoryRouter initialEntries={['/orgs/acme/projects/app/approvals']}>
      <Routes><Route path="/orgs/:org/projects/:project/approvals" element={<ChangeApprovals />} /></Routes>
    </MemoryRouter>,
  );
  await settleTask();
  const select = view.container.querySelector('#ca-review-env');
  if (!(select instanceof HTMLSelectElement)) throw new Error('environment select missing');
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set?.call(select, consentIds.environment);
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await settleTask();
  return view;
}
const click = async (container: HTMLElement, label: string) => {
  const found = [...container.querySelectorAll('button')].find((b) => b.textContent === label);
  if (found === undefined) throw new Error(`missing ${label}`);
  await act(async () => found.click());
};

beforeEach(() => guard.run.mockReset());
afterEach(() => vi.unstubAllGlobals());

describe('change request names', () => {
  it('holds the vote until the key catalogue settles, because the ceremony copies its list on open', async () => {
    let release = () => {};
    const gate = new Promise<void>((resolve) => { release = resolve; });
    route([request()], {}, gate);
    const { container, unmount } = await openQueue();
    try {
      const approve = () => [...container.querySelectorAll('button')].find((b) => b.textContent === 'Approve');
      expect(approve()?.disabled).toBe(true);
      await act(async () => { release(); });
      await settleTask();
      expect(approve()?.disabled).toBe(false);
    } finally {
      await unmount();
    }
  });

  it('heads the card with the requester, moves the request ID into a mono row and lists key names', async () => {
    route([request()]);
    const { container, unmount } = await openQueue();
    try {
      const card = container.querySelector('.change-approvals__request');
      expect(card?.querySelector('.change-approvals__request-head')?.textContent).toContain('Change request by Dana Jacobs');
      expect(card?.querySelector('.change-approvals__request-head')?.textContent).not.toContain('apr_');
      const idRow = [...(card?.querySelectorAll('dt') ?? [])].find((dt) => dt.textContent === 'Request ID')?.nextElementSibling;
      expect(idRow?.className).toBe('mono');
      expect(idRow?.textContent).toBe('apr_00000000-0000-4000-8000-0000000000a1');
      expect(card?.textContent).toContain('3 keys');
      const rows = [...(card?.querySelectorAll('.ceremony__keys li') ?? [])];
      expect(rows.map((row) => row.getAttribute('title'))).toEqual([consentIds.secretKey, consentIds.configKey, consentIds.missingKey]);
      expect(rows[0]?.textContent).toContain('DATABASE_URL');
      expect(rows[2]?.textContent).toBe(consentIds.missingKey);
    } finally { await unmount(); }
  });

  it('heads the card with the purpose when one is set', async () => {
    route([request({ purpose: 'Rotate the billing credentials' })]);
    const { container, unmount } = await openQueue();
    try {
      expect(container.querySelector('.change-approvals__request-head')?.textContent).toContain('Rotate the billing credentials');
    } finally { await unmount(); }
  });

  it('passes key names and classifications to the ceremony while binding every ID', async () => {
    route([request()]);
    const { container, unmount } = await openQueue();
    try {
      await click(container, 'Approve');
      expect(guard.run).toHaveBeenCalledOnce();
      expect(guard.run.mock.calls[0]?.[0]).toEqual([expect.objectContaining({
        environmentName: 'production',
        keys: [
          { id: consentIds.secretKey, name: 'DATABASE_URL', classification: 'secret' },
          { id: consentIds.configKey, name: 'LOG_LEVEL', classification: 'config' },
          { id: consentIds.missingKey, name: consentIds.missingKey },
        ],
      })]);
    } finally { await unmount(); }
  });

  it('falls back to the key ID as its name when the catalogue is unavailable', async () => {
    route([request()], { keys: true });
    const { container, unmount } = await openQueue();
    try {
      await click(container, 'Approve');
      expect(guard.run.mock.calls[0]?.[0]).toEqual([expect.objectContaining({
        keys: [consentIds.secretKey, consentIds.configKey, consentIds.missingKey].map((id) => ({ id, name: id })),
      })]);
    } finally { await unmount(); }
  });
});

// @vitest-environment happy-dom
import { client } from '@hikyo/runtime';
import { zReplaceRulesRequest, zRule } from '@hikyo/zod';
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { z } from 'zod';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { Members } from './Members.tsx';

const org = 'org_11111111-1111-4111-8111-111111111111';
const project = 'prj_22222222-2222-4222-8222-222222222222';
const member = 'prn_33333333-3333-4333-8333-333333333333';
const manager = 'prn_44444444-4444-4444-8444-444444444444';
const ruleIDs: readonly [string, string] = [
  'rul_55555555-5555-4555-8555-555555555555',
  'rul_66666666-6666-4666-8666-666666666666',
];
const mocks = vi.hoisted(() => ({ refresh: vi.fn() }));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: manager } }, refreshSession: mocks.refresh }),
}));
vi.mock('../api/settings.ts', async (original) => ({
  ...(await original<typeof import('../api/settings.ts')>()),
  useOrg: () => ({ data: { id: org, name: 'Acme' } }),
  useOrgTopology: () => ({ projects: [{ id: project, name: 'Application', environments: [] }], isError: false, isPending: false, ready: true }),
}));
vi.mock('../api/access.ts', async (original) => ({
  ...(await original<typeof import('../api/access.ts')>()),
  useOrgGrants: () => ({ data: { items: [{ id: 'grn_member', principal_id: member, principal_name: 'Dana', capability: 'read', scope: { org_id: org, project_id: project }, origins: [], created_at: '2026-10-01T12:00:00Z' }], count: 1 }, isSuccess: true, isError: false }),
  useInstanceGrants: () => ({ isSuccess: false }),
  useRevokeGrant: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock('../ui/useModalDialog.ts', () => ({ useModalDialog: () => ({ current: null }) }));

let rows: z.infer<typeof zRule>[] = [];
let createFailure: 'none' | 'lost' | 'malformed' | 'refused' = 'none';
let deleteFailure = false;
let createCount = 0;
let listingCount = 0;
let deleted: string[] = [];
let listingWait: Promise<void> | null = null;
let listingRefused = false;

const json = (data: object, status = 200) => new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });
const row = (id: string, capability: z.infer<typeof zRule>['capability']): z.infer<typeof zRule> => ({
  id, principal_id: member, principal_name: 'Dana', capability,
  where: { projects: [project], environments: { mode: 'all', items: [] }, keys: { mode: 'all', items: [] } },
  other_projects: false, created_by: manager, created_at: '2026-10-01T12:00:00Z',
});

beforeEach(() => {
  rows = [];
  createFailure = 'none';
  deleteFailure = false;
  createCount = listingCount = 0;
  deleted = [];
  listingWait = null;
  listingRefused = false;
  mocks.refresh.mockReset().mockResolvedValue(undefined);
  client.setConfig({ baseUrl: 'https://hikyo.test' });
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (input, init) => {
    const request = input instanceof Request ? input : new Request(input, init);
    const path = new URL(request.url).pathname;
    if (request.method === 'GET' && path.endsWith('/rules')) {
      listingCount += 1;
      if (listingCount > 1) {
        if (listingRefused) return json({ error: 'not_found' }, 404);
        if (listingWait !== null) await listingWait;
      }
      return json({ items: rows, count: rows.length });
    }
    if (request.method === 'GET' && path.endsWith('/keys')) return json({ items: [], count: 0, schema_revision: 0 });
    if (request.method === 'POST' && path.endsWith('/rules/replace')) {
      const body = zReplaceRulesRequest.parse(await request.json());
      const revoke = body.revoke ?? [];
      const create = body.create ?? [];
      if (body.principal !== member || revoke.some((id) => !rows.some((item) => item.id === id))) {
        return json({ error: 'not_found' }, 404);
      }
      if (deleteFailure && revoke.length > 0) {
        deleteFailure = false;
        return json({ error: 'unavailable' }, 503);
      }
      if (createFailure === 'refused') return json({ error: { code: 'invalid', message: 'Rule refused', detail: 'Adjust the selection.' } }, 400);
      const created = create.map((add) => {
        const id = ruleIDs[createCount++];
        if (id === undefined) throw new Error('Unexpected duplicate create.');
        return { ...row(id, add.capability), where: add.where };
      });
      deleted.push(...revoke);
      rows = [...rows.filter((item) => !revoke.includes(item.id)), ...created];
      // The complete transaction commits before its response is lost or invalid.
      if (createFailure === 'lost') throw new TypeError('Response lost after commit.');
      if (createFailure === 'malformed') return json({ items: [{ capability: 'read' }], count: 1 });
      return json({ items: created, count: created.length });
    }
    throw new Error(`Unexpected request: ${request.method} ${path}`);
  }));
});
afterEach(() => {
  client.setConfig({ baseUrl: '' });
  vi.unstubAllGlobals();
});

const renderMembers = () => renderForm(<MemoryRouter initialEntries={[`/orgs/${org}/members?project=${project}`]}>
  <Routes><Route path="/orgs/:org/members" element={<Members scope={{ kind: 'org' }} />} /></Routes>
</MemoryRouter>);

async function click(container: HTMLElement, label: string) {
  const button = [...container.querySelectorAll('button')].find((candidate) =>
    candidate.textContent?.trim().startsWith(label) || candidate.querySelector('.toggle-chip__label')?.textContent === label);
  if (button === undefined) throw new Error(`No ${label} button.`);
  expect(button.disabled).toBe(false);
  await act(async () => button.click());
  await settleTask();
}

const createFailures: readonly ('lost' | 'malformed')[] = ['lost', 'malformed'];
for (const failure of createFailures) {
  it(`closes an uncertain ${failure} create editor and shows the committed row from a fresh authoritative listing`, async () => {
    createFailure = failure;
    const view = await renderMembers();
    try {
      await settleTask();
      await click(view.container, '+ Add rule');
      await click(view.container, 'Application');
      const reveal = [...view.container.querySelectorAll<HTMLLabelElement>('dialog label')].find((label) => label.textContent === 'Reveal');
      if (reveal === undefined) throw new Error('No Reveal permission.');
      await act(async () => reveal.click());
      await click(view.container, 'Save');
      await settleTask();
      expect(createCount).toBe(2);
      expect(rows.map((item) => item.id)).toEqual(ruleIDs);
      expect(deleted).toEqual([]);
      expect(listingCount).toBeGreaterThan(1);
      expect(view.container.querySelector('dialog')).toBeNull();
      expect(view.container.textContent).not.toContain('Nothing changed');
      expect(view.container.textContent).toContain('could not be confirmed');
      expect(view.container.querySelector('#members-rules')?.textContent).toContain('Reveal');
      await click(view.container, 'Edit rule');
      expect(view.container.querySelector('dialog')?.textContent).toContain('Edit rule');
      // Reopening reads the committed permission, not the old two-create draft.
      await click(view.container, 'Save');
      expect(createCount).toBe(2);
    } finally { await view.unmount(); }
  });
}

it('closes a refused atomic removal and reopening retries the complete authoritative card', async () => {
  rows = [row(ruleIDs[0], 'read'), row(ruleIDs[1], 'reveal')];
  deleteFailure = true;
  const view = await renderMembers();
  try {
    await settleTask();
    await click(view.container, 'Edit rule');
    await click(view.container, 'Remove rule');
    expect(rows.map((item) => item.id)).toEqual(ruleIDs);
    expect(deleted).toEqual([]);
    expect(view.container.querySelector('dialog')).toBeNull();
    expect(view.container.textContent).toContain('atomic change could not be confirmed');
    expect(listingCount).toBeGreaterThan(1);
    await click(view.container, 'Edit rule');
    await click(view.container, 'Remove rule');
    expect(deleted).toEqual(ruleIDs);
    expect(rows).toEqual([]);
    expect(view.container.querySelector('dialog')).toBeNull();
  } finally { await view.unmount(); }
});

const successfulChanges: readonly ('save' | 'remove')[] = ['save', 'remove'];
for (const change of successfulChanges) {
  it(`confirms ${change} after the session owner invalidates the same rule listing twice`, async () => {
    rows = change === 'remove' ? [row(ruleIDs[0], 'read'), row(ruleIDs[1], 'reveal')] : [];
    const view = await renderMembers();
    try {
      await settleTask();
      // AuthProvider invalidates at refresh start and again after whoami.
      // Hold network replies across those two invalidations, as a real network
      // does. A third concurrently awaited refetch can retain a canceled retryer
      // instead of the owner's latest successful answer.
      let releaseListing = () => {};
      listingWait = new Promise<void>((resolve) => { releaseListing = resolve; });
      mocks.refresh.mockImplementation(async () => {
        void view.client.invalidateQueries();
        await new Promise<void>((resolve) => setTimeout(resolve, 0));
        void view.client.invalidateQueries();
        releaseListing();
      });
      if (change === 'remove') {
        await click(view.container, 'Edit rule');
        await click(view.container, 'Remove rule');
        expect(rows).toEqual([]);
        expect(deleted).toEqual(ruleIDs);
      } else {
        await click(view.container, '+ Add rule');
        await click(view.container, 'Application');
        await click(view.container, 'Save');
        expect(rows).toHaveLength(1);
        expect(deleted).toEqual([]);
      }
      expect(view.container.querySelector('dialog')).toBeNull();
      expect(view.container.textContent).toContain(change === 'remove' ? 'Removed the rule from Dana.' : 'Added a rule for Dana.');
      expect(view.container.textContent).not.toContain('current rules or session could not be confirmed');
    } finally { await view.unmount(); }
  });
}

it('closes a committed-create editor when the post-write session refresh cannot be confirmed', async () => {
  mocks.refresh.mockRejectedValue(new Error('Session refresh refused.'));
  const view = await renderMembers();
  try {
    await settleTask();
    await click(view.container, '+ Add rule');
    await click(view.container, 'Application');
    await click(view.container, 'Save');
    expect(createCount).toBe(1);
    expect(rows).toHaveLength(1);
    expect(view.container.querySelector('dialog')).toBeNull();
    expect(view.container.textContent).toContain('current rules or session could not be confirmed');
    expect(view.container.textContent).not.toContain('Nothing changed');
    expect(view.container.textContent).not.toContain('Added a rule for');
    expect(listingCount).toBeGreaterThan(1);
    expect([...view.container.querySelectorAll('#members-rules [data-rule-action]')].map((button) => button.textContent)).toContain('Edit rule 1 of Dana');
  } finally { await view.unmount(); }
});

it('does not expose stale edit actions while the authoritative listing is still refreshing', async () => {
  rows = [row(ruleIDs[0], 'read'), row(ruleIDs[1], 'reveal')];
  deleteFailure = true;
  const view = await renderMembers();
  let release = () => {};
  try {
    await settleTask();
    listingWait = new Promise<void>((resolve) => { release = resolve; });
    await click(view.container, 'Edit rule');
    await click(view.container, 'Remove rule');
    expect([...view.container.querySelectorAll('#members-rules [data-rule-action]')]).toEqual([]);
    expect([...view.container.querySelectorAll<HTMLButtonElement>('dialog .dialog__actions button')].every((button) => button.disabled)).toBe(true);
    await act(async () => release());
    await settleTask();
    expect(view.container.querySelector('dialog')).toBeNull();
    expect(rows.map((item) => item.id)).toEqual(ruleIDs);
    await click(view.container, 'Edit rule');
  } finally {
    release();
    await view.unmount();
  }
});

it('keeps rule actions unavailable when the authoritative post-write listing is masked', async () => {
  const view = await renderMembers();
  try {
    await settleTask();
    await click(view.container, '+ Add rule');
    await click(view.container, 'Application');
    listingRefused = true;
    await click(view.container, 'Save');
    expect(createCount).toBe(1);
    expect(view.container.querySelector('dialog')).toBeNull();
    expect([...view.container.querySelectorAll('#members-rules [data-rule-action]')]).toEqual([]);
    expect(view.container.textContent).toContain('current rules or session could not be confirmed');
    expect(view.container.textContent).not.toContain('Added a rule for');
  } finally { await view.unmount(); }
});

it('retains an edited draft after a definite atomic refusal and retries it unchanged', async () => {
  rows = [row(ruleIDs[0], 'read')];
  createCount = 1;
  createFailure = 'refused';
  const view = await renderMembers();
  try {
    await settleTask();
    await click(view.container, 'Edit rule');
    const reveal = [...view.container.querySelectorAll<HTMLLabelElement>('dialog label')].find((label) => label.textContent === 'Reveal');
    if (reveal === undefined) throw new Error('No Reveal permission.');
    await act(async () => reveal.click());
    await click(view.container, 'Save');
    expect(view.container.querySelector('dialog')).not.toBeNull();
    expect(view.container.querySelector('dialog')?.textContent).not.toContain('could not be confirmed');
    expect(rows.map((item) => item.capability)).toEqual(['read']);
    expect(deleted).toEqual([]);
    createFailure = 'none';
    await click(view.container, 'Save');
    expect(view.container.querySelector('dialog')).toBeNull();
    expect(rows.map((item) => item.capability)).toEqual(['read', 'reveal']);
  } finally { await view.unmount(); }
});

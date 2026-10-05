// @vitest-environment happy-dom
import { client } from '@hikyo/runtime';
import { zReplaceRulesRequest } from '@hikyo/zod';
import type { z } from 'zod';
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { Members } from './Members.tsx';

const org = 'org_11111111-1111-4111-8111-111111111111';
const project = 'prj_22222222-2222-4222-8222-222222222222';
const member = 'prn_33333333-3333-4333-8333-333333333333';
const manager = 'prn_44444444-4444-4444-8444-444444444444';
const ruleID = 'rul_55555555-5555-4555-8555-555555555555';
const environment = 'env_66666666-6666-4666-8666-666666666666';
const mocks = vi.hoisted(() => ({ refresh: vi.fn(), metadataRefused: false }));
const keyID = 'key_77777777-7777-4777-8777-777777777777';

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: manager } }, refreshSession: mocks.refresh }),
}));
vi.mock('../api/settings.ts', async (original) => ({
  ...(await original<typeof import('../api/settings.ts')>()),
  useOrg: () => mocks.metadataRefused ? { data: undefined, isError: true } : { data: { id: org, name: 'Acme' } },
  useOrgTopology: () => mocks.metadataRefused ? { projects: [], isError: true, isPending: false, ready: false } : { projects: [{ id: project, name: 'Application', environments: [{ id: environment, name: 'Development', isProtected: false }] }], isError: false, isPending: false, ready: true },
}));
vi.mock('../ui/useModalDialog.ts', () => ({ useModalDialog: () => ({ current: null }) }));

let ruleListingRefused = false;
let savedReplacement: z.infer<typeof zReplaceRulesRequest> | null = null;
const json = (data: object, status = 200) => new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  ruleListingRefused = false;
  savedReplacement = null;
  mocks.metadataRefused = false;
  mocks.refresh.mockReset().mockResolvedValue(undefined);
  client.setConfig({ baseUrl: 'https://hikyo.test' });
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (input, init) => {
    const request = input instanceof Request ? input : new Request(input, init);
    const path = new URL(request.url).pathname;
    if (request.method === 'GET' && path.endsWith('/grants')) return json({ error: 'not_found' }, 404);
    if (request.method === 'GET' && path.endsWith('/rules')) {
      expect(path).toContain(`/projects/${project}/rules`);
      if (ruleListingRefused) return json({ error: 'not_found' }, 404);
      return json({ items: [{
        id: ruleID, principal_id: member, principal_name: 'Dana', capability: 'edit',
        where: { projects: [project], environments: mocks.metadataRefused ? { mode: 'only', items: [{ project, environment }] } : { mode: 'all', items: [] }, keys: { mode: 'only', items: mocks.metadataRefused ? [{ project, key: keyID }] : [{ project, folder: 'db' }] } },
        other_projects: false, created_by: manager, created_at: '2026-10-01T12:00:00Z',
      }], count: 1 });
    }
    if (request.method === 'GET' && path.endsWith('/keys')) return mocks.metadataRefused ? json({ error: 'not_found' }, 404) : json({ items: [], count: 0, schema_revision: 0 });
    if (request.method === 'POST' && path.endsWith('/rules/replace')) {
      savedReplacement = zReplaceRulesRequest.parse(await request.json());
      return json({ items: [], count: 0 });
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

it('allows a narrowed manager to edit disclosed project rules without offering scope-wide controls or a complete access census', async () => {
  const view = await renderMembers();
  try {
    await settleTask();
    await settleTask();
    expect(view.container.querySelector('#members-rules')?.textContent).toContain('Dana');
    expect(view.container.querySelector('#members-rules')?.textContent).toContain('only db/');
    const edit = view.container.querySelector<HTMLButtonElement>('[data-rule-action]');
    expect(edit).not.toBeNull();
    expect(edit?.disabled).toBe(false);
    expect(view.container.textContent).not.toContain('You hold no manage-members');
    const buttons = [...view.container.querySelectorAll('button')].map((button) => button.textContent?.trim());
    expect(buttons).not.toContain('Add access');
    expect(buttons).not.toContain('Invite');
    expect(buttons.some((text) => text?.includes('Add scope-wide access') || text?.includes('Reset credential'))).toBe(false);
    expect(view.container.querySelector('#members-whocan')?.textContent).toContain('cannot answer who has access across the project');
    expect(view.container.querySelector('#members-whocan select')).toBeNull();
    await act(async () => edit?.click());
    expect(view.container.querySelector('dialog')?.textContent).toContain('Edit rule · Dana');
  } finally { await view.unmount(); }
});

it('blanks stale project rules and closes editing after a refused authoritative refetch', async () => {
  const view = await renderMembers();
  try {
    await settleTask();
    await settleTask();
    const edit = view.container.querySelector<HTMLButtonElement>('[data-rule-action]');
    expect(edit?.disabled).toBe(false);
    await act(async () => edit?.click());
    ruleListingRefused = true;
    await act(async () => { await view.client.invalidateQueries({ queryKey: ['rules', org] }); });
    await settleTask();
    expect(view.container.querySelector('#members-rules')?.textContent).not.toContain('Dana');
    expect(view.container.querySelector('dialog')).toBeNull();
    expect(view.container.querySelector('[data-rule-action]')).toBeNull();
  } finally { await view.unmount(); }
});

it('edits a disclosed rule using stable selector IDs when See and directory metadata are unavailable', async () => {
  mocks.metadataRefused = true;
  const view = await renderMembers();
  try {
    await settleTask();
    await settleTask();
    expect(view.container.querySelector('#members-rules')?.textContent).toContain(keyID);
    expect(view.container.querySelector('#members-rules')?.textContent).toContain(environment);
    expect(view.container.textContent).not.toContain('Application');
    expect(view.container.textContent).not.toContain('Development');
    expect(view.container.textContent).toContain('Names and protection settings are unavailable without See');
    expect(view.container.querySelector('#members-rules .access-rule__reach')).toBeNull();
    const edit = view.container.querySelector<HTMLButtonElement>('[data-rule-action]');
    expect(edit?.disabled).toBe(false);
    await act(async () => edit?.click());
    const dialog = view.container.querySelector('dialog');
    expect(dialog?.textContent).toContain(project);
    expect(dialog?.textContent).toContain(environment);
    expect(dialog?.textContent).toContain(keyID);
    const publish = [...(dialog?.querySelectorAll<HTMLLabelElement>('label') ?? [])].find((label) => label.textContent === 'Publish');
    if (publish === undefined) throw new Error('No Publish checkbox.');
    await act(async () => publish.click());
    const save = [...(dialog?.querySelectorAll('button') ?? [])].find((button) => button.textContent === 'Save');
    expect(save?.disabled).toBe(false);
    await act(async () => save?.click());
    await settleTask();
    expect(savedReplacement).toEqual({ principal: member, revoke: [], create: [{
      principal: member, capability: 'publish', where: {
        projects: [project], environments: { mode: 'only', items: [{ project, environment }] },
        keys: { mode: 'only', items: [{ project, key: keyID }] },
      },
    }] });
    expect(view.container.querySelector('dialog')).toBeNull();
    expect(mocks.refresh).toHaveBeenCalledOnce();
  } finally { await view.unmount(); }
});

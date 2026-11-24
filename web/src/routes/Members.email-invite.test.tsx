// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { Members } from './Members.tsx';

const idle = { data: undefined, error: null, isError: false, isPending: false, isSuccess: false, refetch: vi.fn() };

vi.mock('../api/access.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/access.ts')>()),
  useInstanceGrants: () => idle,
  useOrgGrants: () => ({ ...idle, isSuccess: true, data: { items: [], count: 0 } }),
  useRevokeGrant: () => ({ isPending: false, variables: undefined, mutate: vi.fn() }),
  useCreateGrants: () => ({ isPending: false, mutate: vi.fn() }),
  useApplyTemplate: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock('../api/settings.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/settings.ts')>()),
  useOrg: () => ({ data: undefined, isError: false }),
  useOrgTopology: () => ({ projects: [], isError: false, isPending: false, ready: false }),
  useInstanceOrgs: () => ({ data: { items: [], count: 0 }, isError: false, isPending: false, isSuccess: true }),
}));
vi.mock('../app/AuthProvider.tsx', () => ({ useInstanceOperator: () => false,
  useAuth: () => ({ identity: { principal: { id: 'prn_22222222-2222-4222-8222-222222222222' } } }) }));
vi.mock('../ui/useModalDialog.ts', async (importActual) => ({
  ...(await importActual<typeof import('../ui/useModalDialog.ts')>()),
  useModalDialog: () => ({ current: null }),
}));

function OtherOrg() {
  const navigate = useNavigate();
  return <button type="button" onClick={() => void navigate('/orgs/org_beta/members')}>Go to beta</button>;
}

describe('Members email invitation dialog', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('drops the draft when the page moves to another organisation', async () => {
    // Every list the page reads that is not mocked above answers empty.
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response(JSON.stringify({ items: [], count: 0 }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    })));
    const view = await renderForm(
      <MemoryRouter initialEntries={['/orgs/org_acme/members']}>
        <OtherOrg />
        <Routes>
          <Route path="/orgs/:org/members" element={<Members scope={{ kind: 'org' }} />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      const byLabel = (label: string) => [...view.container.querySelectorAll('button')].find((b) => b.textContent === label);
      await act(async () => byLabel('Invite by email')?.click());
      const email = () => view.container.querySelector<HTMLInputElement>('input[name="email"]');
      const field = email();
      expect(field).not.toBeNull();
      if (field === null) return;
      await act(async () => typeInto(field, 'dana@example.com'));
      expect(email()?.value).toBe('dana@example.com');
      await act(async () => byLabel('Go to beta')?.click());
      expect(email()?.value ?? '').toBe('');
    } finally { await view.unmount(); }
  });

  it('names the signed-in administrator "you" in the pending list', async () => {
    const invitation = {
      id: 'eiv_33333333-3333-4333-8333-333333333333', email: 'dana@example.com', template: 'editor',
      invited_by: 'prn_22222222-2222-4222-8222-222222222222',
      created_at: '2026-11-06T10:00:00Z', expires_at: '2026-11-13T10:00:00Z',
    };
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (input) => {
      const url = input instanceof Request ? input.url : String(input);
      const items = url.includes('/email-invitations') ? [invitation] : [];
      return new Response(JSON.stringify({ items, count: items.length }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }));
    const view = await renderForm(
      <MemoryRouter initialEntries={['/orgs/org_acme/members']}>
        <Routes>
          <Route path="/orgs/:org/members" element={<Members scope={{ kind: 'org' }} />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      await settleTask();
      const row = [...view.container.querySelectorAll('tr')].find((candidate) => candidate.textContent?.includes('dana@example.com'));
      expect(row?.textContent).toContain('you');
      expect(row?.textContent).not.toContain('prn_');
    } finally { await view.unmount(); }
  });
});

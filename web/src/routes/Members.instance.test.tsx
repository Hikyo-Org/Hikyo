// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import type { Grant } from '../api/identities.ts';
import { renderForm } from '../testkit/renderForm.tsx';
import { Members } from './Members.tsx';

type GrantsQuery = {
  data: { items: Grant[]; count: number } | undefined;
  error: unknown;
  isError: boolean;
  isPending: boolean;
  isSuccess: boolean;
  refetch: ReturnType<typeof vi.fn>;
};

type Mocks = { instanceGrants: GrantsQuery; orgGrants: GrantsQuery; orgGrantsCalls: string[] };

const idle = (): GrantsQuery => ({
  data: undefined,
  error: null,
  isError: false,
  isPending: false,
  isSuccess: false,
  refetch: vi.fn(),
});

const mocks = vi.hoisted(
  (): Mocks => ({
    instanceGrants: {
      data: undefined,
      error: null,
      isError: false,
      isPending: true,
      isSuccess: false,
      refetch: vi.fn(),
    },
    orgGrants: {
      data: undefined,
      error: null,
      isError: false,
      isPending: false,
      isSuccess: false,
      refetch: vi.fn(),
    },
    orgGrantsCalls: [],
  }),
);

vi.mock('../api/access.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/access.ts')>();
  return {
    ...actual,
    useInstanceGrants: () => mocks.instanceGrants,
    useOrgGrants: (org: string) => {
      mocks.orgGrantsCalls.push(org);
      return mocks.orgGrants;
    },
    useRevokeGrant: () => ({ isPending: false, variables: undefined, mutate: vi.fn() }),
    useCreateGrants: () => ({ isPending: false, mutate: vi.fn() }),
    useApplyTemplate: () => ({ isPending: false, mutate: vi.fn() }),
  };
});

vi.mock('../api/settings.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/settings.ts')>();
  return {
    ...actual,
    useOrg: () => ({ data: undefined, isError: false }),
    useOrgTopology: () => ({ projects: [], isError: false, isPending: false, ready: false }),
    useInstanceOrgs: () => ({
      data: { items: [{ id: 'org_acme', name: 'Acme', active: true, created_at: '2026-08-24T08:00:00Z' }], count: 1 },
      isError: false,
      isPending: false,
      isSuccess: true,
    }),
  };
});

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: 'usr_current' } } }),
}));

vi.mock('./useModalDialog.ts', async (importActual) => {
  const actual = await importActual<typeof import('./useModalDialog.ts')>();
  return { ...actual, useModalDialog: () => ({ current: null }) };
});

const instanceGrant: Grant = {
  id: 'grn_1',
  principal_id: 'prn_1',
  capability: 'instance-config',
  scope: {},
  origins: [{ kind: 'break-glass', subject: 'host' }],
  created_at: '2026-08-24T08:00:00Z',
};

async function renderInstanceMembers() {
  return renderForm(
    <MemoryRouter initialEntries={['/instance/members']}>
      <Routes>
        <Route path="/instance/members" element={<Members scope={{ kind: 'instance' }} />} />
      </Routes>
    </MemoryRouter>,
  );
}

function text(container: HTMLElement, selector: string): string {
  return container.querySelector(selector)?.textContent ?? '';
}

describe('Members at instance scope', () => {
  beforeEach(() => {
    mocks.orgGrantsCalls.length = 0;
    mocks.orgGrants = idle();
    mocks.instanceGrants = {
      data: { items: [instanceGrant], count: 1 },
      error: null,
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
  });

  it('lists instance grants under an instance heading and offers only the instance scope', async () => {
    const view = await renderInstanceMembers();
    expect(text(view.container, 'h1')).toBe('Members · Instance');
    expect(view.container.querySelector('table')?.textContent).toContain('instance-config');
    expect(view.container.querySelector('table')?.textContent).toContain('break-glass: host');
    expect(text(view.container, '#members-list')).toContain('inherit downward into every organisation');
    // The org read is disabled: no organisation is addressed.
    expect(mocks.orgGrantsCalls).toEqual(['']);

    const open = [...view.container.querySelectorAll('button')].find((b) => b.textContent === 'New grant');
    if (open === undefined) throw new Error('no New grant button');
    expect(open.disabled).toBe(false);
    await act(async () => open.click());
    const scope = [...view.container.querySelectorAll('select')].find(
      (s) => view.container.querySelector(`label[for="${s.id}"]`)?.textContent === 'Scope',
    );
    if (scope === undefined) throw new Error('no Scope select');
    expect([...scope.options].map((o) => o.textContent)).toEqual([
      'Choose a scope…',
      'This instance (every organisation)',
    ]);
    expect(view.container.textContent).toContain('Instance scope reaches every organisation');
    await view.unmount();
  });

  it('renders a second-factor refusal as its own state', async () => {
    mocks.instanceGrants = {
      data: undefined,
      error: new ApiError(403, 'forbidden'),
      isError: true,
      isPending: false,
      isSuccess: false,
      refetch: vi.fn(),
    };
    const view = await renderInstanceMembers();
    expect(text(view.container, '[role="alert"]')).toContain('Instance grants require a second factor');
    await view.unmount();
  });
});

describe('Members at organisation scope without manage-members', () => {
  beforeEach(() => {
    mocks.instanceGrants = idle();
  });

  const staleRows = {
    items: [
      {
        id: 'grn_stale',
        principal_id: 'usr_stale',
        capability: 'read',
        scope: { org_id: 'org_acme' },
        origins: [{ kind: 'manual', subject: 'usr_admin' }],
        created_at: '2026-08-24T08:00:00Z',
      },
    ],
    count: 1,
  };

  const renderOrg = () =>
    renderForm(
      <MemoryRouter initialEntries={['/orgs/org_acme/members']}>
        <Routes>
          <Route path="/orgs/:org/members" element={<Members scope={{ kind: 'org' }} />} />
        </Routes>
      </MemoryRouter>,
    );

  it('treats a 403 as the second-factor refusal it is, keeping the recovery path', async () => {
    mocks.orgGrants = { ...idle(), data: staleRows, error: new ApiError(403, 'refused'), isError: true };
    const view = await renderOrg();
    expect(text(view.container, '[role="alert"]')).toContain('Managing members requires a second factor');
    // A refused refetch must not keep showing the last page or its controls.
    expect(view.container.textContent).not.toContain('usr_stale');
    const buttons = [...view.container.querySelectorAll('button')].map((b) => b.textContent);
    expect(buttons).not.toContain('New grant');
    expect(buttons).not.toContain('Invite');
    expect(buttons.some((b) => b?.startsWith('Reset credential'))).toBe(false);
    expect(buttons.some((b) => b?.startsWith('Revoke'))).toBe(false);
    await view.unmount();
  });

  it('hides the grant, invite and reset actions on a 404 listing refusal and says why', async () => {
    mocks.orgGrants = { ...idle(), data: staleRows, error: new ApiError(404, 'refused'), isError: true };
    const view = await renderOrg();
    expect(view.container.querySelector('[role="alert"]')).toBeNull();
    const statuses = [...view.container.querySelectorAll('[role="status"]')].map((s) => s.textContent);
    expect(statuses).toContain(
      'You hold no manage-members here: this list shows only what you are allowed to see.',
    );
    expect(view.container.textContent).not.toContain('usr_stale');
    const buttons = [...view.container.querySelectorAll('button')].map((b) => b.textContent);
    expect(buttons).not.toContain('New grant');
    expect(buttons).not.toContain('Invite');
    expect(buttons.some((b) => b?.startsWith('Reset credential'))).toBe(false);
    await view.unmount();
  });
});

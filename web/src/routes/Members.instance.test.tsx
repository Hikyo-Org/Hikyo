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

type Mocks = { instanceGrants: GrantsQuery; orgGrantsCalls: string[] };

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
      return { data: undefined, error: null, isError: false, isPending: false, isSuccess: false, refetch: vi.fn() };
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

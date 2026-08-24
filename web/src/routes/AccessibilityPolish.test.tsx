// @vitest-environment happy-dom
import { act, type ReactNode } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Grant } from '../api/identities.ts';
import type { RetentionPolicy } from '../api/settings.ts';
import { renderForm } from '../testkit/renderForm.tsx';
import { Members } from './Members.tsx';
import { OrgSettings } from './OrgSettings.tsx';

type RouteMocks = {
  grants: {
    data: { items: Grant[]; count: number } | undefined;
    error: null;
    isError: boolean;
    isPending: boolean;
    isSuccess: boolean;
    refetch: ReturnType<typeof vi.fn>;
  };
  org: {
    data: { id: string; name: string; active: boolean; created_at: string };
    isError: boolean;
  };
  projects: {
    data: { items: never[]; count: number };
    isError: boolean;
    isPending: boolean;
    isSuccess: boolean;
  };
  retention: {
    data: RetentionPolicy | undefined;
    isError: boolean;
    isPending: boolean;
  };
  topology: {
    projects: never[];
    isError: boolean;
    isPending: boolean;
    ready: boolean;
  };
};

const mocks = vi.hoisted<RouteMocks>(() => ({
  grants: {
    data: undefined,
    error: null,
    isError: false,
    isPending: true,
    isSuccess: false,
    refetch: vi.fn(),
  },
  org: {
    data: {
      id: 'org_acme',
      name: 'Acme',
      active: true,
      created_at: '2026-08-24T08:00:00Z',
    },
    isError: false,
  },
  projects: {
    data: { items: [], count: 0 },
    isError: false,
    isPending: false,
    isSuccess: true,
  },
  retention: {
    data: undefined,
    isError: false,
    isPending: true,
  },
  topology: {
    projects: [],
    isError: false,
    isPending: false,
    ready: true,
  },
}));

vi.mock('../api/access.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/access.ts')>();
  const { useState } = await import('react');
  return {
    ...actual,
    useOrgGrants: () => mocks.grants,
    useRevokeGrant: () => {
      const [variables, setVariables] = useState<{ grant: Grant }>();
      const [isPending, setIsPending] = useState(false);
      return {
        isPending,
        variables,
        mutate: (input: { grant: Grant }) => {
          setVariables(input);
          setIsPending(true);
        },
      };
    },
  };
});

vi.mock('../api/settings.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/settings.ts')>();
  return {
    ...actual,
    useDeleteOrg: () => ({ isPending: false, mutate: vi.fn() }),
    useOrg: () => mocks.org,
    useOrgRetention: () => mocks.retention,
    useOrgTopology: () => mocks.topology,
    useProjectRetentions: () => new Map(),
    useProjects: () => mocks.projects,
    useRenameOrg: () => ({ isPending: false, mutate: vi.fn() }),
    useSetOrgRetention: () => ({ isPending: false, mutate: vi.fn() }),
  };
});

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ identity: { principal: { id: 'usr_current' } } }),
}));

const grants: readonly Grant[] = [
  {
    id: 'grn_edit',
    principal_id: 'usr_alice',
    capability: 'edit',
    scope: { org_id: 'org_acme' },
    origins: [{ kind: 'manual', subject: 'usr_admin' }],
    created_at: '2026-08-24T08:00:00Z',
  },
  {
    id: 'grn_reveal',
    principal_id: 'usr_alice',
    capability: 'reveal',
    scope: { org_id: 'org_acme' },
    origins: [{ kind: 'manual', subject: 'usr_admin' }],
    created_at: '2026-08-24T08:00:00Z',
  },
];

beforeEach(() => {
  mocks.grants = {
    data: undefined,
    error: null,
    isError: false,
    isPending: true,
    isSuccess: false,
    refetch: vi.fn(),
  };
  mocks.retention = {
    data: undefined,
    isError: false,
    isPending: true,
  };
});

function atRoute(path: string, pattern: string, node: ReactNode) {
  return (
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path={pattern} element={node} />
      </Routes>
    </MemoryRouter>
  );
}

async function renderMembers() {
  return renderForm(atRoute('/orgs/org_acme/members', '/orgs/:org/members', <Members />));
}

async function renderOrgSettings() {
  return renderForm(
    atRoute('/orgs/org_acme/settings', '/orgs/:org/settings', <OrgSettings />),
  );
}

describe('Members accessibility polish', () => {
  it('announces the initial list load only while grants are pending', async () => {
    const pending = await renderMembers();
    expect(pending.container.querySelector('#members-list [role="status"]')?.textContent).toBe(
      'Loading members…',
    );
    await pending.unmount();

    mocks.grants = {
      data: { items: [...grants], count: grants.length },
      error: null,
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
    const loaded = await renderMembers();

    expect(loaded.container.querySelector('#members-list [role="status"]')).toBeNull();
    expect(loaded.container.querySelector('#members-list table')).not.toBeNull();
    await loaded.unmount();
  });

  it('attributes an in-flight revoke to only the acting row', async () => {
    mocks.grants = {
      data: { items: [...grants], count: grants.length },
      error: null,
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
    const view = await renderMembers();
    const buttons = [...view.container.querySelectorAll<HTMLButtonElement>('#members-list button')]
      .filter((button) => button.textContent === 'Revoke');
    const acting = buttons[0];
    const sibling = buttons[1];
    if (acting === undefined || sibling === undefined) {
      throw new Error('expected two revoke buttons');
    }

    await act(async () => acting.click());

    expect(acting.textContent).toBe('Revoking…');
    expect(acting.disabled).toBe(true);
    expect(acting.getAttribute('aria-busy')).toBe('true');
    expect(sibling.textContent).toBe('Revoke');
    expect(sibling.disabled).toBe(false);
    expect(sibling.hasAttribute('aria-busy')).toBe(false);
    await view.unmount();
  });
});

describe('Organisation settings accessibility polish', () => {
  it('announces the retention read only while it is pending', async () => {
    const pending = await renderOrgSettings();
    expect(pending.container.querySelector('#org-retention [role="status"]')?.textContent).toBe(
      'Loading the retention policy…',
    );
    await pending.unmount();

    mocks.retention = {
      data: { mode: 'unlimited', max_age_seconds: null, last_revisions: null },
      isError: false,
      isPending: false,
    };
    const loaded = await renderOrgSettings();

    expect(
      [...loaded.container.querySelectorAll('#org-retention [role="status"]')].some(
        (status) => status.textContent === 'Loading the retention policy…',
      ),
    ).toBe(false);
    expect(
      [...loaded.container.querySelectorAll('#org-retention button')].some(
        (button) => button.textContent === 'Save retention',
      ),
    ).toBe(true);
    await loaded.unmount();
  });

  it('associates the organisation Name input with its explanatory hint', async () => {
    const view = await renderOrgSettings();
    const input = view.container.querySelector<HTMLInputElement>('#org-identity input');
    if (input === null) {
      throw new Error('organisation Name input is missing');
    }
    const hintId = input.getAttribute('aria-describedby');
    const hint = hintId === null ? null : view.container.ownerDocument.getElementById(hintId);

    expect(hintId).not.toBeNull();
    expect(hint === null ? false : view.container.contains(hint)).toBe(true);
    expect(hint?.textContent).toContain('Renaming an organisation is instance-operator work');
    await view.unmount();
  });
});

// @vitest-environment happy-dom
import { createClient } from '@hikyo/runtime-core';
import type { WorkspaceContextValue } from '../api/transport.tsx';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { Matrix } from './Matrix.tsx';

const mocks = vi.hoisted<{
  source: 'db' | 'git';
  workspace: WorkspaceContextValue | null;
  emptyEnvironments: boolean;
  canDeclare: boolean;
  canEdit: boolean;
  groupId: string;
  folder: string;
}>(() => ({
  source: 'db',
  workspace: null,
  emptyEnvironments: false,
  canDeclare: true,
  canEdit: true,
  groupId: '',
  folder: 'app',
}));

vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({ count }: { readonly count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({ index, start: index, end: index + 1 })),
    getTotalSize: () => count,
    measureElement: vi.fn(),
    scrollToIndex: vi.fn(),
  }),
}));

vi.mock('../api/transport.tsx', async (importActual) => {
  const actual = await importActual<typeof import('../api/transport.tsx')>();
  return { ...actual, useWorkspaceContext: () => mocks.workspace };
});

vi.mock('../api/definitions.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/definitions.ts')>();
  return {
    ...actual,
    useDefinitionsSettings: () => ({
      data: { definitions_source: mocks.source, can_declare_keys: mocks.canDeclare, can_edit_definitions: mocks.canEdit },
    }),
  };
});

vi.mock('../api/matrix.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/matrix.ts')>();
  const environment = {
    id: 'env_a',
    org_id: 'org_a',
    project_id: 'project_a',
    name: 'development',
    display_order: 0,
    created_at: '2026-08-24T08:00:00Z',
  };
  return {
    ...actual,
    useMatrixProject: () => ({
      environments: { data: { items: mocks.emptyEnvironments ? [] : [environment], count: mocks.emptyEnvironments ? 0 : 1 }, isPending: false, isError: false },
      keys: {
        data: {
          items: [{
            id: 'key_a',
            org_id: 'org_a',
            project_id: 'project_a',
            name: 'LOG_LEVEL',
            folder_path: mocks.folder,
            classification: 'config',
            description: '',
            deprecated: false,
            deprecation_note: '',
            declaration: { rule: { type: 'string', allow_empty: true } },
            presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
            group_id: mocks.groupId,
            created_at: '2026-08-24T08:00:00Z',
          }],
          count: 1,
        },
        isPending: false,
        isError: false,
      },
      groups: { data: { items: [{ id: 'linked_a', name: 'Database credentials' }], count: 1 }, isPending: false, isError: false },
      environmentRows: mocks.emptyEnvironments ? [] : [{
        environmentId: 'env_a',
        environment,
        readiness: 'ready',
        values: {
          status: 'ready',
          data: {
            items: [{ key_id: 'key_a', name: 'LOG_LEVEL', classification: 'config', set: true, revealed: true, value: 'info' }],
            count: 1,
          },
        },
        signals: { status: 'ready', data: { environment_id: 'env_a', revision: 1n, cells: [] } },
        settings: { status: 'ready', data: { protected: false } },
        pendingDrafts: { status: 'ready', data: { items: [], count: 0 } },
      }],
    }),
    useStageMatrixValue: () => ({ mutateAsync: vi.fn(), isPending: false }),
    useClearMatrixValue: () => ({ mutateAsync: vi.fn(), isPending: false }),
    usePublishMatrix: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
    useCopyMatrixConfig: () => ({ mutate: vi.fn(), isPending: false }),
    useReclassifyKey: () => ({ mutateAsync: vi.fn() }),
  };
});

vi.mock('./useProtectedPublishCeremony.ts', () => ({
  useProtectedPublishCeremony: () => ({
    request: null,
    error: null,
    run: vi.fn(),
    onAuthorised: vi.fn(),
    onCancel: vi.fn(),
  }),
}));

afterEach(() => {
  mocks.source = 'db';
  mocks.workspace = null;
  mocks.emptyEnvironments = false;
  mocks.canDeclare = true;
  mocks.canEdit = true;
  mocks.groupId = '';
  mocks.folder = 'app';
});

function render() {
  return renderForm(
    <MemoryRouter initialEntries={['/orgs/org_a/projects/project_a/matrix']}>
      <Routes>
        <Route path="/orgs/:org/projects/:project/matrix" element={<Matrix />} />
      </Routes>
    </MemoryRouter>,
  );
}

function hasButton(container: HTMLElement, text: string): boolean {
  return [...container.querySelectorAll('button')].some((button) => button.textContent === text);
}

describe('Matrix declaration availability by definitions source', () => {
  it('offers the declare actions when definitions live in the database', async () => {
    mocks.source = 'db';
    const view = await render();
    expect(hasButton(view.container, '+ New key')).toBe(true);
    expect(hasButton(view.container, '+ Key')).toBe(true);
    expect(view.container.textContent ?? '').not.toContain('managed in Git');
    await view.unmount();
  });

  it('offers Cleanup only when a key sits at the root and definitions live in the database', async () => {
    mocks.source = 'db';
    const foldered = await render();
    expect(hasButton(foldered.container, 'Cleanup')).toBe(false);
    await foldered.unmount();

    mocks.folder = '';
    const root = await render();
    expect(hasButton(root.container, 'Cleanup')).toBe(true);
    await root.unmount();

    mocks.source = 'git';
    const locked = await render();
    expect(hasButton(locked.container, 'Cleanup')).toBe(false);
    await locked.unmount();
  });

  it('withdraws every declare action from a caller who may not declare', async () => {
    mocks.canDeclare = false;
    mocks.canEdit = false;
    mocks.folder = '';
    const view = await render();
    expect(hasButton(view.container, '+ New key')).toBe(false);
    expect(hasButton(view.container, '+ Key')).toBe(false);
    expect(hasButton(view.container, 'Cleanup')).toBe(false);
    // Not a Git project, so the Git explanation would be a lie.
    expect(view.container.textContent ?? '').not.toContain('managed in Git');
    await view.unmount();
  });

  it('keeps Cleanup for a caller who may edit definitions but not republish', async () => {
    mocks.canDeclare = false;
    mocks.folder = '';
    const view = await render();
    expect(hasButton(view.container, '+ New key')).toBe(false);
    expect(hasButton(view.container, 'Cleanup')).toBe(true);
    await view.unmount();
  });

  it('withdraws every declare action and explains why when git-managed', async () => {
    mocks.source = 'git';
    const view = await render();
    expect(hasButton(view.container, '+ New key')).toBe(false);
    expect(hasButton(view.container, '+ Key')).toBe(false);
    expect(view.container.textContent ?? '').toContain('managed in Git');
    // Value actions stay live: the cell is still an open control.
    expect(hasButton(view.container, '+ New key')).toBe(false);
    expect(
      [...view.container.querySelectorAll('button')].some((button) =>
        (button.getAttribute('aria-label') ?? '').startsWith('LOG_LEVEL in development'),
      ),
    ).toBe(true);
    await view.unmount();
  });
});

it('keeps linked keys in their folder and shows their relationship separately', async () => {
  mocks.groupId = 'linked_a';
  const view = await render();
  const sections = [...view.container.querySelectorAll('.matrix__group-toggle')];
  expect(sections).toHaveLength(1);
  expect(sections[0]?.textContent).toContain('app');
  expect(sections[0]?.textContent).not.toContain('Database credentials');
  expect(view.container.querySelector('.matrix__linked-keys')?.textContent).toContain(
    'Linked keys: Database credentials',
  );
  expect(view.container.querySelector('.matrix__linked-keys')?.getAttribute('title')).toContain(
    'Pending changes publish together',
  );
  await view.unmount();
});

it('takes the remote empty-matrix environment link to that instance settings', async () => {
  mocks.emptyEnvironments = true;
  mocks.workspace = { origin: 'https://remote.example', remote: 'other', client: createClient() };
  const view = await render();
  const link = [...view.container.querySelectorAll('a')].find((link) => link.textContent === 'Project settings › New environment');
  expect(link?.href).toBe('https://remote.example/orgs/org_a/projects/project_a/settings');
  await view.unmount();
});

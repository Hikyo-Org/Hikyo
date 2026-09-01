// @vitest-environment happy-dom
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { renderForm } from '../testkit/renderForm.tsx';
import { Matrix } from './Matrix.tsx';

const holder = vi.hoisted(() => ({ project: undefined as unknown }));

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
  return { ...actual, useWorkspaceContext: () => null };
});

vi.mock('../api/matrix.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/matrix.ts')>();
  return {
    ...actual,
    useMatrixProject: () => holder.project,
    useStageMatrixValue: () => ({ mutateAsync: vi.fn(), isPending: false }),
    useClearMatrixValue: () => ({ mutateAsync: vi.fn(), isPending: false }),
    usePublishMatrix: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
    useCopyMatrixConfig: () => ({ mutate: vi.fn(), isPending: false }),
    useReclassifyKey: () => ({ mutateAsync: vi.fn() }),
  };
});

const environment = (id: string, name: string, order: number) => ({
  id,
  org_id: 'org_a',
  project_id: 'project_a',
  name,
  display_order: order,
  created_at: '2026-08-24T08:00:00Z',
});

const key = {
  id: 'key_a',
  org_id: 'org_a',
  project_id: 'project_a',
  name: 'LOG_LEVEL',
  folder_path: '',
  classification: 'config' as const,
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string' as const, allow_empty: true } },
  // required_in: all is the case that would fabricate a false "missing value"
  // problem for a forbidden column if it were not excluded from detection.
  presence: { required_in: { mode: 'all' as const }, forbidden_in: { mode: 'none' as const } },
  group_id: '',
  created_at: '2026-08-24T08:00:00Z',
};

const readyCatalogue = {
  environments: {
    data: { items: [environment('env_a', 'development', 0), environment('env_b', 'production', 1)], count: 2 },
    isPending: false,
    isError: false,
    isSuccess: true,
    error: null,
  },
  keys: { data: { items: [key], count: 1 }, isPending: false, isError: false, isSuccess: true, error: null },
  groups: { data: { items: [], count: 0 }, isPending: false, isError: false, isSuccess: true, error: null },
};

const readyRow = {
  environmentId: 'env_a',
  environment: environment('env_a', 'development', 0),
  readiness: 'ready' as const,
  values: {
    status: 'ready' as const,
    data: {
      items: [{ key_id: 'key_a', name: 'LOG_LEVEL', classification: 'config' as const, set: true, revealed: true, value: 'info' }],
      count: 1,
    },
  },
  signals: { status: 'ready' as const, data: { environment_id: 'env_a', revision: 1n, cells: [] } },
  settings: { status: 'ready' as const, data: { protected: false } },
  pendingDrafts: { status: 'ready' as const, data: { items: [], count: 0 } },
};

const forbiddenRow = {
  environmentId: 'env_b',
  environment: environment('env_b', 'production', 1),
  readiness: 'forbidden' as const,
  values: { status: 'forbidden' as const },
  signals: { status: 'forbidden' as const },
  settings: { status: 'forbidden' as const },
  pendingDrafts: { status: 'forbidden' as const },
};

const forbiddenCatalogueQuery = {
  data: undefined,
  isPending: false,
  isError: true,
  isSuccess: false,
  error: new ApiError(403, 'forbidden'),
};

function renderMatrix() {
  return renderForm(
    <MemoryRouter initialEntries={['/orgs/org_a/projects/project_a/matrix']}>
      <Routes>
        <Route path="/orgs/:org/projects/:project/matrix" element={<Matrix />} />
      </Routes>
    </MemoryRouter>,
  );
}

function alertText(container: HTMLElement): string {
  return [...container.querySelectorAll('[role="alert"]')].map((node) => node.textContent).join(' ');
}

describe('Matrix per-environment degradation (#451)', () => {
  it('renders the grid with one column degraded when a single environment is forbidden', async () => {
    holder.project = { ...readyCatalogue, environmentRows: [readyRow, forbiddenRow] };
    const view = await renderMatrix();

    // The grid rendered — the catalogue loaded, so no whole-grid failure.
    expect(view.container.textContent).not.toContain('could not be loaded');
    // The forbidden column names its state, in words, in the header.
    expect(view.container.textContent).toContain('No access to this environment');
    // The readable column stays interactive; the forbidden one does not.
    const cellButtons = [...view.container.querySelectorAll('button')].filter((button) =>
      button.getAttribute('aria-label')?.includes('LOG_LEVEL'),
    );
    expect(cellButtons).toHaveLength(1);
    expect(cellButtons[0]?.getAttribute('aria-label')).toContain('development');
    // The forbidden column is excluded from problem detection: a required_in:all
    // key must not fabricate a "missing value" problem for a column we cannot read.
    expect(view.container.textContent).not.toContain('problem');

    await view.unmount();
  });

  it('does not offer a degraded column for publish even when its signals still carry a pending draft', async () => {
    // Mixed-family degradation: settings failed (→ readiness forbidden) while
    // signals still hold a pending draft. The column must not be publishable —
    // it would lose its protected marker and confirmation ceremony (#451).
    const mixedForbiddenRow = {
      ...forbiddenRow,
      settings: { status: 'forbidden' as const },
      signals: {
        status: 'ready' as const,
        data: {
          environment_id: 'env_b',
          revision: 3n,
          cells: [
            {
              key_id: 'key_a',
              name: 'LOG_LEVEL',
              classification: 'config' as const,
              pending_by_others: false,
              pending: { versionId: 'ver_pending', operation: 'set' as const },
            },
          ],
        },
      },
    };
    holder.project = { ...readyCatalogue, environmentRows: [readyRow, mixedForbiddenRow] };
    const view = await renderMatrix();

    // No publish pill: the only pending draft rides a degraded column, so it is
    // not counted as publishable.
    expect(view.container.querySelector('.matrix__drafts')).toBeNull();
    expect(view.container.textContent).toContain('No access to this environment');

    await view.unmount();
  });

  it('renders a permission page, not a reload prompt, when the whole catalogue is forbidden', async () => {
    holder.project = {
      environments: forbiddenCatalogueQuery,
      keys: forbiddenCatalogueQuery,
      groups: forbiddenCatalogueQuery,
      environmentRows: [],
    };
    const view = await renderMatrix();

    expect(alertText(view.container)).toContain('do not have permission');
    expect(view.container.textContent).not.toContain('Reload to try again');

    await view.unmount();
  });
});

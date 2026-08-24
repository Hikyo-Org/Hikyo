// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle } from '../testkit/renderForm.tsx';
import { Matrix } from './Matrix.tsx';

const mocks = vi.hoisted(() => ({
  stage: vi.fn<() => Promise<{ readonly findings: readonly never[] }>>(),
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
  return { ...actual, useWorkspaceContext: () => null };
});

vi.mock('../api/matrix.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/matrix.ts')>();
  return {
    ...actual,
    useMatrixProject: () => ({
      environments: {
        data: {
          items: [{
            id: 'env_a',
            org_id: 'org_a',
            project_id: 'project_a',
            name: 'development',
            display_order: 0,
            created_at: '2026-08-24T08:00:00Z',
          }],
          count: 1,
        },
        isPending: false,
        isError: false,
      },
      keys: {
        data: {
          items: [{
            id: 'key_a',
            org_id: 'org_a',
            project_id: 'project_a',
            name: 'LOG_LEVEL',
            folder_path: '',
            classification: 'config',
            description: '',
            deprecated: false,
            deprecation_note: '',
            declaration: { rule: { type: 'string', allow_empty: true } },
            presence: {
              required_in: { mode: 'none' },
              forbidden_in: { mode: 'none' },
            },
            group_id: '',
            created_at: '2026-08-24T08:00:00Z',
          }],
          count: 1,
        },
        isPending: false,
        isError: false,
      },
      groups: { data: { items: [], count: 0 }, isPending: false, isError: false },
      environmentRows: [{
        environmentId: 'env_a',
        environment: {
          id: 'env_a',
          org_id: 'org_a',
          project_id: 'project_a',
          name: 'development',
          display_order: 0,
          created_at: '2026-08-24T08:00:00Z',
        },
        readiness: 'ready',
        values: {
          status: 'ready',
          data: {
            items: [{
              key_id: 'key_a',
              name: 'LOG_LEVEL',
              classification: 'config',
              set: true,
              revealed: true,
              value: 'info',
            }],
            count: 1,
          },
        },
        signals: { status: 'ready', data: { environment_id: 'env_a', revision: 1n, cells: [] } },
        settings: { status: 'ready', data: { protected: false } },
        pendingDrafts: { status: 'ready', data: { items: [], count: 0 } },
      }],
    }),
    useStageMatrixValue: () => ({ mutateAsync: mocks.stage, isPending: false }),
    useClearMatrixValue: () => ({ mutateAsync: vi.fn(), isPending: false }),
    usePublishMatrix: () => ({
      mutate: vi.fn(),
      isPending: false,
      isError: false,
      error: null,
    }),
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

beforeEach(() => {
  mocks.stage.mockReset();
  mocks.stage.mockRejectedValue(new Error('stage rejected'));
});

describe('Matrix mutation refusal ownership', () => {
  it('shows an editor refusal once and removes it when the editor closes', async () => {
    const view = await renderForm(
      <MemoryRouter initialEntries={['/orgs/org_a/projects/project_a/matrix']}>
        <Routes>
          <Route path="/orgs/:org/projects/:project/matrix" element={<Matrix />} />
        </Routes>
      </MemoryRouter>,
    );

    await act(async () => buttonNamed(view.container, 'LOG_LEVEL in development: info').click());
    const textarea = view.container.querySelector<HTMLTextAreaElement>('textarea');
    if (textarea === null) throw new Error('matrix row editor textarea is missing');
    await act(async () => typeInto(textarea, 'debug'));
    await act(async () => buttonNamed(view.container, 'Save 1 draft').click());
    await settle();

    const refusal = 'The server could not stage this value.';
    expect(alertsNamed(view.container, refusal)).toHaveLength(1);

    await act(async () => buttonNamed(view.container, 'Close row editor').click());
    expect(alertsNamed(view.container, refusal)).toHaveLength(0);

    await view.unmount();
  });
});

function buttonNamed(container: HTMLElement, name: string): HTMLButtonElement {
  const button = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.getAttribute('aria-label') === name || candidate.textContent === name,
  );
  if (button === undefined) throw new Error(`button ${name} is missing`);
  return button;
}

function alertsNamed(container: HTMLElement, text: string): readonly Element[] {
  return [...container.querySelectorAll('[role="alert"]')].filter(
    (alert) => alert.textContent?.includes(text) === true,
  );
}

function typeInto(textarea: HTMLTextAreaElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
  if (setter === undefined) throw new Error('HTMLTextAreaElement exposes no value setter');
  setter.call(textarea, value);
  textarea.dispatchEvent(new Event('input', { bubbles: true }));
}

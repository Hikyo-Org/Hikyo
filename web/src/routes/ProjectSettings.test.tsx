// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { created, renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { NewEnvironmentForm, ProjectSettings } from './ProjectSettings.tsx';

const mocks = vi.hoisted(() => ({
  refreshSession: vi.fn(),
}));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({ refreshSession: mocks.refreshSession }),
}));

vi.mock('../api/definitions.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/definitions.ts')>();
  return {
    ...actual,
    useDefinitionsSettings: () => ({ data: undefined, isError: false, isPending: true }),
    useSetDefinitionsSettings: () => ({ isPending: false, mutate: vi.fn() }),
  };
});

vi.mock('../api/settings.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/settings.ts')>();
  return {
    ...actual,
    useEnvironments: () => ({ data: undefined, isError: false, isPending: true }),
    useEnvironmentSettings: () => new Map(),
    useOrgRetention: () => ({ data: undefined, isError: false, isPending: true }),
    useProject: () => ({
      data: {
        id: 'project_1',
        org_id: 'org_1',
        name: 'Payments',
        created_at: '2026-01-01T00:00:00Z',
      },
      isError: false,
    }),
    useProjectRetention: () => ({ data: undefined, isError: false }),
    useRenameProject: () => ({ isPending: false, mutate: vi.fn() }),
  };
});

afterEach(() => {
  mocks.refreshSession.mockReset();
  vi.unstubAllGlobals();
});

describe('ProjectSettings', () => {
  it('navigates to the project list after deleting the current project', async () => {
    // The redirect must not wait for session refresh: refresh immediately
    // invalidates the deleted project's query in the real provider.
    mocks.refreshSession.mockReturnValue(new Promise<void>(() => {}));
    const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) =>
      Promise.resolve(new Response(null, { status: 204 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { container, unmount } = await renderForm(
      <MemoryRouter initialEntries={['/orgs/org_1/projects/project_1/settings']}>
        <Routes>
          <Route path="/orgs/:org/projects/:project/settings" element={<ProjectSettings />} />
          <Route path="/projects" element={<p>Project list</p>} />
        </Routes>
      </MemoryRouter>,
    );
    const input = container.querySelector<HTMLInputElement>('.danger-zone input');
    if (input === null) {
      throw new Error('the project delete confirmation input is missing');
    }
    const deleteButton = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Delete project',
    );
    if (deleteButton === undefined) {
      throw new Error('the project delete button is missing');
    }

    await act(async () => typeInto(input, 'Payments'));
    await act(async () => deleteButton.click());
    await settleTask();

    expect(fetchMock).toHaveBeenCalledOnce();
    const request = fetchMock.mock.calls[0]?.[0];
    if (!(request instanceof Request)) {
      throw new Error('fetch was not called with a Request');
    }
    expect(request.method).toBe('DELETE');
    expect(new URL(request.url).pathname).toBe('/api/v1/orgs/org_1/projects/project_1');
    expect(container.textContent).toContain('Project list');
    expect(container.textContent).not.toContain('This project could not be read.');

    await unmount();
  });
});

describe('NewEnvironmentForm', () => {
  it('posts the entered name and announces the created environment', async () => {
    const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) =>
      Promise.resolve(
        created({
          id: 'env_123e4567-e89b-12d3-a456-426614174000',
          org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
          project_id: 'prj_123e4567-e89b-12d3-a456-426614174002',
          name: 'staging',
          display_order: 0,
          created_at: '2026-01-01T00:00:00Z',
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderForm(
      <NewEnvironmentForm org="org_1" project="project_1" />,
    );
    const input = container.querySelector('input');
    if (!(input instanceof HTMLInputElement)) {
      throw new Error('the form has no name input');
    }
    const form = container.querySelector('form');
    if (!(form instanceof HTMLFormElement)) {
      throw new Error('the form element is missing');
    }

    await act(async () => {
      typeInto(input, 'staging');
    });
    await act(async () => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    await settle();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const request = fetchMock.mock.calls[0]?.[0];
    if (!(request instanceof Request)) {
      throw new Error('fetch was not called with a Request');
    }
    expect(request.method).toBe('POST');
    expect(new URL(request.url).pathname).toBe(
      '/api/v1/orgs/org_1/projects/project_1/environments',
    );
    expect(await request.json()).toEqual({ name: 'staging' });

    const status = container.querySelector('[role="status"]');
    expect(status?.textContent).toContain('Environment staging created.');
  });
});

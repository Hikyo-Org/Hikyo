// @vitest-environment happy-dom
import { useQuery } from '@tanstack/react-query';
import { act, useState } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { created, renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import {
  EnvironmentLifecycleActions,
  NewEnvironmentForm,
  ProjectSettings,
} from './ProjectSettings.tsx';

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

    const { container, client } = await renderForm(
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

const DEV = {
  id: 'env_123e4567-e89b-12d3-a456-426614174010',
  name: 'dev',
};
const PROD = {
  id: 'env_123e4567-e89b-12d3-a456-426614174011',
  name: 'prod',
};

function environment(id: string, name: string, displayOrder: number) {
  return {
    id,
    org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
    project_id: 'prj_123e4567-e89b-12d3-a456-426614174002',
    name,
    display_order: displayOrder,
    created_at: '2026-01-01T00:00:00Z',
  };
}

function requestFromCall(call: Parameters<typeof fetch> | undefined): Request {
  const request = call?.[0];
  if (!(request instanceof Request)) {
    throw new Error('fetch was not called with a Request');
  }
  return request;
}

function DeleteFeedbackHarness({ onDone }: { readonly onDone: (text: string) => void }) {
  const [done, setDone] = useState('');
  const environments = useQuery({
    queryKey: ['environments', 'org_1', 'project_1'],
    queryFn: () => Promise.resolve({ items: [], count: 0 }),
    initialData: { items: [DEV], count: 1 },
    staleTime: Infinity,
  });
  const current = environments.data.items[0];

  return (
    <>
      {current === undefined ? null : (
        <EnvironmentLifecycleActions
          org="org_1"
          project="project_1"
          environment={current}
          environments={environments.data.items}
          onDone={(text) => {
            setDone(text);
            onDone(text);
          }}
        />
      )}
      <p role="status">{done}</p>
    </>
  );
}

describe('EnvironmentLifecycleActions', () => {
  it('keeps deletion feedback after invalidation unmounts the deleted row', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(null, { status: 204 }))));
    const done = vi.fn();
    const { container } = await renderForm(<DeleteFeedbackHarness onDone={done} />);
    const confirmInput = container.querySelector(`input[placeholder="${DEV.name}"]`);
    if (!(confirmInput instanceof HTMLInputElement)) {
      throw new Error('delete confirmation input is missing');
    }
    await act(async () => typeInto(confirmInput, DEV.name));
    const deleteButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Delete environment',
    );
    if (deleteButton === undefined) {
      throw new Error('delete button is missing');
    }
    await act(async () => deleteButton.click());
    await settleTask();

    expect(container.querySelector('input[name="rename-environment"]')).toBeNull();
    expect(container.querySelector('[role="status"]')?.textContent).toBe(
      'Environment dev deleted.',
    );
    expect(done).toHaveBeenCalledWith('Environment dev deleted.');
  });

  it('renames and deletes through deliberate user controls', async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const request = requestFromCall(args);
      if (request.method === 'PATCH') {
        return Promise.resolve(
          new Response(JSON.stringify(environment(DEV.id, 'development', 0)), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      if (request.method === 'DELETE') {
        return Promise.resolve(new Response(null, { status: 204 }));
      }
      throw new Error(`unexpected ${request.method} ${request.url}`);
    });
    vi.stubGlobal('fetch', fetchMock);
    const done = vi.fn();
    const { container } = await renderForm(
      <EnvironmentLifecycleActions
        org="org_1"
        project="project_1"
        environment={DEV}
        environments={[DEV, PROD]}
        onDone={done}
      />,
    );
    const renameInput = container.querySelector('input[name="rename-environment"]');
    if (!(renameInput instanceof HTMLInputElement)) {
      throw new Error('rename input is missing');
    }
    await act(async () => typeInto(renameInput, 'development'));
    const renameButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Rename environment',
    );
    if (renameButton === undefined) {
      throw new Error('rename button is missing');
    }
    await act(async () => renameButton.click());
    await settleTask();

    const renameRequest = requestFromCall(fetchMock.mock.calls[0]);
    expect(renameRequest.method).toBe('PATCH');
    expect(new URL(renameRequest.url).pathname).toBe(
      `/api/v1/orgs/org_1/projects/project_1/environments/${DEV.id}`,
    );
    expect(await renameRequest.json()).toEqual({ name: 'development' });
    expect(done).toHaveBeenCalledWith('Environment dev renamed to development.');

    const confirmInput = container.querySelector(`input[placeholder="${DEV.name}"]`);
    if (!(confirmInput instanceof HTMLInputElement)) {
      throw new Error('delete confirmation input is missing');
    }
    await act(async () => typeInto(confirmInput, DEV.name));
    const deleteButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Delete environment',
    );
    if (deleteButton === undefined) {
      throw new Error('delete button is missing');
    }
    expect(deleteButton.disabled).toBe(false);
    await act(async () => deleteButton.click());
    await settleTask();

    const deleteRequest = requestFromCall(fetchMock.mock.calls[1]);
    expect(deleteRequest.method).toBe('DELETE');
    expect(new URL(deleteRequest.url).pathname).toBe(
      `/api/v1/orgs/org_1/projects/project_1/environments/${DEV.id}`,
    );
    expect(done).toHaveBeenCalledWith('Environment dev deleted.');
  });

  it('moves with the complete ordered set and clones into a named environment', async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const request = requestFromCall(args);
      if (request.method === 'PUT') {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [environment(PROD.id, PROD.name, 0), environment(DEV.id, DEV.name, 1)],
              count: 2,
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        );
      }
      if (request.method === 'POST') {
        return Promise.resolve(
          created({
            environment: environment(
              'env_123e4567-e89b-12d3-a456-426614174012',
              'staging',
              2,
            ),
            copied: ['API_URL'],
            uncopied_secrets: ['TOKEN'],
          }),
        );
      }
      throw new Error(`unexpected ${request.method} ${request.url}`);
    });
    vi.stubGlobal('fetch', fetchMock);
    const done = vi.fn();
    const { container, client } = await renderForm(
      <EnvironmentLifecycleActions
        org="org_1"
        project="project_1"
        environment={DEV}
        environments={[DEV, PROD]}
        onDone={done}
      />,
    );
    const environmentsKey = ['environments', 'org_1', 'project_1'];
    const matrixKeysKey = ['matrix-keys', 'org_1', 'project_1'];
    client.setQueryData(environmentsKey, { items: [DEV, PROD], count: 2 });
    client.setQueryData(matrixKeysKey, { items: [], count: 0 });

    const moveDown = container.querySelector('button[aria-label="Move dev down"]');
    if (!(moveDown instanceof HTMLButtonElement)) {
      throw new Error('move-down button is missing');
    }
    await act(async () => moveDown.click());
    await settleTask();
    const reorderRequest = requestFromCall(fetchMock.mock.calls[0]);
    expect(reorderRequest.method).toBe('PUT');
    expect(new URL(reorderRequest.url).pathname).toBe(
      '/api/v1/orgs/org_1/projects/project_1/environments/order',
    );
    expect(await reorderRequest.json()).toEqual({ environment_ids: [PROD.id, DEV.id] });
    expect(client.getQueryState(environmentsKey)?.isInvalidated).toBe(true);
    expect(client.getQueryState(matrixKeysKey)?.isInvalidated).toBe(true);

    const cloneInput = container.querySelector('input[name="clone-environment"]');
    if (!(cloneInput instanceof HTMLInputElement)) {
      throw new Error('clone input is missing');
    }
    await act(async () => typeInto(cloneInput, 'staging'));
    const cloneButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Clone environment',
    );
    if (cloneButton === undefined) {
      throw new Error('clone button is missing');
    }
    await act(async () => cloneButton.click());
    await settleTask();
    const cloneRequest = requestFromCall(fetchMock.mock.calls[1]);
    expect(cloneRequest.method).toBe('POST');
    expect(new URL(cloneRequest.url).pathname).toBe(
      '/api/v1/orgs/org_1/projects/project_1/environments/clone',
    );
    expect(await cloneRequest.json()).toEqual({
      name: 'staging',
      source_environment_id: DEV.id,
    });
    expect(done).toHaveBeenCalledWith(
      'Environment dev cloned to staging. Copied 1 value; 1 secret could not be copied.',
    );
  });
});

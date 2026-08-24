// @vitest-environment happy-dom
import { useQuery } from '@tanstack/react-query';
import { act, useState } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { AuthProvider } from '../app/AuthProvider.tsx';
import { created, renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import {
  EnvironmentLifecycleActions,
  NewEnvironmentForm,
  ProjectSettings,
} from './ProjectSettings.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ProjectSettings', () => {
  it('navigates to the project list after deleting the current project', async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const input = args[0];
      const request = input instanceof Request ? input : new Request(input);
      const path = new URL(request.url, 'http://localhost').pathname;
      if (request.method === 'GET' && path === '/api/v1/auth/whoami') {
        return Promise.resolve(new Response(null, { status: 401 }));
      }
      if (request.method === 'DELETE' && path === '/api/v1/orgs/org_1/projects/project_1') {
        return Promise.resolve(new Response(null, { status: 204 }));
      }
      const json = settingsResponse(request.method, path, 'db');
      return Promise.resolve(
        new Response(JSON.stringify(json), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const { container, unmount } = await renderForm(
      <AuthProvider>
        <MemoryRouter initialEntries={['/orgs/org_1/projects/project_1/settings']}>
          <Routes>
            <Route path="/orgs/:org/projects/:project/settings" element={<ProjectSettings />} />
            <Route path="/projects" element={<p>Project list</p>} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>,
    );
    await settleTask();
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

    const request = fetchMock.mock.calls
      .map(([input]) => (input instanceof Request ? input : new Request(input)))
      .find(
        (candidate) =>
          candidate.method === 'DELETE' &&
          new URL(candidate.url).pathname === '/api/v1/orgs/org_1/projects/project_1',
      );
    if (request === undefined) {
      throw new Error('the project delete request is missing');
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

describe('definitions policy', () => {
  it('stages the selected source until Apply sends one mutation', async () => {
    let savedDefinitionsSource: 'db' | 'git' = 'db';
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const input = args[0];
      const request = input instanceof Request ? input : new Request(input);
      const path = new URL(request.url, 'http://localhost').pathname;
      if (request.method === 'GET' && path === '/api/v1/auth/whoami') {
        return Promise.resolve(new Response(null, { status: 401 }));
      }
      if (
        request.method === 'PUT' &&
        path === '/api/v1/orgs/org_1/projects/project_1/definitions/settings'
      ) {
        savedDefinitionsSource = 'git';
      }
      const json = settingsResponse(request.method, path, savedDefinitionsSource);
      return Promise.resolve(
        new Response(JSON.stringify(json), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const view = await renderForm(
      <AuthProvider>
        <MemoryRouter initialEntries={['/orgs/org_1/projects/project_1/settings']}>
          <Routes>
            <Route path="/orgs/:org/projects/:project/settings" element={<ProjectSettings />} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>,
    );

    try {
      await settleTask();
      const source = labelledSelect(view.container, 'Definitions source');
      const apply = button(view.container, 'Apply definitions source');

      expect(source.value).toBe('db');
      expect(apply.disabled).toBe(true);

      await act(async () => {
        selectOption(source, 'git');
      });

      expect(source.value).toBe('git');
      expect(apply.disabled).toBe(false);
      expect(definitionsWrites(fetchMock.mock.calls)).toHaveLength(0);

      await act(async () => {
        apply.click();
      });
      await settleTask();

      const writes = definitionsWrites(fetchMock.mock.calls);
      expect(writes).toHaveLength(1);
      const write = writes[0];
      if (write === undefined) {
        throw new Error('the definitions mutation is missing');
      }
      expect(write.method).toBe('PUT');
      expect(await write.json()).toEqual({ definitions_source: 'git' });
      expect(source.value).toBe('git');
      expect(apply.disabled).toBe(true);
    } finally {
      await view.unmount();
    }
  });
});

function settingsResponse(
  method: string,
  path: string,
  definitionsSource: 'db' | 'git',
): unknown {
  const projectPath = '/api/v1/orgs/org_1/projects/project_1';
  if (method === 'GET' && path === projectPath) {
    return {
      id: 'prj_123e4567-e89b-12d3-a456-426614174000',
      org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
      name: 'Payments',
      created_at: '2026-01-01T00:00:00Z',
    };
  }
  if (method === 'GET' && path === `${projectPath}/environments`) {
    return { items: [], count: 0 };
  }
  if (method === 'GET' && path === '/api/v1/orgs/org_1/retention') {
    return { mode: 'keep-if-either', max_age_seconds: 7_776_000, last_revisions: 10 };
  }
  if (method === 'GET' && path === `${projectPath}/retention`) {
    return {
      inherited: true,
      mode: 'keep-if-either',
      max_age_seconds: 7_776_000,
      last_revisions: 10,
    };
  }
  if (
    (method === 'GET' || method === 'PUT') &&
    path === `${projectPath}/definitions/settings`
  ) {
    return { definitions_source: definitionsSource };
  }
  throw new Error(`unexpected ${method} ${path}`);
}

function labelledSelect(container: HTMLElement, text: string): HTMLSelectElement {
  const label = [...container.querySelectorAll('label')].find(
    (candidate) => candidate.textContent === text,
  );
  const select = label?.htmlFor === undefined ? null : container.querySelector(`#${label.htmlFor}`);
  if (!(select instanceof HTMLSelectElement)) {
    throw new Error(`${text} select is missing`);
  }
  return select;
}

function button(container: HTMLElement, text: string): HTMLButtonElement {
  const match = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent?.trim() === text,
  );
  if (!(match instanceof HTMLButtonElement)) {
    throw new Error(`${text} button is missing`);
  }
  return match;
}

function selectOption(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set;
  if (setter === undefined) {
    throw new Error('HTMLSelectElement exposes no value setter');
  }
  setter.call(select, value);
  select.dispatchEvent(new Event('change', { bubbles: true }));
}

function definitionsWrites(calls: readonly Parameters<typeof fetch>[]): Request[] {
  return calls.flatMap(([input]) => {
    const request = input instanceof Request ? input : new Request(input);
    return request.method === 'PUT' &&
      new URL(request.url, 'http://localhost').pathname.endsWith('/definitions/settings')
      ? [request]
      : [];
  });
}

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

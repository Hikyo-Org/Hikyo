// @vitest-environment happy-dom
import { useQuery } from '@tanstack/react-query';
import { act, useState } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { AuthProvider } from '../app/AuthProvider.tsx';
import { created, renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { EnvironmentLifecycleActions, ProjectSettings } from './ProjectSettings.tsx';

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

describe('definitions policy', () => {
  it('sends one mutation when the definitions source changes', async () => {
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

      expect(source.value).toBe('db');

      await act(async () => {
        selectOption(source, 'git');
      });
      await settleTask();

      expect(source.value).toBe('git');

      const writes = definitionsWrites(fetchMock.mock.calls);
      expect(writes).toHaveLength(1);
      const write = writes[0];
      if (write === undefined) {
        throw new Error('the definitions mutation is missing');
      }
      expect(write.method).toBe('PUT');
      expect(await write.json()).toEqual({ definitions_source: 'git' });
      expect(source.value).toBe('git');
    } finally {
      await view.unmount();
    }
  });
});

describe('environment creation', () => {
  it('posts the typed name to the project environments endpoint', async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const input = args[0];
      const request = input instanceof Request ? input : new Request(input);
      const path = new URL(request.url, 'http://localhost').pathname;
      if (request.method === 'GET' && path === '/api/v1/auth/whoami') {
        return Promise.resolve(new Response(null, { status: 401 }));
      }
      if (
        request.method === 'POST' &&
        path === '/api/v1/orgs/org_1/projects/project_1/environments'
      ) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              id: 'env_123e4567-e89b-12d3-a456-426614174002',
              org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
              project_id: 'prj_123e4567-e89b-12d3-a456-426614174000',
              name: 'production',
              display_order: 0,
              created_at: '2026-01-01T00:00:00Z',
            }),
            { status: 201, headers: { 'Content-Type': 'application/json' } },
          ),
        );
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
      const input = labelledInput(view.container, 'New environment name');
      await act(async () => typeInto(input, 'production'));
      await act(async () => button(view.container, 'Create').click());
      await settleTask();

      const post = fetchMock.mock.calls
        .map(([input]) => (input instanceof Request ? input : new Request(input)))
        .find(
          (candidate) =>
            candidate.method === 'POST' &&
            new URL(candidate.url).pathname ===
              '/api/v1/orgs/org_1/projects/project_1/environments',
        );
      if (post === undefined) {
        throw new Error('the create-environment request is missing');
      }
      expect(await post.json()).toEqual({ name: 'production' });
      expect(view.container.textContent).toContain('Environment production created.');
    } finally {
      await view.unmount();
    }
  });
});

describe('project crypto maintenance', () => {
  it('rotates the project DEK after confirmation, then drains re-encryption across runs', async () => {
    let reencryptCalls = 0;
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const input = args[0];
      const request = input instanceof Request ? input : new Request(input);
      const path = new URL(request.url, 'http://localhost').pathname;
      if (request.method === 'GET' && path === '/api/v1/auth/whoami') {
        return Promise.resolve(new Response(null, { status: 401 }));
      }
      if (request.method === 'POST' && path === '/api/v1/instance/rotate-dek') {
        return Promise.resolve(
          new Response(JSON.stringify({ scope: 'project', key_version: 2 }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      if (
        request.method === 'POST' &&
        path === '/api/v1/orgs/org_1/projects/project_1/reencrypt'
      ) {
        reencryptCalls += 1;
        return Promise.resolve(
          new Response(
            JSON.stringify({ scope: 'project', rows_moved: reencryptCalls === 1 ? 3 : 0 }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        );
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

      await act(async () => button(view.container, 'Rotate the project DEK').click());
      const dialog = view.container.ownerDocument.querySelector('dialog.ceremony');
      expect(dialog?.textContent).toContain('incomplete until');
      await act(async () => button(view.container, 'Rotate the DEK').click());
      await settleTask();

      const rotate = fetchMock.mock.calls
        .map(([input]) => (input instanceof Request ? input : new Request(input)))
        .find(
          (r) => r.method === 'POST' && new URL(r.url).pathname === '/api/v1/instance/rotate-dek',
        );
      if (rotate === undefined) throw new Error('the rotate-dek request is missing');
      expect(await rotate.json()).toEqual({ scope: 'project', org: 'org_1', project: 'project_1' });
      expect(view.container.textContent).toContain('version 2');

      await act(async () => button(view.container, 'Re-encrypt the project').click());
      await settleTask();

      const reencrypts = fetchMock.mock.calls
        .map(([input]) => (input instanceof Request ? input : new Request(input)))
        .filter(
          (r) =>
            r.method === 'POST' &&
            new URL(r.url).pathname === '/api/v1/orgs/org_1/projects/project_1/reencrypt',
        );
      expect(reencrypts).toHaveLength(2);
      expect(view.container.textContent).toContain('moved 3 ciphertext rows');
      expect(view.container.textContent).toContain('2 runs');
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

function labelledInput(container: HTMLElement, text: string): HTMLInputElement {
  const label = [...container.querySelectorAll('label')].find(
    (candidate) => candidate.textContent === text,
  );
  const input = label?.htmlFor === undefined ? null : container.querySelector(`#${label.htmlFor}`);
  if (!(input instanceof HTMLInputElement)) {
    throw new Error(`${text} input is missing`);
  }
  return input;
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

  it('disables a sibling row while any topology mutation is in flight', async () => {
    // A never-resolving reorder holds one mutation in flight so the shared busy
    // gate can be observed: a second row's controls must go disabled too, which
    // is what stops two rows submitting whole-set reorders from a stale set.
    let release: (response: Response) => void = () => {};
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    vi.stubGlobal('fetch', vi.fn(() => pending));
    const { container } = await renderForm(
      <>
        <EnvironmentLifecycleActions
          org="org_1"
          project="project_1"
          environment={DEV}
          environments={[DEV, PROD]}
          onDone={vi.fn()}
        />
        <EnvironmentLifecycleActions
          org="org_1"
          project="project_1"
          environment={PROD}
          environments={[DEV, PROD]}
          onDone={vi.fn()}
        />
      </>,
    );
    const devMoveDown = container.querySelector('button[aria-label="Move dev down"]');
    const prodMoveUp = container.querySelector('button[aria-label="Move prod up"]');
    if (!(devMoveDown instanceof HTMLButtonElement) || !(prodMoveUp instanceof HTMLButtonElement)) {
      throw new Error('move buttons are missing');
    }
    expect(prodMoveUp.disabled).toBe(false);
    // The reorder is now in flight. `useIsMutating` disables every row through a
    // cache subscription that re-renders on its own tick, so poll rather than
    // assume the flip has landed by the time click() returns.
    await act(async () => devMoveDown.click());
    await vi.waitFor(() => {
      if (!prodMoveUp.disabled) throw new Error('sibling row not disabled yet');
    });

    await act(async () => release(new Response(null, { status: 200 })));
    await settleTask();
  });
});

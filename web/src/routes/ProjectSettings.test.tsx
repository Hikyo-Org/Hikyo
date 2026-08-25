// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { AuthProvider } from '../app/AuthProvider.tsx';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { ProjectSettings } from './ProjectSettings.tsx';

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

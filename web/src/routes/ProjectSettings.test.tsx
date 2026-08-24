// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { AuthProvider } from '../app/AuthProvider.tsx';
import { created, renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { NewEnvironmentForm, ProjectSettings } from './ProjectSettings.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
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
      name: 'project_1',
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

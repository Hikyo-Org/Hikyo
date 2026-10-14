// @vitest-environment happy-dom
import { act, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MemoryRouter, Outlet, Route, Routes } from 'react-router';

import { AuthProvider } from '../app/AuthProvider.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { created, renderForm, settle, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { Overview } from './Placeholder.tsx';
import { NewProjectForm, Projects } from './Projects.tsx';

const project = {
  id: 'prj_123e4567-e89b-12d3-a456-426614174000',
  org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
  name: 'billing',
  created_at: '2026-01-01T00:00:00Z',
};

function stubProjects(items: readonly (typeof project)[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((..._args: Parameters<typeof fetch>) =>
      Promise.resolve(
        new Response(JSON.stringify({ items, count: items.length }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    ),
  );
}

function inShell(node: ReactNode, activeOrgId: string, path: string) {
  return (
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<Outlet context={{ activeOrgId }} />}>
          <Route path={path} element={node} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('NewProjectForm', () => {
  it('posts the entered name and announces the created project', async () => {
    const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) =>
      Promise.resolve(
        created({
          id: 'prj_123e4567-e89b-12d3-a456-426614174000',
          org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
          name: 'billing',
          created_at: '2026-01-01T00:00:00Z',
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { container } = await renderForm(<NewProjectForm org="org_1" />);
    const input = container.querySelector('input');
    if (!(input instanceof HTMLInputElement)) {
      throw new Error('the form has no name input');
    }
    const form = container.querySelector('form');
    if (!(form instanceof HTMLFormElement)) {
      throw new Error('the form element is missing');
    }

    await act(async () => {
      typeInto(input, 'billing');
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
    expect(new URL(request.url).pathname).toBe('/api/v1/orgs/org_1/projects');
    expect(await request.json()).toEqual({ name: 'billing' });

    const status = container.querySelector('[role="status"]');
    expect(status?.textContent).toContain('Project billing created.');
  });
});

describe('Projects', () => {
  it('names the next action above the form when the organisation has no projects', async () => {
    stubProjects([]);
    const { container, unmount } = await renderForm(inShell(<Projects />, 'org_1', '/projects'));
    await settleTask();

    expect(container.querySelector('h1')?.textContent).toBe('Projects');
    expect(container.querySelector('.page__lede')).not.toBeNull();
    const statuses = [...container.querySelectorAll('[role="status"]')].map((s) => s.textContent);
    expect(statuses).toContain('No projects yet. Create the first one below.');
    const empty = container.querySelector('#projects-list');
    const form = container.querySelector('#projects-new');
    expect(empty).not.toBeNull();
    expect(form).not.toBeNull();
    expect(empty?.compareDocumentPosition(form ?? empty) ?? 0).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    await unmount();
  });

  it('offers no project form in the Hikyo system organisation and says why', async () => {
    const binding = { org_id: 'org_1', project_id: 'prj_system', environment_id: 'env_system', schema_version: 1 };
    const status = { owner_instance_id: 'instance_local', managed: true, binding, generation: 1, desired_revision: 1, latest_revision: 1, state: 'active', nodes: [], job: null };
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const request = input instanceof Request ? input : new Request(input, init);
        const path = new URL(request.url).pathname;
        const body =
          path === '/api/v1/instance/config'
            ? status
            : path === '/api/v1/auth/whoami'
              ? authenticatedIdentity
              : { items: [project], count: 1 };
        return Promise.resolve(Response.json(body));
      }),
    );
    const { container, unmount } = await renderForm(
      <AuthProvider>{inShell(<Projects />, 'org_1', '/projects')}</AuthProvider>,
    );
    await settleTask();
    await settle();

    expect(container.querySelector('#projects-new')).toBeNull();
    expect(container.querySelector('form')).toBeNull();
    // One section left (the list), so there is no jump index to render.
    expect(container.querySelector('nav.jump')).toBeNull();
    const statuses = [...container.querySelectorAll('[role="status"]')].map((s) => s.textContent ?? '');
    expect(statuses.some((text) => text.includes('Hikyo system organisation'))).toBe(true);
    await unmount();
  });

  it('leads the no-organisation state with the one action available', async () => {
    stubProjects([]);
    const { container, unmount } = await renderForm(inShell(<Projects />, '', '/projects'));
    await settleTask();

    expect(container.querySelector('[role="status"]')?.textContent).toMatch(
      /^Ask an instance administrator to invite you to an organisation\./,
    );
    expect(container.querySelector('form')).toBeNull();
    await unmount();
  });
});

describe('Overview', () => {
  it('links to the project list and lists the projects it knows', async () => {
    // Two: with exactly one the overview opens its matrix (Placeholder.test.tsx).
    stubProjects([project, { ...project, id: 'prj_123e4567-e89b-12d3-a456-426614174002', name: 'ledger' }]);
    const { container, unmount } = await renderForm(inShell(<Overview />, 'org_1', '/'));
    await settleTask();

    const choose = [...container.querySelectorAll('a')].find(
      (a) => a.textContent === 'Choose a project',
    );
    expect(choose?.getAttribute('href')).toBe('/projects');
    expect(container.querySelector('h1')?.textContent).toBe('Overview');
    expect(container.querySelector('#well-title')).toBeNull();
    const rows = [...container.querySelectorAll('.projects__list li')];
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain('billing');
    expect(rows[1]?.textContent).toContain('ledger');
    expect(rows[0]?.querySelector('a')?.getAttribute('href')).toBe(
      '/orgs/org_1/projects/prj_123e4567-e89b-12d3-a456-426614174000/matrix',
    );
    await unmount();
  });

  it('shows no project panel while the organisation has none', async () => {
    stubProjects([]);
    const { container, unmount } = await renderForm(inShell(<Overview />, 'org_1', '/'));
    await settleTask();

    expect(container.querySelector('#overview-projects')).toBeNull();
    expect(container.querySelector('a')?.textContent).toBe('Choose a project');
    await unmount();
  });
});

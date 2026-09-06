// @vitest-environment happy-dom
import { act, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MemoryRouter, Outlet, Route, Routes } from 'react-router';

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
    stubProjects([project]);
    const { container, unmount } = await renderForm(inShell(<Overview />, 'org_1', '/'));
    await settleTask();

    const choose = [...container.querySelectorAll('a')].find(
      (a) => a.textContent === 'Choose a project',
    );
    expect(choose?.getAttribute('href')).toBe('/projects');
    expect(container.querySelector('h1')?.textContent).toBe('Overview');
    expect(container.querySelector('#well-title')).toBeNull();
    const rows = [...container.querySelectorAll('.projects__list li')];
    expect(rows).toHaveLength(1);
    expect(rows[0]?.textContent).toContain('billing');
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

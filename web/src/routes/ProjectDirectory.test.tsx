// @vitest-environment happy-dom
import { QueryClient } from '@tanstack/react-query';
import { act } from 'react';
import { createMemoryRouter, Outlet, RouterProvider } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { Overview } from './Placeholder.tsx';
import { Shell } from './Shell.tsx';

// Only unrelated account controls are replaced. Both surfaces use the actual
// useProjects, generated operation/parser, fetch boundary and QueryClient.
vi.mock('../api/session.ts', async (original) => ({
  ...(await original<typeof import('../api/session.ts')>()),
  useLogout: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock('./StepUpBanner.tsx', () => ({ StepUpBanner: () => null }));

const orgA = 'org_123e4567-e89b-12d3-a456-426614174001';
const orgB = 'org_123e4567-e89b-12d3-a456-426614174002';
const first = {
  id: 'prj_123e4567-e89b-12d3-a456-426614174003',
  org_id: orgA,
  name: 'Billing',
  created_at: '2026-01-01T00:00:00Z',
};
const second = {
  ...first,
  id: 'prj_123e4567-e89b-12d3-a456-426614174004',
  name: 'Ledger',
};
const member = {
  ...authenticatedIdentity,
  capabilities: { ...authenticatedIdentity.capabilities, instance_operator: false },
};

function listed(items: readonly (typeof first)[]): Response {
  return Response.json({ items, count: items.length });
}

function refused(status = 500): Response {
  return Response.json({ error: { code: status === 403 ? 'forbidden' : 'internal_error' } }, { status });
}

function deferredResponse() {
  let finish: ((response: Response) => void) | undefined;
  const promise = new Promise<Response>((resolve) => { finish = resolve; });
  return {
    promise,
    resolve(response: Response) {
      if (finish === undefined) throw new Error('Response resolver was not initialized');
      finish(response);
    },
  };
}

function projectFetch(answer: (org: string) => Promise<Response>) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const path = new URL(input instanceof Request ? input.url : String(input), 'http://localhost').pathname;
    const match = /^\/api\/v1\/orgs\/([^/]+)\/projects$/.exec(path);
    const org = match?.[1];
    if (org === undefined) throw new Error(`Unexpected request: ${path}`);
    return answer(org);
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function button(container: ParentNode, name: string): HTMLButtonElement {
  const found = [...container.querySelectorAll('button')].find(
    (candidate) => candidate.textContent === name || candidate.getAttribute('aria-label') === name,
  );
  if (found === undefined) throw new Error(`Missing button: ${name}`);
  return found;
}

function region(container: ParentNode, selector: string): HTMLElement {
  const found = container.querySelector(selector);
  if (!(found instanceof HTMLElement)) throw new Error(`Missing region: ${selector}`);
  return found;
}

async function mountDirectory(options: { shell?: boolean; org?: string; path?: string } = {}) {
  const org = options.org ?? orgA;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity, gcTime: Infinity } },
  });
  client.setQueryData(['orgs'], {
    items: [{ id: orgA, name: 'Alpha' }, { id: orgB, name: 'Beta' }], count: 2,
  });
  client.setQueryData(['server-version'], 'test');
  const router = createMemoryRouter([{
    element: options.shell === true ? <Shell session={member} /> : <Outlet context={{ activeOrgId: org }} />,
    children: [
      { path: '/', element: <Overview /> },
      { path: '/projects', element: <h1>Project directory destination</h1> },
      { path: '/orgs/:org/projects/:project/matrix', element: <h1>Project matrix destination</h1> },
    ],
  }], { initialEntries: [options.path ?? '/'] });
  const view = await renderForm(<RouterProvider router={router} />, { client });
  return {
    ...view,
    router,
    close: async () => {
      await view.unmount();
      router.dispose();
      client.clear();
    },
  };
}

beforeEach(() => {
  const values = new Map<string, string>();
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    value: {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (key: string) => values.get(key) ?? null,
      key: (index: number) => [...values.keys()][index] ?? null,
      removeItem: (key: string) => values.delete(key),
      setItem: (key: string, value: string) => values.set(key, value),
    } satisfies Storage,
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  window.sessionStorage.clear();
  window.localStorage.clear();
});

describe.each([false, true])('project directory with Shell=%s', (shell) => {
  function surfaces(container: HTMLElement): HTMLElement[] {
    return shell
      ? [region(container, '#content'), region(container, '.sidebar__section--projects')]
      : [container];
  }

  it('announces a pending read without claiming the organisation is empty', async () => {
    const pending = deferredResponse();
    projectFetch(() => pending.promise);
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="status"]')?.textContent).toContain('Loading projects');
        expect(surface.textContent).not.toContain('No projects yet');
        expect(surface.textContent).not.toContain('Choose a project');
      }
      expect(view.client.getQueryState(['projects', orgA])?.status).toBe('pending');
      await act(async () => pending.resolve(listed([])));
      await settleTask();
    } finally { await view.close(); }
  });

  it('announces an HTTP 500 and recovers both consumers with one explicit retry', async () => {
    const pending = deferredResponse();
    const fetchMock = projectFetch(() => Promise.resolve(refused()));
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      const failure = view.client.getQueryState(['projects', orgA]);
      expect(failure?.status).toBe('error');
      expect(failure?.error).toBeInstanceOf(ApiError);
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="alert"]')?.textContent).toContain('Projects could not be loaded');
        expect(surface.textContent).not.toContain('No projects yet');
        expect(surface.textContent).not.toContain('Choose a project');
        expect(button(surface, 'Retry projects').disabled).toBe(false);
      }
      fetchMock.mockImplementation(() => pending.promise);
      const retrySurface = shell ? region(view.container, '.sidebar__section--projects') : view.container;
      await act(async () => button(retrySurface, 'Retry projects').click());
      await settleTask();
      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect(view.container.textContent).not.toContain('No projects yet');
      await act(async () => pending.resolve(listed([first, second])));
      await settleTask();
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="alert"]')).toBeNull();
        expect(surface.textContent).toContain('Billing');
        expect(surface.textContent).toContain('Ledger');
      }
      expect(view.router.state.location.pathname).toBe('/');
    } finally { await view.close(); }
  });

  it('only announces an empty directory after a successful empty response', async () => {
    projectFetch(() => Promise.resolve(listed([])));
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="status"]')?.textContent).toContain('No projects yet');
        expect(surface.querySelector('[role="alert"]')).toBeNull();
      }
      expect(view.client.getQueryState(['projects', orgA])?.status).toBe('success');
      expect(view.router.state.location.pathname).toBe('/');
    } finally { await view.close(); }
  });

  it('opens the only project after a successful read', async () => {
    projectFetch(() => Promise.resolve(listed([first])));
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      expect(view.router.state.location.pathname).toBe(`/orgs/${orgA}/projects/${first.id}/matrix`);
      expect(view.container.querySelector('h1')?.textContent).toBe('Project matrix destination');
    } finally { await view.close(); }
  });

  it.each([403, 500])('hides cached projects after a %s refetch and restores them after retry', async (status) => {
    const fetchMock = projectFetch(() => Promise.resolve(listed([first, second])));
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      expect(view.container.textContent).toContain('Billing');
      fetchMock.mockImplementation(() => Promise.resolve(refused(status)));
      await act(async () => { await view.client.invalidateQueries({ queryKey: ['projects', orgA] }); });
      await settleTask();
      expect(view.client.getQueryState(['projects', orgA])?.status).toBe('error');
      // The UI must suppress denied metadata whether the query retains its
      // old value or the shared metadata-query owner retires it.
      expect(view.container.textContent).not.toContain('Billing');
      expect(view.container.textContent).not.toContain('Ledger');
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="alert"]')).not.toBeNull();
      }
      fetchMock.mockImplementation(() => Promise.resolve(listed([first])));
      const retrySurface = shell ? region(view.container, '#content') : view.container;
      await act(async () => button(retrySurface, 'Retry projects').click());
      await settleTask();
      expect(view.router.state.location.pathname).toBe(`/orgs/${orgA}/projects/${first.id}/matrix`);
    } finally { await view.close(); }
  });
  it('treats a retired cache marker as unavailable even when a cache write marked it successful', async () => {
    projectFetch(() => Promise.resolve(listed([first, second])));
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      // This is the legitimate cache marker written by metadataQueryOptions
      // before it rethrows denial, not a successful empty server response.
      await act(async () => { view.client.setQueryData(['projects', orgA], null); });
      await settleTask();
      expect(view.client.getQueryState(['projects', orgA])?.status).toBe('success');
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="alert"]')).not.toBeNull();
        expect(surface.textContent).not.toContain('No projects yet');
        expect(surface.textContent).not.toContain('Billing');
      }
      expect(view.router.state.location.pathname).toBe('/');
    } finally { await view.close(); }
  });

  it('preserves a newer successful read when a cancelled request later returns 403', async () => {
    const oldResponse = deferredResponse();
    // Deliberately ignore request cancellation at the fetch boundary: an old
    // response can already be decoding when a replacement read takes over.
    const fetchMock = projectFetch(() => oldResponse.promise);
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      expect(fetchMock).toHaveBeenCalledTimes(1);
      await act(async () => { await view.client.cancelQueries({ queryKey: ['projects', orgA], exact: true }); });
      fetchMock.mockImplementation(() => Promise.resolve(listed([first, second])));
      await act(async () => { await view.client.refetchQueries({ queryKey: ['projects', orgA], exact: true }); });
      await settleTask();
      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect(view.client.getQueryState(['projects', orgA])?.status).toBe('success');
      expect(view.container.textContent).toContain('Billing');
      await act(async () => oldResponse.resolve(refused(403)));
      await settleTask();
      // A manual retirement from the cancelled query must not overwrite the
      // replacement query's success or briefly claim a successful empty list.
      expect(view.client.getQueryState(['projects', orgA])?.status).toBe('success');
      expect(view.client.getQueryData(['projects', orgA])).toEqual({ items: [first, second], count: 2 });
      for (const surface of surfaces(view.container)) {
        expect(surface.textContent).toContain('Billing');
        expect(surface.textContent).toContain('Ledger');
        expect(surface.textContent).not.toContain('No projects yet');
        expect(surface.querySelector('[role="alert"]')).toBeNull();
      }
      expect(view.router.state.location.pathname).toBe('/');
    } finally { await view.close(); }
  });

  it('keeps denied metadata hidden through a failed network retry until an authoritative read succeeds', async () => {
    const fetchMock = projectFetch(() => Promise.resolve(listed([first, second])));
    const view = await mountDirectory({ shell });
    try {
      await settleTask();
      expect(view.container.textContent).toContain('Billing');
      fetchMock.mockImplementation(() => Promise.resolve(refused(403)));
      await act(async () => { await view.client.invalidateQueries({ queryKey: ['projects', orgA] }); });
      await settleTask();
      expect(view.client.getQueryState(['projects', orgA])?.status).toBe('error');
      expect(view.client.getQueryData(['projects', orgA])).toBeNull();
      expect(view.container.textContent).not.toContain('Billing');
      fetchMock.mockImplementation(() => Promise.reject(new TypeError('Network unavailable')));
      const content = shell ? region(view.container, '#content') : view.container;
      await act(async () => button(content, 'Retry projects').click());
      await settleTask();
      expect(view.client.getQueryData(['projects', orgA])).toBeNull();
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="alert"]')).not.toBeNull();
        expect(surface.textContent).not.toContain('No projects yet');
      }
      expect(view.container.textContent).not.toContain('Billing');
      expect(view.container.textContent).not.toContain('Ledger');
      expect(view.router.state.location.pathname).toBe('/');
      fetchMock.mockImplementation(() => Promise.resolve(listed([])));
      await act(async () => button(content, 'Retry projects').click());
      await settleTask();
      for (const surface of surfaces(view.container)) {
        expect(surface.querySelector('[role="alert"]')).toBeNull();
        expect(surface.textContent).toContain('No projects yet');
      }
    } finally { await view.close(); }
  });
});

it('does not describe the disabled no-organisation query as loading or empty', async () => {
  const fetchMock = projectFetch(() => Promise.resolve(listed([])));
  const view = await mountDirectory({ org: '' });
  try {
    await settleTask();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(view.container.querySelector('[role="status"]')?.textContent).toContain('organisation');
    expect(view.container.textContent).not.toContain('Loading projects');
    expect(view.container.textContent).not.toContain('No projects yet');
  } finally { await view.close(); }
});

it('does not redirect from a retained single-project value after its refetch was denied', async () => {
  const fetchMock = projectFetch(() => Promise.resolve(listed([first])));
  const view = await mountDirectory();
  try {
    await settleTask();
    fetchMock.mockImplementation(() => Promise.resolve(refused(403)));
    await act(async () => { await view.client.refetchQueries({ queryKey: ['projects', orgA], type: 'all' }); });
    await settleTask();
    expect(view.client.getQueryState(['projects', orgA])?.status).toBe('error');
    await act(async () => { await view.router.navigate('/'); });
    await settleTask();
    expect(view.router.state.location.pathname).toBe('/');
    expect(view.container.querySelector('[role="alert"]')?.textContent).toContain('Projects could not be loaded');
  } finally { await view.close(); }
});

it('keeps a late old-organisation response out of the newly selected organisation', async () => {
  const oldResponse = deferredResponse();
  const newResponse = deferredResponse();
  const fetchMock = projectFetch((org) => org === orgA ? oldResponse.promise : newResponse.promise);
  const view = await mountDirectory({ shell: true });
  try {
    await settleTask();
    const rail = region(view.container, '.rail');
    await act(async () => button(rail, 'Organisation Beta').click());
    await settleTask();
    expect(fetchMock).toHaveBeenCalledTimes(2);
    await act(async () => oldResponse.resolve(listed([first])));
    await settleTask();
    expect(view.container.textContent).not.toContain('Billing');
    expect(view.router.state.location.pathname).toBe('/');
    await act(async () => newResponse.resolve(listed([])));
    await settleTask();
    expect(region(view.container, '#content').textContent).toContain('No projects yet');
    expect(view.container.textContent).not.toContain('Billing');
  } finally { await view.close(); }
});

it('shows an actionable failure in the mobile project switcher on a project route', async () => {
  projectFetch(() => Promise.resolve(refused()));
  const view = await mountDirectory({ shell: true, path: `/orgs/${orgA}/projects/${first.id}/matrix` });
  try {
    await settleTask();
    const switcher = region(view.container, '.sidebar__mobile-projects');
    expect(switcher.querySelector('[role="alert"]')?.textContent).toContain('Projects could not be loaded');
    expect(button(switcher, 'Retry projects').disabled).toBe(false);
    expect(switcher.textContent).not.toContain('No projects yet');
  } finally { await view.close(); }
});

it('drops the previous organisation list while the next organisation fails and retries', async () => {
  const nextResponse = deferredResponse();
  const fetchMock = projectFetch((org) => org === orgA ? Promise.resolve(listed([first, second])) : nextResponse.promise);
  const view = await mountDirectory({ shell: true });
  try {
    await settleTask();
    expect(view.container.textContent).toContain('Billing');
    await act(async () => button(region(view.container, '.rail'), 'Organisation Beta').click());
    await settleTask();
    expect(view.container.textContent).not.toContain('Billing');
    expect(view.container.textContent).not.toContain('Ledger');
    expect(region(view.container, '#content').textContent).toContain('Loading projects');
    await act(async () => nextResponse.resolve(refused()));
    await settleTask();
    expect(view.client.getQueryState(['projects', orgB])?.status).toBe('error');
    expect(view.client.getQueryState(['projects', orgA])?.status).toBe('success');
    fetchMock.mockImplementation((input) => {
      const path = new URL(input instanceof Request ? input.url : String(input), 'http://localhost').pathname;
      expect(path).toBe(`/api/v1/orgs/${orgB}/projects`);
      return Promise.resolve(listed([]));
    });
    await act(async () => button(region(view.container, '#content'), 'Retry projects').click());
    await settleTask();
    expect(region(view.container, '#content').textContent).toContain('No projects yet');
    expect(view.container.textContent).not.toContain('Billing');
  } finally { await view.close(); }
});

it('does not send a local directory request from remote project chrome', async () => {
  const fetchMock = projectFetch(() => Promise.resolve(refused()));
  const view = await mountDirectory({ shell: true, path: `/orgs/${orgA}/projects/${first.id}/matrix?remote=remote-example` });
  try {
    await settleTask();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(view.container.textContent).not.toContain('Loading projects');
    expect(view.container.textContent).not.toContain('No projects yet');
    expect(view.container.textContent).not.toContain('Retry projects');
  } finally { await view.close(); }
});

// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeEach, expect, it, vi } from 'vitest';

import { authenticatedIdentity } from '../testkit/identity.ts';
import type { WhoAmI } from './AuthProvider.tsx';
import { App } from './App.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const auth = vi.hoisted((): { identity: WhoAmI | null } => ({ identity: null }));

vi.mock('./AuthProvider.tsx', () => ({
  useAuth: () => ({
    failure: null,
    identity: auth.identity,
    state: auth.identity === null ? { status: 'anonymous' } : { status: 'authenticated' },
  }),
}));
vi.mock('./route-groups/auth.ts', () => ({
  EnrolmentGate: () => <p>enrolment gate</p>,
  Login: () => <p>sign-in form</p>,
}));
vi.mock('../routes/Shell.tsx', () => ({ Shell: () => <p>application shell</p> }));

beforeEach(() => {
  auth.identity = null;
});

async function renderAt(path: string) {
  globalThis.history.pushState(null, '', path);
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => root.render(<App />));
  // The route groups are lazy: let the import and Suspense settle.
  for (let round = 0; round < 10; round += 1) await act(async () => Promise.resolve());
  return {
    container,
    refresh: async () => { await act(async () => root.render(<App />)); },
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
}

it('confines a gated session to the enrolment gate on /login, whatever path it asked for', async () => {
  auth.identity = { ...authenticatedIdentity, enrolment_required: true };
  const view = await renderAt('/projects');
  expect(globalThis.location.pathname).toBe('/login');
  expect(view.container.textContent).toContain('enrolment gate');
  expect(view.container.textContent).not.toContain('application shell');
  await view.unmount();
});

const destination = '/orgs/org_test/projects/prj_test/machine-access?filter=a%2Fb&tab=members#grant';

it('retains the complete protected URL through sign-in', async () => {
  const view = await renderAt(destination);
  expect(globalThis.location.pathname).toBe('/login');
  expect(new URLSearchParams(globalThis.location.search).get('returnTo')).toBe(destination);
  expect(view.container.textContent).toContain('sign-in form');

  auth.identity = authenticatedIdentity;
  await view.refresh();
  expect(globalThis.location.pathname + globalThis.location.search + globalThis.location.hash).toBe(destination);
  expect(view.container.textContent).toContain('application shell');
  await view.unmount();
});

it('retains the destination until mandatory factor enrolment completes', async () => {
  auth.identity = { ...authenticatedIdentity, enrolment_required: true };
  const view = await renderAt(destination);
  expect(new URLSearchParams(globalThis.location.search).get('returnTo')).toBe(destination);
  expect(view.container.textContent).toContain('enrolment gate');

  auth.identity = { ...authenticatedIdentity, enrolment_required: false };
  await view.refresh();
  expect(globalThis.location.pathname + globalThis.location.search + globalThis.location.hash).toBe(destination);
  expect(view.container.textContent).toContain('application shell');
  await view.unmount();
});

it.each(['/login', '/signup'])('uses the requested destination for an authenticated %s visit', async (door) => {
  auth.identity = authenticatedIdentity;
  const view = await renderAt(`${door}?${new URLSearchParams({ returnTo: destination })}`);
  expect(globalThis.location.pathname + globalThis.location.search + globalThis.location.hash).toBe(destination);
  await view.unmount();
});

it.each([
  'https://other.example/projects', '//other.example/projects', '/\\other.example/projects',
  '/.//other.example/projects', '/login', '/LOGIN/', '/%6cogin', '/signup?returnTo=/projects', '/%',
])('falls back to projects for an unsafe or looping return: %s', async (returnTo) => {
  auth.identity = authenticatedIdentity;
  const view = await renderAt(`/login?${new URLSearchParams({ returnTo })}`);
  expect(globalThis.location.pathname).toBe('/projects');
  await view.unmount();
});

it('sends an ungated session from /login into the application', async () => {
  auth.identity = { ...authenticatedIdentity, enrolment_required: false };
  const view = await renderAt('/login');
  expect(globalThis.location.pathname).not.toBe('/login');
  expect(view.container.textContent).toContain('application shell');
  expect(view.container.textContent).not.toContain('enrolment gate');
  await view.unmount();
});

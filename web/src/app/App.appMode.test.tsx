// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { authenticatedIdentity } from '../testkit/identity.ts';
import type { WhoAmI } from './AuthProvider.tsx';
import { App } from './App.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const auth = vi.hoisted((): { identity: WhoAmI | null } => ({ identity: null }));
const shell = vi.hoisted(() => ({ signIn: vi.fn(), app: true, reasons: new Set<() => void>(), reason: null as string | null }));

vi.mock('./AuthProvider.tsx', () => ({
  useAuth: () => ({
    failure: null,
    identity: auth.identity,
    state: auth.identity === null ? { status: 'anonymous' } : { status: 'authenticated' },
  }),
}));
vi.mock('./shell.ts', async (importActual) => ({
  ...(await importActual<typeof import('./shell.ts')>()),
  isAppMode: () => shell.app,
  shellSignIn: shell.signIn,
}));
vi.mock('./route-groups/auth.ts', () => ({
  EnrolmentGate: () => <p>enrolment gate</p>,
  Login: () => <p>sign-in form</p>,
}));
vi.mock('../routes/Shell.tsx', () => ({ Shell: () => <p>application shell</p> }));

const reload = vi.fn();
beforeEach(() => {
  auth.identity = null;
  shell.app = true;
  shell.signIn.mockReset().mockResolvedValue(undefined);
  reload.mockReset();
  vi.spyOn(globalThis.location, 'reload').mockImplementation(reload);
});
afterEach(() => vi.restoreAllMocks());

async function renderAt(path: string) {
  globalThis.history.pushState(null, '', path);
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => root.render(<App />));
  for (let round = 0; round < 10; round += 1) await act(async () => Promise.resolve());
  return { container, unmount: async () => { await act(async () => root.unmount()); container.remove(); } };
}

it('asks the shell for a session instead of routing a dead session to /login, then reloads the route', async () => {
  const view = await renderAt('/orgs/org_test/projects?tab=a');
  expect(shell.signIn).toHaveBeenCalledTimes(1);
  expect(globalThis.location.pathname + globalThis.location.search).toBe('/orgs/org_test/projects?tab=a');
  expect(view.container.textContent).not.toContain('sign-in form');
  expect(reload).toHaveBeenCalledOnce();
  await view.unmount();
});

it('never runs the login form in the app, even on a public sign-in path', async () => {
  const view = await renderAt('/login');
  expect(view.container.textContent).not.toContain('sign-in form');
  expect(shell.signIn).toHaveBeenCalledTimes(1);
  await view.unmount();
});

it('offers a retry when the sign-in was cancelled, without reloading', async () => {
  shell.signIn.mockRejectedValue(new Error('cancelled'));
  const view = await renderAt('/projects');
  expect(view.container.textContent).toContain('did not finish');
  expect(reload).not.toHaveBeenCalled();
  expect([...view.container.querySelectorAll('button')].some((button) => button.textContent === 'Try again')).toBe(true);
  await view.unmount();
});

it('sends a gated session to the bare /login the shell can open, with no returnTo', async () => {
  auth.identity = { ...authenticatedIdentity, enrolment_required: true };
  const view = await renderAt('/orgs/org_test/projects?tab=a#x');
  expect(globalThis.location.pathname).toBe('/login');
  expect(globalThis.location.search).toBe('');
  expect(view.container.textContent).toContain('enrolment gate');
  await view.unmount();
});

it('is unchanged in a browser: the sign-in form renders and returnTo is kept', async () => {
  shell.app = false;
  const view = await renderAt('/orgs/org_test/projects?tab=a');
  expect(shell.signIn).not.toHaveBeenCalled();
  expect(globalThis.location.pathname).toBe('/login');
  expect(new URLSearchParams(globalThis.location.search).get('returnTo')).toBe('/orgs/org_test/projects?tab=a');
  expect(view.container.textContent).toContain('sign-in form');
  await view.unmount();
});

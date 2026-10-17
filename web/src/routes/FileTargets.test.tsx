// @vitest-environment happy-dom
import { QueryClient, useQueryClient } from '@tanstack/react-query';
import { act, useState } from 'react';
import { Link, MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { AuthProvider, useAuth, type WhoAmI } from '../app/AuthProvider.tsx';
import { makeQueryClient } from '../app/queryClient.ts';
import { Adapters } from './Adapters.tsx';
import { FileTargets, fileReportAge } from './FileTargets.tsx';

const ref = { org: 'acme', project: 'app' };
const base = '/api/v1/orgs/acme/projects/app';
const id = 'ftg_00000000-0000-0000-0000-000000000001';
const env = 'env_00000000-0000-0000-0000-000000000001';
const sa = 'sa_00000000-0000-0000-0000-000000000001';
const key = 'key_00000000-0000-0000-0000-000000000001';
const at = '2026-01-01T00:00:00Z';
const target = {
  id, environment_id: env, service_account_id: sa, principal_id: 'mch_00000000-0000-0000-0000-000000000001',
  name: 'web-host', generation: 4, created_at: at, updated_at: at,
  keys: [{ key_id: key, name: 'DATABASE_URL', classification: 'config' }], report: null,
};
function Harness({ selected = '', project = 'app' }: { selected?: string; project?: string }) {
  const [chosen, setChosen] = useState(selected);
  return <MemoryRouter><main><FileTargets key={project} refData={{ ...ref, project }} selected={chosen} onSelect={setChosen} panelAvailable /></main></MemoryRouter>;
}
function installFetch(overrides: (request: Request) => Response | Promise<Response> | undefined = () => undefined) {
  const requests: Request[] = [];
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    requests.push(request.clone());
    const override = overrides(request);
    if (override !== undefined) return Promise.resolve(override);
    const path = new URL(request.url).pathname;
    if (request.method !== 'GET') throw new Error(`Unexpected ${request.method} ${path}`);
    if (path.endsWith('/adapters')) return Promise.resolve(Response.json({ items: [] }));
    if (path.endsWith('/file-targets')) return Promise.resolve(Response.json({ items: [target] }));
    if (path.endsWith(`/file-targets/${id}`)) return Promise.resolve(Response.json(target));
    if (path.endsWith('/environments')) return Promise.resolve(Response.json({ items: [{ id: env, org_id: 'org_00000000-0000-0000-0000-000000000001', project_id: 'prj_00000000-0000-0000-0000-000000000001', name: 'production', display_order: 0, created_at: at }], count: 1 }));
    if (path.endsWith('/service-accounts')) return Promise.resolve(Response.json({ items: [
      { id: sa, principal_id: target.principal_id, name: 'web-workload', kind: 'workload', created_at: at, created_by: target.principal_id, live_credentials: 1 },
      { id: 'sa_00000000-0000-0000-0000-000000000002', principal_id: target.principal_id, name: 'automation-only', kind: 'automation', created_at: at, created_by: target.principal_id, live_credentials: 0 },
    ], count: 2 }));
    if (path.endsWith('/keys')) return Promise.resolve(Response.json({ items: [], count: 0, schema_revision: 1 }));
    throw new Error(`Unexpected ${path}`);
  }));
  return requests;
}
function button(container: HTMLElement, name: string) {
  const found = [...container.querySelectorAll('button')].find((item) => item.textContent === name);
  if (found === undefined) throw new Error(`Missing button ${name}`);
  return found;
}
function field(container: HTMLElement, label: string) {
  const found = [...container.querySelectorAll('label')].find((item) => item.textContent?.startsWith(label));
  if (found === undefined) throw new Error(`Missing field ${label}`);
  const input = found.querySelector('input,select') ?? container.querySelector(`#${CSS.escape(found.htmlFor)}`);
  if (!(input instanceof HTMLInputElement) && !(input instanceof HTMLSelectElement)) throw new Error(`Missing control ${label}`);
  return input;
}
async function fill(container: HTMLElement, label: string, value: string) {
  const input = field(container, label);
  await act(async () => {
    if (input instanceof HTMLInputElement) typeInto(input, value);
    else {
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set?.call(input, value);
      input.dispatchEvent(new Event('change', { bubbles: true }));
    }
  });
}
afterEach(() => vi.unstubAllGlobals());

it('lists resolved selection and missing reports without asserting health', async () => {
  installFetch();
  const view = await renderForm(<Harness />);
  try {
    await settleTask();
    expect(view.container.textContent).toContain('production');
    expect(view.container.textContent).toContain('web-workload');
    expect(view.container.textContent).toContain('DATABASE_URL');
    expect(view.container.textContent).toContain('No client report. Host health is unknown.');
    expect(view.container.querySelector('.badge--ok')).toBeNull();
  } finally { await view.unmount(); }
});

it.each(['current', 'applied', 'offline', 'refused', 'failed'])('keeps old %s reports visibly historical, with revision, age and generation mismatch', async (state) => {
  installFetch((request) => new URL(request.url).pathname.endsWith('/file-targets') ? Response.json({ items: [{ ...target,
    report: { state, revision: 17, generation: 3, stamp: '', reported_at: at, received_at: at },
  }] }) : undefined);
  const view = await renderForm(<Harness />);
  try {
    await settleTask();
    expect(view.container.textContent).toContain(`Client reported ${state}`);
    expect(view.container.textContent).toContain('17');
    expect(view.container.textContent).toContain('Report age');
    expect(view.container.textContent).toContain('different target generation');
    expect(view.container.textContent).toContain('Current host health is not verified');
    expect(view.container.querySelector('.badge--ok')).toBeNull();
  } finally { await view.unmount(); }
});

it('creates with explicit workload and environment choices and the generated selection shape', async () => {
  const requests = installFetch((request) => request.method === 'POST' ? Response.json(target, { status: 201 }) : undefined);
  const view = await renderForm(<Harness selected="new" />);
  try {
    await settleTask();
    expect(field(view.container, 'Workload service account').textContent).not.toContain('automation-only');
    expect(field(view.container, 'Workload service account').value).toBe('');
    expect(field(view.container, 'Environment').value).toBe('');
    await fill(view.container, 'Target name', 'web-host');
    await fill(view.container, 'Environment', env);
    await fill(view.container, 'Workload service account', sa);
    await fill(view.container, 'Explicit names', 'DATABASE_URL');
    await fill(view.container, 'Include patterns', 'APP_*');
    await fill(view.container, 'Exclude patterns', '*_TEST');
    await fill(view.container, 'Classification', 'config');
    await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await settleTask();
    const sent = requests.find((request) => request.method === 'POST');
    expect(await sent?.text()).toBe(JSON.stringify({ name: 'web-host', environment_id: env, service_account_id: sa, key_ids: [], key_selection: { names: ['DATABASE_URL'], include: ['APP_*'], exclude: ['*_TEST'], classification: 'config' } }));
    expect(new URL(sent?.url ?? '').pathname).toBe(`${base}/file-targets`);
    expect(view.container.textContent).toContain('File target created');
    expect(requests.some((request) => new URL(request.url).pathname === `${base}/file-targets/${id}`)).toBe(true);
  } finally { await view.unmount(); }
});

it('keeps the edit generation pinned across a conflict and requires explicit reload before retry', async () => {
  let generation = 4;
  const requests = installFetch((request) => {
    if (request.method === 'PATCH') { generation = 5; return Response.json({ error: { code: 'conflict', message: 'changed' } }, { status: 409 }); }
    if (new URL(request.url).pathname.endsWith(`/file-targets/${id}`)) return Response.json({ ...target, generation });
    return undefined;
  });
  const view = await renderForm(<Harness selected={id} />);
  try {
    await settleTask();
    await act(async () => button(view.container, 'Edit key selection').click());
    await fill(view.container, 'Explicit names', 'NEW_NAME');
    await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await settleTask();
    const sent = requests.find((request) => request.method === 'PATCH');
    expect(await sent?.text()).toContain('"expected_generation":4');
    expect(field(view.container, 'Explicit names').value).toBe('NEW_NAME');
    expect(button(view.container, 'Save selection').disabled).toBe(true);
    expect(view.container.textContent).toContain('Your selection has not been saved');
    expect(requests.filter((request) => request.method === 'PATCH')).toHaveLength(1);
    await act(async () => button(view.container, 'Cancel').click());
    await act(async () => button(view.container, 'Refresh report').click());
    await settleTask();
    await act(async () => button(view.container, 'Edit key selection').click());
    expect(view.container.textContent).toContain('Replacing selection at generation 5');
  } finally { await view.unmount(); }
});

it('shows live-credential refusal inside the delete dialog, then removes the target on success', async () => {
  let reject = true;
  installFetch((request) => request.method === 'DELETE' ? reject
    ? Response.json({ error: { code: 'conflict', message: 'live credentials' } }, { status: 409 })
    : new Response(null, { status: 204 }) : undefined);
  const view = await renderForm(<Harness selected={id} />);
  try {
    await settleTask();
    await act(async () => button(view.container, 'Delete file target').click());
    await act(async () => button(view.container, 'Confirm delete').click());
    await settleTask();
    const dialog = view.container.querySelector('dialog');
    expect(dialog?.querySelector('[role="alert"]')?.textContent).toContain('still holds live credentials');
    expect(dialog?.textContent).toContain('widen');
    expect(view.container.textContent).not.toContain('File target deleted.');
    reject = false;
    await act(async () => button(view.container, 'Confirm delete').click());
    await settleTask();
    expect(view.container.querySelector('dialog')).toBeNull();
    expect(view.container.textContent).toContain('File target deleted. Local files were not changed.');
  } finally { await view.unmount(); }
});

it('does not display previous project data after changing projects', async () => {
  installFetch((request) => new URL(request.url).pathname.includes('/projects/other/') ? Response.json({ error: { code: 'not_found', message: 'Not found' } }, { status: 404 }) : undefined);
  const view = await renderForm(<Harness />);
  try {
    await settleTask();
    expect(view.container.textContent).toContain('DATABASE_URL');
    await view.rerender(<Harness project="other" />);
    await settleTask();
    expect(view.container.textContent).not.toContain('DATABASE_URL');
    expect(view.container.textContent).not.toContain('web-workload');
    expect(view.container.textContent).toContain('no longer available in this project');
  } finally { await view.unmount(); }
});

it('renders age without inventing a freshness threshold', () => {
  const now = Date.parse(at);
  expect(fileReportAge(at, now + 65_000)).toBe('1m ago');
  expect(fileReportAge(at, now + 3_600_000)).toBe('1h ago');
  expect(fileReportAge(at, now - 1_000)).toBe('Client clock is ahead');
});

it('keeps a pending creation mounted through panel dismissal and refreshes the committed list', async () => {
  let complete: ((response: Response) => void) | undefined;
  let created = false;
  installFetch((request) => {
    if (request.method === 'POST') return new Promise<Response>((resolve) => { complete = resolve; });
    if (new URL(request.url).pathname.endsWith('/file-targets')) return Response.json({ items: created ? [target] : [] });
    return undefined;
  });
  const view = await renderForm(<Harness selected="new" />);
  try {
    await settleTask();
    await fill(view.container, 'Target name', 'web-host');
    await fill(view.container, 'Environment', env);
    await fill(view.container, 'Workload service account', sa);
    await fill(view.container, 'Explicit names', 'DATABASE_URL');
    await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await settleTask();
    const close = [...view.container.querySelectorAll('button')].find((item) => item.textContent?.trim() === 'Close');
    if (!(close instanceof HTMLButtonElement)) throw new Error('Missing close panel button');
    await act(async () => close.click());
    const discard = [...view.container.querySelectorAll('button')].find((item) => item.textContent === 'Discard');
    if (discard !== undefined) await act(async () => discard.click());
    expect(view.container.querySelector('form')).not.toBeNull();
    expect(view.container.textContent).toContain('Saving…');
    created = true;
    await act(async () => complete?.(Response.json(target, { status: 201 })));
    await settleTask();
    expect(view.container.textContent).toContain('File target created');
    expect(view.container.querySelector('article')?.textContent).toContain('web-host');
  } finally { await view.unmount(); }
});

function ActualParent({ selected = '' }: { selected?: string }) {
  const path = `/orgs/acme/projects/app/adapters`;
  return <MemoryRouter initialEntries={[`${path}${selected === '' ? '' : `?file-target=${selected}`}`]}>
    <nav><Link to="/elsewhere">Leave adapters</Link><Link to={path}>Return to adapters</Link></nav>
    <main><Routes>
      <Route path="/orgs/:org/projects/:project/adapters" element={<Adapters />} />
      <Route path="/elsewhere" element={<p>Another route</p>} />
    </Routes></main>
  </MemoryRouter>;
}

it.each([403, 404])('actual Adapters parent retires denied metadata and editing controls after %i refetch', async (status) => {
  let denied = false;
  installFetch((request) => denied && new URL(request.url).pathname.includes('/file-targets')
    ? Response.json({ error: { code: status === 403 ? 'forbidden' : 'not_found', message: 'Denied' } }, { status }) : undefined);
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    await act(async () => button(view.container, 'Edit key selection').click());
    expect(view.container.textContent).toContain('DATABASE_URL');
    denied = true;
    await act(async () => { await view.client.invalidateQueries({ predicate: (query) => String(query.queryKey[0]).startsWith('file-target') }); });
    await settleTask();
    expect(view.container.textContent).not.toContain('DATABASE_URL');
    expect(view.container.textContent).not.toContain('web-host');
    expect(view.container.querySelector('form')).toBeNull();
    expect(view.container.textContent).not.toContain('Save selection');
  } finally { await view.unmount(); }
});

it.each([
  ['malformed success', () => Response.json({ invalid: true }), 'cannot understand'],
  ['unbound status', () => new Response(null, { status: 202 }), 'cannot understand'],
  ['server failure', () => Response.json({ error: { code: 'internal', message: 'Internal' } }, { status: 500 }), 'server error 500'],
  ['network failure', () => { throw new TypeError('Failed to fetch'); }, 'server could not be reached'],
])('actual Adapters parent distinguishes %s', async (_name, response, message) => {
  installFetch((request) => new URL(request.url).pathname.endsWith('/file-targets') ? response() : undefined);
  const view = await renderForm(<ActualParent />, { client: makeQueryClient() });
  try {
    await settleTask();
    expect(view.container.textContent).toContain(message);
  } finally { await view.unmount(); }
});

it('actual Adapters parent refreshes a committed create after leaving and returning inside staleTime', async () => {
  let complete: ((response: Response) => void) | undefined;
  let committed = false;
  const requests = installFetch((request) => {
    if (request.method === 'POST') return new Promise<Response>((resolve) => { complete = resolve; });
    if (new URL(request.url).pathname.endsWith('/file-targets')) return Response.json({ items: committed ? [target] : [] });
    return undefined;
  });
  const view = await renderForm(<ActualParent selected="new" />, { client: makeQueryClient() });
  try {
    await settleTask();
    await fill(view.container, 'Target name', 'web-host');
    await fill(view.container, 'Environment', env);
    await fill(view.container, 'Workload service account', sa);
    await fill(view.container, 'Explicit names', 'DATABASE_URL');
    await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    await settleTask();
    const leave = [...view.container.querySelectorAll('a')].find((item) => item.textContent === 'Leave adapters');
    await act(async () => leave?.click());
    expect(view.container.textContent).toContain('Another route');
    committed = true;
    await act(async () => complete?.(Response.json(target, { status: 201 })));
    await settleTask();
    expect(view.container.textContent).toContain('Another route');
    const back = [...view.container.querySelectorAll('a')].find((item) => item.textContent === 'Return to adapters');
    await act(async () => back?.click());
    await settleTask();
    expect(requests.filter((request) => new URL(request.url).pathname.endsWith('/file-targets') && request.method === 'GET').length).toBeGreaterThan(1);
    expect(view.container.querySelector('article')?.textContent).toContain('web-host');
    expect(view.container.querySelector('aside')).toBeNull();
  } finally { await view.unmount(); }
});

it.each([403, 404])('actual parent retires list metadata when a detail-only refresh discovers %i', async (status) => {
  let denied = false;
  installFetch((request) => denied && new URL(request.url).pathname.includes('/file-targets')
    ? Response.json({ error: { code: status === 403 ? 'forbidden' : 'not_found', message: 'Denied' } }, { status }) : undefined);
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    await act(async () => button(view.container, 'Edit key selection').click());
    denied = true;
    await act(async () => { await view.client.invalidateQueries({ queryKey: ['file-target', 'acme', 'app', id], exact: true }); });
    await settleTask();
    expect(view.container.textContent).not.toContain('DATABASE_URL');
    expect(view.container.textContent).not.toContain('web-host');
    expect(view.container.querySelector('form')).toBeNull();
  } finally { await view.unmount(); }
});

it('keeps explicitly marked last-loaded metadata on a temporary network refresh failure', async () => {
  let disconnected = false;
  installFetch((request) => {
    if (disconnected && new URL(request.url).pathname.includes('/file-targets')) throw new TypeError('Failed to fetch');
    return undefined;
  });
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    disconnected = true;
    await act(async () => { await view.client.invalidateQueries({ predicate: (query) => String(query.queryKey[0]).startsWith('file-target') }); });
    await settleTask();
    expect(view.container.textContent).toContain('DATABASE_URL');
    expect(view.container.textContent).toContain('Showing the last loaded file targets. Refresh failed.');
    expect(view.container.textContent).toContain('Showing the last loaded file target. Refresh failed.');
    expect(view.container.textContent).toContain('server could not be reached');
    expect(button(view.container, 'Edit key selection').disabled).toBe(true);
    expect(button(view.container, 'Delete file target').disabled).toBe(true);
  } finally { await view.unmount(); }
});


async function navigate(container: HTMLElement, name: string) {
  const link = [...container.querySelectorAll('a')].find((item) => item.textContent === name);
  if (link === undefined) throw new Error(`Missing navigation ${name}`);
  await act(async () => link.click());
  await settleTask();
}
async function submitCreation(container: HTMLElement) {
  await fill(container, 'Target name', 'web-host');
  await fill(container, 'Environment', env);
  await fill(container, 'Workload service account', sa);
  await fill(container, 'Explicit names', 'DATABASE_URL');
  await act(async () => container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  await settleTask();
}

it.each([401, 403, 404])('R2 actual parent preserves aggregate %i denial when a later detail refusal prunes its cache', async (status) => {
  let phase: 'loaded' | 'refetch' | 'offline' = 'loaded';
  let denyList: ((response: Response) => void) | undefined;
  let denyDetail: ((response: Response) => void) | undefined;
  installFetch((request) => {
    const path = new URL(request.url).pathname;
    if (!path.includes('/file-targets')) return undefined;
    if (phase === 'offline') throw new TypeError('Failed to fetch');
    if (phase === 'refetch') return new Promise<Response>((resolve) => {
      if (path.endsWith('/file-targets')) denyList = resolve;
      else denyDetail = resolve;
    });
    if (path.endsWith('/file-targets')) return Response.json({ items: [target, { ...target,
      id: 'ftg_00000000-0000-0000-0000-000000000002', name: 'other-private-host',
    }] });
    return undefined;
  });
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    expect(view.container.textContent).toContain('other-private-host');
    phase = 'refetch';
    await act(async () => {
      void view.client.invalidateQueries({ queryKey: ['file-targets', 'acme', 'app'], exact: true });
      void view.client.invalidateQueries({ queryKey: ['file-target', 'acme', 'app', id], exact: true });
    });
    expect(denyList).toBeDefined();
    expect(denyDetail).toBeDefined();
    await act(async () => denyList?.(Response.json({ error: { code: 'forbidden', message: 'Denied' } }, { status })));
    await settleTask();
    expect(view.container.textContent).not.toContain('other-private-host');
    phase = 'offline';
    await act(async () => denyDetail?.(Response.json({ error: { code: 'forbidden', message: 'Denied' } }, { status })));
    await settleTask();
    expect(view.container.textContent).not.toContain('other-private-host');
    expect(view.container.textContent).not.toContain('DATABASE_URL');
    expect(button(view.container, 'Add file target').disabled).toBe(true);
    phase = 'loaded';
    await act(async () => { await view.client.invalidateQueries({ queryKey: ['file-targets', 'acme', 'app'], exact: true }); });
    await settleTask();
    expect(view.container.textContent).toContain('other-private-host');
    expect(button(view.container, 'Add file target').disabled).toBe(false);
  } finally { await view.unmount(); }
});

it.each([401, 403, 404])('R2 actual parent never resurrects detail metadata denied with %i on a later network failure', async (status) => {
  let phase: 'loaded' | 'denied' | 'offline' = 'loaded';
  installFetch((request) => {
    if (!new URL(request.url).pathname.endsWith(`/file-targets/${id}`)) return undefined;
    if (phase === 'denied') return Response.json({ error: { code: 'forbidden', message: 'Denied' } }, { status });
    if (phase === 'offline') throw new TypeError('Failed to fetch');
    return undefined;
  });
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    phase = 'denied';
    await act(async () => { await view.client.invalidateQueries({ queryKey: ['file-target', 'acme', 'app', id], exact: true }); });
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).not.toContain('DATABASE_URL');
    phase = 'offline';
    await act(async () => button(view.container, 'Refresh report').click());
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).not.toContain('DATABASE_URL');
    expect(view.container.querySelector('aside')?.textContent).not.toContain('Edit key selection');
    phase = 'loaded';
    await act(async () => button(view.container, 'Refresh report').click());
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).toContain('DATABASE_URL');
    expect(button(view.container, 'Edit key selection').disabled).toBe(false);
  } finally { await view.unmount(); }
});

it.each(['lost response', 'malformed success'])('R2 actual parent reconciles a committed create with %s after navigation', async (outcome) => {
  let finish: (() => void) | undefined;
  let committed = false;
  installFetch((request) => {
    if (request.method === 'POST') return new Promise<Response>((resolve, reject) => { finish = () => {
      committed = true;
      if (outcome === 'lost response') reject(new TypeError('Failed to fetch'));
      else resolve(Response.json({ invalid: true }, { status: 201 }));
    }; });
    if (new URL(request.url).pathname.endsWith('/file-targets')) return Response.json({ items: committed ? [target] : [] });
    return undefined;
  });
  const view = await renderForm(<ActualParent selected="new" />, { client: makeQueryClient() });
  try {
    await settleTask();
    await submitCreation(view.container);
    await navigate(view.container, 'Leave adapters');
    await act(async () => finish?.());
    await settleTask();
    expect(view.container.textContent).toContain('Another route');
    await navigate(view.container, 'Return to adapters');
    expect(view.container.querySelector('article')?.textContent).toContain('web-host');
    expect(view.container.querySelector('aside')).toBeNull();
  } finally { await view.unmount(); }
});

it('R2 actual parent gives a replacement create panel its own idle dismissal state', async () => {
  let finish: ((response: Response) => void) | undefined;
  installFetch((request) => request.method === 'POST' ? new Promise<Response>((resolve) => { finish = resolve; }) : undefined);
  const view = await renderForm(<ActualParent selected="new" />, { client: makeQueryClient() });
  try {
    await settleTask();
    await submitCreation(view.container);
    await navigate(view.container, 'Return to adapters');
    await act(async () => button(view.container, 'Add file target').click());
    await settleTask();
    expect(field(view.container, 'Target name').value).toBe('');
    await act(async () => button(view.container, 'Cancel').click());
    await settleTask();
    expect(view.container.querySelector('aside')).toBeNull();
    await act(async () => finish?.(Response.json(target, { status: 201 })));
    await settleTask();
    expect(view.container.querySelector('aside')).toBeNull();
  } finally { await view.unmount(); }
});

it('R2 actual parent cannot let an old create completion release a replacement pending panel', async () => {
  const completions: Array<(response: Response) => void> = [];
  installFetch((request) => request.method === 'POST' ? new Promise<Response>((resolve) => { completions.push(resolve); }) : undefined);
  const view = await renderForm(<ActualParent selected="new" />, { client: makeQueryClient() });
  try {
    await settleTask();
    await submitCreation(view.container);
    await navigate(view.container, 'Return to adapters');
    await act(async () => button(view.container, 'Add file target').click());
    await settleTask();
    await submitCreation(view.container);
    expect(completions).toHaveLength(2);
    await act(async () => completions[0]?.(Response.json(target, { status: 201 })));
    await settleTask();
    const close = [...view.container.querySelectorAll('button')].find((item) => item.textContent?.trim() === 'Close');
    if (close === undefined) throw new Error('Missing close panel button');
    await act(async () => close.click());
    const discard = [...view.container.querySelectorAll('button')].find((item) => item.textContent === 'Discard');
    if (discard !== undefined) await act(async () => discard.click());
    await settleTask();
    expect(view.container.textContent).toContain('Saving…');
    expect(view.container.querySelector('form')).not.toBeNull();
    await act(async () => completions[1]?.(Response.json(target, { status: 201 })));
    await settleTask();
    expect(view.container.textContent).toContain('File target created');
  } finally { await view.unmount(); }
});


it.each(['update', 'delete'])('R2 actual parent reconciles an uncertain %s after navigation', async (operation) => {
  let finish: (() => void) | undefined;
  let committed = false;
  installFetch((request) => {
    if (request.method === 'PATCH' || request.method === 'DELETE') return new Promise<Response>((_resolve, reject) => { finish = () => {
      committed = true;
      reject(new TypeError('Failed to fetch'));
    }; });
    if (new URL(request.url).pathname.endsWith('/file-targets')) return Response.json({ items: committed
      ? operation === 'delete' ? [] : [{ ...target, generation: 5, keys: [{ key_id: key, name: 'NEW_NAME', classification: 'config' }] }]
      : [target] });
    return undefined;
  });
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    if (operation === 'update') {
      await act(async () => button(view.container, 'Edit key selection').click());
      await fill(view.container, 'Explicit names', 'NEW_NAME');
      await act(async () => view.container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    } else {
      await act(async () => button(view.container, 'Delete file target').click());
      await act(async () => button(view.container, 'Confirm delete').click());
    }
    await settleTask();
    await navigate(view.container, 'Leave adapters');
    await act(async () => finish?.());
    await settleTask();
    await navigate(view.container, 'Return to adapters');
    expect(view.container.querySelector('aside')).toBeNull();
    if (operation === 'delete') expect(view.container.querySelector('article')).toBeNull();
    else expect(view.container.querySelector('article')?.textContent).toContain('NEW_NAME');
  } finally { await view.unmount(); }
});

function browserIdentity(suffix: string): WhoAmI {
  return {
    session: { id: `ses_00000000-0000-0000-0000-0000000000${suffix}`, artifact: 'browser', created_at: at,
      idle_expires_at: '2099-01-01T00:00:00Z', absolute_expires_at: '2099-01-01T00:00:00Z',
      assurance: { method: 'local-password', factors: ['password'], authenticated_at: at } },
    principal: { id: `prn_00000000-0000-0000-0000-0000000000${suffix}`, kind: 'human', display_name: `Person ${suffix}` },
    capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } },
  };
}
function AuthenticatedParent({ observe }: { observe: (client: QueryClient) => void }) {
  const auth = useAuth();
  const queries = useQueryClient();
  observe(queries);
  return <><button type="button" onClick={() => void auth.revalidate()}>Revalidate session</button>
    {auth.state.status === 'authenticated' ? <ActualParent selected="new" /> : <p>Checking session</p>}
  </>;
}

it.each(['success', 'lost response'])('R2 actual AuthProvider replacement retires a pending create %s and its cache reconciliation', async (outcome) => {
  let replacement = false;
  let finish: (() => void) | undefined;
  const requests = installFetch((request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith('/auth/whoami')) return Response.json(browserIdentity(replacement ? '02' : '01'));
    if (request.method === 'POST') return new Promise<Response>((resolve, reject) => { finish = () => {
      if (outcome === 'lost response') reject(new TypeError('Failed to fetch'));
      else resolve(Response.json(target, { status: 201 }));
    }; });
    if (path.endsWith('/file-targets')) return Response.json({ items: [] });
    return undefined;
  });
  const observed: QueryClient[] = [];
  const view = await renderForm(<AuthProvider><AuthenticatedParent observe={(client) => {
    if (!observed.includes(client)) observed.push(client);
  }} /></AuthProvider>);
  try {
    await settleTask();
    await submitCreation(view.container);
    const outgoing = observed.at(-1);
    replacement = true;
    await act(async () => button(view.container, 'Revalidate session').click());
    await settleTask();
    expect(observed.at(-1)).not.toBe(outgoing);
    expect(outgoing?.getQueryCache().getAll()).toHaveLength(0);
    expect(field(view.container, 'Target name').value).toBe('');
    const reads = requests.filter((request) => request.method === 'GET' && new URL(request.url).pathname.endsWith('/file-targets')).length;
    await act(async () => finish?.());
    await settleTask();
    expect(requests.filter((request) => request.method === 'GET' && new URL(request.url).pathname.endsWith('/file-targets'))).toHaveLength(reads);
    expect(outgoing?.getQueryCache().getAll()).toHaveLength(0);
    expect(view.container.textContent).not.toContain('File target created');
    expect(field(view.container, 'Target name').value).toBe('');
    await act(async () => button(view.container, 'Cancel').click());
    await settleTask();
    expect(view.container.querySelector('aside')).toBeNull();
  } finally { await view.unmount(); }
});

it.each([
  ['file', 'service-accounts', 'web-workload'],
  ['file', 'environments', 'production'],
  ['file', 'keys', 'UNSELECTED_PRIVATE_KEY'],
  ['push', 'environments', 'production'],
  ['push', 'keys', 'UNSELECTED_PRIVATE_KEY'],
].flatMap(([surface, endpoint, label]) => [401, 403, 404].map((status) => ({ surface, endpoint, label: label ?? '', status }))))('R3 actual parent retires $surface form $endpoint metadata after $status until an authoritative successful read', async ({ surface, endpoint, label, status }) => {
  let phase: 'loaded' | 'denied' | 'offline' = 'loaded';
  installFetch((request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith(`/${endpoint}`)) {
      if (phase === 'denied') return Response.json({ error: { code: 'forbidden', message: 'Denied' } }, { status });
      if (phase === 'offline') throw new TypeError('Failed to fetch');
    }
    if (path.endsWith('/keys')) return Response.json({ count: 1, schema_revision: 1, items: [{
      id: key, org_id: 'org_00000000-0000-0000-0000-000000000001', project_id: 'prj_00000000-0000-0000-0000-000000000001',
      name: 'UNSELECTED_PRIVATE_KEY', folder_path: '', classification: 'config', description: '', deprecated: false,
      deprecation_note: '', declaration: { rule: { type: 'string' } },
      presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } }, group_id: '', created_at: at,
    }] });
    return undefined;
  });
  const view = await renderForm(<ActualParent selected={surface === 'file' ? 'new' : ''} />, { client: makeQueryClient() });
  try {
    await settleTask();
    if (surface === 'push') await act(async () => button(view.container, 'Add adapter').click());
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).toContain(label);
    const prefix = endpoint === 'keys' ? 'adapter-keys' : endpoint === 'service-accounts' ? 'file-target-accounts' : 'environments';
    phase = 'denied';
    await act(async () => { await view.client.invalidateQueries({ queryKey: [prefix, 'acme', 'app'], exact: true }); });
    await settleTask();
    phase = 'offline';
    await act(async () => { await view.client.invalidateQueries({ queryKey: [prefix, 'acme', 'app'], exact: true }); });
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).not.toContain(label);
    if (surface === 'file') expect(button(view.container, 'Create file target').disabled).toBe(true);
    phase = 'loaded';
    await act(async () => { await view.client.invalidateQueries({ queryKey: [prefix, 'acme', 'app'], exact: true }); });
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).toContain(label);
  } finally { await view.unmount(); }
});

it('R4 cancelled detail denial cannot erase a newer successful detail or prune its list', async () => {
  let late: ((response: Response) => void) | undefined;
  let phase: 'loaded' | 'delayed' | 'newer' = 'loaded';
  const requests = installFetch((request) => {
    if (!new URL(request.url).pathname.endsWith(`/file-targets/${id}`)) return undefined;
    if (phase === 'delayed') return new Promise<Response>((resolve) => { late = resolve; });
    if (phase === 'newer') return Response.json({ ...target, name: 'newer-host', generation: 5 });
    return undefined;
  });
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    phase = 'delayed';
    await act(async () => { void view.client.invalidateQueries({ queryKey: ['file-target', 'acme', 'app', id], exact: true }); });
    expect(late).toBeDefined();
    phase = 'newer';
    await act(async () => { await view.client.invalidateQueries({ queryKey: ['file-target', 'acme', 'app', id], exact: true }); });
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).toContain('newer-host');
    const requestCount = requests.length;
    await act(async () => late?.(Response.json({ error: { code: 'forbidden', message: 'Denied' } }, { status: 403 })));
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).toContain('newer-host');
    expect(requests).toHaveLength(requestCount);
    expect(view.client.getQueryState(['file-target', 'acme', 'app', id])?.status).toBe('success');
  } finally { await view.unmount(); }
});

it('R4 cancelled catalogue denial cannot erase a newer successful shared environment read', async () => {
  let late: ((response: Response) => void) | undefined;
  let phase: 'loaded' | 'delayed' | 'newer' = 'loaded';
  installFetch((request) => {
    if (!new URL(request.url).pathname.endsWith('/environments')) return undefined;
    if (phase === 'delayed') return new Promise<Response>((resolve) => { late = resolve; });
    if (phase === 'newer') return Response.json({ items: [{ id: env, org_id: 'org_00000000-0000-0000-0000-000000000001',
      project_id: 'prj_00000000-0000-0000-0000-000000000001', name: 'newer-environment', display_order: 0, created_at: at }], count: 1 });
    return undefined;
  });
  const view = await renderForm(<ActualParent selected="new" />, { client: makeQueryClient() });
  try {
    await settleTask();
    phase = 'delayed';
    await act(async () => { void view.client.invalidateQueries({ queryKey: ['environments', 'acme', 'app'], exact: true }); });
    expect(late).toBeDefined();
    phase = 'newer';
    await act(async () => { await view.client.invalidateQueries({ queryKey: ['environments', 'acme', 'app'], exact: true }); });
    await settleTask();
    expect(field(view.container, 'Environment').textContent).toContain('newer-environment');
    await act(async () => late?.(Response.json({ error: { code: 'forbidden', message: 'Denied' } }, { status: 403 })));
    await settleTask();
    expect(field(view.container, 'Environment').textContent).toContain('newer-environment');
    expect(view.client.getQueryData(['environments', 'acme', 'app'])).not.toBeNull();
    expect(view.client.getQueryState(['environments', 'acme', 'app'])?.status).toBe('success');
  } finally { await view.unmount(); }
});

it.each(['fresh', 'three days'])('R4 a newly received %s report uses the current render clock before the next timer tick', async (age) => {
  const mountedAt = Date.parse('2026-10-16T10:00:00Z');
  const clock = vi.spyOn(Date, 'now').mockReturnValue(mountedAt);
  let updated = false;
  installFetch((request) => updated && new URL(request.url).pathname.endsWith(`/file-targets/${id}`)
    ? Response.json({ ...target, report: { state: 'current', revision: 1, generation: 4, stamp: '', received_at: at,
      reported_at: new Date(age === 'fresh' ? mountedAt + 10_000 : mountedAt + 11_000 - 3 * 86_400_000).toISOString() } }) : undefined);
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    clock.mockReturnValue(mountedAt + 11_000);
    updated = true;
    await act(async () => button(view.container, 'Refresh report').click());
    await settleTask();
    const detail = view.container.querySelector('aside');
    expect(detail?.textContent).not.toContain('Client clock is ahead');
    expect(detail?.textContent).toContain(age === 'fresh' ? '1s ago' : '3d ago');
  } finally { await view.unmount(); clock.mockRestore(); }
});

it('resamples an unchanged report on successful refresh and retires its age timer on unmount', async () => {
  const mountedAt = Date.parse('2026-10-16T10:00:00Z');
  const clock = vi.spyOn(Date, 'now').mockReturnValue(mountedAt);
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
  installFetch((request) => new URL(request.url).pathname.endsWith(`/file-targets/${id}`)
    ? Response.json({ ...target, report: { state: 'current', revision: 1, generation: 4, stamp: '', received_at: at,
      reported_at: new Date(mountedAt).toISOString() } }) : undefined);
  const view = await renderForm(<ActualParent selected={id} />, { client: makeQueryClient() });
  try {
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).toContain('0s ago');
    clock.mockReturnValue(mountedAt + 11_000);
    await act(async () => button(view.container, 'Refresh report').click());
    await settleTask();
    expect(view.container.querySelector('aside')?.textContent).toContain('11s ago');
    clock.mockReturnValue(mountedAt + 41_000);
    await act(async () => { vi.advanceTimersByTime(30_000); });
    expect(view.container.querySelector('aside')?.textContent).toContain('41s ago');
  } finally {
    await view.unmount();
    const remaining = vi.getTimerCount();
    vi.useRealTimers();
    clock.mockRestore();
    expect(remaining).toBe(0);
  }
});

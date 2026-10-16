// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { makeQueryClient } from '../app/queryClient.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { useProjectEnvironments } from './adapters.ts';
import { useEnvironments as useSettingsEnvironments, useOrgTopology } from './settings.ts';
import { useEnvironments as useValuesEnvironments } from './values.ts';

const org = 'org_00000000-0000-0000-0000-000000000001';
const project = 'prj_00000000-0000-0000-0000-000000000001';
const environment = 'env_00000000-0000-0000-0000-000000000001';
const created_at = '2026-01-01T00:00:00Z';
const environmentKey = ['environments', org, project];

function SharedReaders() {
  const adapters = useProjectEnvironments({ org, project });
  const settings = useSettingsEnvironments(org, project);
  const values = useValuesEnvironments({ org, project, environment });
  const topology = useOrgTopology(org);
  return <>
    {[adapters, settings, values].map((query, index) => <p key={index}>
      {query.data?.items.map((item) => item.name).join(',') ?? 'Unavailable'}
    </p>)}
    <output>{topology.ready ? 'Topology ready' : 'Topology unavailable'}</output>
    <p>{topology.projects.flatMap((item) => item.environments.map((env) => env.name)).join(',')}</p>
  </>;
}

afterEach(() => vi.unstubAllGlobals());

it.each([401, 403, 404])('all canonical environment readers retire %i metadata through network failure and recover on a successful read', async (status) => {
  let phase: 'loaded' | 'denied' | 'offline' | 'recovered' = 'loaded';
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith('/projects')) return Response.json({ count: 1, items: [{ id: project, org_id: org, name: 'App', created_at }] });
    if (path.endsWith('/environments')) {
      if (phase === 'denied') return Response.json({ error: { code: status === 404 ? 'not_found' : status === 401 ? 'unauthenticated' : 'forbidden', message: 'Denied' } }, { status });
      if (phase === 'offline') throw new TypeError('Failed to fetch');
      return Response.json({ count: 1, items: [{ id: environment, org_id: org, project_id: project,
        name: phase === 'loaded' ? 'Old private environment' : 'Recovered environment', display_order: 0, created_at }] });
    }
    if (path.endsWith('/settings')) return Response.json({ protected: false, reauth_window_seconds: 300 });
    throw new Error(`Unexpected ${path}`);
  }));
  const view = await renderForm(<SharedReaders />, { client: makeQueryClient() });
  try {
    await settleTask();
    expect(view.container.textContent).toContain('Old private environment');
    await vi.waitFor(async () => {
      await settleTask();
      expect(view.container.textContent).toContain('Topology ready');
    });
    phase = 'denied';
    await act(async () => { await view.client.invalidateQueries({ queryKey: environmentKey, exact: true }); });
    await settleTask();
    expect(view.container.textContent).not.toContain('Old private environment');
    expect(view.container.textContent).toContain('Topology unavailable');
    phase = 'offline';
    await act(async () => { await view.client.invalidateQueries({ queryKey: environmentKey, exact: true }); });
    await settleTask();
    expect(view.client.getQueryData(environmentKey)).toBeNull();
    expect(view.container.textContent).not.toContain('Old private environment');
    expect(view.container.textContent).toContain('Topology unavailable');
    phase = 'recovered';
    await act(async () => { await view.client.invalidateQueries({ queryKey: environmentKey, exact: true }); });
    await settleTask();
    expect(view.container.textContent).toContain('Recovered environment');
    await vi.waitFor(async () => {
      await settleTask();
      expect(view.container.textContent).toContain('Topology ready');
    });
  } finally { await view.unmount(); }
});

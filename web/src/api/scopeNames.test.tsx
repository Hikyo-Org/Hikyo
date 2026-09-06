// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { useScopeNames } from './scopeNames.ts';

afterEach(() => vi.unstubAllGlobals());

function Labels() {
  const names = useScopeNames('org_00000000-0000-0000-0000-000000000001', 'prj_00000000-0000-0000-0000-000000000001', 'env_00000000-0000-0000-0000-000000000001');
  return <p>{names.org} / {names.project} / {names.environment}</p>;
}

it('resolves the addressed project and environment without reading a directory', async () => {
  const requests: string[] = [];
  vi.stubGlobal('fetch', async (request: Request) => {
    const path = new URL(request.url).pathname;
    requests.push(path);
    const created_at = '2026-09-01T00:00:00Z';
    if (path === '/api/v1/orgs/org_00000000-0000-0000-0000-000000000001') return Response.json({ id: 'org_00000000-0000-0000-0000-000000000001', name: 'Acme', active: true, created_at });
    if (path === '/api/v1/orgs/org_00000000-0000-0000-0000-000000000001/projects/prj_00000000-0000-0000-0000-000000000001') return Response.json({ id: 'prj_00000000-0000-0000-0000-000000000001', org_id: 'org_00000000-0000-0000-0000-000000000001', name: 'Website', created_at });
    if (path === '/api/v1/orgs/org_00000000-0000-0000-0000-000000000001/projects/prj_00000000-0000-0000-0000-000000000001/environments') return Response.json({ items: [{ id: 'env_00000000-0000-0000-0000-000000000001', org_id: 'org_00000000-0000-0000-0000-000000000001', project_id: 'prj_00000000-0000-0000-0000-000000000001', name: 'Production', display_order: 0, created_at }], count: 1 });
    throw new Error(`Unexpected directory read: ${path}`);
  });
  const view = await renderForm(<Labels />);
  try {
    await settleTask();
    expect(view.container.textContent).toBe('Acme / Website / Production');
    expect(requests).toHaveLength(3);
  } finally { await view.unmount(); }
});

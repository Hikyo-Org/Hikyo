// @vitest-environment happy-dom
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { AuthProvider } from '../app/AuthProvider.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { renderForm, settle, settleTask } from '../testkit/renderForm.tsx';
import { Adapters } from './Adapters.tsx';
import { MachineAccess } from './MachineAccess.tsx';
import { ScimProvisioning } from './ScimProvisioning.tsx';

const binding = { org_id: 'org_system', project_id: 'prj_system', environment_id: 'env_system', schema_version: 1 };
const status = { owner_instance_id: 'instance_local', managed: true, binding, generation: 1, desired_revision: 1, latest_revision: 1, state: 'active', nodes: [], job: null };
const json = (value: object, stat = 200) => new Response(JSON.stringify(value), { status: stat, headers: { 'Content-Type': 'application/json' } });

/** Stubs fetch so the self-config read answers and every tenant read is recorded. */
function stub(config: object | null) {
  const paths: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(input, init);
    const path = new URL(request.url).pathname;
    paths.push(path);
    if (path === '/api/v1/auth/whoami') return json(authenticatedIdentity);
    if (path === '/api/v1/instance/config') return config === null ? json({ error: { code: 'not_found', message: 'not found' } }, 404) : json(config);
    return json({ error: { code: 'not_found', message: 'not found' } }, 404);
  }));
  return paths;
}

afterEach(() => vi.unstubAllGlobals());

async function mount(path: string, pattern: string, element: React.ReactElement) {
  return renderForm(
    <AuthProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path={pattern} element={element} />
        </Routes>
      </MemoryRouter>
    </AuthProvider>,
  );
}

describe('the system scope gate', () => {
  it.each([
    ['machine access', '/orgs/org_system/projects/prj_system/machine-access', '/orgs/:org/projects/:project/machine-access', <MachineAccess key="machine-access" />, 'Machine access', 'service-accounts'],
    ['adapters', '/orgs/org_system/projects/prj_system/adapters', '/orgs/:org/projects/:project/adapters', <Adapters key="adapters" />, 'Deployment adapters', '/adapters'],
    ['scim', '/orgs/org_system/scim', '/orgs/:org/scim', <ScimProvisioning key="scim" />, 'SCIM provisioning', 'scim-bindings'],
  ])('answers a deep link into the system scope for %s with the reason, not a denial', async (_name, path, pattern, element, heading, tenantRead) => {
    const paths = stub(status);
    const { container, unmount } = await mount(path, pattern, element);
    await settleTask();
    await settle();
    expect(container.querySelector('h1')?.textContent).toBe(heading);
    const alert = container.querySelector('[role="alert"]')?.textContent ?? '';
    expect(alert).toContain('Hikyo system configuration');
    expect(alert).toContain('not available here');
    expect(alert).toContain('Create an organisation of your own for this under Instance settings');
    // No tenant read was attempted after the binding resolved: the page did
    // not render and then fail, it never asked.
    expect(paths.filter((p) => p.includes(tenantRead))).toEqual([]);
    await unmount();
  });

  it('renders the ordinary page for any other project', async () => {
    const paths = stub(status);
    const { container, unmount } = await mount('/orgs/org_acme/projects/prj_app/adapters', '/orgs/:org/projects/:project/adapters', <Adapters />);
    await settleTask();
    await settle();
    expect(container.textContent).not.toContain('Hikyo system configuration');
    expect(paths.some((p) => p === '/api/v1/orgs/org_acme/projects/prj_app/adapters')).toBe(true);
    await unmount();
  });

  it('renders the ordinary page while the binding is not disclosed', async () => {
    const paths = stub(null);
    const { container, unmount } = await mount('/orgs/org_system/projects/prj_system/adapters', '/orgs/:org/projects/:project/adapters', <Adapters />);
    await settleTask();
    await settle();
    expect(container.textContent).not.toContain('Hikyo system configuration');
    expect(paths.some((p) => p === '/api/v1/orgs/org_system/projects/prj_system/adapters')).toBe(true);
    await unmount();
  });
});

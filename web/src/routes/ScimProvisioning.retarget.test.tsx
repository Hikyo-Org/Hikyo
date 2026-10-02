// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import type { ScimMapping } from '../api/scim.ts';
import { renderForm } from '../testkit/renderForm.tsx';
import { MappingRow } from './ScimProvisioning.tsx';

const mocks = vi.hoisted(() => ({ update: vi.fn() }));
vi.mock('../api/scim.ts', async (importActual) => ({
  ...(await importActual<typeof import('../api/scim.ts')>()),
  useUpdateScimMapping: () => ({ mutate: mocks.update, isPending: false }),
  useDeleteScimMapping: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock('../api/scopeNames.ts', () => ({
  useScopeNames: () => ({ org: 'Org', project: 'Project', environment: 'Production' }),
}));
afterEach(() => vi.clearAllMocks());

for (const scope of [{}, { project_id: 'prj_a' }, { project_id: 'prj_a', environment_id: 'env_a' }]) {
  it(`retargets the displayed mapping scope ${JSON.stringify(scope)}, never another row with the same group`, async () => {
    const row: ScimMapping = {
      id: 'map_a', binding_id: 'binding_a', group_id: 'group_shared', template: 'reader', capabilities: [],
      inert: false, created_at: '2026-09-01T00:00:00Z', ...scope,
    };
    const view = await renderForm(<MappingRow org="org_a" binding="binding_a" row={row} groupName="Shared group" onDeleted={vi.fn()} />);
    try {
      const retarget = view.container.querySelector<HTMLButtonElement>('button[aria-label="Retarget Shared group"]');
      if (retarget === null) throw new Error('retarget action missing');
      await act(async () => retarget.click());
      const form = view.container.querySelector('form');
      if (form === null) throw new Error('retarget form missing');
      await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
      expect(mocks.update).toHaveBeenCalledOnce();
      expect(mocks.update.mock.calls[0]?.[0]).toEqual({
        groupId: 'group_shared', template: 'reader',
        ...('project_id' in scope ? { projectId: scope.project_id } : {}),
        ...('environment_id' in scope ? { environmentId: scope.environment_id } : {}),
      });
    } finally { await view.unmount(); }
  });
}

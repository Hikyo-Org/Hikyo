// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle } from '../testkit/renderForm.tsx';
import { useRestoreRevision } from './history.ts';

afterEach(() => {
  vi.unstubAllGlobals();
});

function RestoreHarness() {
  const restore = useRestoreRevision({
    org: 'org_a',
    project: 'project_a',
    environment: 'environment_a',
  });
  return (
    <button type="button" onClick={() => restore.mutate({ revision: 3n })}>
      Restore
    </button>
  );
}

describe('useRestoreRevision', () => {
  it('refreshes every matrix and history cache changed by a restore', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(
      new Response(JSON.stringify({
        revision: 3,
        changes: [],
        preview: { token: 'preview_token', environments: [] },
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )));
    const { container, client } = await renderForm(<RestoreHarness />);
    const invalidate = vi.spyOn(client, 'invalidateQueries');

    await act(async () => container.querySelector('button')?.click());
    await settle();

    expect(invalidate.mock.calls.map(([filters]) => filters?.queryKey ?? [])).toEqual([
      ['values', 'org_a', 'project_a', 'environment_a'],
      ['matrix-signals', 'org_a', 'project_a'],
      ['matrix-pending', 'org_a', 'project_a'],
      ['revisions', 'org_a', 'project_a', 'environment_a'],
      ['revision-pins', 'org_a', 'project_a', 'environment_a'],
    ]);
  });
});

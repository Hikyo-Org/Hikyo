// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useOrgIdentityMutations, useProjectIdentityMutations } from './settings.ts';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('identity mutations', () => {
  // The page stays mounted across a switch and the observer adopts the newest
  // options, so a save captured before the switch must still name its own target.
  it('send a save captured before a switch to the entity it was made on', async () => {
    const paths: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: Request) => {
        paths.push(new URL(input.url).pathname);
        return Promise.resolve(new Response('{}', { status: 500 }));
      }),
    );
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const root = createRoot(document.createElement('div'));
    const captured: {
      org?: ReturnType<typeof useOrgIdentityMutations>;
      project?: ReturnType<typeof useProjectIdentityMutations>;
    } = {};
    function Harness({ org, keep }: { readonly org: string; readonly keep: boolean }) {
      const orgs = useOrgIdentityMutations(org);
      const projects = useProjectIdentityMutations(org, 'prj');
      if (keep) {
        captured.org = orgs;
        captured.project = projects;
      }
      return null;
    }
    const draw = (org: string, keep: boolean) =>
      act(async () => {
        root.render(
          <QueryClientProvider client={client}>
            <Harness org={org} keep={keep} />
          </QueryClientProvider>,
        );
      });
    await draw('org_a', true);
    await draw('org_b', false);
    const body = { hue: 1, mark: null };
    await act(async () => {
      await captured.org?.set.mutateAsync(body).catch(() => undefined);
      await captured.org?.clearImage.mutateAsync().catch(() => undefined);
      await captured.project?.set.mutateAsync(body).catch(() => undefined);
    });
    await act(async () => root.unmount());
    expect(paths).toEqual([
      '/api/v1/orgs/org_a/identity',
      '/api/v1/orgs/org_a/identity/image',
      '/api/v1/orgs/org_a/projects/prj/identity',
    ]);
  });
});

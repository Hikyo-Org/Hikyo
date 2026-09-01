// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useDeleteKey, useKey, useMatrixProject } from './matrix.ts';
import { pendingDraftsKey, signalsKey, valuesKey } from './keys.ts';

const ref = { org: 'org_a', project: 'project_a' };
const devId = 'env_01989abc-def0-7123-8123-123456789abc';
const prodId = 'env_01989abc-def0-7123-8123-123456789abd';
const keyId = 'key_01989abc-def0-7123-8123-123456789abc';
const environmentBase = {
  org_id: 'org_01989abc-def0-7123-8123-123456789abc',
  project_id: 'prj_01989abc-def0-7123-8123-123456789abc',
  created_at: '2026-08-22T08:00:00Z',
};
const development = {
  ...environmentBase,
  id: devId,
  name: 'development',
  display_order: 0,
};
const production = {
  ...environmentBase,
  id: prodId,
  name: 'production',
  display_order: 1,
};

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

describe('useMatrixProject query ownership', () => {
  it('renders keyed reorder/removal with one observer per unchanged query', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : String(input);
      const path = new URL(url, 'http://localhost').pathname;
      const body = matrixResponse(path);
      return Promise.resolve(
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }));

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Number.POSITIVE_INFINITY } },
    });
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);

    try {
      await act(async () => {
        root.render(
          <QueryClientProvider client={client}>
            <MatrixRows />
          </QueryClientProvider>,
        );
      });
      await settleTasks();

      expect(renderedRows(container)).toEqual(['development:debug', 'production:warn']);
      expect(activeQueries(client)).toHaveLength(3 + 4 * 2);
      expect(activeQueries(client).every((query) => query.getObserversCount() === 1)).toBe(true);
      expect(client.getQueryData(valuesKey({ ...ref, environment: devId }))).toEqual({
        items: [
          {
            key_id: keyId,
            name: 'LOG_LEVEL',
            classification: 'config',
            set: true,
            revealed: true,
            value: 'debug',
          },
        ],
        count: 1,
      });
      expect(client.getQueryData(signalsKey(ref, devId))).toEqual({
        environment_id: devId,
        revision: 2n,
        cells: [],
      });
      expect(client.getQueryData(pendingDraftsKey(ref, devId))).toEqual({ items: [], count: 0 });

      await act(async () => {
        client.setQueryData(['environments', ref.org, ref.project], {
          items: [production, development],
          count: 2,
        });
      });
      await settleTasks();

      expect(renderedRows(container)).toEqual(['production:warn', 'development:debug']);
      expect(activeQueries(client)).toHaveLength(3 + 4 * 2);
      expect(activeQueries(client).every((query) => query.getObserversCount() === 1)).toBe(true);

      await act(async () => {
        client.setQueryData(['environments', ref.org, ref.project], {
          items: [production],
          count: 1,
        });
      });
      await settleTasks();

      expect(renderedRows(container)).toEqual(['production:warn']);
      expect(activeQueries(client)).toHaveLength(3 + 4);
      expect(activeQueries(client).every((query) => query.getObserversCount() === 1)).toBe(true);
    } finally {
      await act(async () => root.unmount());
      client.clear();
    }
  });
});

describe('useDeleteKey invalidation', () => {
  it('refreshes the list without re-fetching the deleted single key', async () => {
    // matrixKeyKey is matrixKeysKey plus a suffix, so a non-exact list
    // invalidation would re-fetch the still-mounted single-key query — a
    // guaranteed 404 that would reject onSuccess and strand the navigate. The
    // hook invalidates the list `exact`, so the single key is never re-read here.
    const requests: { method: string; path: string }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = input instanceof Request ? input.url : String(input);
        const method = input instanceof Request ? input.method : (init?.method ?? 'GET');
        const path = new URL(url, 'http://localhost').pathname;
        requests.push({ method, path });
        if (method === 'DELETE') {
          return Promise.resolve(new Response(null, { status: 204 }));
        }
        const body = path.endsWith(`/keys/${keyId}`)
          ? keyRecord
          : { items: [], count: 0, schema_revision: 1 };
        return Promise.resolve(
          new Response(JSON.stringify(body), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }),
    );

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Number.POSITIVE_INFINITY } },
    });
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    let deleteFn: (() => void) | null = null;

    function DeleteHarness() {
      const key = useKey(ref, keyId);
      const remove = useDeleteKey(ref, keyId);
      deleteFn = () => remove.mutate();
      return <span>{key.data?.name ?? 'pending'}</span>;
    }

    try {
      await act(async () => {
        root.render(
          <QueryClientProvider client={client}>
            <DeleteHarness />
          </QueryClientProvider>,
        );
      });
      await settleTasks();
      // The single key loaded once.
      expect(container.textContent).toBe('LOG_LEVEL');
      expect(requests.filter((r) => r.method === 'GET' && r.path.endsWith(`/keys/${keyId}`))).toHaveLength(1);

      requests.length = 0;
      await act(async () => deleteFn?.());
      await settleTasks();

      // The DELETE ran; the deleted single key was NOT re-fetched afterwards.
      expect(requests.some((r) => r.method === 'DELETE' && r.path.endsWith(`/keys/${keyId}`))).toBe(true);
      expect(
        requests.some((r) => r.method === 'GET' && r.path.endsWith(`/keys/${keyId}`)),
      ).toBe(false);
    } finally {
      await act(async () => root.unmount());
      client.clear();
    }
  });
});

const keyRecord = {
  id: keyId,
  org_id: environmentBase.org_id,
  project_id: environmentBase.project_id,
  name: 'LOG_LEVEL',
  folder_path: 'app',
  classification: 'config' as const,
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string' } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: 'app',
  created_at: '2026-08-22T08:00:00Z',
};

function MatrixRows() {
  const matrix = useMatrixProject(ref);
  return (
    <ol>
      {matrix.environmentRows.map((row) => (
        <li key={row.environmentId}>
          {`${row.environment.name}:${row.values.data?.items[0]?.value ?? 'pending'}`}
        </li>
      ))}
    </ol>
  );
}

function renderedRows(container: HTMLElement): readonly string[] {
  return [...container.querySelectorAll('li')].map((row) => row.textContent ?? '');
}

function activeQueries(client: QueryClient) {
  return client.getQueryCache().getAll().filter((query) => query.getObserversCount() > 0);
}

async function settleTasks(rounds = 20): Promise<void> {
  for (let round = 0; round < rounds; round += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
}

function matrixResponse(path: string): unknown {
  const projectPath = `/api/v1/orgs/${ref.org}/projects/${ref.project}`;
  if (path === `${projectPath}/environments`) {
    return { items: [development, production], count: 2 };
  }
  if (path === `${projectPath}/keys`) {
    return { items: [], count: 0, schema_revision: 1 };
  }
  if (path === `${projectPath}/key-groups`) {
    return { items: [], count: 0 };
  }
  for (const [environmentId, value, revision, protectedEnvironment] of [
    [devId, 'debug', 2, false],
    [prodId, 'warn', 7, true],
  ] satisfies readonly (readonly [string, string, number, boolean])[]) {
    const environmentPath = `${projectPath}/environments/${environmentId}`;
    if (path === `${environmentPath}/values`) {
      return {
        items: [
          {
            key_id: keyId,
            name: 'LOG_LEVEL',
            classification: 'config',
            set: true,
            revealed: true,
            value,
          },
        ],
        count: 1,
      };
    }
    if (path === `${environmentPath}/signals`) {
      return { environment_id: environmentId, revision, cells: [] };
    }
    if (path === `${environmentPath}/settings`) {
      return { protected: protectedEnvironment, reauth_window_seconds: null };
    }
    if (path === `${environmentPath}/pending`) {
      return { items: [], count: 0 };
    }
  }
  throw new Error(`unexpected matrix request ${path}`);
}

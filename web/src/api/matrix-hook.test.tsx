// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ValueList } from '@hikyo/client';
import { useDeleteKey, useInitializeMatrixValue, useKey, useMatrixProject, useStageMatrixValue } from './matrix.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { pendingDraftsKey, signalsKey, valuesKey } from './keys.ts';
import { ApiError } from './client.ts';

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

describe('matrix first-value draft routing', () => {
  it('uses the initializer for first drafts and keeps ordinary edits on setValue', async () => {
    const calls: { readonly path: string; readonly method: string; readonly body: string }[] = [];
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(new URL(String(input), 'http://localhost'), init);
      calls.push({ path: new URL(request.url).pathname, method: request.method, body: await request.text() });
      return new Response(JSON.stringify({
        version_id: 'pcv_01989abc-def0-7123-8123-123456789abc',
        key_id: keyId,
        name: 'NEW_KEY',
        classification: 'config',
        operation: 'set',
        staged_from_revision: 1,
        created_at: '2026-10-05T08:00:00Z',
      }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }));
    function DraftActions() {
      const initialize = useInitializeMatrixValue(ref);
      const stage = useStageMatrixValue(ref);
      return <>
        <button onClick={() => { void initialize.mutateAsync({ environment: devId, key: 'NEW_KEY', value: 'first' }); }}>Initialize</button>
        <button onClick={() => { void stage.mutateAsync({ environment: devId, key: 'NEW_KEY', value: 'edit' }); }}>Edit</button>
      </>;
    }
    const view = await renderForm(<DraftActions />);
    const buttons = view.container.querySelectorAll('button');
    await act(async () => buttons[0]?.click());
    await settleTask();
    await act(async () => buttons[1]?.click());
    await settleTask();
    const path = `/api/v1/orgs/org_a/projects/project_a/environments/${devId}/values/NEW_KEY`;
    expect(calls).toEqual([
      { path: `${path}/initialize`, method: 'POST', body: JSON.stringify({ value: 'first' }) },
      { path, method: 'PUT', body: JSON.stringify({ value: 'edit' }) },
    ]);
    // Mutation results and typed value arguments never enter the query cache.
    expect(view.client.getQueryCache().getAll()).toEqual([]);
    expect(view.client.getMutationCache().getAll()).toEqual([]);
    await view.unmount();
  });
});

describe('useMatrixProject query ownership', () => {
  it('keeps unchanged rows stable while propagating payload, denial, stale and recovery changes', async () => {
    const valuePath = `/api/v1/orgs/${ref.org}/projects/${ref.project}/environments/${devId}/values`;
    let responseStatus = 200;
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : String(input);
      const path = new URL(url, 'http://localhost').pathname;
      const status = path === valuePath ? responseStatus : 200;
      return Promise.resolve(new Response(JSON.stringify(status === 200 ? matrixResponse(path) : { error: 'refused' }), {
        status, headers: { 'Content-Type': 'application/json' },
      }));
    }));
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Number.POSITIVE_INFINITY } },
    });
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    const snapshots: ReturnType<typeof useMatrixProject>['environmentRows'][] = [];
    function Snapshot({ iteration }: { iteration: number }) {
      const matrix = useMatrixProject(ref);
      useEffect(() => { snapshots.push(matrix.environmentRows); }, [matrix.environmentRows, iteration]);
      return <span>{iteration}</span>;
    }
    const latest = () => {
      const rows = snapshots.at(-1);
      if (rows === undefined) throw new Error('Matrix did not render');
      return rows;
    };
    const render = async (iteration: number) => {
      await act(async () => root.render(<QueryClientProvider client={client}><Snapshot iteration={iteration} /></QueryClientProvider>));
      await settleTasks();
    };
    const refetch = async () => {
      await act(async () => client.refetchQueries({ queryKey: valuesKey({ ...ref, environment: devId }), exact: true }));
      await settleTasks();
    };
    try {
      await render(0);
      const ready = latest();
      expect(ready.map((row) => row.readiness)).toEqual(['ready', 'ready']);
      await render(1);
      expect(latest()).toBe(ready);

      await act(async () => {
        client.setQueryData<ValueList>(valuesKey({ ...ref, environment: devId }), (current) => current === undefined ? undefined : {
          ...current, items: current.items.map((item) => ({ ...item, value: 'changed' })),
        });
      });
      await settleTasks();
      const changed = latest();
      expect(changed).not.toBe(ready);
      expect(changed[0]?.values.data?.items[0]?.value).toBe('changed');
      await render(2);
      expect(latest()).toBe(changed);

      // The cached payload still exists on a failed refetch. A 403 must blank
      // it immediately; another failure retains it only as explicitly stale.
      responseStatus = 403;
      await refetch();
      expect(client.getQueryData(valuesKey({ ...ref, environment: devId }))).toBeDefined();
      expect(latest()[0]?.readiness).toBe('forbidden');
      expect(latest()[0]?.values.data).toBeUndefined();
      responseStatus = 500;
      await refetch();
      expect(latest()[0]?.readiness).toBe('stale');
      expect(latest()[0]?.values.data?.items[0]?.value).toBe('changed');
      responseStatus = 200;
      await refetch();
      expect(latest()[0]?.readiness).toBe('ready');
      expect(latest()[0]?.values.data?.items[0]?.value).toBe('debug');
    } finally {
      await act(async () => root.unmount());
      client.clear();
    }
  });

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
    // invalidation would re-fetch the still-mounted single-key query, a
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
      useEffect(() => {
        deleteFn = () => remove.mutate();
      }, [remove]);
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

it('keeps matrix row identity across local renders and updates it when query data changes', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : String(input);
      return Promise.resolve(
        new Response(JSON.stringify(matrixResponse(new URL(url, 'http://localhost').pathname)), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const observed: ReturnType<typeof useMatrixProject>['environmentRows'][] = [];
  function Rows() {
    const matrix = useMatrixProject(ref);
    observed.push(matrix.environmentRows);
    return <span>{matrix.environmentRows.length}</span>;
  }
  const view = await renderForm(<Rows />, { client });
  try {
    await settleTasks();
    const ready = observed.at(-1);
    if (ready === undefined || ready.some((row) => row.readiness !== 'ready'))
      throw new Error('matrix did not load');
    await view.rerender(<Rows />);
    await settleTasks();
    expect(observed.at(-1)).toBe(ready);
    await act(async () =>
      client.setQueryData(valuesKey({ ...ref, environment: devId }), {
        count: 1,
        items: [
          {
            key_id: keyId,
            name: 'LOG_LEVEL',
            classification: 'config',
            set: true,
            revealed: true,
            value: 'changed',
          },
        ],
      }),
    );
    await settleTasks();
    expect(observed.at(-1)).not.toBe(ready);
    expect(
      observed.at(-1)?.find((row) => row.environmentId === devId)?.values.data?.items[0]?.value,
    ).toBe('changed');
    const values = client
      .getQueryCache()
      .find({ queryKey: valuesKey({ ...ref, environment: devId }), exact: true });
    if (values === undefined) throw new Error('missing value query');
    await act(async () =>
      values.setState({ status: 'error', error: new ApiError(403, 'forbidden') }),
    );
    await settleTasks();
    const denied = observed.at(-1)?.find((row) => row.environmentId === devId);
    expect(denied?.readiness).toBe('forbidden');
    expect(denied?.values.data).toBeUndefined();
  } finally {
    await view.unmount();
    client.clear();
  }
});

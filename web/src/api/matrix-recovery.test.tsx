// @vitest-environment happy-dom
import { QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  INITIAL_ADVISORY_LIVENESS,
  SIGNALS_FALLBACK_POLL_MS,
  type AdvisoryLiveness,
  type AdvisoryEvent,
} from './advisory.ts';
import { environmentsKey, pendingDraftsKey, signalsKey } from './keys.ts';
import { ENVIRONMENT_TOPOLOGY_POLL_MS, useMatrixProject } from './matrix.ts';
import { makeQueryClient } from '../app/queryClient.ts';
import { createClient, createConfig } from '@hikyo/runtime-core';
import { WorkspaceContextProvider } from './transport.tsx';

/**
 * The stream's liveness is driven by hand here: what this file proves is what
 * the matrix DOES with a liveness report, the catch-up refetch on recovery and
 * the fallback cadence on the drafts queries. The transition detection itself
 * is the pure reducer, proven in advisory.test.ts.
 */
const liveness = vi.hoisted((): { current: AdvisoryLiveness; onEvent?: (event: AdvisoryEvent) => void } => ({
  current: { connection: 'connecting', recoveries: 0, lost: false },
}));

vi.mock('./advisory.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./advisory.ts')>();
  return { ...actual, useAdvisoryStream: (_ref: object, _transport: object, onEvent: (event: AdvisoryEvent) => void) => { liveness.onEvent = onEvent; return liveness.current; } };
});

const ref = { org: 'org_a', project: 'project_a' };
const devId = 'env_01989abc-def0-7123-8123-123456789abc';
const prodId = 'env_01989abc-def0-7123-8123-123456789abd';
const environmentBase = {
  org_id: 'org_01989abc-def0-7123-8123-123456789abc',
  project_id: 'prj_01989abc-def0-7123-8123-123456789abc',
  created_at: '2026-08-22T08:00:00Z',
};
const environments = [
  { ...environmentBase, id: devId, name: 'development', display_order: 0 },
  { ...environmentBase, id: prodId, name: 'production', display_order: 1 },
];

const addedEnvironment = { ...environmentBase, id: 'env_01989abc-def0-7123-8123-123456789abe', name: 'staging', display_order: 2 };

afterEach(() => {
  liveness.current = INITIAL_ADVISORY_LIVENESS;
  vi.unstubAllGlobals();
  vi.useRealTimers();
  document.body.replaceChildren();
});

function Matrix() {
  const matrix = useMatrixProject(ref);
  return <section data-schema-revision={String(matrix.keys.data?.schema_revision)}>{matrix.environmentRows.map((row) => <span key={row.environmentId} data-environment={row.environmentId}>
    {row.environment.name}:{row.signals.data?.environment_id}:{String(row.signals.data?.revision)}:{row.readiness}
  </span>)}</section>;
}

async function mountMatrix(remote = false) {
  let backendEnvironments = [...environments];
  let listStatus = 200;
  let schemaRevision = 1;
  const requests: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : String(input);
      const path = new URL(url, 'http://localhost').pathname;
      requests.push(remote ? url : path);
      return Promise.resolve(
        new Response(JSON.stringify(path.endsWith('/environments') && listStatus !== 200 ? { error: 'temporarily unavailable' } : matrixResponse(path, backendEnvironments, schemaRevision)), {
          status: path.endsWith('/environments') ? listStatus : 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
  const client = makeQueryClient();
  const workspace = { origin: 'https://remote.test', remote: 'remote', client: createClient(createConfig({ baseUrl: 'https://remote.test' })) };
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  const render = async (next: AdvisoryLiveness) => {
    liveness.current = next;
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          {remote ? <WorkspaceContextProvider value={workspace}><Matrix /></WorkspaceContextProvider> : <Matrix />}
        </QueryClientProvider>,
      );
    });
    await settleTasks();
  };
  await render(liveness.current);
  return {
    client,
    container,
    failListing: (status: number) => { listStatus = status; },
    setEnvironments: (next: typeof environments) => { backendEnvironments = next; schemaRevision += 1; },
    event: async (event: AdvisoryEvent) => { await act(async () => liveness.onEvent?.(event)); await settleTasks(); },
    requests,
    render,
    unmount: async () => {
      await act(async () => root.unmount());
      client.clear();
    },
  };
}

/** The request paths a full working-set refetch produces, sorted for comparison. */
function workingSet(): readonly string[] {
  const projectPath = `/api/v1/orgs/${ref.org}/projects/${ref.project}`;
  return [
    `${projectPath}/environments`,
    `${projectPath}/keys`,
    `${projectPath}/key-groups`,
    ...[devId, prodId].flatMap((environmentId) => [
      `${projectPath}/environments/${environmentId}/values`,
      `${projectPath}/environments/${environmentId}/signals`,
      `${projectPath}/environments/${environmentId}/pending`,
      `${projectPath}/environments/${environmentId}/settings`,
    ]),
  ].sort();
}

describe('useMatrixProject stream recovery', () => {
  it('refetches the whole working set exactly once per recovery, and never on first connect', async () => {
    const matrix = await mountMatrix();
    try {
      expect(matrix.requests.filter((path) => path.endsWith('/environments'))).toHaveLength(1);
      expect([...matrix.requests].sort()).toEqual(
        workingSet(),
      );

      // First connect: the queries were just mounted, nothing to catch up.
      matrix.requests.length = 0;
      await matrix.render({ connection: 'healthy', recoveries: 0, lost: false });
      expect(matrix.requests).toEqual([]);

      // Lost, then back: one catch-up of the full set, and a re-render at the
      // same count adds nothing.
      await matrix.render({ connection: 'failed', recoveries: 0, lost: true });
      expect(matrix.requests).toEqual([]);
      await matrix.render({ connection: 'healthy', recoveries: 1, lost: false });
      expect([...matrix.requests].sort()).toEqual(workingSet());
      matrix.requests.length = 0;
      await matrix.render({ connection: 'healthy', recoveries: 1, lost: false });
      expect(matrix.requests).toEqual([]);
    } finally {
      await matrix.unmount();
    }
  });

  it('polls pending drafts at the fallback cadence while the stream is not healthy', async () => {
    const matrix = await mountMatrix();
    try {
      const intervals = () =>
        [signalsKey(ref, devId), pendingDraftsKey(ref, devId)].map(
          (queryKey) => matrix.client.getQueryCache().find({ queryKey })?.observers[0]?.options.refetchInterval,
        );

      expect(intervals()).toEqual([SIGNALS_FALLBACK_POLL_MS, SIGNALS_FALLBACK_POLL_MS]);
      await matrix.render({ connection: 'failed', recoveries: 0, lost: true });
      expect(intervals()).toEqual([SIGNALS_FALLBACK_POLL_MS, SIGNALS_FALLBACK_POLL_MS]);
      await matrix.render({ connection: 'healthy', recoveries: 1, lost: false });
      expect(intervals()).toEqual([false, false]);
    } finally {
      await matrix.unmount();
    }
  });
});

async function settleTasks(rounds = 20): Promise<void> {
  for (let round = 0; round < rounds; round += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
}

function matrixResponse(path: string, backendEnvironments: typeof environments, schemaRevision: number): unknown {
  const projectPath = `/api/v1/orgs/${ref.org}/projects/${ref.project}`;
  if (path === `${projectPath}/environments`) {
    return { items: backendEnvironments, count: backendEnvironments.length };
  }
  if (path === `${projectPath}/keys`) {
    return { items: [], count: 0, schema_revision: schemaRevision };
  }
  if (path === `${projectPath}/key-groups`) {
    return { items: [], count: 0 };
  }
  for (const environmentId of [...environments, addedEnvironment].map((environment) => environment.id)) {
    const environmentPath = `${projectPath}/environments/${environmentId}`;
    if (path === `${environmentPath}/values`) {
      return { items: [], count: 0 };
    }
    if (path === `${environmentPath}/signals`) {
      return { environment_id: environmentId, revision: environmentId === devId ? 2 : 3, cells: [] };
    }
    if (path === `${environmentPath}/settings`) {
      return { protected: false, reauth_window_seconds: null };
    }
    if (path === `${environmentPath}/pending`) {
      return { items: [], count: 0 };
    }
  }
  throw new Error(`unexpected matrix request ${path}`);
}

for (const remote of [false, true]) {
  describe(remote ? 'remote topology recovery' : 'local topology recovery', () => {
    for (const delivery of ['recovery', 'healthy']) {
      it(`reconciles additions, rename, reorder and deletion via ${delivery} without crossing cache owners`, async () => {
        const matrix = await mountMatrix(remote);
        const otherClient = makeQueryClient();
        let recoveries = 0;
        const refresh = async () => {
          matrix.requests.length = 0;
          if (delivery === 'recovery') {
            await matrix.render({ connection: 'failed', recoveries, lost: true });
            recoveries += 1;
            await matrix.render({ connection: 'healthy', recoveries, lost: false });
          } else {
            await matrix.event({ type: 'environments.changed', projectId: ref.project });
          }
          expect(matrix.requests.filter((path) => path.endsWith('/environments'))).toHaveLength(1);
          if (remote) expect(matrix.requests.every((path) => path.startsWith('https://remote.test/'))).toBe(true);
          matrix.requests.length = 0;
          await matrix.render({ connection: 'healthy', recoveries, lost: false });
          expect(matrix.requests).toEqual([]);
        };
        try {
          await matrix.render({ connection: 'healthy', recoveries, lost: false });
          matrix.setEnvironments([...environments, addedEnvironment]);
          await refresh();
          expect(matrix.container.querySelectorAll('[data-environment]')).toHaveLength(3);
          expect(matrix.container.textContent).toContain(`staging:${addedEnvironment.id}:3:ready`);
          expect(matrix.container.querySelector('section')?.getAttribute('data-schema-revision')).toBe('2');

          const renamed = { ...environments[0], ...environmentBase, id: devId, name: 'renamed', display_order: 1 };
          const reordered = [{ ...addedEnvironment, display_order: 0 }, renamed, { ...environmentBase, id: prodId, name: 'production', display_order: 2 }];
          matrix.setEnvironments(reordered);
          await refresh();
          expect([...matrix.container.querySelectorAll('[data-environment]')].map((row) => row.getAttribute('data-environment'))).toEqual([addedEnvironment.id, devId, prodId]);
          expect(matrix.container.textContent).toContain(`renamed:${devId}:2:ready`);
          expect(matrix.container.querySelector('section')?.getAttribute('data-schema-revision')).toBe('3');

          const retiredKeys = [
            'values', 'reveal-window', 'revisions', 'revision-detail', 'revision-pins',
            'matrix-signals', 'matrix-pending', 'environment-settings', 'approval-requests', 'access-requests',
            'ssh-cas', 'ssh-profiles', 'ssh-certificates', 'delivery-targets', 'dynamic-leases',
            'pki-certificates', 'pki-certificate-profiles',
          ].map((resource) => [resource, ref.org, ref.project, prodId]);
          for (const key of retiredKeys) {
            if (matrix.client.getQueryData(key) === undefined) matrix.client.setQueryData(key, { fixture: 'retired' });
            otherClient.setQueryData(key, { fixture: 'other workspace' });
          }
          const otherProjectKey = ['values', ref.org, 'other-project', prodId];
          matrix.client.setQueryData(otherProjectKey, { fixture: 'other project' });
          matrix.setEnvironments(reordered.filter((environment) => environment.id !== prodId));
          await refresh();
          expect(matrix.container.querySelectorAll('[data-environment]')).toHaveLength(2);
          expect(matrix.container.textContent).not.toContain(prodId);
          for (const key of retiredKeys) {
            expect(matrix.client.getQueryData(key)).toBeUndefined();
            expect(otherClient.getQueryData(key)).toEqual({ fixture: 'other workspace' });
          }
          expect(matrix.client.getQueryData(otherProjectKey)).toEqual({ fixture: 'other project' });
          expect(matrix.client.getQueryData(environmentsKey(ref))).toMatchObject({ count: 2 });
          expect(matrix.container.querySelector('section')?.getAttribute('data-schema-revision')).toBe('4');
          matrix.requests.length = 0;
          await matrix.event({ type: 'environments.changed', projectId: 'other-project' });
          expect(matrix.requests).toEqual([]);
        } finally {
          otherClient.clear();
          await matrix.unmount();
        }
      });
    }
  });
}

it('polls topology while healthy to recover writes served by another node', async () => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
  liveness.current = { connection: 'healthy', recoveries: 0, lost: false };
  const matrix = await mountMatrix();
  try {
    matrix.requests.length = 0;
    matrix.setEnvironments([...environments, addedEnvironment]);
    await act(async () => { await vi.advanceTimersByTimeAsync(ENVIRONMENT_TOPOLOGY_POLL_MS); });
    await settleTasks();
    expect(matrix.requests.filter((path) => path.endsWith('/environments'))).toHaveLength(1);
    expect(matrix.container.textContent).toContain(`staging:${addedEnvironment.id}:3:ready`);
    expect(matrix.container.querySelector('section')?.getAttribute('data-schema-revision')).toBe('2');
    expect(matrix.requests.filter((path) => path.includes(devId) || path.includes(prodId))).toEqual([]);
  } finally {
    await matrix.unmount();
  }
});

it('cancels removed in-flight environment data so a late response cannot repopulate it', async () => {
  const matrix = await mountMatrix();
  try {
    await matrix.render({ connection: 'healthy', recoveries: 0, lost: false });
    const key = ['pki-certificates', ref.org, ref.project, prodId, 'late'];
    let finish: (() => void) | undefined;
    const response = new Promise<string>((resolve) => { finish = () => resolve('late removed data'); });
    const pending = matrix.client.fetchQuery({ queryKey: key, queryFn: () => response }).catch(() => 'cancelled');
    matrix.setEnvironments(environments.filter((environment) => environment.id !== prodId));
    await matrix.event({ type: 'environments.changed', projectId: ref.project });
    expect(await pending).toBe('cancelled');
    finish?.();
    await settleTasks();
    expect(matrix.client.getQueryData(key)).toBeUndefined();
    expect(matrix.container.textContent).not.toContain(prodId);
  } finally {
    await matrix.unmount();
  }
});

it('keeps last-known topology on failed refresh and retires it after a successful retry', async () => {
  const matrix = await mountMatrix();
  try {
    await matrix.render({ connection: 'healthy', recoveries: 0, lost: false });
    matrix.setEnvironments([]);
    matrix.failListing(503);
    await matrix.event({ type: 'environments.changed', projectId: ref.project });
    expect(matrix.container.querySelectorAll('[data-environment]')).toHaveLength(2);
    expect(matrix.client.getQueryData(signalsKey(ref, prodId))).toBeDefined();
    matrix.failListing(200);
    await matrix.event({ type: 'environments.changed', projectId: ref.project });
    expect(matrix.container.querySelectorAll('[data-environment]')).toHaveLength(0);
    expect(matrix.client.getQueryData(signalsKey(ref, prodId))).toBeUndefined();
  } finally {
    await matrix.unmount();
  }
});


it('keeps environment caches for a successful null metadata tombstone until an authoritative empty list arrives', async () => {
  const matrix = await mountMatrix();
  const ownedKeys = [
    'values', 'reveal-window', 'revisions', 'revision-detail', 'revision-pins',
    'matrix-signals', 'matrix-pending', 'environment-settings', 'approval-requests', 'access-requests',
    'ssh-cas', 'ssh-profiles', 'ssh-certificates', 'delivery-targets', 'dynamic-leases',
    'pki-certificates', 'pki-certificate-profiles',
  ].map((resource) => [resource, ref.org, ref.project, prodId]);
  try {
    await matrix.render({ connection: 'healthy', recoveries: 0, lost: false });
    for (const key of ownedKeys) {
      if (matrix.client.getQueryData(key) === undefined) matrix.client.setQueryData(key, { fixture: 'retained' });
    }
    const before = ownedKeys.map((key) => matrix.client.getQueryData(key));
    // Canonical metadata retirement writes null with setQueryData before its
    // denied request rejects. That write is a successful cache state, so
    // isSuccess alone cannot authorize topology-based retirement.
    await act(async () => { matrix.client.setQueryData(environmentsKey(ref), null); });
    await settleTasks();
    expect(matrix.client.getQueryState(environmentsKey(ref))?.status).toBe('success');
    expect(matrix.client.getQueryData(environmentsKey(ref))).toBeNull();
    expect(matrix.container.querySelectorAll('[data-environment]')).toHaveLength(0);
    expect(ownedKeys.map((key) => matrix.client.getQueryData(key))).toEqual(before);

    matrix.setEnvironments([]);
    await matrix.event({ type: 'environments.changed', projectId: ref.project });
    expect(matrix.client.getQueryData(environmentsKey(ref))).toEqual({ items: [], count: 0 });
    for (const key of ownedKeys) expect(matrix.client.getQueryData(key)).toBeUndefined();
  } finally {
    await matrix.unmount();
  }
});

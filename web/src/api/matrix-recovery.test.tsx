// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  INITIAL_ADVISORY_LIVENESS,
  SIGNALS_FALLBACK_POLL_MS,
  type AdvisoryLiveness,
} from './advisory.ts';
import { pendingDraftsKey, signalsKey } from './keys.ts';
import { useMatrixProject } from './matrix.ts';

/**
 * The stream's liveness is driven by hand here: what this file proves is what
 * the matrix DOES with a liveness report, the catch-up refetch on recovery and
 * the fallback cadence on the drafts queries. The transition detection itself
 * is the pure reducer, proven in advisory.test.ts.
 */
const liveness = vi.hoisted((): { current: AdvisoryLiveness } => ({
  current: { connection: 'connecting', recoveries: 0, lost: false },
}));

vi.mock('./advisory.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./advisory.ts')>();
  return { ...actual, useAdvisoryStream: () => liveness.current };
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

afterEach(() => {
  liveness.current = INITIAL_ADVISORY_LIVENESS;
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

function Matrix() {
  const matrix = useMatrixProject(ref);
  return <span>{matrix.environmentRows.length}</span>;
}

async function mountMatrix() {
  const requests: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : String(input);
      const path = new URL(url, 'http://localhost').pathname;
      requests.push(path);
      return Promise.resolve(
        new Response(JSON.stringify(matrixResponse(path)), {
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
  const render = async (next: AdvisoryLiveness) => {
    liveness.current = next;
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <Matrix />
        </QueryClientProvider>,
      );
    });
    await settleTasks();
  };
  await render(liveness.current);
  return {
    client,
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
      expect(matrix.requests.filter((path) => !path.endsWith('/environments')).sort()).toEqual(
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

function matrixResponse(path: string): unknown {
  const projectPath = `/api/v1/orgs/${ref.org}/projects/${ref.project}`;
  if (path === `${projectPath}/environments`) {
    return { items: environments, count: environments.length };
  }
  if (path === `${projectPath}/keys`) {
    return { items: [], count: 0, schema_revision: 1 };
  }
  if (path === `${projectPath}/key-groups`) {
    return { items: [], count: 0 };
  }
  for (const environmentId of [devId, prodId]) {
    const environmentPath = `${projectPath}/environments/${environmentId}`;
    if (path === `${environmentPath}/values`) {
      return { items: [], count: 0 };
    }
    if (path === `${environmentPath}/signals`) {
      return { environment_id: environmentId, revision: 2, cells: [] };
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

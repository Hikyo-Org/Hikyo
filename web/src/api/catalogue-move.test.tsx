// @vitest-environment happy-dom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { z } from 'zod';

import { useMoveKeysToFolders, type FolderMove, type FolderMoveOutcome } from './catalogue.ts';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const ref = { org: 'org_a', project: 'project_a' };
const projectPath = `/api/v1/orgs/${ref.org}/projects/${ref.project}`;
const ids = {
  time: 'key_01989abc-def0-7123-8123-000000000001',
  memory: 'key_01989abc-def0-7123-8123-000000000002',
  rpo: 'key_01989abc-def0-7123-8123-000000000003',
};
const moves: readonly FolderMove[] = [
  { id: ids.time, name: 'HIKYO_ARGON2_TIME', folder: 'Argon2' },
  { id: ids.memory, name: 'HIKYO_ARGON2_MEMORY_KIB', folder: 'Argon2' },
  { id: ids.rpo, name: 'HIKYO_BACKUP_RPO', folder: 'Backup' },
];

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

type Call = { method: string; path: string; body: string };
const zFolderBody = z.object({ path: z.string() });
const zMoveBody = z.object({ folder_path: z.string() });

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function keyRecord(id: string, folder: string): unknown {
  return {
    id,
    org_id: 'org_01989abc-def0-7123-8123-000000000000',
    project_id: 'prj_01989abc-def0-7123-8123-000000000000',
    name: 'X',
    folder_path: folder,
    classification: 'config',
    description: '',
    deprecated: false,
    deprecation_note: '',
    declaration: { rule: { type: 'string' } },
    presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
    group_id: '',
    created_at: '2026-08-22T08:00:00Z',
  };
}

async function run(
  respond: (call: Call) => Response,
  input: { moves: readonly FolderMove[]; existingFolders: readonly string[] },
): Promise<{ calls: Call[]; outcomes: readonly FolderMoveOutcome[] }> {
  const calls: Call[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (request: RequestInfo | URL, init?: RequestInit) => {
      const url = request instanceof Request ? request.url : String(request);
      const method = request instanceof Request ? request.method : (init?.method ?? 'GET');
      const raw = request instanceof Request ? await request.text() : init?.body;
      const call = { method, path: new URL(url, 'http://localhost').pathname, body: typeof raw === 'string' ? raw : '' };
      calls.push(call);
      return respond(call);
    }),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  type Mutate = (input: { moves: readonly FolderMove[]; existingFolders: readonly string[] }) => Promise<readonly FolderMoveOutcome[]>;
  const holder: { mutate: Mutate | null } = { mutate: null };
  function Harness() {
    const move = useMoveKeysToFolders(ref);
    useEffect(() => {
      holder.mutate = (value) => move.mutateAsync(value);
    }, [move]);
    return null;
  }
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>,
    );
  });
  const mutate = holder.mutate;
  if (mutate === null) throw new Error('hook not mounted');
  let outcomes: readonly FolderMoveOutcome[] = [];
  await act(async () => {
    outcomes = await mutate(input);
  });
  await act(async () => root.unmount());
  client.clear();
  return { calls, outcomes };
}

describe('useMoveKeysToFolders', () => {
  it('creates each missing folder once, tolerates one that already exists, and patches each key by id', async () => {
    const { calls, outcomes } = await run(
      (call) => {
        if (call.method === 'POST' && call.path === `${projectPath}/folders`) {
          const body = zFolderBody.parse(JSON.parse(call.body));
          // Backup exists server-side but the caller's list was stale.
          return body.path === 'Backup' ? json(409, { title: 'Conflict', status: 409 }) : json(201, { id: 'fld_01989abc-def0-7123-8123-000000000009', org_id: 'org_01989abc-def0-7123-8123-000000000000', project_id: 'prj_01989abc-def0-7123-8123-000000000000', path: body.path, created_at: '2026-08-22T08:00:00Z' });
        }
        if (call.method === 'PATCH') {
          const body = zMoveBody.parse(JSON.parse(call.body));
          return json(200, keyRecord(call.path.slice(call.path.lastIndexOf('/') + 1), body.folder_path));
        }
        return json(200, { items: [], count: 0 });
      },
      { moves, existingFolders: [] },
    );
    expect(outcomes).toEqual([
      { id: ids.time, error: null },
      { id: ids.memory, error: null },
      { id: ids.rpo, error: null },
    ]);
    const writes = calls
      .filter((call) => call.method !== 'GET')
      .map((call) => [call.method, call.path.slice(projectPath.length), call.body]);
    expect(writes).toEqual([
      ['POST', '/folders', JSON.stringify({ path: 'Argon2' })],
      ['PATCH', `/keys/${ids.time}`, JSON.stringify({ folder_path: 'Argon2' })],
      ['PATCH', `/keys/${ids.memory}`, JSON.stringify({ folder_path: 'Argon2' })],
      ['POST', '/folders', JSON.stringify({ path: 'Backup' })],
      ['PATCH', `/keys/${ids.rpo}`, JSON.stringify({ folder_path: 'Backup' })],
    ]);
  });

  it('stops at the first 429 and reports the rest as not attempted', async () => {
    const { calls, outcomes } = await run(
      (call) => {
        if (call.method === 'PATCH') {
          return call.path.endsWith(ids.time)
            ? json(200, keyRecord(ids.time, 'Argon2'))
            : json(429, { title: 'Too Many Requests', status: 429 });
        }
        return json(200, { items: [], count: 0 });
      },
      { moves, existingFolders: ['Argon2', 'Backup'] },
    );
    expect(outcomes[0]).toEqual({ id: ids.time, error: null });
    expect(outcomes[1]?.error).toContain('schema-revision budget');
    expect(outcomes[2]?.error).toContain('schema-revision budget');
    // No folder creation (both existed), and no PATCH after the 429.
    expect(calls.filter((call) => call.method === 'POST')).toHaveLength(0);
    expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(2);
  });

  it('reports a throttled folder create on that key, not as the revision budget', async () => {
    const { calls, outcomes } = await run(
      (call) => {
        if (call.method === 'POST') {
          return json(429, { title: 'Too Many Requests', status: 429 });
        }
        if (call.method === 'PATCH') {
          return json(200, keyRecord(call.path.slice(call.path.lastIndexOf('/') + 1), 'Argon2'));
        }
        return json(200, { items: [], count: 0 });
      },
      { moves, existingFolders: ['Argon2'] },
    );
    // Argon2 existed, so its two moves PATCH fine; Backup's create is
    // refused, so its key is reported as a folder-create failure and no
    // PATCH is sent for it.
    expect(outcomes.map((outcome) => outcome.error === null)).toEqual([true, true, false]);
    expect(outcomes[2]?.error).toContain('create the folder');
    expect(outcomes[2]?.error).not.toContain('schema-revision budget');
    expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(2);
  });

  it('records a non-429 refusal on that key and continues with the others', async () => {
    const { outcomes } = await run(
      (call) => {
        if (call.method === 'PATCH') {
          return call.path.endsWith(ids.memory)
            ? json(403, { title: 'Forbidden', status: 403 })
            : json(200, keyRecord(call.path.slice(call.path.lastIndexOf('/') + 1), 'Argon2'));
        }
        return json(200, { items: [], count: 0 });
      },
      { moves, existingFolders: ['Argon2', 'Backup'] },
    );
    expect(outcomes.map((outcome) => outcome.error === null)).toEqual([true, false, true]);
    expect(outcomes[1]?.error).toContain('permission');
  });
});

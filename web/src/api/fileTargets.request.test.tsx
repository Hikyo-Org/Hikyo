// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { useCreateFileTarget, useUpdateFileTarget } from './fileTargets.ts';
import { retireSensitiveOperations } from './sensitiveMutation.ts';

const ref = { org: 'acme', project: 'app' };
const at = '2026-01-01T00:00:00Z';
const target = {
  id: 'ftg_00000000-0000-0000-0000-000000000001',
  environment_id: 'env_00000000-0000-0000-0000-000000000001',
  service_account_id: 'sa_00000000-0000-0000-0000-000000000001',
  principal_id: 'mch_00000000-0000-0000-0000-000000000001', name: 'host',
  generation: 1, created_at: at, updated_at: at, keys: [], report: null,
};
function Mutations({ ready }: { ready: (create: ReturnType<typeof useCreateFileTarget>, update: ReturnType<typeof useUpdateFileTarget>) => void }) {
  ready(useCreateFileTarget(ref), useUpdateFileTarget(ref));
  return null;
}
afterEach(() => vi.unstubAllGlobals());

it('refuses unsafe generations before sending and serializes the exact generated PATCH path', async () => {
  const requests: Request[] = [];
  vi.stubGlobal('fetch', vi.fn((...args: Parameters<typeof fetch>) => {
    const request = args[0] instanceof Request ? args[0] : new Request(args[0]);
    requests.push(request.clone());
    return Promise.resolve(Response.json(target));
  }));
  let mutation: ReturnType<typeof useUpdateFileTarget> | undefined;
  const view = await renderForm(<Mutations ready={(_create, update) => { mutation = update; }} />);
  try {
    await act(async () => {
      for (const generation of [0n, -1n, BigInt(Number.MAX_SAFE_INTEGER) + 1n]) {
        await expect(mutation?.mutateAsync({ target: target.id, generation, selection: { key_ids: [] } })).rejects.toThrow('expected generation');
      }
    });
    expect(requests).toHaveLength(0);
    await act(async () => {
      await mutation?.mutateAsync({ target: target.id, generation: BigInt(Number.MAX_SAFE_INTEGER), selection: { key_selection: { names: ['CONFIG'] } } });
    });
    expect(requests).toHaveLength(1);
    expect(requests[0]?.method).toBe('PATCH');
    expect(new URL(requests[0]?.url ?? '').pathname).toBe(`/api/v1/orgs/acme/projects/app/file-targets/${target.id}`);
    expect(await requests[0]?.text()).toContain(`"expected_generation":${Number.MAX_SAFE_INTEGER}`);
  } finally { await view.unmount(); }
});

it.each(['unmount', 'session retirement'])('retires pending create results on %s', async (boundary) => {
  let complete: ((response: Response) => void) | undefined;
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { complete = resolve; })));
  let mutation: ReturnType<typeof useCreateFileTarget> | undefined;
  const view = await renderForm(<Mutations ready={(create) => { mutation = create; }} />);
  const success = vi.fn();
  const failure = vi.fn();
  let pending: Promise<void> | undefined;
  await act(async () => {
    pending = mutation?.mutateAsync({ environment_id: target.environment_id, service_account_id: target.service_account_id, name: 'host', key_selection: { names: ['CONFIG'] } }).then(success, failure);
    await Promise.resolve();
  });
  if (boundary === 'unmount') await view.unmount();
  else await act(async () => retireSensitiveOperations(view.client));
  await act(async () => {
    complete?.(Response.json(target, { status: 201 }));
    await pending;
  });
  expect(success).not.toHaveBeenCalled();
  expect(failure).toHaveBeenCalledWith(expect.objectContaining({ name: 'AbortError' }));
  if (boundary !== 'unmount') await view.unmount();
});

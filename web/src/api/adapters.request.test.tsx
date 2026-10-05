// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { renderForm } from '../testkit/renderForm.tsx';
import {
  useUpdateAdapterTarget, useAdoptAdapterNames,
  type UpdateAdapterTargetInput, type AdoptAdapterNamesInput,
} from './adapters.ts';

const ref = { org: 'org_same', project: 'project_same' };
const update: UpdateAdapterTargetInput = {
  target: 'target_a', expectedGeneration: 1n,
  input: {
    environment_id: 'env_a', destination_kind: 'repository', destination_owner: 'owner',
    destination_name: 'repo', destination_environment: '', visibility: '',
    selected_repository_ids: [], name_prefix: '', key_ids: ['key_a'],
  },
};
const adopt: AdoptAdapterNamesInput = {
  target: 'target_a', targetGeneration: 1n, entries: [],
  artifact: { id: 'artifact_a', destination_id: 1n, repository_id: 0n, target_generation: 1n, entries: [], created_at: '2026-10-05T00:00:00Z' },
};
function Mutations({ ready }: { ready: (update: ReturnType<typeof useUpdateAdapterTarget>, adopt: ReturnType<typeof useAdoptAdapterNames>) => void }) {
  ready(useUpdateAdapterTarget(ref), useAdoptAdapterNames(ref));
  return null;
}
afterEach(() => vi.unstubAllGlobals());
it('refuses unsafe and invalid adapter integer fields before sending a request', async () => {
  const fetch = vi.fn();
  vi.stubGlobal('fetch', fetch);
  let updateMutation: ReturnType<typeof useUpdateAdapterTarget> | undefined;
  let adoptMutation: ReturnType<typeof useAdoptAdapterNames> | undefined;
  const view = await renderForm(<Mutations ready={(update, adopt) => { updateMutation = update; adoptMutation = adopt; }} />);
  const unsafe = BigInt(Number.MAX_SAFE_INTEGER) + 1n;
  await act(async () => {
    for (const value of [0n, -1n, unsafe]) {
      await expect(updateMutation?.mutateAsync({ ...update, expectedGeneration: value })).rejects.toThrow('expected generation');
      await expect(adoptMutation?.mutateAsync({ ...adopt, targetGeneration: value })).rejects.toThrow('target generation');
      await expect(adoptMutation?.mutateAsync({ ...adopt, artifact: { ...adopt.artifact, destination_id: value } })).rejects.toThrow('destination id');
    }
    for (const value of [-1n, unsafe]) {
      await expect(adoptMutation?.mutateAsync({ ...adopt, artifact: { ...adopt.artifact, repository_id: value } })).rejects.toThrow('repository id');
    }
  });
  expect(fetch).not.toHaveBeenCalled();
  await view.unmount();
});
it('sends exact safe-limit adapter values and allows the non-repository zero sentinel', async () => {
  const requests: Request[] = [];
  vi.stubGlobal('fetch', async (request: Request) => {
    requests.push(request);
    // The response is deliberately refused after recording the request body.
    return new Response('', { status: 409 });
  });
  let updateMutation: ReturnType<typeof useUpdateAdapterTarget> | undefined;
  let adoptMutation: ReturnType<typeof useAdoptAdapterNames> | undefined;
  const view = await renderForm(<Mutations ready={(update, adopt) => { updateMutation = update; adoptMutation = adopt; }} />);
  const limit = BigInt(Number.MAX_SAFE_INTEGER);
  await act(async () => {
    await expect(updateMutation?.mutateAsync({ ...update, expectedGeneration: limit })).rejects.toMatchObject({ status: 409 });
    await expect(adoptMutation?.mutateAsync({ ...adopt, targetGeneration: limit, artifact: { ...adopt.artifact, destination_id: limit } })).rejects.toMatchObject({ status: 409 });
  });
  expect(requests).toHaveLength(2);
  expect(await requests[0]?.text()).toContain(`"expected_generation":${Number.MAX_SAFE_INTEGER}`);
  const adoptionBody = await requests[1]?.text();
  expect(adoptionBody).toContain(`"target_generation":${Number.MAX_SAFE_INTEGER}`);
  expect(adoptionBody).toContain(`"destination_id":${Number.MAX_SAFE_INTEGER}`);
  expect(adoptionBody).toContain('"repository_id":0');
  await view.unmount();
});

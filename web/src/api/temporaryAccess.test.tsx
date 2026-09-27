// @vitest-environment happy-dom
import type { QueryClient } from '@tanstack/react-query';
import { act, useLayoutEffect } from 'react';
import { afterEach, describe, expect, it, vi, type MockInstance } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { ApiError } from './client.ts';
import {
  accessQueueKey,
  useAccessAction,
  useAccessPolicies,
  useAccessQueue,
  useDeleteAccessPolicy,
  useSaveAccessPolicy,
  type AccessPolicyDraft,
} from './temporaryAccess.ts';

const ref = { org: 'org_a', project: 'prj_a' };
const environment = 'env_00000000-0000-0000-0000-000000000001';
const policyId = 'xpol_00000000-0000-0000-0000-000000000001';
const requestId = 'xreq_00000000-0000-0000-0000-000000000001';
const principal = 'usr_00000000-0000-0000-0000-000000000001';
const base = '/api/v1/orgs/org_a/projects/prj_a';
const queuePath = `${base}/environments/${environment}/access-requests`;
const request = {
  id: requestId, environment_id: environment, policy_id: policyId,
  policy_version: 1, requester: principal, capabilities: ['reveal'],
  duration_seconds: 3600, reason: 'incident 42', bypassed: false, state: 'open',
  invalidated_cause: '', min_approvals: 1, approvals: 0, votes: [],
  created_at: '2026-09-01T00:00:00Z', review_expires_at: '2026-09-02T00:00:00Z',
};
const draft: AccessPolicyDraft = {
  environmentId: '', capabilities: ['read', 'reveal'], maxDurationSeconds: 3600,
  minApprovals: 2, allowSelfApproval: false, requestTtlSeconds: 86400, enabled: true,
  approvers: [{ kind: 'principal', subject_id: principal },
    { kind: 'scim_group', subject_id: 'grp_00000000-0000-0000-0000-000000000001', binding_id: 'bnd_00000000-0000-0000-0000-000000000001' }],
  bypassers: [principal],
};
const policyBody = {
  environment_id: '', capabilities: ['read', 'reveal'], max_duration_seconds: 3600,
  min_approvals: 2, allow_self_approval: false, request_ttl_seconds: 86400, enabled: true,
  approvers: draft.approvers, bypassers: [principal],
};
const policy = {
  ...policyBody, id: policyId, version: 1,
  created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
};

const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function mountHook<T>(useValue: () => T) {
  let value: T;
  function Harness() {
    const current = useValue();
    useLayoutEffect(() => { value = current; }, [current]);
    return null;
  }
  const mounted = await renderForm(<Harness />);
  cleanups.push(async () => {
    await mounted.unmount();
    mounted.client.clear();
  });
  return { ...mounted, current: () => value };
}

function mockResponse(body: unknown, status = 200) {
  const fetch = vi.fn((_request: Request) => Promise.resolve(
    status === 204 ? new Response(null, { status }) : Response.json(body, { status }),
  ));
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

function invalidatedKeys(spy: MockInstance<QueryClient['invalidateQueries']>) {
  return spy.mock.calls.map(([filters]) => filters?.queryKey);
}

describe('temporary access policy mutations', () => {
  it.each([null, policyId])('saves policy %s and refreshes all environment offers in its project', async (id) => {
    const fetch = mockResponse(policy);
    const hook = await mountHook(() => useSaveAccessPolicy(ref));
    const invalidate = vi.spyOn(hook.client, 'invalidateQueries');
    await act(async () => {
      const result = await hook.current().mutateAsync({ id, draft });
      expect(result.version).toBe(1n);
    });
    expect(fetch).toHaveBeenCalledTimes(1);
    const sent = fetch.mock.calls[0]![0];
    expect(new URL(sent.url).pathname).toBe(`${base}/access-policies${id === null ? '' : `/${id}`}`);
    expect(sent.method).toBe(id === null ? 'POST' : 'PUT');
    expect(await sent.json()).toEqual(policyBody);
    expect(invalidatedKeys(invalidate)).toEqual([
      ['access-policies', ref.org, ref.project], ['access-requests', ref.org, ref.project],
    ]);
  });

  it('deletes a policy with a 204 response and refreshes its project queues', async () => {
    const fetch = mockResponse(null, 204);
    const hook = await mountHook(() => useDeleteAccessPolicy(ref));
    const invalidate = vi.spyOn(hook.client, 'invalidateQueries');
    await act(async () => { await hook.current().mutateAsync(policyId); });
    const sent = fetch.mock.calls[0]![0];
    expect(sent.method).toBe('DELETE');
    expect(new URL(sent.url).pathname).toBe(`${base}/access-policies/${policyId}`);
    expect(invalidatedKeys(invalidate)).toEqual([
      ['access-policies', ref.org, ref.project], ['access-requests', ref.org, ref.project],
    ]);
  });
});

describe('temporary access actions', () => {
  type Action = Parameters<ReturnType<typeof useAccessAction>['mutateAsync']>[0];
  const reason = 'incident 42';
  const cases: Array<{ name: string; input: Action; suffix: string; body: unknown }> = [
    { name: 'request', input: { kind: 'request', draft: { capabilities: ['reveal'], reason, durationSeconds: 3600 } }, suffix: '', body: { capabilities: ['reveal'], reason, duration_seconds: 3600 } },
    { name: 'emergency default duration', input: { kind: 'emergency', draft: { capabilities: ['reveal'], reason } }, suffix: '/emergency', body: { capabilities: ['reveal'], reason } },
    { name: 'emergency explicit duration', input: { kind: 'emergency', draft: { capabilities: ['reveal'], reason, durationSeconds: 60 } }, suffix: '/emergency', body: { capabilities: ['reveal'], reason, duration_seconds: 60 } },
    ...(['approve', 'reject'] as const).map((kind) => ({ name: kind, input: { kind, request: requestId }, suffix: `/${requestId}/vote`, body: { decision: kind } })),
    ...(['cancel', 'revoke'] as const).map((kind) => ({ name: kind, input: { kind, request: requestId }, suffix: `/${requestId}/${kind}`, body: null })),
  ];

  it.each(cases)('$name sends the correct body and refreshes only its environment queue', async ({ input, suffix, body }) => {
    const fetch = mockResponse(request);
    const hook = await mountHook(() => useAccessAction(ref, environment));
    const invalidate = vi.spyOn(hook.client, 'invalidateQueries');
    await act(async () => {
      expect(await hook.current().mutateAsync(input)).toMatchObject({ id: requestId, policy_version: 1n });
    });
    expect(fetch).toHaveBeenCalledTimes(1);
    const sent = fetch.mock.calls[0]![0];
    expect(sent.method).toBe('POST');
    expect(new URL(sent.url).pathname).toBe(queuePath + suffix);
    if (body === null) expect(await sent.text()).toBe('');
    else expect(await sent.json()).toEqual(body);
    expect(invalidatedKeys(invalidate)).toEqual([accessQueueKey(ref, environment)]);
  });

  it('propagates a stale request conflict without invalidating successful data', async () => {
    const fetch = mockResponse({ code: 'conflict', message: 'request already resolved' }, 409);
    const hook = await mountHook(() => useAccessAction(ref, environment));
    const invalidate = vi.spyOn(hook.client, 'invalidateQueries');
    await act(async () => {
      await expect(hook.current().mutateAsync({ kind: 'approve', request: requestId })).rejects.toBeInstanceOf(ApiError);
    });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(invalidate).not.toHaveBeenCalled();
  });
});

describe('temporary access queries', () => {
  it.each([
    { org: '', project: 'prj_a', environment },
    { org: 'org_a', project: '', environment },
    { org: 'org_a', project: 'prj_a', environment: '' },
  ])('does not fetch a queue for incomplete scope %j', async (scope) => {
    const fetch = mockResponse({ items: [] });
    const hook = await mountHook(() => useAccessQueue({ org: scope.org, project: scope.project }, scope.environment));
    await settleTask();
    expect(hook.current().fetchStatus).toBe('idle');
    expect(fetch).not.toHaveBeenCalled();
  });

  it.each([{ org: '', project: 'prj_a' }, { org: 'org_a', project: '' }])('does not fetch policies for incomplete scope %j', async (scope) => {
    const fetch = mockResponse({ items: [] });
    await mountHook(() => useAccessPolicies(scope));
    await settleTask();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('caches parsed requests under the full tenant and environment scope', async () => {
    const fetch = mockResponse({ items: [request] });
    const hook = await mountHook(() => useAccessQueue(ref, environment));
    await settleTask();
    expect(hook.current().data?.items[0]?.policy_version).toBe(1n);
    expect(new URL(fetch.mock.calls[0]![0].url).pathname).toBe(queuePath);
    expect(hook.client.getQueryData(accessQueueKey(ref, environment))).toEqual(hook.current().data);
    expect(hook.client.getQueryData(accessQueueKey(ref, 'other_env'))).toBeUndefined();
    expect(hook.client.getQueryData(accessQueueKey({ ...ref, org: 'other_org' }, environment))).toBeUndefined();
  });
});

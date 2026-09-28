import {
  cancelAccessRequestOp,
  createAccessPolicyOp,
  createAccessRequestOp,
  deleteAccessPolicyOp,
  emergencyAccessOp,
  listAccessPoliciesOp,
  listAccessRequestsOp,
  revokeAccessRequestOp,
  updateAccessPolicyOp,
  voteAccessRequestOp,
} from '@hikyo/operations';
import { zAccessCapability, zAccessPolicy, zAccessQueue, zAccessRequest } from '@hikyo/zod';
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query';
import type { z } from 'zod';

import { ok, parsed } from './client.ts';
import type { MatrixRef } from './keys.ts';
import { useTransport } from './transport.tsx';

// Approval-mediated temporary access (#152). Policy administration is
// project-scoped; the request queue, decisions and emergency access are
// environment-scoped. Temporary grants carry an absolute expiry the server's
// authorization chokepoint enforces; nothing here decides authority.

export type AccessPolicy = z.infer<typeof zAccessPolicy>;
export type AccessRequest = z.infer<typeof zAccessRequest>;
export type AccessQueue = z.infer<typeof zAccessQueue>;
export type AccessCapability = z.infer<typeof zAccessCapability>;
export type AccessApprover = AccessPolicy['approvers'][number];

/** The closed set a policy may offer, in the order the server lists it. */
export const ACCESS_CAPABILITIES: readonly AccessCapability[] = [
  'read',
  'reveal',
  'reveal-history',
  'edit',
  'publish',
  'pin',
];

/** A policy's editable shape, as the editor collects it. */
export type AccessPolicyDraft = {
  readonly environmentId: string;
  readonly capabilities: readonly AccessCapability[];
  readonly maxDurationSeconds: number;
  readonly minApprovals: number;
  readonly allowSelfApproval: boolean;
  readonly requestTtlSeconds: number;
  readonly enabled: boolean;
  readonly approvers: readonly AccessApprover[];
  readonly bypassers: readonly string[];
};

/** What a requester asks for. */
/** An ordinary request names its duration; the server never picks one. */
export type AccessRequestDraft = {
  readonly capabilities: readonly AccessCapability[];
  readonly durationSeconds: number;
  readonly reason: string;
};

/** Emergency access may omit the duration: the server defaults it to one hour, capped by the policy. */
export type EmergencyAccessDraft = {
  readonly capabilities: readonly AccessCapability[];
  readonly durationSeconds?: number;
  readonly reason: string;
};

/** Builds the project-wide policy cache key, independent of environment. */
const accessPoliciesKey = (ref: MatrixRef) => ['access-policies', ref.org, ref.project] as const;
/** Builds the cache key for one environment's temporary-access queue. */
export const accessQueueKey = (ref: MatrixRef, environment: string) =>
  ['access-requests', ref.org, ref.project, environment] as const;

/** Loads project policies without retries; disabled until both scope IDs are present. */
export function useAccessPolicies(ref: MatrixRef): UseQueryResult<{ items: AccessPolicy[] }> {
  const transport = useTransport();
  return useQuery({
    queryKey: accessPoliciesKey(ref),
    queryFn: () => parsed(listAccessPoliciesOp, { path: ref, ...transport }),
    enabled: ref.org !== '' && ref.project !== '',
    retry: false,
  });
}

/** Loads the selected environment's queue; disabled until all scope IDs are present. */
export function useAccessQueue(ref: MatrixRef, environment: string): UseQueryResult<AccessQueue> {
  const transport = useTransport();
  return useQuery({
    queryKey: accessQueueKey(ref, environment),
    queryFn: () => parsed(listAccessRequestsOp, { path: { ...ref, environment }, ...transport }),
    enabled: ref.org !== '' && ref.project !== '' && environment !== '',
  });
}

/** Copies the editor draft into wire fields, preserving seconds and optional group bindings. */
function policyBody(draft: AccessPolicyDraft) {
  return {
    environment_id: draft.environmentId,
    capabilities: [...draft.capabilities],
    max_duration_seconds: draft.maxDurationSeconds,
    min_approvals: draft.minApprovals,
    allow_self_approval: draft.allowSelfApproval,
    request_ttl_seconds: draft.requestTtlSeconds,
    enabled: draft.enabled,
    approvers: draft.approvers.map((a) => ({
      kind: a.kind,
      subject_id: a.subject_id,
      ...(a.binding_id === undefined ? {} : { binding_id: a.binding_id }),
    })),
    bypassers: [...draft.bypassers],
  };
}

/** Builds an ordinary request body with its explicit duration in seconds. */
function requestBody(draft: AccessRequestDraft) {
  return { capabilities: [...draft.capabilities], reason: draft.reason, duration_seconds: draft.durationSeconds };
}

/** Omits an undefined duration so the server can apply the policy-capped default. */
function emergencyBody(draft: EmergencyAccessDraft) {
  return {
    capabilities: [...draft.capabilities],
    reason: draft.reason,
    ...(draft.durationSeconds === undefined ? {} : { duration_seconds: draft.durationSeconds }),
  };
}

/**
 * Creates a policy when id is null, otherwise replaces it. Success invalidates
 * project policy and request queues; request or response-validation failures
 * are exposed through the mutation.
 */
export function useSaveAccessPolicy(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly id: string | null; readonly draft: AccessPolicyDraft }) => {
      if (input.id === null) {
        return parsed(createAccessPolicyOp, { path: ref, body: policyBody(input.draft), ...transport });
      }
      return parsed(updateAccessPolicyOp, {
        path: { ...ref, policy: input.id },
        body: policyBody(input.draft),
        ...transport,
      });
    },
    onSuccess: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: accessPoliciesKey(ref) }),
        queries.invalidateQueries({ queryKey: ['access-requests', ref.org, ref.project] }),
      ]),
  });
}

/**
 * Deletes a policy and invalidates project policy and request queues on success.
 * Request failures are exposed through the mutation.
 */
export function useDeleteAccessPolicy(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (id: string) => ok(deleteAccessPolicyOp, { path: { ...ref, policy: id }, ...transport }),
    onSuccess: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: accessPoliciesKey(ref) }),
        queries.invalidateQueries({ queryKey: ['access-requests', ref.org, ref.project] }),
      ]),
  });
}

/**
 * useAccessAction files, decides, withdraws, revokes or takes access in one environment.
 * Success returns the request and invalidates that environment's queue.
 * Request and response-validation failures are exposed through the mutation;
 * callers must arrange any required emergency-access reauthentication.
 */
export function useAccessAction(ref: MatrixRef, environment: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  const path = { ...ref, environment };
  return useMutation({
    mutationFn: (
      input:
        | { readonly kind: 'request'; readonly draft: AccessRequestDraft }
        | { readonly kind: 'emergency'; readonly draft: EmergencyAccessDraft }
        | { readonly kind: 'approve' | 'reject' | 'cancel' | 'revoke'; readonly request: string },
    ): Promise<AccessRequest> => {
      switch (input.kind) {
        case 'request':
          return parsed(createAccessRequestOp, { path, body: requestBody(input.draft), ...transport });
        case 'emergency':
          return parsed(emergencyAccessOp, { path, body: emergencyBody(input.draft), ...transport });
        case 'approve':
        case 'reject':
          return parsed(voteAccessRequestOp, {
            path: { ...path, accessRequest: input.request },
            body: { decision: input.kind },
            ...transport,
          });
        case 'cancel':
          return parsed(cancelAccessRequestOp, { path: { ...path, accessRequest: input.request }, ...transport });
        case 'revoke':
          return parsed(revokeAccessRequestOp, { path: { ...path, accessRequest: input.request }, ...transport });
      }
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: accessQueueKey(ref, environment) }),
  });
}

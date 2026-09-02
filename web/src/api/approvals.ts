import {
  createApprovalPolicyOp,
  deleteApprovalPolicyOp,
  listApprovalPoliciesOp,
  listApprovalRequestsOp,
  publishPendingChangesOp,
  updateApprovalPolicyOp,
  voteApprovalRequestOp,
} from '@hikyo/operations';
import { zApprovalPolicy, zApprovalRequest } from '@hikyo/zod';
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

// Secret-change approvals (#151). Policy administration is project-scoped; the
// review queue and voting are environment-scoped. Merge and bypass are the
// ordinary publish with the request id, so they ride publishPendingChangesOp
// rather than a second mutation path.

export type ApprovalPolicy = z.infer<typeof zApprovalPolicy>;
export type ApprovalRequest = z.infer<typeof zApprovalRequest>;
export type ApproverSpec = ApprovalPolicy['approvers'][number];

/** A policy's editable shape, as the editor collects it. */
export type PolicyDraft = {
  readonly environmentId: string;
  readonly minApprovals: number;
  readonly allowSelfApproval: boolean;
  readonly requestTtlSeconds: number;
  readonly enabled: boolean;
  readonly approvers: readonly ApproverSpec[];
  readonly bypassers: readonly string[];
};

export const approvalPoliciesKey = (ref: MatrixRef) =>
  ['approval-policies', ref.org, ref.project] as const;
export const approvalRequestsKey = (ref: MatrixRef, environment: string) =>
  ['approval-requests', ref.org, ref.project, environment] as const;

export function useApprovalPolicies(ref: MatrixRef): UseQueryResult<{ items: ApprovalPolicy[] }> {
  const transport = useTransport();
  return useQuery({
    queryKey: approvalPoliciesKey(ref),
    queryFn: () => parsed(listApprovalPoliciesOp, { path: ref, ...transport }),
    enabled: ref.org !== '' && ref.project !== '',
    retry: false,
  });
}

export function useApprovalRequests(
  ref: MatrixRef,
  environment: string,
): UseQueryResult<{ items: ApprovalRequest[] }> {
  const transport = useTransport();
  return useQuery({
    queryKey: approvalRequestsKey(ref, environment),
    queryFn: () =>
      parsed(listApprovalRequestsOp, { path: { ...ref, environment }, ...transport }),
    enabled: ref.org !== '' && ref.project !== '' && environment !== '',
    retry: false,
  });
}

function policyBody(draft: PolicyDraft) {
  return {
    environment_id: draft.environmentId,
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

export function useSavePolicy(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly id: string | null; readonly draft: PolicyDraft }) => {
      if (input.id === null) {
        return parsed(createApprovalPolicyOp, { path: ref, body: policyBody(input.draft), ...transport });
      }
      return parsed(updateApprovalPolicyOp, {
        path: { ...ref, policy: input.id },
        body: policyBody(input.draft),
        ...transport,
      });
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: approvalPoliciesKey(ref) }),
  });
}

export function useDeletePolicy(ref: MatrixRef) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (id: string) =>
      ok(deleteApprovalPolicyOp, { path: { ...ref, policy: id }, ...transport }),
    onSuccess: () => queries.invalidateQueries({ queryKey: approvalPoliciesKey(ref) }),
  });
}

export function useVote(ref: MatrixRef, environment: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly request: string; readonly decision: 'approve' | 'reject' }) =>
      parsed(voteApprovalRequestOp, {
        path: { ...ref, environment, approvalRequest: input.request },
        body: { decision: input.decision },
        ...transport,
      }),
    onSuccess: () =>
      queries.invalidateQueries({ queryKey: approvalRequestsKey(ref, environment) }),
  });
}

/**
 * useResolveRequest merges an approved request, or emergency-bypasses one with a
 * reason. Both ride the ordinary publish endpoint with the request id, so a
 * merge is the same validated materialization any publish is.
 */
export function useResolveRequest(ref: MatrixRef, environment: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { readonly request: string; readonly bypassReason?: string }) =>
      parsed(publishPendingChangesOp, {
        path: { ...ref, environment },
        body: {
          approval_request_id: input.request,
          ...(input.bypassReason === undefined ? {} : { bypass: { reason: input.bypassReason } }),
        },
        ...transport,
      }),
    onSuccess: () =>
      Promise.all([
        queries.invalidateQueries({ queryKey: approvalRequestsKey(ref, environment) }),
        queries.invalidateQueries({ queryKey: ['values', ref.org, ref.project] }),
      ]),
  });
}

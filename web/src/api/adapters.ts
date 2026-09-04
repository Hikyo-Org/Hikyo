import {
  addAdapterTargetOp,
  adoptAdapterTargetNamesOp,
  cancelAdapterMoveOp,
  createAdapterOp,
  deleteAdapterOp,
  listAdaptersOp,
  listEnvironmentsOp,
  listKeysOp,
  pauseAdapterTargetOp,
  planAdapterTargetOp,
  removeAdapterTargetOp,
  resumeAdapterMoveOp,
  resumeAdapterTargetOp,
  revokeAdapterCredentialOp,
  setAdapterCredentialOp,
  showAdapterMoveOp,
  showAdapterTargetOp,
  syncAdapterTargetOp,
  testAdapterTargetOp,
  updateAdapterOriginOp,
  updateAdapterTargetOp,
} from '@hikyo/operations';
import type {
  AdapterTargetInput,
  UpdateAdapterTargetRequest,
} from '@hikyo/client';
import {
  zAdapter,
  zAdapterConflictArtifact,
  zAdapterConnection,
  zAdapterList,
  zAdapterMove,
  zAdapterPlan,
  zAdapterTarget,
  zAdapterTargetDetail,
  zEnvironmentList,
  zKeyList,
} from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, ok, parsed } from './client.ts';

export type Adapter = z.infer<typeof zAdapter>;
export type AdapterList = z.infer<typeof zAdapterList>;
export type AdapterTarget = z.infer<typeof zAdapterTarget>;
export type AdapterTargetDetail = z.infer<typeof zAdapterTargetDetail>;
// Request shapes come from the generated client, not the response zod: the
// two disagree on int64 (bigint in a parsed response, number on the wire).
export type { AdapterTargetInput };
export type AdapterConflictArtifact = z.infer<typeof zAdapterConflictArtifact>;
export type AdapterMove = z.infer<typeof zAdapterMove>;
export type AdapterPlan = z.infer<typeof zAdapterPlan>;
export type AdapterConnection = z.infer<typeof zAdapterConnection>;
export type AdapterHealth = AdapterTarget['sync_status'];
export type ProjectEnvironment = z.infer<typeof zEnvironmentList>['items'][number];
export type ProjectKey = z.infer<typeof zKeyList>['items'][number];

export type ProjectRef = { readonly org: string; readonly project: string };
type AdaptersKey = readonly ['adapters', string, string];
type AdapterTargetKey = readonly ['adapter-target', string, string, string];
type AdapterMoveKey = readonly ['adapter-move', string, string, string];
type EnvironmentsKey = readonly ['environments', string, string];
type KeysKey = readonly ['adapter-keys', string, string];

// Query keys are addressed by (org, project): the adapters list is a project
// aggregate, and a target detail keys on its own id beneath it so a mutation
// on one target invalidates exactly its detail and the list that renders it.
export const adaptersKey = (ref: ProjectRef): AdaptersKey => ['adapters', ref.org, ref.project];
const adapterTargetKey = (ref: ProjectRef, target: string): AdapterTargetKey => [
  'adapter-target',
  ref.org,
  ref.project,
  target,
];
const adapterMoveKey = (ref: ProjectRef, move: string): AdapterMoveKey => [
  'adapter-move',
  ref.org,
  ref.project,
  move,
];
const environmentsKey = (ref: ProjectRef): EnvironmentsKey => ['environments', ref.org, ref.project];
const keysKey = (ref: ProjectRef): KeysKey => ['adapter-keys', ref.org, ref.project];

/** The project's environments, for the target form and for naming targets. */
export function useProjectEnvironments(
  ref: ProjectRef,
  enabled = true,
): UseQueryResult<z.infer<typeof zEnvironmentList>> {
  return useQuery({
    enabled,
    queryKey: environmentsKey(ref),
    queryFn: () => parsed(listEnvironmentsOp, { path: ref }),
  });
}

/** The project's key catalogue, for the explicit key subset picker. */
export function useProjectKeys(ref: ProjectRef): UseQueryResult<z.infer<typeof zKeyList>> {
  return useQuery({
    queryKey: keysKey(ref),
    queryFn: () => parsed(listKeysOp, { path: ref }),
  });
}

export function useAdapters(ref: ProjectRef): UseQueryResult<AdapterList> {
  return useQuery({
    queryKey: adaptersKey(ref),
    queryFn: () => parsed(listAdaptersOp, { path: ref }),
  });
}

/**
 * useAdapterTarget reads ONE target with its conflict artifacts and the
 * names-only workflow mapping. Enabled only while a target is selected. The
 * detail is polled while the target is mid-converge so the status chip moves
 * without a reload; a settled target stops polling.
 */
export function useAdapterTarget(ref: ProjectRef, target: string): UseQueryResult<AdapterTargetDetail> {
  return useQuery({
    queryKey: adapterTargetKey(ref, target),
    queryFn: () => parsed(showAdapterTargetOp, { path: { ...ref, target } }),
    enabled: target !== '',
    refetchInterval: (query) => {
      const status = query.state.data?.target.sync_status;
      return status === 'pending' || status === 'converging' ? 2_000 : false;
    },
  });
}

/**
 * healthLabel is the operator's word for each state. Text carries the meaning;
 * the chip's colour only echoes it, so a monochrome or screen-reader read of
 * the surface loses nothing.
 */
export function healthLabel(status: AdapterHealth): string {
  switch (status) {
    case 'never':
      return 'Never synced';
    case 'pending':
      return 'Queued';
    case 'converging':
      return 'Converging';
    case 'converged':
      return 'Healthy';
    case 'degraded':
      return 'Degraded';
    case 'failed':
      return 'Failed';
    case 'paused':
      return 'Paused';
  }
}

/** errorClassText renders the bounded cause the server recorded. */
export function errorClassText(cause: AdapterTarget['last_error_class']): string {
  switch (cause) {
    case '':
      return '';
    case 'auth':
      return 'the provider refused the credential';
    case 'network':
      return 'the provider could not be reached';
    case 'conflict':
      return 'an unowned name is in the way';
    case 'provider_limit':
      return 'the provider is rate limiting';
    case 'provider_ambiguous':
      return 'the provider gave an indeterminate answer or the destination moved';
    case 'refused':
      return 'the recorded authority or generation no longer holds';
  }
}

/** Every mutation invalidates the list and, when it names one, the target. */
function useInvalidateAdapters(ref: ProjectRef) {
  const queries = useQueryClient();
  return (target?: string) => {
    void queries.invalidateQueries({ queryKey: adaptersKey(ref) });
    if (target !== undefined) {
      void queries.invalidateQueries({ queryKey: adapterTargetKey(ref, target) });
    }
  };
}

export type CreateAdapterInput = {
  readonly provider: 'forgejo' | 'github-actions';
  readonly origin: string;
  /** Write-only. Held in component state only for the request. */
  readonly credential: string;
  readonly target: AdapterTargetInput;
};

export function useCreateAdapter(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: CreateAdapterInput) =>
      parsed(createAdapterOp, {
        path: ref,
        body: {
          provider: input.provider,
          origin: input.origin,
          credential: input.credential,
          target: input.target,
        },
      }),
    onSettled: () => invalidate(),
  });
}

export function useAddAdapterTarget(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: { readonly adapter: string; readonly target: AdapterTargetInput }) =>
      parsed(addAdapterTargetOp, { path: { ...ref, adapter: input.adapter }, body: input.target }),
    onSettled: () => invalidate(),
  });
}

export type UpdateAdapterTargetInput = {
  readonly target: string;
  readonly expectedGeneration: bigint;
  readonly input: AdapterTargetInput;
};

/**
 * useUpdateAdapterTarget replaces the key subset and prefix in place. A
 * destination change is a scrub-before-switch move and is NOT offered from
 * the browser: the CLI's `adapter update --target` owns that ceremony.
 */
export function useUpdateAdapterTarget(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: UpdateAdapterTargetInput) =>
      parsed(updateAdapterTargetOp, {
        path: { ...ref, target: input.target },
        body: updateBody(input),
      }),
    onSettled: (_data, _error, input) => invalidate(input.target),
  });
}

function updateBody(input: UpdateAdapterTargetInput): UpdateAdapterTargetRequest {
  return {
    environment_id: input.input.environment_id,
    destination_kind: input.input.destination_kind,
    destination_owner: input.input.destination_owner,
    destination_name: input.input.destination_name,
    destination_environment: input.input.destination_environment,
    visibility: input.input.visibility,
    selected_repository_ids: input.input.selected_repository_ids,
    name_prefix: input.input.name_prefix,
    key_ids: input.input.key_ids,
    ...(input.input.key_selection === undefined ? {} : { key_selection: input.input.key_selection }),
    expected_generation: Number(input.expectedGeneration),
  };
}

export function usePauseAdapterTarget(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (target: string) => parsed(pauseAdapterTargetOp, { path: { ...ref, target } }),
    onSettled: (_data, _error, target) => invalidate(target),
  });
}

export function useResumeAdapterTarget(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (target: string) => parsed(resumeAdapterTargetOp, { path: { ...ref, target } }),
    onSettled: (_data, _error, target) => invalidate(target),
  });
}

/** Resync is the manual converge: newest wins, idempotent by target. */
export function useResyncAdapterTarget(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (target: string) => parsed(syncAdapterTargetOp, { path: { ...ref, target } }),
    onSettled: (_data, _error, target) => invalidate(target),
  });
}

export type RemoveAdapterTargetInput = {
  readonly target: string;
  /** The explicit retain-or-prune decision; there is no default in the UI. */
  readonly decision: 'prune' | 'retain';
};

export function useRemoveAdapterTarget(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: RemoveAdapterTargetInput) =>
      parsed(removeAdapterTargetOp, {
        path: { ...ref, target: input.target },
        query: { keep_remote: input.decision === 'retain' },
      }),
    onSettled: (_data, _error, input) => invalidate(input.target),
  });
}

export type AdoptAdapterNamesInput = {
  readonly target: string;
  readonly artifact: AdapterConflictArtifact;
  /** The exact subset of the artifact's entries the human acknowledged. */
  readonly entries: AdapterConflictArtifact['entries'];
  readonly targetGeneration: bigint;
};

/**
 * useAdoptAdapterNames commits an enumerated adoption bound to the artifact,
 * generation and destination it was observed under. There is no adopt-all:
 * the entries are exactly the rows the human ticked.
 */
export function useAdoptAdapterNames(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: AdoptAdapterNamesInput) =>
      parsed(adoptAdapterTargetNamesOp, {
        path: { ...ref, target: input.target },
        body: {
          artifact_id: input.artifact.id,
          target_generation: Number(input.targetGeneration),
          destination_id: Number(input.artifact.destination_id),
          repository_id: Number(input.artifact.repository_id),
          entries: [...input.entries],
        },
      }),
    onSettled: (_data, _error, input) => invalidate(input.target),
  });
}

/** A move still has work in flight while it scrubs or activates. */
export function moveInFlight(state: AdapterMove['state']): boolean {
  return state === 'scrubbing' || state === 'activating';
}

/**
 * useAdapterMove polls a durable route move while it is in flight and stops
 * once it settles: the server's own state machine decides when the poll ends,
 * not a client timer. Credentials are never represented in the response.
 */
export function useAdapterMove(ref: ProjectRef, move: string): UseQueryResult<AdapterMove> {
  return useQuery({
    queryKey: adapterMoveKey(ref, move),
    queryFn: () => parsed(showAdapterMoveOp, { path: { ...ref, move } }),
    enabled: move !== '',
    refetchInterval: (query) =>
      query.state.data !== undefined && moveInFlight(query.state.data.state) ? 2_000 : false,
  });
}

export type MoveAdapterOriginInput = {
  readonly adapter: string;
  readonly origin: string;
  /** Write-only. Held in component state only for the request. */
  readonly credential: string;
  readonly keepRemote: boolean;
};

/**
 * useMoveAdapterOrigin starts the scrub-before-switch origin move. It answers
 * 202 with the move, whose id is the only handle on its progress: the
 * adapter itself reports `moving` and nothing more.
 */
export function useMoveAdapterOrigin(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: MoveAdapterOriginInput) =>
      parsed(updateAdapterOriginOp, {
        path: { ...ref, adapter: input.adapter },
        body: { origin: input.origin, credential: input.credential, keep_remote: input.keepRemote },
      }),
    onSettled: () => invalidate(),
  });
}

export type ResumeAdapterMoveInput = {
  readonly move: string;
  readonly origin: string;
  /** Write-only replacement for the pending origin credential. */
  readonly credential: string;
};

/** useResumeAdapterMove replaces an attention-required pending origin route and reactivates it. */
export function useResumeAdapterMove(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: ResumeAdapterMoveInput) =>
      parsed(resumeAdapterMoveOp, {
        path: { ...ref, move: input.move },
        body: { origin: input.origin, credential: input.credential },
      }),
    onSettled: (_data, _error, input) => {
      invalidate();
      void queries.invalidateQueries({ queryKey: adapterMoveKey(ref, input.move) });
    },
  });
}

/** useCancelAdapterMove abandons an attention-required move and reconverges the old route. */
export function useCancelAdapterMove(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (move: string) => parsed(cancelAdapterMoveOp, { path: { ...ref, move } }),
    onSettled: (_data, _error, move) => {
      invalidate();
      void queries.invalidateQueries({ queryKey: adapterMoveKey(ref, move) });
    },
  });
}

/** useSetAdapterCredential replaces the write-only provider credential; nothing is enqueued. */
export function useSetAdapterCredential(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: { readonly adapter: string; readonly credential: string }) =>
      ok(setAdapterCredentialOp, {
        path: { ...ref, adapter: input.adapter },
        body: { credential: input.credential },
      }),
    onSettled: () => invalidate(),
  });
}

/**
 * useRevokeAdapterCredential destroys outbound custody. It needs no ceremony:
 * it narrows what Hikyo can do at the destination, and a later scrub may then
 * be impossible, which the surface states before the act.
 */
export function useRevokeAdapterCredential(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (adapter: string) => ok(revokeAdapterCredentialOp, { path: { ...ref, adapter } }),
    onSettled: () => invalidate(),
  });
}

export type DeleteAdapterInput = {
  readonly adapter: string;
  /** The explicit retain-or-prune decision over every target; no default. */
  readonly decision: 'prune' | 'retain';
};

/** useDeleteAdapter tombstones the adapter and every target under it. */
export function useDeleteAdapter(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (input: DeleteAdapterInput) =>
      parsed(deleteAdapterOp, {
        path: { ...ref, adapter: input.adapter },
        query: { keep_remote: input.decision === 'retain' },
      }),
    onSettled: () => invalidate(),
  });
}

/**
 * usePlanAdapterTarget computes the value-blind name plan. A plan that finds
 * unowned names in the way records a conflict artifact on the target, so the
 * detail is invalidated to show it for adoption.
 */
export function usePlanAdapterTarget(ref: ProjectRef) {
  const invalidate = useInvalidateAdapters(ref);
  return useMutation({
    mutationFn: (target: string) => parsed(planAdapterTargetOp, { path: { ...ref, target } }),
    onSettled: (_data, _error, target) => invalidate(target),
  });
}

/** useTestAdapterTarget probes provider version and destination identity; no values, no writes. */
export function useTestAdapterTarget(ref: ProjectRef) {
  return useMutation({
    mutationFn: (target: string) => parsed(testAdapterTargetOp, { path: { ...ref, target } }),
  });
}

/** moveStateText is the one sentence each move state means to an operator. */
export function moveStateText(state: AdapterMove['state']): string {
  switch (state) {
    case 'scrubbing':
      return 'Scrubbing the old route. Owned names are being removed before the switch.';
    case 'activating':
      return 'Activating the new route. The first converge is running.';
    case 'attention_required':
      return 'Activation was refused at the new origin. Resume with a working credential, or cancel to reconverge the old route.';
    case 'completed':
      return 'Completed. The adapter now serves the new origin.';
    case 'canceled':
      return 'Canceled. The old route was reconverged and nothing pending remains.';
  }
}

/** adapterRefusalText renders a refusal the way the surface speaks. */
export function adapterRefusalText<Failure>(error: Failure): string {
  if (error instanceof ApiError) {
    if (error.status === 401) {
      return 'Your session no longer authorises this; sign in again.';
    }
    if (error.status === 403) {
      return 'This act needs a fresh adapter reauthentication for every environment the adapter serves.';
    }
    if (error.status === 404) {
      return 'This adapter or target is not available, or you may not manage it.';
    }
    if (error.status === 409) {
      return error.detail ?? 'The current state of this target refuses the request.';
    }
    if (error.status === 400) {
      return error.detail ?? 'The request is not valid.';
    }
    if (error.status === 429) {
      return 'The instance is busy. Try again shortly.';
    }
  }
  return error instanceof Error ? error.message : 'The request failed.';
}

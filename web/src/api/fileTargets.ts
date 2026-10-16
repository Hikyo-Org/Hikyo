import type { CreateFileTargetRequest, UpdateFileTargetRequest } from '@hikyo/client';
import {
  createFileTargetOp, deleteFileTargetOp, getFileTargetOp, listFileTargetsOp,
  listServiceAccountsOp, updateFileTargetOp,
} from '@hikyo/operations';
import type { zFileTarget, zFileTargetList } from '@hikyo/zod';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { z } from 'zod';

import type { ProjectRef } from './adapters.ts';
import { ApiError, ok, parsed, transportRefusalText } from './client.ts';
import { assertSessionEpoch, captureSessionEpoch, SessionChangedError } from './sessionEpoch.ts';
import { metadataReadDenied as fileTargetReadDenied, useMetadataQuery } from './metadataQuery.ts';
export { metadataReadDenied as fileTargetReadDenied } from './metadataQuery.ts';
import { useSensitiveMutation } from './sensitiveMutation.ts';
import { requestInteger } from './requestInteger.ts';
import { commonRefusalText, statusText } from './statusText.ts';

export type FileTarget = z.infer<typeof zFileTarget>;
// Null retires data after an authoritative denial until a successful read.
// A later network failure must not resurrect the previously cached metadata.
type FileTargetListCache = z.infer<typeof zFileTargetList> | null;

export type FileTargetSelection = Omit<UpdateFileTargetRequest, 'expected_generation'>;

type ListKey = readonly ['file-targets', string, string];
type DetailKey = readonly ['file-target', string, string, string];
const listKey = (ref: ProjectRef): ListKey => ['file-targets', ref.org, ref.project];
const detailKey = (ref: ProjectRef, target: string): DetailKey => ['file-target', ref.org, ref.project, target];

export function useFileTargets(ref: ProjectRef) {
  return useMetadataQuery(listKey(ref), (signal) => parsed(listFileTargetsOp, { path: ref, signal }));
}

export function useFileTarget(ref: ProjectRef, target: string) {
  const queries = useQueryClient();
  return useQuery<FileTarget | null>({
    queryKey: detailKey(ref, target),
    queryFn: async ({ signal }) => {
      try {
        return await parsed(getFileTargetOp, { path: { ...ref, fileTarget: target }, signal });
      } catch (error) {
        if (!signal.aborted && fileTargetReadDenied(error)) {
          // A denied/deleted detail also retires its copy in the aggregate.
          // Refresh the list to learn whether other rows remain accessible.
          queries.setQueryData(detailKey(ref, target), null);
          const aggregate = queries.getQueryState<FileTargetListCache>(listKey(ref));
          // setQueryData marks a query successful. Never let detail pruning
          // clear an aggregate denial or replace its retired-data marker.
          if (aggregate?.data != null && !fileTargetReadDenied(aggregate.error)) {
            queries.setQueryData<FileTargetListCache>(listKey(ref), (previous) => previous == null ? previous : {
              ...previous, items: previous.items.filter((item) => item.id !== target),
            });
          }
          void queries.invalidateQueries({ queryKey: listKey(ref) });
        }
        throw error;
      }
    },
  });
}

/** Adapters are local project administration, including inside a workspace. */
export function useFileTargetAccounts(ref: ProjectRef) {
  return useMetadataQuery(['file-target-accounts', ref.org, ref.project],
    (signal) => parsed(listServiceAccountsOp, { path: ref, signal }));
}

export function useRefreshFileTargets(ref: ProjectRef) {
  const queries = useQueryClient();
  return (target?: string) => {
    void queries.invalidateQueries({ queryKey: listKey(ref) });
    if (target !== undefined) void queries.invalidateQueries({ queryKey: detailKey(ref, target) });
  };
}

/** Reconcile uncertain outcomes even after navigation retires the form. Only
 * the originating browser session may refresh its metadata; result delivery
 * remains fenced separately by useSensitiveMutation. */
async function reconcileFileTargetMutation<Result>(request: () => Promise<Result>, refresh: () => void): Promise<Result> {
  const epoch = captureSessionEpoch();
  try {
    return await request();
  } finally {
    refreshFileTargetsInSession(epoch, refresh);
  }
}

function refreshFileTargetsInSession(epoch: number, refresh: () => void): void {
  try {
    assertSessionEpoch(epoch);
  } catch (error) {
    if (error instanceof SessionChangedError) return;
    throw error;
  }
  refresh();
}

export function useCreateFileTarget(ref: ProjectRef) {
  const refresh = useRefreshFileTargets(ref);
  return useSensitiveMutation({
    mutationFn: (body: CreateFileTargetRequest) => reconcileFileTargetMutation(
      () => parsed(createFileTargetOp, { path: ref, body }), () => refresh(),
    ),
  });
}

export function useUpdateFileTarget(ref: ProjectRef) {
  const refresh = useRefreshFileTargets(ref);
  return useSensitiveMutation({
    mutationFn: (input: { target: string; generation: bigint; selection: FileTargetSelection }) => reconcileFileTargetMutation(
      () => parsed(updateFileTargetOp, {
        path: { ...ref, fileTarget: input.target },
        body: {
          ...input.selection,
          expected_generation: requestInteger(input.generation, 'expected generation', 1n),
        },
      }), () => refresh(input.target),
    ),
  });
}

export function useDeleteFileTarget(ref: ProjectRef) {
  const queries = useQueryClient();
  const refresh = useRefreshFileTargets(ref);
  return useSensitiveMutation({
    mutationFn: (target: string) => reconcileFileTargetMutation(async () => {
      await ok(deleteFileTargetOp, { path: { ...ref, fileTarget: target } });
      queries.removeQueries({ queryKey: detailKey(ref, target), exact: true });
    }, () => refresh(target)),
  });
}

export function fileTargetRefusalText(error: unknown): string {
  // Caller-safe validation detail is feature-specific; generic transport and
  // contract failures keep the canonical distinction from client.ts.
  if (!(error instanceof ApiError && error.status === 400)) {
    const generic = transportRefusalText(error);
    if (generic !== null) return generic;
  }
  return statusText(error, {
    401: commonRefusalText.sessionEnded,
    403: 'You need manage-adapters to inspect file targets, and manage-identities as well to change them.',
    404: 'This file target is no longer available in this project. Refresh the list.',
    400: (refusal) => refusal.detail ?? 'Check the target name, workload account and key selection.',
    409: (refusal) => refusal.detail ?? 'The target changed or its account is already bound. Refresh and review before retrying.',
    429: commonRefusalText.requests,
  }, 'The file-target request could not be completed. Try again.');
}

export function fileTargetConflict(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409;
}

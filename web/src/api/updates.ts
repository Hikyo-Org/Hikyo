import {
  getInstanceUpdateJobOp,
  getUpdateStatusOp,
  requestInstanceUpdateOp,
} from '@hikyo/operations';
import type { Client } from '@hikyo/runtime-core';
import { zInstanceUpdateJob, zUpdateStatus } from '@hikyo/zod';
import { useMutation, useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, parsed } from './client.ts';
import type { WorkspaceBearer } from './workspace.ts';
import { createWorkspaceClient } from './workspaceClient.ts';

export type UpdateStatus = z.infer<typeof zUpdateStatus>;
export type InstanceUpdateJob = z.infer<typeof zInstanceUpdateJob>;
export type RemoteUpdateProbe = {
  origin: string;
  status: UpdateStatus | null | undefined;
  error: Error | null;
};

export const updateStatusKey = ['update-status'];
export const updateStatusPollMs = 6 * 60 * 60 * 1_000;
const updateJobPollMs = 2_000;

async function readStatus(client?: Client): Promise<UpdateStatus | null> {
  try {
    return await parsed(getUpdateStatusOp, client === undefined ? {} : { client });
  } catch (error) {
    if (error instanceof ApiError && (error.status === 403 || error.status === 404)) {
      return null;
    }
    throw error;
  }
}

/**
 * The endpoint enforces instance-config. A uniform 403/404 is absence for an
 * ordinary member; release-source failures are quiet UI failures and retry on
 * the next six-hour poll or reload.
 */
export function useUpdateStatus(enabled: boolean): UseQueryResult<UpdateStatus | null> {
  return useQuery({
    queryKey: updateStatusKey,
    queryFn: () => readStatus(),
    enabled,
    staleTime: updateStatusPollMs,
    // Re-probe even after a uniform 403/404: an instance-config grant can be
    // added while this long-lived admin shell remains open.
    refetchInterval: updateStatusPollMs,
    retry: false,
  });
}

/** Reads update status directly from every connected remote workspace. */
export function useRemoteUpdateStatuses(
  workspaces: readonly WorkspaceBearer[],
): RemoteUpdateProbe[] {
  const queries = useQueries({
    queries: workspaces.map((workspace) => ({
      queryKey: ['remote-update-status', workspace.origin, workspace.session],
      queryFn: () => readStatus(createWorkspaceClient(workspace.origin)),
      staleTime: updateStatusPollMs,
      refetchInterval: updateStatusPollMs,
      retry: false,
    })),
  });
  return queries.flatMap((query, index) => {
    const workspace = workspaces[index];
    return workspace === undefined
      ? []
      : [{ origin: workspace.origin, status: query.data, error: query.error }];
  });
}

export function useRequestRemoteUpdate() {
  return useMutation({
    mutationFn: async ({ origin, version }: { origin: string; version: string }) =>
      parsed(requestInstanceUpdateOp, {
        client: createWorkspaceClient(origin),
        body: { version },
      }),
  });
}

export function useRemoteUpdateJob(origin: string, job: string | undefined) {
  return useQuery({
    queryKey: ['remote-update-job', origin, job ?? ''],
    queryFn: () =>
      parsed(getInstanceUpdateJobOp, {
        client: createWorkspaceClient(origin),
        path: { job: job ?? '' },
      }),
    enabled: job !== undefined,
    refetchInterval: (query) => {
      if (query.state.status === 'error' && query.state.data === undefined) return false;
      const state = query.state.data?.state;
      return state === undefined || state === 'queued' || state === 'running'
        ? updateJobPollMs
        : false;
    },
    retry: false,
  });
}

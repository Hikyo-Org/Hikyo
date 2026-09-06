import {
  getInstanceUpdateJobOp,
  getMetaOp,
  getUpdateStatusOp,
} from '@hikyo/operations';
import type { Client } from '@hikyo/runtime-core';
import { zInstanceUpdateJob, zUpdateStatus } from '@hikyo/zod';
import { useMutation, useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, parsed, parsedPick } from './client.ts';
import type { WorkspaceBearer } from './workspace.ts';
import { createWorkspaceClient } from './workspaceClient.ts';

export const remoteApplyDisabledReason =
  'Remote apply is disabled because the legacy updater cannot prove migration-safe rollback. Follow the manual signed-bundle upgrade procedure.';

export type UpdateStatus = z.infer<typeof zUpdateStatus>;
export type InstanceUpdateJob = z.infer<typeof zInstanceUpdateJob>;

/**
 * The three lifecycle outcomes a consumer renders differently: still working,
 * clean success, or a terminal failure that needs operator attention.
 * `rolled-back` and `rollback-failed` are both failures, the instance did not
 * reach the requested version, so they collapse into `failed` and carry the
 * diagnostic `failure_code` through. Centralizing the six-state → three-outcome
 * mapping here keeps the enum from drifting out of the contract in the view.
 */
export type UpdateJobOutcome = {
  kind: 'running' | 'succeeded' | 'failed';
  failureCode?: string;
};

export function updateJobOutcome(job: InstanceUpdateJob): UpdateJobOutcome {
  switch (job.state) {
    case 'queued':
    case 'running':
      return { kind: 'running' };
    case 'succeeded':
      return { kind: 'succeeded' };
    case 'failed':
    case 'rolled-back':
    case 'rollback-failed':
      return { kind: 'failed', failureCode: job.failure_code };
  }
}

/**
 * Whether to show the "job status could not be read" alert. A refetch error can
 * land while the last successful read, a terminal `failed` job, is still
 * cached; in that case the failure alert already tells the operator the job is
 * broken, so the read-error alert is suppressed to avoid a double-up. It shows
 * only when there is a live read error and no terminal-failure outcome to
 * supersede it.
 */
export function jobReadErrorVisible(isError: boolean, job: InstanceUpdateJob | undefined): boolean {
  return isError && (job === undefined || updateJobOutcome(job).kind !== 'failed');
}
export type RemoteUpdateProbe = {
  origin: string;
  status: UpdateStatus | null | undefined;
  error: Error | null;
};

const updateStatusKey = ['update-status'];
const serverVersionKey = ['server-version'];
const updateStatusPollMs = 6 * 60 * 60 * 1_000;
const updateJobPollMs = 2_000;

/**
 * useServerVersion reads the running build's version from the contract meta
 * endpoint (`server_version`, `dev` for an unreleased build). It is the same
 * fact every caller of `/api/v1/meta` already trusts, narrowed to the one
 * field the chrome shows. The version is fixed for the life of a process, an
 * applied update reloads the SPA, so it never goes stale in-session.
 */
export function useServerVersion(): UseQueryResult<string> {
  return useQuery({
    queryKey: serverVersionKey,
    queryFn: async () =>
      (await parsedPick(getMetaOp, {}, { server_version: true })).server_version,
    staleTime: Infinity,
  });
}

async function readStatus(client?: Client): Promise<UpdateStatus | null> {
  try {
    const status = await parsed(getUpdateStatusOp, client === undefined ? {} : { client });
    // An older remote may still advertise its unsafe legacy helper. Preserve
    // release metadata while refusing to offer that mutation from this client.
    return { ...status, apply_supported: false, apply_error: remoteApplyDisabledReason };
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
    mutationFn: async (_request: { origin: string; version: string }): Promise<InstanceUpdateJob> => {
      throw new Error(remoteApplyDisabledReason);
    },
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
  });
}

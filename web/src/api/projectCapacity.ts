import { getProjectCapacityOp, setProjectCapacityOp } from '@hikyo/operations';
import { zProjectCapacity, zSetProjectCapacityRequest } from '@hikyo/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, parsed } from './client.ts';
import { metadataReadDenied, useMetadataQuery } from './metadataQuery.ts';
import { requestInteger } from './requestInteger.ts';
import { useTransport } from './transport.tsx';

export type ProjectCapacity = z.infer<typeof zProjectCapacity>;
export type SetProjectCapacity = z.infer<typeof zSetProjectCapacityRequest>;

export const projectCapacityKey = (org: string, project: string) => ['project-capacity', org, project];

/** Metadata only. The parent mounts this surface only with project-settings permission. */
export function useProjectCapacity(org: string, project: string) {
  const transport = useTransport();
  return useMetadataQuery(projectCapacityKey(org, project),
    (signal) => parsed(getProjectCapacityOp, { path: { org, project }, signal, ...transport }),
    org !== '' && project !== '');
}

export function useSetProjectCapacity(org: string, project: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  const key = projectCapacityKey(org, project);
  return useMutation({
    mutationFn: (body: SetProjectCapacity) => parsed(setProjectCapacityOp, {
      path: { org, project }, body: { ...body, expected_revision: requestInteger(body.expected_revision, 'capacity revision', 0n) }, ...transport,
    }),
    onSuccess: (result) => { queries.setQueryData(key, result); },
    onError: (error) => {
      if (metadataReadDenied(error)) {
        queries.setQueryData(key, null);
        return queries.invalidateQueries({ queryKey: ['project', org, project] });
      }
      if (error instanceof ApiError && error.status === 409) {
        return queries.invalidateQueries({ queryKey: key });
      }
    },
  });
}

/** Server details are not displayed in the policy editor. */
export function projectCapacityFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400: return 'Enter a whole number within the instance maximum, or inherit the instance default.';
      case 401: return 'Your session ended. Sign in again to continue.';
      case 403:
      case 404: return 'Key capacity is not available to this session. Your project-settings permission may have changed.';
      case 409: return 'The project or instance capacity policy changed. Load the current policy, review your draft, and save again.';
    }
  }
  return 'Key capacity could not be saved. Refresh the current policy before retrying; the outcome may be unconfirmed.';
}

import { getUpdateStatusOp } from '@hikyo/operations';
import { zUpdateStatus } from '@hikyo/zod';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, parsed } from './client.ts';

export type UpdateStatus = z.infer<typeof zUpdateStatus>;

export const updateStatusKey = ['update-status'] as const;
export const updateStatusPollMs = 6 * 60 * 60 * 1_000;

/**
 * The endpoint enforces instance-config. A uniform 403/404 is absence for an
 * ordinary member; release-source failures are quiet UI failures and retry on
 * the next six-hour poll or reload.
 */
export function useUpdateStatus(enabled: boolean): UseQueryResult<UpdateStatus | null> {
  return useQuery({
    queryKey: updateStatusKey,
    queryFn: async () => {
      try {
        return await parsed(getUpdateStatusOp, {});
      } catch (error) {
        if (error instanceof ApiError && (error.status === 403 || error.status === 404)) {
          return null;
        }
        throw error;
      }
    },
    enabled,
    staleTime: updateStatusPollMs,
    // Re-probe even after a uniform 403/404: an instance-config grant can be
    // added while this long-lived admin shell remains open.
    refetchInterval: updateStatusPollMs,
    retry: false,
  });
}

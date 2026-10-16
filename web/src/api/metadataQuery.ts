import { queryOptions, useQuery, type QueryKey } from '@tanstack/react-query';

import { ApiError } from './client.ts';

export function metadataReadDenied(error: unknown): boolean {
  return error instanceof ApiError && [401, 403, 404].includes(error.status);
}

/** Retire previously readable metadata on an authoritative refusal. Null is a
 * cache-only marker: later network failures cannot restore the old data, and
 * only a successful read replaces it. Request authorization stays server-owned.
 * Every reader of a shared key must use these options and accept nullable data.
 */
export function metadataQueryOptions<Data>(
  queryKey: QueryKey, read: (signal: AbortSignal) => Promise<Data>, enabled = true,
) {
  return queryOptions<Data | null>({
    queryKey,
    enabled,
    queryFn: async ({ signal, client: queries }) => {
      try {
        return await read(signal);
      } catch (error) {
        // TanStack discards cancelled results, but manual writes outlive its
        // result delivery. A retired request must not erase a newer success.
        if (!signal.aborted && metadataReadDenied(error)) queries.setQueryData(queryKey, null);
        throw error;
      }
    },
  });
}

export function useMetadataQuery<Data>(
  queryKey: QueryKey, read: (signal: AbortSignal) => Promise<Data>, enabled = true,
) {
  return useQuery(metadataQueryOptions(queryKey, read, enabled));
}

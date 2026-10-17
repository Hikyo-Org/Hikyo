import type { ReactNode } from 'react';

import type { useProjects } from '../api/settings.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';

/** Shared directory feedback for the landing page and both shell lists. */
export function ProjectQueryFeedback({
  query,
  empty,
  className,
}: {
  query: ReturnType<typeof useProjects>;
  empty: ReactNode;
  className?: string;
}) {
  // A retired cache entry is unavailable even if a cache write temporarily
  // marked it successful. Only an actual list can establish an empty result.
  if (query.isError || query.data === null) {
    return (
      <Alert action={
        <Button
          type="button"
          readOnlySafe
          disabled={query.isFetching}
          onClick={() => { void query.refetch(); }}
        >
          {query.isFetching ? 'Retrying projects…' : 'Retry projects'}
        </Button>
      }>
        Projects could not be loaded. Try again.
      </Alert>
    );
  }
  if (query.isPending) {
    return <p className={className} role="status">Loading projects…</p>;
  }
  return query.data.items.length === 0
    ? <p className={className} role="status">{empty}</p>
    : null;
}

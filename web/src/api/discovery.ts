import { getMetaOp } from '@hikyo/operations';
import { useQuery } from '@tanstack/react-query';

import { parsedPick } from './client.ts';

/** The local persisted identity, also checked by the server's pinned add operation. */
export function useInstanceIdentity() {
  return useQuery({
    queryKey: ['instance-identity'],
    queryFn: async () => (await parsedPick(getMetaOp, {}, { instance_identity: true })).instance_identity ?? null,
  });
}

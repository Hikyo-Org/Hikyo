import { getProjectRetentionOp, listEnvironmentsOp } from '@hikyo/operations';
import type { Client } from '@hikyo/runtime-core';

import { parsed } from './client.ts';
import { environmentsKey, projectRetentionKey, type MatrixRef } from './keys.ts';

/** Shared resource boundaries; callers choose their transport and read gate. */
export function environmentListQueryOptions(ref: MatrixRef, client?: Client) {
  return {
    queryKey: environmentsKey(ref),
    queryFn: () => parsed(listEnvironmentsOp, { path: { org: ref.org, project: ref.project }, client }),
    enabled: ref.org !== '' && ref.project !== '',
  };
}

export function projectRetentionQueryOptions(ref: MatrixRef, client?: Client) {
  return {
    queryKey: projectRetentionKey(ref),
    queryFn: () => parsed(getProjectRetentionOp, { path: { org: ref.org, project: ref.project }, client }),
    enabled: ref.org !== '' && ref.project !== '',
  };
}

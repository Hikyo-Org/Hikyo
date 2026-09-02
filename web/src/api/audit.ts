import { queryEnvAuditOp, queryOrgAuditOp, queryProjectAuditOp } from '@hikyo/operations';
import { zAuditPage } from '@hikyo/zod';
import { useInfiniteQuery, type UseInfiniteQueryResult, type InfiniteData } from '@tanstack/react-query';
import type { z } from 'zod';

import { parsed } from './client.ts';

export type AuditPage = z.infer<typeof zAuditPage>;
export type AuditEvent = AuditPage['items'][number];

/**
 * AuditScope names the trail being read (#572). The org trail holds every
 * event; the project and environment trails are the SERVER's scoped slices,
 * each behind its own `audit-read` proof at that scope, so a project-level
 * holder reads the project trail without an org-level grant. Environment is
 * only meaningful under a project.
 */
export type AuditScope = {
  readonly org: string;
  readonly project?: string;
  readonly environment?: string;
};

/**
 * exportPath is the scope's export route, spelled as one literal per route so
 * the parity registry's path evidence (api/parity.yaml `reach: path`) can see
 * each of the three operations reached from this module.
 */
function exportPath(scope: AuditScope): string {
  const org = encodeURIComponent(scope.org);
  if (scope.project === undefined || scope.project === '') {
    return `/api/v1/orgs/${org}/audit/export`;
  }
  const project = encodeURIComponent(scope.project);
  if (scope.environment === undefined || scope.environment === '') {
    return `/api/v1/orgs/${org}/projects/${project}/audit/export`;
  }
  const environment = encodeURIComponent(scope.environment);
  return `/api/v1/orgs/${org}/projects/${project}/environments/${environment}/audit/export`;
}

/**
 * The audit outcomes, in the closed order the contract's enum declares them.
 * Kept here so the filter control renders the same set the server validates.
 */
export const AUDIT_OUTCOMES = [
  'intent',
  'success',
  'denied',
  'failure',
  'unknown',
  'disconnected',
] as const;
export type AuditOutcome = (typeof AUDIT_OUTCOMES)[number];

/**
 * AuditFilter is the browser's view of the query parameters. Every field is
 * optional; an empty string means "no filter" and is dropped before the
 * request so it never reaches the URL. `from`/`to` are datetime-local strings.
 */
export type AuditFilter = {
  readonly from: string;
  readonly to: string;
  readonly actor: string;
  readonly operation: string;
  readonly outcome: string;
  readonly objectType: string;
  readonly objectId: string;
  readonly correlationId: string;
};

export const emptyAuditFilter: AuditFilter = {
  from: '',
  to: '',
  actor: '',
  operation: '',
  outcome: '',
  objectType: '',
  objectId: '',
  correlationId: '',
};

/** How many rows a page scans; the server clamps its own ceiling. */
const AUDIT_PAGE_LIMIT = 100;

/**
 * auditQuery renders the filter as the operation's query object, dropping every
 * empty field so unset filters never appear in the URL. A datetime-local value
 * carries no zone; treat it as UTC so the bound the operator picked is the
 * bound the server compares against, not one shifted by the browser's zone.
 */
function auditQuery(filter: AuditFilter): Record<string, string> {
  const query: Record<string, string> = {};
  if (filter.from !== '') {
    query['from'] = localDatetimeToUtc(filter.from);
  }
  if (filter.to !== '') {
    query['to'] = localDatetimeToUtc(filter.to);
  }
  if (filter.actor !== '') {
    query['actor'] = filter.actor;
  }
  if (filter.operation !== '') {
    query['operation'] = filter.operation;
  }
  if (filter.outcome !== '') {
    query['outcome'] = filter.outcome;
  }
  if (filter.objectType !== '') {
    query['object_type'] = filter.objectType;
  }
  if (filter.objectId !== '') {
    query['object_id'] = filter.objectId;
  }
  if (filter.correlationId !== '') {
    query['correlation_id'] = filter.correlationId;
  }
  return query;
}

/**
 * A `datetime-local` value read in the browser's own zone and converted to UTC.
 * The events render in that same local zone (see Audit.tsx), so the boundary the
 * operator picks is the boundary the server compares against, not one shifted by
 * their offset.
 */
export function localDatetimeToUtc(local: string): string {
  return new Date(local).toISOString();
}

/** The trail cursor: the resume position and the pinned session ceiling. */
type AuditCursor = { readonly after: bigint; readonly ceiling: bigint };

/**
 * safeSeq narrows an int64 seq to a JS number for a request parameter, refusing
 * a value past 2^53 rather than silently rounding it. Seq is a monotonic row
 * counter, so this is unreachable in practice; the guard is what makes it
 * unreachable in principle instead of a silent skip. The response side keeps
 * full int64 precision (zod bigint).
 */
function safeSeq(seq: bigint): number {
  if (seq > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error(`audit seq ${seq} exceeds the safe integer range`);
  }
  return Number(seq);
}

export const auditPageKey = (scope: AuditScope, filter: AuditFilter) =>
  ['audit', scope.org, scope.project ?? '', scope.environment ?? '', filter] as const;

/** queryScoped calls the one operation the scope addresses; the three share a response contract. */
function queryScoped(scope: AuditScope, query: Record<string, string | number>): Promise<AuditPage> {
  if (scope.project === undefined || scope.project === '') {
    return parsed(queryOrgAuditOp, { path: { org: scope.org }, query });
  }
  if (scope.environment === undefined || scope.environment === '') {
    return parsed(queryProjectAuditOp, { path: { org: scope.org, project: scope.project }, query });
  }
  return parsed(queryEnvAuditOp, {
    path: { org: scope.org, project: scope.project, environment: scope.environment },
    query,
  });
}

/**
 * useAuditTrail pages the org trail. Each page SCANS up to the limit and RETURNS
 * the filtered subset; `getNextPageParam` follows `next_after_seq` and stops
 * only when the server reports the scan reached the trail's end. A sparse filter
 * therefore yields more pages with few or no items — deliberately: the caller
 * drives "load more" so the trail is never walked unbounded, and every page it
 * reads writes its own audit.query event.
 */
export function useAuditTrail(
  scope: AuditScope,
  filter: AuditFilter,
): UseInfiniteQueryResult<InfiniteData<AuditPage>, unknown> {
  return useInfiniteQuery({
    queryKey: auditPageKey(scope, filter),
    // The cursor is the trail's int64 seq (bigint end to end). The first page
    // carries no ceiling; the server pins one and returns it as upper_seq, which
    // every later page echoes as to_seq so paging is stable across concurrent
    // writes. Request params are plain numbers in the generated client, so the
    // cursor is narrowed — guarded — only at the call.
    initialPageParam: { after: 0n, ceiling: 0n } as AuditCursor,
    queryFn: ({ pageParam }) =>
      queryScoped(scope, {
        ...auditQuery(filter),
        after_seq: safeSeq(pageParam.after),
        limit: AUDIT_PAGE_LIMIT,
        ...(pageParam.ceiling === 0n ? {} : { to_seq: safeSeq(pageParam.ceiling) }),
      }),
    getNextPageParam: (last): AuditCursor | undefined =>
      last.exhausted ? undefined : { after: last.next_after_seq, ceiling: last.upper_seq },
    retry: false,
  });
}

/**
 * auditExportUrl is the JSONL download URL for the current filter. It is a plain
 * same-origin GET so the browser streams the bytes to disk under the session
 * cookie — nothing is buffered in the SPA and no token rides the URL. Paging
 * fields are omitted: the export streams the whole filtered slice.
 */
export function auditExportUrl(scope: AuditScope, filter: AuditFilter): string {
  const params = new URLSearchParams(auditQuery(filter));
  const query = params.toString();
  const base = exportPath(scope);
  return query === '' ? base : `${base}?${query}`;
}

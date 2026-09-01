import { queryOrgAuditOp } from '@hikyo/operations';
import { zAuditPage } from '@hikyo/zod';
import { useInfiniteQuery, type UseInfiniteQueryResult, type InfiniteData } from '@tanstack/react-query';
import type { z } from 'zod';

import { parsed } from './client.ts';

export type AuditPage = z.infer<typeof zAuditPage>;
export type AuditEvent = AuditPage['items'][number];

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

/** A `datetime-local` value (no zone) read as UTC, in RFC 3339. */
export function localDatetimeToUtc(local: string): string {
  // Appending `Z` interprets the wall-clock value the operator typed as UTC.
  const seconds = local.length === 16 ? `${local}:00` : local;
  return `${seconds}Z`;
}

export const auditPageKey = (org: string, filter: AuditFilter) =>
  ['audit', org, filter] as const;

/**
 * useAuditTrail pages the org trail. Each page SCANS up to the limit and RETURNS
 * the filtered subset; `getNextPageParam` follows `next_after_seq` and stops
 * only when the server reports the scan reached the trail's end. A sparse filter
 * therefore yields more pages with few or no items — deliberately: the caller
 * drives "load more" so the trail is never walked unbounded, and every page it
 * reads writes its own audit.query event.
 */
export function useAuditTrail(
  org: string,
  filter: AuditFilter,
): UseInfiniteQueryResult<InfiniteData<AuditPage>, unknown> {
  return useInfiniteQuery({
    queryKey: auditPageKey(org, filter),
    // The cursor is the trail's int64 seq, so it is a bigint end to end; the
    // request param is a plain number in the generated client, so it is
    // narrowed only at the call. Seq values are far inside 2^53.
    initialPageParam: 0n,
    queryFn: ({ pageParam }) =>
      parsed(queryOrgAuditOp, {
        path: { org },
        query: { ...auditQuery(filter), after_seq: Number(pageParam), limit: AUDIT_PAGE_LIMIT },
      }),
    getNextPageParam: (last) => (last.exhausted ? undefined : last.next_after_seq),
    retry: false,
  });
}

/**
 * auditExportUrl is the JSONL download URL for the current filter. It is a plain
 * same-origin GET so the browser streams the bytes to disk under the session
 * cookie — nothing is buffered in the SPA and no token rides the URL. Paging
 * fields are omitted: the export streams the whole filtered slice.
 */
export function auditExportUrl(org: string, filter: AuditFilter): string {
  const params = new URLSearchParams(auditQuery(filter));
  const query = params.toString();
  const base = `/api/v1/orgs/${encodeURIComponent(org)}/audit/export`;
  return query === '' ? base : `${base}?${query}`;
}

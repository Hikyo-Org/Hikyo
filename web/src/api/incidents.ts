import {
  attestIncidentItemOp,
  closeIncidentOp,
  getIncidentOp,
  listIncidentItemsOp,
  openIncidentOp,
  resumeIncidentOp,
} from '@hikyo/operations';
import { zIncident, zIncidentItem, zIncidentItemList, zIncidentResumeResult } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ok, parsed } from './client.ts';
import { commonRefusalText, statusText } from './statusText.ts';

/**
 * The incident containment ledger (#1039). Everything shown is the server's own
 * answer: the item's outcome and reason from the ledger, and a rotation item's
 * state read live by the server when the page is built. Nothing here decides
 * authority or infers a state the server did not name.
 */

export type Incident = z.infer<typeof zIncident>;
export type IncidentItem = z.infer<typeof zIncidentItem>;
export type IncidentItemList = z.infer<typeof zIncidentItemList>;
export type IncidentResumeResult = z.infer<typeof zIncidentResumeResult>;

/** What an incident can be opened on, in the order the form offers them. */
export const INCIDENT_SCOPES = ['account', 'org', 'project', 'adapter'] as const;
export type IncidentScope = (typeof INCIDENT_SCOPES)[number];

export type IncidentOpenDraft = {
  readonly scope: IncidentScope;
  readonly principal: string;
  readonly org: string;
  readonly project: string;
  readonly adapter: string;
};

const incidentKey = (id: string) => ['incident', id] as const;
const itemsKey = (id: string) => ['incident-items', id] as const;
const ITEM_PAGE = 500;

/** The incident with its outcome counts; a disabled query stays idle until an id is named. */
export function useIncident(id: string): UseQueryResult<Incident> {
  return useQuery({
    queryKey: incidentKey(id),
    queryFn: () => parsed(getIncidentOp, { path: { incident: id } }),
    enabled: id !== '',
  });
}

/** Every item of the incident, read page by page so the table never silently truncates. */
export function useIncidentItems(id: string): UseQueryResult<readonly IncidentItem[]> {
  return useQuery({
    queryKey: itemsKey(id),
    queryFn: async () => {
      const all: IncidentItem[] = [];
      let after: string | undefined;
      for (;;) {
        const page = await parsed(listIncidentItemsOp, {
          path: { incident: id },
          query: after === undefined ? { limit: ITEM_PAGE } : { limit: ITEM_PAGE, after },
        });
        all.push(...page.items);
        if (page.next_after === undefined || page.items.length === 0) return all;
        after = page.next_after;
      }
    },
    enabled: id !== '',
  });
}

function openBody(draft: IncidentOpenDraft) {
  switch (draft.scope) {
    case 'account':
      return { scope_kind: 'account', principal: draft.principal };
    case 'org':
      return { scope_kind: 'org', org: draft.org };
    case 'project':
      return { scope_kind: 'project', org: draft.org, project: draft.project };
    case 'adapter':
      return { scope_kind: 'adapter', org: draft.org, project: draft.project, adapter: draft.adapter };
  }
}

export function useOpenIncident() {
  return useMutation({
    mutationFn: (draft: IncidentOpenDraft) => parsed(openIncidentOp, { body: openBody(draft) }),
  });
}

/** Resume, attest and close each refresh the incident and its items. */
export function useIncidentActions(id: string) {
  const queries = useQueryClient();
  const refresh = () =>
    Promise.all([
      queries.invalidateQueries({ queryKey: incidentKey(id) }),
      queries.invalidateQueries({ queryKey: itemsKey(id) }),
    ]);
  return {
    resume: useMutation({
      mutationFn: () => parsed(resumeIncidentOp, { path: { incident: id } }),
      onSettled: refresh,
    }),
    attest: useMutation({
      mutationFn: (item: string) => ok(attestIncidentItemOp, { path: { incident: id, item } }),
      onSettled: refresh,
    }),
    close: useMutation({
      mutationFn: (withResiduals: boolean) =>
        parsed(closeIncidentOp, { path: { incident: id }, body: { with_residuals: withResiduals } }),
      onSettled: refresh,
    }),
  };
}

/** The refusal wording; a 404 is deliberately the same for an unknown and a forbidden incident. */
export function incidentFailureText(error: unknown): string {
  return statusText(
    error,
    {
      400: 'The incident scope is not valid.',
      401: commonRefusalText.sessionEnded,
      404: 'No such incident, or you may not work it.',
      409: (e) => e.detail ?? 'The incident changed or is closed. Reload and try again.',
      429: commonRefusalText.requests,
    },
    'The incident action could not be completed.',
  );
}

/** How an item's state reads: refused and not yet run are different things. */
export function itemStateLabel(item: IncidentItem): string {
  if (item.refused) return 'Refused by its owner';
  switch (item.outcome) {
    case 'pending':
      return 'Not yet run';
    case 'done':
      return 'Done';
    case 'unknown':
      return 'Unconfirmed';
    case 'attested':
      return 'Attested';
    case 'blocked':
      return 'Blocked';
    case 'out_of_scope':
      return 'Out of scope';
  }
}

/** True for the outcomes resume works again. */
export function isOpenItem(item: IncidentItem): boolean {
  return item.outcome === 'pending' || item.outcome === 'unknown' || item.outcome === 'blocked';
}

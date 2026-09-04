import {
  getDefinitionsSettingsOp,
  setDefinitionsSettingsOp,
} from '@hikyo/operations';
import { zDefinitionsSettings, zSetDefinitionsSettingsRequest } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { parsed } from './client.ts';
import { useTransport, type TransportOptions } from './transport.tsx';

/**
 * Definitions governance is project policy, not repository integration.
 *
 * Hikyo stores the selected authority mode and optional labels supplied by the
 * last apply. It neither knows nor renders a repository URL, and every body is
 * parsed at this boundary before settings UI can rely on it.
 */

// Normative text: docs/spec/ui-spec.md § Git-mode.
export const GIT_DEFINITIONS_NOTICE =
  'Definitions for this project are managed in Git — changes arrive through `definitions plan` / `definitions apply`.';

export type DefinitionsSettings = z.infer<typeof zDefinitionsSettings>;
export type DefinitionsSource = DefinitionsSettings['definitions_source'];

const definitionsSettingsKey = (org: string, project: string) =>
  ['definitions-settings', org, project];

/** Parse a DOM selector value instead of asserting that it is a contract enum. */
export function parseDefinitionsSource(value: string): DefinitionsSource {
  return zSetDefinitionsSettingsRequest.shape.definitions_source.parse(value);
}

/**
 * Shared query options keep the resource key and parsed response together.
 *
 * `transport` is the workspace seam (#71): definitions settings are product
 * data, so in a workspace this call MUST route through the remote's client, or
 * it hits the viewing server — a leak the two-instance e2e route-guard catches.
 * Defaults to the same-origin singleton for non-hook callers.
 */
export function definitionsSettingsQueryOptions(
  org: string,
  project: string,
  transport: TransportOptions = {},
) {
  return {
    queryKey: definitionsSettingsKey(org, project),
    queryFn: () =>
      parsed(getDefinitionsSettingsOp, { path: { org, project }, ...transport }),
    enabled: org !== '' && project !== '',
  };
}

export function useDefinitionsSettings(
  org: string,
  project: string,
): UseQueryResult<DefinitionsSettings> {
  const transport = useTransport();
  return useQuery(definitionsSettingsQueryOptions(org, project, transport));
}

export function useSetDefinitionsSettings(org: string, project: string) {
  const queries = useQueryClient();
  const transport = useTransport();
  return useMutation({
    mutationFn: (definitionsSource: DefinitionsSource) =>
      parsed(setDefinitionsSettingsOp, {
          path: { org, project },
          body: { definitions_source: definitionsSource },
          ...transport,
        }),
    onSuccess: () =>
      queries.invalidateQueries({ queryKey: definitionsSettingsKey(org, project) }),
  });
}

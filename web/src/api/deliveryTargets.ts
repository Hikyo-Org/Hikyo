import { getMetaOp, listDeliveryTargetsOp } from '@hikyo/operations';
import type { zDeliveryTarget, zDeliveryTargetList, zDeliveryTargetPrincipal } from '@hikyo/zod';
import { useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, parsed, parsedPick } from './client.ts';

/**
 * Delivery-target condition reports, as the machine-access Kubernetes tab reads
 * them (condition-reporting ADR D2, D5, D7, D11; #790).
 *
 * Two layers the server keeps apart and this module never merges: per
 * reporting principal, what the SERVER observed (its last authenticated
 * delivery fetch, and the quota notice); per target, what its CONTROLLER
 * asserted, with a state the server derived at read time. Nothing here is a
 * health check Hikyo performed.
 */

export type DeliveryTargetList = z.infer<typeof zDeliveryTargetList>;
export type DeliveryTarget = z.infer<typeof zDeliveryTarget>;
export type DeliveryTargetPrincipal = z.infer<typeof zDeliveryTargetPrincipal>;

type EnvRef = { readonly id: string; readonly name: string };

/** One readable environment's listing. */
export type EnvironmentReports = { readonly environment: EnvRef; readonly list: DeliveryTargetList };

/**
 * Whether the server accepts delivery-target reports at all: it advertises
 * `delivery-target-report/<vocabulary>` in `/meta` (D10). A server that does
 * not is never asked for a list, and says so rather than "no reports".
 */
export type ReportingSupport = 'supported' | 'unsupported' | 'pending' | 'failed';

export type DeliveryTargetsView = {
  readonly support: ReportingSupport;
  /** Readable environments only: an unreadable one is absent, never redacted (D7). */
  readonly reports: readonly EnvironmentReports[];
  /** Environments whose listing failed for any reason other than "not readable". */
  readonly failures: readonly EnvRef[];
  readonly isPending: boolean;
};

type ListResult = Pick<UseQueryResult<DeliveryTargetList>, 'data' | 'error' | 'isPending'>;

/**
 * combineDeliveryTargets folds the per-environment listings into one view.
 *
 * A 404 is the uniform nonexistent answer for an environment the caller cannot
 * read, so that environment contributes nothing: its rows are absent, not an
 * error (D7). Every other failure is kept, because a listing that failed is
 * `unknown`, never "no reports".
 */
export function combineDeliveryTargets(
  environments: readonly EnvRef[],
  results: readonly ListResult[],
): Omit<DeliveryTargetsView, 'support'> {
  const reports: EnvironmentReports[] = [];
  const failures: EnvRef[] = [];
  environments.forEach((environment, index) => {
    const result = results[index];
    if (result?.data !== undefined) {
      reports.push({ environment, list: result.data });
    } else if (result?.error != null && !(result.error instanceof ApiError && result.error.status === 404)) {
      failures.push(environment);
    }
  });
  return { reports, failures, isPending: results.some((r) => r.isPending) };
}

/** reportingSupport reads the advertised protocols; any vocabulary counts. */
export function reportingSupport(capabilities: readonly string[]): 'supported' | 'unsupported' {
  return capabilities.some((c) => c.startsWith('delivery-target-report/')) ? 'supported' : 'unsupported';
}

/**
 * useReportingSupport reads `/meta` once for every consumer (one query key):
 * the Kubernetes tab's listings and the grant dialog's report checkbox.
 */
export function useReportingSupport(): ReportingSupport {
  const meta = useQuery({
    queryKey: ['meta', 'protocol-capabilities'] as const,
    queryFn: async () =>
      (await parsedPick(getMetaOp, {}, { protocol_capabilities: true })).protocol_capabilities,
    // Fixed for the life of the process, like the server version.
    staleTime: Infinity,
  });
  return meta.isSuccess ? reportingSupport(meta.data) : meta.isError ? 'failed' : 'pending';
}

/**
 * useDeliveryTargets lists the reports in every environment of a project,
 * once `/meta` says the server accepts them. Reports are environment-scoped
 * (`read` on the environment, D7), so the project-scoped surface fans out like
 * the lease listing does.
 */
export function useDeliveryTargets(
  p: { readonly org: string; readonly project: string },
  environments: readonly EnvRef[],
): DeliveryTargetsView {
  const support = useReportingSupport();
  const lists = useQueries({
    queries: environments.map((env) => ({
      queryKey: ['delivery-targets', p.org, p.project, env.id] as const,
      queryFn: () =>
        parsed(listDeliveryTargetsOp, {
          path: { org: p.org, project: p.project, environment: env.id },
        }),
      enabled: support === 'supported',
    })),
    combine: (results) => combineDeliveryTargets(environments, results),
  });
  // A disabled query stays pending forever, so only a supported server's
  // listings can hold the view in flight.
  return {
    ...lists,
    support,
    isPending: support === 'pending' || (support === 'supported' && lists.isPending),
  };
}

/** One target with the environment it reports on. */
export type TargetRow = { readonly environment: EnvRef; readonly target: DeliveryTarget };

/** Targets sharing one cluster and namespace (D11). */
export type TargetGroup = {
  readonly cluster: string;
  readonly namespace: string;
  readonly rows: readonly TargetRow[];
};

/** groupTargets groups by cluster, then namespace, then orders by name. */
export function groupTargets(reports: readonly EnvironmentReports[]): TargetGroup[] {
  const groups = new Map<string, { cluster: string; namespace: string; rows: TargetRow[] }>();
  for (const { environment, list } of reports) {
    for (const target of list.targets) {
      const { cluster_id: cluster, namespace } = target.target;
      const key = `${cluster} ${namespace}`;
      const group = groups.get(key) ?? { cluster, namespace, rows: [] };
      group.rows.push({ environment, target });
      groups.set(key, group);
    }
  }
  const byText = (a: string, b: string) => a.localeCompare(b);
  return [...groups.values()]
    .sort((a, b) => byText(a.cluster, b.cluster) || byText(a.namespace, b.namespace))
    .map((group) => ({
      ...group,
      rows: group.rows.toSorted((a, b) => byText(a.target.target.name, b.target.target.name)),
    }));
}

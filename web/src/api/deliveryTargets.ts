import { listDeliveryTargetsOp } from '@hikyo/operations';
import type { zDeliveryTarget, zDeliveryTargetList, zDeliveryTargetPrincipal } from '@hikyo/zod';
import { useQueries, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, parsed } from './client.ts';
import { remoteStateText } from './remotes.ts';
import { useTransport } from './transport.tsx';
import { WorkspaceError } from './workspace.ts';

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

export type DeliveryTargetsView = {
  /** Readable environments only: an unreadable one is absent, never redacted (D7). */
  readonly reports: readonly EnvironmentReports[];
  /** Listings that failed for any reason other than "not readable". */
  readonly failures: readonly { readonly environment: EnvRef; readonly error: Error }[];
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
): DeliveryTargetsView {
  const reports: EnvironmentReports[] = [];
  const failures: { environment: EnvRef; error: Error }[] = [];
  environments.forEach((environment, index) => {
    const result = results[index];
    if (result?.data !== undefined) {
      reports.push({ environment, list: result.data });
    } else if (result?.error != null && !(result.error instanceof ApiError && result.error.status === 404)) {
      failures.push({ environment, error: result.error });
    }
  });
  return { reports, failures, isPending: results.some((r) => r.isPending) };
}

/**
 * useDeliveryTargets lists the reports in every environment of a project.
 * Reports are environment-scoped (`read` on the environment, D7), so the
 * project-scoped surface fans out like the lease listing does. Inside a remote
 * workspace the transport sends each call to the remote itself (D11).
 */
export function useDeliveryTargets(
  p: { readonly org: string; readonly project: string },
  environments: readonly EnvRef[],
): DeliveryTargetsView {
  const transport = useTransport();
  return useQueries({
    queries: environments.map((env) => ({
      queryKey: ['delivery-targets', p.org, p.project, env.id] as const,
      queryFn: () =>
        parsed(listDeliveryTargetsOp, {
          path: { org: p.org, project: p.project, environment: env.id },
          ...transport,
        }),
    })),
    combine: (results) => combineDeliveryTargets(environments, results),
  });
}

/**
 * deliveryTargetsRefusalText names a failed listing. Inside a workspace the
 * two remote failures take the multi-instance words, and they stay apart: a
 * rejected credential and an unreachable remote have different fixes. Either
 * way the environment's reports are `unknown` here, never empty.
 */
export function deliveryTargetsRefusalText(error: Error, environment: string, remote: boolean): string {
  if (error instanceof WorkspaceError) {
    return error.message;
  }
  if (remote && error instanceof ApiError && error.status === 401) {
    return `${remoteStateText('credential-rejected')}: the remote refused this workspace's credential, so the reports for ${environment} are unknown. Reconnect to continue.`;
  }
  // fetch rejects with a TypeError when the request never got an answer.
  if (remote && error instanceof TypeError) {
    return `${remoteStateText('unreachable')}: the remote did not answer, so the reports for ${environment} are unknown.`;
  }
  return `The delivery-target reports for ${environment} could not be read, so they are unknown here. Reload to try again.`;
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

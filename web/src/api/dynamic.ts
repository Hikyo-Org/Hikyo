import { listLeasesOp } from '@hikyo/operations';
import { zDynamicLease } from '@hikyo/zod';
import { useQueries } from '@tanstack/react-query';
import type { z } from 'zod';

import { parsed } from './client.ts';
import { useTransport } from './transport.tsx';

export type DynamicLease = z.infer<typeof zDynamicLease>;

/** A lease paired with the name of the environment it belongs to. */
export type LeaseRow = { readonly environmentName: string; readonly lease: DynamicLease };

export type LeasesView = {
  readonly rows: readonly LeaseRow[];
  readonly isPending: boolean;
  readonly isError: boolean;
};

type EnvRef = { readonly id: string; readonly name: string };

const leasesKey = (org: string, project: string, env: string) =>
  ['dynamic-leases', org, project, env] as const;

/**
 * useLeases lists the dynamic-secret leases across a project's environments.
 * Leases are environment-scoped, so the machine-access surface (project-scoped)
 * fans out over the environments and flattens the result. Status and metadata
 * only: the credential is disclosed once, at mint, and never read back.
 */
export function useLeases(
  p: { readonly org: string; readonly project: string },
  environments: readonly EnvRef[],
): LeasesView {
  const transport = useTransport();
  return useQueries({
    queries: environments.map((env) => ({
      queryKey: leasesKey(p.org, p.project, env.id),
      queryFn: () =>
        parsed(listLeasesOp, {
          path: { org: p.org, project: p.project, environment: env.id },
          ...transport,
        }),
      retry: false,
    })),
    combine: (results): LeasesView => ({
      rows: environments.flatMap((env, index) =>
        (results[index]?.data?.items ?? []).map((lease) => ({ environmentName: env.name, lease })),
      ),
      isPending: results.some((r) => r.isPending),
      isError: results.some((r) => r.isError),
    }),
  });
}
